package main

import (
	"log"
	"os"
	"os/exec"
)

// Use drivers-evergreen-tools to run a load-balanced server on the latest
// MongoDB version. This script is used by Evergreen and can also be run
// locally.
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
		"MONGODB_VERSION=latest",
		"TOPOLOGY=load_balanced",
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("run-server.sh: %v", err)
	}
}
