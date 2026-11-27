// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deploycontracts

import (
	"context"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// shared_contracts.go — M9 Step 6 P1: the cross-context shared DTOs + narrow
// interfaces that build_orchestrator.go used to host, extracted here so the
// shared vocabulary is physically isolated before it becomes the
// services/shared leaf package (S1). Consumed by build/release/preview/domain/
// scaling/platform. Still package services (in-place); S1 moves this file to
// services/shared/.

// AppSpec describes a single app from loopzero.yaml as received from the CLI.
// This is the raw input -- use NormalizeApp() to resolve into a fully populated AppUnit.
type AppSpec struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"` // "server", "worker", "frontend"
	Port        int                 `json:"port"`
	Path        string              `json:"path,omitempty"`
	Runtime     string              `json:"runtime,omitempty"`     // services.json runtime: "python" | "node" — the build-strategy key (F-dom-2)
	Build       string              `json:"build,omitempty"`       // v2: "dockerfile", "buildpack", "custom"
	Start       string              `json:"start,omitempty"`       // T0.2: declared services.json start command (canonical Dockerfile CMD)
	Dockerfile  string              `json:"dockerfile,omitempty"`  // v2: Dockerfile path relative to app path
	Rendering   string              `json:"rendering,omitempty"`   // "server" or empty; NormalizeApp refuses anything else
	Secrets     []string            `json:"secrets,omitempty"`     // declared secret names from loopzero.yaml
	Replicas    int                 `json:"replicas,omitempty"`    // desired replica count (default: 1)
	Autoscaling *AppAutoscalingSpec `json:"autoscaling,omitempty"` // HPA configuration
}

// AppAutoscalingSpec holds HPA configuration from loopzero.yaml.
type AppAutoscalingSpec struct {
	MinReplicas    int `json:"min,omitempty"`
	MaxReplicas    int `json:"max,omitempty"`
	TargetCPU      int `json:"target_cpu,omitempty"`
	TargetMemory   int `json:"target_memory,omitempty"` // Memory utilization target; 0 scales on CPU alone
	CooldownPeriod int `json:"cooldown,omitempty"`      // Seconds before scale-to-zero (default: 300)
}

// AppRelease describes a built app with its image digest and URL.
type AppRelease struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"` // legacy: this is actually the image TAG (e.g., registry/org/app:commit), NOT the cryptographic digest. See ImageDigestSHA256 below for the real sha256.
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"`      // "server", "worker", "frontend"
	Rendering   string `json:"rendering,omitempty"` // copied from the stored app spec; every app is a container
	DurationMs  int    `json:"duration_ms"`

	// CHG-2026-04-25-ATTESTATION-WIRE: supply-chain attestation fields
	// surfaced from Argo workflow output parameters by the reconciler.
	// Empty/nil when the workflow declared no outputs (legacy or
	// non-build workflows). The deploy must NOT fail when these are
	// missing — they are observability data, not control data.
	ImageDigestSHA256 string     `json:"image_digest_sha256,omitempty"`
	ImageSignature    string     `json:"image_signature,omitempty"`
	ImageSignedAt     *time.Time `json:"image_signed_at,omitempty"`

	// Sprint 6 / T3.1: SBOM fields surfaced from the syft step. The SBOM
	// blob lives in S3; here we keep only metadata so per-app responses
	// stay light. SBOM step is best-effort (continueOn.failed) so absence
	// of these fields never blocks a deploy.
	SBOMS3Key          string `json:"sbom_s3_key,omitempty"`
	SBOMFormat         string `json:"sbom_format,omitempty"`
	SBOMComponentCount int    `json:"sbom_component_count,omitempty"`

	// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure
	// (SLSA L3 SBOM-to-image attestation linking).
	// Rekor entry URL produced by `cosign attest --predicate <sbom>` step.
	// Empty when CosignAttestEnabled=false (default until Sigstore active)
	// OR when attest step failed (build also fails per ADR D1 — no continueOn).
	// Backed by nullable releases.image_attestation_rekor_url DB column
	// (migration 000019 — forward-only per pre-release governance).
	ImageAttestationRekorURL string `json:"image_attestation_rekor_url,omitempty"`

	// ImageReused is true when this build did not build or sign the image: it
	// deploys the signed image an earlier build of the same commit produced,
	// and the attestation fields above are copied from that build.
	ImageReused bool `json:"image_reused,omitempty"`
}

// WorkflowSpec contains everything needed to submit a build workflow.
type WorkflowSpec struct {
	BuildID     string
	TenantID    string
	ProjectID   string
	Environment string
	CommitSHA   string
	// NoCache carries the user's INTENT — rebuild from scratch, reusing no earlier
	// work — not the mechanism that realises it. The previous comment said "Skip
	// BuildKit layer cache", naming in the domain contract a tool that exists only
	// in the builder (`api/internal/builds/argo/buildkit.go`, the one reader
	// that decides anything with this field). Changing the builder does not change
	// what the user asked for; it changed this comment, and that is how one plane's
	// vocabulary leaks to where it has no address (B-062).
	NoCache       bool
	Apps          []AppSpec
	Plan          *BuildPlan // v2: resolved build plan (when present, WorkflowBuilder uses this instead of Apps)
	CorrelationID string
	Namespace     string // Target namespace for the workflow (e.g., "build-acme")
}

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

	// Red Team loop-console#133: pod-level stuck-state signal. When Argo reports
	// the workflow Phase as "Running" but the underlying pod is wedged in
	// ImagePullBackOff / ErrImagePull / CreateContainerError sustained past
	// the RunningTimeout window, the reaper's M-03 "skip if workflow still
	// active" heuristic backfires — the workflow will never make progress but
	// the M-03 gate keeps protecting it. StuckPodReason surfaces the pod-level
	// failure category so the reaper can override M-03 and reap the build with
	// an actionable typed message. Empty when no stuck pod signal is present.
	//
	// Populated by WorkflowSubmitter.GetStatus from `wf.status.nodes[*].message`
	// which Argo copies from Kubernetes pod containerStatuses.reason.
	StuckPodReason string

	// FailedStep is the first required step of the build that failed on every
	// attempt, and FailedStepMessage what Argo recorded for its last attempt.
	// Both are empty when no required step failed. Phase is "Failed" whenever
	// FailedStep is set, also when Argo reported the workflow Succeeded.
	FailedStep        string
	FailedStepMessage string
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

// WorkflowSubmitter abstracts Argo Workflows submission (DIP -- no direct coupling).
type WorkflowSubmitter interface {
	Submit(ctx context.Context, spec WorkflowSpec) (workflowID string, err error)
	Cancel(ctx context.Context, namespace, workflowID string) error
	GetStatus(ctx context.Context, namespace, workflowID string) (*WorkflowStatus, error)
}

// SyncClient is the ArgoCD sync port. Every implementer MUST satisfy these 6 methods:
//   - *argocd.SyncClient (production, api/internal/release/argocd/sync_client.go)
//   - *CircuitBreakerSyncClient (production wrapper, pg_circuit_breaker.go)
//   - test fakes in *_test.go
type SyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
	// WaitForSync polls until the ArgoCD Application is synced and the specified
	// deployedApps are healthy. If deployedApps is empty, checks global Application health.
	WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error
	// ForceReconcile annotates the ArgoCD Application with refresh=hard, bypassing
	// the controller workqueue. Used by ReleaseConvergenceLoop as L22 recovery
	// when TriggerSync returns 200 but operationState never advances.
	ForceReconcile(ctx context.Context, appName string) error
	// GetSyncRevision returns the last commit SHA actually synced by ArgoCD
	// (Application.status.sync.revision). Used to detect revision mismatch
	// between a release.GitCommit and what ArgoCD just synced — guards against
	// rapid sequential deploys activating the wrong release.
	GetSyncRevision(ctx context.Context, appName string) (string, error)
	// TerminateOperation kills an in-flight ArgoCD sync operation. Used as
	// L21 recovery when the operation lock is stuck (SYNC_OPERATION_IN_PROGRESS
	// repeated >=2 consecutive ticks) and on sync-timeout to free the next deploy.
	TerminateOperation(ctx context.Context, appName string) error
	// GetAppStatus returns (synced, healthy) without blocking. Used by the
	// convergence loop's single-shot status poll (external polling cadence).
	// If deployedApps is empty, returns global Application health.
	GetAppStatus(ctx context.Context, appName string, deployedApps []string) (synced, healthy bool, err error)
}

// EnvironmentLocker abstracts the advisory lock acquisition (DIP for testability).
// In production, this wraps lock.WithEnvironmentLock with a real pgxpool.Pool.
// In tests, a fake locker executes fn immediately with a nil tx.
type EnvironmentLocker interface {
	WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error
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
