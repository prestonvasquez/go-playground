package goplayground

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

func TestMGD_MarshalingWriteConcern(t *testing.T) {
	wcBytes, err := bson.Marshal(writeconcern.Majority())
	require.NoError(t, err)

	wc := writeconcern.WriteConcern{}
	require.NoError(t, bson.Unmarshal(wcBytes, &wc))
	require.Equal(t, writeconcern.WCMajority, wc.W)
}
