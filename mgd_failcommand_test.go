package goplayground

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestFailCommandFailPoint(t *testing.T) {
	ctx := context.Background()

	// Start MongoDB container with test commands enabled
	client, teardown := mongolocal.New(t, ctx, mongolocal.WithEnableTestCommands())
	defer teardown(t)

	// Verify basic connectivity works
	require.NoError(t, client.Ping(ctx, nil))

	// Configure the failCommand failpoint to make "find" commands fail with error code 2
	var result bson.M
	err := client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "configureFailPoint", Value: "failCommand"},
		{Key: "mode", Value: "alwaysOn"},
		{Key: "data", Value: bson.D{
			{Key: "errorCode", Value: 2},
			{Key: "failCommands", Value: bson.A{"find"}},
		}},
	}).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, float64(1), result["ok"])

	// Try to execute a find command - it should fail with error code 2
	collection := client.Database("testdb").Collection("testcoll")
	cursor, err := collection.Find(ctx, bson.D{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "BadValue")
	require.Nil(t, cursor)

	// Disable the failpoint
	err = client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "configureFailPoint", Value: "failCommand"},
		{Key: "mode", Value: "off"},
	}).Decode(&result)
	require.NoError(t, err)

	// Verify find command works again after disabling failpoint
	cursor, err = collection.Find(ctx, bson.D{})
	require.NoError(t, err)
	require.NotNil(t, cursor)
	require.NoError(t, cursor.Close(ctx))
}

func TestFailCommandFailPointWithTimes(t *testing.T) {
	ctx := context.Background()

	// Start MongoDB container with test commands enabled
	client, teardown := mongolocal.New(t, ctx, mongolocal.WithEnableTestCommands())
	defer teardown(t)

	// Configure the failCommand failpoint to fail exactly 2 times
	var result bson.M
	err := client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "configureFailPoint", Value: "failCommand"},
		{Key: "mode", Value: bson.D{{Key: "times", Value: 2}}},
		{Key: "data", Value: bson.D{
			{Key: "errorCode", Value: 2},
			{Key: "failCommands", Value: bson.A{"ping"}},
		}},
	}).Decode(&result)
	require.NoError(t, err)
	require.Equal(t, float64(1), result["ok"])

	// First ping should fail
	err = client.Ping(ctx, nil)
	require.Error(t, err)

	// Second ping should fail
	err = client.Ping(ctx, nil)
	require.Error(t, err)

	// Third ping should succeed (failpoint auto-disabled after 2 times)
	err = client.Ping(ctx, nil)
	require.NoError(t, err)
}

func TestFailCommandNotEnabledByDefault(t *testing.T) {
	ctx := context.Background()

	// Start MongoDB container WITHOUT test commands enabled
	client, teardown := mongolocal.New(t, ctx)
	defer teardown(t)

	// Verify basic connectivity works
	require.NoError(t, client.Ping(ctx, nil))

	// Try to configure failCommand - it should fail since test commands are disabled
	var result bson.M
	err := client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "configureFailPoint", Value: "failCommand"},
		{Key: "mode", Value: "alwaysOn"},
		{Key: "data", Value: bson.D{
			{Key: "errorCode", Value: 2},
			{Key: "failCommands", Value: bson.A{"find"}},
		}},
	}).Decode(&result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "configureFailPoint")
}
