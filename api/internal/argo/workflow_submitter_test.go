package argo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/services"
)

// These tests run against real Argo Workflows in the build cluster.
// Set BUILD_KUBECONFIG to enable (path to kubeconfig with k3d-loopzero-build context).
// Without it, tests are skipped — not faked.

func skipIfNoBuildCluster(t *testing.T) string {
	t.Helper()
	kubeconfig := os.Getenv("BUILD_KUBECONFIG")
	if kubeconfig == "" {
		// Try default kubeconfig with build context
		home, _ := os.UserHomeDir()
		defaultPath := home + "/.kube/config"
		if _, err := os.Stat(defaultPath); err != nil {
			t.Skip("BUILD_KUBECONFIG not set and ~/.kube/config not found")
		}
		return defaultPath
	}
	return kubeconfig
}

func newTestSubmitter(t *testing.T) *WorkflowSubmitter {
	t.Helper()
	kubeconfig := skipIfNoBuildCluster(t)

	context := os.Getenv("BUILD_KUBE_CONTEXT")
	if context == "" {
		context = "k3d-loopzero-build"
	}

	submitter, err := NewWorkflowSubmitter(WorkflowSubmitterConfig{
		Kubeconfig:     kubeconfig,
		Context:        context,
		Namespace:      "argo",
		TemplateName:   "loopzero-build-nodejs-v1",
		CallbackSecret: "test-secret",
		GiteaURL:       "http://gitea-http.loopzero-system.svc.cluster.local:3000",
		GiteaToken:     "test-token",
		MinIOEndpoint:  "minio.loopzero-system.svc.cluster.local:9000",
		MinIOAccessKey: "loopzero-admin",
		MinIOSecretKey: "loopzero-dev-password",
		RegistryURL:    "k3d-loopzero-registry:5000",
	})
	if err != nil {
		t.Fatalf("failed to create workflow submitter: %v", err)
	}
	return submitter
}

func TestWorkflowSubmitter_SubmitCreatesWorkflow(t *testing.T) {
	submitter := newTestSubmitter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workflowID, err := submitter.Submit(ctx, services.WorkflowSpec{
		BuildID:     "test-build-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123def456",
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
	if err := submitter.Cancel(ctx, workflowID); err != nil {
		t.Logf("warning: cleanup cancel failed: %v", err)
	}
}

func TestWorkflowSubmitter_CancelWorkflow(t *testing.T) {
	submitter := newTestSubmitter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Submit a workflow
	workflowID, err := submitter.Submit(ctx, services.WorkflowSpec{
		BuildID:     "test-cancel-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "staging",
		CommitSHA:   "def456",
		Apps: []services.AppSpec{
			{Name: "web", Type: "frontend", Port: 0},
		},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Cancel it
	err = submitter.Cancel(ctx, workflowID)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
}

func TestWorkflowSubmitter_SubmitWithMultipleApps(t *testing.T) {
	submitter := newTestSubmitter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workflowID, err := submitter.Submit(ctx, services.WorkflowSpec{
		BuildID:     "test-multi-001",
		TenantID:    "acme",
		ProjectID:   "monorepo",
		Environment: "production",
		CommitSHA:   "789abc",
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
	_ = submitter.Cancel(ctx, workflowID)
}
