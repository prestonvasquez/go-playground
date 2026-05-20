package mongolocal

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// localKMSKeyLen is the master-key length the "local" KMS provider requires.
const localKMSKeyLen = 96

// WithBypassAutoEncryption sets AutoEncryptionOptions.BypassAutoEncryption.
// Default is true: the driver does not try to spawn mongocryptd on client
// creation and writes go through the normal (non-encrypted) wire path. Set
// to false to exercise the real auto-encryption write path — requires
// mongocryptd in PATH or crypt_shared configured via WithCryptSharedLibPath.
func WithBypassAutoEncryption(bypass bool) Option {
	return func(o *options) {
		o.bypassAutoEncryption = &bypass
	}
}

// WithCryptSharedLibPath sets cryptSharedLibPath in AutoEncryptionOptions
// ExtraOptions so the driver loads MongoDB's crypt_shared library at the
// given path instead of spawning mongocryptd. crypt_shared is the .dylib /
// .so available from MongoDB Enterprise downloads.
func WithCryptSharedLibPath(path string) Option {
	return func(o *options) {
		o.cryptSharedLibPath = path
	}
}

// NewCSFLE is mongolocal.New with CSFLE pre-wired: it spins up a sibling
// MongoDB container, generates an ephemeral 96-byte master key for the
// "local" KMS provider, returns a v2 mongo.Client whose
// AutoEncryptionOptions are populated against that key, and a
// ClientEncryption constructed against the same client and key so explicit
// encryption uses the same key as auto-encryption.
//
// Requires libmongocrypt installed on the host and the test compiled with
// `-tags cse`. Without the build tag, ClientEncryption construction panics
// at runtime — callers of NewCSFLE are expected to opt in.
//
// Defaults BypassAutoEncryption(true) so the driver doesn't try to spawn
// mongocryptd on client creation. Cases that need the real auto-encryption
// write path (which reaches AppendBatchArray in the driver) should pass
// WithBypassAutoEncryption(false) together with WithCryptSharedLibPath.
func NewCSFLE(t *testing.T, ctx context.Context, optionFuncs ...Option) (
	*mongo.Client, *mongo.ClientEncryption, TeardownFunc,
) {
	t.Helper()

	opts := &options{}
	for _, apply := range optionFuncs {
		apply(opts)
	}

	masterKey := make([]byte, localKMSKeyLen)
	_, err := rand.Read(masterKey)
	require.NoError(t, err, "generate local KMS master key")

	kmsProviders := map[string]map[string]any{
		"local": {"key": masterKey},
	}
	const keyVaultNS = "encryption.__keyVault"

	bypass := true
	if opts.bypassAutoEncryption != nil {
		bypass = *opts.bypassAutoEncryption
	}

	autoEnc := mongooptions.AutoEncryption().
		SetKmsProviders(kmsProviders).
		SetKeyVaultNamespace(keyVaultNS).
		SetBypassAutoEncryption(bypass)

	if opts.cryptSharedLibPath != "" {
		autoEnc.SetExtraOptions(map[string]any{
			"cryptSharedLibPath": opts.cryptSharedLibPath,
		})
	}

	clientOpts := opts.mongoClientOpts
	if clientOpts == nil {
		clientOpts = mongooptions.Client()
	}
	clientOpts.SetAutoEncryptionOptions(autoEnc)

	combined := append(append([]Option{}, optionFuncs...), WithMongoClientOptions(clientOpts))

	client, teardown := StartT(t, ctx, combined...)

	ce, err := mongo.NewClientEncryption(client, mongooptions.ClientEncryption().
		SetKmsProviders(kmsProviders).
		SetKeyVaultNamespace(keyVaultNS))
	require.NoError(t, err, "NewClientEncryption")

	return client, ce, teardown
}
