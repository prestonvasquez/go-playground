package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/atlaslocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_CreateVectorSearchIndex(t *testing.T) {
	// Test all autoEmbed fields according to spec:
	// Required: type, modality, path, model
	// Optional: compression, hnswOptions
	// Unsupported (should fail): similarity, quantization, numDimensions

	ctx := context.Background()
	client, teardown := atlaslocal.New(t, ctx)
	defer teardown(t)

	coll := client.Database("testdb").Collection("testcoll")

	// Insert a document to ensure the collection exists
	_, err := coll.InsertOne(ctx, bson.D{{Key: "sample", Value: "data"}})
	require.NoError(t, err)

	view := coll.SearchIndexes()

	// Define field struct for testing
	type autoEmbedField struct {
		Type          string      `bson:"type"`
		Modality      string      `bson:"modality"`
		Path          string      `bson:"path"`
		Model         string      `bson:"model"`
		Compression   interface{} `bson:"compression,omitempty"`
		HnswOptions   interface{} `bson:"hnswOptions,omitempty"`
		Similarity    string      `bson:"similarity,omitempty"`
		Quantization  interface{} `bson:"quantization,omitempty"`
		NumDimensions int         `bson:"numDimensions,omitempty"`
	}

	type autoEmbedDefinition struct {
		Fields []autoEmbedField `bson:"fields"`
	}

	t.Run("required fields only", func(t *testing.T) {
		indexName := "test-autoembed-required"
		definition := autoEmbedDefinition{
			Fields: []autoEmbedField{
				{
					Type:     "autoEmbed",
					Modality: "text",
					Path:     "description",
					Model:    "voyage-4-large",
				},
			},
		}

		opts := options.SearchIndexes().SetName(indexName).SetType("vectorSearch")
		createdName, err := view.CreateOne(ctx, mongo.SearchIndexModel{
			Definition: definition,
			Options:    opts,
		})
		require.NoError(t, err)
		require.Equal(t, indexName, createdName)

		// Verify index was created with correct definition
		doc := waitForIndex(t, ctx, view, opts, indexName)
		verifyAutoEmbedIndex(t, doc, "voyage-4-large", "description", "text")
	})

	t.Run("with compression field", func(t *testing.T) {
		// Note: compression is optional per spec, but Atlas Local may not support it yet
		indexName := "test-autoembed-compression"
		definition := autoEmbedDefinition{
			Fields: []autoEmbedField{
				{
					Type:     "autoEmbed",
					Modality: "text",
					Path:     "content",
					Model:    "voyage-4",
					Compression: bson.D{
						{Key: "type", Value: "scalar"},
					},
				},
			},
		}

		opts := options.SearchIndexes().SetName(indexName).SetType("vectorSearch")
		_, err := view.CreateOne(ctx, mongo.SearchIndexModel{
			Definition: definition,
			Options:    opts,
		})

		// Atlas Local currently does not support the compression field (as of testing)
		// This documents the current behavior - may change when fully implemented
		if err != nil {
			require.ErrorContains(t, err, "unrecognized field \"compression\"",
				"compression field not yet supported in Atlas Local")
			t.Skip("Skipping compression test - not yet supported in Atlas Local")
			return
		}

		// If compression becomes supported, verify it works
		doc := waitForIndex(t, ctx, view, opts, indexName)
		verifyAutoEmbedIndex(t, doc, "voyage-4", "content", "text")

		// Verify compression field exists
		latestDef, _ := doc.Lookup("latestDefinition").DocumentOK()
		fieldsArray, _ := latestDef.Lookup("fields").ArrayOK()
		fieldsValues, _ := fieldsArray.Values()
		fieldDoc, _ := fieldsValues[0].DocumentOK()

		compressionVal := fieldDoc.Lookup("compression")
		require.NotEqual(t, bson.TypeNull, compressionVal.Type, "compression should be present")
	})

	t.Run("with hnswOptions field", func(t *testing.T) {
		// Note: hnswOptions is optional per spec, but Atlas Local may not support it yet
		indexName := "test-autoembed-hnsw"
		definition := autoEmbedDefinition{
			Fields: []autoEmbedField{
				{
					Type:     "autoEmbed",
					Modality: "text",
					Path:     "title",
					Model:    "voyage-4-lite",
					HnswOptions: bson.D{
						{Key: "m", Value: 16},
						{Key: "efConstruction", Value: 64},
					},
				},
			},
		}

		opts := options.SearchIndexes().SetName(indexName).SetType("vectorSearch")
		_, err := view.CreateOne(ctx, mongo.SearchIndexModel{
			Definition: definition,
			Options:    opts,
		})

		// Atlas Local currently does not support the hnswOptions field (as of testing)
		// This documents the current behavior - may change when fully implemented
		if err != nil {
			require.ErrorContains(t, err, "unrecognized field \"hnswOptions\"",
				"hnswOptions field not yet supported in Atlas Local")
			t.Skip("Skipping hnswOptions test - not yet supported in Atlas Local")
			return
		}

		// If hnswOptions becomes supported, verify it works
		doc := waitForIndex(t, ctx, view, opts, indexName)
		verifyAutoEmbedIndex(t, doc, "voyage-4-lite", "title", "text")

		// Verify hnswOptions field exists
		latestDef, _ := doc.Lookup("latestDefinition").DocumentOK()
		fieldsArray, _ := latestDef.Lookup("fields").ArrayOK()
		fieldsValues, _ := fieldsArray.Values()
		fieldDoc, _ := fieldsValues[0].DocumentOK()

		hnswVal := fieldDoc.Lookup("hnswOptions")
		require.NotEqual(t, bson.TypeNull, hnswVal.Type, "hnswOptions should be present")
	})

	t.Run("unsupported similarity field should fail", func(t *testing.T) {
		indexName := "test-autoembed-similarity-fail"
		definition := autoEmbedDefinition{
			Fields: []autoEmbedField{
				{
					Type:       "autoEmbed",
					Modality:   "text",
					Path:       "text",
					Model:      "voyage-4-nano",
					Similarity: "cosine", // This should cause an error
				},
			},
		}

		opts := options.SearchIndexes().SetName(indexName).SetType("vectorSearch")
		_, err := view.CreateOne(ctx, mongo.SearchIndexModel{
			Definition: definition,
			Options:    opts,
		})
		// Expect an error about invalid index definition
		require.Error(t, err, "similarity field should not be supported for autoEmbed")
	})

	t.Run("unsupported quantization field should fail", func(t *testing.T) {
		indexName := "test-autoembed-quantization-fail"
		definition := autoEmbedDefinition{
			Fields: []autoEmbedField{
				{
					Type:         "autoEmbed",
					Modality:     "text",
					Path:         "text",
					Model:        "voyage-code-3",
					Quantization: bson.D{{Key: "type", Value: "scalar"}}, // This should cause an error
				},
			},
		}

		opts := options.SearchIndexes().SetName(indexName).SetType("vectorSearch")
		_, err := view.CreateOne(ctx, mongo.SearchIndexModel{
			Definition: definition,
			Options:    opts,
		})
		// Expect an error about invalid index definition
		require.Error(t, err, "quantization field should not be supported for autoEmbed")
	})

	t.Run("unsupported numDimensions field should fail", func(t *testing.T) {
		indexName := "test-autoembed-numdimensions-fail"
		definition := autoEmbedDefinition{
			Fields: []autoEmbedField{
				{
					Type:          "autoEmbed",
					Modality:      "text",
					Path:          "text",
					Model:         "voyage-4",
					NumDimensions: 1536, // This should cause an error
				},
			},
		}

		opts := options.SearchIndexes().SetName(indexName).SetType("vectorSearch")
		_, err := view.CreateOne(ctx, mongo.SearchIndexModel{
			Definition: definition,
			Options:    opts,
		})
		// Expect an error about invalid index definition
		require.Error(t, err, "numDimensions field should not be supported for autoEmbed")
	})
}

// Helper function to wait for an index to appear in the list
func waitForIndex(t *testing.T, ctx context.Context, view mongo.SearchIndexView, opts *options.SearchIndexesOptionsBuilder, indexName string) bson.Raw {
	t.Helper()

	awaitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var doc bson.Raw
	for doc == nil {
		cursor, err := view.List(awaitCtx, opts)
		require.NoError(t, err, "failed to list")

		if !cursor.Next(awaitCtx) {
			time.Sleep(2 * time.Second)
			continue
		}

		name := cursor.Current.Lookup("name").StringValue()
		if name == indexName {
			doc = cursor.Current
			t.Logf("Index found: %s", cursor.Current.String())
			break
		}
		time.Sleep(2 * time.Second)
	}
	require.NotNil(t, doc, "got empty document")
	return doc
}

// Helper function to verify autoEmbed index fields
func verifyAutoEmbedIndex(t *testing.T, doc bson.Raw, expectedModel, expectedPath, expectedModality string) {
	t.Helper()

	// Verify the index type is vectorSearch
	indexType := doc.Lookup("type").StringValue()
	require.Equal(t, "vectorSearch", indexType)

	// Verify the index definition contains autoEmbed fields
	latestDefVal := doc.Lookup("latestDefinition")
	latestDef, ok := latestDefVal.DocumentOK()
	require.True(t, ok, "latestDefinition should be a document")

	// Check that the fields array exists
	fieldsVal := latestDef.Lookup("fields")
	fieldsArray, ok := fieldsVal.ArrayOK()
	require.True(t, ok, "fields should be an array")

	fieldsValues, err := fieldsArray.Values()
	require.NoError(t, err)
	require.Len(t, fieldsValues, 1, "should have one field")

	fieldDoc, ok := fieldsValues[0].DocumentOK()
	require.True(t, ok, "field should be a document")

	// Verify required fields
	fieldType := fieldDoc.Lookup("type").StringValue()
	require.Equal(t, "autoEmbed", fieldType, "field type should be autoEmbed")

	modality := fieldDoc.Lookup("modality").StringValue()
	require.Equal(t, expectedModality, modality, "modality mismatch")

	path := fieldDoc.Lookup("path").StringValue()
	require.Equal(t, expectedPath, path, "path mismatch")

	model := fieldDoc.Lookup("model").StringValue()
	require.Equal(t, expectedModel, model, "model mismatch")
}
