// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
)

// WorkflowStatus represents the external state of a build workflow.
type WorkflowStatus struct {
	Phase      string // "Succeeded", "Failed", "Running", "Pending", ""
	FinishedAt string // RFC3339 timestamp when workflow reached terminal state; empty while in-flight.

	// CHG-2026-04-25-ATTESTATION-WIRE: per-step output parameters captured
	// from `status.nodes[*].outputs.parameters[]` on the Argo Workflow CR.
	// Outer key is the node displayName (which equals the step template
	// name, e.g., "sign-image-api", "build-image-api"). Inner key is the
	// parameter name (e.g., "signature", "digest"). Empty/nil if the
	// workflow declared no outputs (legacy or non-build workflows).
	Outputs map[string]map[string]string
}

// WorkflowSubmitter abstracts Argo Workflows submission (DIP -- no direct coupling).
type WorkflowSubmitter interface {
	Submit(ctx context.Context, spec WorkflowSpec) (workflowID string, err error)
	Cancel(ctx context.Context, namespace, workflowID string) error
	GetStatus(ctx context.Context, namespace, workflowID string) (*WorkflowStatus, error)
}

// StaticRoutingClient writes routing entries for static frontends (CF Workers KV).
// The key is the flat subdomain (e.g., "tn-loopzero-e2e-project-web-production"),
// the value is a JSON object with buildId and s3Prefix.
// Nil is valid -- it means CF KV is not configured (local dev).
type StaticRoutingClient interface {
	Put(ctx context.Context, subdomain string, entry interface{}) error
}

// WorkflowSpec contains everything needed to submit a build workflow.
type WorkflowSpec struct {
	BuildID       string
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	NoCache       bool // Skip BuildKit layer cache (full rebuild via --no-cache)
	Apps          []AppSpec
	Plan          *BuildPlan // v2: resolved build plan (when present, WorkflowBuilder uses this instead of Apps)
	CorrelationID string
	Namespace     string // Target namespace for the workflow (e.g., "build-acme")
}

// AppSpec describes a single app from loopzero.yaml as received from the CLI.
// This is the raw input -- use NormalizeApp() to resolve into a fully populated AppUnit.
// AppAutoscalingSpec holds HPA configuration from loopzero.yaml.
type AppAutoscalingSpec struct {
	MinReplicas int `json:"min,omitempty"`
	MaxReplicas int `json:"max,omitempty"`
	TargetCPU   int `json:"target_cpu,omitempty"`
}

type AppSpec struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"`                        // "server", "worker", "frontend"
	Port        int                 `json:"port"`
	Path        string              `json:"path,omitempty"`
	Framework   string              `json:"framework,omitempty"`          // "nextjs", "express", "gin", "fastapi", "custom", etc.
	Build       string              `json:"build,omitempty"`              // v2: "dockerfile", "buildpack", "custom"
	Dockerfile  string              `json:"dockerfile,omitempty"`         // v2: Dockerfile path relative to app path
	Rendering   string              `json:"rendering,omitempty"`          // v2: "static" or "server" (MEET-009)
	Env         map[string]string   `json:"env,omitempty"`                // v2: build-time environment variables
	Secrets     []string            `json:"secrets,omitempty"`            // declared secret names from loopzero.yaml
	Replicas    int                 `json:"replicas,omitempty"`           // desired replica count (default: 1)
	Autoscaling *AppAutoscalingSpec `json:"autoscaling,omitempty"`        // HPA configuration
}

// GitOpsPusher abstracts the commit-and-push of manifests to the tenant GitOps
// repo (Gitea). Step 6 architectural fix: the build pipeline no longer touches
// GitOps -- the API owns the entire release flow after a build succeeds.
type GitOpsPusher interface {
	// Push commits manifests to the tenant GitOps repo and returns the git commit SHA.
	// Path inside repo: {projectID}/{environment}/manifests.yaml
	Push(ctx context.Context, opts GitOpsPushOpts) (commitSHA string, err error)

	// Remove deletes a project's manifests from the tenant GitOps repo.
	// The operation is idempotent: if the directory doesn't exist, returns success.
	// ArgoCD with prune: true will automatically remove the K8s resources.
	Remove(ctx context.Context, opts GitOpsRemoveOpts) (commitSHA string, err error)
}

// GitOpsPushOpts contains everything needed to push manifests to the GitOps repo.
type GitOpsPushOpts struct {
	TenantID      string
	ProjectID     string
	Environment   string
	Manifests     string // raw YAML content
	BuildID       string
	ReleaseID     string
	CorrelationID string
}

// GitOpsRemoveOpts contains everything needed to remove a project from the GitOps repo.
type GitOpsRemoveOpts struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CorrelationID string
}

// AppRelease describes a built app with its image digest and URL.
type AppRelease struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"` // legacy: this is actually the image TAG (e.g., registry/org/app:commit), NOT the cryptographic digest. See ImageDigestSHA256 below for the real sha256.
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"`      // "server", "worker", "frontend"
	Rendering   string `json:"rendering,omitempty"` // "static" or "server" (Sprint 10)
	DurationMs  int    `json:"duration_ms"`

	// CHG-2026-04-25-ATTESTATION-WIRE: supply-chain attestation fields
	// surfaced from Argo workflow output parameters by the reconciler.
	// Empty/nil when the workflow declared no outputs (legacy or
	// non-build workflows). The deploy must NOT fail when these are
	// missing — they are observability data, not control data.
	ImageDigestSHA256 string     `json:"image_digest_sha256,omitempty"`
	ImageSignature    string     `json:"image_signature,omitempty"`
	ImageSignedAt     *time.Time `json:"image_signed_at,omitempty"`
}

// SyncClient abstracts ArgoCD sync operations (DIP).
type SyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
	// WaitForSync polls until the ArgoCD Application is synced and the specified
	// deployedApps are healthy. If deployedApps is empty, checks global Application health.
	WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error
}

// EnvironmentLocker abstracts the advisory lock acquisition (DIP for testability).
// In production, this wraps lock.WithEnvironmentLock with a real pgxpool.Pool.
// In tests, a fake locker executes fn immediately with a nil tx.
type EnvironmentLocker interface {
	WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error
}

// SubmitBuildRequest contains the inputs for submitting a new build.
type SubmitBuildRequest struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	NoCache       bool
	Apps          []AppSpec
	CreatedBy     string
	CorrelationID string
}

// SubmitBuildResponse is returned after a build is successfully submitted.
type SubmitBuildResponse struct {
	BuildID   string `json:"build_id"`
	StreamURL string `json:"stream_url"`
}

// BuildCallbackRequest is sent by the build system when a build completes.
type BuildCallbackRequest struct {
	Status    string       `json:"status"` // "succeeded" or "failed"
	Error     string       `json:"error,omitempty"`
	Apps      []AppRelease `json:"apps"`
	Manifests string       `json:"manifests,omitempty"` // raw K8s YAML from generate-manifests step
}

// BuildOrchestratorDeps holds all dependencies for BuildOrchestrator.
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
// ErrReplicasExceedPlan with full context for the user.
type PlanCapChecker interface {
	CheckReplicasValue(tenantID string, requested int) (allowed bool, plan string, limit int)
}

type BuildOrchestratorDeps struct {
	BuildStore     db.BuildStore
	ReleaseStore   db.ReleaseStore
	EnvTargetStore db.EnvironmentTargetStore
	ProjectStore   ProjectEnsurer // nil = project ensure disabled (local dev without projects table)
	EventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Workflow           WorkflowSubmitter
	Sync               SyncClient
	Locker             EnvironmentLocker
	GitOps             GitOpsPusher
	Delivery           DeliveryActivator // MEET-009: polymorphic release activation
	RegistryURL        string        // Container registry URL for BuildPlan image tag resolution
	ArgocdSyncTimeout  time.Duration // 0 = default 120s
	KVClient           StaticRoutingClient // CF KV client for static frontend routing (nil = disabled)
	BaseDomain         string              // Base domain for app hostnames
	// B15: plan-cap enforcement for per-service replica count. nil = disabled.
	PlanCap PlanCapChecker
}

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
	envTargetStore db.EnvironmentTargetStore
	projectStore   ProjectEnsurer
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	workflow          WorkflowSubmitter
	sync              SyncClient
	locker            EnvironmentLocker
	gitops            GitOpsPusher
	delivery          DeliveryActivator // MEET-009: polymorphic release activation
	registryURL       string // Container registry URL for BuildPlan image tag resolution
	argocdSyncTimeout time.Duration
	kvClient          StaticRoutingClient // CF KV client for static frontend routing
	baseDomain        string
	planCap           PlanCapChecker // B15: nil = enforcement disabled
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
	// MEET-009: If no DeliveryActivator provided, create default ContainerDeliveryActivator.
	// This ensures backward compatibility -- existing call sites don't need to change.
	delivery := deps.Delivery
	if delivery == nil {
		delivery = NewContainerDeliveryActivator(ContainerDeliveryDeps{
			ReleaseStore:      deps.ReleaseStore,
			EnvTargetStore:    deps.EnvTargetStore,
			EventStore:        deps.EventStore,
			Sync:              deps.Sync,
			Locker:            deps.Locker,
			GitOps:            deps.GitOps,
			ArgocdSyncTimeout: syncTimeout,
		})
	}

	return &BuildOrchestrator{
		buildStore:        deps.BuildStore,
		releaseStore:      deps.ReleaseStore,
		envTargetStore:    deps.EnvTargetStore,
		projectStore:      deps.ProjectStore,
		eventStore:        deps.EventStore,
		workflow:          deps.Workflow,
		sync:              deps.Sync,
		locker:            deps.Locker,
		gitops:            deps.GitOps,
		delivery:          delivery,
		registryURL:       registryURL,
		argocdSyncTimeout: syncTimeout,
		kvClient:          deps.KVClient,
		baseDomain:        deps.BaseDomain,
		planCap:           deps.PlanCap, // B15: optional plan-cap enforcement
	}
}

// FindReleaseForBuild returns the most recent release for a build, or nil if none exists.
// Used by the reconcile loop to determine whether Activate() has already been called.
func (o *BuildOrchestrator) FindReleaseForBuild(ctx context.Context, buildID string) (*db.Release, error) {
	return o.releaseStore.GetLatestByBuildID(ctx, buildID)
}
