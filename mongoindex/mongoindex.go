package mongoindex

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// DefMappings represents the mappings for an index definition.
type DefMappings struct {
	Dynamic bool
}

// Def represents the definition of an index.
type Def struct {
	Mappings DefMappings
}

// AwaitIndex waits for a search index with the given name to become queryable.
func AwaitIndex(t *testing.T, ctx context.Context, siv mongo.SearchIndexView, searchName string) bson.Raw {
	t.Helper()

	// Await the creation of the index.
	var doc bson.Raw
	for doc == nil {
		cursor, err := siv.List(ctx, options.SearchIndexes().SetName(searchName))
		require.NoError(t, err)

		if !cursor.Next(ctx) {
			break
		}

		name := cursor.Current.Lookup("name").StringValue()
		queryable := cursor.Current.Lookup("queryable").Boolean()

		if name == searchName && queryable {
			doc = cursor.Current
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return doc
}
