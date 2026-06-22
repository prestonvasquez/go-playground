package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 1-1 Go port of the CSOT unified spec test:
// "timeoutMS can be configured for an operation - createChangeStream on client"
// from override-operation-timeoutMS.json (client uriOptions timeoutMS: 10).
func TestMGD_CSOT_CreateChangeStreamOperationOverride(t *testing.T) {
	ctx := context.Background()

	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017,localhost:27018"
	}

	// client (uriOptions: { timeoutMS: 10 })
	mon := monitor.New(t, true, "aggregate")
	client, err := mongo.Connect(options.Client().
		ApplyURI(uri).
		SetTimeout(10 * time.Millisecond).
		SetMonitor(mon.CommandMonitor))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

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
