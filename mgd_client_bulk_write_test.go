package goplayground

import (
	"context"
	"fmt"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestMGD_ClientBulkWrite_DupeKeyErrorCodes(t *testing.T) {
	// Inserting a duplicate _id via Client.BulkWrite should return a
	// duplicate-key error whose codes are visible through mongo.ErrorCodes.

	client, teardown := mongolocal.New(t, context.Background())
	defer teardown(t)

	coll := mongolocal.ArbColl(client)

	writes := []mongo.ClientBulkWrite{
		{
			Database:   coll.Database().Name(),
			Collection: coll.Name(),
			Model:      mongo.NewClientInsertOneModel().SetDocument(bson.D{{"_id", 1}}),
		},
	}

	// First insert should succeed.
	_, err := client.BulkWrite(context.Background(), writes)
	require.NoError(t, err)

	// Second insert with the same _id should cause a duplicate-key error.
	_, err = client.BulkWrite(context.Background(), writes)
	require.Error(t, err)

	codes := mongo.ErrorCodes(err)
	fmt.Printf("%v\n", codes)
	require.Contains(t, codes, 11000, "expected duplicate key error code 11000, got %v", codes)
}
