package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/prestonvasquez/go-playground/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_ChangeStream_Resume(t *testing.T) {
	monitor := monitor.New(t, true, "getMore")
	opts := options.Client().SetMonitor(monitor.CommandMonitor)

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(opts),
		mongolocal.WithReplicaSet("rs"),
	)

	defer teardown(t)

	coll := mongolocal.ArbColl(client)
	csOpts := options.ChangeStream().SetBatchSize(1)

	cs, err := coll.Watch(context.Background(), mongo.Pipeline{}, csOpts)
	require.NoError(t, err)

	defer cs.Close(context.Background())

	// Insert 5 documents
	for range 5 {
		_, err = coll.InsertOne(context.Background(), bson.D{})
		require.NoError(t, err)
	}

	ok := cs.Next(context.Background())
	assert.True(t, ok)

	// Create a FP that will block the next "getMore" call causing the CS next
	// to fail with a CSOT.
	fpTeardown := failpoint.Enable(t, client, failpoint.NewSingleBlock("getMore", 500))
	defer fpTeardown(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	ok = cs.Next(ctx)
	assert.False(t, ok)

	// A subsequent call to next that does not time out should resume since the
	// error was CSOT.
	ok = cs.Next(context.Background())

	// According to GODRIVER-3380 and the CSOT specifications, this should work:
	//
	// > If a next call fails with a timeout error, drivers MUST NOT invalidate
	// > the change stream. The subsequent next call MUST perform a resume attempt
	// > to establish a new change stream on the server.
	//
	// assert.True(t, ok)

	assert.False(t, ok) // but it doesn't due to https://jira.mongodb.org/browse/GODRIVER-3380
}
