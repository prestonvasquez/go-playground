package goplayground

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mongov1 "go.mongodb.org/mongo-driver/mongo"
	mongov1options "go.mongodb.org/mongo-driver/mongo/options"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
)

func TestMGD_TLSHeartbeatV1WithMongoLocal(t *testing.T) {
	ctx := context.Background()

	// Start MongoDB container using mongolocal
	_, teardown := mongolocal.New(t, ctx, mongolocal.WithImage("mongo:6.0"))
	defer teardown(t)

	// We'll manually construct connection strings to test with v1 driver
	// Get the endpoint from the container
	endpoint := "localhost:27017" // mongolocal uses testcontainers which maps to localhost

	// Track all network traffic
	var mu sync.Mutex
	connections := []connectionInfo{}

	customDialer := &tlsTrackingDialer{
		onConnect: func(network, address string, isTLS bool) {
			mu.Lock()
			defer mu.Unlock()
			connections = append(connections, connectionInfo{
				network: network,
				address: address,
				isTLS:   isTLS,
				time:    time.Now(),
			})
			t.Logf("Connection: network=%s, address=%s, isTLS=%v", network, address, isTLS)
		},
	}

	// Test 1: Connect WITHOUT TLS
	t.Run("WithoutTLS", func(t *testing.T) {
		mu.Lock()
		connections = []connectionInfo{} // reset
		mu.Unlock()

		uri := fmt.Sprintf("mongodb://%s/?directConnection=true", endpoint)
		clientOpts := mongov1options.Client().
			ApplyURI(uri).
			SetDialer(customDialer).
			SetHeartbeatInterval(1 * time.Second)

		client, err := mongov1.Connect(ctx, clientOpts)
		require.NoError(t, err, "Failed to connect without TLS")

		defer client.Disconnect(ctx)

		// Ping to ensure connection
		require.NoError(t, client.Ping(ctx, nil), "Ping failed without TLS")

		// Wait for heartbeat
		time.Sleep(2 * time.Second)

		mu.Lock()
		defer mu.Unlock()

		require.Greater(t, len(connections), 0, "No connections tracked")

		for _, conn := range connections {
			require.False(t, conn.isTLS, "Expected no TLS for connection to %s", conn.address)
		}
	})

	// Test 2: Connect WITH TLS (will fail without certs, but we can track the attempt)
	t.Run("WithTLS", func(t *testing.T) {
		mu.Lock()
		connections = []connectionInfo{} // reset
		mu.Unlock()

		uri := fmt.Sprintf("mongodb://%s/?tls=true&tlsInsecure=true&directConnection=true", endpoint)
		clientOpts := mongov1options.Client().
			ApplyURI(uri).
			SetDialer(customDialer).
			SetHeartbeatInterval(1 * time.Second)

		client, err := mongov1.Connect(ctx, clientOpts)
		require.NoError(t, err, "Failed to connect with TLS")

		defer client.Disconnect(ctx)

		// Even if connection failed, we should have tracked the TLS attempt
		time.Sleep(500 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		require.Greater(t, len(connections), 0, "No connections tracked")

		for _, conn := range connections {
			require.True(t, conn.isTLS, "Expected TLS for connection to %s", conn.address)
		}
	})
}

func TestMGD_TLSHeartbeatRetryTiming(t *testing.T) {
	ctx := context.Background()

	// Start MongoDB container
	_, teardown := mongolocal.New(t, ctx, mongolocal.WithImage("mongo:6.0"))
	defer teardown(t)

	endpoint := "localhost:27017"

	// Track connection attempts and timing
	var mu sync.Mutex
	var attemptTimes []time.Time
	var attemptCount atomic.Int32

	// Custom dialer that always fails TLS handshake
	failingTLSDialer := &tlsFailingDialer{
		onAttempt: func() {
			mu.Lock()
			defer mu.Unlock()
			attemptTimes = append(attemptTimes, time.Now())
			attemptCount.Add(1)
			t.Logf("TLS connection attempt #%d at %v", len(attemptTimes), time.Now())
		},
	}

	// Configure client with very short heartbeat interval for faster test
	// and TLS enabled (which will fail)
	heartbeatInterval := 2 * time.Second
	uri := "mongodb://" + endpoint + "/?tls=true&tlsInsecure=true&directConnection=true&serverSelectionTimeoutMS=100"

	clientOpts := mongov1options.Client().
		ApplyURI(uri).
		SetDialer(failingTLSDialer).
		SetHeartbeatInterval(heartbeatInterval). // 2 seconds instead of default 10
		SetServerSelectionTimeout(100 * time.Millisecond)

	client, err := mongov1.Connect(ctx, clientOpts)
	if err != nil {
		t.Logf("Initial connect failed as expected: %v", err)
	}
	if client != nil {
		defer client.Disconnect(ctx)
	}

	// Wait for at least 3 connection attempts
	// This should take ~4 seconds (initial + 2s + 2s) if timing is correct
	// NOT ~1 second if it were using minHeartbeatFrequencyMS (500ms)
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for connection attempts")
		case <-ticker.C:
			if attemptCount.Load() >= 3 {
				goto analyze
			}
		}
	}

analyze:
	mu.Lock()
	defer mu.Unlock()

	if len(attemptTimes) < 3 {
		t.Fatalf("Expected at least 3 connection attempts, got %d", len(attemptTimes))
	}

	t.Logf("Got %d connection attempts", len(attemptTimes))

	// Filter out duplicate attempts that happen within 100ms (pool + monitor)
	// This deduplicates nearly-simultaneous connection attempts
	filteredTimes := []time.Time{attemptTimes[0]}
	for i := 1; i < len(attemptTimes); i++ {
		delay := attemptTimes[i].Sub(filteredTimes[len(filteredTimes)-1])
		if delay > 100*time.Millisecond {
			filteredTimes = append(filteredTimes, attemptTimes[i])
		}
	}

	t.Logf("After filtering duplicates: %d unique attempt groups", len(filteredTimes))

	if len(filteredTimes) < 2 {
		t.Fatalf("Expected at least 2 unique attempt groups after filtering, got %d", len(filteredTimes))
	}

	// Verify timing between filtered attempts
	// Should be close to heartbeatFrequencyMS (2s in our test), not minHeartbeatFrequencyMS (500ms)
	for i := 1; i < len(filteredTimes); i++ {
		delay := filteredTimes[i].Sub(filteredTimes[i-1])
		t.Logf("Delay between attempt group %d and %d: %v", i, i+1, delay)

		// Allow some tolerance, but verify it's closer to heartbeatFrequencyMS than minHeartbeatFrequencyMS
		// Should be ~2s, not ~500ms
		require.GreaterOrEqual(t, delay, 1500*time.Millisecond,
			"Delay between attempts %d and %d was %v, expected ~%v (heartbeatFrequencyMS), not ~500ms (minHeartbeatFrequencyMS)",
			i, i+1, delay, heartbeatInterval)
		require.LessOrEqual(t, delay, 3000*time.Millisecond,
			"Delay between attempts %d and %d was %v, expected ~%v",
			i, i+1, delay, heartbeatInterval)
		t.Logf("✓ Delay between attempts %d and %d correctly used heartbeatFrequencyMS (~%v)",
			i, i+1, heartbeatInterval)
	}

	t.Logf("SUCCESS: TLS failures correctly wait heartbeatFrequencyMS between retries, not minHeartbeatFrequencyMS")
}

// tlsFailingDialer always fails TLS handshakes to simulate TLS errors
type tlsFailingDialer struct {
	onAttempt func()
}

func (d *tlsFailingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.onAttempt != nil {
		d.onAttempt()
	}

	// Establish TCP connection but fail TLS immediately
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	// Return a connection that will fail TLS handshake
	return &tlsFailingConn{Conn: conn}, nil
}

// tlsFailingConn wraps net.Conn and fails on any TLS operations
type tlsFailingConn struct {
	net.Conn
}

func (c *tlsFailingConn) Read(b []byte) (n int, err error) {
	// Simulate TLS handshake failure
	return 0, &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: &tls.CertificateVerificationError{},
	}
}

func (c *tlsFailingConn) Write(b []byte) (n int, err error) {
	// Simulate TLS handshake failure
	return 0, &net.OpError{
		Op:  "write",
		Net: "tcp",
		Err: &tls.CertificateVerificationError{},
	}
}

func (c *tlsFailingConn) Handshake() error {
	// Fail TLS handshake immediately
	return &tls.CertificateVerificationError{
		UnverifiedCertificates: nil,
		Err:                    &net.OpError{Op: "handshake", Net: "tcp"},
	}
}

type connectionInfo struct {
	network string
	address string
	isTLS   bool
	time    time.Time
}

// tlsTrackingDialer wraps the default dialer and tracks TLS usage
type tlsTrackingDialer struct {
	onConnect func(network, address string, isTLS bool)
}

func (d *tlsTrackingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	wrapper := &tlsDetectorConn{
		Conn:      conn,
		onConnect: d.onConnect,
		network:   network,
		address:   address,
	}

	return wrapper, nil
}

// tlsDetectorConn wraps a net.Conn and detects when TLS handshake occurs
type tlsDetectorConn struct {
	net.Conn
	onConnect func(network, address string, isTLS bool)
	network   string
	address   string
	reported  bool
	mu        sync.Mutex
}

func (c *tlsDetectorConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if n > 0 {
		c.detectTLS(b[:n])
	}
	return n, err
}

func (c *tlsDetectorConn) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	if n > 0 {
		c.detectTLS(b[:n])
	}
	return n, err
}

func (c *tlsDetectorConn) detectTLS(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.reported || len(data) == 0 {
		return
	}

	// TLS handshake starts with:
	// - Byte 0: 0x16 (handshake record type)
	// - Byte 1-2: 0x03 0x01, 0x03 0x02, 0x03 0x03 (TLS versions)
	if len(data) >= 3 && data[0] == 0x16 && data[1] == 0x03 {
		c.reported = true
		if c.onConnect != nil {
			c.onConnect(c.network, c.address, true)
		}
		return
	}

	// MongoDB wire protocol message (no TLS)
	// First 4 bytes are message length in little-endian
	// Should be reasonable size (not starting with 0x16)
	if data[0] != 0x16 && len(data) >= 16 {
		c.reported = true
		if c.onConnect != nil {
			c.onConnect(c.network, c.address, false)
		}
	}
}

func (c *tlsDetectorConn) Handshake() error {
	// If this is called, we know it's a TLS connection
	if tlsConn, ok := c.Conn.(*tls.Conn); ok {
		return tlsConn.Handshake()
	}
	return nil
}
