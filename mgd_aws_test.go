package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/awsauth"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestMGD_AWS exercises the awsauth shim, which replaces the driver's
// built-in MONGODB-AWS authenticator with one backed by aws-sdk-go-v2.
//
// Skipped when MONGODB_URI is unset.
func TestMGD_AWS(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("Skipping AWS test: set MONGODB_URI to an AWS-IAM-enabled cluster")
	}

	awsauth.Register()

	clientOpts := options.Client().
		ApplyURI(uri)
		// SetAuth(options.Credential{AuthMechanism: "MONGODB-AWS"})

	client, err := mongo.Connect(clientOpts)
	require.NoError(t, err, "connect")

	t.Cleanup(func() {
		require.NoError(t, client.Disconnect(context.Background()), "disconnect")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, client.Ping(ctx, nil), "ping with AWS SDK authenticator")
	t.Log("AWS SDK authentication successful!")
}
