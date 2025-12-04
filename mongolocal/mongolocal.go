package mongolocal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TeardownFunc is a function that tears down resources used during testing.
type TeardownFunc func(t *testing.T)

type options struct {
	mongoClientOpts *mongooptions.ClientOptions
	image           string
}

// NewAtlasLocalOption is a function that configures NewAtlasLocal.
type Option func(*options)

// WithMongoClientOptions configures the mongo.Client options used to connect
func WithMongoClientOptions(opts *mongooptions.ClientOptions) Option {
	return func(o *options) {
		o.mongoClientOpts = opts
	}
}

// WithImage configures the Docker image used for the MongoDB container.
func WithImage(image string) Option {
	return func(o *options) {
		o.image = image
	}
}

// New creates a new MongoDB test container and returns a connected mongo.Client
// and a TeardownFunc to clean up resources.
func New(t *testing.T, ctx context.Context, optionFuncs ...Option) (*mongo.Client, TeardownFunc) {
	t.Helper()

	opts := &options{}
	for _, apply := range optionFuncs {
		apply(opts)
	}

	image := "mongo:latest"
	if opts.image != "" {
		image = opts.image
	}

	mongolocalContainer, err := mongodb.Run(ctx, image)
	require.NoError(t, err, "failed to start atlaslocal container")

	tdFunc := func(t *testing.T) {
		t.Helper()

		require.NoError(t, testcontainers.TerminateContainer(mongolocalContainer),
			"failed to terminate atlaslocal container")
	}

	connString, err := mongolocalContainer.ConnectionString(ctx)
	if err != nil {
		tdFunc(t)
		t.Fatalf("failed to get connection string: %s", err)
	}

	mopts := opts.mongoClientOpts
	if mopts == nil {
		mopts = mongooptions.Client()
	}

	// Users can't override the connection string.
	mopts = mopts.ApplyURI(connString)

	mongoClient, err := mongo.Connect(mopts)
	if err != nil {
		tdFunc(t)
		t.Fatalf("failed to connect to mongo: %s", err)
	}

	return mongoClient, func(t *testing.T) {
		t.Helper()

		require.NoError(t, mongoClient.Disconnect(ctx), "failed to disconnect mongo client")
		tdFunc(t)
	}
}
