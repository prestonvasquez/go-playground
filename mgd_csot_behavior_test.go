package goplayground

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/det"
	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/topology"
	topologyv1 "go.mongodb.org/mongo-driver/x/mongo/driver/topology"

	bsonv1 "go.mongodb.org/mongo-driver/bson"
	optionsv1 "go.mongodb.org/mongo-driver/mongo/options"
)

func TestMGD_CSOT_ContextWithTimeout(t *testing.T) {
	// Does SetTimeout have to be used for context deadlines to work?

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	// Block a find command for 20 seconds.
	fpTeardown := failpoint.Enable(t, client, failpoint.NewSingleBlock("find", 20000))
	defer fpTeardown(t)

	bgReadCalled := false
	topology.BGReadCallback = func(_ string, _, _ time.Time, _ []error, _ bool) {
		bgReadCalled = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := mongolocal.ArbColl(client).Find(ctx, bson.D{})
	require.ErrorIs(t, err, context.DeadlineExceeded, "expected context deadline exceeded error")

	// Sleep to give the background read a chance to be called.
	time.Sleep(topology.BGReadTimeout)

	require.False(t, bgReadCalled, "expected background read callback to be called")
}

func TestMGD_CSOT_V1_ContextDeadlineWithoutSetTimeout(t *testing.T) {
	// Can you use CSOT V1 without SetTimeout? Does it activate the background
	// reader?
	//
	// Ans. context will timeout without SetTimeout, but the background reader
	// will not be activated unless SetTimeout is used.

	client, teardown := mongolocal.NewV1(t, context.Background(),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	// Block a find command for 20 seconds.
	fpTeardown := failpoint.EnableV1(t, client, failpoint.NewSingleBlock("find", 20000))
	defer fpTeardown(t)

	bgReadCalled := false
	topologyv1.BGReadCallback = func(_ string, _, _ time.Time, _ []error, _ bool) {
		bgReadCalled = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := mongolocal.ArbCollV1(client).Find(ctx, bsonv1.D{})

	require.Error(t, err, "expected error from blocked find command")
	require.ErrorIs(t, err, context.DeadlineExceeded, "expected context deadline exceeded error")

	// Sleep to give the background read a chance to be called.
	time.Sleep(topologyv1.BGReadTimeout)

	require.False(t, bgReadCalled, "expected background read callback to be called")
}

func TestMGD_CSOT_V1_ContextDeadlineWithSetTimeout(t *testing.T) {
	// V1 context deadline with SetTimeout - does the background reader get
	// activated?

	opts := optionsv1.Client().SetTimeout(0)

	client, teardown := mongolocal.NewV1(t, context.Background(),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptionsV1(opts))

	defer teardown(t)

	// Block a find command for 20 seconds.
	fpTeardown := failpoint.EnableV1(t, client, failpoint.NewSingleBlock("find", 20000))
	defer fpTeardown(t)

	bgReadCalled := false
	topologyv1.BGReadCallback = func(_ string, _, _ time.Time, _ []error, _ bool) {
		bgReadCalled = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := mongolocal.ArbCollV1(client).Find(ctx, bsonv1.D{})

	require.Error(t, err, "expected error from blocked find command")
	require.ErrorIs(t, err, context.DeadlineExceeded, "expected context deadline exceeded error")

	// Sleep to give the background read a chance to be called.
	time.Sleep(topologyv1.BGReadTimeout)

	require.True(t, bgReadCalled, "expected background read callback to be called")
}

func TestMGD_SERVER_96344(t *testing.T) {
	client, teardwon := det.New(t, context.Background(), det.WithTopology("sharded_cluster"))
	defer teardwon(t)

	require.NoError(t, client.Ping(context.Background(), nil), "failed to ping mongo server")
}

func TestMGD_CSOT_WithTransaction_InheritTimeoutMS_ClientLevel(t *testing.T) {
	opts := mongooptions.Client().
		SetTimeout(500 * time.Millisecond).
		SetMinPoolSize(1)

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithMongoClientOptions(opts),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	fpTeardown := failpoint.Enable(t, client, failpoint.NewBlock(600, 2, "insert", "abortTransaction"))
	defer fpTeardown(t)

	coll := mongolocal.ArbColl(client)

	sess, err := client.StartSession()
	require.NoError(t, err, "failed to start session")
	defer sess.EndSession(context.Background())

	ctx := context.WithValue(context.Background(), "test1", true)

	_, err = sess.WithTransaction(ctx, func(sctx context.Context) (any, error) {
		_, err := coll.InsertOne(sctx, bson.D{{"_id", 1}})
		return nil, err
	})

	// Expect a timeout error from WithTransaction.
	require.Error(t, err, "expected error from WithTransaction")
	require.True(t, mongo.IsTimeout(err), "expected timeout error, got: %v", err)
}

func TestMGD_CSOT_WithTransaction_InheritTimeoutMS_OperationLevel(t *testing.T) {
	opts := mongooptions.Client().
		SetMinPoolSize(1)

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithMongoClientOptions(opts),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	fpTeardown := failpoint.Enable(t, client, failpoint.NewBlock(600, 2, "insert", "abortTransaction"))
	defer fpTeardown(t)

	coll := mongolocal.ArbColl(client)

	sess, err := client.StartSession()
	require.NoError(t, err, "failed to start session")
	defer sess.EndSession(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = sess.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		_, err := coll.InsertOne(ctx, bson.D{{"_id", 1}})
		return nil, err
	})

	// Expect a timeout error from WithTransaction.
	require.Error(t, err, "expected error from WithTransaction")
	require.True(t, mongo.IsTimeout(err), "expected timeout error, got: %v", err)
}

// TestMGD_CSOT_RetryableWrite_ReturnsMostRecentError tests that when multiple
// retryable errors occur (without NoWritesPerformed), the most recent error is returned.
//
// Per the spec: "If the driver has encountered only errors that indicate write
// attempts were made, the most recently encountered error must be returned."
func TestMGD_CSOT_RetryableWrite_ReturnsMostRecentError(t *testing.T) {
	// We need two separate clients:
	// 1. helperClient: configures failpoints (no CSOT, independent topology)
	// 2. client: runs the operation under test (with CSOT)
	//
	// We use a helper client because after certain errors, the test client marks
	// the server as Unknown in its topology. If we tried to configure the
	// second failpoint using the same client, server selection would fail.
	// The helper client maintains its own topology state.

	helperClient, teardown, env := mongolocal.NewWithEnv(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands())
	defer teardown(t)

	setupCh := make(chan func(), 1)

	monitor := &event.CommandMonitor{
		Failed: func(_ context.Context, evt *event.CommandFailedEvent) {
			if evt.CommandName == "insert" {
				select {
				case setup := <-setupCh:
					setup()
				default:
				}
			}
		},
	}

	opts := mongooptions.Client().SetTimeout(5 * time.Second).SetMonitor(monitor)

	client, err := mongo.Connect(opts.ApplyURI(env.ConnectionString()))
	require.NoError(t, err)
	defer client.Disconnect(context.Background())

	// First failpoint: error 134 (ReadConcernMajorityNotAvailableYet), fires once.
	// Using 134 instead of 91 (ShutdownInProgress) because 91 marks the server Unknown,
	// which causes server selection to fail on retry.
	failpoint.Enable(t, helperClient, failpoint.NewSingleErrWithLabels("insert", 134, []string{"RetryableWriteError"}))

	// Queue second failpoint setup to run on first failure.
	// Use AlwaysOn so it keeps failing until timeout.
	setupCh <- func() {
		failpoint.Enable(t, helperClient, failpoint.NewAlwaysOnErrWithLabels("insert", 10107, []string{"RetryableWriteError"}))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Database("test").Collection("test").InsertOne(ctx, struct{ X int }{X: 1})

	require.Error(t, err)

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr))

	// Should return 10107 (most recent), not 134 (first)
	require.True(t, srvErr.HasErrorCode(10107), "expected error 10107, got %v", srvErr.ErrorCodes())
}
