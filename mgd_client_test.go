package goplayground

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMutatingEventMonitoring(t *testing.T) {
	// Can you mutate a monitor after it's been set on the client?
	// This uses an indirection pattern to allow mutation.

	var startedFunc func(context.Context, *event.CommandStartedEvent)

	var monitorCalled bool
	monitor := &event.CommandMonitor{
		Started: func(ctx context.Context, evt *event.CommandStartedEvent) {
			if startedFunc != nil {
				startedFunc(ctx, evt)
			}
		},
	}

	opts := options.Client().SetMonitor(monitor)

	client, teardown := mongolocal.StartT(t, context.Background(), mongolocal.WithMongoClientOptions(opts))
	defer teardown(t)

	// Mutate the monitor behavior by setting the function it delegates to.
	var mutated bool
	startedFunc = func(ctx context.Context, evt *event.CommandStartedEvent) {
		mutated = true
	}

	// Run an operation to see if the mutated monitor is used.
	res, err := mongolocal.ArbColl(client).InsertOne(context.Background(), map[string]interface{}{"test": "value"})
	t.Logf("InsertOne result: %v, error: %v", res, err)
	t.Logf("Monitor called: %v, Mutated: %v", monitorCalled, mutated)

	require.True(t, mutated, "expected mutated monitor to be used")
}

// The ConnString type has functions to validate auth/ssl/etc, but this
// validation would not be done if ClientOptions were used instead of a URI.
func TestMGD_MergeConnStringValidation_GODRIVER1714(t *testing.T) {
	t.Run("with URI", func(t *testing.T) {
		uri := "mongodb://localhost:27017/?authMechanism=SCRAM-SHA-1"
		opts := options.Client().ApplyURI(uri)

		_, err := mongo.Connect(opts)
		require.ErrorContains(t, err, "username required")
	})

	t.Run("with ClientOptions", func(t *testing.T) {
		opts := options.Client().SetAuth(options.Credential{AuthMechanism: "SCRAM-SHA-1"})

		_, err := mongo.Connect(opts)
		require.ErrorContains(t, err, "username required")
	})
}
