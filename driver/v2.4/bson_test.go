package v24

// TestBSON_NilableTypesToRaw documents which nilable Go types encode to BSON
// null and how BSON null decodes into a bson.Raw field.
//
// GODRIVER-3924 (PR #2401) changed the decode behavior:
//
//	≤ v2.5.0: BSON null → bson.Raw{} (non-nil empty slice; nil check fails)
//	≥ v2.7.0: BSON null → bson.Raw(nil) (nil slice; nil check passes)
//
// The distinction matters for callers that test rt.Doc == nil to detect a
// missing/null field.

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestBSON_NilableTypesToRaw(t *testing.T) {
	t.Parallel()

	type namedStruct struct{ Name string }

	type tc struct {
		name    string
		value   any
		wantNil bool // rt.Doc == nil after unmarshal
		wantErr bool // bson.Marshal expected to fail
	}

	tests := []tc{
		// ── Untyped nil ──────────────────────────────────────────────────────
		// encodeElement short-circuits: e.Value == nil → WriteNull
		{name: "untyped nil", value: nil, wantNil: true},

		// ── Nil pointers ─────────────────────────────────────────────────────
		// ptrEncodeValue: val.IsNil() → WriteNull
		{name: "nil *struct{}", value: (*struct{})(nil), wantNil: true},
		{name: "nil *namedStruct", value: (*namedStruct)(nil), wantNil: true},
		{name: "nil *int", value: (*int)(nil), wantNil: true},
		{name: "nil *string", value: (*string)(nil), wantNil: true},
		{name: "nil *bson.D", value: (*bson.D)(nil), wantNil: true},
		{name: "nil *bson.M", value: (*bson.M)(nil), wantNil: true},
		{name: "nil *bson.Raw", value: (*bson.Raw)(nil), wantNil: true},
		{name: "nil *bson.A", value: (*bson.A)(nil), wantNil: true},

		// ── Nil maps ─────────────────────────────────────────────────────────
		// mapEncodeValue: val.IsNil() && !encodeNilAsEmpty → WriteNull
		{name: "nil map[string]any", value: (map[string]any)(nil), wantNil: true},
		{name: "nil bson.M", value: bson.M(nil), wantNil: true},

		// ── Nil slices ───────────────────────────────────────────────────────
		// sliceEncodeValue: nil check (line 28) fires before the []E document
		// path (line 40) and the []byte binary path (line 33).
		// byteSliceCodec: nil check fires before WriteBinary.
		{name: "nil bson.D", value: bson.D(nil), wantNil: true},
		{name: "nil bson.A", value: bson.A(nil), wantNil: true},
		{name: "nil []any", value: ([]any)(nil), wantNil: true},
		{name: "nil []byte", value: ([]byte)(nil), wantNil: true},

		// ── Non-nil empty types (encode as BSON document or array) ───────────
		// rawDecodeValue handles TypeEmbeddedDocument and TypeArray by reading
		// the BSON bytes, so the result is a non-nil (possibly minimal) bson.Raw.
		{name: "empty struct{}", value: struct{}{}, wantNil: false},
		{name: "empty bson.D{}", value: bson.D{}, wantNil: false},
		{name: "empty bson.M{}", value: bson.M{}, wantNil: false},
		{name: "empty bson.A{}", value: bson.A{}, wantNil: false},
		{name: "empty map[string]any{}", value: map[string]any{}, wantNil: false},
		{name: "non-nil namedStruct", value: namedStruct{Name: "hello"}, wantNil: false},
		{name: "non-nil bson.D with field", value: bson.D{{Key: "k", Value: "v"}}, wantNil: false},

		// ── bson.Raw edge cases ───────────────────────────────────────────────
		// rawEncodeValue calls copyDocumentFromBytes(vw, rdr). Both nil and
		// empty Raw have 0 bytes — too short to hold a BSON length prefix
		// (4 bytes minimum) — so marshal fails.
		{name: "bson.Raw(nil)", value: bson.Raw(nil), wantErr: true},
		{name: "bson.Raw{}", value: bson.Raw{}, wantErr: true},
	}

	type container struct {
		Doc bson.Raw `bson:"doc"`
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := bson.D{{Key: "doc", Value: tc.value}}
			bin, err := bson.Marshal(doc)

			if tc.wantErr {
				require.Error(t, err, "expected marshal error")
				return
			}
			require.NoError(t, err)

			// Report the BSON type written for "doc".
			bsonType := bson.Raw(bin).Lookup("doc").Type

			var rt container
			require.NoError(t, bson.Unmarshal(bin, &rt))

			fmt.Printf("%-40s  bsonType=%-20s  result=%#v\n", tc.name+":", bsonType, rt.Doc)

			if tc.wantNil {
				assert.Nil(t, rt.Doc,
					"BSON type %s: expected bson.Raw(nil) but got %#v", bsonType, rt.Doc)
			} else {
				assert.NotNil(t, rt.Doc,
					"BSON type %s: expected non-nil bson.Raw but got nil", bsonType)
			}
		})
	}
}

func TestBSON_Raw(t *testing.T) {
	assert.True(t, bson.Raw(nil) == nil, "Raw(nil) should be valid")
	assert.False(t, bson.Raw{} == nil, "Raw{} should not be valid")
}
