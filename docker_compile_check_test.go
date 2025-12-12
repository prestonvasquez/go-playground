package goplayground

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

var versions = []string{
	"1.19",
	"1.20",
	"1.21",
	"1.22",
	"1.23",
	"1.24",
	"1.25",
}

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
		Entrypoint: []string{"tail", "-f", "/dev/null"},
		WorkingDir: "/workspace",
	}

	genReq := testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true}

	container, err := testcontainers.GenericContainer(context.Background(), genReq)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, container.Terminate(context.Background()))
	}()

	for _, ver := range versions {
		ver := ver // capture
		t.Run("go:"+ver, func(t *testing.T) {
			cmd := fmt.Sprintf("PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=go%[1]s.0+auto go version || PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=go%[1]s go version", ver)

			exit, out, err := container.Exec(context.Background(), []string{"bash", "-lc", cmd})
			require.NoError(t, err)

			b, err := io.ReadAll(out)

			require.NoError(t, err)
			require.Equal(t, 0, exit, "go version failed: %s", b)
			require.Contains(t, string(b), "go"+ver, "unexpected go version: %s", b)
		})
	}
}
