package goplayground

import (
	"context"
	"os"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_OIDC(t *testing.T) {
	ctx := context.Background()

	// Read the OIDC token from the default location.
	const tokenFile = "/tmp/oidc/test_user1"
	tokenBytes, err := os.ReadFile(tokenFile)
	require.NoError(t, err, "reading token file (run ./scripts/setup-oidc.sh first)")
	token := string(tokenBytes)

	// Create an OIDC callback that returns the token.
	oidcCallback := func(ctx context.Context, args *options.OIDCArgs) (*options.OIDCCredential, error) {
		return &options.OIDCCredential{
			AccessToken: token,
		}, nil
	}

	// Configure client options with OIDC authentication.
	clientOpts := options.Client().SetAuth(options.Credential{
		AuthMechanism:       "MONGODB-OIDC",
		OIDCMachineCallback: oidcCallback,
	})

	// Start MongoDB with OIDC enabled and connect using OIDC auth.
	client, teardown := mongolocal.New(t, ctx,
		mongolocal.WithOIDC(&mongolocal.OIDCConfig{}),
		mongolocal.WithMongoClientOptions(clientOpts))
	defer teardown(t)

	// Verify OIDC authentication works by pinging.
	err = client.Ping(ctx, nil)
	require.NoError(t, err, "ping with OIDC client should succeed")

	t.Log("OIDC authentication successful!")
}
