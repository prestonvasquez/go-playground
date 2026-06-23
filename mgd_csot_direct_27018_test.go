package goplayground

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Pin a client to 27018 only (directConnection) and send the raw change-stream
// aggregate with a 1000ms context. If it hangs ~1000ms, 27018 itself is dead
// for reads; if it returns fast, the node is fine and it's the Watch read path.
func TestMGD_CSOT_Direct27018(t *testing.T) {
	ctx := context.Background()

	client, err := mongo.Connect(options.Client().
		ApplyURI("mongodb://localhost:27018/?directConnection=true"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(ctx)

	c, cancel := context.WithTimeout(ctx, 1000*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = client.Database("admin").RunCommand(c, bson.D{
		{Key: "aggregate", Value: 1},
		{Key: "pipeline", Value: bson.A{bson.D{{Key: "$changeStream", Value: bson.D{{Key: "allChangesForCluster", Value: true}}}}}},
		{Key: "cursor", Value: bson.D{}},
	}).Err()
	t.Logf("raw change-stream aggregate to 27018 returned after %v, err=%v", time.Since(start), err)
}
