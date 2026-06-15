package goplayground

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Encoder.OmitEmpty() is not propagated into nested structs, so the nil Inner
// field is written as null instead of being omitted.
func TestMGD_BSON_OmitEmpty(t *testing.T) {
	type Nested struct {
		Inner *int `bson:"inner"`
	}

	buf := new(bytes.Buffer)
	enc := bson.NewEncoder(bson.NewDocumentWriter(buf))
	enc.OmitEmpty()

	err := enc.Encode(struct {
		Nested Nested `bson:"nested"`
	}{})
	require.NoError(t, err)

	nested := bson.Raw(buf.Bytes()).Lookup("nested").Document()

	// OmitEmpty() should drop the nil Inner pointer, so "inner" must be absent.
	_, err = nested.LookupErr("inner")
	require.Error(t, err, "OmitEmpty() not propagated to nested struct: %q", nested.String())
}
