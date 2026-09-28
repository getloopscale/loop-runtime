// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// release_types.go — public types for the releases domain that survive
// the T1.1c2c deletion of the legacy hand-rolled `release_store.go`. Per
// plan 2026-05-29-sqlc-migration-plan v1.3 § T1.1c2c, this
// file is **NOT** deleted along with PostgresReleaseStore — it carries:
//
//   - Release struct (public type consumed by services + tests)
//   - ReleaseStore interface (service field types depend on it)
//   - releaseMachine singleton (state-machine SafeTransition target;
//     used by ReleaseStoreSqlc.UpdateStatus)
//
// The sqlc-backed implementation (ReleaseStoreSqlc) lives in
// release_wrapper.go and satisfies ReleaseStore 1:1.

import (
	"context"
	"encoding/json"
	"time"
)

// releaseMachine is the release state machine singleton. Used to validate
// all status transitions.
//
// Phase 5 caller migration of ADR `2026-05-15-T6-1-typed-state-machines.md`
// (iter 38): switched from untyped `NewReleaseMachine() *Machine` to the
// typed wrapper `NewReleaseTypedMachine() *ReleaseMachine` shipped iter
// 33. Each `Transition` / `SafeTransition` call now takes typed
// `ReleasePhase` + `ReleasePhaseEvent` — a cross-machine confusion (e.g.,
// passing `BPEventStarted` to `releaseMachine.SafeTransition`) becomes a
// Go compile error instead of a runtime panic.
//
// Wire format unchanged: `string(RPhX) == string(ReleaseX)` for every
// constant (verified by TestReleaseTypedMachine_WireFormatCompatibility),
// so DB rows persisted under the untyped path read back identically.
// No migration of `releases.status` column needed.

// Release represents a release record in the database.
type Release struct {
	ID              string          `json:"id"`
	BuildID         string          `json:"build_id"`
	TenantID        string          `json:"tenant_id"`
	ProjectID       string          `json:"project_id"`
	Environment     string          `json:"environment"`
	Status          string          `json:"status"`
	CorrelationID   string          `json:"correlation_id"`
	CreatedBy       string          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	GitCommit       string          `json:"git_commit,omitempty"`
	GitMirrorStatus string          `json:"git_mirror_status"`
	Apps            json.RawMessage `json:"apps,omitempty"`
	Artifacts       json.RawMessage `json:"artifacts,omitempty"`
	SideEffects     json.RawMessage `json:"side_effects,omitempty"`

	// CHG-2026-04-25-ATTESTATION-WIRE: supply-chain attestation fields.
	// All three are populated by phaseACreateRelease when the build
	// workflow surfaced cosign+digest outputs (Phase 1+2 of the change).
	// NULL on legacy releases (workflows pre-this-change). Never set
	// SignedAt without Signature — the timestamp without the signature
	// reference is meaningless. ImageDigest is the cryptographic sha256,
	// distinct from the legacy `image_digest` aliasing the tag in
	// AppRelease.ImageDigest.
	ImageDigest    *string    `json:"image_digest,omitempty"`
	ImageSignature *string    `json:"image_signature,omitempty"`
	ImageSignedAt  *time.Time `json:"image_signed_at,omitempty"`

	// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure. Rekor
	// entry URL from cosign-attest step (SLSA L3 SBOM-attestation chain).
	// NULL when CosignAttestEnabled=false (default) OR attest step never
	// ran. Backed by migration 000019 nullable column.
	ImageAttestationRekorURL *string `json:"image_attestation_rekor_url,omitempty"`
}

// ReleaseStore defines the interface for release persistence operations.
//
// Implemented by *ReleaseStoreSqlc (release_wrapper.go) as of T1.1c2c of
// plan 2026-05-29-sqlc-migration v1.3. Previously implemented by
// *PostgresReleaseStore in release_store.go — that file was deleted in
// T1.1c2c after the 3 prod call sites switched to NewReleaseStoreSqlc.
//
// NOTE: a `releaseSelectColumns` const was introduced in CHG-2026-04-25-
// ATTESTATION-WIRE (commit 1f093272) intending to centralize the 9+
// scattered SELECT column lists. The refactor was never completed (zero
// call sites), so the orphan const was removed 2026-05-15 iter 14. With
// sqlc adoption (T3 governance-approved), the centralization is now
// implicit — every SELECT lives in `queries/releases.sql` and the
// generated row type is the single SoT for column lists.
type ReleaseStore interface {
	Create(ctx context.Context, tx DBTX, release *Release) error
	GetByID(ctx context.Context, id string) (*Release, error)
	UpdateStatus(ctx context.Context, tx DBTX, id string, event string) (*Release, error)
	// AppendSideEffect merges a JSON object into the release's
	// side_effects column. Used by the release reaper (T4.1) to capture
	// transition reasons in the audit trail. Idempotent: re-running with
	// the same key overwrites that key only; other keys are preserved
	// (jsonb || jsonb merge). fix-all-platform-issues-plan T4.1.
	AppendSideEffect(ctx context.Context, tx DBTX, id string, patch map[string]any) error
	SetGitCommit(ctx context.Context, tx DBTX, id string, commitSHA string) error
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*Release, error)
	GetActiveByEnvTx(ctx context.Context, tx DBTX, tenantID, projectID, env string) (*Release, error)
	GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*Release, error)
	GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*Release, error)
	ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int) ([]Release, error)
	// GetLatestByBuildID returns the most recent release for a build, or
	// nil if none exists.
	GetLatestByBuildID(ctx context.Context, buildID string) (*Release, error)
	// FindPendingReleases returns releases in PENDING state (awaiting
	// ArgoCD sync verification) for the given cell. Used by the Release
	// Convergence Loop (MEET-2026-03-27-012 Phase 1.1). Roadmap 03 adds
	// the cell_id filter so multi-cell controllers stay isolated. Pass
	// domain.DefaultCellID for single-cell backward compatibility.
	FindPendingReleases(ctx context.Context, cellID string) ([]Release, error)
	// FindActiveReleases returns releases in ACTIVE state for the given
	// cell, used by the health check ticker (MEET-2026-03-31-023 T1-4)
	// to detect post-activation ArgoCD divergence.
	FindActiveReleases(ctx context.Context, cellID string) ([]Release, error)
	// GetByIDTx retrieves a release by ID within a transaction.
	// MEET-2026-03-31-023 T0-1: Used by ActivationKernel.Activate
	// idempotency guard.
	GetByIDTx(ctx context.Context, tx DBTX, id string) (*Release, error)
	// CancelAllByEnv transitions every non-terminal release for the
	// given (tenant, project, env) to CANCELLED via the state-machine
	// ProjectDeleted event. Used by ProjectDeleter so future
	// GetActiveByEnv lookups don't return zombie rows after the workload
	// is gone. Returns the number of releases cancelled.
	CancelAllByEnv(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, error)
}
