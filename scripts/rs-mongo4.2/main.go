package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/prestonvasquez/go-playground/mongolocal"
)

// Use to run old server versions not supported by the OSs (Ubuntu 22.04 and
// RHEL 8.0) that are not supported by drivers-evergreen-tools.
func main() {
	image := flag.String("image", "mongo:4.2", "MongoDB image to run")
	name := flag.String("name", "mongo-rs-4.2", "Container name (also visible to docker ps)")
	rs := flag.String("rs", "rs0", "Replica set name")
	port := flag.Int("port", 27017, "Host port to bind container 27017 to")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	env, cleanup, err := mongolocal.Start(context.Background(),
		mongolocal.WithImage(*image),
		mongolocal.WithReplicaSet(*rs),
		mongolocal.WithContainerName(*name),
		mongolocal.WithHostPort(*port),
	)
	if err != nil {
		log.Fatalf("start: %v", err)
	}

	defer func() {
		if err := cleanup(); err != nil {
			log.Printf("cleanup: %v", err)
		}
		log.Println("Cleanup complete.")
	}()

	fmt.Printf("Replica set ready: %s\n", env.ConnectionString())
	fmt.Println("Press Ctrl-C to stop.")

	<-ctx.Done()
	fmt.Println("Shutting down...")
}
