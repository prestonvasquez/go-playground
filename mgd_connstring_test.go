package goplayground

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_ConnString_NoTokenResourceWithAzure(t *testing.T) {
	// From the spec: If [...] TOKEN_RESOURCE is not provided and ENVIRONMENT is
	// one of ["azure", "gcp"], the driver MUST raise an error.

	connStr := "mongodb://clusterOne01.fancyCorp.com:20020,clusterOne02.fancyCorp.com:20020,clusterOne03.fancyCorp.com:20020/?authMechanism=MONGODB-OIDC&authMechanismProperties=ENVIRONMENT:azure"
	opts := options.Client().ApplyURI(connStr)

	_, err := mongo.Connect(opts)

	require.Error(t, err)
	require.ErrorContains(t, err, `TOKEN_RESOURCE" must be specified for "azure" "ENVIRONMENT"`)
}
