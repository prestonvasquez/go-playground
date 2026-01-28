package det

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/mongo"

	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TeardownFunc is a function that tears down resources used during testing.
type TeardownFunc func(t *testing.T)

type MongoDBContainer struct {
	testcontainers.Container
	URI string
}

type options struct {
	// MongoDB configuration
	mongoDBVersion string // default: "latest"
	topology       string // server, replica_set, sharded_cluster

	// Docker configuration
	detPath         string // path to drivers-evergreen-tools repo
	dockerfile      string // default: .evergreen/docker/ubuntu22.04/Dockerfile
	mongoClientOpts *mongooptions.ClientOptions
}

// Option is a functional option for configuring the MongoDB container.
type Option func(*options)

// WithMongoDBVersion sets the MongoDB version to use.
func WithMongoDBVersion(version string) Option {
	return func(o *options) {
		o.mongoDBVersion = version
	}
}

// WithTopology sets the MongoDB topology (server, replica_set, sharded_cluster).
func WithTopology(topology string) Option {
	return func(o *options) {
		o.topology = topology
	}
}

// WithDETPath sets the path to the drivers-evergreen-tools repository.
func WithDETPath(path string) Option {
	return func(o *options) {
		o.detPath = path
	}
}

// WithDockerfile sets the path to the Dockerfile relative to DET root.
func WithDockerfile(dockerfile string) Option {
	return func(o *options) {
		o.dockerfile = dockerfile
	}
}

// WithMongoClientOptions configures the mongo.Client options used to connect
func WithMongoClientOptions(opts *mongooptions.ClientOptions) Option {
	return func(o *options) {
		o.mongoClientOpts = opts
	}
}

// New creates a new MongoDB container with the given options.
func New(t *testing.T, ctx context.Context, opts ...Option) (*mongo.Client, TeardownFunc) {
	t.Helper()

	settings := &options{
		mongoDBVersion: "latest",
		topology:       "server",
		detPath:        os.Getenv("DRIVERS_TOOLS"),
		dockerfile:     ".evergreen/docker/ubuntu22.04/Dockerfile",
	}

	for _, apply := range opts {
		apply(settings)
	}

	// The detPath and dockerfile have to exist. If not the test must be skipped.
	if _, err := os.Stat(settings.detPath); os.IsNotExist(err) {
		t.Skipf("DET path %s does not exist", settings.detPath)
	}

	dockerfilePath := filepath.Join(settings.detPath, settings.dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Skipf("Dockerfile %s does not exist in DET path %s", settings.dockerfile, settings.detPath)
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    settings.detPath,
			Dockerfile: settings.dockerfile,
			Repo:       "det-mongodb",
			Tag:        "latest",
			KeepImage:  true,
		},
		Env: map[string]string{
			"MONGODB_VERSION":       settings.mongoDBVersion,
			"TOPOLOGY":              settings.topology,
			"AUTH":                  "noauth",
			"SSL":                   "nossl",
			"ORCHESTRATION_FILE":    "",
			"LOAD_BALANCER":         "",
			"STORAGE_ENGINE":        "",
			"REQUIRE_API_VERSION":   "",
			"DISABLE_TEST_COMMANDS": "",
			"MONGODB_DOWNLOAD_URL":  "",
		},
		Entrypoint: []string{"/root/local-entrypoint.sh"},
		// Use host network mode so replica set members on 127.0.0.1 are accessible
		NetworkMode: "host",
		WaitingFor:  wait.ForLog("send_result(200)"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "failed to start atlaslocal container")

	tdFunc := func(t *testing.T) {
		t.Helper()

		require.NoError(t, testcontainers.TerminateContainer(container),
			"failed to terminate atlaslocal container")
	}

	// Get connection URI based on topology
	connString, err := buildConnectionURI(ctx, container, settings)
	require.NoError(t, err, "failed to build connection URI")

	mopts := settings.mongoClientOpts
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

	pingCtx, pingCancel := context.WithTimeout(ctx, 1*time.Second)
	defer pingCancel()

	if err := mongoClient.Ping(pingCtx, nil); err != nil {
		tdFunc(t)
		t.Fatalf("failed to ping mongo: %s", err)
	}

	return mongoClient, func(t *testing.T) {
		t.Helper()

		require.NoError(t, mongoClient.Disconnect(ctx), "failed to disconnect mongo client")
		tdFunc(t)
	}
}

func buildConnectionURI(ctx context.Context, container testcontainers.Container, cfg *options) (string, error) {
	// With host networking, MongoDB is accessible on localhost with standard ports
	if cfg.topology == "replica_set" {
		// Connect to all three replica set members on localhost
		// The replica set name is "repl0" based on the orchestration config
		return "mongodb://localhost:27017,localhost:27018,localhost:27019/?replicaSet=repl0", nil
	}

	// Standalone server
	return "mongodb://localhost:27017", nil
}

func getExposedPorts(topology string) []string {
	switch topology {
	case "replica_set":
		return []string{"27017/tcp", "27018/tcp", "27019/tcp"}
	case "sharded_cluster":
		return []string{"27017/tcp", "27018/tcp"} // mongos ports
	default: // server (standalone)
		return []string{"27017/tcp"}
	}
}
