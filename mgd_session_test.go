package goplayground

import (
	"bytes"
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/examplepkg"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/prestonvasquez/go-playground/timeutil"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMgdSession_SnapshotPoolConflict(t *testing.T) {
	// Test if using a snapshot time that's older than a pooled session's
	// lastUse timestamp causes a conflict error (WriteConflict, InvalidOptions).
	//
	// This is the scenario described in the BaaS issue where transactions
	// with client-supplied atClusterTime fail when paired with a session whose
	// lastUse is newer.

	ctx := context.Background()

	client, teardown := mongolocal.New(t, ctx, mongolocal.WithReplicaSet("rs0"))
	defer teardown(t)

	coll := mongolocal.ArbColl(client)
	defer func() { _ = coll.Drop(ctx) }()

	// Driver pools server session whose last use was at cluster time T_old.
	_, err := coll.InsertOne(context.Background(), bson.D{})
	require.NoError(t, err)

	res := client.Database("admin").RunCommand(context.Background(), bson.D{{Key: "hello"}})
	require.NoError(t, res.Err())

	rawHello, err := res.Raw()
	require.NoError(t, err)

	tsT, tsI, ok := rawHello.Lookup("lastWrite", "majorityOpTime", "ts").TimestampOK()
	require.True(t, ok, "failed to get lastWrite.majorityOpTime.ts from hello result")

	tOld := bson.Timestamp{T: tsT, I: tsI}

	session1, err := client.StartSession()
	require.NoError(t, err)

	// Use session1 to perform an operation
	_, err = coll.InsertOne(mongo.NewSessionContext(context.Background(), session1), bson.D{})
	require.NoError(t, err)

	session1.EndSession(context.Background())

	// Performa a santity check that the session has lastUsed = T_old.
	lastUsedS1 := timeutil.BSONTimestampFromTime(session1.ClientSession().Server.LastUsed, tOld.I)
	require.Equal(t, tOld, lastUsedS1)

	// Caller starts a snapshot session where T_user != T_old.
	var session2 *mongo.Session
	for {
		tUser := bson.Timestamp{T: tOld.T, I: tOld.I - 1} // T_user < T_old
		s2Opts := options.Session().SetSnapshot(true).SetSnapshotTime(tUser)

		session2, err = client.StartSession(s2Opts)
		require.NoError(t, err)

		if bytes.Equal(session2.ID(), session1.ID()) {
			break
		}
		session2.EndSession(context.Background())
	}

	// Driver pulls session1 from the pool and issues a snapshot command with
	// readConcern.atClusterTime = T_user
	findRes := coll.FindOne(mongo.NewSessionContext(context.Background(), session2), bson.D{})

	// Server does not detects the mismatch and does not return WriteConflict,
	// InvalidOptions, or similar.
	//
	// This is only true because the operation is a non-transactional snapshot
	// read; a transaction with readConcern "snapshot" and atClusterTime on a
	// reused server session can hit WriteConflict at commit time. Snapshot
	// sessions themselves can't be used in transactions (per the spec), so using
	// snapshotTime on a snapshot session cannot reproduce or avoid that
	// transactional conflict.
	require.Error(t, mongo.ErrNoDocuments, findRes.Err())
}

func TestMgdSession_ClientSessionAccessors(t *testing.T) {
	t.Run("Mutating with pointer accessor", func(t *testing.T) {
		sess := examplepkg.NewSession() // timestamp T=1, I=0

		sess.ClientSessionPtr().SnapshotTime = bson.Timestamp{T: 5, I: 10}
		require.Equal(t, bson.Timestamp{T: 5, I: 10}, sess.ClientSession().SnapshotTime)
	})

	t.Run("Non-mutating with value accessor", func(t *testing.T) {
		sess := examplepkg.NewSession() // timestamp T=1, I=0

		snapTime := sess.ClientSession().SnapshotTime
		snapTime.T = 10

		// Mutating top-level value does not affect underlying struct.
		require.Equal(t, bson.Timestamp{T: 1, I: 0}, sess.ClientSession().SnapshotTime)
	})
}
