// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package argo provides real Argo Workflows integration for build pipeline execution.
package argo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/k8s"

	forge "github.com/getloopscale/argo-workflows-go"
	forgemodel "github.com/getloopscale/argo-workflows-go/model"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	submitMaxRetries = 3
	submitBaseDelay  = 1 * time.Second
)

// WorkflowGVR is the GroupVersionResource for Argo Workflows v1alpha1.
// Exported so the argo_step_watcher service can reuse the same constant
// instead of duplicating it (Plan J-17 T1.1).
var WorkflowGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "workflows",
}

// workflowGVR is kept as an unexported alias for backward compat with
// the existing functions in this file. New callers should use WorkflowGVR.
var workflowGVR = WorkflowGVR

// WorkflowSubmitterConfig holds configuration for connecting to the build cluster.
type WorkflowSubmitterConfig struct {
	// ObjectStoreIRSA and StaticAssetsBucket: see InfraParams.
	ObjectStoreIRSA    bool
	StaticAssetsBucket string

	// CosignImage and SyftImage override the signer and SBOM scanner images;
	// empty means the built-in ones.
	CosignImage string
	// WorkspaceStorageClass is the build workspace PVC's StorageClass; empty = cluster default.
	WorkspaceStorageClass string
	SyftImage             string

	Kubeconfig string // Path to kubeconfig file
	Context    string // Kube context (e.g., "k3d-loopruntime-build")

	// Infrastructure parameters passed to every workflow via the WorkflowBuilder.
	// C-01 fix: CallbackURL/CallbackSecret removed. No more HTTP callback.
	// BLOCKER-2 fix: GiteaURL/GiteaToken removed. Clone CWT gets credentials
	// from K8s Secret (loopruntime-gitea-credentials) via envFrom.
	RegistryURL      string // e.g., "k3d-loopruntime-registry:5000"
	RegistryInsecure bool   // true for local dev registries without TLS
	BaseDomain       string // e.g., "loopruntime.local" — base domain for app hostnames
	// CosignKeyless toggles cosign signing mode in user-app build pipeline.
	// false (default, Sprint 6+): key-based, uses cosign-signing-key Secret.
	// true (Sprint 9+ deferred):  keyless via Fulcio + Rekor + projected SA token.
	// Plan T4.1 / MEET-2026-05-03-063 (Track-2): keyless requires cluster OIDC
	// infrastructure not yet provisioned. Code path is implemented and tested;
	// runtime activation tracks separate change request.
	CosignKeyless   bool
	CosignFulcioURL string // optional override (default: https://fulcio.sigstore.dev)
	CosignRekorURL  string // optional override (default: https://rekor.sigstore.dev)
	// CosignAttestEnabled toggles SLSA L3 SBOM-to-image attestation linking
	// via cosignAttestTemplate(). false (default) = no attestation step.
	// Plan 2026-05-27-M07-to-3-85 M2 T8.0 (v1.1 EC-3): MUST be wired via env var
	// LOOPRUNTIME_COSIGN_ATTEST_ENABLED to avoid silent "Implemented but not wired" pattern.
	CosignAttestEnabled bool
	// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
	// CR-1 fix: gate `--insecure-skip-verify` flag in cosign sign/attest
	// scripts. Wired from Config.CosignInsecureSkipSCT (env var
	// LOOPRUNTIME_COSIGN_INSECURE_SKIP_SCT_VERIFY). Propagated to InfraParams →
	// CosignSignParams / CosignAttestParams. Config.Validate() rejects
	// true in production without TUF root pinned.
	CosignInsecureSkipSCT bool
	// Env is the canonical LOOPRUNTIME_ENV value ("development" / "staging" /
	// "production"). T1.1 of plan 2026-06-01-p1-security-hardening
	// (sec-010 / CR-7) — drives per-tenant Fulcio audience selection
	// via D4 dev-fallback in workflow_builder.audienceFor().
	Env string
	// MinIO credentials are injected via K8s Secret (envFrom) in CWTs, not via workflow params.
}

// WorkflowSubmitter implements deploycontracts.WorkflowSubmitter using real Argo Workflows.
// It uses the WorkflowBuilder to generate self-contained Workflow CRs with
// ClusterWorkflowTemplate refs for reusable steps and inline templates for
// language-specific steps.
//
// MOD-2: The submitter is namespace-agnostic. Each workflow runs in the tenant's
// own build namespace (e.g., "build-acme"), passed via WorkflowSpec.Namespace.
type WorkflowSubmitter struct {
	client  dynamic.Interface
	builder *WorkflowBuilder
}

// NewWorkflowSubmitter creates a WorkflowSubmitter connected to the build cluster.
func NewWorkflowSubmitter(cfg WorkflowSubmitterConfig) (*WorkflowSubmitter, error) {
	// The build cluster is never the one the API runs in, so no in-cluster
	// preference: k8s.ExplicitRestConfig.
	config, err := k8s.ExplicitRestConfig(cfg.Kubeconfig, cfg.Context)
	if err != nil {
		return nil, fmt.Errorf("argo: build cluster config: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("argo: dynamic client: %w", err)
	}

	builder := NewWorkflowBuilder(cfg.infraParams())

	return &WorkflowSubmitter{
		client:  client,
		builder: builder,
	}, nil
}

// Submit generates a self-contained Workflow CR via the WorkflowBuilder and creates it
// in the build cluster with retry for transient errors.
func (s *WorkflowSubmitter) Submit(ctx context.Context, spec deploycontracts.WorkflowSpec) (string, error) {
	ctx, span := otel.Tracer("loopruntime-api").Start(ctx, "loopruntime.api.build.workflow_submit",
		trace.WithAttributes(
			attribute.String("loopruntime.workflow.namespace", spec.Namespace),
		))
	defer span.End()

	wf, err := s.builder.Build(spec)
	if err != nil {
		return "", fmt.Errorf("argo: build workflow: %w", err)
	}

	wf.Namespace = spec.Namespace

	// Sprint 6 B3: Propagate trace context to the Argo Workflow via annotations.
	// This allows the build reconcile loop to link the completed workflow back
	// to the originating deploy trace, creating end-to-end trace visibility.
	if spanCtx := trace.SpanFromContext(ctx).SpanContext(); spanCtx.IsValid() {
		if wf.Annotations == nil {
			wf.Annotations = make(map[string]string)
		}
		wf.Annotations["runtime.getloopscale.com/trace-id"] = spanCtx.TraceID().String()
		wf.Annotations["runtime.getloopscale.com/span-id"] = spanCtx.SpanID().String()
	}

	wfObject, err := wf.ToDict()
	if err != nil {
		return "", fmt.Errorf("argo: serialize workflow: %w", err)
	}

	workflow := &unstructured.Unstructured{Object: wfObject}

	var lastErr error
	for attempt := 0; attempt <= submitMaxRetries; attempt++ {
		if attempt > 0 {
			delay := submitBaseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		created, err := s.client.Resource(workflowGVR).Namespace(spec.Namespace).Create(ctx, workflow, metav1.CreateOptions{})
		if err == nil {
			name := created.GetName()
			span.SetAttributes(attribute.String("loopruntime.workflow.name", name))
			return name, nil
		}

		lastErr = err

		// Only retry on transient server errors (5xx), not client errors (4xx)
		if k8serrors.IsServerTimeout(err) || k8serrors.IsServiceUnavailable(err) ||
			k8serrors.IsTooManyRequests(err) || k8serrors.IsInternalError(err) {
			continue
		}

		// Non-retryable error — fail immediately
		return "", fmt.Errorf("argo: create workflow: %w", err)
	}

	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "workflow submission failed after retries")
	return "", fmt.Errorf("argo: create workflow after %d retries: %w", submitMaxRetries, lastErr)
}

// Cancel stops a running workflow by setting its shutdown strategy.
// The patch document includes apiVersion + kind explicitly so K8s server-side
// patching (and the fake dynamic client used in tests) accept the merge.
// Without these, fake-server validation rejects the patch with
// "patch: Invalid value: ...apiVersion:”,kind:”...".
func (s *WorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	shutdownPatch := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"spec": map[string]any{
			"shutdown": "Stop",
		},
	}
	patchBytes, err := json.Marshal(shutdownPatch)
	if err != nil {
		return fmt.Errorf("argo: marshal shutdown patch: %w", err)
	}
	_, err = s.client.Resource(workflowGVR).Namespace(namespace).Patch(
		ctx, workflowID, "application/merge-patch+json", patchBytes, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("argo: cancel workflow %s: %w", workflowID, err)
	}
	return nil
}

// Delete removes a workflow object from the build cluster.
// Used to free ResourceQuota (count/workflows.argoproj.io) after a build completes.
// Returns nil if the workflow is already deleted (NotFound).
func (s *WorkflowSubmitter) Delete(ctx context.Context, namespace, workflowID string) error {
	err := s.client.Resource(workflowGVR).Namespace(namespace).Delete(ctx, workflowID, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("argo: delete workflow %s/%s: %w", namespace, workflowID, err)
	}
	return nil
}

// GetStatus checks the external state of a workflow in the build cluster.
// Returns (status, nil) if the workflow exists, or (nil, nil) if not found.
//
// BuildKit sidecar fix: When Argo kills the buildkitd daemon sidecar (SIGTERM after
// main container exits), the sidecar exits non-zero and Argo marks the workflow as
// "Failed" even though the build succeeded (main container exit code 0). This is a
// known Argo Workflows behavior with daemon sidecars.
//
// To handle this, when the phase is "Failed", we check if all Pod-type nodes have
// exitCode "0" on their outputs. If so, the build actually succeeded — the failure
// is only from daemon sidecar termination.
func (s *WorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*deploycontracts.WorkflowStatus, error) {
	wf, err := s.client.Resource(workflowGVR).Namespace(namespace).Get(ctx, workflowID, metav1.GetOptions{})
	if err != nil {
		// BRC-006 fix: distinguish NotFound (workflow deleted/expired) from transient
		// errors (network timeout, auth expiry, API server blip). Previously ALL errors
		// returned (nil, nil), causing the reconcile loop to permanently fail live builds
		// on any transient K8s API disruption.
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		// Transient error — propagate to caller so reconcile loop retries next tick
		return nil, fmt.Errorf("argo: get workflow %s/%s: %w", namespace, workflowID, err)
	}

	status, err := forge.ParseWorkflowStatusFromUnstructured(wf.Object)
	if err != nil {
		return nil, fmt.Errorf("argo: parse workflow status %s/%s: %w", namespace, workflowID, err)
	}
	if status == nil {
		return &deploycontracts.WorkflowStatus{}, nil
	}

	phase := string(status.Phase)

	// Defensive check: if Argo reports "Failed" but all pod main containers exited with 0,
	// override to "Succeeded". Originally needed for sidecar false-failures (MEET-2026-03-26-007).
	// Retained as defense-in-depth against Argo phase-reporting edge cases (node eviction,
	// CRI timeout, etc.). Should fire zero times with single-container BuildKit pattern.
	if phase == "Failed" && forge.AllPodNodesExitedZero(status) {
		slog.Warn("workflow phase overridden: all main containers exited 0 but Argo reported Failed",
			"workflow_id", workflowID, "namespace", namespace)
		phase = "Succeeded"
	}

	// A finished workflow whose required step failed is a failed build, whatever
	// phase Argo gave the workflow: see failedRequiredStep.
	failedStep, failedStepMessage := "", ""
	if status.FinishedAt != "" {
		failedStep, failedStepMessage = failedRequiredStep(status)
	}
	if failedStep != "" && phase == "Succeeded" {
		slog.Warn("workflow phase overridden: a required step failed but Argo reported Succeeded",
			"workflow_id", workflowID, "namespace", namespace, "step", failedStep)
		phase = "Failed"
	}

	return &deploycontracts.WorkflowStatus{
		Phase:             phase,
		FinishedAt:        status.FinishedAt,
		Outputs:           extractStepOutputs(wf.Object),
		StuckPodReason:    extractStuckPodReason(status),
		FailedStep:        failedStep,
		FailedStepMessage: failedStepMessage,
	}, nil
}

// bestEffortStepPrefix names the steps the build goes on without. The SBOM
// task runs with continueOn.failed (workflow_builder.go).
const bestEffortStepPrefix = "sbom-"

// failedRequiredStep returns the first required step that failed on every
// attempt, with the message of its last attempt, or two empty strings.
//
// The workflow's own phase cannot be trusted for this. The only leaf of the
// build DAG is the SBOM task, which has continueOn.failed, and Argo then ends
// the workflow Succeeded when an earlier step failed and the rest were
// omitted. The build went on to render manifests and failed there with
// "missing image digest", whatever the step and its cause had been.
func failedRequiredStep(status *forgemodel.WorkflowStatusDetail) (step, message string) {
	if status == nil {
		return "", ""
	}
	// A step with a retry strategy is a Retry node whose children are the
	// attempts. Only the Retry node says how the step ended.
	attempts := make(map[string]bool)
	for _, node := range status.Nodes {
		if node.Type == "Retry" {
			for _, child := range node.Children {
				attempts[child] = true
			}
		}
	}
	var failed *forgemodel.NodeStatus
	for id, node := range status.Nodes {
		if node.Type != "Retry" && node.Type != "Pod" {
			continue
		}
		if attempts[id] || (node.Phase != "Failed" && node.Phase != "Error") {
			continue
		}
		if strings.HasPrefix(node.DisplayName, bestEffortStepPrefix) {
			continue
		}
		if failed == nil || node.StartedAt < failed.StartedAt ||
			(node.StartedAt == failed.StartedAt && node.DisplayName < failed.DisplayName) {
			current := node
			failed = &current
		}
	}
	if failed == nil {
		return "", ""
	}
	message = failed.Message
	lastAttempt := ""
	for _, child := range failed.Children {
		attempt, ok := status.Nodes[child]
		if ok && attempt.Message != "" && attempt.StartedAt >= lastAttempt {
			lastAttempt, message = attempt.StartedAt, attempt.Message
		}
	}
	return failed.DisplayName, message
}

// extractStuckPodReason walks the workflow's per-node status looking for pod
// nodes wedged in image-pull failure states. Red Team loopruntime-cloud#133.
//
// Argo mirrors pod-level container-status reasons into `wf.status.nodes[*].message`
// verbatim. The three kubelet reasons that produce a permanently-stuck workflow
// (workflow.Phase stays "Running" while the pod cannot start) are:
//   - ImagePullBackOff       (kubelet gave up retrying)
//   - ErrImagePull           (initial pull failure — precedes ImagePullBackOff)
//   - CreateContainerError   (image pulled but container config invalid)
//
// Returns the offending node's Message (empty when no pod node is stuck).
// Only Pod-typed nodes are considered — StepGroup / DAG parent nodes echo
// the same failure message and would produce redundant reasons.
func extractStuckPodReason(status *forgemodel.WorkflowStatusDetail) string {
	if status == nil {
		return ""
	}
	for _, node := range status.Nodes {
		if node.Type != "Pod" {
			continue
		}
		if node.Message == "" {
			continue
		}
		if containsStuckSignal(node.Message) {
			return node.Message
		}
	}
	return ""
}

// stuckSignals are the substrings that identify a pod stuck at image-pull.
// Sourced from kubelet's canonical reason strings (kubernetes/pkg/kubelet/images).
var stuckSignals = []string{
	"ImagePullBackOff",
	"ErrImagePull",
	"CreateContainerError",
	"Back-off pulling image",
}

func containsStuckSignal(msg string) bool {
	for _, sig := range stuckSignals {
		for i := 0; i+len(sig) <= len(msg); i++ {
			if msg[i:i+len(sig)] == sig {
				return true
			}
		}
	}
	return false
}

// extractStepOutputs walks `status.nodes[*]` on a workflow CR and
// returns each Pod node's output parameters keyed by displayName.
//
// Argo writes per-node outputs at `status.nodes.<nodeId>.outputs.parameters`,
// each entry being `{name, value}`. The `displayName` of the node is what
// callers use to address steps (it matches the template name like
// "sign-image-api"), so we key by displayName, not nodeId.
//
// CHG-2026-04-25-ATTESTATION-WIRE: this is the bridge between the CWT
// outputs declared in cosign.go / buildkit.go and the API's persistence
// layer. Defensive: any malformed node is skipped, never panics.
func extractStepOutputs(wfObject map[string]interface{}) map[string]map[string]string {
	if wfObject == nil {
		return nil
	}
	status, ok := wfObject["status"].(map[string]interface{})
	if !ok {
		return nil
	}
	nodes, ok := status["nodes"].(map[string]interface{})
	if !ok || len(nodes) == 0 {
		return nil
	}

	out := make(map[string]map[string]string)
	for _, raw := range nodes {
		node, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		// Only Pod-type nodes have actual outputs (Steps/DAG nodes
		// aggregate from children but don't declare their own).
		if t, _ := node["type"].(string); t != "Pod" {
			continue
		}
		display, _ := node["displayName"].(string)
		if display == "" {
			continue
		}
		outputs, ok := node["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		params, ok := outputs["parameters"].([]interface{})
		if !ok {
			continue
		}
		paramMap := make(map[string]string)
		for _, p := range params {
			pm, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := pm["name"].(string)
			value, _ := pm["value"].(string)
			if name == "" {
				continue
			}
			paramMap[name] = value
		}
		if len(paramMap) > 0 {
			out[display] = paramMap
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// infraParams is the part of the configuration every workflow the builder makes uses.
func (cfg WorkflowSubmitterConfig) infraParams() InfraParams {
	return InfraParams{
		ObjectStoreIRSA:       cfg.ObjectStoreIRSA,
		StaticAssetsBucket:    cfg.StaticAssetsBucket,
		RegistryURL:           cfg.RegistryURL,
		RegistryInsecure:      cfg.RegistryInsecure,
		BaseDomain:            cfg.BaseDomain,
		WorkspaceStorageClass: cfg.WorkspaceStorageClass,
		CosignKeyless:         cfg.CosignKeyless,
		CosignFulcioURL:       cfg.CosignFulcioURL,
		CosignRekorURL:        cfg.CosignRekorURL,
		CosignAttestEnabled:   cfg.CosignAttestEnabled,
		CosignInsecureSkipSCT: cfg.CosignInsecureSkipSCT, // T1.2 CR-1 fix
		CosignImage:           cfg.CosignImage,
		SyftImage:             cfg.SyftImage,
		Env:                   cfg.Env, // T1.1 of plan 2026-06-01-p1-security-hardening (sec-010)
	}
}
