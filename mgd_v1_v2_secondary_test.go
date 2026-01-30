package goplayground

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"

	bsonv1 "go.mongodb.org/mongo-driver/bson"
	mongov1 "go.mongodb.org/mongo-driver/mongo"
	optionsv1 "go.mongodb.org/mongo-driver/mongo/options"
)

// replicaSetURI is the connection string for the det replica set.
//
// This assumes you are connecting using mongo orchestration docker setup from
// drivers-evergreen-tools.
const replicaSetURI = "mongodb://localhost:27017,localhost:27018,localhost:27019"

func findSecondaryHost(t *testing.T, ctx context.Context, client any) string {
	t.Helper()

	var members []any

	switch c := client.(type) {
	case *mongov1.Client:
		var rsStatus bsonv1.M
		err := c.Database("admin").RunCommand(ctx, bsonv1.D{{"replSetGetStatus", 1}}).Decode(&rsStatus)
		require.NoError(t, err, "failed to get replica set status")
		arr, ok := rsStatus["members"].(bsonv1.A)
		require.True(t, ok, "failed to get members from replica set status")
		members = arr
	case *mongo.Client:
		var rsStatus bson.M
		err := c.Database("admin").RunCommand(ctx, bson.D{{"replSetGetStatus", 1}}).Decode(&rsStatus)
		require.NoError(t, err, "failed to get replica set status")
		arr, ok := rsStatus["members"].(bson.A)
		require.True(t, ok, "failed to get members from replica set status, got %T", rsStatus["members"])
		members = arr
	default:
		t.Fatalf("unsupported client type: %T", client)
	}

	for _, member := range members {
		var stateStr, host string
		switch m := member.(type) {
		case bsonv1.M:
			stateStr, _ = m["stateStr"].(string)
			host, _ = m["name"].(string)
		case bson.M:
			stateStr, _ = m["stateStr"].(string)
			host, _ = m["name"].(string)
		case bson.D:
			for _, elem := range m {
				if elem.Key == "stateStr" {
					stateStr, _ = elem.Value.(string)
				} else if elem.Key == "name" {
					host, _ = elem.Value.(string)
				}
			}
		default:
			t.Fatalf("unexpected member type: %T", member)
		}
		if stateStr == "SECONDARY" {
			return host
		}
	}

	t.Fatal("no secondary found in replica set")
	return ""
}

func TestMGD_V1_DirectSecondary_NotWritablePrimary(t *testing.T) {
	ctx := context.Background()

	client, err := mongov1.Connect(ctx, optionsv1.Client().ApplyURI(replicaSetURI))
	require.NoError(t, err)

	defer func() { require.NoError(t, client.Disconnect(ctx)) }()

	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("Skipping test because cannot connect to replica set: %v", err)
	}

	// Find a secondary.
	secondaryHost := findSecondaryHost(t, ctx, client)

	// Create a direct connection to the secondary only.
	directClientOpts := optionsv1.Client().ApplyURI(fmt.Sprintf("mongodb://%s/?directConnection=true", secondaryHost))

	directClient, err := mongov1.Connect(ctx, directClientOpts)
	require.NoError(t, err)

	defer func() { require.NoError(t, directClient.Disconnect(ctx)) }()

	// Attempt a write operation.
	coll := mongolocal.ArbCollV1(directClient)
	_, err = coll.InsertOne(ctx, struct{ X int }{X: 1})

	// Assert we get a NotWritablePrimary error (error code 10107) .
	var srcErr mongov1.ServerError
	require.True(t, errors.As(err, &srcErr))
	require.True(t, srcErr.HasErrorCode(10107))
}

func TestMGD_V2_DirectSecondary_WriteBehavior(t *testing.T) {
	ctx := context.Background()

	client, err := mongo.Connect(options.Client().ApplyURI(replicaSetURI))
	require.NoError(t, err)

	defer func() { require.NoError(t, client.Disconnect(ctx)) }()

	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("Skipping test because cannot connect to replica set: %v", err)
	}

	// Find a secondary.
	secondaryHost := findSecondaryHost(t, ctx, client)

	// Create a direct connection to the secondary only.
	directClientOpts := mongooptions.Client().ApplyURI(fmt.Sprintf("mongodb://%s/?directConnection=true", secondaryHost))

	directClient, err := mongo.Connect(directClientOpts)
	require.NoError(t, err)

	defer func() { require.NoError(t, directClient.Disconnect(ctx)) }()

	// Attempt a write operation.
	coll := mongolocal.ArbColl(directClient)
	_, err = coll.InsertOne(ctx, struct{ X int }{X: 1})

	// Assert we get a NotWritablePrimary error (error code 10107).
	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr))
	require.True(t, srvErr.HasErrorCode(10107))
}
