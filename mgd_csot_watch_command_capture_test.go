package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Capture the exact aggregate command the driver's Watch() helper sends, so it
// can be diffed against the fast raw change-stream aggregate.
func TestMGD_CSOT_WatchCommandCapture(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	mon := &event.CommandMonitor{
		Started: func(_ context.Context, e *event.CommandStartedEvent) {
			if e.CommandName == "aggregate" || e.CommandName == "getMore" {
				t.Logf("%s command: %s", e.CommandName, e.Command.String())
			}
		},
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetMonitor(mon))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(ctx)

	c, cancel := context.WithTimeout(ctx, 1000*time.Millisecond)
	defer cancel()

	start := time.Now()
	cs, err := client.Watch(c, mongo.Pipeline{})
	t.Logf("Watch returned after %v, err=%v", time.Since(start), err)
	if err == nil {
		cs.Close(ctx)
	}
}
