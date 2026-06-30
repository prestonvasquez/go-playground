package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func TestBSON_NilableTypesToRaw(t *testing.T) {
	t.Parallel()

	type namedStruct struct{ Name string }

	type tc struct {
		name    string
		value   any
		wantNil bool
		wantErr bool
	}

	tests := []tc{
		{name: "untyped nil", value: nil, wantNil: true},
		{name: "nil *struct{}", value: (*struct{})(nil), wantNil: true},
		{name: "nil *namedStruct", value: (*namedStruct)(nil), wantNil: true},
		{name: "nil *int", value: (*int)(nil), wantNil: true},
		{name: "nil *string", value: (*string)(nil), wantNil: true},
		{name: "nil *bson.D", value: (*bson.D)(nil), wantNil: true},
		{name: "nil *bson.M", value: (*bson.M)(nil), wantNil: true},
		{name: "nil *bson.Raw", value: (*bson.Raw)(nil), wantNil: true},
		{name: "nil *bson.A", value: (*bson.A)(nil), wantNil: true},
		{name: "nil map[string]any", value: (map[string]any)(nil), wantNil: true},
		{name: "nil bson.M", value: bson.M(nil), wantNil: true},
		{name: "nil bson.D", value: bson.D(nil), wantNil: true},
		{name: "nil bson.A", value: bson.A(nil), wantNil: true},
		{name: "nil []any", value: ([]any)(nil), wantNil: true},
		{name: "nil []byte", value: ([]byte)(nil), wantNil: true},
		{name: "bson.Raw(nil)", value: bson.Raw(nil), wantErr: true},
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

			bsonType := bson.Raw(bin).Lookup("doc").Type

			var rt container
			require.NoError(t, bson.Unmarshal(bin, &rt))

			fmt.Printf("%-40s  bsonType=%-20s  result=%#v\n", tc.name+":", bsonType, rt.Doc)

			if tc.wantNil {
				assert.Nil(t, rt.Doc,
					"BSON type %s: expected nil but got %#v", bsonType, rt.Doc)
			} else {
				assert.NotNil(t, rt.Doc,
					"BSON type %s: expected non-nil but got nil", bsonType)
			}
		})
	}
}
