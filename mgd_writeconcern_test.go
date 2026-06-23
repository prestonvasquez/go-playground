package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Source          string
	CanExpire       bool
	Expires         time.Time
	AccountID       string
}

// Lives inside the awsauth package
type AccessKeyIDGetter interface {
	GetAccessKeyID() string
}

type SecretAccessKeyGetter interface {
	GetSecretAccessKey() string
}

type AWSCredentialsProvider interface {
	AccessKeyIDGetter
	SecretAccessKeyGetter
}

func TestMGD_MarshalingWriteConcern(t *testing.T) {
	// Can writeconcern.WriteConcern be marshaled into BSON?

	wcBytes, err := bson.Marshal(writeconcern.Majority())
	require.NoError(t, err)

	wc := writeconcern.WriteConcern{}
	require.NoError(t, bson.Unmarshal(wcBytes, &wc))
	require.Equal(t, writeconcern.WCMajority, wc.W)
}

func TestMGD_RunCommandWithWCJournal(t *testing.T) {
	// Does using journal in the write concern cause a server error?

	client, teardown := mongolocal.StartT(t, context.Background())
	defer teardown(t)

	cmd := bson.D{
		{Key: "insert", Value: "foo"},
		{Key: "documents", Value: bson.A{bson.D{}}},
		{Key: "writeConcern", Value: bson.D{
			{Key: "w", Value: "majority"},
			{Key: "journal", Value: true},
		}},
	}

	res := mongolocal.ArbDB(client).RunCommand(context.Background(), cmd)
	require.Error(t, res.Err())
	require.Contains(t, mongo.ErrorCodes(res.Err()), 40415) // 40415: IDLUnknownField
}
