// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// previewEnvCRGVR is the canonical GroupVersionResource for the
// PreviewEnvironment custom resource. Lives in the SAME tenantNamespace
// as Tenant and Environment CRs (loopzero-system).
var previewEnvCRGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "previewenvironments",
}

// PreviewCRSpec is the minimal subset of PreviewEnvironment.spec the
// API submits — kept duplicated from services.PreviewCRSpec to avoid
// the cycle k8s ↔ services. Same field names.
type PreviewCRSpec struct {
	Name      string
	TenantID  string
	ProjectID string
	PRNumber  int
	Branch    string
	ReleaseID string
	ExpiresAt time.Time
}

// PreviewCRManager manages PreviewEnvironment custom resources in the
// management cluster. Same pattern (and same dynamic client) as
// TenantCRManager — the two share the tenantNamespace constant.
//
// LIVE catch 2026-06-07 J-23 bug #4: PreviewCRCreator interface in
// services had no production implementation. Adapter now lives here
// (next to TenantCRManager) rather than in api/cmd/ so the namespace
// + GVR + dynamic client wiring is canonical, not duplicated.
type PreviewCRManager struct {
	Client dynamic.Interface
}

// NewPreviewCRManager constructs a PreviewCRManager from an existing
// dynamic client. The same Client used by TenantCRManager is fine
// (and preferred — both target the mgmt cluster).
func NewPreviewCRManager(client dynamic.Interface) *PreviewCRManager {
	return &PreviewCRManager{Client: client}
}

// EnsurePreviewCR is upsert: tries Create; on AlreadyExists patches
// `spec` (PR sync to a new commit reuses the same preview row + CR
// name). Idempotent — safe to call from a webhook retry.
func (m *PreviewCRManager) EnsurePreviewCR(ctx context.Context, spec PreviewCRSpec) error {
	resource := m.Client.Resource(previewEnvCRGVR).Namespace(tenantNamespace)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "loopzero.io",
		Version: "v1",
		Kind:    "PreviewEnvironment",
	})
	obj.SetName(spec.Name)
	obj.SetNamespace(tenantNamespace)
	// LIVE catch 2026-06-07 J-23 bug #5: DeletePreviewCR needs to find
	// the CR by (tenant, project, pr) — but the CR's metadata.name is
	// the DB row ID (prev-<uuid>), not a synthetic shape derivable from
	// the trio. Without these labels the close-PR path can't find the
	// CR to delete (silently returns NotFound on a synthetic name),
	// leaving the namespace + ArgoCD App orphaned forever. The labels
	// give DeletePreviewCR a deterministic label-selector lookup.
	obj.SetLabels(map[string]string{
		"loopzero.io/tenant":     spec.TenantID,
		"loopzero.io/project":    spec.ProjectID,
		"loopzero.io/pr-number":  fmt.Sprintf("%d", spec.PRNumber),
		"loopzero.io/managed-by": "loopzero-api",
	})
	obj.Object["spec"] = map[string]interface{}{
		"tenantId":  spec.TenantID,
		"projectId": spec.ProjectID,
		"prNumber":  int64(spec.PRNumber),
		"branch":    spec.Branch,
		"releaseId": spec.ReleaseID,
		"expiresAt": metav1.Time{Time: spec.ExpiresAt}.Format(time.RFC3339),
	}

	_, err := resource.Create(ctx, obj, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !errors.IsAlreadyExists(err) {
		return fmt.Errorf("preview CR create: %w", err)
	}

	existing, getErr := resource.Get(ctx, spec.Name, metav1.GetOptions{})
	if getErr != nil {
		return fmt.Errorf("preview CR get-after-conflict: %w", getErr)
	}
	existing.Object["spec"] = obj.Object["spec"]
	if _, updErr := resource.Update(ctx, existing, metav1.UpdateOptions{}); updErr != nil {
		return fmt.Errorf("preview CR update: %w", updErr)
	}
	return nil
}

// GetPhases returns the operator-observed phase per preview ID. Used
// by PreviewHandler.List to enrich the dashboard /previews response
// without requiring a CR-to-PG writeback path.
//
// Missing CRs (NotFound) are returned as absent entries — caller falls
// back to the DB status (creating) which is honest stale.
func (m *PreviewCRManager) GetPhases(ctx context.Context, previewIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(previewIDs))
	if len(previewIDs) == 0 {
		return out, nil
	}
	resource := m.Client.Resource(previewEnvCRGVR).Namespace(tenantNamespace)
	for _, id := range previewIDs {
		obj, err := resource.Get(ctx, id, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return out, fmt.Errorf("get preview CR %q: %w", id, err)
		}
		phase, found, _ := unstructured.NestedString(obj.Object, "status", "phase")
		if found && phase != "" {
			out[id] = phase
		}
	}
	return out, nil
}

// DeletePreviewCR is called when the PR closes. Idempotent — NotFound
// is not an error (closing a PR that never had a preview).
//
// LIVE catch 2026-06-07 J-23 bug #5: the CR's metadata.name is the DB
// row ID (prev-<uuid>), not a deterministic shape — earlier this
// function tried `Delete("<tenant>-<project>-pr-<N>")` which ALWAYS
// returned NotFound, silently swallowed, leaving the CR + namespace +
// ArgoCD App orphaned forever after PR close. Fix: lookup by the
// `loopzero.io/tenant + loopzero.io/project + loopzero.io/pr-number` label
// selector that EnsurePreviewCR now sets at creation time. Deletes
// every CR that matches (typically one).
func (m *PreviewCRManager) DeletePreviewCR(ctx context.Context, tenantID, projectID string, prNumber int) error {
	resource := m.Client.Resource(previewEnvCRGVR).Namespace(tenantNamespace)
	selector := fmt.Sprintf("loopzero.io/tenant=%s,loopzero.io/project=%s,loopzero.io/pr-number=%d",
		tenantID, projectID, prNumber)
	list, err := resource.List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("preview CR list: %w", err)
	}
	for _, item := range list.Items {
		if err := resource.Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("preview CR delete %q: %w", item.GetName(), err)
		}
	}
	return nil
}
