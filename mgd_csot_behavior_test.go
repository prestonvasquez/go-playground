package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMGD_CSOT_ContextWithTimeout(t *testing.T) {
	// Does SetTimeout have to be used for context deadlines to work?

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithEnableTestCommands())

	defer teardown(t)

	// Block a find command for 20 seconds.
	fpTeardown := failpoint.Enable(t, client, failpoint.NewSingleBlock("find", 20000))
	defer fpTeardown(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := mongolocal.ArbColl(client).Find(ctx, bson.D{})
	require.ErrorIs(t, err, context.DeadlineExceeded, "expected context deadline exceeded error")
}
