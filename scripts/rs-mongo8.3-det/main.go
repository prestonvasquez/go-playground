package main

import (
	"log"
	"os"
	"os/exec"
)

// Use to run old server versions not supported by the OSs (Ubuntu 22.04 and
// RHEL 8.0) that are not supported by drivers-evergreen-tools.
func main() {
	var (
		driversEvergreenTools = os.Getenv("DRIVERS_TOOLS")
		workingDir            = driversEvergreenTools + "/.evergreen/docker"
	)

	if err := os.Chdir(workingDir); err != nil {
		log.Fatalf("chdir: %v", err)
	}

	cmd := exec.Command("./run-server.sh")

	cmd.Env = append(os.Environ(),
		"MONGODB_VERSION=8.3",
		"TOPOLOGY=replica_set",
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("run-server.sh: %v", err)
	}
}
