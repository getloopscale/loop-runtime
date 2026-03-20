// Package k8s provides Kubernetes client implementations.
package k8s

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	"github.com/loopzero/loopzero/api/internal/routes"
)

const tenantNamespace = "loopzero-system"

var tenantCRGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "tenants",
}

// TenantCRManager manages Tenant custom resources in Kubernetes.
// It implements routes.TenantCRManager (DIP) using the dynamic client
// to avoid importing the operator's typed SDK.
type TenantCRManager struct {
	Client dynamic.Interface
}

// NewTenantCRManager creates a TenantCRManager connected to the management cluster.
// It tries in-cluster config first (for when the API runs inside the cluster),
// then falls back to kubeconfig with the specified context.
func NewTenantCRManager(kubeconfig, kubeContext string) (*TenantCRManager, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to kubeconfig (local development)
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
		overrides := &clientcmd.ConfigOverrides{}
		if kubeContext != "" {
			overrides.CurrentContext = kubeContext
		}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s: tenant CR manager config: %w", err)
		}
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("k8s: tenant CR manager dynamic client: %w", err)
	}

	return &TenantCRManager{Client: client}, nil
}

// EnsureTenant creates or updates a Tenant CR in the loopzero-system namespace.
// It is idempotent: calling it multiple times with the same tenantID is safe.
// If the CR already exists, it updates the spec to match the desired state.
// It uses retry-on-conflict to handle concurrent modifications by the TenantController.
func (m *TenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec routes.TenantCRSpec) error {
	resource := m.Client.Resource(tenantCRGVR).Namespace(tenantNamespace)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := resource.Get(ctx, tenantID, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			// CR does not exist — create it
			desired := buildTenantCR(tenantID, spec)
			_, createErr := resource.Create(ctx, desired, metav1.CreateOptions{})
			if k8serrors.IsAlreadyExists(createErr) {
				// Another caller created it between our Get and Create — retry as update
				return k8serrors.NewConflict(
					schema.GroupResource{Group: "loopzero.io", Resource: "tenants"},
					tenantID,
					createErr,
				)
			}
			if createErr != nil {
				return fmt.Errorf("k8s: create tenant CR %q: %w", tenantID, createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("k8s: get tenant CR %q: %w", tenantID, err)
		}

		// CR exists — update spec, preserving resourceVersion for optimistic concurrency
		desired := buildTenantCR(tenantID, spec)
		desired.SetResourceVersion(existing.GetResourceVersion())
		_, err = resource.Update(ctx, desired, metav1.UpdateOptions{})
		if err != nil {
			return err // RetryOnConflict handles Conflict errors automatically
		}
		return nil
	})
}

// buildTenantCR constructs the unstructured Tenant CR matching the CRD schema
// defined in operators/api/v1/tenant_types.go.
func buildTenantCR(tenantID string, spec routes.TenantCRSpec) *unstructured.Unstructured {
	plan := spec.Plan
	if plan == "" {
		plan = "starter"
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "loopzero.io/v1",
			"kind":       "Tenant",
			"metadata": map[string]interface{}{
				"name":      tenantID,
				"namespace": tenantNamespace,
			},
			"spec": map[string]interface{}{
				"displayName": tenantID,
				"plan":        plan,
				"environments": []interface{}{
					map[string]interface{}{
						"name":    "production",
						"cluster": "workload",
					},
				},
				"gitops": map[string]interface{}{
					"repoName": fmt.Sprintf("loopzero-tenant-%s", tenantID),
					"branch":   "main",
				},
				"preview": map[string]interface{}{
					"maxEnvironments": int64(3),
					"ttlHours":        int64(72),
				},
				"limits": map[string]interface{}{
					"maxCPU":              "4",
					"maxMemory":           "8Gi",
					"maxStorage":          "20Gi",
					"maxBuildConcurrency": int64(2),
				},
			},
		},
	}
}
