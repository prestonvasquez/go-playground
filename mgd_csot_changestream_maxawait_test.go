package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Confirm the ~1s sharded change-stream open is the maxAwaitTimeMS await: vary
// maxAwaitTimeMS and see if the open latency tracks it.
func TestMGD_CSOT_ChangeStreamMaxAwait(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	require.NotEmpty(t, uri, "set MONGODB_URI to a sharded (mongos) deployment")

	base := options.Client().ApplyURI(uri)
	require.NotEmpty(t, base.Hosts)
	firstHost := base.Hosts[0]
	o := options.Client().ApplyURI(uri)
	o.Hosts = []string{firstHost}

	client, err := mongo.Connect(o)
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	var hello struct {
		Msg string `bson:"msg"`
	}
	require.NoError(t, client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
	if hello.Msg != "isdbgrid" {
		t.Skipf("requires sharded (mongos); got msg=%q", hello.Msg)
	}

	open := func(name string, csOpts *options.ChangeStreamOptionsBuilder) {
		_ = client.Database("test").Collection("coll").Drop(ctx)
		require.NoError(t, client.Database("test").CreateCollection(ctx, "coll"))

		// Generous budget so the open never times out; we only measure latency.
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		start := time.Now()
		cs, err := client.Watch(c, mongo.Pipeline{}, csOpts)
		t.Logf("%-18s open took %v err=%v", name, time.Since(start), err)
		if err == nil {
			_ = cs.Close(ctx)
		}
	}

	open("default", options.ChangeStream())
	open("maxAwait=100ms", options.ChangeStream().SetMaxAwaitTime(100*time.Millisecond))
	open("maxAwait=3000ms", options.ChangeStream().SetMaxAwaitTime(3000*time.Millisecond))
}
