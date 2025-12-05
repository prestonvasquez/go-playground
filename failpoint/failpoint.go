// Copyright (C) MongoDB, Inc. 2024-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package failpoint

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const (
	// ModeAlwaysOn is the fail point mode that enables the fail point for an
	// indefinite number of matching commands.
	ModeAlwaysOn = "alwaysOn"

	// ModeOff is the fail point mode that disables the fail point.
	ModeOff = "off"
)

// FailPoint is used to configure a server fail point. It is intended to be
// passed as the command argument to RunCommand.
//
// For more information about fail points, see
// https://github.com/mongodb/specifications/tree/HEAD/source/transactions/tests#server-fail-point
type FailPoint struct {
	ConfigureFailPoint string `bson:"configureFailPoint"`
	// Mode should be a string, FailPointMode, or map[string]any
	Mode any  `bson:"mode"`
	Data Data `bson:"data"`
}

// Mode configures when a fail point will be enabled. It is used to set the
// FailPoint.Mode field.
type Mode struct {
	Times int32 `bson:"times"`
	Skip  int32 `bson:"skip"`
}

// Data configures how a fail point will behave. It is used to set the
// FailPoint.Data field.
type Data struct {
	FailCommands                  []string           `bson:"failCommands,omitempty"`
	CloseConnection               bool               `bson:"closeConnection,omitempty"`
	ErrorCode                     int32              `bson:"errorCode,omitempty"`
	FailBeforeCommitExceptionCode int32              `bson:"failBeforeCommitExceptionCode,omitempty"`
	ErrorLabels                   *[]string          `bson:"errorLabels,omitempty"`
	WriteConcernError             *WriteConcernError `bson:"writeConcernError,omitempty"`
	BlockConnection               bool               `bson:"blockConnection,omitempty"`
	BlockTimeMS                   int32              `bson:"blockTimeMS,omitempty"`
	AppName                       string             `bson:"appName,omitempty"`
}

// WriteConcernError is the write concern error to return when the fail point is
// triggered. It is used to set the FailPoint.Data.WriteConcernError field.
type WriteConcernError struct {
	Code        int32     `bson:"code"`
	Name        string    `bson:"codeName"`
	Errmsg      string    `bson:"errmsg"`
	ErrorLabels *[]string `bson:"errorLabels,omitempty"`
	ErrInfo     bson.Raw  `bson:"errInfo,omitempty"`
}

type TeardownFunc func(t *testing.T)

// Enable sets a fail point for the client associated with T. Commands to
// create the failpoint will appear in command monitoring channels. The fail
// point will automatically be disabled after this test has run.
func Enable(t *testing.T, client *mongo.Client, fp FailPoint) TeardownFunc {
	t.Helper()

	if modeMap, ok := fp.Mode.(map[string]any); ok {
		var key string
		var err error

		if times, ok := modeMap["times"]; ok {
			key = "times"
			modeMap["times"], err = interfaceToInt32(times)
		}
		if skip, ok := modeMap["skip"]; ok {
			key = "skip"
			modeMap["skip"], err = interfaceToInt32(skip)
		}

		require.NoError(t, err, "failed to convert failpoint mode %q to int32", key)
	}

	admin := client.Database("admin")
	require.NoError(t, admin.RunCommand(context.Background(), fp).Err(), "error enabling failpoint")

	return func(t *testing.T) {
		db := client.Database("admin")
		cmd := FailPoint{
			ConfigureFailPoint: fp.ConfigureFailPoint,
			Mode:               ModeOff,
		}

		require.NoError(t, db.RunCommand(context.Background(), cmd).Err())
	}
}

func interfaceToInt32(i any) (int32, error) {
	switch conv := i.(type) {
	case int:
		return int32(conv), nil
	case int32:
		return conv, nil
	case int64:
		return int32(conv), nil
	case float64:
		return int32(conv), nil
	}

	return 0, fmt.Errorf("type %T cannot be converted to int32", i)
}

// NewSingleErr creates a FailPoint that will cause the specified command to
// Fail once with the given error code.
func NewSingleErr(cmdName string, errCode int32) FailPoint {
	return FailPoint{
		ConfigureFailPoint: "failCommand",
		Mode: Mode{
			Times: 1,
		},
		Data: Data{
			FailCommands: []string{cmdName},
			ErrorCode:    errCode,
		},
	}
}
