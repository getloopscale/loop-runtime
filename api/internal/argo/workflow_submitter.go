// Package argo provides real Argo Workflows integration for build pipeline execution.
package argo

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/loopzero/loopzero/api/internal/services"
)

var workflowGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "workflows",
}

// WorkflowSubmitterConfig holds configuration for connecting to the build cluster.
type WorkflowSubmitterConfig struct {
	Kubeconfig   string // Path to kubeconfig file
	Context      string // Kube context (e.g., "k3d-loopzero-build")
	Namespace    string // Namespace for workflows (e.g., "argo")
	TemplateName string // WorkflowTemplate name (e.g., "loopzero-build-nodejs-v1")

	// Infrastructure parameters passed to every workflow
	CallbackURL    string // e.g., "http://10.0.1.3:30001/api/v1/builds" (cross-cluster)
	CallbackSecret string // Shared secret for build callback auth
	GiteaURL       string // e.g., "http://10.0.1.3:30000" (cross-cluster for workflow)
	GiteaToken     string // Admin token for Gitea access
	MinIOEndpoint  string // e.g., "minio.loopzero-system.svc:9000"
	MinIOAccessKey string
	MinIOSecretKey string
	RegistryURL    string // e.g., "k3d-loopzero-registry:5000"
}

// WorkflowSubmitter implements services.WorkflowSubmitter using real Argo Workflows.
// It creates Workflow CRs in the build cluster that reference a WorkflowTemplate.
type WorkflowSubmitter struct {
	client       dynamic.Interface
	namespace    string
	templateName string
	infraParams  WorkflowSubmitterConfig
}

// NewWorkflowSubmitter creates a WorkflowSubmitter connected to the build cluster.
func NewWorkflowSubmitter(cfg WorkflowSubmitterConfig) (*WorkflowSubmitter, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.Context != "" {
		overrides.CurrentContext = cfg.Context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("argo: build cluster config: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("argo: dynamic client: %w", err)
	}

	return &WorkflowSubmitter{
		client:       client,
		namespace:    cfg.Namespace,
		templateName: cfg.TemplateName,
		infraParams:  cfg,
	}, nil
}

// Submit creates a Workflow CR in the build cluster that references the WorkflowTemplate.
func (s *WorkflowSubmitter) Submit(ctx context.Context, spec services.WorkflowSpec) (string, error) {
	appsJSON, err := json.Marshal(spec.Apps)
	if err != nil {
		return "", fmt.Errorf("argo: marshal apps: %w", err)
	}

	// Build the Workflow CR as unstructured to avoid Argo SDK dependency
	workflow := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Workflow",
			"metadata": map[string]interface{}{
				"generateName": fmt.Sprintf("loopzero-build-%s-", spec.BuildID),
				"namespace":    s.namespace,
				"labels": map[string]interface{}{
					"loopzero.io/tenant":      spec.TenantID,
					"loopzero.io/project":     spec.ProjectID,
					"loopzero.io/environment": spec.Environment,
					"loopzero.io/build-id":    spec.BuildID,
				},
			},
			"spec": map[string]interface{}{
				"workflowTemplateRef": map[string]interface{}{
					"name": s.templateName,
				},
				"arguments": map[string]interface{}{
					"parameters": []interface{}{
						param("tenant-id", spec.TenantID),
						param("project-id", spec.ProjectID),
						param("environment", spec.Environment),
						param("commit-sha", spec.CommitSHA),
						param("apps", string(appsJSON)),
						param("build-id", spec.BuildID),
						param("correlation-id", spec.CorrelationID),
						param("api-callback-url", s.infraParams.CallbackURL),
						param("callback-secret", s.infraParams.CallbackSecret),
						param("gitea-url", s.infraParams.GiteaURL),
						param("gitea-token", s.infraParams.GiteaToken),
						param("minio-endpoint", s.infraParams.MinIOEndpoint),
						param("minio-access-key", s.infraParams.MinIOAccessKey),
						param("minio-secret-key", s.infraParams.MinIOSecretKey),
						param("registry-url", s.infraParams.RegistryURL),
					},
				},
			},
		},
	}

	created, err := s.client.Resource(workflowGVR).Namespace(s.namespace).Create(ctx, workflow, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("argo: create workflow: %w", err)
	}

	return created.GetName(), nil
}

// Cancel stops a running workflow by setting its shutdown strategy.
func (s *WorkflowSubmitter) Cancel(ctx context.Context, workflowID string) error {
	patch := []byte(`{"spec":{"shutdown":"Stop"}}`)
	_, err := s.client.Resource(workflowGVR).Namespace(s.namespace).Patch(
		ctx, workflowID, "application/merge-patch+json", patch, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("argo: cancel workflow %s: %w", workflowID, err)
	}
	return nil
}

func param(name, value string) map[string]interface{} {
	return map[string]interface{}{
		"name":  name,
		"value": value,
	}
}
