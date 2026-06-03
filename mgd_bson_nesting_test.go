package goplayground

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func makeDeepNestedMap(depth int) map[string]any {
	m := map[string]any{"a": 1}

	for range depth {
		m = map[string]any{"a": m}
	}

	return m
}

// TestMGD_DeepNestedDocument reproduces the stack overflow from
// mongo-go-driver PR #2405. Aborts the test binary with
// "fatal error: stack overflow".
func TestMGD_DeepNestedDocument(t *testing.T) {
	// debug.SetMaxStack(64 * 1024 * 1024) // Without this, the test will pass

	bytes, err := bson.Marshal(makeDeepNestedMap(200_000))
	require.NoError(t, err)

	var got bson.M
	err = bson.Unmarshal(bytes, &got)
	require.NoError(t, err)
}

func TestJSON_DeepNestedDocument(t *testing.T) {
	// debug.SetMaxStack(64 * 1024 * 1024) // Without this, the test will pass

	bytes, err := json.Marshal(makeDeepNestedMap(200_000))
	require.NoError(t, err)

	var got map[string]any
	err = json.Unmarshal(bytes, &got)
	require.NoError(t, err)
}

// TestMGD_BoundedBytes_ImpliesBoundedDepth illustrates the encoding/json
// stdlib principle applied to BSON: bound the bytes at the I/O boundary,
// not the structure inside the parser. Each nested BSON document costs
// ~8 wire bytes, so a byte cap of N implies a structural cap of ~N/8
// levels — no parser-internal depth limit needed.
func TestMGD_BoundedBytes_ImpliesBoundedDepth(t *testing.T) {
	payload, err := bson.Marshal(makeDeepNestedMap(200_000))
	require.NoError(t, err)
	t.Logf("attacker payload: %d bytes (depth ~%d)", len(payload), 200_000)

	// Cap input bytes at the I/O boundary — same pattern as
	// http.MaxBytesReader. 256 KiB is well under the 1.6 MB payload.
	const maxBytes = 256 * 1024
	src := io.LimitReader(bytes.NewReader(payload), maxBytes)

	var got bson.M
	err = bson.NewDecoder(bson.NewDocumentReader(src)).Decode(&got)
	require.Error(t, err)
	t.Logf("bounded decode err: %v", err)
}
