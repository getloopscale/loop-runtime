// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// BuildCompletionHandler abstracts the methods that BuildReconcileLoop calls on
// the BuildOrchestrator. Decoupling via interface enables testing the reconcile
// loop independently and follows ISP (arch-cp-004).
type BuildCompletionHandler interface {
	// OnBuildCompleted handles a successful build workflow completion.
	OnBuildCompleted(ctx context.Context, buildID string, result BuildCallbackRequest) error

	// RetryReleaseActivation retries Phase 2 (release activation) for a RELEASING build.
	RetryReleaseActivation(ctx context.Context, build *db.Build, result BuildCallbackRequest) error

	// FindReleaseForBuild returns the most recent release for a build, or nil if none exists.
	FindReleaseForBuild(ctx context.Context, buildID string) (*db.Release, error)
}

// BuildReconcileLoopConfig configures the reconcile loop.
type BuildReconcileLoopConfig struct {
	// Interval is how often the loop polls for non-terminal builds.
	Interval time.Duration
	// RegistryURL is used to compute image references (e.g., "k3d-loopzero-registry:5000").
	RegistryURL string
	// BaseDomain is the base domain for app hostnames (e.g., "loopzero.local").
	BaseDomain string
	// ClusterIssuer is the cert-manager ClusterIssuer used for TLS Certificates.
	// B14 (MEET-2026-04-23-058): required, no fallback default. Sourced from
	// Config.ClusterIssuer (env LOOPZERO_CLUSTER_ISSUER).
	ClusterIssuer string
	// MaxConcurrent is the maximum number of builds reconciled concurrently.
	// Bounds pressure on the Kubernetes API and Argo Workflow queries.
	// Default: 10.
	MaxConcurrent int
	// MaxBuildsPerTenant limits how many builds a single tenant can have
	// processing simultaneously. Prevents one busy tenant from starving others.
	// Default: 3. Override via MAX_BUILDS_PER_TENANT env var.
	MaxBuildsPerTenant int
	// CellID scopes the loop to a single cell (roadmap 03). Empty or
	// "cell-default" means single-cell / backward-compatible behavior.
	// MEET-053 A-07: When CellLister is set, CellID is ignored and the
	// loop iterates over all cells returned by the lister each tick.
	CellID string
}

// CellLister returns the list of active cell IDs.
// MEET-053 A-07: Used by BuildReconcileLoop to process all cells.
type CellLister interface {
	ListCellIDs(ctx context.Context) ([]string, error)
}

// BuildReconcileLoop continuously reconciles non-terminal builds against
// the actual Argo Workflow CRD state in the build cluster.
//
// C-01/C-03 fix: This replaces the HTTP callback and the StartupReconciler.
// Instead of the build cluster calling the API (violating the network boundary),
// the API polls the build cluster's Workflow CRDs.
//
// The control plane OBSERVES the build plane — the build plane is write-only
// for its own CRDs and artifacts. No cross-cluster HTTP from build to control.
//
// On workflow completion:
//   - Succeeded: generate manifests, push to GitOps, create Release, trigger ArgoCD sync
//   - Failed: transition build to FAILED with error from workflow status
//
// This loop also subsumes the StartupReconciler: on boot, it immediately catches up
// with any workflows that completed while the API was down.
//
// Methods are split across files:
//   - build_reconcile_loop.go: struct, New*, Start, WithPGClaims, WithCellLister, IsProcessing
//   - build_reconcile_dispatch.go: reconcile() — dispatches builds to goroutines
//   - build_reconcile_single.go: reconcileBuild() — single build state check
//   - build_reconcile_complete.go: onWorkflowSucceeded(), completeBuild(), ensureApplicationCRs()
//   - build_reconcile_retry.go: retryReleaseActivation(), failBuild()

// CustomDomainQuerier queries active custom domains for manifest rendering.
// MEET-2026-04-06-034 Block 0: both deploy and domain provisioner must include custom domains.
type CustomDomainQuerier interface {
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]routes.CustomDomain, error)
}

// ApplicationCREnsurer creates LoopZeroApplication CRs on the mgmt cluster.
// CHG-2026-04-08-001: Wires ApplicationController to the deploy flow.
type ApplicationCREnsurer interface {
	EnsureLoopZeroApplication(ctx context.Context, input ApplicationCRInput) error
}

// ApplicationCRInput is the services-layer input for creating LoopZeroApplication CRs.
// Decoupled from the k8s package to follow DIP.
type ApplicationCRInput struct {
	TenantID    string
	Project     string
	Environment string
	AppName     string
	ServiceName string
	ServicePort int32
}

type BuildReconcileLoop struct {
	buildStore   db.BuildStore
	workflow     WorkflowSubmitter
	orch         BuildCompletionHandler
	locker       EnvironmentLocker
	domainStore  CustomDomainQuerier // MEET-034: custom domains in manifests
	appCRMgr     ApplicationCREnsurer
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger *slog.Logger
	cfg    BuildReconcileLoopConfig

	// sem bounds concurrent build reconciliations. Initialized once in constructor.
	// REVIEW-MOD-2: moved from per-reconcile allocation to struct field so
	// goroutines from previous ticks continue to hold semaphore slots.
	sem chan struct{}

	// active tracks build IDs currently being processed to avoid
	// duplicate processing and coordinate with BuildReaper (MOD-6 fix).
	active sync.Map

	// pgDedup is the optional PG-backed dedup layer (Phase 0.2,
	// scale-50k-plan). When non-nil and enabled, claim/release go
	// through PG for cross-replica dedup visibility. The sync.Map
	// above remains as the fast-path local check.
	pgDedup *PGClaimDedup

	// cellLister returns all active cell IDs for multi-cell iteration.
	// MEET-053 A-07: When set, reconcile processes builds from ALL cells.
	cellLister CellLister

	// BRC-003 fix: tracks activation attempt counts per build ID.
	// After maxActivationAttempts consecutive failures, the build is failed
	// immediately instead of waiting for the 10-minute reaper timeout.
	activationAttempts sync.Map // map[string]int

	// F-01 fix: tracks OnBuildCompleted failure count per build ID.
	// After maxCompletionAttempts failures, the build is failed to avoid
	// permanent RUNNING state when completion processing is broken.
	completionAttempts sync.Map // map[string]int
}

// NewBuildReconcileLoop creates a new BuildReconcileLoop.
func NewBuildReconcileLoop(
	buildStore db.BuildStore,
	workflow WorkflowSubmitter,
	orch BuildCompletionHandler,
	locker EnvironmentLocker,
	domainStore CustomDomainQuerier,
	appCRMgr ApplicationCREnsurer,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	logger *slog.Logger,
	cfg BuildReconcileLoopConfig,
) *BuildReconcileLoop {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	if cfg.MaxBuildsPerTenant <= 0 {
		cfg.MaxBuildsPerTenant = 3
	}
	return &BuildReconcileLoop{
		buildStore:  buildStore,
		workflow:    workflow,
		orch:        orch,
		locker:      locker,
		domainStore: domainStore,
		appCRMgr:    appCRMgr,
		eventStore:  eventStore,
		logger:      logger,
		cfg:        cfg,
		sem:        make(chan struct{}, cfg.MaxConcurrent),
	}
}

// WithPGClaims attaches a PG-backed dedup layer to the reconcile loop.
// When enabled, every build claim/release goes through PG in addition
// to the local sync.Map, making the dedup state visible across
// controller replicas. This is the Phase 0.2 wiring from the scale
// plan.
//
// Call this after NewBuildReconcileLoop and before Start:
//
//	loop := NewBuildReconcileLoop(...)
//	loop.WithPGClaims(pgDedup)
//	loop.Start(ctx)
func (r *BuildReconcileLoop) WithPGClaims(dedup *PGClaimDedup) {
	r.pgDedup = dedup
}

// WithCellLister enables multi-cell iteration per tick.
// MEET-053 A-07: When set, reconcile processes builds from ALL known cells
// instead of only cfg.CellID. This ensures non-default cells are not abandoned.
func (r *BuildReconcileLoop) WithCellLister(lister CellLister) {
	r.cellLister = lister
}

// Start runs the reconcile loop until the context is cancelled.
// It runs one cycle immediately on start (catching up on missed workflows from
// API downtime) and then polls at the configured interval.
func (r *BuildReconcileLoop) Start(ctx context.Context) {
	r.logger.Info("build reconcile loop started",
		"interval", r.cfg.Interval,
		"registry_url", r.cfg.RegistryURL,
		"base_domain", r.cfg.BaseDomain)

	// Immediate reconciliation on boot (replaces StartupReconciler)
	r.reconcile(ctx)

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("build reconcile loop stopped")
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

// IsProcessing returns true if the reconcile loop is currently processing a build.
// Used by BuildReaper to avoid competing on the same build (MOD-6).
func (r *BuildReconcileLoop) IsProcessing(buildID string) bool {
	_, ok := r.active.Load(buildID)
	return ok
}
