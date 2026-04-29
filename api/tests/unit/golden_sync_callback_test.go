// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Golden tests for SyncCallbackActivator.
// Behavioral contracts derived from ArgoCD notification_controller and sync patterns.
//
// Reference: argo-cd/notification_controller/controller/controller_test.go
//   - Table-driven tests with clear name/expected pairs
//   - assert/require from testify
//   - Context with timeout for async operations
//
// Reference: argo-cd/controller/sync_test.go
//   - Fake controllers with injected dependencies
//   - Verify state transitions after sync operations
//   - Revision history validation
//
// Reference: argo-cd/test/e2e/notification_test.go
//   - Given/When/Then style for integration flows
//   - Webhook service configuration verification

package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// ---------------------------------------------------------------------------
// Fakes — modeled after argo-cd's fake controllers (newFakeController pattern)
// and LoopZero's existing fakeConvergenceReleaseStore from release_convergence_loop_test.go
// ---------------------------------------------------------------------------

type fakeSyncReleaseStore struct {
	pending  []db.Release
	statuses map[string]string      // releaseID -> current status
	active   map[string]*db.Release // "tenant:project:env" -> active release
}

func newFakeSyncReleaseStore() *fakeSyncReleaseStore {
	return &fakeSyncReleaseStore{
		statuses: make(map[string]string),
		active:   make(map[string]*db.Release),
	}
}

func (f *fakeSyncReleaseStore) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	// Return only releases whose current status is still PENDING.
	// This mirrors how the real store works — once activated, they disappear.
	var result []db.Release
	for _, r := range f.pending {
		status := f.statuses[r.ID]
		if status == "" {
			status = r.Status
		}
		if status == string(statemachine.ReleasePending) {
			result = append(result, r)
		}
	}
	return result, nil
}

func (f *fakeSyncReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id string, event string) (*db.Release, error) {
	m := statemachine.NewReleaseMachine()
	current := f.statuses[id]
	if current == "" {
		current = string(statemachine.ReleasePending)
	}
	newState, err := safeTransition(m, statemachine.State(current), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	f.statuses[id] = string(newState)
	return &db.Release{ID: id, Status: string(newState)}, nil
}

func (f *fakeSyncReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (*db.Release, error) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	return f.active[key], nil
}

func (f *fakeSyncReleaseStore) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeSyncReleaseStore) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	for _, r := range f.pending {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("release %s not found", id)
}

func (f *fakeSyncReleaseStore) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

func safeTransition(m *statemachine.Machine, current statemachine.State, event statemachine.Event) (statemachine.State, error) {
	defer func() {
		// recover is needed to catch panics from invalid state machine transitions;
		// the inner closure below handles the actual error conversion.
		recover() //nolint:errcheck
	}()
	// This may panic on invalid transition; the deferred recover turns it into an error.
	var errResult error
	func() {
		defer func() {
			if r := recover(); r != nil { // expected: no panic
				errResult = fmt.Errorf("invalid transition: %s + %s: %v", current, event, r)
			}
		}()
		next := m.Transition(current, event)
		current = next
	}()
	if errResult != nil {
		return "", errResult
	}
	return current, nil
}

type fakeSyncEnvTargetStore struct {
	activeCalls int
}

func (f *fakeSyncEnvTargetStore) ListByProject(_ context.Context, _, _ string) ([]db.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeSyncEnvTargetStore) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}
func (f *fakeSyncEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeSyncEnvTargetStore) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	f.activeCalls++
	return nil
}
func (f *fakeSyncEnvTargetStore) Get(_ context.Context, _, _, _ string) (*db.EnvironmentTarget, error) {
	return nil, nil
}

type fakeSyncEventStore struct {
	events []string
}

func (f *fakeSyncEventStore) Append(_ context.Context, _ string, eventType string, _ json.RawMessage) (int64, error) {
	f.events = append(f.events, eventType)
	return 1, nil
}

type fakeSyncLocker struct{}

func (f *fakeSyncLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil) // Execute immediately without real lock — unit test isolation.
}

// ---------------------------------------------------------------------------
// Test 1: ParseArgoAppName — last-hyphen split
// Pattern: table-driven (from argo-cd controller_test.go TestIsAppSyncStatusRefreshed)
// ---------------------------------------------------------------------------

func TestParseArgoAppName_LastHyphenSplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantTenant string
		wantEnv    string
		wantOK     bool
	}{
		{
			name:       "standard two-segment",
			input:      "tenant-production",
			wantTenant: "tenant",
			wantEnv:    "production",
			wantOK:     true,
		},
		{
			name:       "tenant with hyphens (last-hyphen rule)",
			input:      "tenant-abc-production",
			wantTenant: "tenant-abc",
			wantEnv:    "production",
			wantOK:     true,
		},
		{
			name:       "real-world tenant ID format",
			input:      "tn-loopzero-production",
			wantTenant: "tn-loopzero",
			wantEnv:    "production",
			wantOK:     true,
		},
		{
			name:       "staging environment",
			input:      "tn-myteam-staging",
			wantTenant: "tn-myteam",
			wantEnv:    "staging",
			wantOK:     true,
		},
		{
			name:       "deeply nested tenant ID",
			input:      "tn-my-complex-org-dev",
			wantTenant: "tn-my-complex-org",
			wantEnv:    "dev",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tenant, env, ok := services.ParseArgoAppName(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ParseArgoAppName(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if tenant != tt.wantTenant {
				t.Errorf("ParseArgoAppName(%q) tenant = %q, want %q", tt.input, tenant, tt.wantTenant)
			}
			if env != tt.wantEnv {
				t.Errorf("ParseArgoAppName(%q) env = %q, want %q", tt.input, env, tt.wantEnv)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 2: ParseArgoAppName edge cases — empty, no hyphen, leading/trailing hyphen
// ---------------------------------------------------------------------------

func TestParseArgoAppName_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "no hyphen", input: "tenantproduction"},
		{name: "leading hyphen only", input: "-production"},
		{name: "trailing hyphen only", input: "tenant-"},
		{name: "single hyphen", input: "-"},
		{name: "only hyphens", input: "---"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, ok := services.ParseArgoAppName(tt.input)
			if ok {
				t.Errorf("ParseArgoAppName(%q) should return false for invalid input", tt.input)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: build a configured SyncCallbackActivator with fakes
// ---------------------------------------------------------------------------

type syncCallbackFixture struct {
	activator      *services.SyncCallbackActivator
	releaseStore   *fakeSyncReleaseStore
	envTargetStore *fakeSyncEnvTargetStore
	eventStore     *fakeSyncEventStore
}

func newSyncCallbackFixture() *syncCallbackFixture {
	rs := newFakeSyncReleaseStore()
	et := &fakeSyncEnvTargetStore{}
	es := &fakeSyncEventStore{}
	locker := &fakeSyncLocker{}
	logger := slog.Default()

	return &syncCallbackFixture{
		activator:      services.NewSyncCallbackActivator(rs, et, locker, es, "test.local", logger),
		releaseStore:   rs,
		envTargetStore: et,
		eventStore:     es,
	}
}

func makePendingRelease(id, tenantID, projectID, env, gitCommit string) db.Release {
	return db.Release{
		ID:          id,
		BuildID:     "bld-" + id,
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		Status:      string(statemachine.ReleasePending),
		GitCommit:   gitCommit,
		CreatedAt:   time.Now().Add(-10 * time.Second),
		Apps:        json.RawMessage(`[{"name":"api","image":"reg/api:v1"}]`),
	}
}

// ---------------------------------------------------------------------------
// Test 3: OnSyncCompleted happy path — PENDING release gets activated
// Mirrors argo-cd/controller/sync_test.go TestPersistRevisionHistory:
//   submit operation -> verify state transition + history recorded
// ---------------------------------------------------------------------------

func TestOnSyncCompleted_HappyPath_ActivatesPendingRelease(t *testing.T) {
	t.Parallel()

	// Arrange
	f := newSyncCallbackFixture()
	release := makePendingRelease("rel-001", "tn-loopzero", "myproject", "production", "abc123")
	f.releaseStore.pending = []db.Release{release}
	f.releaseStore.statuses[release.ID] = string(statemachine.ReleasePending)

	// Act — ArgoCD reports tn-loopzero-production as Synced+Healthy with matching revision
	err := f.activator.OnSyncCompleted("tn-loopzero-production", "abc123")

	// Assert
	if err != nil {
		t.Fatalf("OnSyncCompleted() unexpected error: %v", err)
	}

	// Release should have transitioned to ACTIVE
	status := f.releaseStore.statuses[release.ID]
	if status != string(statemachine.ReleaseActive) {
		t.Errorf("release status = %q, want %q", status, statemachine.ReleaseActive)
	}

	// Environment target should have been updated
	if f.envTargetStore.activeCalls < 1 {
		t.Error("expected SetActive to be called at least once")
	}

	// SSE event should have been emitted
	if len(f.eventStore.events) == 0 {
		t.Error("expected at least one event to be appended")
	}
}

// ---------------------------------------------------------------------------
// Test 4: OnSyncCompleted wrong tenant — release with different tenant is NOT activated
// Mirrors argo-cd notification controller's namespace filtering:
//   apps in wrong namespace are skipped (checkAppNotInAdditionalNamespaces)
// ---------------------------------------------------------------------------

func TestOnSyncCompleted_WrongTenant_ReleaseNotActivated(t *testing.T) {
	t.Parallel()

	// Arrange — release belongs to tn-other, but callback is for tn-loopzero
	f := newSyncCallbackFixture()
	release := makePendingRelease("rel-002", "tn-other", "myproject", "production", "abc123")
	f.releaseStore.pending = []db.Release{release}
	f.releaseStore.statuses[release.ID] = string(statemachine.ReleasePending)

	// Act
	err := f.activator.OnSyncCompleted("tn-loopzero-production", "abc123")

	// Assert — no error (idempotent), but release stays PENDING
	if err != nil {
		t.Fatalf("OnSyncCompleted() unexpected error: %v", err)
	}

	status := f.releaseStore.statuses[release.ID]
	if status != string(statemachine.ReleasePending) {
		t.Errorf("release should remain PENDING, got %q", status)
	}

	if f.envTargetStore.activeCalls != 0 {
		t.Errorf("SetActive should not be called for wrong tenant, got %d calls", f.envTargetStore.activeCalls)
	}
}

// ---------------------------------------------------------------------------
// Test 5: OnSyncCompleted revision mismatch — release with wrong GitCommit is skipped
// Mirrors argo-cd/controller/sync_test.go TestSyncComparisonError:
//   sync with mismatched verification result still records revision but flags error
// ---------------------------------------------------------------------------

func TestOnSyncCompleted_RevisionMismatch_ReleaseSkipped(t *testing.T) {
	t.Parallel()

	// Arrange — release expects commit "abc123", callback reports "def456"
	f := newSyncCallbackFixture()
	release := makePendingRelease("rel-003", "tn-loopzero", "myproject", "production", "abc123")
	f.releaseStore.pending = []db.Release{release}
	f.releaseStore.statuses[release.ID] = string(statemachine.ReleasePending)

	// Act
	err := f.activator.OnSyncCompleted("tn-loopzero-production", "def456")

	// Assert — no error, but release stays PENDING due to revision mismatch
	if err != nil {
		t.Fatalf("OnSyncCompleted() unexpected error: %v", err)
	}

	status := f.releaseStore.statuses[release.ID]
	if status != string(statemachine.ReleasePending) {
		t.Errorf("release should remain PENDING on revision mismatch, got %q", status)
	}

	if f.envTargetStore.activeCalls != 0 {
		t.Errorf("SetActive should not be called on revision mismatch, got %d calls", f.envTargetStore.activeCalls)
	}
}

// ---------------------------------------------------------------------------
// Test 6: OnSyncCompleted no pending releases — no error (idempotent)
// Mirrors argo-cd/test/e2e/notification_test.go patterns:
//   operations on empty state produce no errors
// ---------------------------------------------------------------------------

func TestOnSyncCompleted_NoPendingReleases_NoError(t *testing.T) {
	t.Parallel()

	// Arrange — no pending releases at all
	f := newSyncCallbackFixture()
	f.releaseStore.pending = nil

	// Act
	err := f.activator.OnSyncCompleted("tn-loopzero-production", "abc123")

	// Assert — idempotent: no error, no side effects
	if err != nil {
		t.Fatalf("OnSyncCompleted() unexpected error: %v", err)
	}

	if f.envTargetStore.activeCalls != 0 {
		t.Errorf("SetActive should not be called when no pending releases exist, got %d calls", f.envTargetStore.activeCalls)
	}

	if len(f.eventStore.events) != 0 {
		t.Errorf("no events should be emitted when no pending releases exist, got %d events", len(f.eventStore.events))
	}
}

// ---------------------------------------------------------------------------
// Test 7: OnSyncCompleted invalid app name — returns error
// Mirrors argo-cd controller_test.go TestGetAppProj_invalidProjectNestedString:
//   invalid input produces explicit error, not a panic
// ---------------------------------------------------------------------------

func TestOnSyncCompleted_InvalidAppName_ReturnsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		appName string
	}{
		{name: "empty string", appName: ""},
		{name: "no hyphen", appName: "nohyphenhere"},
		{name: "leading hyphen", appName: "-production"},
		{name: "trailing hyphen", appName: "tenant-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newSyncCallbackFixture()
			err := f.activator.OnSyncCompleted(tt.appName, "abc123")

			if err == nil {
				t.Fatalf("OnSyncCompleted(%q) should return error for invalid app name", tt.appName)
			}

			// Error message should mention the invalid app name
			errMsg := err.Error()
			if !strings.Contains(errMsg, "invalid app name") {
				t.Errorf("error should mention 'invalid app name', got: %s", errMsg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 8: OnSyncCompleted idempotent — called twice, second call is no-op
// Mirrors argo-cd sync patterns where re-sync on already-synced app is safe.
// ArgoCD's reconcile loop can invoke sync callbacks multiple times;
// the system must handle this gracefully.
// ---------------------------------------------------------------------------

func TestOnSyncCompleted_Idempotent_SecondCallIsNoOp(t *testing.T) {
	t.Parallel()

	// Arrange
	f := newSyncCallbackFixture()
	release := makePendingRelease("rel-004", "tn-loopzero", "myproject", "production", "abc123")
	f.releaseStore.pending = []db.Release{release}
	f.releaseStore.statuses[release.ID] = string(statemachine.ReleasePending)

	// Act — first call activates the release
	err := f.activator.OnSyncCompleted("tn-loopzero-production", "abc123")
	if err != nil {
		t.Fatalf("first OnSyncCompleted() unexpected error: %v", err)
	}

	// Verify first call activated it
	status := f.releaseStore.statuses[release.ID]
	if status != string(statemachine.ReleaseActive) {
		t.Fatalf("after first call: release status = %q, want ACTIVE", status)
	}

	activateCallsBefore := f.envTargetStore.activeCalls
	eventsBefore := len(f.eventStore.events)

	// Act — second call should be a no-op (release is no longer PENDING)
	err = f.activator.OnSyncCompleted("tn-loopzero-production", "abc123")

	// Assert — no error, no additional side effects
	if err != nil {
		t.Fatalf("second OnSyncCompleted() unexpected error: %v", err)
	}

	if f.envTargetStore.activeCalls != activateCallsBefore {
		t.Errorf("second call should not trigger additional SetActive calls; before=%d, after=%d",
			activateCallsBefore, f.envTargetStore.activeCalls)
	}

	if len(f.eventStore.events) != eventsBefore {
		t.Errorf("second call should not emit additional events; before=%d, after=%d",
			eventsBefore, len(f.eventStore.events))
	}
}

// No additional utility functions needed — uses strings.Contains from stdlib.
