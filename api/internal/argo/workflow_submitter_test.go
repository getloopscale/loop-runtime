// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"context"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/loopzero/loopzero/api/internal/services"
)

// These tests run against real Argo Workflows in the build cluster.
// Set BUILD_KUBECONFIG to enable (path to kubeconfig with k3d-loopzero-build context).
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
		kubeContext = "k3d-loopzero-build"
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
		kubeContext = "k3d-loopzero-build"
	}

	// Ensure per-tenant build namespaces exist before submitting workflows.
	for _, ns := range namespaces {
		ensureNamespace(t, kubeconfig, kubeContext, ns)
	}

	submitter, err := NewWorkflowSubmitter(WorkflowSubmitterConfig{
		Kubeconfig:       kubeconfig,
		Context:          kubeContext,
		RegistryURL:      "k3d-loopzero-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopzero.local",
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

	workflowID, err := submitter.Submit(ctx, services.WorkflowSpec{
		BuildID:     "test-build-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123def456",
		Namespace:   "build-acme",
		Apps: []services.AppSpec{
			{Name: "api", Type: "server", Port: 3000},
		},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if workflowID == "" {
		t.Fatal("expected non-empty workflow ID")
	}

	t.Logf("created workflow: %s", workflowID)

	// Cleanup: cancel the workflow so it doesn't run indefinitely
	if err := submitter.Cancel(ctx, "build-acme", workflowID); err != nil {
		t.Logf("warning: cleanup cancel failed: %v", err)
	}
}

func TestWorkflowSubmitter_CancelWorkflow(t *testing.T) {
	submitter := newTestSubmitter(t, "build-acme")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Submit a workflow
	workflowID, err := submitter.Submit(ctx, services.WorkflowSpec{
		BuildID:     "test-cancel-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "staging",
		CommitSHA:   "def456",
		Namespace:   "build-acme",
		Apps: []services.AppSpec{
			{Name: "web", Type: "frontend", Port: 0},
		},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Cancel it
	err = submitter.Cancel(ctx, "build-acme", workflowID)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
}

func TestWorkflowSubmitter_SubmitWithMultipleApps(t *testing.T) {
	submitter := newTestSubmitter(t, "build-acme")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workflowID, err := submitter.Submit(ctx, services.WorkflowSpec{
		BuildID:     "test-multi-001",
		TenantID:    "acme",
		ProjectID:   "monorepo",
		Environment: "production",
		CommitSHA:   "789abc",
		Namespace:   "build-acme",
		Apps: []services.AppSpec{
			{Name: "api", Type: "server", Port: 3000},
			{Name: "web", Type: "frontend", Port: 0},
			{Name: "worker", Type: "worker", Port: 0},
		},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if workflowID == "" {
		t.Fatal("expected non-empty workflow ID")
	}

	// Cleanup
	_ = submitter.Cancel(ctx, "build-acme", workflowID)
}
