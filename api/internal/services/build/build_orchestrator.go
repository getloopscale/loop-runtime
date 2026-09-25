// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"

	"github.com/getloopscale/loopzero/api/internal/scaling"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// StaticRoutingClient writes routing entries for static frontends (CF Workers KV).
// The key is the flat subdomain (e.g., "tn-loopzero-e2e-project-web-production"),
// the value is a JSON object with buildId and s3Prefix.
// Nil is valid -- it means CF KV is not configured (local dev).
type StaticRoutingClient interface {
	Put(ctx context.Context, subdomain string, entry interface{}) error
}

// SyncClient abstracts ArgoCD sync operations (DIP).
//
// Sprint B T1.1 (plan 2026-06-01-p2-interface-contracts-robustness, ADR D1):
// interface widened from 2 to 6 methods. The 4 added methods were previously
// invoked via duck-typed assertions in release_convergence_loop.go
// (`r.sync_.(forceReconciler)` etc.). The L22 incident (commit 098b6846)
// proved that duck typing silently fails when an outer wrapper omits the
// delegation — assertion returns ok=false at runtime, becomes a no-op,
// undetected at compile time. Declaring the methods on the interface
// forces every wrapper (e.g. CircuitBreakerSyncClient) to satisfy the
// full surface OR fail to compile.
//

// ProjectEnsurer guarantees a project exists in the projects table.
// Used by the deploy flow so that deleted-then-redeployed projects
// reappear in `loopzero project list`.
type ProjectEnsurer interface {
	EnsureProject(ctx context.Context, tx db.DBTX, tenantID, name, createdBy string) error
}

// PlanCapChecker is the minimal contract the orchestrator needs to enforce
// per-service replica caps (B15, CHG-2026-04-23-B15 / MEET-2026-04-23-059).
// A nil checker means enforcement is disabled (dev / legacy callsites).
//
// CheckReplicasValue returns (allowed, planName, cap) for the given tenant
// and requested replica count. Callers map (allowed=false, plan, cap) into
// ErrReplicasExceedPlan with full context for the user. A non-nil error means
// the cap could not be verified; the submission is refused with it.
type PlanCapChecker interface {
	CheckReplicasValue(ctx context.Context, tenantID string, requested int) (allowed bool, plan string, limit int, err error)
}

// BuildOrchestratorDeps holds all dependencies for BuildOrchestrator.
type BuildOrchestratorDeps struct {
	BuildStore     db.BuildStore
	ReleaseStore   db.ReleaseStore
	EnvTargetStore store.EnvironmentTargetStore
	ProjectStore   ProjectEnsurer // nil = project ensure disabled (local dev without projects table)
	EventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Workflow deploycontracts.WorkflowSubmitter
	Sync     deploycontracts.SyncClient
	Locker   deploycontracts.EnvironmentLocker
	GitOps   deploycontracts.GitOpsPusher
	// Delivery activates a completed build. Required: it is constructed once at
	// the composition root and shared with the rollback orchestrator, so both
	// write through the same outbox and read manifests with the same reader.
	Delivery          deploycontracts.DeliveryActivator
	RegistryURL       string              // Container registry URL for BuildPlan image tag resolution
	ArgocdSyncTimeout time.Duration       // 0 = default 120s
	KVClient          StaticRoutingClient // CF KV client for static frontend routing (nil = disabled)
	BaseDomain        string              // Base domain for app hostnames
	// B15: plan-cap enforcement for per-service replica count. nil = disabled.
	PlanCap PlanCapChecker
	// KEDAEnabled gates scale-to-zero (MinReplicas=0). When false, min:0 is rejected.
	KEDAEnabled bool
	// B40: ScalingResolver merges yaml AppSpecs with persisted scaling config
	// (app_scaling_config). When non-nil, Submit calls Resolve before
	// enforceScaling + BuildAppSpec persistence so DB rows take precedence
	// (D1) and plan caps operate on resolved config (MEET-2026-04-23-059
	// Constraint 6). Nil = disabled (yaml-only — backward-compat).
	ScalingResolver scaling.ScalingResolver
	// AuditLogger persists supply-chain audit events. When non-nil and
	// the build callback reports SUCCEEDED, the orchestrator emits one
	// `image.signed` audit entry per app with a cosign signature
	// (T1.2 mitigation #3 of MEET-2026-05-12). Nil-safe.
	AuditLogger ports.AuditLogger
	// DeployTimelineSpawn is the canonical hook for spawning the
	// per-deploy 5-phase aggregator goroutine when a build transitions
	// to RUNNING. The wired function constructs the sink + source
	// readers + calls aggregator.SpawnFor.
	//
	// Plan deploy-timeline-canonical-5-phases T5.1 — keeps the
	// orchestrator decoupled from adapter wiring (sponsor T2 boundary).
	// Nil-safe: when nil, no aggregator spawns and SubmitBuild returns
	// identically (backwards-compat).
	DeployTimelineSpawn DeployTimelineSpawner
	// J-16 slice 3 (MEET-2026-07-04-001): deploy.failed outbox writer for the
	// build-completed FAILED path. May be nil (emit skipped).
	DeployEventStore store.DeployEventStore
}

// DeployTimelineSpawner is the canonical callback the orchestrator
// invokes after build.started is emitted. Implementations construct
// the EventSink + N SourceReaders and call aggregator.SpawnFor.
type DeployTimelineSpawner func(ctx context.Context, deployID string)

// BuildOrchestrator orchestrates the entire deploy flow:
// build submission, completion callback, release creation, and activation.
//
// Build lifecycle methods are split across files:
//   - build_orchestrator.go: constructor, types, interfaces, FindReleaseForBuild
//   - build_orchestrator_submit.go: SubmitBuild, failQueuedBuild
//   - build_orchestrator_cancel.go: CancelBuild
//   - build_orchestrator_complete.go: OnBuildCompleted, writeStaticRoutingEntries
//   - build_orchestrator_retry.go: RetryReleaseActivation
//
// Release activation methods live in release_activator.go.
type BuildOrchestrator struct {
	buildStore     db.BuildStore
	releaseStore   db.ReleaseStore
	envTargetStore store.EnvironmentTargetStore
	projectStore   ProjectEnsurer
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	workflow            deploycontracts.WorkflowSubmitter
	sync                deploycontracts.SyncClient
	locker              deploycontracts.EnvironmentLocker
	gitops              deploycontracts.GitOpsPusher
	delivery            deploycontracts.DeliveryActivator // MEET-009: polymorphic release activation
	registryURL         string                            // Container registry URL for BuildPlan image tag resolution
	argocdSyncTimeout   time.Duration
	kvClient            StaticRoutingClient // CF KV client for static frontend routing
	baseDomain          string
	planCap             PlanCapChecker          // B15: nil = enforcement disabled
	kedaEnabled         bool                    // gates scale-to-zero (min=0)
	scalingResolver     scaling.ScalingResolver // B40: nil = yaml-only (backward-compat)
	auditLogger         ports.AuditLogger       // T1.2 mitigation #3: per-sign cosign audit
	deployTimelineSpawn DeployTimelineSpawner   // plan T5.1 — nil-safe
	// J-16 slice 3 (MEET-2026-07-04-001): stages deploy.failed on the outbox
	// when the build workflow reports FAILED. Nil-safe (emit skipped).
	deployEventStore store.DeployEventStore
}

// defaultArgocdSyncTimeout is used when no timeout is configured.
// 600s accommodates monorepo deploys with multiple apps (3+ Deployments rolling
// out simultaneously, each with image pull + rollout + cert issuance + health probes).
// At 300s, 3-app monorepos consistently hit SYNC_FAILED during ArgoCD WaitForSync.
const defaultArgocdSyncTimeout = 600 * time.Second

// NewBuildOrchestrator creates a new BuildOrchestrator with all dependencies injected.
func NewBuildOrchestrator(deps BuildOrchestratorDeps) *BuildOrchestrator {
	syncTimeout := deps.ArgocdSyncTimeout
	if syncTimeout == 0 {
		syncTimeout = defaultArgocdSyncTimeout
	}
	registryURL := deps.RegistryURL
	if registryURL == "" {
		registryURL = "k3d-loopzero-registry:5000" // local dev default
	}
	return &BuildOrchestrator{
		buildStore:          deps.BuildStore,
		releaseStore:        deps.ReleaseStore,
		envTargetStore:      deps.EnvTargetStore,
		projectStore:        deps.ProjectStore,
		eventStore:          deps.EventStore,
		workflow:            deps.Workflow,
		sync:                deps.Sync,
		locker:              deps.Locker,
		gitops:              deps.GitOps,
		delivery:            deps.Delivery,
		registryURL:         registryURL,
		argocdSyncTimeout:   syncTimeout,
		kvClient:            deps.KVClient,
		baseDomain:          deps.BaseDomain,
		planCap:             deps.PlanCap, // B15: optional plan-cap enforcement
		kedaEnabled:         deps.KEDAEnabled,
		scalingResolver:     deps.ScalingResolver,     // B40: nil-safe (resolveScaling handles)
		auditLogger:         deps.AuditLogger,         // T1.2 mitigation #3: nil = no-op
		deployTimelineSpawn: deps.DeployTimelineSpawn, // plan T5.1: nil = no-op
		deployEventStore:    deps.DeployEventStore,    // J-16 slice 3: nil = no emit
	}
}

// resolveScaling delegates to scalingResolver when wired; otherwise returns
// apps unchanged. This nil-safe accessor keeps existing tests working without
// requiring the resolver to be set (backward-compat per T1.2 Deep Dives).
//
// Per D1: when DB row exists, it overrides yaml; when absent, yaml preserved.
// Race condition (T2.1 Deep Dive): resolve runs inside the submit transaction.
// A concurrent `loopzero scale` Upsert running between resolve start and tx commit
// produces a stale BuildAppSpec snapshot — acceptable per eventual consistency
// (next deploy picks up the newer state). The serializable isolation level
// guarantees no torn reads within this transaction.
func (o *BuildOrchestrator) resolveScaling(
	ctx context.Context,
	tenantID, projectID, environment string,
	apps []deploycontracts.AppSpec,
) ([]deploycontracts.AppSpec, error) {
	if o.scalingResolver == nil {
		return apps, nil
	}
	return o.scalingResolver.Resolve(ctx, tenantID, projectID, environment, apps)
}

// FindReleaseForBuild returns the most recent release for a build, or nil if none exists.
// Used by the reconcile loop to determine whether Activate() has already been called.
func (o *BuildOrchestrator) FindReleaseForBuild(ctx context.Context, buildID string) (*db.Release, error) {
	return o.releaseStore.GetLatestByBuildID(ctx, buildID)
}

// errDeliveryNotWired is returned when a build reaches activation on an
// orchestrator constructed without a DeliveryActivator — a wiring bug that must
// fail the activation loudly rather than panic on a nil interface.
var errDeliveryNotWired = &loopzeroerrors.LoopZeroError{
	Code:    loopzeroerrors.ErrInternalError.Code,
	Message: "build orchestrator: no delivery activator wired; set BuildOrchestratorDeps.Delivery at the composition root",
}

// activate hands a completed build to the wired delivery activator.
func (o *BuildOrchestrator) activate(ctx context.Context, build *db.Build, result deploycontracts.BuildCallbackRequest) error {
	if o.delivery == nil {
		return errDeliveryNotWired
	}
	return o.delivery.Activate(ctx, activationBuildFrom(build), result)
}

// finalizer takes the orchestrator's builds to a terminal phase.
func (o *BuildOrchestrator) finalizer() buildFinalizer {
	return buildFinalizer{builds: o.buildStore, locker: o.locker, sse: o.eventStore,
		deployEvents: o.deployEventStore, logger: slog.Default()}
}
