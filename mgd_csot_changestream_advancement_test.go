package goplayground

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Proof test for: "the change-stream open blocks waiting for cluster-time /
// shard advancement." Prediction: on an idle cluster the open blocks ~budget;
// with continuous writes during the open, it returns fast. If the latency flips,
// the open is waiting for advancement, not doing real work.
func TestMGD_CSOT_ChangeStreamWaitsForAdvancement(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	require.NotEmpty(t, uri, "set MONGODB_URI to a sharded (mongos) deployment")

	// Pin to a single mongos.
	base := options.Client().ApplyURI(uri)
	require.NotEmpty(t, base.Hosts)
	firstHost := base.Hosts[0]
	o := options.Client().ApplyURI(uri)
	o.Hosts = []string{firstHost}

	client, err := mongo.Connect(o)
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	// Confirm sharded + report shard count.
	var hello struct {
		Msg string `bson:"msg"`
	}
	require.NoError(t, client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
	if hello.Msg != "isdbgrid" {
		t.Skipf("requires sharded (mongos); got msg=%q", hello.Msg)
	}
	var ls struct {
		Shards []bson.Raw `bson:"shards"`
	}
	require.NoError(t, client.Database("admin").RunCommand(ctx, bson.D{{Key: "listShards", Value: 1}}).Decode(&ls))
	t.Logf("shard count: %d", len(ls.Shards))

	measure := func(name string, active bool) time.Duration {
		// Recent write (initialData equivalent): advances operationTime to ~now.
		_ = client.Database("test").Collection("coll").Drop(ctx)
		require.NoError(t, client.Database("test").CreateCollection(ctx, "coll"))

		var wg sync.WaitGroup
		stop := make(chan struct{})
		if active {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					_, _ = client.Database("test").Collection("coll").InsertOne(ctx, bson.D{{Key: "x", Value: 1}})
					time.Sleep(20 * time.Millisecond)
				}
			}()
		}

		c, cancel := context.WithTimeout(ctx, 2000*time.Millisecond)
		defer cancel()
		start := time.Now()
		cs, err := client.Watch(c, mongo.Pipeline{})
		d := time.Since(start)

		close(stop)
		wg.Wait()

		t.Logf("%-7s open took %v err=%v", name, d, err)
		if err == nil {
			_ = cs.Close(ctx)
		}
		return d
	}

	idle := measure("idle", false)
	active := measure("active", true)
	t.Logf("RESULT: idle=%v  active=%v", idle, active)
}
