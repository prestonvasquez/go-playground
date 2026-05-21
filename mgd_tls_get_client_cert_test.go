package goplayground

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madflojo/testcerts"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMGD_GetClientCertificate_ReReadsFromDisk(t *testing.T) {
	ca := testcerts.NewCA()

	serverKP, err := ca.NewKeyPairFromConfig(testcerts.KeyPairConfig{
		CommonName:  "server",
		Domains:     []string{"localhost"},
		IPAddresses: []string{"127.0.0.1"},
	})
	require.NoError(t, err, "server keypair")

	clientV1, err := ca.NewKeyPairFromConfig(testcerts.KeyPairConfig{
		CommonName: "client-v1",
		Domains:    []string{"client-v1"},
	})
	require.NoError(t, err, "client-v1 keypair")

	clientV2, err := ca.NewKeyPairFromConfig(testcerts.KeyPairConfig{
		CommonName: "client-v2",
		Domains:    []string{"client-v2"},
	})
	require.NoError(t, err, "client-v2 keypair")

	// Write client-v1 to disk; we'll overwrite with client-v2 mid-test.
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client.key")

	require.NoError(t, clientV1.ToFile(certPath, keyPath), "write client-v1 to disk")

	// Write the CA + server bundle to host paths so mongolocal.WithTLS can
	// mount them into the container.
	caHostPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caHostPath, ca.PublicKey(), 0o644), "write CA")

	serverHostPath := filepath.Join(dir, "server.pem")
	serverPEM := append(append([]byte{}, serverKP.PublicKey()...), serverKP.PrivateKey()...)
	require.NoError(t, os.WriteFile(serverHostPath, serverPEM, 0o644), "write server pem")

	// Hash the bytes we read from disk on each callback invocation so we can
	// prove the second invocation saw v2 contents, not a cached parse of v1.
	hashOf := func(b []byte) string {
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:8])
	}

	var closureCallCount atomic.Int64
	var observed sync.Map // hash -> *atomic.Int64

	// One *tls.Config built ONCE, with GetClientCertificate reading from disk
	// on every handshake.
	clientTLSConfig := &tls.Config{
		RootCAs:    ca.CertPool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			closureCallCount.Add(1)
			certBytes, err := os.ReadFile(certPath)
			if err != nil {
				return nil, err
			}
			counter, _ := observed.LoadOrStore(hashOf(certBytes), new(atomic.Int64))
			counter.(*atomic.Int64).Add(1)
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				return nil, err
			}
			return &cert, nil
		},
	}

	// Custom dialer wraps net.Dialer so we can sever every live TCP conn
	// between operations and force the driver to dial new ones.
	tracker := &connTracker{}

	clientOpts := options.Client().
		SetTLSConfig(clientTLSConfig).
		SetDialer(tracker).
		SetMaxPoolSize(1).
		SetHeartbeatInterval(500 * time.Millisecond).
		SetServerSelectionTimeout(10 * time.Second)

	ctx := context.Background()
	client, teardown := mongolocal.StartT(t, ctx,
		mongolocal.WithImage("mongo:7.0"),
		mongolocal.WithTLS(caHostPath, serverHostPath),
		mongolocal.WithMongoClientOptions(clientOpts),
	)
	defer teardown(t)

	opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)
	defer opCancel()

	v1Bytes, err := os.ReadFile(certPath)
	require.NoError(t, err)

	v1Hash := hashOf(v1Bytes)
	if c, ok := observed.Load(v1Hash); ok {
		require.Greater(t, c.(*atomic.Int64).Load(), int64(0),
			"callback should have read v1 from disk during startup ping")
	} else {
		t.Fatalf("callback never read v1 hash %s; observed: %v", v1Hash, mapKeys(&observed))
	}

	// Rotate the cert/key on disk to v2: same path, different bytes.
	require.NoError(t, clientV2.ToFile(certPath, keyPath), "rotate to v2")

	v2Bytes, err := os.ReadFile(certPath)

	require.NoError(t, err)
	require.NotEqual(t, v1Bytes, v2Bytes, "v2 bytes should differ from v1 bytes")

	v2Hash := hashOf(v2Bytes)

	require.NotEqual(t, v1Hash, v2Hash, "v2 hash should differ from v1 hash")

	// Load the number of times the closure was called establishing the
	// initial connections.
	closureCallCountBeforeReset := closureCallCount.Load()
	require.Greater(t, closureCallCountBeforeReset, int64(0),
		"GetClientCertificate should have been invoked at least once on the initial handshake")

	// Sever every TCP conn to force the driver to dial again. The new dial
	// will pass through TLS, which calls GetClientCertificate, which reads
	// disk now v2.
	tracker.closeAll()

	// Await the next successful ping on a new connection.
	require.Eventually(t, func() bool {
		return client.Database("admin").RunCommand(opCtx, bson.D{{Key: "ping", Value: 1}}).Err() == nil
	}, 20*time.Second, 100*time.Millisecond, "second ping should succeed on a new conn")

	// There should have been an additional call to the closure after the cert
	// rotation, proving that the closure was not only invoked on the initial
	// handshake, but also on the new handshake after we severed connections.
	require.Greater(t, closureCallCount.Load(), closureCallCountBeforeReset,
		"GetClientCertificate should have been invoked again on the new handshake")

	if c, ok := observed.Load(v2Hash); ok {
		require.Greater(t, c.(*atomic.Int64).Load(), int64(0),
			"callback should have read v2 from disk on the new handshake")
	} else {
		t.Fatalf("callback never read v2 hash %s; observed: %v", v2Hash, mapKeys(&observed))
	}
}

type connTracker struct {
	mu    sync.Mutex
	conns []net.Conn
}

func (d *connTracker) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.conns = append(d.conns, c)
	d.mu.Unlock()
	return c, nil
}

func (d *connTracker) closeAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.conns {
		_ = c.Close()
	}
	d.conns = nil
}

func mapKeys(m *sync.Map) []string {
	var out []string
	m.Range(func(k, _ any) bool { out = append(out, k.(string)); return true })
	return out
}
