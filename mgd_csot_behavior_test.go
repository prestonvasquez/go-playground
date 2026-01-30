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
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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

// Test 7a: All write attempts made returns most recent error
//
// Tests that when multiple retryable errors occur (without NoWritesPerformed),
// the most recent error is returned.
//
// Per the spec: "If the driver has encountered only errors that indicate write
// attempts were made, the most recently encountered error must be returned."
func TestMGD_CSOT_RetryableWrite_7a_AllWriteAttemptsMade(t *testing.T) {
	setupCh := make(chan func(), 1)

	// Step 1: Create a client with retryWrites=true.
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

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(options.Client().SetMonitor(monitor).SetRetryWrites(true)))
	defer teardown(t)

	// Step 2: Configure a fail point with error code 134
	// (ReadConcernMajorityNotAvailableYet).
	failpoint.Enable(t, client,
		failpoint.NewSingleErrWithLabels("insert", 134, []string{"RetryableWriteError"}))

	// Step 3: Via the CommandFailedEvent, configure a fail point with error code
	// 10107 (NotWritablePrimary). Drivers SHOULD only configure the `10107` fail
	// point command if the the failed event is for the `134` error configured in
	// step 2.
	setupCh <- func() {
		failpoint.Enable(t, client,
			failpoint.NewAlwaysOnErrWithLabels("insert", 10107, []string{"RetryableWriteError"}))
	}

	// Step 4: Set a 5s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Step 5: Attempt an insertOne operation. Assert that the error code is 10107
	// (most recent), not 134 configured in step 2.
	_, err := client.Database("test").Collection("test").InsertOne(ctx, struct{ X int }{X: 1})

	require.Error(t, err)

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr))
	require.True(t, srvErr.HasErrorCode(10107), "expected error 10107, got %v", srvErr.ErrorCodes())

	// Step 6: Disable the fail point (handled by test cleanup).
}

// Test 7b: No write attempts made returns first error
//
// Tests that when all retryable errors have NoWritesPerformed, the first error
// is returned.
//
// Per the spec: "If all errors indicate no attempt was made (e.g., all errors
// contain the NoWritesPerformed error label or are client-side errors before
// a command is sent), the first error encountered must be returned."
func TestMGD_CSOT_RetryableWrite_7b_NoWriteAttemptsMade(t *testing.T) {
	setupCh := make(chan func(), 1)

	// Step 1: Create a client with retryWrites=true.
	monitor := &event.CommandMonitor{
		Failed: func(_ context.Context, evt *event.CommandFailedEvent) {
			errorCodes := mongo.ErrorCodes(evt.Failure)
			if evt.CommandName == "insert" && len(errorCodes) > 0 && errorCodes[0] == 134 {
				select {
				case setup := <-setupCh:
					setup()
				default:
				}
			}
		},
	}

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(options.Client().SetMonitor(monitor).SetRetryWrites(true)))
	defer teardown(t)

	// Step 2: Configure a fail point with error code 134
	// (ReadConcernMajorityNotAvailableYet) and NoWritesPerformed.
	failpoint.Enable(t, client,
		failpoint.NewSingleErrWithLabels("insert", 134, []string{"RetryableWriteError", "NoWritesPerformed"}))

	// Step 3: Via the CommandFailedEvent, configure a fail point with error code
	// 10107 (NoWritablePrimary) and NoWritesPerformed. Drivers SHOULD only
	// configure the `10107` fail point command if the the failed event is for the
	// `134` error configured in step 2.
	setupCh <- func() {
		failpoint.Enable(t, client,
			failpoint.NewAlwaysOnErrWithLabels("insert", 10107, []string{"RetryableWriteError", "NoWritesPerformed"}))
	}

	// Step 4: Set a 1s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Step 5: Attempt an insertOne operation. Assert that the error code is 134
	// configured in step 2 (first error).
	_, err := client.Database("test").Collection("test").InsertOne(ctx, struct{ X int }{X: 1})

	require.Error(t, err)

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr))
	require.True(t, srvErr.HasErrorCode(134), "expected error 134, got %v", srvErr.ErrorCodes())

	// Step 6: Disable the fail point (handled by test cleanup).
}

// Test 7c: Mixed write attempts returns most recent error indicating write
// attempt
//
// Tests that when there's a mix of errors with and without NoWritesPerformed,
// the most recent error WITHOUT NoWritesPerformed is returned.
//
// Per the spec: "If the driver has encountered some errors which indicate a
// write attempt was made and some which indicate no write attempt was made,
// the most recently encountered error which indicates a write attempt occurred
// must be returned."
func TestMGD_CSOT_RetryableWrite_7c_MixedWriteAttempts(t *testing.T) {
	setupCh := make(chan func(), 1)

	// Step 1: Create a client with retryWrites=true.
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

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(options.Client().SetMonitor(monitor).SetRetryWrites(true)))
	defer teardown(t)

	// Step 2: Configure a fail point with error code 134
	// (ReadConcernMajorityNotAvailableYet) WITHOUT NoWritesPerformed (write attempt
	// was made).
	failpoint.Enable(t, client,
		failpoint.NewSingleErrWithLabels("insert", 134, []string{"RetryableWriteError"}))

	// Step 3: Via the CommandFailedEvent, configure a fail point with error code
	// 10107 (NotWritablePrimary) WITH NoWritesPerformed. Drivers SHOULD only
	// configure the `10107` fail point command if the the failed event is for the
	// `134` error configured in step 2.
	setupCh <- func() {
		failpoint.Enable(t, client,
			failpoint.NewAlwaysOnErrWithLabels("insert", 10107, []string{"RetryableWriteError", "NoWritesPerformed"}))
	}

	// Step 4: Set a 5s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Step 5: Attempt an insertOne operation. Assert that the error code is 134
	// configured in step 2 (most recent error indicating write attempt was made).
	_, err := client.Database("test").Collection("test").InsertOne(ctx, struct{ X int }{X: 1})

	require.Error(t, err)

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr))
	require.True(t, srvErr.HasErrorCode(134), "expected error 134, got %v", srvErr.ErrorCodes())

	// Step 6: Disable the fail point (handled by test cleanup).
}

func TestMGD_CSOT_Aggregate_MaxAwaitTimeMS_GreaterThan_TimeoutMS(t *testing.T) {
	client, teardown := mongolocal.New(t, context.Background())
	defer teardown(t)

	db := client.Database("testdb")

	// Create capped collection like the spec test
	err := db.CreateCollection(context.Background(), "capped_coll", options.CreateCollection().SetCapped(true).SetSizeInBytes(500))
	require.NoError(t, err)

	coll := db.Collection("capped_coll")

	// Insert 2 documents like the spec test
	_, err = coll.InsertMany(context.Background(), []any{
		bson.D{{"_id", 0}},
		bson.D{{"_id", 1}},
	})
	require.NoError(t, err)

	// Set timeoutMS=100ms and maxAwaitTimeMS=200ms (maxAwaitTimeMS >= timeoutMS)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	opts := options.Aggregate().SetMaxAwaitTime(200 * time.Millisecond).SetBatchSize(1)

	cursor, err := coll.Aggregate(ctx, bson.A{}, opts)
	require.NoError(t, err)

	defer cursor.Close(ctx)

	err = cursor.All(ctx, &([]bson.D{}))
	require.NoError(t, err)
}
