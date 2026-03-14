package argocd

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// RealProvisioner manages ArgoCD Application CRs using the Kubernetes API.
type RealProvisioner struct {
	client    dynamic.Interface
	namespace string // ArgoCD namespace (typically "argocd")
}

// NewRealProvisioner creates a provisioner that manages real ArgoCD Application CRs.
func NewRealProvisioner(namespace string) (*RealProvisioner, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("argocd provisioner: get config: %w", err)
	}

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("argocd provisioner: dynamic client: %w", err)
	}

	return &RealProvisioner{
		client:    client,
		namespace: namespace,
	}, nil
}

// EnsureApplication creates or updates an ArgoCD Application CR.
func (p *RealProvisioner) EnsureApplication(ctx context.Context, spec ApplicationSpec) error {
	app := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]interface{}{
				"name":      spec.Name,
				"namespace": p.namespace,
				"labels": map[string]interface{}{
					"loopzero.io/tenant":     spec.TenantID,
					"loopzero.io/managed-by": "tenant-controller",
				},
				"finalizers": []interface{}{
					"resources-finalizer.argocd.argoproj.io",
				},
			},
			"spec": map[string]interface{}{
				"project": "default",
				"source": map[string]interface{}{
					"repoURL":        spec.RepoURL,
					"targetRevision": spec.TargetRevision,
					"path":           spec.Path,
					"directory": map[string]interface{}{
						"recurse": true,
					},
				},
				"destination": map[string]interface{}{
					"server":    spec.DestinationURL,
					"namespace": spec.DestinationNS,
				},
				"syncPolicy": map[string]interface{}{
					"syncOptions": []interface{}{
						"CreateNamespace=false",
					},
				},
			},
		},
	}

	// Try to get existing application
	existing, err := p.client.Resource(applicationGVR).Namespace(p.namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		// Create new
		_, err = p.client.Resource(applicationGVR).Namespace(p.namespace).Create(ctx, app, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("argocd: create application %s: %w", spec.Name, err)
		}
		return nil
	}

	// Update existing — preserve resourceVersion
	app.SetResourceVersion(existing.GetResourceVersion())
	_, err = p.client.Resource(applicationGVR).Namespace(p.namespace).Update(ctx, app, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("argocd: update application %s: %w", spec.Name, err)
	}

	return nil
}

// DeleteApplications removes all ArgoCD Application CRs for a tenant.
func (p *RealProvisioner) DeleteApplications(ctx context.Context, tenantID string) error {
	list, err := p.client.Resource(applicationGVR).Namespace(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("loopzero.io/tenant=%s", tenantID),
	})
	if err != nil {
		return fmt.Errorf("argocd: list applications for tenant %s: %w", tenantID, err)
	}

	for _, app := range list.Items {
		if err := p.client.Resource(applicationGVR).Namespace(p.namespace).Delete(ctx, app.GetName(), metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("argocd: delete application %s: %w", app.GetName(), err)
		}
	}

	return nil
}
