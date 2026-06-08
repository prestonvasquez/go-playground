//go:build aeoust
// +build aeoust

package goplayground

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// kmsPlaceholderDoc is the canonical $$placeholder document used in unified
// spec tests: { "$$placeholder": 1 }. A credential field set to this document
// signals that the value should be substituted from an environment variable.
var kmsPlaceholderDoc, _ = bson.Marshal(bson.D{{Key: "$$placeholder", Value: int32(1)}})

func isKMSPlaceholder(v bson.RawValue) bool {
	doc, ok := v.DocumentOK()
	return ok && bytes.Equal([]byte(doc), kmsPlaceholderDoc)
}

func TestMGDAutoEncryptOpts(t *testing.T) {
	// How can we create an idiomatic solution for decoding autoEncryptOpts from
	// a unified spec test?
	//
	// https://github.com/mongodb/specifications/blob/master/source/unified-test-format/unified-test-format.md#entity

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

	providers := getKMSProviders(t, &ce)

	require.NotNil(t, providers["aws:name1"])
	require.Equal(t, "tempAccessKeyIdValue", providers["aws:name1"]["accessKeyId"])
	require.Equal(t, "tempSecretAccessKeyValue", providers["aws:name1"]["secretAccessKey"])
	require.Equal(t, "tempSessionTokenValue", providers["aws:name1"]["sessionToken"])
}

// TestMGDAutoEncryptOpts_NamedProvider confirms that the KMSProviders
// UnmarshalBSON correctly routes any "[provider]:[name]" key to the right
// typed provider struct via strings.Cut, preserving the full original key in
// the output map.
func TestMGDAutoEncryptOpts_NamedProvider(t *testing.T) {
	t.Setenv("CSFLE_AWS_TEMP_ACCESS_KEY_ID", "tempAccessKeyIdValue")
	t.Setenv("CSFLE_AWS_TEMP_SECRET_ACCESS_KEY", "tempSecretAccessKeyValue")
	t.Setenv("CSFLE_AWS_TEMP_SESSION_TOKEN", "tempSessionTokenValue")

	var ce clientEntity
	require.NoError(t, bson.UnmarshalExtJSON(testJSONNamedProvider, false, &ce))

	require.NotNil(t, ce.AutoEncryptOpts)
	require.NotNil(t, ce.AutoEncryptOpts.KMSProviders.AWS)

	providers := getKMSProviders(t, &ce)

	require.NotNil(t, providers["aws:myname"])
	require.Equal(t, "tempAccessKeyIdValue", providers["aws:myname"]["accessKeyId"])
}

type awsKMSProvider struct {
	AccessKeyID     string `bson:"accessKeyId"`
	SecretAccessKey string `bson:"secretAccessKey"`
	SessionToken    string `bson:"sessionToken,omitempty"`
}

type KMSProviders struct {
	AWS    *awsKMSProvider
	AWSKey string // original key, e.g. "aws" or "aws:name1"
}

func (kp *KMSProviders) UnmarshalBSON(data []byte) error {
	elems, err := bson.Raw(data).Elements()
	if err != nil {
		return err
	}
	for _, elem := range elems {
		base, _, _ := strings.Cut(elem.Key(), ":")
		switch base {
		case "aws":
			kp.AWSKey = elem.Key()
			kp.AWS = new(awsKMSProvider)
			if err := bson.Unmarshal(elem.Value().Document(), kp.AWS); err != nil {
				return err
			}
		}
	}
	return nil
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

	// Reject unknown fields.
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

	// Defaults (non-temp credentials).
	accessKeyEnv := "FLE_AWS_KEY"
	secretKeyEnv := "FLE_AWS_SECRET"

	// Check sessionToken first: if it is the $$placeholder document, switch to
	// temporary credential environment variables.
	if v, err := raw.LookupErr("sessionToken"); err == nil {
		if isKMSPlaceholder(v) {
			awskp.SessionToken = os.Getenv("CSFLE_AWS_TEMP_SESSION_TOKEN")
			accessKeyEnv = "CSFLE_AWS_TEMP_ACCESS_KEY_ID"
			secretKeyEnv = "CSFLE_AWS_TEMP_SECRET_ACCESS_KEY"
		} else if s, ok := v.StringValueOK(); ok {
			awskp.SessionToken = s
		}
	}

	if v, err := raw.LookupErr("accessKeyId"); err == nil {
		if isKMSPlaceholder(v) {
			awskp.AccessKeyID = os.Getenv(accessKeyEnv)
		} else if s, ok := v.StringValueOK(); ok {
			awskp.AccessKeyID = s
		}
	}

	if v, err := raw.LookupErr("secretAccessKey"); err == nil {
		if isKMSPlaceholder(v) {
			awskp.SecretAccessKey = os.Getenv(secretKeyEnv)
		} else if s, ok := v.StringValueOK(); ok {
			awskp.SecretAccessKey = s
		}
	}

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

// getKMSProviders converts KMSProviders into a map[string]map[string]any
// suitable for passing to SetKmsProviders, preserving the original key
// (including any "[provider]:[name]" suffix).
func getKMSProviders(t *testing.T, ce *clientEntity) map[string]map[string]any {
	t.Helper()

	providers := make(map[string]map[string]any)

	if kp := ce.AutoEncryptOpts.KMSProviders; kp.AWS != nil {
		key := kp.AWSKey
		if key == "" {
			key = "aws"
		}
		providers[key] = mapify(t, key, kp.AWS)
	}

	return providers
}

// testJSON mirrors the autoEncryptOpts structure used in the unified spec
// tests: named KMS provider keys ("aws:name1") and $$placeholder documents
// ({ "$$placeholder": 1 }) for credentials to be substituted from env vars.
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
      "aws:name1": {
        "accessKeyId":     { "$$placeholder": 1 },
        "secretAccessKey": { "$$placeholder": 1 },
        "sessionToken":    { "$$placeholder": 1 }
      }
    }
  }
}`)

var testJSONNamedProvider = []byte(`
{
  "autoEncryptOpts": {
    "keyVaultNamespace": "keyvault.datakeys",
    "kmsProviders": {
      "aws:myname": {
        "accessKeyId":     { "$$placeholder": 1 },
        "secretAccessKey": { "$$placeholder": 1 },
        "sessionToken":    { "$$placeholder": 1 }
      }
    }
  }
}`)
