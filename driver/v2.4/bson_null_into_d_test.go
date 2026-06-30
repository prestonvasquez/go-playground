package v24

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestBSON_NullDecodesIntoD is the minimal, single-path version of the
// bson.D case: encode a document whose "doc" field is BSON null, then decode
// that null INTO a bson.D field, and check it comes back nil.
//
// Unlike bson.Raw, decoding null into bson.D yields bson.D(nil) on every
// driver version (≤ v2.5.0 included) — bson.Raw was the outlier.
func TestBSON_NullDecodesIntoD(t *testing.T) {
	// 1. Build {doc: null}.
	bin, err := bson.Marshal(bson.D{{Key: "doc", Value: nil}})
	require.NoError(t, err)

	// 2. Confirm the wire type really is null (not an empty document).
	require.Equal(t, bson.TypeNull, bson.Raw(bin).Lookup("doc").Type)

	// 3. Decode INTO a bson.D field.
	var out struct {
		Doc bson.Raw `bson:"doc"`
	}
	require.NoError(t, bson.Unmarshal(bin, &out))

	// 4. The decoded bson.D is nil.
	t.Logf("Doc = %#v (==nil: %v)", out.Doc, out.Doc == nil)
	// assert.Nil(t, out.Doc) // -> bson.Raw{}
	assert.Equal(t, out.Doc, bson.Raw{})
}
