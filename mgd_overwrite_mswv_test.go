package goplayground

import (
	"context"
	"testing"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/description"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/topology"
)

func TestMGDOverwriteMinimumSupportedWireVersion(t *testing.T) {
	topology.MinSupportedMongoDBVersion = "4.0"
	topology.SupportedWireVersions = description.VersionRange{Min: 7, Max: 25}

	client, teardown := mongolocal.New(t, context.Background(), mongolocal.WithImage("mongo:6"))
	defer teardown(t)

	require.NoError(t, client.Ping(context.Background(), nil))
}
