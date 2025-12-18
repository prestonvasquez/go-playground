package goplayground

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

var versions = []string{
	"1.19", // Minimum supported Go version for mongo-driver v2
	"1.20",
	"1.21",
	"1.22",
	"1.23",
	"1.24",
	"1.25", // Test suite Go Version
}

var architectures = []string{
	"386",
	"amd64",
	"arm",
	"arm64",
	"mips",
	"mips64",
	"mips64le",
	"mipsle",
	"ppc64",
	"ppc64le",
	"riscv64",
	"s390x",
}

const mainSrc = `package main

import (
    "fmt"

    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
    "go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
    _, _ = mongo.Connect(options.Client())
    fmt.Println(bson.D{{Key: "key", Value: "value"}})
}
`

func TestCompileCheck(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	// Build the image and start one container we can reuse for all subtests.
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:       filepath.Join(cwd, "docker"),
			Dockerfile:    "build.Dockerfile",
			PrintBuildLog: true,
		},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(mainSrc),
				ContainerFilePath: "/workspace/main.go",
				FileMode:          0o644,
			},
		},
		Entrypoint: []string{"tail", "-f", "/dev/null"},
		WorkingDir: "/workspace",
	}

	genReq := testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true}

	container, err := testcontainers.GenericContainer(context.Background(), genReq)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, container.Terminate(context.Background()))
	}()

	testSuiteVersion := versions[len(versions)-1]

	// Initialize Go module and download dependencies for this version.
	_ = execGo(t, container, &goExecConfig{version: testSuiteVersion}, "mod", "init", "example.com/mymodule")
	_ = execGo(t, container, &goExecConfig{version: testSuiteVersion}, "mod", "tidy")

	// Edit go.mod to set minimum version to what the driver claims
	execContainer(t, container, fmt.Sprintf(`sed -i 's/^go %s\(\.0\)\?$/go %s/' /workspace/go.mod`, testSuiteVersion, versions[0]))

	for _, ver := range versions {
		ver := ver // capture
		t.Run("go:"+ver, func(t *testing.T) {
			// Ensure the correct Go version is selected.
			out := execGo(t, container, &goExecConfig{version: ver}, "version")
			require.Contains(t, out, "go"+ver, "unexpected go version: %s", out)

			// Ensure the code builds with this version.
			t.Run("build", func(t *testing.T) {
				_ = execGo(t, container, &goExecConfig{version: ver}, "build", "./...")
			})

			// Build with cse,gssapi,mongointernal tags.
			t.Run("build-tags", func(t *testing.T) {
				_ = execGo(t, container, &goExecConfig{
					version: ver,
					env: map[string]string{
						"PKG_CONFIG_PATH": "/root/install/libmongocrypt/lib/pkgconfig",
						"CGO_CFLAGS":      "-I/root/install/libmongocrypt/include",
						"CGO_LDFLAGS":     "-L/root/install/libmongocrypt/lib -Wl,-rpath,/root/install/libmongocrypt/lib",
						"CGO_ENABLED":     "1",
					},
				}, "build", "-tags=cse,gssapi,mongointernal", ".")
			})

			// Build for each architecture.
			for _, architecture := range architectures {
				architecture := architecture // capture
				t.Run("build/"+architecture, func(t *testing.T) {
					_ = execGo(t, container, &goExecConfig{
						version: ver,
						env: map[string]string{
							"GOOS":   "linux",
							"GOARCH": architecture,
						},
					}, "build", ".")
				})
			}
		})
	}
}

// execContainer runs a shell command inside the container using `bash -lc`.
// It fails the test if the command exits with a non‑zero status and returns
// the command's stdout as a string.
func execContainer(t *testing.T, c testcontainers.Container, cmd string) string {
	t.Helper()

	exit, out, err := c.Exec(context.Background(), []string{"bash", "-lc", cmd})
	require.NoError(t, err)

	b, err := io.ReadAll(out)
	require.NoError(t, err)

	// If the command failed, show full output.
	require.Equal(t, 0, exit, "command failed: %s", b)

	s := string(b)
	// Strip any leading non‑printable bytes (some Docker/TTY combos do this).
	for len(s) > 0 && s[0] < 0x20 {
		s = s[1:]
	}
	return s
}

type goExecConfig struct {
	version string            // Optional: Go version to use with GOTOOLCHAIN. If empty, uses default.
	env     map[string]string // Optional: Additional environment variables.
}

// execGo runs `go <args...>` inside the container.
// If cfg.version is set, uses GOTOOLCHAIN to select that version.
// If cfg is nil or version is empty, uses the default toolchain on PATH.
func execGo(t *testing.T, c testcontainers.Container, cfg *goExecConfig, args ...string) string {
	t.Helper()

	if cfg == nil {
		cfg = &goExecConfig{}
	}

	// Build environment variables string
	var envStr strings.Builder
	envStr.WriteString("PATH=/usr/local/go/bin:$PATH")
	for k, v := range cfg.env {
		envStr.WriteString(fmt.Sprintf(" %s='%s'", k, strings.ReplaceAll(v, "'", "'\\''")))
	}

	goArgs := strings.Join(args, " ")

	// If no version specified, use default toolchain
	if cfg.version == "" {
		cmd := fmt.Sprintf("%s go %s", envStr.String(), goArgs)
		return execContainer(t, c, cmd)
	}

	// Use GOTOOLCHAIN to select the specific Go version
	cmd := fmt.Sprintf(
		"%[1]s GOTOOLCHAIN=go%[2]s.0+auto go %[3]s || "+
			"%[1]s GOTOOLCHAIN=go%[2]s go %[3]s",
		envStr.String(),
		cfg.version,
		goArgs,
	)

	return execContainer(t, c, cmd)
}
