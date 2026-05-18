package goplayground

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/prestonvasquez/go-playground/awsauth"
	"github.com/prestonvasquez/go-playground/awsutil"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	mongov1 "go.mongodb.org/mongo-driver/mongo"
	optionsv1 "go.mongodb.org/mongo-driver/mongo/options"
)

// TestMGD_V1_AWS_STSCredentials_ExpireWithoutClientRecreation demonstrates
// that a custom MONGODB-AWS authenticator can refresh AWS credentials on
// each new connection without recreating the mongo.Client.
//
// Programmatically emulates drivers-evergreen-tools/.evergreen/auth_aws:
//   - setup_secrets.py: pull drivers/aws_auth from Secrets Manager via AWS_PROFILE.
//   - aws_tester.py setup_regular: create the $external MongoDB user for the
//     IAM ECS account ARN.
//   - The actual auth + 5s fake-expiry refresh path.
//
// Required env vars:
//
//	AWS_PROFILE - profile with read access to the drivers/aws_auth secret
//	MONGODB_URI - admin URI for a MONGODB-AWS-enabled server, e.g.
//	              mongodb://bob:pwd123@localhost:27017/ from start-mongodb-aws.sh
func TestMGD_V1_AWS_STSCredentials_ExpireWithoutClientRecreation(t *testing.T) {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		t.Skip("set AWS_PROFILE to run this test")
	}

	adminURI := os.Getenv("MONGODB_URI")
	if adminURI == "" {
		t.Skip("set MONGODB_URI to the SCRAM admin URI for a MONGODB-AWS-enabled cluster")
	}

	ctx := context.Background()

	// Step 1. emulate setup_secrets.py drivers/aws_auth
	secrets, err := awsutil.FetchSecret(ctx, profile, "drivers/aws_auth")
	require.NoError(t, err, "fetch drivers/aws_auth from Secrets Manager")

	accessKey := secrets["IAM_AUTH_ECS_ACCOUNT"]
	require.NotEmpty(t, accessKey, "IAM_AUTH_ECS_ACCOUNT missing from secret")

	secretKey := secrets["IAM_AUTH_ECS_SECRET_ACCESS_KEY"]
	require.NotEmpty(t, secretKey, "IAM_AUTH_ECS_SECRET_ACCESS_KEY missing from secret")

	userARN := secrets["IAM_AUTH_ECS_ACCOUNT_ARN"]
	require.NotEmpty(t, userARN, "IAM_AUTH_ECS_ACCOUNT_ARN missing from secret")

	t.Logf("fetched IAM creds for %s", userARN)

	// Step 2.  emulate aws_tester.py create_user(): bob/pwd123 -> $external
	adminClient, err := mongov1.Connect(ctx, optionsv1.Client().ApplyURI(adminURI))
	require.NoError(t, err, "admin connect")
	defer adminClient.Disconnect(ctx)
	require.NoError(t, ensureExternalAWSUser(ctx, adminClient, userARN), "create $external user")

	// Step 3. register the custom authenticator with fake 5s expiry
	inner := awscreds.NewStaticCredentialsProvider(accessKey, secretKey, "")
	fakeProvider := &fakeExpiringProvider{
		inner:  inner,
		expiry: time.Now().Add(5 * time.Second),
	}
	provider := aws.NewCredentialsCache(fakeProvider)
	awsauth.RegisterV1WithProvider(provider)

	awsURI, err := awsutil.StripURICredentials(adminURI)
	require.NoError(t, err)
	clientOpts := optionsv1.Client().
		ApplyURI(awsURI).
		SetAuth(optionsv1.Credential{AuthMechanism: "MONGODB-AWS"}).
		SetMaxConnIdleTime(2 * time.Second)

	client, err := mongov1.Connect(ctx, clientOpts)
	require.NoError(t, err, "MONGODB-AWS connect")
	defer client.Disconnect(ctx)

	require.NoError(t, client.Ping(ctx, nil), "initial ping — credentials valid")

	t.Log("waiting 8s for fake credential expiry and idle connection close...")
	time.Sleep(8 * time.Second)

	require.NoError(t, client.Ping(ctx, nil), "ping after credential expiry — no client recreation needed")

	// Step 4. verify that the provider's Retrieve method was called at least
	// twice.
	retrieveCount := fakeProvider.retrieveCount.Load()
	require.GreaterOrEqual(t, retrieveCount, int32(2),
		"expected Retrieve to be called at least twice, got %d", retrieveCount)
}

// ensureExternalAWSUser creates username in the $external db with read on
// the aws database. Matches aws_tester.py create_user(): tolerates "already
// exists" and re-runs cleanly.
func ensureExternalAWSUser(ctx context.Context, client *mongov1.Client, username string) error {
	res := client.Database("$external").RunCommand(ctx, bson.D{
		{Key: "createUser", Value: username},
		{Key: "roles", Value: bson.A{
			bson.D{{Key: "role", Value: "read"}, {Key: "db", Value: "aws"}},
		}},
	})
	if err := res.Err(); err != nil {
		var ce mongov1.CommandError
		if errors.As(err, &ce) && ce.Code == 51003 { // DuplicateKey: user already exists
			return nil
		}
		// Pre-4.4 servers return a generic code; fall back to message check.
		if errors.As(err, &ce) && ce.Name == "Location51003" {
			return nil
		}
		return err
	}
	return nil
}

// fakeExpiringProvider wraps a real credential provider but overrides the
// expiry with an artificial deadline. aws.NewCredentialsCache calls Retrieve
// again once that TTL passes, exercising the refresh path without needing
// real credential rotation.
type fakeExpiringProvider struct {
	inner         aws.CredentialsProvider
	expiry        time.Time
	retrieveCount atomic.Int32
}

func (p *fakeExpiringProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	p.retrieveCount.Add(1)

	creds, err := p.inner.Retrieve(ctx)
	if err != nil {
		return creds, err
	}
	creds.CanExpire = true
	creds.Expires = p.expiry
	return creds, nil
}
