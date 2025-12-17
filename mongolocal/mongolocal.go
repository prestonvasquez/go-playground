package mongolocal

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

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
	replSetName        string
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

// WithReplicaSet configures the MongoDB container to run as a replica set with
// the given name.
func WithReplicaSet(replSetName string) Option {
	return func(o *options) {
		o.replSetName = replSetName
	}
}

// New creates a new MongoDB test container and returns a connected mongo.Client
// and a TeardownFunc to clean up resources.
func New(t *testing.T, ctx context.Context, optionFuncs ...Option) (*mongo.Client, TeardownFunc) {
	t.Helper()

	opts := &options{
		image: "mongo:latest",
	}

	for _, apply := range optionFuncs {
		apply(opts)
	}

	// MongoDB 4.x and earlier use lowercase "waiting for connections"
	// Only override the default wait strategy for these older versions
	var containerOpts []testcontainers.ContainerCustomizer
	if needsCustomWaitStrategy(opts.image) {
		waitStrategy := wait.ForAll(
			wait.ForLog("(?i)waiting for connections").AsRegexp().WithOccurrence(1),
			wait.ForListeningPort("27017/tcp"),
		)
		containerOpts = append(containerOpts, testcontainers.WithWaitStrategy(waitStrategy))
	}

	// Enable test commands if requested
	if opts.enableTestCommands {
		t.Log("Enabling test commands in mongod")

		containerOpts = append(containerOpts,
			testcontainers.WithCmdArgs("--setParameter", "enableTestCommands=1"))
	}

	if opts.replSetName != "" {
		t.Logf("Configuring replica set with name %s", opts.replSetName)

		containerOpts = append(containerOpts,
			mongodb.WithReplicaSet(opts.replSetName))
	}

	mongolocalContainer, err := mongodb.Run(ctx, opts.image, containerOpts...)
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

	if opts.replSetName != "" {
		// This is a bug in testcontainers-go where the replica set name is not
		// included. No idea why it matters.
		host, err := mongolocalContainer.Host(ctx)
		require.NoError(t, err, "failed to get container host")

		port, err := mongolocalContainer.MappedPort(ctx, "27017")
		require.NoError(t, err, "failed to get mapped port")

		connString = fmt.Sprintf("mongodb://%s:%s/?directConnection=true", host, port.Port())
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

	require.Eventually(t, func() bool {
		err := mongoClient.Ping(ctx, nil)
		return err == nil
	}, 60*time.Second, 5*time.Second)

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
