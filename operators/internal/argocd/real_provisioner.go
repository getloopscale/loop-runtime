// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/loopzero/loopzero/operators/internal/constants"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

var appProjectGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "appprojects",
}

// Compile-time interface compliance check.
var _ Provisioner = (*RealProvisioner)(nil)

// RealProvisioner manages ArgoCD Application CRs using the Kubernetes API.
type RealProvisioner struct {
	client    dynamic.Interface
	namespace string // ArgoCD namespace (typically "argocd")
}

// NewRealProvisioner creates a provisioner that manages real ArgoCD Application CRs.
// The dynamic client is injected rather than created internally (ARCH-003: DIP compliance).
// In cmd/main.go, create the client via dynamic.NewForConfig(mgr.GetConfig()) and pass it here.
func NewRealProvisioner(namespace string, client dynamic.Interface) *RealProvisioner {
	return &RealProvisioner{
		client:    client,
		namespace: namespace,
	}
}

// EnsureAppProject creates or updates an ArgoCD AppProject CR for tenant RBAC isolation (MOD-5).
func (p *RealProvisioner) EnsureAppProject(ctx context.Context, spec AppProjectSpec) error {
	// Build destination list scoped to tenant namespaces only.
	// Each destination restricts to the tenant's environment namespaces,
	// preventing cross-tenant deployment via ArgoCD.
	destinations := make([]interface{}, 0, len(spec.DestinationNamespaces))
	for _, ns := range spec.DestinationNamespaces {
		destinations = append(destinations, map[string]interface{}{
			"namespace": ns,
			"server":    "*",
		})
	}
	// Fallback: if no specific namespaces provided, allow tenant-prefixed namespaces.
	if len(destinations) == 0 {
		destinations = append(destinations, map[string]interface{}{
			"namespace": fmt.Sprintf("loopzero-%s-*", spec.TenantID),
			"server":    "*",
		})
	}

	proj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "AppProject",
			"metadata": map[string]interface{}{
				"name":      spec.Name,
				"namespace": p.namespace,
				"labels": map[string]interface{}{
					constants.LabelTenant:    spec.TenantID,
					constants.LabelManagedBy: constants.ManagedByTenantController,
				},
			},
			"spec": map[string]interface{}{
				"sourceRepos":  toInterfaceSlice(spec.SourceRepos),
				"destinations": destinations,
				"clusterResourceWhitelist": []interface{}{
					map[string]interface{}{
						"group": "",
						"kind":  "Namespace",
					},
				},
			},
		},
	}

	existing, err := p.client.Resource(appProjectGVR).Namespace(p.namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("argocd: get appproject %s: %w", spec.Name, err)
		}
		_, err = p.client.Resource(appProjectGVR).Namespace(p.namespace).Create(ctx, proj, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("argocd: create appproject %s: %w", spec.Name, err)
		}
		return nil
	}

	proj.SetResourceVersion(existing.GetResourceVersion())
	_, err = p.client.Resource(appProjectGVR).Namespace(p.namespace).Update(ctx, proj, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("argocd: update appproject %s: %w", spec.Name, err)
	}
	return nil
}

// toInterfaceSlice converts a string slice to an interface slice for unstructured objects.
func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

// buildApplicationObject constructs the ArgoCD Application unstructured object.
// Pure function (no side effects) — testable without a K8s client.
//
// Sync policy: manual sync only (automated: nil). The API controls sync timing
// via POST /api/v1/applications/{name}/sync. Auto-sync (selfHeal/prune) creates
// race conditions with the API's manual trigger. See MEET-2026-03-27-012.
//
// CreateNamespace: false — the TenantController creates namespaces, not ArgoCD.
func buildApplicationObject(spec ApplicationSpec) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]interface{}{
				"name": spec.Name,
				"labels": map[string]interface{}{
					constants.LabelTenant:    spec.TenantID,
					constants.LabelManagedBy: constants.ManagedByTenantController,
				},
				"annotations": map[string]interface{}{
					// ArgoCD Notifications: subscribe to sync-completed trigger.
					// When Synced + Healthy, ArgoCD calls POST /internal/argocd-callback
					// on the API to activate the release (push-based, replaces polling).
					"notifications.argoproj.io/subscribe.on-sync-healthy.loopzero-api": "",
				},
				"finalizers": []interface{}{
					constants.ArgoCDResourcesFinalizer,
				},
			},
			"spec": map[string]interface{}{
				"project": spec.TenantID,
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
					"automated": nil,
					"syncOptions": []interface{}{
						"CreateNamespace=false",
						"PrunePropagationPolicy=foreground",
						"PruneLast=true",
					},
				},
			},
		},
	}
}

// EnsureApplication creates or updates an ArgoCD Application CR.
func (p *RealProvisioner) EnsureApplication(ctx context.Context, spec ApplicationSpec) error {
	app := buildApplicationObject(spec)
	app.Object["metadata"].(map[string]interface{})["namespace"] = p.namespace

	// Try to get existing application
	existing, err := p.client.Resource(applicationGVR).Namespace(p.namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("argocd: get application %s: %w", spec.Name, err)
		}
		// Resource does not exist — create it.
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
		LabelSelector: fmt.Sprintf("%s=%s", constants.LabelTenant, tenantID),
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

// DeleteAppProject removes the ArgoCD AppProject CR for a tenant.
func (p *RealProvisioner) DeleteAppProject(ctx context.Context, tenantID string) error {
	err := p.client.Resource(appProjectGVR).Namespace(p.namespace).Delete(ctx, tenantID, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("argocd: delete appproject %s: %w", tenantID, err)
	}
	return nil
}
