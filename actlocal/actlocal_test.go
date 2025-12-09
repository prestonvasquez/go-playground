package actlocal_test

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/actlocal"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	ctx := context.Background()

	// Create a new act container
	container, teardown := actlocal.New(t, ctx)
	defer teardown(t)

	// Verify the container is running
	require.NotNil(t, container)

	// Verify act is installed by checking version
	exitCode, reader, err := container.Exec(ctx, []string{"act", "--version"})
	require.NoError(t, err, "failed to exec act --version")
	require.Equal(t, 0, exitCode, "act --version should exit with code 0")

	// Read and verify output
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	require.NoError(t, err)

	output := string(buf[:n])
	require.Contains(t, output, "act version", "output should contain act version info")
}

//func TestNewWithCustomImage(t *testing.T) {
//	ctx := context.Background()
//
//	// Create container with custom image option
//	container, teardown := actlocal.New(t, ctx, actlocal.WithImage("docker:dind"))
//	defer teardown(t)
//
//	require.NotNil(t, container)
//}
//
//func TestExecAct(t *testing.T) {
//	t.Skip("Skipping exec test - requires workflow files to be mounted")
//
//	ctx := context.Background()
//
//	container, teardown := actlocal.New(t, ctx)
//	defer teardown(t)
//
//	// Example of how to use ExecAct
//	// Note: This would require mounting a GitHub workflow directory
//	output, err := actlocal.ExecAct(ctx, container, "--list")
//	require.NoError(t, err)
//	require.NotEmpty(t, output)
//}
