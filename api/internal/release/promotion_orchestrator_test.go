// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db"
	pkgid "github.com/getloopscale/loopzero/pkg/id"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Test doubles for PromotionOrchestrator ---

type fakePromotionReleaseStore struct {
	releases    map[string]*db.Release
	activeByEnv map[string]*db.Release // key: tenant-project-env
	created     []*db.Release
	lastStatus  map[string]string

	// Sprint B T2.1 (ADR D2): per-test hook that fires on every
	// GetByIDForTenant call. Used to simulate the Lock1→Lock2 race:
	// the hook mutates f.releases[releaseID] BEFORE the orchestrator's
	// re-verify reads the row, so the re-verify sees stale-vs-current
	// drift exactly like a concurrent promote would produce.
	preLock2Mutator func(store *fakePromotionReleaseStore, releaseID string)
}

func newFakePromotionReleaseStore() *fakePromotionReleaseStore {
	return &fakePromotionReleaseStore{
		releases:    make(map[string]*db.Release),
		activeByEnv: make(map[string]*db.Release),
		created:     nil,
		lastStatus:  make(map[string]string),
	}
}

func (f *fakePromotionReleaseStore) GetActiveByEnv(_ context.Context, tenant, project, env string) (*db.Release, error) {
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}

func (f *fakePromotionReleaseStore) Create(_ context.Context, _ db.DBTX, release *db.Release) error {
	f.created = append(f.created, release)
	f.releases[release.ID] = release
	return nil
}

func (f *fakePromotionReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.lastStatus[id] = event
	if r, ok := f.releases[id]; ok {
		// V9 L17: mirror production behavior — apply the event to the
		// stored release via the canonical transitions so callers
		// observe the post-transition state. Production
		// ReleaseStoreSqlc.UpdateStatus does this via the state machine
		// and a real UPDATE; the mock must do the same for tests to
		// validate the post-condition end-to-end.
		switch event {
		case "GitMirrorCompleted":
			if r.Status == "READY" {
				r.Status = "PENDING"
			}
		case "SyncCompleted":
			if r.Status == "PENDING" {
				r.Status = "ACTIVE"
			}
		}
		return r, nil
	}
	return nil, nil
}

func (f *fakePromotionReleaseStore) SetGitCommit(_ context.Context, _ db.DBTX, id, sha string) error {
	if r, ok := f.releases[id]; ok {
		r.GitCommit = sha
	}
	return nil
}

// Sprint B T2.1 (ADR D2): re-fetch used by the INSIDE-Lock2 re-verify.
// Returns a copy of the stored release; nil indicates "deleted" which
// the orchestrator maps to CONCURRENT_PROMOTE. Test hook
// (preLock2Mutator) below lets specific specs simulate races by
// mutating the row between Lock1 and Lock2.
func (f *fakePromotionReleaseStore) GetByIDForTenant(_ context.Context, _, releaseID string) (*db.Release, error) {
	if f.preLock2Mutator != nil {
		f.preLock2Mutator(f, releaseID)
	}
	r, ok := f.releases[releaseID]
	if !ok {
		return nil, nil
	}
	// Return a defensive copy so callers cannot accidentally mutate
	// the test store via the returned pointer.
	cp := *r
	return &cp, nil
}

type fakePromotionEnvTargetStore struct {
	targets map[string]*store.EnvironmentTarget
	desired map[string]string
}

func newFakePromotionEnvTargetStore() *fakePromotionEnvTargetStore {
	return &fakePromotionEnvTargetStore{
		targets: make(map[string]*store.EnvironmentTarget),
		desired: make(map[string]string),
	}
}

func (f *fakePromotionEnvTargetStore) Get(_ context.Context, tenant, project, env string) (*store.EnvironmentTarget, error) {
	return f.targets[tenant+"-"+project+"-"+env], nil
}

func (f *fakePromotionEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, tenant, project, env, releaseID, _, _ string) error {
	f.desired[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

type fakePromotionGitReader struct {
	manifests map[string][]byte
}

func (f *fakePromotionGitReader) GetManifestsAtCommit(_ context.Context, _, _, _, commitSHA string) ([]byte, error) {
	m, ok := f.manifests[commitSHA]
	if !ok {
		return nil, fmt.Errorf("manifests not found for commit %s", commitSHA)
	}
	return m, nil
}

type fakePromotionGitPusher struct {
	pushed    bool
	lastOpts  deploycontracts.GitOpsPushOpts
	commitSHA string
}

func (f *fakePromotionGitPusher) Push(_ context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	f.pushed = true
	f.lastOpts = opts
	if f.commitSHA == "" {
		return "commit-sha-promote", nil
	}
	return f.commitSHA, nil
}

type fakePromotionSyncClient struct {
	synced  bool
	syncErr error
}

func (f *fakePromotionSyncClient) TriggerSync(_ context.Context, _ string) error {
	f.synced = true
	return f.syncErr
}

type fakePromotionLocker struct{}

func (f *fakePromotionLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestPromote_HappyPath(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		BuildID:     "build-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "abc123",
		Apps:        json.RawMessage(`[{"name":"api","type":"server","url":"https://api.staging.dev.loopzero.example"}]`),
	}
	rs.releases["rel-src-1"] = sourceRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	ets := newFakePromotionEnvTargetStore()
	gitReader := &fakePromotionGitReader{manifests: map[string][]byte{"abc123": []byte("deployment-yaml")}}
	gitPusher := &fakePromotionGitPusher{}
	syncClient := &fakePromotionSyncClient{}

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: ets,
		GitReader:  gitReader,
		GitPusher:  gitPusher,
		Sync:       syncClient,
		Locker:     &fakePromotionLocker{},
		BaseDomain: "dev.loopzero.example",
		Logger:     slog.Default(),
	})

	// Act
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:      "tn-1",
		ProjectID:     "proj-1",
		ProjectName:   "my-api",
		FromEnv:       "staging",
		ToEnv:         "production",
		CorrelationID: "corr-1",
		UserID:        "user-1",
	})

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PromotionID == "" {
		t.Error("expected promotion ID to be set")
	}
	if result.SourceRelease.ID != "rel-src-1" {
		t.Errorf("expected source release rel-src-1, got %s", result.SourceRelease.ID)
	}
	if result.NewRelease == nil {
		t.Fatal("expected new release to be created")
	}
	if result.NewRelease.Environment != "production" {
		t.Errorf("expected new release environment production, got %s", result.NewRelease.Environment)
	}
	if result.NewRelease.BuildID != "build-1" {
		t.Errorf("expected new release to reference same build, got %s", result.NewRelease.BuildID)
	}
	if result.NewRelease.Status != "PENDING" {
		t.Errorf("expected PENDING status, got %s", result.NewRelease.Status)
	}
	if result.FromEnv != "staging" {
		t.Errorf("expected from staging, got %s", result.FromEnv)
	}
	if result.ToEnv != "production" {
		t.Errorf("expected to production, got %s", result.ToEnv)
	}
	// Red Team #159 canonical formula: `{tenant}-{project}-{entryApp}-{env}.{domain}`.
	if result.URL != "https://tn-1-my-api-api-production.dev.loopzero.example" {
		t.Errorf("expected canonical URL, got %s", result.URL)
	}

	// Verify git push was to target environment
	if !gitPusher.pushed {
		t.Error("expected git push")
	}
	if gitPusher.lastOpts.Environment != "production" {
		t.Errorf("expected push to production, got %s", gitPusher.lastOpts.Environment)
	}
	if gitPusher.lastOpts.BuildID != "build-1" {
		t.Errorf("expected push with build-1, got %s", gitPusher.lastOpts.BuildID)
	}

	// Verify sync was triggered
	if !syncClient.synced {
		t.Error("expected sync trigger")
	}

	// Verify desired was set in target env
	if ets.desired["tn-1-proj-1-production"] == "" {
		t.Error("expected desired target set for production")
	}

	// Verify new release was created
	if len(rs.created) != 1 {
		t.Fatalf("expected 1 release created, got %d", len(rs.created))
	}
}

func TestPromote_SameEnvironment(t *testing.T) {
	// Arrange
	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   newFakePromotionReleaseStore(),
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		Logger:     slog.Default(),
	})

	// Act
	_, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:  "tn-1",
		ProjectID: "proj-1",
		FromEnv:   "staging",
		ToEnv:     "staging",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for same environment")
	}
	if err != loopzeroerrors.ErrSameEnvironment {
		t.Errorf("expected ErrSameEnvironment, got %v", err)
	}
}

func TestPromote_NoSourceRelease(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	// No active release in staging

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		Logger:     slog.Default(),
	})

	// Act
	_, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:  "tn-1",
		ProjectID: "proj-1",
		FromEnv:   "staging",
		ToEnv:     "production",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for no source release")
	}
	if err != loopzeroerrors.ErrNoActiveRelease {
		t.Errorf("expected ErrNoActiveRelease, got %v", err)
	}
}

func TestPromote_NoGitCommit(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "", // no git commit
	}
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		Logger:     slog.Default(),
	})

	// Act
	_, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:  "tn-1",
		ProjectID: "proj-1",
		FromEnv:   "staging",
		ToEnv:     "production",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for no git commit")
	}
}

func TestPromote_SyncFailure_DoesNotFail(t *testing.T) {
	// Arrange: sync fails but promotion should still succeed
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		BuildID:     "build-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "abc123",
		Apps:        json.RawMessage(`[]`),
	}
	rs.releases["rel-src-1"] = sourceRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	gitReader := &fakePromotionGitReader{manifests: map[string][]byte{"abc123": []byte("yaml")}}
	syncClient := &fakePromotionSyncClient{syncErr: fmt.Errorf("sync unavailable")}

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  gitReader,
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       syncClient,
		Locker:     &fakePromotionLocker{},
		BaseDomain: "dev.loopzero.example",
		Logger:     slog.Default(),
	})

	// Act
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-api",
		FromEnv:     "staging",
		ToEnv:       "production",
	})

	// Assert: promotion succeeds despite sync failure (convergence loop picks up)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewRelease == nil {
		t.Fatal("expected new release to be created")
	}
}

func TestPromote_NoBaseDomain_EmptyURL(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		BuildID:     "build-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "abc123",
		Apps:        json.RawMessage(`[]`),
	}
	rs.releases["rel-src-1"] = sourceRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	gitReader := &fakePromotionGitReader{manifests: map[string][]byte{"abc123": []byte("yaml")}}

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  gitReader,
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		BaseDomain: "", // no base domain
		Logger:     slog.Default(),
	})

	// Act
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-api",
		FromEnv:     "staging",
		ToEnv:       "production",
	})

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.URL != "" {
		t.Errorf("expected empty URL, got %q", result.URL)
	}
}

// =============================================================================
// Sprint B T2.1 — Promotion Lock2 INSIDE-lock re-verify (ADR D2)
// =============================================================================
// Three structural specs assert the new race-detection behavior added in
// promotion_orchestrator.go. The shared setup mirrors TestPromote_HappyPath
// (single source release ACTIVE in staging, gitops + sync fakes idempotent);
// each test sets fakePromotionReleaseStore.preLock2Mutator to simulate the
// race that happens between Lock1 and Lock2 in production.

// promoteTestFixture is the canonical happy-path fixture used by every T2.1
// spec. Returned as a struct so each test can override fields surgically
// (mutator, releases map) before invoking Promote.
type promoteTestFixture struct {
	orch *PromotionOrchestrator
	rs   *fakePromotionReleaseStore
	req  PromotionRequest
}

func newPromoteTestFixture() promoteTestFixture {
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-t21",
		BuildID:     "build-t21",
		TenantID:    "tn-t21",
		ProjectID:   "proj-t21",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "src-commit-t21",
		Apps:        json.RawMessage(`[{"name":"api","type":"server","url":"https://api.staging.dev.loopzero.example"}]`),
	}
	rs.releases[sourceRelease.ID] = sourceRelease
	rs.activeByEnv["tn-t21-proj-t21-staging"] = sourceRelease

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{manifests: map[string][]byte{"src-commit-t21": []byte("deployment-yaml")}},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		BaseDomain: "dev.loopzero.example",
		Logger:     slog.Default(),
	})

	return promoteTestFixture{
		orch: orch,
		rs:   rs,
		req: PromotionRequest{
			TenantID:      "tn-t21",
			ProjectID:     "proj-t21",
			ProjectName:   "my-api",
			FromEnv:       "staging",
			ToEnv:         "production",
			CorrelationID: "corr-t21",
			UserID:        "user-t21",
		},
	}
}

// TestPromote_ReleaseStillReady_TransitionsSuccessfully — happy-path
// regression: with no mutator wired, the INSIDE-Lock2 re-verify reads the
// READY state set by Lock1 and proceeds to GitMirrorCompleted normally.
// Asserts the re-verify path is INERT in the absence of a race.
func TestPromote_ReleaseStillReady_TransitionsSuccessfully(t *testing.T) {
	fx := newPromoteTestFixture()

	result, err := fx.orch.Promote(context.Background(), fx.req)
	if err != nil {
		t.Fatalf("unexpected error on non-racing promote: %v", err)
	}
	if result == nil || result.NewRelease == nil {
		t.Fatal("expected new release to be created")
	}
	if result.NewRelease.Status != "PENDING" {
		t.Errorf("expected PENDING post-transition, got %s", result.NewRelease.Status)
	}
}

// TestPromote_ConcurrentRace_RejectsLateTransition — the canonical race
// case. preLock2Mutator simulates "another promote already advanced this
// row" by flipping the target release's Status from READY to PENDING
// between Lock1 (where it was created READY by the orchestrator) and the
// re-verify inside Lock2. Asserts CONCURRENT_PROMOTE is surfaced via
// INVALID_TRANSITION (Code → HTTP 409).
func TestPromote_ConcurrentRace_RejectsLateTransition(t *testing.T) {
	fx := newPromoteTestFixture()
	// Race injection: just before GetByIDForTenant reads the row inside
	// Lock2, mutate it to PENDING — the state another Lock2 holder would
	// have left it in after applying GitMirrorCompleted.
	fx.rs.preLock2Mutator = func(store *fakePromotionReleaseStore, releaseID string) {
		if r, ok := store.releases[releaseID]; ok && r.Status == "READY" {
			r.Status = "PENDING"
		}
	}

	_, err := fx.orch.Promote(context.Background(), fx.req)
	if err == nil {
		t.Fatal("expected CONCURRENT_PROMOTE error; got nil")
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) {
		t.Fatalf("expected wrapped *loopzeroerrors.LoopZeroError; got %T (%v)", err, err)
	}
	if loopzeroErr.Code != "INVALID_TRANSITION" {
		t.Errorf("expected Code=INVALID_TRANSITION, got %s", loopzeroErr.Code)
	}
	if !strings.Contains(loopzeroErr.Message, "CONCURRENT_PROMOTE") {
		t.Errorf("expected Message to mention CONCURRENT_PROMOTE, got %q", loopzeroErr.Message)
	}
}

// TestPromote_ReleaseAlreadyPending_RejectsRetry — variant of the race
// where the row is observed in PENDING (post-GitMirrorCompleted) before
// Lock2 even runs. Same expected outcome: deterministic 409 via
// INVALID_TRANSITION + CONCURRENT_PROMOTE detail. Distinguished from the
// previous test by injecting the mutation on the FIRST call (before any
// Lock1 logic), simulating an older PENDING row left from a prior promote
// of the same target env that the orchestrator inadvertently re-encountered.
func TestPromote_ReleaseAlreadyPending_RejectsRetry(t *testing.T) {
	fx := newPromoteTestFixture()
	// Force the row to PENDING the moment the re-verify reads it.
	fx.rs.preLock2Mutator = func(store *fakePromotionReleaseStore, releaseID string) {
		if r, ok := store.releases[releaseID]; ok {
			r.Status = "PENDING"
		}
	}

	_, err := fx.orch.Promote(context.Background(), fx.req)
	if err == nil {
		t.Fatal("expected CONCURRENT_PROMOTE on PENDING row; got nil")
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) {
		t.Fatalf("expected *loopzeroerrors.LoopZeroError; got %T", err)
	}
	if loopzeroErr.Code != "INVALID_TRANSITION" {
		t.Errorf("expected INVALID_TRANSITION, got %s", loopzeroErr.Code)
	}
}

// TestPromote_NewReleaseID_IsCanonical_NoDoublePrefix — Red Team loopzero-cloud#120
// sub-finding 2: pre-fix the orchestrator concatenated "rel-" + id.GenerateReleaseID()
// at line 155, producing "rel-rel-<16hex>" (failing id.IsValidReleaseID + confusing
// downstream consumers). Post-fix: id.GenerateReleaseID alone produces the canonical
// "rel-<16hex>" format.
func TestPromote_NewReleaseID_IsCanonical_NoDoublePrefix(t *testing.T) {
	fx := newPromoteTestFixture()
	result, err := fx.orch.Promote(context.Background(), fx.req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewRelease == nil {
		t.Fatal("expected NewRelease to be set")
	}
	if !pkgid.IsValidReleaseID(result.NewRelease.ID) {
		t.Errorf("Red Team #120 — NewRelease.ID=%q is NOT canonical (^rel-[0-9a-f]{16}$); likely double-prefix bug returned", result.NewRelease.ID)
	}
	if strings.HasPrefix(result.NewRelease.ID, "rel-rel-") {
		t.Errorf("Red Team #120 — NewRelease.ID=%q has duplicate 'rel-' prefix (promotion_orchestrator.go:155 regression)", result.NewRelease.ID)
	}
}

// TestPromote_PublicDomain_PreferredOverBase — Red Team loopzero-cloud#120
// sub-finding 3 (regression of #61): pre-fix the URL was built from BaseDomain
// only ("loopzero.local" internal). Post-fix PublicDomain takes precedence so the
// response URL is the canonical Live URL.
//
// 2026-07-01 (Red Team loopzero-cloud#159): the URL formula is now the FAANG
// canonical `naming.AppURL` — includes tenant + entryApp segments so the URL
// matches the IngressRoute Host rule. Fixture apps updated to include
// `type: "server"` so entryApp resolves.
func TestPromote_PublicDomain_PreferredOverBase(t *testing.T) {
	rs := newFakePromotionReleaseStore()
	src := &db.Release{
		ID: "rel-src-1", BuildID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "preview", Status: string(statemachine.ReleaseActive), GitCommit: "abc",
		Apps: json.RawMessage(`[{"name":"api","type":"server","url":"https://api.preview.dev.loopzero.example"}]`),
	}
	rs.releases["rel-src-1"] = src
	rs.activeByEnv["tn-1-proj-1-preview"] = src
	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases: rs, EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader: &fakePromotionGitReader{manifests: map[string][]byte{"abc": []byte("y")}},
		GitPusher: &fakePromotionGitPusher{}, Sync: &fakePromotionSyncClient{},
		Locker:       &fakePromotionLocker{},
		BaseDomain:   "loopzero.local",            // internal cluster domain (#61 leaker)
		PublicDomain: "prod-dev.loopzero.example", // canonical public — should win
		Logger:       slog.Default(),
	})
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID: "tn-1", ProjectID: "proj-1", ProjectName: "my-app",
		FromEnv: "preview", ToEnv: "production", CorrelationID: "c", UserID: "u",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.URL, "loopzero.local") {
		t.Errorf("Red Team #120 — URL %q must NOT contain 'loopzero.local' when PublicDomain is set", result.URL)
	}
	if !strings.Contains(result.URL, "prod-dev.loopzero.example") {
		t.Errorf("Red Team #120 — URL %q must contain canonical PublicDomain 'prod-dev.loopzero.example'", result.URL)
	}
	// Red Team #159 canonical FAANG formula: tn + project + entryApp + env
	want := "https://tn-1-my-app-api-production.prod-dev.loopzero.example"
	if result.URL != want {
		t.Errorf("expected URL %q, got %q", want, result.URL)
	}
}

// TestPromote_PublicDomainEmpty_FallsBackToBaseDomain — back-compat: when
// LOOPZERO_PUBLIC_DOMAIN is unset (local Tilt dev), URL falls back to BaseDomain.
// This preserves the pre-#120 behavior for setups that haven't set PublicDomain.
//
// 2026-07-01 (Red Team #159): URL formula now canonical (tenant + entryApp).
func TestPromote_PublicDomainEmpty_FallsBackToBaseDomain(t *testing.T) {
	rs := newFakePromotionReleaseStore()
	src := &db.Release{
		ID: "rel-src-1", BuildID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "preview", Status: string(statemachine.ReleaseActive), GitCommit: "abc",
		Apps: json.RawMessage(`[{"name":"api","type":"server","url":"https://api.preview.dev.loopzero.example"}]`),
	}
	rs.releases["rel-src-1"] = src
	rs.activeByEnv["tn-1-proj-1-preview"] = src
	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases: rs, EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader: &fakePromotionGitReader{manifests: map[string][]byte{"abc": []byte("y")}},
		GitPusher: &fakePromotionGitPusher{}, Sync: &fakePromotionSyncClient{},
		Locker:       &fakePromotionLocker{},
		BaseDomain:   "dev.loopzero.example",
		PublicDomain: "", // unset — should fall back
		Logger:       slog.Default(),
	})
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID: "tn-1", ProjectID: "proj-1", ProjectName: "my-app",
		FromEnv: "preview", ToEnv: "production", CorrelationID: "c", UserID: "u",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Red Team #159 canonical FAANG formula (fallback BaseDomain path).
	want := "https://tn-1-my-app-api-production.dev.loopzero.example"
	if result.URL != want {
		t.Errorf("expected fallback URL %q, got %q", want, result.URL)
	}
}

// TestPromote_URL_UsesCanonicalNamingFormula_RedTeam159 — locks the FAANG
// canonical URL formula (naming.AppURL) that promote now uses so it matches
// the IngressRoute Host rule + status.go emission. Pre-fix the URL was
// `{project}-{env}.{domain}` which produced hostnames like
// `loopzerokit-example-production.prod-dev.loopzero.example` that Traefik 404'd
// because they lacked the tenant + entryApp segments — the exact user-visible
// symptom of #159 (CLI reports success + URL that 404s while the real ingress
// serves 200 at a different hostname).
func TestPromote_URL_UsesCanonicalNamingFormula_RedTeam159(t *testing.T) {
	rs := newFakePromotionReleaseStore()
	src := &db.Release{
		ID: "rel-src-1", BuildID: "build-1", TenantID: "tn-1ae1c0d930691e45", ProjectID: "loopzerokit-example",
		Environment: "preview", Status: string(statemachine.ReleaseActive), GitCommit: "abc",
		// FAANG entryApp resolution: server > frontend non-static > empty.
		// This fixture mirrors the Red Team's actual repro loopzerokit-example.
		Apps: json.RawMessage(`[{"name":"app","type":"server","url":"https://preview.dev.loopzero.example"}]`),
	}
	rs.releases["rel-src-1"] = src
	rs.activeByEnv["tn-1ae1c0d930691e45-loopzerokit-example-preview"] = src
	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases: rs, EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader: &fakePromotionGitReader{manifests: map[string][]byte{"abc": []byte("y")}},
		GitPusher: &fakePromotionGitPusher{}, Sync: &fakePromotionSyncClient{},
		Locker:       &fakePromotionLocker{},
		PublicDomain: "prod-dev.loopzero.example",
		Logger:       slog.Default(),
	})
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:  "tn-1ae1c0d930691e45",
		ProjectID: "loopzerokit-example", ProjectName: "loopzerokit-example",
		FromEnv: "preview", ToEnv: "production", CorrelationID: "c", UserID: "u",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Canonical: `{tenant}-{project}-{entryApp}-{env}.{domain}` — matches
	// IngressRoute Host rule + status.go emission (SAME URL both surfaces
	// print → user can copy either and it opens).
	wantCanonical := "https://tn-1ae1c0d930691e45-loopzerokit-example-app-production.prod-dev.loopzero.example"
	if result.URL != wantCanonical {
		t.Errorf("Red Team #159 — URL %q must equal canonical FAANG %q", result.URL, wantCanonical)
	}
	// Anti-regression: URL MUST NOT emit the legacy simplified formula.
	legacyWrong := "https://loopzerokit-example-production.prod-dev.loopzero.example"
	if result.URL == legacyWrong {
		t.Errorf("Red Team #159 — URL %q is the legacy formula that produced Traefik 404", result.URL)
	}
}

// TestPromote_URL_EmptyWhenNoEntryApp_RedTeam159 — worker-only / static-only
// releases resolve entryApp=""; per ADR D3 fail-closed, promote must return
// URL="" (not a synthetic hostname that 404s). Same semantic status.go uses.
func TestPromote_URL_EmptyWhenNoEntryApp_RedTeam159(t *testing.T) {
	rs := newFakePromotionReleaseStore()
	src := &db.Release{
		ID: "rel-src-2", BuildID: "build-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "preview", Status: string(statemachine.ReleaseActive), GitCommit: "def",
		// No server, only static frontend → entryApp empty
		Apps: json.RawMessage(`[{"name":"web","type":"frontend","rendering":"static"}]`),
	}
	rs.releases["rel-src-2"] = src
	rs.activeByEnv["tn-1-proj-1-preview"] = src
	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases: rs, EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader: &fakePromotionGitReader{manifests: map[string][]byte{"def": []byte("y")}},
		GitPusher: &fakePromotionGitPusher{}, Sync: &fakePromotionSyncClient{},
		Locker:       &fakePromotionLocker{},
		PublicDomain: "prod-dev.loopzero.example",
		Logger:       slog.Default(),
	})
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID: "tn-1", ProjectID: "proj-1", ProjectName: "static-site",
		FromEnv: "preview", ToEnv: "production", CorrelationID: "c", UserID: "u",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.URL != "" {
		t.Errorf("Red Team #159 — URL %q must be empty when entryApp resolves empty (worker-only / static-only); status.go does the same", result.URL)
	}
}
