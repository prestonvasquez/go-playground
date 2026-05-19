// E2E repro for the AppendBatchArray accounting bug, reachable only via
// the CSFLE auto-encryption write path
// (x/mongo/driver/operation.go: addEncryptCommandFields → AppendBatchArray).
//
// The bug: AppendBatchArray's size accumulator misses (a) the bytes
// already in dst when called, (b) the per-element overhead (BSON type
// byte + ASCII array index + key terminator) for each appended doc,
// and (c) the final 0x00 array terminator. When the sum of plain
// document bytes lands just under cryptMaxBsonObjectSize (2 MiB), the
// buggy splitter packs all of them; the resulting command body exceeds
// the budget, libmongocrypt then encrypts an oversize input (or the
// resulting wire message exceeds maxMessageSizeBytes), and the write
// surfaces as "broken pipe" on the driver side.
//
// Requirements:
//   - libmongocrypt installed on the host (`brew install libmongocrypt` on macOS)
//   - either MongoDB's crypt_shared library in a standard location OR
//     mongocryptd in PATH — libmongocrypt auto-discovers both
//   - run with `-tags cse`
package goplayground

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_CSFLEAppendBatchArrayBoundary(t *testing.T) {
	ctx := context.Background()

	// Real auto-encryption: bypass=false routes writes through
	// addEncryptCommandFields → AppendBatchArray, which is the buggy
	// path we want to exercise.
	client, ce, teardown := mongolocal.NewCSFLE(t, ctx,
		mongolocal.WithBypassAutoEncryption(false),
	)
	defer teardown(t)

	// Create a data key so libmongocrypt has something to work with.
	_, err := ce.CreateDataKey(ctx, "local",
		mongooptions.DataKey().SetKeyAltNames([]string{"appendbatcharray-test-key"}))
	require.NoError(t, err, "CreateDataKey")

	// Build documents whose sum lands inside the trigger window:
	//   sum(len(doc)) ≤ cryptMaxBsonObjectSize  (buggy splitter packs all)
	//   sum(len(doc)) + len(dst) + per-elem-overhead + 1 > cryptMaxBsonObjectSize
	// 100 equal-sized docs holding random bytes so wire compression
	// can't shrink the payload.
	//
	// cryptMaxBsonObjectSize = 2,097,152 bytes (2 MiB) — the budget
	// AppendBatchArray is called with on the CSFLE path.
	const (
		cryptMaxBsonObjectSize = 2 * 1024 * 1024
		numDocs                = 100
		// Per-doc overhead: 30 bytes accounts for BSON envelope (13)
		// plus driver-injected _id ObjectID (17).
		perDocOverhead = 30
		// Margin under cryptMaxBsonObjectSize so the buggy size
		// accumulator (which only tracks len(doc)) sees the sum as
		// in-bounds. The undercount fills this gap and overflows.
		totalBudgetMargin = 100
	)

	docPayloadSize := (cryptMaxBsonObjectSize-totalBudgetMargin)/numDocs - perDocOverhead

	docs := make([]any, numDocs)
	payload := make([]byte, docPayloadSize)
	for i := range docs {
		_, err := rand.Read(payload)
		require.NoError(t, err)
		docs[i] = bson.D{{Key: "x", Value: bson.Binary{Subtype: 0x00, Data: append([]byte(nil), payload...)}}}
	}

	coll := mongolocal.ArbColl(client)

	_, err = coll.InsertMany(ctx, docs)
	if err != nil && strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("AppendBatchArray bug triggered: server tore down the connection "+
			"because the buggy size accumulator packed an oversize command body. "+
			"Error: %v", err)
	}
	require.NoError(t, err, "InsertMany under auto-encryption should split correctly")
}
