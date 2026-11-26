// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
	forgemodel "github.com/getloopscale/argo-workflows-go/model"

	corev1 "k8s.io/api/core/v1"
	apiextclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// These tests run against real Argo Workflows in the build cluster.
// Set BUILD_KUBECONFIG to enable (path to kubeconfig with k3d-loopruntime-build context).
// Without it, tests are skipped — not faked.

func skipIfNoBuildCluster(t *testing.T) string {
	t.Helper()
	kubeconfig := os.Getenv("BUILD_KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		defaultPath := home + "/.kube/config"
		if _, err := os.Stat(defaultPath); err != nil {
			t.Skip("BUILD_KUBECONFIG not set and ~/.kube/config not found")
		}
		kubeconfig = defaultPath
	}

	// Verify the build cluster is actually reachable before running integration tests.
	kubeContext := os.Getenv("BUILD_KUBE_CONTEXT")
	if kubeContext == "" {
		kubeContext = "k3d-loopruntime-build"
	}
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		t.Skipf("build cluster not configured: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Skipf("build cluster client failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		t.Skipf("build cluster not reachable: %v", err)
	}

	// 2026-04-30 LIVE finding: a build cluster with valid kubeconfig but
	// missing Argo Workflows CRDs (e.g., after a k3d cluster stop/start
	// where etcd wasn't persisted, or before bootstrap completes) caused
	// these tests to fail confusingly with `the server could not find the
	// requested resource` instead of skipping. The skip protocol promises
	// "tests are skipped — not faked". Probe the CRD before proceeding.
	apiextClient, err := apiextclientset.NewForConfig(cfg)
	if err != nil {
		t.Skipf("apiext client failed: %v", err)
	}
	if _, err := apiextClient.ApiextensionsV1().CustomResourceDefinitions().Get(
		ctx, "workflows.argoproj.io", metav1.GetOptions{},
	); err != nil {
		t.Skipf("Argo Workflows CRD not installed in build cluster: %v", err)
	}

	return kubeconfig
}

// ensureNamespace creates the namespace if it doesn't exist, for integration tests.
func ensureNamespace(t *testing.T, kubeconfig, kubeContext, namespace string) {
	t.Helper()
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		t.Fatalf("failed to load kubeconfig for namespace creation: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create clientset for namespace creation: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		t.Fatalf("failed to create namespace %s: %v", namespace, err)
	}
}

func newTestSubmitter(t *testing.T, namespaces ...string) *WorkflowSubmitter {
	t.Helper()
	kubeconfig := skipIfNoBuildCluster(t)

	kubeContext := os.Getenv("BUILD_KUBE_CONTEXT")
	if kubeContext == "" {
		kubeContext = "k3d-loopruntime-build"
	}

	// Ensure per-tenant build namespaces exist before submitting workflows.
	for _, ns := range namespaces {
		ensureNamespace(t, kubeconfig, kubeContext, ns)
	}

	submitter, err := NewWorkflowSubmitter(WorkflowSubmitterConfig{
		Kubeconfig:       kubeconfig,
		Context:          kubeContext,
		RegistryURL:      "k3d-loopruntime-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopruntime.local",
	})
	if err != nil {
		t.Fatalf("failed to create workflow submitter: %v", err)
	}
	return submitter
}

func TestWorkflowSubmitter_SubmitCreatesWorkflow(t *testing.T) {
	submitter := newTestSubmitter(t, "build-acme")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	apps := []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000},
	}
	workflowID, err := submitter.Submit(ctx, deploycontracts.WorkflowSpec{
		BuildID:     "test-build-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123def456",
		Namespace:   "build-acme",
		Apps:        apps,
		Plan:        planFromApps(apps),
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if workflowID == "" {
		t.Fatal("expected non-empty workflow ID")
	}

	t.Logf("created workflow: %s", workflowID)

	// Cleanup: cancel then delete the workflow to free ResourceQuota
	if err := submitter.Cancel(ctx, "build-acme", workflowID); err != nil {
		t.Logf("warning: cleanup cancel failed: %v", err)
	}
	if err := submitter.Delete(ctx, "build-acme", workflowID); err != nil {
		t.Logf("warning: cleanup delete failed: %v", err)
	}
}

func TestWorkflowSubmitter_CancelWorkflow(t *testing.T) {
	submitter := newTestSubmitter(t, "build-acme")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Submit a workflow.
	// The fixed LoopZeroKit stack is server|worker only, every app built as a
	// container. Use a single server app for cancel testing.
	apps := []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000},
	}
	workflowID, err := submitter.Submit(ctx, deploycontracts.WorkflowSpec{
		BuildID:     "test-cancel-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "staging",
		CommitSHA:   "def456",
		Namespace:   "build-acme",
		Apps:        apps,
		Plan:        planFromApps(apps),
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Cancel it
	err = submitter.Cancel(ctx, "build-acme", workflowID)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Cleanup: delete the workflow to free ResourceQuota
	if err := submitter.Delete(ctx, "build-acme", workflowID); err != nil {
		t.Logf("warning: cleanup delete failed: %v", err)
	}
}

func TestWorkflowSubmitter_SubmitWithMultipleApps(t *testing.T) {
	submitter := newTestSubmitter(t, "build-acme")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The fixed LoopZeroKit stack is server|worker only, so the multi-app
	// fixture exercises server + worker.
	apps := []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000},
		{Name: "worker", Type: "worker", Port: 0},
	}
	workflowID, err := submitter.Submit(ctx, deploycontracts.WorkflowSpec{
		BuildID:     "test-multi-001",
		TenantID:    "acme",
		ProjectID:   "monorepo",
		Environment: "production",
		CommitSHA:   "789abc",
		Namespace:   "build-acme",
		Apps:        apps,
		Plan:        planFromApps(apps),
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if workflowID == "" {
		t.Fatal("expected non-empty workflow ID")
	}

	// Cleanup: cancel then delete to free ResourceQuota
	_ = submitter.Cancel(ctx, "build-acme", workflowID)
	if err := submitter.Delete(ctx, "build-acme", workflowID); err != nil {
		t.Logf("warning: cleanup delete failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Typed status parsing — sidecar false-failure detection via forge helpers
// Tests now use forge.ParseWorkflowStatusFromUnstructured and
// forge.AllPodNodesExitedZero (typed API replaces raw unstructured access).
// ---------------------------------------------------------------------------

func TestParseStatusAndAllPodNodesExitedZero_AllExitZero(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Failed",
			"nodes": map[string]interface{}{
				"dag-node": map[string]interface{}{
					"type": "DAG",
				},
				"pod-1": map[string]interface{}{
					"type":    "Pod",
					"outputs": map[string]interface{}{"exitCode": "0"},
				},
				"pod-2": map[string]interface{}{
					"type":    "Pod",
					"outputs": map[string]interface{}{"exitCode": "0"},
				},
			},
		},
	}
	status, err := forge.ParseWorkflowStatusFromUnstructured(obj)
	if err != nil {
		t.Fatalf("ParseWorkflowStatusFromUnstructured: %v", err)
	}
	if !forge.AllPodNodesExitedZero(status) {
		t.Error("expected true when all pods have exitCode 0")
	}
}

func TestParseStatusAndAllPodNodesExitedZero_OnePodFailed(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Failed",
			"nodes": map[string]interface{}{
				"pod-1": map[string]interface{}{
					"type":    "Pod",
					"outputs": map[string]interface{}{"exitCode": "0"},
				},
				"pod-2": map[string]interface{}{
					"type":    "Pod",
					"outputs": map[string]interface{}{"exitCode": "1"},
				},
			},
		},
	}
	status, err := forge.ParseWorkflowStatusFromUnstructured(obj)
	if err != nil {
		t.Fatalf("ParseWorkflowStatusFromUnstructured: %v", err)
	}
	if forge.AllPodNodesExitedZero(status) {
		t.Error("expected false when a pod has exitCode 1")
	}
}

func TestParseStatusAndAllPodNodesExitedZero_NoStatus(t *testing.T) {
	obj := map[string]interface{}{}
	status, err := forge.ParseWorkflowStatusFromUnstructured(obj)
	if err != nil {
		t.Fatalf("ParseWorkflowStatusFromUnstructured: %v", err)
	}
	if forge.AllPodNodesExitedZero(status) {
		t.Error("expected false when no status exists")
	}
}

func TestParseStatusAndAllPodNodesExitedZero_NoPods(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Failed",
			"nodes": map[string]interface{}{
				"dag-node": map[string]interface{}{
					"type": "DAG",
				},
			},
		},
	}
	status, err := forge.ParseWorkflowStatusFromUnstructured(obj)
	if err != nil {
		t.Fatalf("ParseWorkflowStatusFromUnstructured: %v", err)
	}
	if forge.AllPodNodesExitedZero(status) {
		t.Error("expected false when no Pod-type nodes exist")
	}
}

// CHG-2026-04-25-ATTESTATION-WIRE Phase 2 (AC-2.1): the parser must
// extract `status.nodes[*].outputs.parameters[]` keyed by displayName
// so the API can correlate signature/digest values back to the app
// they belong to.
func TestExtractStepOutputs_ParsesCosignAndBuildkitOutputs(t *testing.T) {
	wf := map[string]interface{}{
		"status": map[string]interface{}{
			"nodes": map[string]interface{}{
				"loopruntime-build-build-abc-sign-1": map[string]interface{}{
					"type":        "Pod",
					"displayName": "sign-image-api",
					"outputs": map[string]interface{}{
						"parameters": []interface{}{
							map[string]interface{}{"name": "signature", "value": "registry.example.com/org/api:abc@sha256:deadbeef"},
							map[string]interface{}{"name": "digest", "value": "sha256:deadbeef"},
						},
					},
				},
				"loopruntime-build-build-abc-build-1": map[string]interface{}{
					"type":        "Pod",
					"displayName": "build-image-api",
					"outputs": map[string]interface{}{
						"parameters": []interface{}{
							map[string]interface{}{"name": "digest", "value": "sha256:deadbeef"},
						},
					},
				},
				"loopruntime-build-build-abc-clone-1": map[string]interface{}{
					"type":        "Pod",
					"displayName": "clone-repo",
					// No outputs declared — must be skipped.
				},
				"loopruntime-build-build-abc": map[string]interface{}{
					// Workflow root — DAG-type, NOT Pod. Must be skipped
					// even if it has aggregate outputs.
					"type":        "DAG",
					"displayName": "loopruntime-build",
				},
			},
		},
	}

	got := extractStepOutputs(wf)
	if len(got) != 2 {
		t.Fatalf("expected 2 steps with outputs, got %d: %#v", len(got), got)
	}

	signOutputs, ok := got["sign-image-api"]
	if !ok {
		t.Fatal("missing sign-image-api in extracted outputs")
	}
	if signOutputs["signature"] != "registry.example.com/org/api:abc@sha256:deadbeef" {
		t.Errorf("sign-image-api signature: got %q", signOutputs["signature"])
	}
	if signOutputs["digest"] != "sha256:deadbeef" {
		t.Errorf("sign-image-api digest: got %q", signOutputs["digest"])
	}

	buildOutputs, ok := got["build-image-api"]
	if !ok {
		t.Fatal("missing build-image-api in extracted outputs")
	}
	if buildOutputs["digest"] != "sha256:deadbeef" {
		t.Errorf("build-image-api digest: got %q", buildOutputs["digest"])
	}

	if _, ok := got["clone-repo"]; ok {
		t.Error("clone-repo (no outputs) should not appear in result")
	}
	if _, ok := got["loopruntime-build"]; ok {
		t.Error("DAG-type root node should not appear in result (only Pod nodes have real outputs)")
	}
}

// AC-2.2: legacy/missing inputs MUST NOT panic, MUST return nil
// (so callers can `len() == 0` and degrade gracefully).
func TestExtractStepOutputs_DefensiveAgainstMissingFields(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]interface{}
	}{
		{"nil object", nil},
		{"no status", map[string]interface{}{"spec": map[string]interface{}{}}},
		{"status not a map", map[string]interface{}{"status": "running"}},
		{"no nodes", map[string]interface{}{"status": map[string]interface{}{"phase": "Succeeded"}}},
		{"empty nodes", map[string]interface{}{"status": map[string]interface{}{"nodes": map[string]interface{}{}}}},
		{
			"node missing outputs",
			map[string]interface{}{
				"status": map[string]interface{}{
					"nodes": map[string]interface{}{
						"n1": map[string]interface{}{"type": "Pod", "displayName": "step1"},
					},
				},
			},
		},
		{
			"node outputs not a map",
			map[string]interface{}{
				"status": map[string]interface{}{
					"nodes": map[string]interface{}{
						"n1": map[string]interface{}{"type": "Pod", "displayName": "step1", "outputs": "string"},
					},
				},
			},
		},
		{
			"parameter missing name",
			map[string]interface{}{
				"status": map[string]interface{}{
					"nodes": map[string]interface{}{
						"n1": map[string]interface{}{
							"type":        "Pod",
							"displayName": "step1",
							"outputs": map[string]interface{}{
								"parameters": []interface{}{
									map[string]interface{}{"value": "x"},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractStepOutputs(tc.obj)
			if len(got) != 0 {
				t.Errorf("expected nil/empty for %q, got %#v", tc.name, got)
			}
		})
	}
}

// AC-2.3: GetStatus consumers must compile (the new field is additive).
// This test is a compile-time guard: if WorkflowStatus.Outputs is
// removed or renamed, this won't build.
func TestWorkflowStatus_OutputsField_CompileGuard(t *testing.T) {
	var s deploycontracts.WorkflowStatus
	s.Outputs = map[string]map[string]string{
		"sign-image-api": {"signature": "x"},
	}
	if s.Outputs["sign-image-api"]["signature"] != "x" {
		t.Fatal("compile guard inconsistent")
	}
}

// TestExtractStuckPodReason — Red Team loopruntime-cloud#133 unit coverage.
// Locks the truth table of the pod-stuck detector so a future refactor
// cannot silently regress the ImagePullBackOff / ErrImagePull / CreateContainerError
// signals that gate the reaper's M-03 override.
func TestExtractStuckPodReason(t *testing.T) {
	tests := []struct {
		name   string
		status *forgemodel.WorkflowStatusDetail
		want   string
	}{
		{
			name:   "nil status returns empty",
			status: nil,
			want:   "",
		},
		{
			name: "no nodes returns empty",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
			},
			want: "",
		},
		{
			name: "pod node with ImagePullBackOff message returns the message",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "Pod", Message: "ImagePullBackOff: Back-off pulling image \"registry.digitalocean.com/loopruntime-system/loopruntime-cosign-signer:v0.1.0\""},
				},
			},
			want: "ImagePullBackOff: Back-off pulling image \"registry.digitalocean.com/loopruntime-system/loopruntime-cosign-signer:v0.1.0\"",
		},
		{
			name: "pod node with ErrImagePull returns the message",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "Pod", Message: "ErrImagePull: rpc error 401"},
				},
			},
			want: "ErrImagePull: rpc error 401",
		},
		{
			name: "pod node with CreateContainerError returns the message",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "Pod", Message: "CreateContainerError: invalid image config"},
				},
			},
			want: "CreateContainerError: invalid image config",
		},
		{
			name: "non-Pod node with stuck signal is IGNORED (avoids duplicate reasons from parent StepGroup / DAG nodes)",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "StepGroup", Message: "ImagePullBackOff: mirror"},
				},
			},
			want: "",
		},
		{
			name: "pod node with a benign progress message returns empty (not a stuck signal)",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "Pod", Message: "Waiting for pod to be scheduled"},
				},
			},
			want: "",
		},
		{
			name: "pod node with empty message returns empty",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "Pod", Message: ""},
				},
			},
			want: "",
		},
		{
			name: "Back-off pulling image (kubelet canonical) is a stuck signal",
			status: &forgemodel.WorkflowStatusDetail{
				Phase: "Running",
				Nodes: map[string]forgemodel.NodeStatus{
					"n1": {Type: "Pod", Message: "Back-off pulling image \"ghcr.io/foo:bar\""},
				},
			},
			want: "Back-off pulling image \"ghcr.io/foo:bar\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStuckPodReason(tt.status)
			if got != tt.want {
				t.Errorf("extractStuckPodReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
