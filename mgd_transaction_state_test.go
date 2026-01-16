package goplayground

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestMGD_SessionContextHandlesTransactionAutomatically(t *testing.T) {
	client, teardown := mongolocal.New(t, context.Background(), mongolocal.WithReplicaSet("rs0"))
	defer teardown(t)

	coll := mongolocal.ArbColl(client)

	codeB := func(ctx context.Context, value string) error {
		_, err := coll.InsertOne(ctx, struct {
			Value string `bson:"value"`
		}{Value: value})
		return err
	}

	t.Run("with transaction", func(t *testing.T) {
		sess, err := client.StartSession()
		require.NoError(t, err)
		defer sess.EndSession(context.Background())

		err = sess.StartTransaction()
		require.NoError(t, err)

		ctx := mongo.NewSessionContext(context.Background(), sess)

		err = codeB(ctx, "in-txn")
		require.NoError(t, err)

		err = sess.CommitTransaction(context.Background())
		require.NoError(t, err)
	})

	t.Run("without transaction", func(t *testing.T) {
		sess, err := client.StartSession()
		require.NoError(t, err)
		defer sess.EndSession(context.Background())

		// Session exists but no transaction started.
		ctx := mongo.NewSessionContext(context.Background(), sess)

		err = codeB(ctx, "no-txn")
		require.NoError(t, err)
	})

	// Both operations succeeded
	count, err := coll.CountDocuments(context.Background(), bson.D{})
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
}
