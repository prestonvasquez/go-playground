package goplayground

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Prove the fixed ~1s sharded change-stream open is periodicNoopIntervalSecs:
// read it off the shard, change it, and check the open latency tracks it.
func TestMGD_CSOT_ChangeStreamNoopInterval(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	require.NotEmpty(t, uri, "set MONGODB_URI to a sharded (mongos) deployment")

	base := options.Client().ApplyURI(uri)
	require.NotEmpty(t, base.Hosts)
	o := options.Client().ApplyURI(uri)
	o.Hosts = []string{base.Hosts[0]}

	mongosClient, err := mongo.Connect(o)
	require.NoError(t, err)
	defer mongosClient.Disconnect(ctx)

	var hello struct {
		Msg string `bson:"msg"`
	}
	require.NoError(t, mongosClient.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
	if hello.Msg != "isdbgrid" {
		t.Skipf("requires sharded (mongos); got msg=%q", hello.Msg)
	}

	// Find a shard mongod host.
	var ls struct {
		Shards []struct {
			ID   string `bson:"_id"`
			Host string `bson:"host"`
		} `bson:"shards"`
	}
	require.NoError(t, mongosClient.Database("admin").RunCommand(ctx, bson.D{{Key: "listShards", Value: 1}}).Decode(&ls))
	require.NotEmpty(t, ls.Shards)
	host := ls.Shards[0].Host
	if i := strings.Index(host, "/"); i >= 0 { // "rs/host:port,host:port" -> hosts
		host = host[i+1:]
	}
	host = strings.Split(host, ",")[0]
	t.Logf("shard mongod: %s", host)

	shardOpts := options.Client().ApplyURI("mongodb://" + host + "/?directConnection=true")
	shard, err := mongo.Connect(shardOpts)
	require.NoError(t, err)
	defer shard.Disconnect(ctx)

	getNoop := func() int32 {
		var r struct {
			V int32 `bson:"periodicNoopIntervalSecs"`
		}
		require.NoError(t, shard.Database("admin").RunCommand(ctx,
			bson.D{{Key: "getParameter", Value: 1}, {Key: "periodicNoopIntervalSecs", Value: 1}}).Decode(&r))
		return r.V
	}
	setNoop := func(v int32) error {
		return shard.Database("admin").RunCommand(ctx,
			bson.D{{Key: "setParameter", Value: 1}, {Key: "periodicNoopIntervalSecs", Value: v}}).Err()
	}

	orig := getNoop()
	t.Logf("periodicNoopIntervalSecs (original) = %d", orig)
	defer func() { _ = setNoop(orig) }()

	openLatency := func() time.Duration {
		_ = mongosClient.Database("test").Collection("coll").Drop(ctx)
		require.NoError(t, mongosClient.Database("test").CreateCollection(ctx, "coll"))
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		start := time.Now()
		cs, err := mongosClient.Watch(c, mongo.Pipeline{})
		d := time.Since(start)
		if err == nil {
			_ = cs.Close(ctx)
		}
		return d
	}

	for _, v := range []int32{orig, 3, 5} {
		if err := setNoop(v); err != nil {
			t.Logf("setParameter periodicNoopIntervalSecs=%d failed: %v", v, err)
			continue
		}
		time.Sleep(200 * time.Millisecond)
		t.Logf("periodicNoopIntervalSecs=%d -> open took %v", v, openLatency())
	}
}
