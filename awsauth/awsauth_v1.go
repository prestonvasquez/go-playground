package awsauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	bsonv1 "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
	authv1 "go.mongodb.org/mongo-driver/x/mongo/driver/auth"
)

// RegisterV1 replaces the v1 driver's built-in MONGODB-AWS authenticator with
// one backed by aws-sdk-go-v2, using credentials from config.LoadDefaultConfig.
func RegisterV1() error {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return err
	}
	RegisterV1WithProvider(cfg.Credentials)
	return nil
}

// RegisterV1WithProvider replaces the v1 driver's MONGODB-AWS authenticator
// using the provided credential provider. Intended for testing with a custom
// or short-lived provider.
func RegisterV1WithProvider(provider aws.CredentialsProvider) {
	authv1.RegisterAuthenticatorFactory(authv1.MongoDBAWS, func(_ *authv1.Cred, _ *http.Client) (authv1.Authenticator, error) {
		return &v1Authenticator{provider: provider, signer: awsv4.NewSigner()}, nil
	})
}

type v1Authenticator struct {
	provider aws.CredentialsProvider
	signer   *awsv4.Signer
}

func (a *v1Authenticator) Auth(ctx context.Context, cfg *authv1.Config) error {
	return authv1.ConductSaslConversation(ctx, cfg, sourceExternal, &v1SaslClient{
		provider: a.provider,
		signer:   a.signer,
	})
}

func (a *v1Authenticator) Reauth(context.Context, *authv1.Config) error {
	return errors.New("AWS does not support reauthentication")
}

type v1SaslClient struct {
	provider aws.CredentialsProvider
	signer   *awsv4.Signer
	nonce    []byte
	done     bool
}

var _ authv1.SaslClient = (*v1SaslClient)(nil)

func (c *v1SaslClient) Start() (string, []byte, error) {
	c.nonce = make([]byte, 32)
	if _, err := rand.Read(c.nonce); err != nil {
		return "", nil, err
	}
	idx, msg := bsoncore.AppendDocumentStart(nil)
	msg = bsoncore.AppendInt32Element(msg, "p", 110)
	msg = bsoncore.AppendBinaryElement(msg, "r", 0x00, c.nonce)
	msg, _ = bsoncore.AppendDocumentEnd(msg, idx)
	return authv1.MongoDBAWS, msg, nil
}

func (c *v1SaslClient) Next(ctx context.Context, challenge []byte) ([]byte, error) {
	var sm struct {
		Nonce primitive.Binary `bson:"s"`
		Host  string           `bson:"h"`
	}
	if err := bsonv1.Unmarshal(challenge, &sm); err != nil {
		return nil, err
	}
	if sm.Nonce.Subtype != 0x00 || len(sm.Nonce.Data) != responceNonceLength {
		return nil, errors.New("invalid server nonce")
	}
	if !bytes.HasPrefix(sm.Nonce.Data, c.nonce) {
		return nil, errors.New("server nonce did not extend client nonce")
	}

	region, err := getRegion(sm.Host)
	if err != nil {
		return nil, err
	}

	creds, err := c.provider.Retrieve(ctx)
	if err != nil {
		return nil, err
	}

	const body = "Action=GetCallerIdentity&Version=2011-06-15"
	now := time.Now().UTC()
	req, _ := http.NewRequestWithContext(ctx, "POST", "/", strings.NewReader(body))
	req.Host = sm.Host
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Length", "43")
	req.Header.Set("X-Amz-Date", now.Format(amzDateFormat))
	req.Header.Set("X-MongoDB-Server-Nonce", base64.StdEncoding.EncodeToString(sm.Nonce.Data))
	req.Header.Set("X-MongoDB-GS2-CB-Flag", "n")

	sum := sha256.Sum256([]byte(body))
	if err = c.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), "sts", region, now); err != nil {
		return nil, err
	}

	c.done = true
	idx, msg := bsoncore.AppendDocumentStart(nil)
	msg = bsoncore.AppendStringElement(msg, "a", req.Header.Get("Authorization"))
	msg = bsoncore.AppendStringElement(msg, "d", req.Header.Get("X-Amz-Date"))
	if tok := req.Header.Get("X-Amz-Security-Token"); tok != "" {
		msg = bsoncore.AppendStringElement(msg, "t", tok)
	}
	msg, _ = bsoncore.AppendDocumentEnd(msg, idx)
	return msg, nil
}

func (c *v1SaslClient) Completed() bool { return c.done }
