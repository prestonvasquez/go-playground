// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package goplayground

import (
	"context"
	"testing"
	"time"

	"github.com/prestonvasquez/go-playground/failpoint"
	"github.com/prestonvasquez/go-playground/mongolocal"
	"github.com/prestonvasquez/go-playground/monitor"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestMGD_SDAM_MonitorTimeout_InterruptsInUseCursor reproduces the customer
// scenario behind GODRIVER-3919 / the SDAM interruptInUseConnections debate.
//
// A server monitor network timeout clears the pool with
// interruptInUseConnections=true, which closes the in-use connection holding a
// healthy long-lived tailable getMore — killing the cursor (and the driver does
// not retry it).
//
// In Go, interruptInUseConnections=true is reached only via pool.clearAll, and
// only on the SECOND consecutive monitor timeout (server.go: timeoutCnt>0). The
// first timeout is retried (GODRIVER-2577). So we block hello/isMaster twice,
// longer than connectTimeout, to drive two consecutive heartbeat timeouts.
//
// Observable signal: a PoolCleared event with Interruption=true (the
// PoolEvent.Interruption field is json-tagged "interruptInUseConnections"),
// coincident with the cursor erroring out.
func TestMGD_SDAM_MonitorTimeout_InterruptsInUseCursor(t *testing.T) {
	const appName = "sdam-interrupt-repro"

	mon := monitor.New(t, true, "getMore", "hello", "isMaster")

	clientOpts := options.Client().
		SetAppName(appName).
		SetServerMonitoringMode("poll"). // predictable heartbeat timeout (no streaming await)
		SetHeartbeatInterval(500 * time.Millisecond).
		SetConnectTimeout(2 * time.Second).
		SetMonitor(mon.CommandMonitor).
		SetPoolMonitor(mon.PoolMonitor)

	client, teardown := mongolocal.StartT(t, context.Background(),
		mongolocal.WithEnableTestCommands(),
		mongolocal.WithMongoClientOptions(clientOpts),
	)
	defer teardown(t)

	ctx := context.Background()
	db := client.Database("db")

	// Capped collection to tail.
	_ = db.Collection("tail").Drop(ctx)
	require.NoError(t, db.CreateCollection(ctx, "tail",
		options.CreateCollection().SetCapped(true).SetSizeInBytes(100_000)),
		"create capped collection")

	coll := db.Collection("tail")
	_, err := coll.InsertOne(ctx, bson.D{{Key: "_id", Value: 0}})
	require.NoError(t, err, "seed insert")

	// Open a tailable+awaitData cursor and consume the seed doc so the next
	// iteration blocks server-side in getMore (connection in use).
	findOpts := options.Find().
		SetCursorType(options.TailableAwait).
		SetMaxAwaitTime(60 * time.Second).
		SetBatchSize(1)
	cur, err := coll.Find(ctx, bson.D{}, findOpts)
	require.NoError(t, err, "open tailable cursor")
	defer cur.Close(ctx)

	require.True(t, cur.Next(ctx), "expected to read seed doc; err=%v", cur.Err())

	// Iterate in the background; this Next blocks in getMore until interrupted.
	cursorDone := make(chan error, 1)
	go func() {
		if cur.Next(context.Background()) {
			cursorDone <- nil // got a doc (not expected — nothing else is inserted)
			return
		}
		cursorDone <- cur.Err()
	}()

	// Let the cursor settle into a blocking getMore and the first heartbeat
	// succeed before arming the failpoint.
	time.Sleep(1 * time.Second)

	// Block the monitor's hello/isMaster longer than connectTimeout, twice, so
	// the monitor observes two consecutive timeouts -> clearAll (interrupt
	// in-use). getMore is NOT in failCommands, so the cursor is killed by the
	// pool clear, not by the failpoint directly.
	fp := failpoint.FailPoint{
		ConfigureFailPoint: "failCommand",
		Mode:               failpoint.Mode{Times: 2},
		Data: failpoint.Data{
			FailCommands:    []string{"hello", "isMaster"},
			AppName:         appName,
			BlockConnection: true,
			BlockTimeMS:     5000, // > connectTimeout (2s)
		},
	}
	fpTeardown := failpoint.Enable(t, client, fp)
	defer fpTeardown(t)

	// Wait for the cursor to be interrupted.
	var cursorErr error
	select {
	case cursorErr = <-cursorDone:
		t.Logf("cursor Next returned: %v", cursorErr)
	case <-time.After(20 * time.Second):
		t.Fatal("cursor was not interrupted within 20s")
	}

	// The point: the pool was cleared with interruptInUseConnections=true, and
	// that is what killed the in-use cursor.
	cleared := mon.PoolClearedEvents()
	t.Logf("pool cleared events: %d", len(cleared))
	sawInterrupt := false
	for _, pe := range cleared {
		t.Logf("  pool cleared: interruptInUseConnections=%t", pe.Interruption)
		if pe.Interruption {
			sawInterrupt = true
		}
	}

	require.True(t, sawInterrupt,
		"expected a pool clear with interruptInUseConnections=true")
	require.Error(t, cursorErr,
		"expected the in-use tailable getMore to be interrupted")
}
