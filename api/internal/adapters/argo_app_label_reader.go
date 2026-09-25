// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/getloopscale/loopzero/api/internal/release"
)

// argoApplicationGVR is the GroupVersionResource for ArgoCD Application CRs.
var argoApplicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// DynamicArgoAppLabelReader implements release.ArgoAppLabelReader against a live
// Kubernetes API via dynamic.Interface. The reader fetches the Application
// CR from the configured namespace (typically "argocd" on the mgmt cluster).
//
// Failure modes:
//   - App does not exist → returns the wrapped NotFound error.
//   - K8s API unreachable → returns the underlying error.
//   - App exists but has no labels → returns a nil map.
//
// In every case the callback cannot identify the release and leaves its
// activation to the convergence loop.
//
// The reader does NOT cache. Each callback issues one Get against the mgmt
// API server. Volume is bounded by ArgoCD sync rate (default ~3min poll +
// webhook events), so the load is negligible vs. existing controller traffic.
type DynamicArgoAppLabelReader struct {
	client    dynamic.Interface
	namespace string
}

var _ release.ArgoAppLabelReader = (*DynamicArgoAppLabelReader)(nil)

// NewDynamicArgoAppLabelReader constructs a production ArgoAppLabelReader.
// namespace is typically "argocd" — the namespace where ArgoCD Application
// CRs live on the mgmt cluster.
func NewDynamicArgoAppLabelReader(client dynamic.Interface, namespace string) *DynamicArgoAppLabelReader {
	if namespace == "" {
		namespace = "argocd"
	}
	return &DynamicArgoAppLabelReader{client: client, namespace: namespace}
}

// GetAppLabels fetches the ArgoCD Application by name and returns its
// metadata.labels. Returns nil map when the App has no labels.
func (r *DynamicArgoAppLabelReader) GetAppLabels(ctx context.Context, appName string) (map[string]string, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("argo app label reader: not configured")
	}
	app, err := r.client.Resource(argoApplicationGVR).Namespace(r.namespace).Get(ctx, appName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get argocd application %s/%s: %w", r.namespace, appName, err)
	}
	return app.GetLabels(), nil
}
