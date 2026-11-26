// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package workload

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// PreviewCRSpec is the minimal subset of PreviewEnvironment.spec the
// API submits — kept duplicated from preview.PreviewCRSpec to avoid
// the cycle k8s ↔ services. Same field names.
type PreviewCRSpec struct {
	Name      string
	TenantID  string
	ProjectID string
	PRNumber  int
	Branch    string
	ReleaseID string
	ExpiresAt time.Time
	// PublicURL is the preview's user-facing URL (pkg/naming.PreviewURL).
	PublicURL string
}

// PreviewCRManager manages PreviewEnvironment custom resources in the
// management cluster. It uses the TYPED controller-runtime client (M8):
// the api is the second Go consumer of the operators/api contract module,
// so the compiler checks every CR field at build time (was unstructured
// map pre-M8). Shares the typed Client used by TenantCRManager — both
// target loopruntime-system on the mgmt cluster.
//
// LIVE catch 2026-06-07 J-23 bug #4: PreviewCRCreator interface in
// services had no production implementation. Adapter lives here (next to
// TenantCRManager) so the namespace + scheme + client wiring is canonical,
// not duplicated.
type PreviewCRManager struct {
	Client client.Client
}

// NewPreviewCRManager constructs a PreviewCRManager from an existing typed
// client. The same Client used by TenantCRManager is fine (and preferred —
// both target the mgmt cluster).
func NewPreviewCRManager(c client.Client) *PreviewCRManager {
	return &PreviewCRManager{Client: c}
}

// EnsurePreviewCR is upsert: tries Create; on AlreadyExists patches `spec`
// (PR sync to a new commit reuses the same preview row + CR name).
// Idempotent — safe to call from a webhook retry.
func (m *PreviewCRManager) EnsurePreviewCR(ctx context.Context, spec PreviewCRSpec) error {
	desired := buildPreviewCR(spec)

	createErr := m.Client.Create(ctx, desired)
	if createErr == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("preview CR create: %w", createErr)
	}

	// Exists — fetch, overwrite spec (preserving resourceVersion), update.
	var existing apiv1.PreviewEnvironment
	if getErr := m.Client.Get(ctx, client.ObjectKey{Namespace: k8s.ControlPlaneNamespace, Name: spec.Name}, &existing); getErr != nil {
		return fmt.Errorf("preview CR get-after-conflict: %w", getErr)
	}
	existing.Spec = desired.Spec
	if updErr := m.Client.Update(ctx, &existing); updErr != nil {
		return fmt.Errorf("preview CR update: %w", updErr)
	}
	return nil
}

// GetPhases returns the operator-observed phase per preview ID. Used by
// PreviewHandler.List to enrich the dashboard /previews response without
// requiring a CR-to-PG writeback path.
//
// Missing CRs (NotFound) are returned as absent entries — caller falls
// back to the DB status (creating) which is honest stale.
func (m *PreviewCRManager) GetPhases(ctx context.Context, previewIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(previewIDs))
	if len(previewIDs) == 0 {
		return out, nil
	}
	for _, id := range previewIDs {
		var pe apiv1.PreviewEnvironment
		err := m.Client.Get(ctx, client.ObjectKey{Namespace: k8s.ControlPlaneNamespace, Name: id}, &pe)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return out, fmt.Errorf("get preview CR %q: %w", id, err)
		}
		if phase := string(pe.Status.Phase); phase != "" {
			out[id] = phase
		}
	}
	return out, nil
}

// DeletePreviewCR is called when the PR closes. Idempotent — NotFound is
// not an error (closing a PR that never had a preview).
//
// LIVE catch 2026-06-07 J-23 bug #5: the CR's metadata.name is the DB row
// ID (prev-<uuid>), not a deterministic shape — earlier this function
// tried `Delete("<tenant>-<project>-pr-<N>")` which ALWAYS returned
// NotFound, silently swallowed, leaving the CR + namespace + ArgoCD App
// orphaned forever after PR close. Fix: lookup by the
// `runtime.getloopscale.com/tenant + runtime.getloopscale.com/project + runtime.getloopscale.com/pr-number` label selector
// that EnsurePreviewCR sets at creation time. Deletes every CR that
// matches (typically one).
func (m *PreviewCRManager) DeletePreviewCR(ctx context.Context, tenantID, projectID string, prNumber int) error {
	var list apiv1.PreviewEnvironmentList
	if err := m.Client.List(ctx, &list,
		client.InNamespace(k8s.ControlPlaneNamespace),
		client.MatchingLabels{
			"runtime.getloopscale.com/tenant":    tenantID,
			"runtime.getloopscale.com/project":   projectID,
			"runtime.getloopscale.com/pr-number": fmt.Sprintf("%d", prNumber),
		},
	); err != nil {
		return fmt.Errorf("preview CR list: %w", err)
	}
	for i := range list.Items {
		item := &list.Items[i]
		if err := m.Client.Delete(ctx, item); err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("preview CR delete %q: %w", item.GetName(), err)
		}
	}
	return nil
}

// buildPreviewCR constructs a typed PreviewEnvironment CR matching the CRD
// schema defined in operators/api/v1/preview_types.go. The labels give
// DeletePreviewCR a deterministic label-selector lookup (J-23 bug #5).
func buildPreviewCR(spec PreviewCRSpec) *apiv1.PreviewEnvironment {
	expiresAt := metav1.NewTime(spec.ExpiresAt)
	return &apiv1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: k8s.ControlPlaneNamespace,
			Labels: map[string]string{
				"runtime.getloopscale.com/tenant":     spec.TenantID,
				"runtime.getloopscale.com/project":    spec.ProjectID,
				"runtime.getloopscale.com/pr-number":  fmt.Sprintf("%d", spec.PRNumber),
				"runtime.getloopscale.com/managed-by": "loopruntime-api",
			},
		},
		Spec: apiv1.PreviewEnvironmentSpec{
			TenantID:  spec.TenantID,
			ProjectID: spec.ProjectID,
			PRNumber:  spec.PRNumber,
			Branch:    spec.Branch,
			ReleaseID: spec.ReleaseID,
			ExpiresAt: &expiresAt,
			PublicURL: spec.PublicURL,
		},
	}
}
