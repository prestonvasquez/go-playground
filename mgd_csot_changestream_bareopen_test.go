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

// Minimal: just open a change stream (no preceding write, no failpoint), pinned
// to the mongos, generous budget. Does a bare open cost ~1s, or is the ~1s only
// when a write precedes it?
func TestMGD_CSOT_ChangeStreamBareOpen(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	require.NotEmpty(t, uri, "set MONGODB_URI to a sharded (mongos) deployment")

	base := options.Client().ApplyURI(uri)
	require.NotEmpty(t, base.Hosts)
	o := options.Client().ApplyURI(uri)
	o.Hosts = []string{base.Hosts[0]}

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

	for i := 0; i < 6; i++ {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		start := time.Now()
		cs, err := client.Watch(c, mongo.Pipeline{})
		t.Logf("bare open #%d took %v err=%v", i, time.Since(start), err)
		if err == nil {
			_ = cs.Close(ctx)
		}
		cancel()
		time.Sleep(250 * time.Millisecond)
	}
}
