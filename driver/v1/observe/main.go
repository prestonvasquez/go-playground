package main

import (
	"fmt"
	"log"

	"go.mongodb.org/mongo-driver/bson"
)

func main() {
	cases := []struct {
		name  string
		value any
	}{
		{"untyped nil", nil},
		{"nil *struct{}", (*struct{})(nil)},
		{"nil *int", (*int)(nil)},
		{"nil *bson.D", (*bson.D)(nil)},
		{"nil *bson.Raw", (*bson.Raw)(nil)},
		{"nil map[string]any", (map[string]any)(nil)},
		{"nil bson.M", bson.M(nil)},
		{"nil bson.D", bson.D(nil)},
		{"nil bson.A", bson.A(nil)},
		{"nil []any", ([]any)(nil)},
		{"nil []byte", ([]byte)(nil)},
		{"bson.Raw(nil)", bson.Raw(nil)},
	}

	type container struct {
		Doc bson.Raw `bson:"doc"`
	}

	for _, c := range cases {
		doc := bson.D{{Key: "doc", Value: c.value}}
		bin, err := bson.Marshal(doc)
		if err != nil {
			fmt.Printf("%-40s  marshal error: %v\n", c.name+":", err)
			continue
		}

		bsonType := bson.Raw(bin).Lookup("doc").Type

		var rt container
		if err := bson.Unmarshal(bin, &rt); err != nil {
			fmt.Printf("%-40s  bsonType=%-15s  unmarshal error: %v\n", c.name+":", bsonType, err)
			continue
		}

		isNil := rt.Doc == nil
		fmt.Printf("%-40s  bsonType=%-15s  result=%-20#v  isNil=%v\n", c.name+":", bsonType, rt.Doc, isNil)
	}

	_ = log.Println
}
