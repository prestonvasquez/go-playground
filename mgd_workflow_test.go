package goplayground

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/actlocal"
)

//const (
//	mongoDriverPath = "/Users/preston.vasquez/Developer/mongo-go-driver"
//)

func TestCodeQLWorkflow(t *testing.T) {
	// Create a new act container
	container, teardown := actlocal.New(t, context.Background(),
		actlocal.WithRepo("https://github.com/RafaelCenzano/mongo-go-driver"),
		actlocal.WithBranch("GODRIVER-3573"),
	)

	defer teardown(t)

	actlocal.Exec(t, container, "act", "--version")
	actlocal.Exec(t, container, "act", "--list")
	actlocal.Exec(t, container, "act", "pull_request", "-W", ".github/workflows/codeql.yml", "-j", "analyze",
		"-P", "ubuntu-latest=catthehacker/ubuntu:act-latest", "--pull=false")
	//// Read token from GITHUB_PAT for full CodeQL runs; skip if not present
	//githubToken := os.Getenv("GITHUB_PAT")
	//if githubToken == "" {
	//	t.Skip("GITHUB_PAT not set; skipping full CodeQL run")
	//}

	//// Copy the mongo-go-driver repository into the container
	//// We need to copy the entire repository including .github/workflows
	//err := copyDirToContainer(ctx, container, mongoDriverPath, "/workspace")
	//require.NoError(t, err, "failed to copy mongo-go-driver to container")

	//// Fix git detached HEAD state by creating/updating a temporary branch at current commit
	//// This ensures act can properly identify the git ref without changing files
	//t.Log("Fixing git repository state...")
	//exitCode, reader, err := container.Exec(ctx, []string{
	//	"sh", "-c",
	//	"apk add --no-cache git >/dev/null 2>&1 && git config --global --add safe.directory /workspace && cd /workspace && BR=$(git rev-parse --short HEAD) && git checkout -B act-test-$BR >/dev/null 2>&1 && git symbolic-ref --short HEAD",
	//})
	//if err != nil {
	//	t.Logf("Warning: failed to fix git state: %v", err)
	//}
	//branchOut, _ := io.ReadAll(reader)
	//if exitCode != 0 {
	//	t.Logf("Warning: git branch fix exited with code %d: %s", exitCode, string(branchOut))
	//} else {
	//	t.Logf("Current branch inside container: %s", string(branchOut))
	//}

	//// Determine whether to emulate a pull_request (preferred for reproducing PR failures)
	//prNum := os.Getenv("PR_NUMBER")
	//baseRef := os.Getenv("BASE_REF")
	//if baseRef == "" {
	//	baseRef = "master"
	//}

	//// Build event payload inside the container
	//if prNum != "" {
	//	// pull_request event payload with base/head and explicit ref
	//	prCmd := `PR=` + prNum + ` BASE=` + baseRef + `; cd /workspace && BR=$(git symbolic-ref --short HEAD || git rev-parse --short HEAD) && SHA=$(git rev-parse HEAD) && cat > /tmp/event.json <<'JSON'
	//{"//ref":"refs/pull/${PR}/merge","pull_request":{"number":${PR},"base":{"ref":"${BASE}"},"head":{"ref":"${BR}","sha":"${SHA}"}}}
	//JS//ON`
	//	//	exitCode, reader, err = container.Exec(ctx, []string{"sh", "-lc", prCmd})
	//	//	require.NoError(t, err, "failed to write pull_request event payload")
	//	//} else {
	//	//	// workflow_call event payload pinned to the current branch
	//	//	exitCode, reader, err = container.Exec(ctx, []string{
	//	//		"sh", "-c",
	//	//		"cd /workspace && BR=$(git symbolic-ref --short HEAD || git rev-parse --short HEAD) && printf '{\"ref\":\"refs/heads/%s\",\"inputs\":{\"ref\":\"%s\"}}' \"$BR\" \"$BR\" > /tmp/event.json",
	//	//	})
	//	//	require.NoError(t, err, "failed to write workflow_call event payload")
	//	//}
	//
	//	//// Show the event payload (truncated) for debugging
	//	//exitCode, reader, err = container.Exec(ctx, []string{"sh", "-c", "head -c 500 /tmp/event.json || true"})
	//	//require.NoError(t, err)
	//	//payload, _ := io.ReadAll(reader)
	//	//t.Logf("Event payload (first 500 bytes): %s", string(payload))
	//
	//	//// Build a custom job image with Go preinstalled (based on act-latest)
	//	//buildCmd := `cat > /tmp/act-go.Dockerfile <<'DOCKER'
	//FR//OM catthehacker/ubuntu:act-latest
	//RU//N apt-get update && apt-get install -y --no-install-recommends golang ca-certificates && rm -rf /var/lib/apt/lists/*
	//EN//V PATH=/usr/lib/go/bin:$PATH
	//RU//N go version || true
	//DO//CKER
	//
	//do//cker build --platform linux/amd64 -t local/ubuntu-act-go:latest -f /tmp/act-go.Dockerfile /tmp`
	//	//exitCode, reader, err = container.Exec(ctx, []string{"sh", "-lc", buildCmd})
	//	//require.NoError(t, err, "failed to build custom job image with Go")
	//	//if exitCode != 0 {
	//	//	out, _ := io.ReadAll(reader)
	//	//	t.Fatalf("docker build failed (%d): %s", exitCode, string(out))
	//	//}
	//
	//	//// List available workflows to verify they're accessible
	//	//t.Log("Listing available workflows...")
	//	//exitCode, reader, err = container.Exec(ctx, []string{
	//	//	"sh", "-c", "cd /workspace && act --list",
	//	//})
	//	//require.NoError(t, err, "failed to list workflows")
	//
	//	//output, _ := io.ReadAll(reader)
	//	//t.Logf("Available workflows:\n%s", string(output))
	//
	//	//if exitCode != 0 {
	//	//	t.Logf("Warning: act --list exited with code %d", exitCode)
	//	//}
	//
	//	//// Run the CodeQL workflow with dry-run first to see what would execute
	//	//// Using --platform flag to specify runner image and avoid interactive prompt
	//	//t.Log("Running CodeQL workflow (dry-run)...")
	//
	//	//// Build the act command with proper flags (always full run, no --dryrun)
	//	//// Choose event type based on whether we're emulating a PR
	//	//eventType := "workflow_call"
	//	//if prNum != "" {
	//	//	eventType = "pull_request"
	//	//}
	//ac//tCmd := fmt.Sprintf("cd /workspace && act %s --workflows .github/workflows/codeql.yml --platform ubuntu-latest=local/ubuntu-act-go:latest --container-architecture linux/amd64 --eventpath /tmp/event.json --pull=false", eventType)

	//// Token is required (we already skipped if missing); pass it as GITHUB_TOKEN secret
	//actCmd += fmt.Sprintf(" --secret GITHUB_TOKEN=%s", githubToken)

	// cmdArgs := []string{"sh", "-c", actCmd}

	// exitCode, reader, err = container.Exec(ctx, cmdArgs)
	// require.NoError(t, err, "failed to execute act command")

	// output, err = io.ReadAll(reader)
	// require.NoError(t, err, "failed to read act output")

	// t.Logf("Act output (truncated):\n%s", string(output))

	//// For PR reproduction we expect a non-zero exit here matching the CI failure mode
	//require.NotEqual(t, 0, exitCode, "expected non-zero exit to reproduce CI failure")
	//// Common failure we expect to see on this branch when analyze runs
	//require.Contains(t, string(output), "CodeQL detected code written in Go but this run didn't build any of it")
}

//// copyDirToContainer copies an entire directory (including .git) to a container using docker cp
//func copyDirToContainer(ctx context.Context, container testcontainers.Container, srcPath, dstPath string) error {
//	// Get the container ID
//	containerID := container.GetContainerID()
//
//	// First create the destination directory in the container
//	_, _, err := container.Exec(ctx, []string{"mkdir", "-p", dstPath})
//	if err != nil {
//		return fmt.Errorf("failed to create destination directory: %w", err)
//	}
//
//	// Use docker cp from the host to copy the entire directory
//	// The /. syntax copies the contents of srcPath into dstPath
//	cmd := fmt.Sprintf("docker cp %s/. %s:%s", srcPath, containerID, dstPath)
//
//	// Execute docker cp using os/exec (runs on host, not in container)
//	execCmd := []string{"/bin/sh", "-c", cmd}
//	output, err := runHostCommand(execCmd...)
//	if err != nil {
//		return fmt.Errorf("docker cp failed: %w, output: %s", err, string(output))
//	}
//
//	return nil
//}
//
//// runHostCommand runs a command on the host system
//func runHostCommand(args ...string) ([]byte, error) {
//	cmd := exec.Command(args[0], args[1:]...)
//	return cmd.CombinedOutput()
//}
