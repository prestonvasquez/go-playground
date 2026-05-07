// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package goplayground

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests illustrate the technique GODRIVER-3603 enables: a centralized
// bufio.Reader on topology.connection lets the driver Peek at the wire
// stream to perform a liveness check without consuming bytes that the
// protocol parser will need.
//
// Compare against the alternative, where a "read 1 byte to check the
// connection is alive" approach corrupts the protocol stream.

// pipePair returns a connected (server, client) pair over a TCP loopback
// listener. TCP gives realistic Close semantics: server.Close() only
// signals the peer; the client side stays usable for SetReadDeadline,
// Peek, etc., until it sees EOF. (net.Pipe's bidirectional-close
// behavior breaks the peer-closed test case.)
func pipePair(t *testing.T) (server, client net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := listener.Accept()
		accepted <- c
	}()

	client, err = net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)

	select {
	case server = <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout accepting connection")
	}
	require.NotNil(t, server)

	t.Cleanup(func() {
		listener.Close()
		server.Close()
		client.Close()
	})
	return server, client
}

// TestPeek_NonDestructive shows that bufio.Reader.Peek does not advance
// the reader. A subsequent ReadFull observes the same bytes Peek returned,
// which is the property that lets a connection wrapper run a pre-flight
// liveness check without disturbing the protocol parser downstream.
func TestPeek_NonDestructive(t *testing.T) {
	server, client := pipePair(t)

	// Server writes a typical wire-frame: 4-byte little-endian size header
	// followed by a body of that many bytes.
	payload := []byte("hello, world!")
	sizeHeader := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeHeader, uint32(len(payload)))

	go func() {
		_, _ = server.Write(sizeHeader)
		_, _ = server.Write(payload)
	}()

	br := bufio.NewReader(client)

	// Peek the first byte. This reads from the underlying conn into bufio's
	// internal buffer, but does NOT advance the reader.
	peeked, err := br.Peek(1)
	require.NoError(t, err)
	require.Equal(t, sizeHeader[0], peeked[0])

	// The protocol parser proceeds normally. The size header is still there.
	var sz uint32
	require.NoError(t, binary.Read(br, binary.LittleEndian, &sz),
		"size header preserved across Peek")
	require.Equal(t, uint32(len(payload)), sz)

	body := make([]byte, sz)
	_, err = io.ReadFull(br, body)
	require.NoError(t, err, "body intact after Peek")
	require.Equal(t, payload, body)
}

// TestPeek_DetectsServerClose shows that Peek surfaces a peer-side close
// as io.EOF immediately. This is the case the change-stream wrapper
// currently misses: the connection has died silently (peer closed,
// network dropped, server timed out) and Closed() still reports false.
func TestPeek_DetectsServerClose(t *testing.T) {
	server, client := pipePair(t)

	// Peer closes without writing — the silent-death case.
	server.Close()

	br := bufio.NewReader(client)
	_, err := br.Peek(1)
	require.True(t, errors.Is(err, io.EOF), "expected EOF, got %v", err)
}

// TestPeek_DetectsSelfClose shows that calling Close on the local side
// of the connection produces net.ErrClosed (not io.EOF) on subsequent
// Peek. The asymmetry matters for diagnostics: io.EOF means the peer
// hung up, net.ErrClosed means our own code already closed this side.
func TestPeek_DetectsSelfClose(t *testing.T) {
	_, client := pipePair(t)

	// Local side closes.
	client.Close()

	br := bufio.NewReader(client)
	_, err := br.Peek(1)
	require.True(t, errors.Is(err, net.ErrClosed),
		"expected net.ErrClosed on self-close, got %v", err)
	require.False(t, errors.Is(err, io.EOF),
		"self-close should NOT surface as io.EOF")
}

// TestPeek_DistinguishesIdleFromClosed shows the full liveness-probe
// pattern: a Peek under a tiny read deadline returns
// os.ErrDeadlineExceeded for an alive-but-idle connection and io.EOF for
// a peer-closed connection. In neither case is a protocol byte consumed.
func TestPeek_DistinguishesIdleFromClosed(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		_, client := pipePair(t)

		br := bufio.NewReader(client)
		require.NoError(t, client.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
		_, err := br.Peek(1)
		require.NoError(t, client.SetReadDeadline(time.Time{})) // restore

		require.True(t, errors.Is(err, os.ErrDeadlineExceeded),
			"alive-but-idle should produce a deadline error, got %v", err)
	})

	t.Run("peer-closed", func(t *testing.T) {
		server, client := pipePair(t)
		server.Close()

		br := bufio.NewReader(client)
		require.NoError(t, client.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
		_, err := br.Peek(1)
		require.NoError(t, client.SetReadDeadline(time.Time{}))

		require.True(t, errors.Is(err, io.EOF), "peer-closed should produce EOF, got %v", err)
	})
}

// TestPeek_VsRawRead_Corruption contrasts the peek approach against the
// naive "read 1 byte to check liveness" approach. The raw read consumes
// a byte from the protocol stream. The downstream parser is then short
// one byte and produces wrong output: exactly the failure mode the
// GODRIVER-3603 description warns about: "no way to do that without
// discarding bytes from the TCP buffer, which can cause problems with
// subsequently reading the size."
func TestPeek_VsRawRead_Corruption(t *testing.T) {
	server, client := pipePair(t)

	payload := []byte("hello")
	go func() {
		_, _ = server.Write(payload)
		_ = server.Close()
	}()

	// Raw-read approach: consume a byte to test that the connection has data.
	consumed := make([]byte, 1)
	_, err := client.Read(consumed)
	require.NoError(t, err)
	require.Equal(t, payload[0], consumed[0]) // got 'h'

	// Subsequent read by the "protocol parser" — only sees "ello".
	rest, err := io.ReadAll(client)
	require.NoError(t, err)
	require.NotEqual(t, payload, rest, "raw read corrupted the stream")
	require.Equal(t, payload[1:], rest, "first byte was lost to the liveness check")
}
