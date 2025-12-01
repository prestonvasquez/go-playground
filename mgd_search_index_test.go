package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/atlaslocal"
	"github.com/prestonvasquez/go-playground/mongoindex"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// What happens when a name and type are not provided when creating a search
// index?
func TestSearchIndexDefault(t *testing.T) {
	// Create an Atlas test client
	client, teardown := atlaslocal.New(t, context.Background())
	defer teardown(t)

	collection := client.Database("testdb").Collection("testCollection2")

	// Insert a sample document { stringField: "test-string-field" }
	sampleDoc := struct{ StringField string }{StringField: "test-string-field"}

	_, err := collection.InsertOne(context.Background(), sampleDoc)
	require.NoError(t, err)

	// Set up search index definition with dynamic mappings but no name or type.
	def := mongoindex.Def{Mappings: mongoindex.DefMappings{Dynamic: true}}

	searchIndexModel := mongo.SearchIndexModel{
		Definition: def,
		Options:    options.SearchIndexes(),
	}

	indexName, err := collection.SearchIndexes().CreateOne(context.Background(), searchIndexModel)
	require.NoError(t, err)

	// Ensure that an index name was generated to the server default:
	require.Equal(t, "default", indexName)

	// Await the index to become queryable.
	awaitCtx, awaitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer awaitCancel()

	doc := mongoindex.AwaitIndex(t, awaitCtx, collection.SearchIndexes(), indexName)

	siType := doc.Lookup("type").StringValue()
	require.Equal(t, "search", siType)
}
