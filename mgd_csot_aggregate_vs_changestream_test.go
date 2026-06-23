package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Is the "blocks for the full maxTimeMS" behavior specific to change streams,
// or does a plain aggregate with maxTimeMS block too? Run both, time them.
func TestMGD_CSOT_AggregateVsChangeStream(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(ctx)

	run := func(name string, cmd bson.D) {
		t.Run(name, func(t *testing.T) {
			c, cancel := context.WithTimeout(ctx, 1000*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := client.Database("admin").RunCommand(c, cmd).Err()
			t.Logf("%s returned after %v, err=%v", name, time.Since(start), err)
		})
	}

	// Plain aggregate (non-tailable). maxTimeMS is added by the driver from the
	// 1000ms context deadline (CSOT).
	run("plain_aggregate", bson.D{
		{Key: "aggregate", Value: 1},
		{Key: "pipeline", Value: bson.A{bson.D{{Key: "$documents", Value: bson.A{}}}}},
		{Key: "cursor", Value: bson.D{}},
	})

	// Change-stream aggregate (tailable awaitData) as a raw command.
	run("changestream_aggregate", bson.D{
		{Key: "aggregate", Value: 1},
		{Key: "pipeline", Value: bson.A{bson.D{{Key: "$changeStream", Value: bson.D{{Key: "allChangesForCluster", Value: true}}}}}},
		{Key: "cursor", Value: bson.D{}},
	})

	// Same change stream via the driver's Watch() helper.
	t.Run("watch_helper", func(t *testing.T) {
		c, cancel := context.WithTimeout(ctx, 1000*time.Millisecond)
		defer cancel()
		start := time.Now()
		cs, err := client.Watch(c, mongo.Pipeline{})
		t.Logf("watch_helper returned after %v, err=%v", time.Since(start), err)
		if err == nil {
			cs.Close(ctx)
		}
	})
}
