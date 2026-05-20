package goplayground

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bsonv1 "go.mongodb.org/mongo-driver/bson"
	mongov1 "go.mongodb.org/mongo-driver/mongo"
)

func Test_IndexView_DropAll_Async(t *testing.T) {
	client, teardown := mongolocal.StartTV1(t, context.Background())
	defer teardown(t)

	coll := mongolocal.ArbCollV1(client)
	ctx := context.Background()

	// Insert a document so the collection is created.
	_, err := coll.InsertOne(ctx, bsonv1.D{{"x", 1}})
	require.NoError(t, err)

	// Create 2 initial indexes (+ default _id = 3 total).
	_, err = coll.Indexes().CreateMany(ctx, []mongov1.IndexModel{
		{Keys: bsonv1.D{{"field1", 1}}},
		{Keys: bsonv1.D{{"field2", 1}}},
	})
	require.NoError(t, err)

	// Concurrently create indexes while we call DropAll.
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < 10; i++ {
			fieldName := fmt.Sprintf("concurrent_field_%d", i)

			_, _ = coll.Indexes().CreateOne(ctx, mongov1.IndexModel{
				Keys: bsonv1.D{{fieldName, 1}},
			})
		}
	}()

	result, err := coll.Indexes().DropAll(ctx)
	require.NoError(t, err)

	wg.Wait()

	nIndexesWas := result.Lookup("nIndexesWas").Int32()
	t.Logf("nIndexesWas: %d", nIndexesWas)

	// List indexes immediately after DropAll. If concurrent creates landed
	// after the drop, there will be more than just the _id index.
	cursor, err := coll.Indexes().List(ctx)
	require.NoError(t, err)

	var indexes []bsonv1.Raw
	require.NoError(t, cursor.All(ctx, &indexes))

	t.Logf("indexes after DropAll: %d", len(indexes))

	assert.Greater(t, len(indexes), 1,
		"concurrent creates caused indexes to exist after DropAll, making nIndexesWas meaningless")
}
