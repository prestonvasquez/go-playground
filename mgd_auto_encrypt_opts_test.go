//go:build aeoust
// +build aeoust

package goplayground

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMGDAutoEncryptOpts(t *testing.T) {
	// How can we create an idiomatic solution for decoding autoEncryptOpts from
	// a unified spec test?
	//
	// https://github.com/mongodb/specifications/blob/master/source/unified-test-format/unified-test-format.md#entity

	// Set environment variables for $$placeholder substitution
	t.Setenv("CSFLE_AWS_TEMP_ACCESS_KEY_ID", "tempAccessKeyIdValue")
	t.Setenv("CSFLE_AWS_TEMP_SECRET_ACCESS_KEY", "tempSecretAccessKeyValue")
	t.Setenv("CSFLE_AWS_TEMP_SESSION_TOKEN", "tempSessionTokenValue")

	var ce clientEntity
	require.NoError(t, bson.UnmarshalExtJSON(testJSON, false, &ce))

	require.NotNil(t, ce.AutoEncryptOpts)
	require.NotNil(t, ce.AutoEncryptOpts.KMSProviders.AWS)

	require.Equal(t, "tempAccessKeyIdValue", ce.AutoEncryptOpts.KMSProviders.AWS.AccessKeyID)
	require.Equal(t, "tempSecretAccessKeyValue", ce.AutoEncryptOpts.KMSProviders.AWS.SecretAccessKey)
	require.Equal(t, "tempSessionTokenValue", ce.AutoEncryptOpts.KMSProviders.AWS.SessionToken)

	providers := getKMSPRoviders(t, &ce)

	require.NotNil(t, providers["aws"])
	require.Equal(t, "tempAccessKeyIdValue", providers["aws"]["accessKeyId"])
	require.Equal(t, "tempSecretAccessKeyValue", providers["aws"]["secretAccessKey"])
	require.Equal(t, "tempSessionTokenValue", providers["aws"]["sessionToken"])
}

type awsKMSProvider struct {
	AccessKeyID     string `bson:"accessKeyId"`
	SecretAccessKey string `bson:"secretAccessKey"`
	SessionToken    string `bson:"sessionToken,omitempty"`
}

type KMSProviders struct {
	AWS *awsKMSProvider `bson:"aws"`
}

type autoEncryptOpts struct {
	KMSProviders         KMSProviders   `bson:"kmsProviders,omitempty"`
	SchemaMap            map[string]any `bson:"schemaMap,omitempty"`
	KeyVaultNamespace    string         `bson:"keyVaultNamespace,omitempty"`
	BypassAutoEncryption bool           `bson:"bypassAutoEncryption,omitempty"`
	EncryptedFieldsMap   map[string]any `bson:"encryptedFieldsMap,omitempty"`
	BypassQueryAnalysis  bool           `bson:"bypassQueryAnalysis,omitempty"`
}

type clientEntity struct {
	AutoEncryptOpts *autoEncryptOpts `bson:"autoEncryptOpts"`
}

func (awskp *awsKMSProvider) UnmarshalBSON(data []byte) error {
	raw := bson.Raw(data)

	// Reject unknown fields
	allowed := map[string]struct{}{
		"accessKeyId":     {},
		"secretAccessKey": {},
		"sessionToken":    {},
	}

	elems, err := raw.Elements()
	if err != nil {
		return err
	}

	for _, el := range elems {
		if _, ok := allowed[el.Key()]; !ok {
			return fmt.Errorf("invalid field %q in aws kms provider", el.Key())
		}
	}

	// Decode into a temporary struct
	type awsAlias awsKMSProvider
	var tmp awsAlias
	if err := bson.Unmarshal(data, &tmp); err != nil {
		return err
	}

	// Apply placeholder logic

	// Defaults (non-temp credentials)
	accessKeyEnv := "FLE_AWS_KEY"
	secretKeyEnv := "FLE_AWS_SECRET"

	// If sessionToken == "$$placeholder", switch to temp creds
	if tmp.SessionToken == "$$placeholder" {
		tmp.SessionToken = os.Getenv("CSFLE_AWS_TEMP_SESSION_TOKEN")
		accessKeyEnv = "CSFLE_AWS_TEMP_ACCESS_KEY_ID"
		secretKeyEnv = "CSFLE_AWS_TEMP_SECRET_ACCESS_KEY"
	}

	// If fields are missing/empty, backfill from env
	if tmp.AccessKeyID == "" {
		tmp.AccessKeyID = os.Getenv(accessKeyEnv)
	}
	if tmp.SecretAccessKey == "" {
		tmp.SecretAccessKey = os.Getenv(secretKeyEnv)
	}

	// Assign back to the receiver
	*awskp = awsKMSProvider(tmp)

	return nil
}

// mapify converts a struct to a map[string]any via BSON marshalling.
func mapify(t *testing.T, key string, v any) map[string]any {
	t.Helper()

	if v == nil {
		return nil
	}

	raw, err := bson.MarshalExtJSON(v, false, false)
	require.NoError(t, err)

	m := make(map[string]any)
	require.NoError(t, bson.UnmarshalExtJSON(raw, false, &m))

	return m
}

// Need some way to convert KMSProviders back into a map[string]map[string]any
// to pass to SetKMSProviders.
func getKMSPRoviders(t *testing.T, ce *clientEntity) map[string]map[string]any {
	t.Helper()

	providers := make(map[string]map[string]any)

	providers["aws"] = mapify(t, "aws", ce.AutoEncryptOpts.KMSProviders.AWS)

	return providers
}

var testJSON = []byte(`
{
  "autoEncryptOpts": {
    "keyVaultNamespace": "keyvault.datakeys",
    "schemaMap": {
      "default.default": {
        "bsonType": "object",
        "properties": {
          "encrypted_string": {
            "encrypt": {
              "bsonType": "string",
              "algorithm": "AEAD_AES_256_CBC_HMAC_SHA_512-Deterministic",
              "keyId": [
                {
                  "$binary": {
                    "base64": "OyQRAeK7QlWMr0E2xWapYg==",
                    "subType": "04"
                  }
                }
              ]
            }
          }
        }
      }
    },
    "kmsProviders": {
      "aws": {
        "accessKeyId": "",
        "secretAccessKey": "",
        "sessionToken": "$$placeholder"
      }
    }
  }
}`)
