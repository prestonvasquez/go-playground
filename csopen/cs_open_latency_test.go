// Copyright (C) MongoDB, Inc. 2025-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package csopen

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestChangeStreamOpenLatencySharded illustrates that opening a whole-cluster
// change stream with the DEFAULT API (client.Watch, no start time) on a sharded
// cluster blocks until the config server's cluster time advances past the mongos
// cluster time (bounded by periodicNoopIntervalSecs), while passing a past
// startAtOperationTime returns immediately.
//
// Both opens run back-to-back at the same instant, so the only variable is the
// start time. Run against a sharded cluster: MONGODB_URI=mongodb://localhost:27017
//
//	go test ./internal/integration/csproof/ -run TestChangeStreamOpenLatencySharded -v
func TestChangeStreamOpenLatencySharded(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	ctx := context.Background()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	// Require a sharded cluster (the delay only exists when there is a config server).
	var hello struct {
		Msg string `bson:"msg"`
	}
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if hello.Msg != "isdbgrid" {
		t.Skipf("requires a sharded cluster (mongos); msg=%q", hello.Msg)
	}

	// Capture a cluster time, then let it fall into the past.
	raw, err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Raw()
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	tt, ii := raw.Lookup("$clusterTime", "clusterTime").Timestamp()
	past := bson.Timestamp{T: tt, I: ii}
	time.Sleep(1 * time.Second)

	// openMS times only the open (client.Watch runs the aggregate and returns the
	// first, empty batch; getMore is not called until ChangeStream.Next).
	openMS := func(opts *options.ChangeStreamOptionsBuilder) int64 {
		start := time.Now()
		cs, err := client.Watch(ctx, mongo.Pipeline{}, opts)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			t.Logf("watch error: %v", err)
			return ms
		}
		_ = cs.Close(ctx)
		return ms
	}

	t.Log("whole-cluster change stream open latency on sharded (default Watch vs past start time):")
	for i := 0; i < 6; i++ {
		noStart := openMS(options.ChangeStream())
		pastStart := openMS(options.ChangeStream().SetStartAtOperationTime(&past))
		t.Logf("#%d: noStart=%4dms   pastStart=%4dms", i, noStart, pastStart)
		time.Sleep(250 * time.Millisecond)
	}
}
