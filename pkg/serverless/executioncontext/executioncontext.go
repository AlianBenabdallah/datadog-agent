// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package executioncontext

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

const persistedStateFilePath = "/tmp/dd-lambda-extension-cache.json"

// ExecutionContext represents the execution context
type ExecutionContext struct {
	m                  sync.Mutex
	arn                string
	lastRequestID      string
	coldstartRequestID string
	lastLogRequestID   string
	coldstart          bool
	coldstartDuration  float64
	startTime          time.Time
	endTime            time.Time
}

// State represents the state of the execution context at a point in time
type State struct {
	ARN                string
	LastRequestID      string
	ColdstartRequestID string
	LastLogRequestID   string
	Coldstart          bool
	ColdstartDuration  float64
	StartTime          time.Time
	EndTime            time.Time
}

// GetCurrentState gets the current state of the execution context
func (ec *ExecutionContext) GetCurrentState() State {
	ec.m.Lock()
	defer ec.m.Unlock()
	return State{
		ARN:                ec.arn,
		LastRequestID:      ec.lastRequestID,
		ColdstartRequestID: ec.coldstartRequestID,
		LastLogRequestID:   ec.lastLogRequestID,
		Coldstart:          ec.coldstart,
		ColdstartDuration:  ec.coldstartDuration,
		StartTime:          ec.startTime,
		EndTime:            ec.endTime,
	}
}

func (ec *ExecutionContext) SetColdStartDuration(duration float64) {
	ec.m.Lock()
	defer ec.m.Unlock()
	ec.coldstartDuration = duration
}

// SetFromInvocation sets the execution context based on an invocation
func (ec *ExecutionContext) SetFromInvocation(arn string, requestID string) {
	ec.m.Lock()
	defer ec.m.Unlock()
	ec.arn = strings.ToLower(arn)
	ec.lastRequestID = requestID
	if len(ec.coldstartRequestID) == 0 {
		ec.coldstart = true
		ec.coldstartRequestID = requestID
	} else {
		ec.coldstart = false
	}
}

// UpdateStartTime updates the execution context based on a platform.Start log message
func (ec *ExecutionContext) UpdateStartTime(time time.Time) {
	ec.m.Lock()
	defer ec.m.Unlock()
	ec.startTime = time
}

// UpdateEndTime updates the execution context based on a
// platform.runtimeDone log message
func (ec *ExecutionContext) UpdateEndTime(time time.Time) {
	ec.m.Lock()
	defer ec.m.Unlock()
	ec.endTime = time
}

// SaveCurrentExecutionContext stores the current context to a file
func (ec *ExecutionContext) SaveCurrentExecutionContext() error {
	ecs := ec.GetCurrentState()
	file, err := json.Marshal(ecs)
	if err != nil {
		return err
	}
	err = os.WriteFile(persistedStateFilePath, file, 0600)
	if err != nil {
		return err
	}
	return nil
}

// RestoreCurrentStateFromFile loads the current context from a file
func (ec *ExecutionContext) RestoreCurrentStateFromFile() error {
	ec.m.Lock()
	defer ec.m.Unlock()
	file, err := os.ReadFile(persistedStateFilePath)
	if err != nil {
		return err
	}
	var restoredExecutionContextState State
	err = json.Unmarshal(file, &restoredExecutionContextState)
	if err != nil {
		return err
	}
	ec.arn = restoredExecutionContextState.ARN
	ec.lastRequestID = restoredExecutionContextState.LastRequestID
	ec.lastLogRequestID = restoredExecutionContextState.LastLogRequestID
	ec.coldstartRequestID = restoredExecutionContextState.ColdstartRequestID
	ec.startTime = restoredExecutionContextState.StartTime
	ec.endTime = restoredExecutionContextState.EndTime
	return nil
}
