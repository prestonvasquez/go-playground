// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package goplayground

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/prestonvasquez/go-playground/monitor"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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

// TestMGD_Backpressure_OverloadRetargetingDeprioritizes demonstrates that
// with EnableOverloadRetargeting=true on a replica set, an overload failpoint
// scoped to a single secondary causes retries to land on a different host;
// with the flag off, retries land on the same host.
//
// TODO: implement.
func TestMGD_Backpressure_OverloadRetargetingDeprioritizes(t *testing.T) {
	t.Skip("not implemented")
}
