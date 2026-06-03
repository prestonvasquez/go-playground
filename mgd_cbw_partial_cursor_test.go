package goplayground

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestMGD_ClientBulkWrite_PartialCursorOnGetMoreError demonstrates what the Go
// driver includes in ClientBulkWriteException.PartialResult when a top-level
// error occurs mid-cursor during clientBulkWrite.
//
// Setup:
//   - Two upsert operations whose result documents are each ~MaxBsonObjectSize/2,
//     forcing the server to return results across two cursor batches (initial +
//     one getMore).
//   - A failpoint causes the getMore to fail with error code 8.
//
// The question: does PartialResult contain the result from the first batch only,
// or does it also reflect results that would have come from the failed getMore?
//
// Requires MongoDB 8.0+ replica set (clientBulkWrite is 8.0+; failpoints need
// test commands enabled).
func TestMGD_ClientBulkWrite_PartialCursorOnGetMoreError(t *testing.T) {
	client, teardown := mongolocal.StartT(t, context.Background(),
		mongolocal.WithImage("mongo:8.0"),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands(),
	)
	defer teardown(t)

	// Resolve MaxBsonObjectSize so we can construct documents large enough to
	// span two cursor batches.
	var hello struct {
		MaxBsonObjectSize int
	}

	helloCmd := bson.D{{Key: "hello", Value: 1}}
	err := client.Database("admin").RunCommand(context.Background(), helloCmd).Decode(&hello)
	require.NoError(t, err, "hello command failed")

	coll := mongolocal.ArbColl(client)

	upsert := true

	// Each filter document is ~MaxBsonObjectSize/2, forcing each result
	// document to occupy its own cursor batch and requiring getMore calls.
	makeUpsert := func(id string) mongo.ClientBulkWrite {
		return mongo.ClientBulkWrite{
			Database:   coll.Database().Name(),
			Collection: coll.Name(),
			Model: &mongo.ClientUpdateOneModel{
				Filter: bson.D{{Key: "_id", Value: strings.Repeat(id, hello.MaxBsonObjectSize/2)}},
				Update: bson.D{{Key: "$set", Value: bson.D{{Key: "x", Value: 1}}}},
				Upsert: &upsert,
			},
		}
	}

	models := []mongo.ClientBulkWrite{
		makeUpsert("a"),
		makeUpsert("b"),
		makeUpsert("c"),
	}

	// Skip the first getMore (let it succeed), then fail the second. This
	// ensures at least one successful getMore round trip occurs before the
	// top-level error, which is the scenario the Java driver bug describes.
	fpTeardown := failpoint.Enable(t, client, failpoint.FailPoint{
		ConfigureFailPoint: "failCommand",
		Mode:               failpoint.Mode{Skip: 1, Times: 1},
		Data:               failpoint.Data{FailCommands: []string{"getMore"}, ErrorCode: 8},
	})
	defer fpTeardown(t)

	_, err = client.BulkWrite(
		context.Background(),
		models,
		options.ClientBulkWrite().SetVerboseResults(true),
	)
	require.Error(t, err, "expected BulkWrite to fail")

	var bwe mongo.ClientBulkWriteException
	require.True(t, errors.As(err, &bwe), "expected ClientBulkWriteException, got %T: %v", err, err)

	require.NotNil(t, bwe.WriteError, "expected a top-level WriteError")
	t.Logf("top-level error code: %d", bwe.WriteError.Code)

	require.NotNil(t, bwe.PartialResult, "expected a PartialResult")

	// Both operations succeeded (upserted) — reflected in the summary count from
	// the command response, which arrives before cursor exhaustion.
	t.Logf("UpsertedCount: %d (expected 2)", bwe.PartialResult.UpsertedCount)

	// Verbose per-operation results come from cursor documents. The first batch
	// holds one result; the second (failed getMore) holds the other.
	// Does PartialResult include the result from the first batch only, or both?
	// 3 ops succeeded. Initial batch has 1 result, first getMore has 1 result,
	// second getMore (which failed) would have had the third. The question is
	// whether results from the successful first getMore are preserved (2) or
	// discarded along with the failure (1).
	t.Logf("UpdateResults entries: %d (expected 2 if partial results are preserved, 1 if discarded)",
		len(bwe.PartialResult.UpdateResults))

	for idx, r := range bwe.PartialResult.UpdateResults {
		t.Logf("  [%d] upserted=%v", idx, r.UpsertedID)
	}
}
