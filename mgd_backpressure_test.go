// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package goplayground

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/prestonvasquez/go-playground/monitor"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver"
)

// TestMGD_Backpressure_RetriesOnOverload illustrates GODRIVER-3658 / 3844:
// when a command returns SystemOverloadedError + RetryableError, the driver
// retries with exponential backoff (BASE 100ms, doubling, capped at 10s,
// jittered). Default maxAdaptiveRetries=2 gives 1 original attempt + 2
// retries = 3 total tries; a fail-twice failpoint is exhausted on retry 2,
// so the third attempt succeeds.
//
// Code 462 is NOT in retryableCodes and "RetryableError" is distinct from
// "RetryableWriteError", so a pre-backpressure driver would not retry this
// error at all — this test exercises behavior introduced by the feature.
func TestMGD_Backpressure_RetriesOnOverload(t *testing.T) {
	mon := monitor.New(t, false, "find")

	clientOpts := options.Client().SetMonitor(mon.CommandMonitor)

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(clientOpts))
	defer teardown(t)

	fpTeardown := failpoint.Enable(t, client, failpoint.NewOverloadErr("find", 2))
	defer fpTeardown(t)

	start := time.Now()
	err := mongolocal.ArbColl(client).FindOne(context.Background(), bson.D{}).Err()
	elapsed := time.Since(start)

	// FindOne on an empty collection returns ErrNoDocuments — that's the
	// success path here. What we're asserting is that the failpoint's
	// overload error did not propagate; the driver retried past it.
	if err != nil {
		require.ErrorIs(t, err, mongo.ErrNoDocuments,
			"find should retry past the overload errors, got: %v", err)
	}

	starts := mon.CommandStartedEvents()
	require.Len(t, starts, 3, "expected 1 original find + 2 retries, got %d", len(starts))

	t.Logf("succeeded after %v across %d find attempts (BASE_BACKOFF=100ms with [0,1) jitter, doubling)",
		elapsed, len(starts))
}

// TestMGD_Backpressure_MaxAdaptiveRetriesGoverns is the same scenario as the
// previous test but with maxAdaptiveRetries=1 instead of the default 2. With
// only one retry available, the fail-twice failpoint is not exhausted before
// the budget is and the operation fails with the original overload error.
func TestMGD_Backpressure_MaxAdaptiveRetriesGoverns(t *testing.T) {
	mon := monitor.New(t, false, "find")

	clientOpts := options.Client().
		SetMonitor(mon.CommandMonitor).
		SetMaxAdaptiveRetries(1)

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithReplicaSet("rs0"),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(clientOpts))
	defer teardown(t)

	fpTeardown := failpoint.Enable(t, client, failpoint.NewOverloadErr("find", 2))
	defer fpTeardown(t)

	err := mongolocal.ArbColl(client).FindOne(context.Background(), bson.D{}).Err()

	require.Error(t, err, "find should fail when retry budget is exhausted before failpoint")

	var srvErr mongo.ServerError
	require.True(t, errors.As(err, &srvErr), "expected ServerError, got %T: %v", err, err)
	require.True(t, srvErr.HasErrorCode(int(failpoint.OverloadErrorCode)),
		"expected error %d, got %v", failpoint.OverloadErrorCode, srvErr.ErrorCodes())

	starts := mon.CommandStartedEvents()
	require.Len(t, starts, 2, "expected 1 original find + 1 retry, got %d", len(starts))
}

// switchableDialer delegates to a real net.Dialer until failing is set,
// then returns the injected error for every subsequent call. The flag
// lets the test let setup (mongolocal startup, Ping-readiness loop, the
// initial heartbeat) succeed before forcing the operation-side dial to
// fail.
type switchableDialer struct {
	real     net.Dialer
	failing  atomic.Bool
	n        atomic.Int32
	injected error
}

func (d *switchableDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.n.Add(1)
	if d.failing.Load() {
		return nil, d.injected
	}
	return d.real.DialContext(ctx, network, addr)
}

// TestMGD_Backpressure_TLSProtocolErrorMustNotBeLabeled asserts the
// CMAP contract that non-I/O TLS errors MUST NOT carry backpressure
// labels:
//
//	"For errors that the driver can distinguish as never occurring
//	 due to server overload, such as DNS lookup failures, non-I/O TLS
//	 errors (e.g., certificate validation or hostname-mismatch
//	 failures), or errors encountered while establishing a connection
//	 to a SOCKS5 proxy, the driver MUST NOT add backpressure error
//	 labels for these error types."
//
// The "e.g." opens the "non-I/O TLS errors" category beyond cert
// validation. Protocol-level TLS errors like tls.RecordHeaderError
// (peer sent something that isn't TLS) and tls.AlertError (peer sent
// a fatal alert) are also non-I/O TLS errors and must be excluded.
//
// Today the Go driver's deny-list in topology.wrapConnectionError
// (added in GODRIVER-3646) catches only specific x509 cert types and
// labels everything else — including tls.RecordHeaderError — with
// SystemOverloadedError + RetryableError + NetworkError. This test
// therefore FAILS today, demonstrating the bug. After the deny-list
// is extended to exclude tls.RecordHeaderError and tls.AlertError,
// the test will pass and serve as a regression guard.
//
// The test injects a tls.RecordHeaderError at the dialer layer for
// the operation's pool-checkout dial (after the heartbeat has
// succeeded and the server is selectable). The injected error flows
// through wrapConnectionError; the resulting label is observable on
// the driver.Error in the user-visible error chain.
func TestMGD_Backpressure_TLSProtocolErrorMustNotBeLabeled(t *testing.T) {
	dialer := &switchableDialer{
		injected: tls.RecordHeaderError{Msg: "test-injected non-I/O TLS error"},
	}

	clientOpts := options.Client().
		SetDialer(dialer).
		SetMaxConnIdleTime(1 * time.Millisecond). // expire pool conns aggressively
		SetRetryReads(false).                     // avoid retries that would mask the bug
		SetRetryWrites(false)

	client, teardown := mongolocal.New(t, context.Background(),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(clientOpts))
	defer teardown(t)

	// Setup is done; flip the dialer into failure mode. Subsequent dials
	// (which the next operation will trigger when it pulls a fresh pool
	// connection) get the injected tls.RecordHeaderError.
	t.Logf("dials during setup: %d", dialer.n.Load())
	dialer.failing.Store(true)

	// Wait long enough for any pooled connection from setup to be considered
	// stale. The pool's checkout will discard stale conns and dial a new one.
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pingErr := client.Database("admin").RunCommand(ctx, bson.D{{"ping", 1}}).Err()
	require.Error(t, pingErr, "expected ping to fail; dialer should have rejected new conn")

	t.Logf("dialer call count: %d", dialer.n.Load())
	t.Logf("ping error chain:")
	for cur := pingErr; cur != nil; cur = errors.Unwrap(cur) {
		t.Logf("  %T: %v", cur, cur)
	}

	var de driver.Error
	require.True(t, errors.As(pingErr, &de),
		"expected driver.Error in chain; got %v", pingErr)

	// Spec contract: a non-I/O TLS error must NOT be labeled
	// SystemOverloadedError. This assertion fails today (proving the
	// bug) and will pass once wrapConnectionError's deny-list is
	// extended to exclude tls.RecordHeaderError / tls.AlertError.
	require.False(t, de.HasErrorLabel(driver.ErrSystemOverloadedError),
		"non-I/O TLS error must not carry SystemOverloadedError label per CMAP; got labels=%v",
		de.Labels)
}

// TestMGD_Backpressure_OverloadRetargetingDeprioritizes demonstrates that
// with EnableOverloadRetargeting=true on a replica set, an overload failpoint
// scoped to a single secondary causes retries to land on a different host;
// with the flag off, retries land on the same host.
//
// TODO: implement.
func TestMGD_Backpressure_OverloadRetargetingDeprioritizes(t *testing.T) {
	t.Skip("not implemented")
}
