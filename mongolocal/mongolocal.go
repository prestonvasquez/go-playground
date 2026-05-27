package mongolocal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/mongo"

	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"

	mongov1 "go.mongodb.org/mongo-driver/mongo"
	mongooptionsv1 "go.mongodb.org/mongo-driver/mongo/options"
)

const (
	oidcDefaultClientID      = "0oadp0hpl7q3UIehP297"
	oidcDefaultTokenFileName = "test_machine"
	oidcDefaultDir           = "/tmp/oidc"
	oidcSecretsFileName      = "secrets.json"
)

// TeardownFunc is a function that tears down resources used during testing.
type TeardownFunc func(t *testing.T)

type options struct {
	mongoClientOpts      *mongooptions.ClientOptions
	mongoClientOptsV1    *mongooptionsv1.ClientOptions
	image                string
	enableTestCommands   bool
	replSetName          string
	oidcConfig           *OIDCConfig
	bypassAutoEncryption *bool  // nil = default (true)
	cryptSharedLibPath   string // crypt_shared library path for auto-encryption
	hostPort             int    // 0 = let testcontainers pick a free port
	containerName        string // empty = let testcontainers generate one

	// extraContainerOpts is populated internally for cases (like OIDC) where
	// callers need to inject testcontainers customizers that depend on
	// *testing.T-only setup steps. Not exposed via a public Option.
	extraContainerOpts []testcontainers.ContainerCustomizer
}

// OIDCConfig configures OIDC authentication for the MongoDB container.
// When provided to WithOIDC, the container will be started with OIDC enabled.
//
// Before using OIDC, you must run the setup script to generate secrets and tokens:
//
//	./scripts/setup-oidc.sh
//
// This will create /tmp/oidc/secrets.json and /tmp/oidc/test_machine.
type OIDCConfig struct {
	// Dir is the directory containing OIDC secrets and tokens.
	// Defaults to "/tmp/oidc".
	// Expected files:
	//   - secrets.json: OIDC secrets from AWS Secrets Manager
	//   - test_machine: JWT token file for testing
	Dir string

	// Artifacts is populated after New() completes with OIDC connection info.
	// Use this to get the OIDC URI and token file paths for testing.
	Artifacts *OIDCArtifacts
}

// OIDCArtifacts contains the generated OIDC tokens and configuration
// needed for testing.
type OIDCArtifacts struct {
	// TokenDir is the directory containing generated token files.
	TokenDir string

	// TokenFile is the path to the default token file (test_user1).
	TokenFile string

	// URI is the connection string with authMechanism=MONGODB-OIDC.
	URI string
}

// NewAtlasLocalOption is a function that configures NewAtlasLocal.
type Option func(*options)

// WithMongoClientOptions configures the mongo.Client options used to connect
func WithMongoClientOptions(opts *mongooptions.ClientOptions) Option {
	return func(o *options) {
		o.mongoClientOpts = opts
	}
}

// WithMongoClientOptionsV1 configures the v1 mongo.Client options used to
// connect.
//
// This cannot be used together with WithMongoClientOptions.
func WithMongoClientOptionsV1(opts *mongooptionsv1.ClientOptions) Option {
	return func(o *options) {
		o.mongoClientOptsV1 = opts
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

// mongoShell returns the shell binary name for the given image. The legacy
// "mongo" shell was removed in MongoDB 6.0; use "mongosh" from 6.0 onwards.
func mongoShell(image string) string {
	re := regexp.MustCompile(`mongo:?(\d+)\.`)
	matches := re.FindStringSubmatch(image)
	if len(matches) >= 2 {
		if major, err := strconv.Atoi(matches[1]); err == nil && major < 6 {
			return "mongo"
		}
	}
	return "mongosh"
}

// WithReplicaSet configures the MongoDB container to run as a replica set with
// the given name.
func WithReplicaSet(replSetName string) Option {
	return func(o *options) {
		o.replSetName = replSetName
	}
}

// WithHostPort pins the container's 27017/tcp to a fixed host port. If 0
// (the default), testcontainers picks a random free port.
func WithHostPort(port int) Option {
	return func(o *options) {
		o.hostPort = port
	}
}

// WithContainerName sets a fixed container name. If unset, testcontainers
// generates one.
func WithContainerName(name string) Option {
	return func(o *options) {
		o.containerName = name
	}
}

// WithOIDC enables OIDC authentication on the MongoDB container.
// This will:
//   - Fetch OIDC secrets from AWS Secrets Manager
//   - Generate JWT tokens for testing
//   - Configure MongoDB with OIDC identity providers
//   - Implicitly enable replica set (required for OIDC)
//
// The cfg parameter must not be nil.
func WithOIDC(cfg *OIDCConfig) Option {
	return func(o *options) {
		o.oidcConfig = cfg
	}
}

// WithTLS configures the MongoDB container to require TLS. caHostPath is a
// PEM-encoded CA file the server uses to validate client certificates;
// serverPemHostPath is a PEM bundle containing the server's certificate
// followed by its unencrypted private key.
//
// Both files are mounted read-only into the container at fixed paths and
// mongod is launched with the corresponding --tls* flags. The image's
// docker-entrypoint.sh adds --bind_ip_all when no bind flag is provided, so
// the listener is reachable through testcontainers' port mapping.
//
// Callers that need to connect must supply matching TLS material via
// WithMongoClientOptions (e.g. options.Client().SetTLSConfig(...)); StartT
// will pass the URI through and the driver will negotiate TLS during its
// initial ping.
func WithTLS(caHostPath, serverPemHostPath string) Option {
	return func(o *options) {
		o.extraContainerOpts = append(o.extraContainerOpts,
			testcontainers.WithFiles(
				testcontainers.ContainerFile{
					HostFilePath:      caHostPath,
					ContainerFilePath: "/etc/mongo-tls/ca.pem",
					FileMode:          0o644,
				},
				testcontainers.ContainerFile{
					HostFilePath:      serverPemHostPath,
					ContainerFilePath: "/etc/mongo-tls/server.pem",
					FileMode:          0o644,
				},
			),
			testcontainers.WithCmdArgs(
				"--tlsMode", "requireTLS",
				"--tlsCAFile", "/etc/mongo-tls/ca.pem",
				"--tlsCertificateKeyFile", "/etc/mongo-tls/server.pem",
			),
		)
	}
}

type newResult struct {
	clientV2   *mongo.Client
	clientV1   *mongov1.Client
	teardown   TeardownFunc
	connString string
}

// Env provides access to the underlying test environment.
type Env struct {
	connString string
}

// ConnectionString returns the MongoDB connection URI.
func (e *Env) ConnectionString() string {
	return e.connString
}

// startContainer is the testless core: it applies the given options, starts a
// mongo container via testcontainers, and returns the container along with a
// usable connection string. The caller is responsible for terminating the
// container on error after it is returned successfully (errors during this
// function tear down anything that was started).
//
// OIDC is not supported here because OIDC setup currently requires *testing.T;
// use New / NewWithEnv for tests that need OIDC.
func startContainer(ctx context.Context, opts *options) (*mongodb.MongoDBContainer, string, error) {
	var containerOpts []testcontainers.ContainerCustomizer

	if needsCustomWaitStrategy(opts.image) {
		waitStrategy := wait.ForAll(
			wait.ForLog("(?i)waiting for connections").AsRegexp().WithOccurrence(1),
			wait.ForListeningPort("27017/tcp"),
		)
		containerOpts = append(containerOpts, testcontainers.WithWaitStrategy(waitStrategy))
	}

	if opts.enableTestCommands {
		containerOpts = append(containerOpts,
			testcontainers.WithCmdArgs("--setParameter", "enableTestCommands=1"))
	}

	if opts.replSetName != "" {
		containerOpts = append(containerOpts, mongodb.WithReplicaSet(opts.replSetName))
	}

	if opts.hostPort != 0 || opts.containerName != "" {
		req := testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Name: opts.containerName,
			},
		}
		if opts.hostPort != 0 {
			req.HostConfigModifier = func(hc *container.HostConfig) {
				hc.PortBindings = nat.PortMap{
					"27017/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(opts.hostPort)}},
				}
			}
		}
		containerOpts = append(containerOpts, testcontainers.CustomizeRequest(req))
	}

	containerOpts = append(containerOpts, opts.extraContainerOpts...)

	c, err := mongodb.Run(ctx, opts.image, containerOpts...)
	if err != nil {
		return nil, "", fmt.Errorf("start mongo container: %w", err)
	}

	connString, err := c.ConnectionString(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, "", fmt.Errorf("connection string: %w", err)
	}

	if opts.replSetName != "" {
		host, err := c.Host(ctx)
		if err != nil {
			_ = testcontainers.TerminateContainer(c)
			return nil, "", fmt.Errorf("container host: %w", err)
		}
		port, err := c.MappedPort(ctx, "27017")
		if err != nil {
			_ = testcontainers.TerminateContainer(c)
			return nil, "", fmt.Errorf("mapped port: %w", err)
		}

		if opts.hostPort != 0 {
			// Fixed host port: rewrite the replset member to a host-routable
			// address so SDAM works without directConnection=true. (The default
			// module initiates with the container's internal Docker IP.)
			memberHost := fmt.Sprintf("127.0.0.1:%d", opts.hostPort)
			reconfig := fmt.Sprintf(
				`cfg = rs.conf(); cfg.members[0].host = %q; rs.reconfig(cfg, {force: true})`,
				memberHost,
			)
			shell := mongoShell(opts.image)
			if rc, _, execErr := c.Exec(ctx, []string{shell, "--quiet", "--eval", reconfig}); execErr != nil || rc != 0 {
				_ = testcontainers.TerminateContainer(c)
				return nil, "", fmt.Errorf("rs.reconfig: rc=%d err=%v", rc, execErr)
			}
			waitPrimary := `for (var i=0;i<60;i++) { try { if (db.hello().isWritablePrimary) quit(0); } catch (e) {} sleep(500); } quit(1)`
			if rc, _, execErr := c.Exec(ctx, []string{shell, "--quiet", "--eval", waitPrimary}); execErr != nil || rc != 0 {
				_ = testcontainers.TerminateContainer(c)
				return nil, "", fmt.Errorf("primary not ready after reconfig: rc=%d err=%v", rc, execErr)
			}
			connString = fmt.Sprintf("mongodb://localhost:%d/?replicaSet=%s", opts.hostPort, opts.replSetName)
		} else {
			// Random host port: SDAM can't reach the container IP from the host,
			// so bypass replset discovery with directConnection=true.
			connString = fmt.Sprintf("mongodb://%s:%s/?directConnection=true", host, port.Port())
		}
	}

	return c, connString, nil
}

// Cleanup tears down resources started by Start. It is safe to call from a
// defer that fires when the program receives SIGINT/SIGTERM.
type Cleanup func() error

// Start spins up a mongo container using the same options as New, but without
// requiring *testing.T. Intended for long-running dev scripts (CLI helpers,
// docker-compose replacements) where the caller blocks on a signal and the
// container is torn down by a deferred Cleanup.
//
// OIDC is not supported via Start; use New / NewWithEnv for that.
func Start(ctx context.Context, optionFuncs ...Option) (*Env, Cleanup, error) {
	opts := &options{image: "mongo:latest"}
	for _, apply := range optionFuncs {
		apply(opts)
	}

	c, connString, err := startContainer(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	cleanup := Cleanup(func() error {
		return testcontainers.TerminateContainer(c)
	})
	return &Env{connString: connString}, cleanup, nil
}

func startContainerT(t *testing.T, ctx context.Context, optionFuncs ...Option) *newResult {
	t.Helper()

	opts := &options{
		image: "mongo:latest",
	}

	for _, apply := range optionFuncs {
		apply(opts)
	}

	// Both v1 and v2 mongo client options cannot be set.
	require.False(t, opts.mongoClientOpts != nil && opts.mongoClientOptsV1 != nil,
		"mongo.Client options v1 and v2 cannot both be set")

	// OIDC requires a replica set.
	require.True(t, opts.oidcConfig == nil || opts.oidcConfig != nil, "OIDC requires using a replica set")

	// The only image supported for oidcs is enterprise. If anything other than
	// "mongo:latest" is defined, we should throw an error.
	require.True(t, opts.oidcConfig == nil || opts.image == "mongodb/mongodb-enterprise-server:latest" ||
		opts.image == "mongo:latest", "OIDC requires using the mongodb/mongodb-enterprise-server:latest image")

	if opts.oidcConfig != nil {
		// OIDC requires the enterprise server image.
		opts.image = "mongodb/mongodb-enterprise-server:latest"
	}

	// Handle OIDC setup if enabled. OIDC must run before startContainer so its
	// --setParameter args can be appended to the container command via
	// opts.extraContainerOpts.
	var oidcArtifacts *OIDCArtifacts
	if opts.oidcConfig != nil {
		artifacts, providers := setupOIDC(t, opts.oidcConfig)
		oidcArtifacts = artifacts
		t.Log("Enabling OIDC authentication in mongod")

		opts.extraContainerOpts = append(opts.extraContainerOpts,
			testcontainers.WithCmdArgs(
				"--setParameter", "authenticationMechanisms=SCRAM-SHA-1,SCRAM-SHA-256,MONGODB-OIDC",
				"--setParameter", "oidcIdentityProviders="+providers,
			))
	}

	if opts.enableTestCommands {
		t.Log("Enabling test commands in mongod")
	}
	if opts.replSetName != "" {
		t.Logf("Configuring replica set with name %s", opts.replSetName)
	}

	mongolocalContainer, connString, err := startContainer(ctx, opts)
	require.NoError(t, err, "failed to start mongolocal container")

	tdFunc := func(t *testing.T) {
		t.Helper()

		require.NoError(t, testcontainers.TerminateContainer(mongolocalContainer),
			"failed to terminate mongolocal container")
	}

	// Populate OIDC artifacts if enabled.
	if oidcArtifacts != nil {
		oidcArtifacts.URI = connString + "&authMechanism=MONGODB-OIDC"
		opts.oidcConfig.Artifacts = oidcArtifacts
		t.Logf("OIDC enabled:")
		t.Logf("  Token file: %s", oidcArtifacts.TokenFile)
		t.Logf("  OIDC URI: %s", oidcArtifacts.URI)
	}

	mopts := opts.mongoClientOpts
	if mopts == nil {
		mopts = mongooptions.Client()

		// Users can't override the connection string.
		mopts = mopts.ApplyURI(connString)
	}

	moptsV1 := opts.mongoClientOptsV1
	if moptsV1 != nil {
		// v1 only applies if explicitly requested.

		// Users can't override the connection string.
		moptsV1 = moptsV1.ApplyURI(connString)
	}

	result := &newResult{connString: connString}

	if moptsV1 != nil {
		t.Log("Using v1 mongo client as requested")

		mongoClientV1, err := mongov1.Connect(ctx, moptsV1.ApplyURI(connString))
		require.NoError(t, err, "failed to connect to v1 mongo client")

		result.clientV1 = mongoClientV1
		result.teardown = func(t *testing.T) {
			t.Helper()

			require.NoError(t, mongoClientV1.Disconnect(ctx), "failed to disconnect v1 mongo client")
			tdFunc(t)
		}

		require.Eventually(t, func() bool {
			err := mongoClientV1.Ping(ctx, nil)
			return err == nil
		}, 60*time.Second, 5*time.Second)

		t.Log("Connected to mongolocal MongoDB V1 instance")
	} else {
		t.Log("Using v2 mongo client as requested")

		// The default is v2 client.
		mongoClient, err := mongo.Connect(mopts.ApplyURI(connString))
		require.NoError(t, err, "failed to connect to mongo client")

		result.clientV2 = mongoClient
		result.teardown = func(t *testing.T) {
			t.Helper()

			require.NoError(t, mongoClient.Disconnect(ctx), "failed to disconnect mongo client")
			tdFunc(t)
		}

		require.Eventually(t, func() bool {
			err := mongoClient.Ping(ctx, nil)
			return err == nil
		}, 60*time.Second, 5*time.Second)

		t.Log("Connected to mongolocal MongoDB V2 instance")
	}

	return result
}

// StartT creates a new MongoDB test container and returns a connected mongo.Client
// and a TeardownFunc to clean up resources.
func StartT(t *testing.T, ctx context.Context, optionFuncs ...Option) (*mongo.Client, TeardownFunc) {
	result := startContainerT(t, ctx, optionFuncs...)

	return result.clientV2, result.teardown
}

// StartTWithEnv creates a new MongoDB test container and returns a connected
// mongo.Client, a TeardownFunc, and an Env for accessing the underlying
// test environment.
func StartTWithEnv(t *testing.T, ctx context.Context, optionFuncs ...Option) (*mongo.Client, TeardownFunc, *Env) {
	result := startContainerT(t, ctx, optionFuncs...)

	env := &Env{connString: result.connString}

	return result.clientV2, result.teardown, env
}

// StartTV1 creates a new MongoDB test container and returns a connected v1
// mongo.Client
func StartTV1(t *testing.T, ctx context.Context, optionFuncs ...Option) (*mongov1.Client, TeardownFunc) {
	opts := &options{}
	for _, apply := range optionFuncs {
		apply(opts)
	}

	if opts.mongoClientOptsV1 == nil {
		optionFuncs = append(optionFuncs, WithMongoClientOptionsV1(mongooptionsv1.Client()))
	}

	result := startContainerT(t, ctx, optionFuncs...)

	return result.clientV1, result.teardown
}

// ArbDB returns a database with an arbitrary name intended for one-off use in
// tests.
func ArbDB(client *mongo.Client) *mongo.Database {
	return client.Database(uuid.New().String())
}

// ArbColl returns a collection with an arbitrary name in an arbitrary database
// intended for one-off use in tests.
func ArbColl(client *mongo.Client) *mongo.Collection {
	return ArbDB(client).Collection(uuid.New().String())
}

// ArbDBV1 returns a database with an arbitrary name intended for one-off use in
// v1 tests.
func ArbDBV1(client *mongov1.Client) *mongov1.Database {
	return client.Database(uuid.New().String())
}

// ArbCollV1 returns a collection with an arbitrary name in an arbitrary
// database intended for one-off use in v1 tests.
func ArbCollV1(client *mongov1.Client) *mongov1.Collection {
	return ArbDBV1(client).Collection(uuid.New().String())
}

// oidcSecrets holds the secrets fetched from AWS Secrets Manager.
type oidcSecrets struct {
	Issuer1URI   string `json:"OIDC_ISSUER_1_URI"`
	Issuer2URI   string `json:"OIDC_ISSUER_2_URI"`
	JWKSURI      string `json:"OIDC_JWKS_URI"`
	RSAKey       string `json:"OIDC_RSA_KEY"`
	ClientSecret string `json:"OIDC_CLIENT_SECRET"`
	Domain       string `json:"OIDC_DOMAIN"`
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true // File exists and no error occurred
	}

	// Check if the error is specifically due to the file not existing
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	return false
}

// setupOIDC prepares OIDC authentication by reading secrets and tokens from
// the local filesystem. Returns the artifacts and the JSON string for
// oidcIdentityProviders.
//
// Prerequisites: Run ./scripts/setup-oidc.sh to generate secrets and tokens.
func setupOIDC(t *testing.T, cfg *OIDCConfig) (*OIDCArtifacts, string) {
	t.Helper()

	t.Log("Setting up OIDC authentication")

	// Set defaults.
	oidcDir := cfg.Dir
	if oidcDir == "" {
		oidcDir = oidcDefaultDir
	}

	secretsFile := filepath.Join(oidcDir, oidcSecretsFileName)
	tokenFile := filepath.Join(oidcDir, "test_user1")

	artifacts := &OIDCArtifacts{
		TokenDir:  oidcDir,
		TokenFile: tokenFile,
	}

	// Check that required files exist.
	if !fileExists(secretsFile) || !fileExists(tokenFile) {
		t.Fatalf(`OIDC setup required. Run the setup script first:

    AWS_PROFILE=<your-profile> ./scripts/setup-oidc.sh

Expected files:
    %s
    %s
`, secretsFile, tokenFile)
	}

	// Read secrets from local file.
	secrets := readOIDCSecrets(t, secretsFile)

	// Build the oidcIdentityProviders JSON.
	providersJSON := buildOIDCProvidersJSON(t, secrets)

	return artifacts, providersJSON
}

// readOIDCSecrets reads OIDC secrets from a local JSON file.
func readOIDCSecrets(t *testing.T, path string) *oidcSecrets {
	t.Helper()
	t.Logf("Reading OIDC secrets from %s", path)

	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading OIDC secrets file")

	var secrets oidcSecrets
	require.NoError(t, json.Unmarshal(data, &secrets), "parsing OIDC secrets JSON")

	return &secrets
}

// oidcProvider represents a single OIDC identity provider configuration.
type oidcProvider struct {
	AuthNamePrefix     string   `json:"authNamePrefix"`
	Issuer             string   `json:"issuer"`
	ClientID           string   `json:"clientId"`
	Audience           string   `json:"audience"`
	AuthorizationClaim string   `json:"authorizationClaim"`
	MatchPattern       string   `json:"matchPattern"`
	RequestScopes      []string `json:"requestScopes,omitempty"`
}

// buildOIDCProvidersJSON builds the JSON string for the oidcIdentityProviders parameter.
func buildOIDCProvidersJSON(t *testing.T, secrets *oidcSecrets) string {
	t.Helper()

	providers := []oidcProvider{
		{
			AuthNamePrefix:     "test1",
			Issuer:             secrets.Issuer1URI,
			ClientID:           oidcDefaultClientID,
			Audience:           oidcDefaultClientID,
			AuthorizationClaim: "foo",
			MatchPattern:       "test_user1",
			RequestScopes:      []string{"fizz", "buzz"},
		},
		{
			AuthNamePrefix:     "test2",
			Issuer:             secrets.Issuer2URI,
			ClientID:           oidcDefaultClientID,
			Audience:           oidcDefaultClientID,
			AuthorizationClaim: "bar",
			MatchPattern:       "test_user2",
			RequestScopes:      []string{"foo", "bar"},
		},
	}

	data, err := json.Marshal(providers)
	require.NoError(t, err, "marshaling OIDC providers JSON")

	return string(data)
}
