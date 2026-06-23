package goplayground

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Sweep the operation budget around 1000ms (spec-test conditions: initialData
// write + 15ms failpoint) and report pass rate. If the threshold sits at the
// ~1s open latency, that confirms it's open-latency vs budget — and answers
// whether 1020ms would "work".
func TestMGD_CSOT_ChangeStreamBudgetSweep(t *testing.T) {
	ctx := context.Background()
	uri := os.Getenv("MONGODB_URI")
	require.NotEmpty(t, uri, "set MONGODB_URI to a sharded (mongos) deployment")

	base := options.Client().ApplyURI(uri)
	require.NotEmpty(t, base.Hosts)
	firstHost := base.Hosts[0]
	mk := func() *options.ClientOptions {
		o := options.Client().ApplyURI(uri)
		o.Hosts = []string{firstHost}
		return o
	}

	fpClient, err := mongo.Connect(mk())
	require.NoError(t, err)
	defer fpClient.Disconnect(ctx)

	var hello struct {
		Msg string `bson:"msg"`
	}
	require.NoError(t, fpClient.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
	if hello.Msg != "isdbgrid" {
		t.Skipf("requires sharded (mongos); got msg=%q", hello.Msg)
	}

	client, err := mongo.Connect(mk())
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	const reps = 6
	for _, budgetMS := range []int{1000, 1020, 1050, 1100, 1200, 1500, 2000} {
		pass := 0
		for i := 0; i < reps; i++ {
			_ = fpClient.Database("test").Collection("coll").Drop(ctx)
			require.NoError(t, fpClient.Database("test").CreateCollection(ctx, "coll"))
			fpTeardown := failpoint.Enable(t, fpClient, failpoint.NewBlock(15, 1, "aggregate"))

			c, cancel := context.WithTimeout(ctx, time.Duration(budgetMS)*time.Millisecond)
			cs, werr := client.Watch(c, mongo.Pipeline{})
			cancel()
			if werr == nil {
				pass++
				_ = cs.Close(ctx)
			}
			fpTeardown(t)
		}
		t.Logf("budget=%4dms  pass %d/%d", budgetMS, pass, reps)
	}
}
