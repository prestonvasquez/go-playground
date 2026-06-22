// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package goplayground

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mitm is a tiny TCP man-in-the-middle that models a stateful middlebox
// (load balancer / NAT / firewall). It transparently forwards bytes for each
// connection. blackholeExisting() simulates a middlebox failover/flush: it
// drops the *server* side of every currently-open pair while leaving the
// *client* side open and silent — so an in-flight client read on those
// connections hangs (no data, no RST), exactly like a black hole. New
// connections opened afterward are forwarded normally.
type mitm struct {
	ln      net.Listener
	backend string

	mu    sync.Mutex
	pairs []*mitmPair
}

type mitmPair struct {
	client     net.Conn
	backend    net.Conn
	blackholed atomic.Bool
}

func newMITM(t *testing.T, backend string) *mitm {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "mitm listen")
	m := &mitm{ln: ln, backend: backend}
	go m.serve()
	return m
}

func (m *mitm) addr() string { return m.ln.Addr().String() }

func (m *mitm) serve() {
	for {
		c, err := m.ln.Accept()
		if err != nil {
			return
		}
		b, err := net.Dial("tcp", m.backend)
		if err != nil {
			_ = c.Close()
			continue
		}
		p := &mitmPair{client: c, backend: b}
		m.mu.Lock()
		m.pairs = append(m.pairs, p)
		m.mu.Unlock()
		go m.pump(p, p.backend, p.client) // client -> backend
		go m.pump(p, p.client, p.backend) // backend -> client
	}
}

func (m *mitm) pump(p *mitmPair, dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	// On normal EOF/error, tear the pair down. If this pair was black-holed,
	// deliberately leave the client side open and silent so its in-flight read
	// hangs.
	if !p.blackholed.Load() {
		_ = p.client.Close()
		_ = p.backend.Close()
	}
}

// blackholeExisting drops the server side of every currently-open pair (so the
// client side goes silent but stays open) and stops tracking them. Connections
// opened after this call are forwarded normally — modeling a middlebox that
// forgets existing connections but accepts new ones.
func (m *mitm) blackholeExisting() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pairs {
		p.blackholed.Store(true)
		_ = p.backend.Close() // client side intentionally left open + silent
	}
	m.pairs = nil
}

func (m *mitm) close() {
	_ = m.ln.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pairs {
		_ = p.client.Close()
		_ = p.backend.Close()
	}
}

// hostPortFromURI extracts host:port from a "mongodb://host:port/..." URI.
func hostPortFromURI(uri string) string {
	s := strings.TrimPrefix(uri, "mongodb://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	return s
}

// TestMGD_SDAM_Blackhole_RetrySucceedsButCursorHangs demonstrates the
// pathological case for "confirm the monitor with a retry before interrupting"
// (Go's GODRIVER-2577 deviation).
//
// A middlebox failover black-holes all existing connections (server side
// dropped, client side left open + silent) while new connections still work.
// The monitor's heartbeat times out, but its retry opens a *fresh* connection
// that succeeds — so the driver concludes the server is healthy and does NOT
// clear/interrupt. The cursor's pre-existing connection, however, is still
// black-holed, so its in-flight getMore hangs (no interrupt, no fast failure).
//
// This is the cost of confirmation: a healthy-looking retry leaves a genuinely
// dead in-use connection stranded. Spec-style immediate interrupt-on-timeout
// would have aborted the getMore on the first timeout instead.
func TestMGD_SDAM_Blackhole_RetrySucceedsButCursorHangs(t *testing.T) {
	const appName = "sdam-blackhole-repro"

	// Real mongod (we talk to it through the proxy).
	_, teardown, env := mongolocal.StartTWithEnv(t, context.Background(),
		mongolocal.WithEnableTestCommands(),
	)
	defer teardown(t)

	backend := hostPortFromURI(env.ConnectionString())
	proxy := newMITM(t, backend)
	defer proxy.close()

	// Observe pool clears and server heartbeats.
	var clearedInterrupting atomic.Int32
	poolMon := &event.PoolMonitor{
		Event: func(pe *event.PoolEvent) {
			if pe.Type == event.ConnectionPoolCleared {
				t.Logf("pool cleared: interruptInUseConnections=%t", pe.Interruption)
				if pe.Interruption {
					clearedInterrupting.Add(1)
				}
			}
		},
	}

	var hbSucceededAfterBlackhole atomic.Int32
	var blackholeAt atomic.Int64 // unix nanos; 0 until set
	srvMon := &event.ServerMonitor{
		ServerHeartbeatSucceeded: func(*event.ServerHeartbeatSucceededEvent) {
			if t0 := blackholeAt.Load(); t0 != 0 {
				hbSucceededAfterBlackhole.Add(1)
			}
		},
		ServerHeartbeatFailed: func(e *event.ServerHeartbeatFailedEvent) {
			t.Logf("heartbeat failed: %v", e.Failure)
		},
	}

	// Connect THROUGH the proxy, direct connection so the client only ever
	// talks to the proxy address (never the mongod address from hello).
	clientOpts := options.Client().
		ApplyURI("mongodb://" + proxy.addr()).
		SetDirect(true).
		SetAppName(appName).
		SetServerMonitoringMode("poll").
		SetHeartbeatInterval(500 * time.Millisecond).
		SetConnectTimeout(2 * time.Second).
		SetPoolMonitor(poolMon).
		SetServerMonitor(srvMon)

	client, err := mongo.Connect(clientOpts)
	require.NoError(t, err, "connect through proxy")
	defer func() { _ = client.Disconnect(context.Background()) }()

	ctx := context.Background()
	db := client.Database("db")

	_ = db.Collection("tail").Drop(ctx)
	require.NoError(t, db.CreateCollection(ctx, "tail",
		options.CreateCollection().SetCapped(true).SetSizeInBytes(100_000)),
		"create capped collection")

	coll := db.Collection("tail")
	_, err = coll.InsertOne(ctx, bson.D{{Key: "_id", Value: 0}})
	require.NoError(t, err, "seed insert")

	// Long maxAwaitTime so the getMore's read deadline is far out — the hang is
	// observable within our window rather than self-timing-out quickly.
	findOpts := options.Find().
		SetCursorType(options.TailableAwait).
		SetMaxAwaitTime(5 * time.Minute).
		SetBatchSize(1)
	cur, err := coll.Find(ctx, bson.D{}, findOpts)
	require.NoError(t, err, "open tailable cursor")
	defer cur.Close(ctx)

	require.True(t, cur.Next(ctx), "read seed doc; err=%v", cur.Err())

	cursorDone := make(chan error, 1)
	go func() {
		if cur.Next(context.Background()) {
			cursorDone <- nil
			return
		}
		cursorDone <- cur.Err()
	}()

	// Let the cursor settle into a blocking getMore and a heartbeat succeed.
	time.Sleep(1 * time.Second)

	// Middlebox failover: black-hole all existing connections (cursor + monitor),
	// keep accepting new ones.
	t.Log("black-holing existing connections")
	blackholeAt.Store(time.Now().UnixNano())
	proxy.blackholeExisting()

	// Give the monitor time to time out, retry on a fresh connection, and
	// recover — and give the cursor a chance to (not) return.
	select {
	case err := <-cursorDone:
		t.Fatalf("cursor returned (expected it to hang): %v", err)
	case <-time.After(15 * time.Second):
		t.Log("cursor still hung after 15s (expected)")
	}

	// The monitor recovered via a fresh connection...
	require.Greater(t, hbSucceededAfterBlackhole.Load(), int32(0),
		"expected the monitor to recover (a heartbeat to succeed) after the blackhole")
	// ...so the driver never interrupted the in-use cursor connection.
	require.Equal(t, int32(0), clearedInterrupting.Load(),
		"expected NO interrupting pool clear (confirmation suppressed it)")
}
