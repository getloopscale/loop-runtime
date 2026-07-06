// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ArgoAppLabelReader reads the labels off an ArgoCD Application CR by name.
//
// Sprint B B3 (closes code-005): the legacy SyncCallbackActivator path used
// ParseArgoAppName which calls `strings.LastIndex(appName, "-")` to split
// `{tenantID}-{environment}`. Tenant IDs can contain hyphens (e.g.
// `tn-loopzero-e2e`) AND env names can contain hyphens too (`j-rollout-a`,
// `j-secrets`) — the LastIndex parse is ambiguous when both contain hyphens,
// mis-routing the callback to the wrong (tenant, env) pair.
//
// MEET-ASYNC-AMENDMENT-2026-06-02-002 documented why the prior fix attempt
// (tightening the env-name regex) was reverted (broke 11/66 journey
// fixtures). This interface unblocks the closure without forcing fixture
// migration: SyncCallbackActivator reads `loopzero.io/tenant` +
// `loopzero.io/environment` labels off the live ArgoCD App CR, falling back to
// the legacy parser only when the labels are absent (backward-compat for
// apps that pre-date commit `8854f64e` which started emitting the env label).
type ArgoAppLabelReader interface {
	GetAppLabels(ctx context.Context, appName string) (map[string]string, error)
}

// applicationGVR is the GroupVersionResource for ArgoCD Application CRs.
var argoApplicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// DynamicArgoAppLabelReader implements ArgoAppLabelReader against a live
// Kubernetes API via dynamic.Interface. The reader fetches the Application
// CR from the configured namespace (typically "argocd" on the mgmt cluster).
//
// Failure modes:
//   - App does not exist → returns wrapped NotFound error; caller falls back
//     to ParseArgoAppName.
//   - K8s API unreachable → returns the underlying error; caller falls back.
//   - App exists but has no labels → returns nil map; caller falls back.
//
// The reader does NOT cache. Each callback issues one Get against the mgmt
// API server. Volume is bounded by ArgoCD sync rate (default ~3min poll +
// webhook events), so the load is negligible vs. existing controller traffic.
type DynamicArgoAppLabelReader struct {
	client    dynamic.Interface
	namespace string
}

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
