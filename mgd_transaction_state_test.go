package goplayground

import (
	"context"
	"errors"
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

// TestMGD_RequireExistingTransaction shows how to require an existing
// transaction using the current API (workaround for IsTransactionRunning).
func TestMGD_RequireExistingTransaction(t *testing.T) {
	client, teardown := mongolocal.New(t, context.Background(), mongolocal.WithReplicaSet("rs0"))
	defer teardown(t)

	coll := mongolocal.ArbColl(client)

	// Operation that requires an existing transaction
	addLineItem := func(ctx context.Context, sess *mongo.Session, item string) error {
		err := sess.StartTransaction()
		if err == nil {
			sess.AbortTransaction(ctx)
			return errors.New("this operation requires an existing transaction")
		}

		_, err = coll.InsertOne(ctx, bson.D{{"item", item}})
		return err
	}

	t.Run("fails without transaction", func(t *testing.T) {
		sess, err := client.StartSession()
		require.NoError(t, err)
		defer sess.EndSession(context.Background())

		ctx := mongo.NewSessionContext(context.Background(), sess)

		err = addLineItem(ctx, sess, "widget")
		require.Error(t, err)
		require.Equal(t, "this operation requires an existing transaction", err.Error())
	})

	t.Run("succeeds with transaction", func(t *testing.T) {
		sess, err := client.StartSession()
		require.NoError(t, err)
		defer sess.EndSession(context.Background())

		err = sess.StartTransaction()
		require.NoError(t, err)

		ctx := mongo.NewSessionContext(context.Background(), sess)

		err = addLineItem(ctx, sess, "gadget")
		require.NoError(t, err)

		err = sess.CommitTransaction(context.Background())
		require.NoError(t, err)
	})

	// Only the second operation succeeded
	count, err := coll.CountDocuments(context.Background(), bson.D{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}
