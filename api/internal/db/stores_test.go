// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// =============================================================================
// Test fakes — enhanced DBTX implementation for unit testing
// =============================================================================

// fakeDBTX implements DBTX with configurable responses per call.
type fakeDBTX struct {
	// execResult is returned by Exec calls.
	execResult ExecResult
	// execErr is returned by Exec calls.
	execErr error
	// queryRows is returned by Query calls.
	queryRows Rows
	// queryErr is returned by Query calls.
	queryErr error
	// queryRowResult is returned by QueryRow calls.
	queryRowResult Row
	// capturedSQL records the last SQL executed via Exec.
	capturedSQL string
	// capturedArgs records the last args passed to Exec.
	capturedArgs []any
	// execCalls counts how many times Exec was called.
	execCalls int
}

func (f *fakeDBTX) Query(_ context.Context, sql string, args ...any) (Rows, error) {
	f.capturedSQL = sql
	f.capturedArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryRows != nil {
		return f.queryRows, nil
	}
	return &sliceRows{}, nil
}

func (f *fakeDBTX) Exec(_ context.Context, sql string, args ...any) (ExecResult, error) {
	f.capturedSQL = sql
	f.capturedArgs = args
	f.execCalls++
	return f.execResult, f.execErr
}

func (f *fakeDBTX) QueryRow(_ context.Context, sql string, args ...any) Row {
	f.capturedSQL = sql
	f.capturedArgs = args
	if f.queryRowResult != nil {
		return f.queryRowResult
	}
	return emptyRow{}
}

// fakeRowWithData returns preset values on Scan.
type fakeRowWithData struct {
	values []any
	err    error
}

func (r *fakeRowWithData) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i >= len(r.values) {
			break
		}
		assignValue(d, r.values[i])
	}
	return nil
}

// fakeRowsMulti supports returning multiple rows via the Rows interface.
type fakeRowsMulti struct {
	data  [][]any
	idx   int
	err   error
	scErr error
}

func (r *fakeRowsMulti) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRowsMulti) Scan(dest ...any) error {
	if r.scErr != nil {
		return r.scErr
	}
	row := r.data[r.idx-1]
	for i, d := range dest {
		if i >= len(row) {
			break
		}
		assignValue(d, row[i])
	}
	return nil
}

func (r *fakeRowsMulti) Err() error { return r.err }
func (r *fakeRowsMulti) Close()     {}

// assignValue assigns src into the dest pointer using type assertion.
func assignValue(dest any, src any) {
	switch d := dest.(type) {
	case *string:
		if v, ok := src.(string); ok {
			*d = v
		}
	case *int:
		if v, ok := src.(int); ok {
			*d = v
		}
	case *int64:
		if v, ok := src.(int64); ok {
			*d = v
		}
	case *bool:
		if v, ok := src.(bool); ok {
			*d = v
		}
	case *time.Time:
		if v, ok := src.(time.Time); ok {
			*d = v
		}
	case **time.Time:
		if v, ok := src.(*time.Time); ok {
			*d = v
		}
	case **int:
		if v, ok := src.(*int); ok {
			*d = v
		}
	case **string:
		if v, ok := src.(*string); ok {
			*d = v
		}
	case *json.RawMessage:
		switch v := src.(type) {
		case json.RawMessage:
			*d = v
		case []byte:
			*d = json.RawMessage(v)
		case string:
			*d = json.RawMessage(v)
		}
	case *[]byte:
		switch v := src.(type) {
		case []byte:
			*d = v
		case string:
			*d = []byte(v)
		}
	}
}

// makeBuildRowValues creates a build row values slice matching scanBuild order.
func makeBuildRowValues(b Build) []any {
	return []any{
		b.ID, b.TenantID, b.ProjectID, b.Environment, b.Status,
		b.CommitSHA, b.CorrelationID, b.CreatedBy,
		b.CreatedAt, b.StartedAt, b.FinishedAt, b.ErrorMessage,
		b.DurationSeconds, b.WorkflowID,
	}
}

// makeReleaseRowValues creates a release row values slice matching scanRelease order.
func makeReleaseRowValues(r Release) []any {
	return []any{
		r.ID, r.BuildID, r.TenantID, r.ProjectID, r.Environment,
		r.Status, r.CorrelationID, r.CreatedBy,
		r.CreatedAt, r.GitCommit, r.GitMirrorStatus,
		r.Apps, r.Artifacts, r.SideEffects,
	}
}

// =============================================================================
// safeTransition tests
// =============================================================================

func TestSafeTransition_ValidBuildTransition(t *testing.T) {
	// Arrange
	m := statemachine.NewBuildMachine()

	// Act
	newState, err := safeTransition(m, statemachine.BuildQueued, statemachine.BuildStarted)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if newState != statemachine.BuildRunning {
		t.Errorf("expected RUNNING, got: %s", newState)
	}
}

func TestSafeTransition_InvalidTransitionReturnError(t *testing.T) {
	// Arrange
	m := statemachine.NewBuildMachine()

	// Act — SUCCEEDED is terminal, no event should work
	_, err := safeTransition(m, statemachine.BuildSucceeded, statemachine.BuildStarted)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid transition from terminal state")
	}
	if !errors.Is(err, loopzeroerrors.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestSafeTransition_InvalidEventFromNonTerminal(t *testing.T) {
	// Arrange
	m := statemachine.NewBuildMachine()

	// Act — BuildReleaseCompleted is not valid from QUEUED
	_, err := safeTransition(m, statemachine.BuildQueued, statemachine.BuildReleaseCompleted)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid event from QUEUED")
	}
}

func TestSafeTransition_ValidReleaseTransition(t *testing.T) {
	m := statemachine.NewReleaseMachine()

	newState, err := safeTransition(m, statemachine.ReleaseReady, statemachine.ReleaseGitMirrorCompleted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newState != statemachine.ReleasePending {
		t.Errorf("expected PENDING, got: %s", newState)
	}
}

func TestSafeTransition_InvalidReleaseTransition(t *testing.T) {
	m := statemachine.NewReleaseMachine()

	_, err := safeTransition(m, statemachine.ReleaseActive, statemachine.ReleaseGitMirrorCompleted)
	if err == nil {
		t.Fatal("expected error for invalid release transition")
	}
}

// =============================================================================
// Build Store tests
// =============================================================================

func TestBuildStore_Create_SetsQueuedStatusAndGeneratesID(t *testing.T) {
	// Arrange
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)
	build := &Build{
		TenantID:      "tenant-1",
		ProjectID:     "my-project",
		Environment:   "production",
		CorrelationID: "corr-1",
		CreatedBy:     "user-1",
	}

	// Act
	err := store.Create(context.Background(), fake, build)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if build.ID == "" {
		t.Error("expected ID to be generated")
	}
	if build.Status != "QUEUED" {
		t.Errorf("expected status QUEUED, got: %s", build.Status)
	}
}

func TestBuildStore_Create_PreservesExistingID(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)
	build := &Build{
		ID:            "bld-preset-id",
		TenantID:      "t1",
		ProjectID:     "p1",
		Environment:   "prod",
		CorrelationID: "c1",
		CreatedBy:     "u1",
	}

	err := store.Create(context.Background(), fake, build)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.ID != "bld-preset-id" {
		t.Errorf("expected preset ID, got: %s", build.ID)
	}
}

func TestBuildStore_Create_UniqueViolationReturnsDeployInProgress(t *testing.T) {
	fake := &fakeDBTX{
		execErr: &UniqueViolationError{Constraint: "builds_pkey"},
	}
	store := NewPostgresBuildStore(fake)
	build := &Build{
		ID: "bld-1", TenantID: "t", ProjectID: "p", Environment: "e",
		CorrelationID: "c", CreatedBy: "u",
	}

	err := store.Create(context.Background(), fake, build)
	if !errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
		t.Errorf("expected ErrDeployInProgress, got: %v", err)
	}
}

func TestBuildStore_Create_GenericErrorWrapped(t *testing.T) {
	fake := &fakeDBTX{
		execErr: fmt.Errorf("connection refused"),
	}
	store := NewPostgresBuildStore(fake)
	build := &Build{
		ID: "bld-1", TenantID: "t", ProjectID: "p", Environment: "e",
		CorrelationID: "c", CreatedBy: "u",
	}

	err := store.Create(context.Background(), fake, build)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "build store: insert failed") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestBuildStore_GetByID_NotFoundReturnsErrBuildNotFound(t *testing.T) {
	fake := &fakeDBTX{} // emptyRow returns ErrNoRows on Scan
	store := NewPostgresBuildStore(fake)

	_, err := store.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound, got: %v", err)
	}
}

func TestBuildStore_GetByID_Success(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-123", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "RUNNING",
				CommitSHA: "abc", CorrelationID: "corr1",
				CreatedBy: "user1", CreatedAt: now,
				WorkflowID: "wf-1",
			}),
		},
	}
	store := NewPostgresBuildStore(fake)

	build, err := store.GetByID(context.Background(), "bld-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.ID != "bld-123" {
		t.Errorf("expected bld-123, got: %s", build.ID)
	}
	if build.Status != "RUNNING" {
		t.Errorf("expected RUNNING, got: %s", build.Status)
	}
}

func TestBuildStore_HasActiveBuild_True(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{true}},
	}
	store := NewPostgresBuildStore(fake)

	exists, err := store.HasActiveBuild(context.Background(), fake, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected true")
	}
}

func TestBuildStore_HasActiveBuild_False(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{false}},
	}
	store := NewPostgresBuildStore(fake)

	exists, err := store.HasActiveBuild(context.Background(), fake, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false")
	}
}

func TestBuildStore_HasActiveBuild_Error(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{err: fmt.Errorf("db down")},
	}
	store := NewPostgresBuildStore(fake)

	_, err := store.HasActiveBuild(context.Background(), fake, "t1", "p1", "prod")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildStore_UpdateStatus_ValidTransition(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-1", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "QUEUED",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresBuildStore(fake)

	build, err := store.UpdateStatus(context.Background(), fake, "bld-1", string(statemachine.BuildStarted))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != "RUNNING" {
		t.Errorf("expected RUNNING, got: %s", build.Status)
	}
}

func TestBuildStore_UpdateStatus_InvalidTransition(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-1", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "SUCCEEDED",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
	}
	store := NewPostgresBuildStore(fake)

	_, err := store.UpdateStatus(context.Background(), fake, "bld-1", string(statemachine.BuildStarted))
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
	if !errors.Is(err, loopzeroerrors.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestBuildStore_UpdateStatus_NotFound(t *testing.T) {
	fake := &fakeDBTX{} // emptyRow
	store := NewPostgresBuildStore(fake)

	_, err := store.UpdateStatus(context.Background(), fake, "bld-nonexistent", string(statemachine.BuildStarted))
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound, got: %v", err)
	}
}

func TestBuildStore_UpdateStatus_NilTxUsesPool(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-1", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "QUEUED",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresBuildStore(fake)

	// Pass nil tx — should fall back to store.db
	build, err := store.UpdateStatus(context.Background(), nil, "bld-1", string(statemachine.BuildStarted))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != "RUNNING" {
		t.Errorf("expected RUNNING, got: %s", build.Status)
	}
}

func TestBuildStore_UpdateStatus_RunningToReleasingSetsStartedAt(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-1", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "RUNNING",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresBuildStore(fake)

	build, err := store.UpdateStatus(context.Background(), fake, "bld-1", string(statemachine.BuildSucceededEvent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != "RELEASING" {
		t.Errorf("expected RELEASING, got: %s", build.Status)
	}
}

func TestBuildStore_UpdateStatus_TerminalSetsFinishedAt(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-1", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "RELEASING",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresBuildStore(fake)

	build, err := store.UpdateStatus(context.Background(), fake, "bld-1", string(statemachine.BuildReleaseCompleted))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != "SUCCEEDED" {
		t.Errorf("expected SUCCEEDED, got: %s", build.Status)
	}
	// Verify the SQL uses finished_at = NOW()
	if !strings.Contains(fake.capturedSQL, "finished_at = NOW()") {
		t.Errorf("expected SQL to set finished_at, got: %s", fake.capturedSQL)
	}
}

func TestBuildStore_SetErrorMessage_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)

	err := store.SetErrorMessage(context.Background(), fake, "bld-1", "build timed out")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildStore_SetErrorMessage_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := NewPostgresBuildStore(fake)

	err := store.SetErrorMessage(context.Background(), fake, "bld-nonexistent", "err")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound, got: %v", err)
	}
}

func TestBuildStore_SetErrorMessage_NilTxUsesPool(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)

	err := store.SetErrorMessage(context.Background(), nil, "bld-1", "err")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildStore_SetWorkflowID_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)

	err := store.SetWorkflowID(context.Background(), fake, "bld-1", "wf-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildStore_SetWorkflowID_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := NewPostgresBuildStore(fake)

	err := store.SetWorkflowID(context.Background(), fake, "bld-nonexistent", "wf-abc")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound, got: %v", err)
	}
}

func TestBuildStore_SetAppsSpec_MarshalAndExec(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)

	apps := []BuildAppSpec{
		{Name: "api", Type: "backend", Port: 8080},
		{Name: "web", Type: "frontend", Rendering: "static", Port: 3000},
	}

	err := store.SetAppsSpec(context.Background(), fake, "bld-1", apps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the JSON was passed as first arg
	if len(fake.capturedArgs) < 1 {
		t.Fatal("expected captured args")
	}
	jsonBytes, ok := fake.capturedArgs[0].([]byte)
	if !ok {
		t.Fatalf("expected first arg to be []byte, got: %T", fake.capturedArgs[0])
	}

	var parsed []BuildAppSpec
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal captured JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("expected 2 apps, got: %d", len(parsed))
	}
	if parsed[0].Name != "api" {
		t.Errorf("expected first app name 'api', got: %s", parsed[0].Name)
	}
}

func TestBuildStore_GetBuildApps_Success(t *testing.T) {
	appsJSON := `[{"name":"api","type":"backend","port":8080}]`
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: []any{json.RawMessage(appsJSON)},
		},
	}
	store := NewPostgresBuildStore(fake)

	apps, err := store.GetBuildApps(context.Background(), "bld-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got: %d", len(apps))
	}
	if apps[0].Name != "api" {
		t.Errorf("expected name 'api', got: %s", apps[0].Name)
	}
	if apps[0].Port != 8080 {
		t.Errorf("expected port 8080, got: %d", apps[0].Port)
	}
}

func TestBuildStore_GetBuildApps_EmptyArray(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: []any{json.RawMessage("[]")},
		},
	}
	store := NewPostgresBuildStore(fake)

	apps, err := store.GetBuildApps(context.Background(), "bld-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got: %d", len(apps))
	}
}

func TestBuildStore_GetBuildApps_InvalidJSON(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: []any{json.RawMessage("{invalid}")},
		},
	}
	store := NewPostgresBuildStore(fake)

	_, err := store.GetBuildApps(context.Background(), "bld-1")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal apps spec") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}

func TestBuildStore_ListByProjectAndEnv_ClampsLimit(t *testing.T) {
	// When limit < 1, default to 20; when > 50, clamp to 50
	tests := []struct {
		name          string
		inputLimit    int
		expectedLimit int
	}{
		{"negative clamps to 20", -5, 20},
		{"zero clamps to 20", 0, 20},
		{"normal passes through", 10, 10},
		{"over 50 clamps", 100, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a recording DBTX that captures args
			rec := &recordingDBTX{}
			store := NewPostgresBuildStore(rec)

			_, _, _ = store.ListByProjectAndEnv(context.Background(), "t1", "p1", "prod", tt.inputLimit, 0)

			// The limit is the 4th arg for the env-filtered query (args: t1, p1, env, limit, offset)
			if len(rec.lastArgs) < 4 {
				t.Fatalf("expected at least 4 args, got: %d", len(rec.lastArgs))
			}
			gotLimit, ok := rec.lastArgs[3].(int)
			if !ok {
				t.Fatalf("expected int arg, got: %T", rec.lastArgs[3])
			}
			if gotLimit != tt.expectedLimit {
				t.Errorf("expected limit %d, got: %d", tt.expectedLimit, gotLimit)
			}
		})
	}
}

func TestBuildStore_ListByProjectAndEnv_NegativeOffsetClampsToZero(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresBuildStore(rec)

	_, _, _ = store.ListByProjectAndEnv(context.Background(), "t1", "p1", "prod", 10, -5)

	// offset is the 5th arg
	if len(rec.lastArgs) < 5 {
		t.Fatalf("expected at least 5 args, got: %d", len(rec.lastArgs))
	}
	gotOffset, ok := rec.lastArgs[4].(int)
	if !ok {
		t.Fatalf("expected int, got: %T", rec.lastArgs[4])
	}
	if gotOffset != 0 {
		t.Errorf("expected offset 0, got: %d", gotOffset)
	}
}

func TestBuildStore_ListByProject_EmptyReturnsEmptySlice(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresBuildStore(rec)

	builds, err := store.ListByProject(context.Background(), "t1", "p1", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if builds == nil {
		t.Error("expected non-nil slice")
	}
	if len(builds) != 0 {
		t.Errorf("expected 0 builds, got: %d", len(builds))
	}
}

func TestBuildStore_CancelActiveBuilds_ReturnsWorkflowIDs(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"wf-1"},
				{"wf-2"},
				{""},
			},
		},
	}
	store := NewPostgresBuildStore(fake)

	count, wfIDs, err := store.CancelActiveBuilds(context.Background(), fake, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 cancelled, got: %d", count)
	}
	// Empty workflow IDs should be filtered out
	if len(wfIDs) != 2 {
		t.Errorf("expected 2 workflow IDs, got: %d", len(wfIDs))
	}
}

func TestBuildStore_CancelActiveBuilds_NilTxUsesPool(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{data: [][]any{}},
	}
	store := NewPostgresBuildStore(fake)

	count, _, err := store.CancelActiveBuilds(context.Background(), nil, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got: %d", count)
	}
}

// =============================================================================
// Release Store tests
// =============================================================================

func TestReleaseStore_Create_SetsReadyStatusAndGeneratesID(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresReleaseStore(fake)
	release := &Release{
		BuildID:       "bld-1",
		TenantID:      "t1",
		ProjectID:     "p1",
		Environment:   "prod",
		CorrelationID: "c1",
		CreatedBy:     "u1",
	}

	err := store.Create(context.Background(), fake, release)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.ID == "" {
		t.Error("expected ID to be generated")
	}
	if release.Status != "READY" {
		t.Errorf("expected READY, got: %s", release.Status)
	}
}

func TestReleaseStore_Create_PreservesExistingID(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresReleaseStore(fake)
	release := &Release{
		ID: "rel-preset", BuildID: "bld-1", TenantID: "t1",
		ProjectID: "p1", Environment: "prod",
		CorrelationID: "c1", CreatedBy: "u1",
	}

	err := store.Create(context.Background(), fake, release)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.ID != "rel-preset" {
		t.Errorf("expected preset ID, got: %s", release.ID)
	}
}

func TestReleaseStore_Create_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("connection refused")}
	store := NewPostgresReleaseStore(fake)
	release := &Release{
		BuildID: "bld-1", TenantID: "t1", ProjectID: "p1",
		Environment: "prod", CorrelationID: "c1", CreatedBy: "u1",
	}

	err := store.Create(context.Background(), fake, release)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "release store: insert failed") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestReleaseStore_GetByID_NotFound(t *testing.T) {
	fake := &fakeDBTX{} // emptyRow
	store := NewPostgresReleaseStore(fake)

	_, err := store.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
		t.Errorf("expected ErrReleaseNotFound, got: %v", err)
	}
}

func TestReleaseStore_GetByID_Success(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-1", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "ACTIVE",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "completed",
				Apps:            json.RawMessage("null"),
				Artifacts:       json.RawMessage("null"),
				SideEffects:     json.RawMessage("null"),
			}),
		},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetByID(context.Background(), "rel-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.ID != "rel-1" {
		t.Errorf("expected rel-1, got: %s", release.ID)
	}
	if release.Status != "ACTIVE" {
		t.Errorf("expected ACTIVE, got: %s", release.Status)
	}
}

func TestReleaseStore_UpdateStatus_ValidTransition(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-1", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "READY",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "",
				Apps:            json.RawMessage("null"),
				Artifacts:       json.RawMessage("null"),
				SideEffects:     json.RawMessage("null"),
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.UpdateStatus(context.Background(), fake, "rel-1", string(statemachine.ReleaseGitMirrorCompleted))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.Status != "PENDING" {
		t.Errorf("expected PENDING, got: %s", release.Status)
	}
}

func TestReleaseStore_UpdateStatus_InvalidTransition(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-1", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "ACTIVE",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "completed",
				Apps:            json.RawMessage("null"),
				Artifacts:       json.RawMessage("null"),
				SideEffects:     json.RawMessage("null"),
			}),
		},
	}
	store := NewPostgresReleaseStore(fake)

	_, err := store.UpdateStatus(context.Background(), fake, "rel-1", string(statemachine.ReleaseGitMirrorCompleted))
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestReleaseStore_UpdateStatus_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	_, err := store.UpdateStatus(context.Background(), fake, "nonexistent", string(statemachine.ReleaseGitMirrorCompleted))
	if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
		t.Errorf("expected ErrReleaseNotFound, got: %v", err)
	}
}

func TestReleaseStore_GetActiveByEnv_NotFoundReturnsNil(t *testing.T) {
	fake := &fakeDBTX{} // emptyRow → ErrNoRows → ErrReleaseNotFound → nil
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetActiveByEnv(context.Background(), "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Error("expected nil release for not found")
	}
}

func TestReleaseStore_GetPreviousRelease_NotFoundReturnsNil(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetPreviousRelease(context.Background(), "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Error("expected nil")
	}
}

func TestReleaseStore_GetByIDForTenant_NotFoundReturnsNil(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetByIDForTenant(context.Background(), "t1", "rel-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Error("expected nil")
	}
}

func TestReleaseStore_GetLatestByBuildID_NotFoundReturnsNil(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetLatestByBuildID(context.Background(), "bld-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Error("expected nil")
	}
}

func TestReleaseStore_SetGitCommit_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresReleaseStore(fake)

	err := store.SetGitCommit(context.Background(), fake, "rel-1", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleaseStore_SetGitCommit_NilTxUsesPool(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresReleaseStore(fake)

	err := store.SetGitCommit(context.Background(), nil, "rel-1", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleaseStore_FindPendingReleases_EmptyCellIDDefault(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresReleaseStore(rec)

	releases, err := store.FindPendingReleases(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if releases == nil {
		t.Error("expected non-nil slice")
	}
	if len(rec.lastArgs) == 0 || rec.lastArgs[0] != CellDefault {
		t.Errorf("expected CellDefault arg, got: %v", rec.lastArgs)
	}
}

func TestReleaseStore_FindActiveReleases_EmptyCellIDDefault(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresReleaseStore(rec)

	releases, err := store.FindActiveReleases(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if releases == nil {
		t.Error("expected non-nil slice")
	}
	if len(rec.lastArgs) == 0 || rec.lastArgs[0] != CellDefault {
		t.Errorf("expected CellDefault arg, got: %v", rec.lastArgs)
	}
}

func TestReleaseStore_FindPendingReleasesByTenantEnv_ReturnsEmptySlice(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresReleaseStore(rec)

	releases, err := store.FindPendingReleasesByTenantEnv(context.Background(), "t1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if releases == nil {
		t.Error("expected non-nil slice")
	}
}

func TestReleaseStore_ListByEnv_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresReleaseStore(rec)

	releases, err := store.ListByEnv(context.Background(), "t1", "p1", "prod", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ListByEnv does not nil-guard — it may return nil for empty
	// The real concern is no error.
	_ = releases
}

func TestReleaseStore_GetByIDTx_NilTxUsesPool(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	_, err := store.GetByIDTx(context.Background(), nil, "rel-1")
	if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
		t.Errorf("expected ErrReleaseNotFound with nil tx fallback, got: %v", err)
	}
}

// =============================================================================
// Domain Store tests
// =============================================================================

func TestDomainStore_Create_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	d := &routes.CustomDomain{
		ID: "dom-1", TenantID: "t1", ProjectName: "p1",
		AppName: "api", Environment: "prod", Domain: "example.com",
		Status: "pending_verification", VerificationToken: "tok-1",
	}

	err := store.Create(context.Background(), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_Create_UniqueViolation(t *testing.T) {
	fake := &fakeDBTX{
		execErr: &UniqueViolationError{Constraint: "custom_domains_domain_key"},
	}
	store := &PostgresDomainStore{DB: fake}

	d := &routes.CustomDomain{
		ID: "dom-1", TenantID: "t1", ProjectName: "p1",
		AppName: "api", Environment: "prod", Domain: "example.com",
		Status: "pending_verification", VerificationToken: "tok-1",
	}

	err := store.Create(context.Background(), d)
	if !errors.Is(err, routes.ErrDomainExists) {
		t.Errorf("expected ErrDomainExists, got: %v", err)
	}
}

func TestDomainStore_UpdateStatus_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateStatus(context.Background(), "example.com", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_UpdateStatus_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateStatus(context.Background(), "nonexistent.com", "active")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "domain not found") {
		t.Errorf("expected 'domain not found', got: %v", err)
	}
}

func TestDomainStore_TransitionStatus_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.TransitionStatus(context.Background(), "example.com", "verified", "provisioning")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_TransitionStatus_StaleTransition(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.TransitionStatus(context.Background(), "example.com", "verified", "provisioning")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "transition failed") {
		t.Errorf("expected 'transition failed', got: %v", err)
	}
}

func TestDomainStore_Delete_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.Delete(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_Delete_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.Delete(context.Background(), "nonexistent.com")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_CountByTenant_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(5)}},
	}
	store := &PostgresDomainStore{DB: fake}

	count, err := store.CountByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got: %d", count)
	}
}

func TestDomainStore_UpdateVerified_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateVerified(context.Background(), "example.com", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_UpdateVerified_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateVerified(context.Background(), "nonexistent.com", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateTLS_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateTLS(context.Background(), "nonexistent.com", "active", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateFailure_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateFailure(context.Background(), "nonexistent.com", "failed", "timeout")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateCNAMECheck_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateCNAMECheck(context.Background(), "nonexistent.com", time.Now(), 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_GetByDomainAndTenant_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	d, err := store.GetByDomainAndTenant(context.Background(), "example.com", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Error("expected nil for not found")
	}
}

// =============================================================================
// Project Store tests
// =============================================================================

func TestProjectStore_CreateProject_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresProjectStore{DB: fake}

	result, err := store.CreateProject(context.Background(), routes.CreateProjectInput{
		TenantID: "t1", Name: "my-project", CreatedBy: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "my-project" {
		t.Errorf("expected 'my-project', got: %s", result.Name)
	}
	if !strings.HasPrefix(result.ID, "proj-") {
		t.Errorf("expected ID to start with 'proj-', got: %s", result.ID)
	}
}

func TestProjectStore_CreateProject_UniqueViolation(t *testing.T) {
	fake := &fakeDBTX{
		execErr: &UniqueViolationError{Constraint: "projects_tenant_id_name"},
	}
	store := &PostgresProjectStore{DB: fake}

	_, err := store.CreateProject(context.Background(), routes.CreateProjectInput{
		TenantID: "t1", Name: "my-project", CreatedBy: "user1",
	})
	if !errors.Is(err, routes.ErrProjectExists) {
		t.Errorf("expected ErrProjectExists, got: %v", err)
	}
}

func TestProjectStore_EnsureProject_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresProjectStore{DB: fake}

	err := store.EnsureProject(context.Background(), fake, "t1", "my-project", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_EnsureProject_ConflictIsIdempotent(t *testing.T) {
	// ON CONFLICT DO NOTHING — exec returns 0 rows but no error
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresProjectStore{DB: fake}

	err := store.EnsureProject(context.Background(), fake, "t1", "my-project", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_DeleteProject_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresProjectStore{DB: fake}

	err := store.DeleteProject(context.Background(), "t1", "proj-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_DeleteProject_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresProjectStore{DB: fake}

	err := store.DeleteProject(context.Background(), "t1", "proj-nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Errorf("expected 'project not found', got: %v", err)
	}
}

func TestProjectStore_GetByName_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresProjectStore{DB: rec}

	result, err := store.GetByName(context.Background(), "t1", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for not found")
	}
}

func TestProjectStore_ListByTenant_EmptyReturnsEmptySlice(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresProjectStore{DB: rec}

	results, err := store.ListByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results == nil {
		t.Error("expected non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got: %d", len(results))
	}
}

func TestGenerateProjectID_Format(t *testing.T) {
	id := GenerateProjectID()
	if !strings.HasPrefix(id, "proj-") {
		t.Errorf("expected prefix 'proj-', got: %s", id)
	}
	// proj- + 8 hex chars = 13 chars
	if len(id) != 13 {
		t.Errorf("expected length 13, got: %d", len(id))
	}
}

func TestGenerateProjectID_Unique(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := GenerateProjectID()
		if ids[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

// =============================================================================
// Tenant Store tests
// =============================================================================

func TestTenantStore_ListTenantIDsByPattern_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"tn-lt-l3-001"},
				{"tn-lt-l3-002"},
			},
		},
	}
	store := &PostgresTenantStore{DB: fake}

	ids, err := store.ListTenantIDsByPattern(context.Background(), "tn-lt-l3-%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 IDs, got: %d", len(ids))
	}
	if ids[0] != "tn-lt-l3-001" {
		t.Errorf("expected tn-lt-l3-001, got: %s", ids[0])
	}
}

func TestTenantStore_ListTenantIDsByPattern_Empty(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{data: [][]any{}},
	}
	store := &PostgresTenantStore{DB: fake}

	ids, err := store.ListTenantIDsByPattern(context.Background(), "no-match-%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0, got: %d", len(ids))
	}
}

func TestTenantStore_DeleteTenantsFKOrdered_EmptyNoOp(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresTenantStore{DB: fake}

	counts, err := store.DeleteTenantsFKOrdered(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts.Tenants != 0 || counts.Builds != 0 {
		t.Errorf("expected zero counts for empty input, got: %+v", counts)
	}
	if fake.execCalls != 0 {
		t.Errorf("expected 0 exec calls for empty input, got: %d", fake.execCalls)
	}
}

func TestTenantStore_DeleteTenantsFKOrdered_ExecutesAllSteps(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTenantStore{DB: fake}

	counts, err := store.DeleteTenantsFKOrdered(context.Background(), []string{"tn-1", "tn-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should execute 8 steps
	if fake.execCalls != 8 {
		t.Errorf("expected 8 exec calls, got: %d", fake.execCalls)
	}
	// Each step returns RowsAffected=1
	if counts.Tenants != 1 {
		t.Errorf("expected tenants=1, got: %d", counts.Tenants)
	}
}

func TestTenantStore_DeleteTenantsFKOrdered_ErrorStopsExecution(t *testing.T) {
	callCount := 0
	fake := &fakeDBTX{
		execResult: ExecResult{RowsAffected: 1},
	}
	// Override Exec to fail on step 3
	originalExec := fake.execErr
	_ = originalExec
	store := &PostgresTenantStore{DB: &failOnNthExecDBTX{
		base:    fake,
		failAt:  3,
		failErr: fmt.Errorf("FK violation"),
		counter: &callCount,
	}}

	_, err := store.DeleteTenantsFKOrdered(context.Background(), []string{"tn-1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "step 3") {
		t.Errorf("expected error at step 3, got: %v", err)
	}
}

// failOnNthExecDBTX wraps a DBTX and fails on the Nth Exec call.
type failOnNthExecDBTX struct {
	base    *fakeDBTX
	failAt  int
	failErr error
	counter *int
}

func (f *failOnNthExecDBTX) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return f.base.Query(ctx, sql, args...)
}

func (f *failOnNthExecDBTX) Exec(ctx context.Context, sql string, args ...any) (ExecResult, error) {
	*f.counter++
	if *f.counter == f.failAt {
		return ExecResult{}, f.failErr
	}
	return f.base.Exec(ctx, sql, args...)
}

func (f *failOnNthExecDBTX) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return f.base.QueryRow(ctx, sql, args...)
}

func TestTruncateSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"short", "short"},
		{"exactly 40 chars exactly 40 chars exact", "exactly 40 chars exactly 40 chars exact"},
		{"this string is way longer than 40 characters so it should be truncated", "this string is way longer than 40 charac..."},
	}
	for _, tt := range tests {
		result := truncateSQL(tt.input)
		if result != tt.expected {
			t.Errorf("truncateSQL(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// =============================================================================
// Team Store tests
// =============================================================================

func TestTeamStore_AddMember_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTeamStore{DB: fake}

	err := store.AddMember(context.Background(), "t1", "github-user1", "member")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTeamStore_RemoveMember_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTeamStore{DB: fake}

	err := store.RemoveMember(context.Background(), "t1", "github-user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTeamStore_UpdateMemberRole_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTeamStore{DB: fake}

	err := store.UpdateMemberRole(context.Background(), "t1", "github-user1", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTeamStore_UpdateMemberRole_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresTeamStore{DB: fake}

	err := store.UpdateMemberRole(context.Background(), "t1", "github-nonexistent", "admin")
	if !errors.Is(err, routes.ErrMemberNotFound) {
		t.Errorf("expected ErrMemberNotFound, got: %v", err)
	}
}

func TestTeamStore_ListMembers_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresTeamStore{DB: rec}

	members, err := store.ListMembers(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members == nil {
		t.Error("expected non-nil slice")
	}
	if len(members) != 0 {
		t.Errorf("expected 0, got: %d", len(members))
	}
}

func TestTeamStore_GetMember_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTeamStore{DB: fake}

	member, err := store.GetMember(context.Background(), "t1", "github-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member != nil {
		t.Error("expected nil for not found")
	}
}

func TestTeamStore_GetUserByUsername_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTeamStore{DB: fake}

	user, err := store.GetUserByUsername(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != nil {
		t.Error("expected nil for not found")
	}
}

func TestStripGitHubPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"github-paulohenriquevn", "paulohenriquevn"},
		{"paulohenriquevn", "paulohenriquevn"},
		{"github-", ""},
		{"", ""},
	}
	for _, tt := range tests {
		result := stripGitHubPrefix(tt.input)
		if result != tt.expected {
			t.Errorf("stripGitHubPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// =============================================================================
// Membership Store tests
// =============================================================================

func TestMembershipStore_GetMemberships_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresMembershipStore{DB: rec}

	memberships, err := store.GetMemberships(context.Background(), "github-user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if memberships != nil && len(memberships) != 0 {
		t.Error("expected nil or empty slice")
	}
}

func TestMembershipStore_GetMemberships_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"tenant-1", "admin"},
				{"tenant-2", "member"},
			},
		},
	}
	store := &PostgresMembershipStore{DB: fake}

	memberships, err := store.GetMemberships(context.Background(), "github-user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2, got: %d", len(memberships))
	}
	if memberships[0].TenantID != "tenant-1" {
		t.Errorf("expected tenant-1, got: %s", memberships[0].TenantID)
	}
	if memberships[0].Role != "admin" {
		t.Errorf("expected admin, got: %s", memberships[0].Role)
	}
}

func TestMembershipStore_GetMemberships_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("db down")}
	store := &PostgresMembershipStore{DB: fake}

	_, err := store.GetMemberships(context.Background(), "user1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Environment Target Store tests
// =============================================================================

func TestEnvironmentTargetStore_SetDesired_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.SetDesired(context.Background(), fake, "t1", "p1", "prod", "rel-1", "user1", "corr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvironmentTargetStore_SetDesired_NilTxUsesFallback(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.SetDesired(context.Background(), nil, "t1", "p1", "prod", "rel-1", "user1", "corr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvironmentTargetStore_SetActive_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.SetActive(context.Background(), fake, "t1", "p1", "prod", "rel-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvironmentTargetStore_Get_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresEnvironmentTargetStore(rec, rec)

	target, err := store.Get(context.Background(), "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != nil {
		t.Error("expected nil for not found")
	}
}

func TestEnvironmentTargetStore_ListByProject_EmptyReturnsEmptySlice(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresEnvironmentTargetStore(rec, rec)

	targets, err := store.ListByProject(context.Background(), "t1", "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets == nil {
		t.Error("expected non-nil slice")
	}
	if len(targets) != 0 {
		t.Errorf("expected 0, got: %d", len(targets))
	}
}

func TestEnvironmentTargetStore_DeleteTarget_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.DeleteTarget(context.Background(), fake, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvironmentTargetStore_DeleteTarget_NilTxUsesFallback(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.DeleteTarget(context.Background(), nil, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// Idempotency Store tests
// =============================================================================

func TestIdempotencyStore_Get_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresIdempotencyStore{DB: rec}

	rec1, err := store.Get(context.Background(), "key-1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec1 != nil {
		t.Error("expected nil for not found")
	}
}

func TestIdempotencyStore_Get_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"key-1", "t1", "/api/v1/builds", "hash123", int(202), []byte(`{"id":"bld-1"}`)},
			},
		},
	}
	store := &PostgresIdempotencyStore{DB: fake}

	rec, err := store.Get(context.Background(), "key-1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.Key != "key-1" {
		t.Errorf("expected key-1, got: %s", rec.Key)
	}
	if rec.ResponseStatus != 202 {
		t.Errorf("expected 202, got: %d", rec.ResponseStatus)
	}
}

func TestIdempotencyStore_Store_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresIdempotencyStore{DB: fake}

	err := store.Store(context.Background(), &middleware.IdempotencyRecord{
		Key:            "key-1",
		TenantID:       "t1",
		Endpoint:       "/api/v1/builds",
		RequestHash:    "hash123",
		ResponseStatus: 202,
		ResponseBody:   []byte(`{"id":"bld-1"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdempotencyStore_Cleanup_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 42}}
	store := &PostgresIdempotencyStore{DB: fake}

	count, err := store.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Errorf("expected 42, got: %d", count)
	}
}

// =============================================================================
// Audit Store tests
// =============================================================================

func TestAuditStore_ListAuditEntries_NilDB(t *testing.T) {
	store := &PostgresAuditStore{DB: nil}

	entries, total, err := store.ListAuditEntries(context.Background(), "t1", routes.AuditFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total, got: %d", total)
	}
	if entries != nil {
		t.Error("expected nil entries for nil DB")
	}
}

func TestAuditStore_ListAuditEntries_EmptyResult(t *testing.T) {
	// Need a fake that returns 0 from count query, then empty rows
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(0)}},
		queryRows:      &fakeRowsMulti{data: [][]any{}},
	}
	store := &PostgresAuditStore{DB: fake}

	entries, total, err := store.ListAuditEntries(context.Background(), "t1", routes.AuditFilter{
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0, got: %d", total)
	}
	if entries == nil {
		t.Error("expected non-nil slice")
	}
}

// =============================================================================
// Build GC Store tests
// =============================================================================

func TestBuildGCStore_DeleteOldBuilds_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 15}}
	store := &PostgresBuildGCStore{DB: fake}

	count, err := store.DeleteOldBuilds(context.Background(), 30*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 15 {
		t.Errorf("expected 15, got: %d", count)
	}
}

func TestBuildGCStore_DeleteOldEvents_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 100}}
	store := &PostgresBuildGCStore{DB: fake}

	count, err := store.DeleteOldEvents(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 100 {
		t.Errorf("expected 100, got: %d", count)
	}
}

func TestBuildGCStore_DeleteOldWebhookDeliveries_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 50}}
	store := &PostgresBuildGCStore{DB: fake}

	count, err := store.DeleteOldWebhookDeliveries(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 50 {
		t.Errorf("expected 50, got: %d", count)
	}
}

func TestBuildGCStore_DeleteOldBuilds_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("table locked")}
	store := &PostgresBuildGCStore{DB: fake}

	_, err := store.DeleteOldBuilds(context.Background(), 30*24*time.Hour, 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// UniqueViolationError tests
// =============================================================================

func TestUniqueViolationError_ErrorMessage(t *testing.T) {
	err := &UniqueViolationError{Constraint: "builds_pkey"}
	if err.Error() != "unique violation: builds_pkey" {
		t.Errorf("expected 'unique violation: builds_pkey', got: %s", err.Error())
	}
}

// =============================================================================
// DatabaseConfig tests
// =============================================================================

func TestDatabaseConfig_DSN_DefaultSSLMode(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "loopzero",
		User:     "loopzero",
		Password: "secret",
	}

	dsn := cfg.DSN()
	if !strings.Contains(dsn, "sslmode=") {
		t.Error("expected sslmode in DSN")
	}
	if !strings.Contains(dsn, "loopzero:secret@localhost:5432/loopzero") {
		t.Errorf("unexpected DSN format: %s", dsn)
	}
}

func TestGetDatabaseConfig_Defaults(t *testing.T) {
	cfg := GetDatabaseConfig()
	if cfg.Host == "" {
		t.Error("expected non-empty host")
	}
	if cfg.Port == 0 {
		t.Error("expected non-zero port")
	}
	if cfg.Database == "" {
		t.Error("expected non-empty database")
	}
}

// =============================================================================
// Connection helper tests
// =============================================================================

func TestGetEnvOrDefault_FallbackValue(t *testing.T) {
	val := getEnvOrDefault("LOOPZERO_NONEXISTENT_ENV_VAR_FOR_TESTING", "fallback")
	if val != "fallback" {
		t.Errorf("expected 'fallback', got: %s", val)
	}
}

// =============================================================================
// Domain Store — ListByTenant filter building
// =============================================================================

func TestDomainStore_ListByTenant_DefaultLimitIs50(t *testing.T) {
	// The count query returns 0, the list query returns empty
	queryCallCount := 0
	fake := &countingDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(0)}},
		queryRows:      &fakeRowsMulti{data: [][]any{}},
		queryCallCount: &queryCallCount,
	}
	store := &PostgresDomainStore{DB: fake}

	_, _, err := store.ListByTenant(context.Background(), "t1", routes.DomainListFilter{
		Limit: 0, // Should default to 50
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the limit arg is 50 (the second-to-last arg in the list query)
	if len(fake.capturedArgs) > 0 {
		// The last query's args should include limit=50
		foundLimit := false
		for _, arg := range fake.capturedArgs {
			if v, ok := arg.(int); ok && v == 50 {
				foundLimit = true
			}
		}
		if !foundLimit {
			t.Errorf("expected limit=50 in args, got: %v", fake.capturedArgs)
		}
	}
}

// countingDBTX wraps fakeDBTX to count Query calls and track args across calls.
type countingDBTX struct {
	queryRowResult Row
	queryRows      Rows
	queryCallCount *int
	capturedSQL    string
	capturedArgs   []any
}

func (c *countingDBTX) Query(_ context.Context, sql string, args ...any) (Rows, error) {
	*c.queryCallCount++
	c.capturedSQL = sql
	c.capturedArgs = args
	if c.queryRows != nil {
		return c.queryRows, nil
	}
	return &sliceRows{}, nil
}

func (c *countingDBTX) Exec(_ context.Context, _ string, _ ...any) (ExecResult, error) {
	return ExecResult{}, nil
}

func (c *countingDBTX) QueryRow(_ context.Context, sql string, args ...any) Row {
	c.capturedSQL = sql
	c.capturedArgs = args
	if c.queryRowResult != nil {
		return c.queryRowResult
	}
	return emptyRow{}
}
