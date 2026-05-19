// Smoke test for mongolocal.NewCSFLE. Requires libmongocrypt installed on
// the host (e.g. `brew install libmongocrypt` on macOS,
// `apt-get install libmongocrypt-dev` on Debian-likes) and:
//
//	go test -tags cse -run TestCSFLESmoke ./mongolocal/
package mongolocal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestCSFLESmoke(t *testing.T) {
	ctx := context.Background()

	// NewCSFLE returns the v2 client (auto-encryption pre-configured) and
	// a ClientEncryption sharing the same KMS key and key-vault namespace.
	// Don't ce.Close — it disconnects the shared key-vault client; the
	// outer teardown handles disconnection.
	_, ce, teardown := NewCSFLE(t, ctx)
	defer teardown(t)

	// Create a data encryption key and round-trip a value through it. The
	// CreateDataKey + Encrypt + Decrypt chain exercises libmongocrypt's
	// crypto path end-to-end.
	keyID, err := ce.CreateDataKey(ctx, "local",
		mongooptions.DataKey().SetKeyAltNames([]string{"smoke-test-key"}))
	require.NoError(t, err, "CreateDataKey")

	plaintext := bson.RawValue{Type: bson.TypeString, Value: bsoncoreString("hello-csfle")}
	encrypted, err := ce.Encrypt(ctx, plaintext,
		mongooptions.Encrypt().
			SetKeyID(keyID).
			SetAlgorithm("AEAD_AES_256_CBC_HMAC_SHA_512-Deterministic"))
	require.NoError(t, err, "Encrypt")
	require.NotEqual(t, plaintext.Value, encrypted.Data)

	decrypted, err := ce.Decrypt(ctx, encrypted)
	require.NoError(t, err, "Decrypt")
	require.Equal(t, plaintext.Value, decrypted.Value)

	t.Logf("CSFLE smoke test passed: encrypt/decrypt round-trip succeeded")
}

// bsoncoreString builds the raw-BSON value bytes for a BSON string:
// int32 length-including-NUL || bytes || 0x00.
func bsoncoreString(s string) []byte {
	n := int32(len(s) + 1)
	out := make([]byte, 4+len(s)+1)
	out[0] = byte(n)
	out[1] = byte(n >> 8)
	out[2] = byte(n >> 16)
	out[3] = byte(n >> 24)
	copy(out[4:], s)
	out[4+len(s)] = 0
	return out
}
