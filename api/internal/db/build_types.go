// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

// build_types.go — public types for the builds domain that survive the
// T1.2b deletion of the legacy hand-rolled `build_store.go`,
// `build_store_list.go`, and `build_store_reconcile.go`. Per plan
// 2026-05-29-sqlc-migration-plan v1.3 § T1.2b, this file
// carries:
//
//   - Build struct (public type consumed by services + tests)
//   - BuildAppSpec / BuildAppAutoscaling (app spec types for reconcile)
//   - BuildStore interface (service field types depend on it)
//   - buildMachine singleton (state-machine SafeTransition target;
//     used by BuildStoreSqlc.UpdateStatus)
//
// The sqlc-backed implementation (BuildStoreSqlc) lives in
// build_wrapper.go and satisfies BuildStore 1:1.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// buildMachine is the build state machine singleton. Used to validate
// all status transitions.
//
// Phase 5 caller migration of ADR `2026-05-15-T6-1-typed-state-machines.md`
// (iter 37): switched from untyped `NewBuildMachine() *Machine` to the
// typed wrapper `NewBuildTypedMachine() *BuildMachine`. The wrapper
// carries the panic-trapping `SafeTransition` method which the
// store-layer (build_wrapper.go UpdateStatus) invokes per D4 invariant.
//
// Wire format unchanged: `string(BPhX) == string(BuildX)` for every
// constant (verified by TestBuildTypedMachine_WireFormatCompatibility),
// so DB rows persisted under the untyped path read back identically.
// No migration of `builds.status` column needed.

// Build represents a build record in the database.
type Build struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	ProjectID       string          `json:"project_id"`
	Environment     string          `json:"environment"`
	Status          string          `json:"status"`
	CommitSHA       string          `json:"commit_sha,omitempty"`
	CorrelationID   string          `json:"correlation_id"`
	CreatedBy       string          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	DurationSeconds *int            `json:"duration_seconds,omitempty"`
	WorkflowID      string          `json:"workflow_id,omitempty"`
	AppsSpec        json.RawMessage `json:"apps_spec,omitempty"` // App specs from submission (name, type, port, secrets)
	// BundleDigest is the SHA256 hex of the source bundle uploaded by
	// the CLI. Computed at upload time + verified by the clone-runner
	// workflow step. T2.2 of deep-review-2026-05-10-remediation-plan
	// (find_sec_no_bundle_integrity HIGH). Nullable for backward
	// compat with pre-T2.2 builds.
	BundleDigest string `json:"bundle_digest,omitempty"`
}

// BuildAppAutoscaling holds HPA configuration persisted with the build.
type BuildAppAutoscaling struct {
	MinReplicas int `json:"min,omitempty"`
	MaxReplicas int `json:"max,omitempty"`
	TargetCPU   int `json:"target_cpu,omitempty"`
	// TargetMemory is the memory utilization target; 0 scales on CPU alone.
	TargetMemory int `json:"target_memory,omitempty"`
}

// BuildAppSpec is a single app spec stored at build submission time.
// Used by the reconcile loop to generate manifests and compute image
// refs.
type BuildAppSpec struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Rendering string `json:"rendering,omitempty"` // "server" or empty; older rows may say "static", which is no longer delivered
	Port      int    `json:"port"`
	// T0.2 (plan simplify-deploy-loopzerokit-only v2.0): persist the declared
	// services.json start command + app path + build mode. Removing loopruntime-packs
	// (which auto-detected these at build time) means the canonical Dockerfile
	// generator needs them from the stored spec, not from heuristics.
	// JSONB back-compat: legacy rows omit these keys → decode as empty.
	Start       string               `json:"start,omitempty"`
	Path        string               `json:"path,omitempty"`
	Build       string               `json:"build,omitempty"`
	Runtime     string               `json:"runtime,omitempty"` // T0.1 (F-dom-2 wiring): persisted services.json runtime (python|node)
	Secrets     []string             `json:"secrets,omitempty"`
	Replicas    int                  `json:"replicas,omitempty"`
	Autoscaling *BuildAppAutoscaling `json:"autoscaling,omitempty"`
	// ReusedImage is set when an earlier build already built and signed this
	// app's image for the same commit. The workflow then skips the app's image
	// build, signing and SBOM, and the reconcile loop delivers this image.
	ReusedImage *ReusedImage `json:"reused_image,omitempty"`
}

// ReusedImage is the signed image an earlier build produced for one app at a
// given commit. Image tags are the commit SHA and the tenant repositories have
// immutable tags, so a second build of the same commit cannot push the image
// again: BuildKit's rebuild yields a different index digest (the provenance
// attestation changes) and the registry refuses the manifest. The build uses
// this image instead.
type ReusedImage struct {
	Digest             string    `json:"digest"`
	Signature          string    `json:"signature"`
	SignedAt           time.Time `json:"signed_at"`
	SBOMS3Key          string    `json:"sbom_s3_key,omitempty"`
	SBOMFormat         string    `json:"sbom_format,omitempty"`
	SBOMComponentCount int       `json:"sbom_component_count,omitempty"`
}

// BuildStore defines the interface for build persistence operations.
//
// Implemented by *BuildStoreSqlc (build_wrapper.go) as of T1.2b of plan
// 2026-05-29-sqlc-migration v1.3. Previously implemented by
// *PostgresBuildStore in build_store.go / build_store_list.go /
// build_store_reconcile.go — those files were deleted in T1.2b after
// the 3 prod call sites switched to NewBuildStoreSqlc.
type BuildStore interface {
	Create(ctx context.Context, tx DBTX, build *Build) error
	// GetByID returns a build by ID with NO tenant filter. Reserved
	// for internal callers (convergence loops, reaper,
	// domain_provisioner) that have already authenticated via a
	// different gate.
	//
	// HTTP handlers MUST NOT call GetByID — use GetByIDForTenant
	// instead. T0.4 of deep-review-2026-05-10-remediation-plan
	// (find_sec_idor_buildstore).
	GetByID(ctx context.Context, id string) (*Build, error)
	// GetByIDForTenant is the tenant-scoped variant. Returns
	// ErrBuildNotFound when the build exists but belongs to a
	// different tenant — closing the IDOR vector that handler-level
	// checks left as a single point of failure.
	GetByIDForTenant(ctx context.Context, tenantID, id string) (*Build, error)
	HasActiveBuild(ctx context.Context, tx DBTX, tenantID, projectID, env string) (bool, error)
	UpdateStatus(ctx context.Context, tx DBTX, id string, event string) (*Build, error)
	// UpdateStatusIfCurrent is the lock-free, single-statement CAS variant of
	// UpdateStatus for callers that legitimately run OUTSIDE withEnvironmentLock
	// (M9 5b, ADR 2026-07-03-m9-5b-nil-tx-status-atomicity). The target phase is
	// computed in Go via buildMachine.SafeTransition(from, event) — MA-7: the
	// transition table never leaks into SQL — and the UPDATE applies ONLY if the
	// row is still in `from` (WHERE ... AND status = from), closing the
	// read-then-write window without a tx or advisory lock. A superseded row
	// (a concurrent lock-holder already advanced it) returns (nil, nil): a
	// correct no-op, not an error. Only RUNNING and terminal
	// (SUCCEEDED/FAILED/CANCELLED) targets are supported (the lock-free callers).
	UpdateStatusIfCurrent(ctx context.Context, buildID, tenantID string, from statemachine.BuildPhase, event string) (*Build, error)
	SetErrorMessage(ctx context.Context, tx DBTX, id string, errorMessage string) error
	SetWorkflowID(ctx context.Context, tx DBTX, id string, workflowID string) error
	SetAppsSpec(ctx context.Context, tx DBTX, id string, apps []BuildAppSpec) error
	GetBuildApps(ctx context.Context, buildID string) ([]BuildAppSpec, error)
	ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]Build, error)
	ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]Build, int, error)
	// ListEnvsForProject returns the distinct env names the project has
	// at least one build for, ordered by most-recent build per env DESC.
	// Empty slice (not nil) for cold projects. Post plan
	// 2026-06-08-dashboard-screens-review-fixes T2.2 + ADR D3.
	ListEnvsForProject(ctx context.Context, tenantID, projectName string) ([]string, error)
	// FindActiveBuilds returns QUEUED/RUNNING/RELEASING builds
	// belonging to the given cell (roadmap 03). Pass domain.DefaultCellID for
	// single-cell / backward compatible behavior.
	FindActiveBuilds(ctx context.Context, cellID string) ([]Build, error)
	CancelActiveBuilds(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, []string, error)
}
