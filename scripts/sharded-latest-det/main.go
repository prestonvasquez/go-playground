package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	version = "latest"
)

// Start a sharded cluster against the latest MongoDB version using
// drivers-evergreen-tools, run it in the foreground, and tear it down on
// Ctrl-C. This script is used by Evergreen and can also be run locally.
//
// We invoke run-orchestration.sh (the Python mongo-orchestration backend)
// rather than the docker run-server.sh path. The docker path runs
// `make run-server` -> `run-mongodb.sh start`, which forces the experimental
// `--mongodb-runner` backend. That backend brings up the config server and
// shard replica sets but never completes addShard for sharded_cluster
// topologies, leaving config.shards empty. The result is a mongos with no
// shards, so any database creation fails with:
//
//	(ShardNotFound) ... No non-draining shard found
//
// mongo-orchestration performs addShard correctly, so we use it here.
func main() {
	driversTools := os.Getenv("DRIVERS_TOOLS")
	if driversTools == "" {
		log.Fatal("DRIVERS_TOOLS is not set")
	}

	// Remove the .bin dir from driversTools
	binDir := filepath.Join(driversTools, ".bin")
	if err := os.RemoveAll(binDir); err != nil {
		log.Fatalf("removing %s: %v", binDir, err)
	}

	// run-orchestration.sh writes the cluster URI and CSFLE crypt_shared path
	// to mo-expansion.{sh,yml} in the working directory, and the URI to
	// $DRIVERS_TOOLS/uri.txt. Capture the working dir so we can report them.
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}

	// By default mongo-orchestration downloads binaries to
	// $DRIVERS_TOOLS/mongodb/bin. On a machine that previously ran the docker
	// (root) path, that directory can be left root-owned and unwritable, which
	// breaks extraction. Use a writable location unless the caller overrides it.
	if os.Getenv("MONGODB_BINARIES") == "" {
		os.Setenv("MONGODB_BINARIES", filepath.Join(driversTools, "mongodb-local", "bin"))
	}

	env := append(os.Environ(),
		fmt.Sprintf("MONGODB_VERSION=%s", version),
		"TOPOLOGY=sharded_cluster",
	)

	// Notify before starting so a Ctrl-C during the (potentially slow) download
	// and provisioning step still results in a clean teardown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Start the cluster. run-orchestration.sh provisions via mongo-orchestration
	// and returns once the cluster is up (the daemon keeps running in the
	// background), so we block in the foreground ourselves below.
	startErr := run(env, filepath.Join(driversTools, ".evergreen", "run-orchestration.sh"))

	teardown := func() {
		fmt.Println("\nStopping sharded cluster...")
		if err := run(env, filepath.Join(driversTools, ".evergreen", "orchestration", "drivers-orchestration"), "stop"); err != nil {
			log.Fatalf("stopping cluster: %v", err)
		}
		fmt.Println("Sharded cluster stopped.")
	}

	// If we were interrupted during startup, tear down whatever came up.
	select {
	case <-sig:
		teardown()
		return
	default:
	}
	if startErr != nil {
		log.Fatalf("starting cluster: %v", startErr)
	}

	reportConnectionInfo(driversTools, workDir)

	fmt.Println("\nSharded cluster is running. Press Ctrl-C to stop and tear it down.")
	<-sig
	teardown()
}

// run executes a command, wiring it to the current stdio, with the given env.
func run(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// reportConnectionInfo prints the cluster URI and the environment variables a
// caller likely needs to export (MONGODB_URI, CRYPT_SHARED_LIB_PATH) to talk to
// the cluster, including a copy-paste line to source them into the shell.
func reportConnectionInfo(driversTools, workDir string) {
	expansion := filepath.Join(workDir, "mo-expansion.sh")

	fmt.Println("\n========================================================================")
	fmt.Println("Sharded cluster is up.")

	if uri, err := os.ReadFile(filepath.Join(driversTools, "uri.txt")); err == nil {
		fmt.Printf("Cluster URI: %s\n", strings.TrimSpace(string(uri)))
	}

	if data, err := os.ReadFile(expansion); err == nil {
		fmt.Println("\nExport these into your shell to use the cluster:")
		for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				fmt.Printf("  export %s\n", line)
			}
		}
		fmt.Printf("\nOr source them all at once:\n  set -a; source %q; set +a\n", expansion)
	} else {
		fmt.Printf("\n(no mo-expansion.sh found in %s)\n", workDir)
	}
	fmt.Println("========================================================================")
}
