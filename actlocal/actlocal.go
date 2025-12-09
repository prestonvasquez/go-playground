package actlocal

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const workingDir = "/workspace"

// TeardownFunc is a function that tears down resources used during testing.
type TeardownFunc func(t *testing.T)

type options struct {
	image      string
	actVersion string
	repo       string
	branch     string
	secrets    map[string]string
}

// Option is a function that configures New.
type Option func(*options)

// WithImage configures the Docker image used for the container.
// Default is "docker:dind" (Docker-in-Docker) which provides Docker support needed by act.
func WithImage(image string) Option {
	return func(o *options) {
		o.image = image
	}
}

// WithActVersion configures the specific version of act to install.
// Default is "latest" which installs the most recent version.
func WithActVersion(version string) Option {
	return func(o *options) {
		o.actVersion = version
	}
}

// WithRepo configures the Git repository to clone into the container.
func WithRepo(repo string) Option {
	return func(o *options) {
		o.repo = repo
	}
}

// WithBranch configures the Git branch to checkout in the container.
func WithBranch(branch string) Option {
	return func(o *options) {
		o.branch = branch
	}
}

// WithSecrets configures secrets to be made available to act during execution.
func WithSecrets(secrets map[string]string) Option {
	return func(o *options) {
		o.secrets = secrets
	}
}

// New creates a new container with act installed and returns the container
// and a TeardownFunc to clean up resources.
//
// The container runs Docker-in-Docker by default to support act's Docker requirements.
// Act is installed using the official installation script from the nektos/act repository.
func New(t *testing.T, ctx context.Context, optionFuncs ...Option) (testcontainers.Container, TeardownFunc) {
	t.Helper()

	opts := &options{
		image:      "docker:dind",
		actVersion: "latest",
		repo:       "https://github.com/mongodb/mongo-go-driver",
		branch:     "master",
	}
	for _, apply := range optionFuncs {
		apply(opts)
	}

	// Create container request
	req := testcontainers.ContainerRequest{
		Image:      opts.image,
		Privileged: true, // Required for Docker-in-Docker
		WaitingFor: wait.ForLog("API listen on"),
		WorkingDir: workingDir,

		// These environment variables ensure act can find Docker inside the
		// container since we're using a working directory.
		Env: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/root/.local/bin:/workspace/bin",
		},
	}

	if len(opts.secrets) > 0 {
		// If secrets are provided, mount them as environment variables
		for k, v := range opts.secrets {
			req.Env[k] = v
		}
	}

	actContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "failed to start act container")

	tdFunc := func(t *testing.T) {
		t.Helper()

		require.NoError(t, testcontainers.TerminateContainer(actContainer),
			"failed to terminate act container")
	}

	exec(t, actContainer, tdFunc, []string{"git", "clone", opts.repo, workingDir}...)
	t.Logf("Cloned repository %s into container", opts.repo)

	exec(t, actContainer, tdFunc, []string{"git", "-C", workingDir, "checkout", opts.branch}...)
	t.Logf("Checked out branch %s", opts.branch)

	installCmd := []string{
		"sh", "-c",
		"apk add --no-cache curl bash && curl --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/nektos/act/master/install.sh | bash",
	}

	exec(t, actContainer, tdFunc, installCmd...)
	t.Log("Installed act in container")

	return actContainer, tdFunc
}

func exec(t *testing.T, ctr testcontainers.Container, td TeardownFunc, args ...string) {
	t.Helper()

	// Build a shell-wrapped command that keeps ordering deterministic and streams
	// only through the attached reader.
	shellQuote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	var b strings.Builder
	for i, a := range args {
		if i > 0 { b.WriteByte(' ') }
		b.WriteString(shellQuote(a))
	}
	cmd := "cd " + workingDir + " && export PATH=\"$PATH:/root/.local/bin:/workspace/bin\" && set -o pipefail; stdbuf -oL -eL " + b.String()
	wrapped := []string{"sh", "-lc", cmd}

	t.Logf("Executing: %v", args)
	exitCode, reader, err := ctr.Exec(context.Background(), wrapped)
	if err != nil {
		if td != nil { td(t) }
		t.Fatalf("exec %v failed: %v", args, err)
	}

	s := bufio.NewScanner(reader)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Text()
		if line != "" { t.Log(line) }
	}
	if scanErr := s.Err(); scanErr != nil && scanErr != io.EOF {
		t.Logf("stream error: %v", scanErr)
	}

	if exitCode != 0 {
		if td != nil { td(t) }
		t.Fatalf("command %v exited with code %d", args, exitCode)
	}

	t.Logf("Executed command %v successfully", args)
}

func Exec(t *testing.T, ctr testcontainers.Container, args ...string) {
	exec(t, ctr, nil, args...)
}
