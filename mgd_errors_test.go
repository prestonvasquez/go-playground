package goplayground

import (
	"context"
	"errors"
	"testing"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongoevent"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGDErrorsAsFromCommandFailedEvent(t *testing.T) {
	// If we get an error in the CommandFailedEvent, can we use errors.As to
	// get the error codes via mongo.ServerError?

	monitor := mongoevent.NewCommandMonitor()
	opts := options.Client().SetMonitor(mongoevent.NewCommandEventMonitor(monitor))

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(opts))

	defer teardown(t)

	// Create a failpoint that makes "find" commands fail with error code 42.
	fpTeardown := failpoint.Enable(t, client, failpoint.NewSingleErr("find", 42))
	defer fpTeardown(t)

	// Run a find command that should fail.
	_, err := mongolocal.ArbColl(client).Find(context.Background(), bson.D{})
	require.Error(t, err)

	// Check to see if we can get the error code using errors.As.
	var serverErr mongo.ServerError
	if errors.As(err, &serverErr) {
		t.Logf("Got server error with code: %d", serverErr.ErrorCodes())
	}

	// Check to see if the error from the failed command works.
	require.Len(t, monitor.FailedErrors, 1)

	fcErr := monitor.FailedErrors[0]
	if errors.As(fcErr, &serverErr) {
		t.Logf("Got server error from CommandFailedEvent with code: %d", serverErr.ErrorCodes())
	} else {
		t.Log("Could not extract mongo.ServerError from CommandFailedEvent error")
	}
}
