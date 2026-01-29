package goplayground

import (
	"context"
	"errors"
	"testing"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_Failpoint_Enable(t *testing.T) {
	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	// Set failpoint and run operation using same client.
	failpointTeardown := failpoint.Enable(t, client, failpoint.NewAlwaysOnErr("find", 91))
	defer failpointTeardown(t)

	err := mongolocal.ArbColl(client).FindOne(context.Background(), bson.D{}).Err()

	require.Error(t, err, "expected error from failpoint")
	t.Logf("Got error: %v (type: %T)", err, err)

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr), "expected ServerError, got %T: %v", err, err)
	require.True(t, srvErr.HasErrorCode(91), "expected error 91, got %v", srvErr.ErrorCodes())
}

func TestMGD_Failpoint_SetFromDifferentClients(t *testing.T) {
	client1, teardown, env := mongolocal.NewWithEnv(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	client2Opts := options.Client().ApplyURI(env.ConnectionString())

	client2, err := mongo.Connect(client2Opts)
	require.NoError(t, err, "error connecting client2")

	// Set failpoint and run operation using same client.
	failpointTeardown := failpoint.Enable(t, client2, failpoint.NewAlwaysOnErr("find", 91))
	defer failpointTeardown(t)

	err = mongolocal.ArbColl(client1).FindOne(context.Background(), bson.D{}).Err()

	require.Error(t, err, "expected error from failpoint")
	t.Logf("Got error: %v (type: %T)", err, err)

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr), "expected ServerError, got %T: %v", err, err)
	require.True(t, srvErr.HasErrorCode(91), "expected error 91, got %v", srvErr.ErrorCodes())
}

//func TestFailpoint_SetFromDifferentClient(t *testing.T) {
//	ctx := context.Background()
//
//	// Start MongoDB container with test commands enabled.
//	container, err := mongodb.Run(ctx, "mongo:latest",
//		testcontainers.WithCmdArgs("--setParameter", "enableTestCommands=1"))
//	require.NoError(t, err)
//	defer testcontainers.TerminateContainer(container)
//
//	uri, err := container.ConnectionString(ctx)
//	require.NoError(t, err)
//
//	// Client A: used to configure failpoints.
//	clientA, err := mongo.Connect(mongooptions.Client().ApplyURI(uri))
//	require.NoError(t, err)
//	defer clientA.Disconnect(ctx)
//
//	// Verify client A can connect.
//	require.NoError(t, clientA.Ping(ctx, nil), "clientA ping failed")
//	t.Log("clientA connected")
//
//	// Client B: used to run operations.
//	clientB, err := mongo.Connect(mongooptions.Client().ApplyURI(uri))
//	require.NoError(t, err)
//	defer clientB.Disconnect(ctx)
//
//	// Verify client B can connect.
//	require.NoError(t, clientB.Ping(ctx, nil), "clientB ping failed")
//	t.Log("clientB connected")
//
//	// Set failpoint using client A.
//	t.Log("Setting failpoint via clientA")
//	failpoint.Enable(t, clientA, failpoint.NewSingleErr("find", 91))
//	t.Log("Failpoint set")
//
//	// Run operation using client B - should hit the failpoint.
//	t.Log("Running find via clientB")
//	err = clientB.Database("test").Collection("test").FindOne(ctx, bson.D{}).Err()
//	t.Logf("Find completed with error: %v", err)
//
//	require.Error(t, err, "expected error from failpoint")
//	t.Logf("Got error: %v (type: %T)", err, err)
//
//	var srvErr mongo.ServerError
//	require.True(t, errors.As(err, &srvErr), "expected ServerError, got %T: %v", err, err)
//	require.True(t, srvErr.HasErrorCode(91), "expected error 91, got %v", srvErr.ErrorCodes())
//}
