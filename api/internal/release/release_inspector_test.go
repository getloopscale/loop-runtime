// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// T4.4 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
// Asserts:
//   - happy path: full payload with audit rows
//   - 404: release not found maps to ErrReleaseInspectionNotFound
//   - signing_path derivation (keyless / keyed / unsigned)
//   - attestation_path derivation (present / absent)
//   - EC-14: workflow CR reaped → workflow_id null + degraded gracefully
//   - audit fetch failure → 200 with empty audit list (not 500)
//   - audit ResourceID filter scopes to this release only

// fakeReleaseStore is the minimal db.ReleaseStore the inspector needs.
type fakeReleaseStore struct {
	get func(ctx context.Context, id string) (*db.Release, error)
	db.ReleaseStore
}

func (f *fakeReleaseStore) GetByID(ctx context.Context, id string) (*db.Release, error) {
	return f.get(ctx, id)
}

type fakeAudit struct {
	entries []ports.AuditEntryResult
	total   int // rows matching the filter upstream; 0 means len(entries)
	err     error
}

func (f *fakeAudit) ListAuditEntries(_ context.Context, _ string, _ ports.AuditFilter) ([]ports.AuditEntryResult, int, error) {
	total := f.total
	if total == 0 {
		total = len(f.entries)
	}
	return f.entries, total, f.err
}

type fakeBuildStore struct {
	wfID string
	err  error
}

func (f *fakeBuildStore) GetWorkflowIDByBuildID(_ context.Context, _ string) (string, error) {
	return f.wfID, f.err
}

func newRelease(id string) *db.Release {
	return &db.Release{
		ID:            id,
		BuildID:       "bld-" + id,
		TenantID:      "tn-test",
		ProjectID:     "proj-test",
		Environment:   "production",
		Status:        "ACTIVE",
		CorrelationID: "corr-" + id,
	}
}

func TestReleaseInspector_Inspect_HappyPath(t *testing.T) {
	rel := newRelease("rel-001")
	digest := "sha256:abc"
	sig := "registry.example.com/x@" + digest
	signedAt := time.Now().UTC()
	url := "https://rekor.loopzero.test/api/v1/log/entries?logIndex=42"
	rel.ImageDigest = &digest
	rel.ImageSignature = &sig
	rel.ImageSignedAt = &signedAt
	rel.ImageAttestationRekorURL = &url

	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	audit := &fakeAudit{entries: []ports.AuditEntryResult{
		{Action: "release.attestation_url_set", Actor: "system:release-attestation", ResourceType: "release", ResourceID: "rel-001", CreatedAt: time.Now().UTC()},
		{Action: "release.attestation_url_set", Actor: "system:release-attestation", ResourceType: "release", ResourceID: "rel-OTHER", CreatedAt: time.Now().UTC()},
	}}
	bs := &fakeBuildStore{wfID: "loopzero-build-001-xyz"}

	insp := NewReleaseInspector(store, audit, bs)
	got, err := insp.Inspect(context.Background(), "rel-001")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.ReleaseID != "rel-001" {
		t.Errorf("release_id = %q, want rel-001", got.ReleaseID)
	}
	if got.WorkflowID == nil || *got.WorkflowID != "loopzero-build-001-xyz" {
		t.Errorf("workflow_id = %v, want loopzero-build-001-xyz", got.WorkflowID)
	}
	if got.SigningPath != "keyless" {
		t.Errorf("signing_path = %q, want keyless", got.SigningPath)
	}
	if got.AttestationPath != "present" {
		t.Errorf("attestation_path = %q, want present", got.AttestationPath)
	}
	// Audit filter must scope to this release only.
	if n := len(got.AuditLogEntries); n != 1 {
		t.Errorf("audit_log_entries = %d, want 1 (filtered to rel-001 ResourceID)", n)
	}
}

func TestReleaseInspector_Inspect_NotFound(t *testing.T) {
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return nil, nil }}
	insp := NewReleaseInspector(store, &fakeAudit{}, &fakeBuildStore{})
	_, err := insp.Inspect(context.Background(), "rel-missing")
	if err == nil {
		t.Fatal("expected error on missing release, got nil")
	}
	var nfe ErrReleaseInspectionNotFound
	if !errors.As(err, &nfe) {
		t.Errorf("expected ErrReleaseInspectionNotFound, got %T", err)
	}
}

func TestReleaseInspector_Inspect_KeyedSigning(t *testing.T) {
	rel := newRelease("rel-002")
	sig := "registry.example.com/x@sha256:abc"
	rel.ImageSignature = &sig
	// No Rekor URL → keyed mode.
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	insp := NewReleaseInspector(store, &fakeAudit{}, &fakeBuildStore{})
	got, _ := insp.Inspect(context.Background(), "rel-002")
	if got.SigningPath != "keyed" {
		t.Errorf("signing_path = %q, want keyed", got.SigningPath)
	}
	if got.AttestationPath != "absent" {
		t.Errorf("attestation_path = %q, want absent", got.AttestationPath)
	}
}

func TestReleaseInspector_Inspect_UnsignedRelease(t *testing.T) {
	rel := newRelease("rel-003") // no signature, no URL
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	insp := NewReleaseInspector(store, &fakeAudit{}, &fakeBuildStore{})
	got, _ := insp.Inspect(context.Background(), "rel-003")
	if got.SigningPath != "unsigned" {
		t.Errorf("signing_path = %q, want unsigned", got.SigningPath)
	}
}

// EC-14: workflow CR reaped → workflow_id null, NOT 500.
func TestReleaseInspector_Inspect_WorkflowReaped_GracefulDegrade(t *testing.T) {
	rel := newRelease("rel-old")
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	// Empty workflow_id + nil error (reaped CR).
	bs := &fakeBuildStore{wfID: "", err: nil}
	insp := NewReleaseInspector(store, &fakeAudit{}, bs)
	got, err := insp.Inspect(context.Background(), "rel-old")
	if err != nil {
		t.Fatalf("must NOT 500 on reaped workflow: %v", err)
	}
	if got.WorkflowID != nil {
		t.Errorf("workflow_id = %v, want nil (reaped)", got.WorkflowID)
	}
}

func TestReleaseInspector_Inspect_AuditFetchError_NonFatal(t *testing.T) {
	rel := newRelease("rel-004")
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	audit := &fakeAudit{err: errors.New("audit store offline")}
	insp := NewReleaseInspector(store, audit, &fakeBuildStore{})
	got, err := insp.Inspect(context.Background(), "rel-004")
	if err != nil {
		t.Fatalf("audit fetch failure must NOT 500: %v", err)
	}
	if got.AuditLogEntries == nil {
		t.Error("audit_log_entries should be non-nil empty slice for stable JSON shape")
	}
	if len(got.AuditLogEntries) != 0 {
		t.Errorf("audit_log_entries = %d, want 0", len(got.AuditLogEntries))
	}
}

func TestReleaseInspector_Inspect_NilAudit_NoCrash(t *testing.T) {
	rel := newRelease("rel-005")
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	insp := NewReleaseInspector(store, nil, &fakeBuildStore{})
	got, err := insp.Inspect(context.Background(), "rel-005")
	if err != nil {
		t.Fatalf("nil audit must NOT panic: %v", err)
	}
	if got.AuditLogEntries == nil {
		t.Error("audit_log_entries must be non-nil empty slice for stable JSON shape")
	}
}

// inspectorLoggingTo returns an inspector whose log output is captured in buf at
// the most verbose level, so a test can assert what an operator would see.
func inspectorLoggingTo(buf *bytes.Buffer, store db.ReleaseStore, audit ReleaseInspectorAuditQuerier) *releaseStoreInspector {
	insp := NewReleaseInspector(store, audit, &fakeBuildStore{}).(*releaseStoreInspector)
	insp.logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return insp
}

func TestReleaseInspector_AuditReadFailureIsObservableInTheLog(t *testing.T) {
	rel := newRelease("rel-a1")
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	var buf bytes.Buffer
	insp := inspectorLoggingTo(&buf, store, &fakeAudit{err: errors.New("audit store offline")})

	if _, err := insp.Inspect(context.Background(), "rel-a1"); err != nil {
		t.Fatalf("an audit read failure must not fail the inspection: %v", err)
	}

	if !strings.Contains(buf.String(), "audit store offline") {
		t.Errorf("the audit read failure left no trace in the log:\n%s", buf.String())
	}
}

func TestReleaseInspector_AuditReadFailureIsDistinguishableFromAnEmptyTrail(t *testing.T) {
	rel := newRelease("rel-a2")
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}

	failed, _ := NewReleaseInspector(store, &fakeAudit{err: errors.New("offline")}, &fakeBuildStore{}).Inspect(context.Background(), "rel-a2")
	empty, _ := NewReleaseInspector(store, &fakeAudit{}, &fakeBuildStore{}).Inspect(context.Background(), "rel-a2")

	if failed.AuditLogStatus != AuditLogUnavailable {
		t.Errorf("failed read: audit_log_status = %q, want %q", failed.AuditLogStatus, AuditLogUnavailable)
	}
	if empty.AuditLogStatus != AuditLogComplete {
		t.Errorf("empty trail: audit_log_status = %q, want %q", empty.AuditLogStatus, AuditLogComplete)
	}
}

func TestReleaseInspector_TruncatedAuditWindowIsSignalled(t *testing.T) {
	rel := newRelease("rel-a3")
	store := &fakeReleaseStore{get: func(_ context.Context, _ string) (*db.Release, error) { return rel, nil }}
	audit := &fakeAudit{entries: []ports.AuditEntryResult{{ResourceID: "rel-other", CreatedAt: time.Now()}}, total: 500}

	got, err := NewReleaseInspector(store, audit, &fakeBuildStore{}).Inspect(context.Background(), "rel-a3")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AuditLogStatus != AuditLogTruncated {
		t.Errorf("audit_log_status = %q, want %q when the upstream window held more rows than were read", got.AuditLogStatus, AuditLogTruncated)
	}
}
