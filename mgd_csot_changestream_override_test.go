package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 1-1 Go port of the CSOT unified spec test:
// "timeoutMS can be configured for an operation - createChangeStream on client"
// from override-operation-timeoutMS.json (client uriOptions timeoutMS: 10).
func TestMGD_CSOT_CreateChangeStreamOperationOverride(t *testing.T) {
	ctx := context.Background()

	// This reproduces only on a SHARDED cluster (mongos). Require a sharded
	// MONGODB_URI; useMultipleMongoses:false equivalent — a single mongos so the
	// node-local failpoint applies to the operation.
	uri := os.Getenv("MONGODB_URI")
	require.NotEmpty(t, uri, "set MONGODB_URI to a sharded (mongos) deployment")

	// useMultipleMongoses:false — pin BOTH clients to a single mongos so the
	// node-local failpoint set by failPointClient applies to client's aggregate.
	base := options.Client().ApplyURI(uri)
	require.NotEmpty(t, base.Hosts)
	firstHost := base.Hosts[0]
	clientOpts := func() *options.ClientOptions {
		o := options.Client().ApplyURI(uri)
		o.Hosts = []string{firstHost}
		return o
	}

	// failPointClient
	failPointClient, err := mongo.Connect(clientOpts())
	require.NoError(t, err)
	defer failPointClient.Disconnect(ctx)

	// Verify the deployment is sharded (mongos reports msg: "isdbgrid").
	var hello struct {
		Msg string `bson:"msg"`
	}
	require.NoError(t, failPointClient.Database("admin").
		RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
	if hello.Msg != "isdbgrid" {
		t.Skipf("requires a sharded cluster (mongos); got msg=%q", hello.Msg)
	}

	// client (uriOptions: { timeoutMS: 10 })
	mon := monitor.New(t, true, "aggregate")
	client, err := mongo.Connect(clientOpts().
		SetTimeout(10 * time.Millisecond).
		SetMonitor(mon.CommandMonitor))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	// initialData: create test/coll (a write that advances cluster operationTime
	// right before the change stream).
	_ = failPointClient.Database("test").Collection("coll").Drop(ctx)
	require.NoError(t, failPointClient.Database("test").CreateCollection(ctx, "coll"))

	// operations:
	//   - failPoint: failCommand aggregate, blockConnection, blockTimeMS: 15, times: 1
	fpTeardown := failpoint.Enable(t, failPointClient, failpoint.NewBlock(15, 1, "aggregate"))
	defer fpTeardown(t)

	//   - createChangeStream { timeoutMS: 1000, pipeline: [] }
	opCtx, cancel := context.WithTimeout(ctx, 1000*time.Millisecond)
	defer cancel()

	cs, err := client.Watch(opCtx, mongo.Pipeline{})
	require.NoError(t, err)
	defer cs.Close(ctx)

	// expectEvents:
	//   - commandStartedEvent: aggregate, command { aggregate: 1, maxTimeMS: <int|long> }
	started := mon.CommandStartedEvents()
	require.Len(t, started, 1)
	assert.Equal(t, "aggregate", started[0].CommandName)
	_, ok := started[0].Command.Lookup("maxTimeMS").AsInt64OK()
	assert.True(t, ok, "expected aggregate command to carry maxTimeMS")
}
