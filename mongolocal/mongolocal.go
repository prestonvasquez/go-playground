package mongolocal

import (
	"context"
	"regexp"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/mongo"

	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TeardownFunc is a function that tears down resources used during testing.
type TeardownFunc func(t *testing.T)

type options struct {
	mongoClientOpts    *mongooptions.ClientOptions
	image              string
	enableTestCommands bool
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

// WithEnableTestCommands enables MongoDB test commands including failCommand failpoint.
// This adds --setParameter enableTestCommands=1 to the mongod startup.
func WithEnableTestCommands() Option {
	return func(o *options) {
		o.enableTestCommands = true
	}
}

// needsCustomWaitStrategy checks if the MongoDB image version requires a custom
// wait strategy. MongoDB 4.x and earlier use lowercase "waiting for
// connections".
func needsCustomWaitStrategy(image string) bool {
	// Extract version from image string.
	re := regexp.MustCompile(`mongo:?(\d+)\.`)
	matches := re.FindStringSubmatch(image)
	if len(matches) < 2 {
		// Can't determine version, use default wait strategy
		return false
	}

	majorVersion, err := strconv.Atoi(matches[1])
	if err != nil {
		return false
	}

	// MongoDB 4.x and earlier need the custom strategy
	return majorVersion <= 4
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

	// MongoDB 4.x and earlier use lowercase "waiting for connections"
	// Only override the default wait strategy for these older versions
	var containerOpts []testcontainers.ContainerCustomizer
	if needsCustomWaitStrategy(image) {
		waitStrategy := wait.ForAll(
			wait.ForLog("(?i)waiting for connections").AsRegexp().WithOccurrence(1),
			wait.ForListeningPort("27017/tcp"),
		)
		containerOpts = append(containerOpts, testcontainers.WithWaitStrategy(waitStrategy))
	}

	// Enable test commands if requested
	if opts.enableTestCommands {
		containerOpts = append(containerOpts,
			testcontainers.WithCmdArgs("--setParameter", "enableTestCommands=1"))
	}

	mongolocalContainer, err := mongodb.Run(ctx, image, containerOpts...)
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

// ArbColl returns a collection with an arbitrary name in an arbitrary database
// intended for one-off use in tests.
func ArbColl(client *mongo.Client) *mongo.Collection {
	return client.Database(uuid.New().String()).Collection(uuid.New().String())
}
