package goplayground

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
)

func TestMGD_IdempotentClientDisconnect(t *testing.T) {
	// What happens if you call disconect multiple times on a mongo.Client?

	client, teardown := mongolocal.New(t, context.Background())
	defer teardown(t)

	require.NoError(t, client.Ping(context.Background(), nil))

	//_ = client.Disconnect(context.Background())
	//_ = client.Disconnect(context.Background())
}
