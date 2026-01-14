package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
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
