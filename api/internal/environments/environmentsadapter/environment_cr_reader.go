// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Sprint A trio-fix 2026-06-01 — EnvironmentCRReader queries the Environment
// CR `status.phase` to gate `EnvironmentEnsurer` poll loop. Without this,
// the API returned from `EnsureEnvironment` as soon as the env existed in
// `tenant.spec.environments` — BEFORE the tenant-controller had reconciled
// the AppProject destination list AND BEFORE the environment-controller
// had finished provisioning namespace + RBAC + NetworkPolicy + ResourceQuota.
//
// Race window: API → AddEnvironment → poll HasEnvironment (returns true
// IMMEDIATELY) → Submit build → ArgoCD App stuck `InvalidSpecError:
// destination does not match allowed destinations in project`. LIVE-observed
// during Sprint A trio dogfood 2026-06-01: 8/132 specs failed with
// SYNC_FAILED + builds in FAILED state because Apps never reconciled.
//
// Fix: poll for `Environment CR.status.phase == "Active"` in addition to the
// existing `HasEnvironment` check. The environment-controller sets phase
// to Active ONLY after namespace + RBAC + NetworkPolicy + ResourceQuota
// are all Ready (see operators/internal/environment/controller.go), which
// is causally ordered AFTER the tenant-controller has updated AppProject
// destinations. Single signal, no double-check needed.

package environmentsadapter

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/getloopscale/loopzero/api/internal/environments"
)

// environmentCRGVR — same Group/Version as Tenant CR, different Resource.
// Mirrors the operator-side definition in operators/api/v1/environment_types.go.
var environmentCRGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "environments",
}

// EnvironmentCRReader queries the Environment CR for readiness status.
// Independent of TenantCRManager (which queries Tenant CR.spec.environments —
// a shallow check that returns true before reconciliation completes).
type EnvironmentCRReader struct {
	Client    dynamic.Interface
	Namespace string // canonical "loopzero-system" (matches operators/internal/tenant)
}

var _ environments.EnvironmentReadinessChecker = (*EnvironmentCRReader)(nil)

// NewEnvironmentCRReader constructs a reader bound to the core-cluster
// dynamic client. Namespace defaults to "loopzero-system" if empty.
func NewEnvironmentCRReader(client dynamic.Interface, namespace string) *EnvironmentCRReader {
	if namespace == "" {
		namespace = "loopzero-system"
	}
	return &EnvironmentCRReader{Client: client, Namespace: namespace}
}

// IsReady returns (true, nil) if the Environment CR
// `<tenantID>[-<project>]-<envName>` exists in mgmt namespace AND has
// `status.phase == "Active"`. Returns (false, nil) if the CR is absent
// or has any other phase (Pending, Failed, "", etc). Errors are returned
// for transient API errors (network, RBAC); the caller's poll loop should
// continue.
//
// CR naming convention (M2 v1.2, 2026-06-07):
//   - canonical (post-M2): `<tenantID>-<project>-<envName>` — when
//     project is non-empty. Per `pkg/naming.Namespace` semantics, this
//     matches the cell-0-cluster namespace `loopzero-<tenant>-<project>-<env>`
//     once the `loopzero-` prefix is stripped (CR names live in mgmt, no prefix).
//   - legacy (pre-M2 / production): `<tenantID>-<envName>` — when
//     project is empty (ADR D2 — backward compatibility for tenants whose
//     production env was provisioned before the M2 migration).
func (r *EnvironmentCRReader) IsReady(ctx context.Context, tenantID, project, envName string) (bool, error) {
	if r == nil || r.Client == nil {
		// Defensive: nil reader means "skip the extra check" (legacy behavior).
		return true, nil
	}
	if tenantID == "" || envName == "" {
		return false, fmt.Errorf("env cr reader: tenantID and envName are required")
	}
	crName := envCRName(tenantID, project, envName)

	// Bounded read timeout — environment-controller reconcile typically
	// completes in <2s; 5s tolerates transient API server slowness.
	getCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	obj, err := r.Client.Resource(environmentCRGVR).Namespace(r.Namespace).
		Get(getCtx, crName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// CR doesn't exist yet → tenant-controller hasn't reconciled.
			return false, nil
		}
		return false, fmt.Errorf("env cr reader: get %s/%s: %w", r.Namespace, crName, err)
	}

	phase, found, err := unstructuredString(obj.Object, "status", "phase")
	if err != nil {
		return false, fmt.Errorf("env cr reader: extract status.phase: %w", err)
	}
	if !found {
		// Status not populated yet → operator hasn't completed first reconcile.
		return false, nil
	}
	return phase == "Active", nil
}

// envCRName builds the Environment CR `metadata.name` for the given
// (tenant, project, env) tuple. M2 v1.2 helper consumed by IsReady (and
// any future direct-lookup callers).
//
//   - canonical (project != ""): "<tenant>-<project>-<env>"
//   - legacy   (project == ""): "<tenant>-<env>"
//
// The legacy form preserves the pre-M2 naming so production envs that
// landed on the cluster before the M2 migration continue to resolve
// (ADR D2 — backward compatibility for the production-legacy entry).
func envCRName(tenantID, project, envName string) string {
	if project == "" {
		return fmt.Sprintf("%s-%s", tenantID, envName)
	}
	return fmt.Sprintf("%s-%s-%s", tenantID, project, envName)
}

// unstructuredString reads a string field from a nested map[string]interface{}
// without panicking on missing/wrong-type keys. Returns (value, exists, err).
func unstructuredString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	var cursor interface{} = obj
	for _, f := range fields {
		m, ok := cursor.(map[string]interface{})
		if !ok {
			return "", false, nil
		}
		v, exists := m[f]
		if !exists {
			return "", false, nil
		}
		cursor = v
	}
	s, ok := cursor.(string)
	if !ok {
		return "", false, fmt.Errorf("field at %v is not a string (got %T)", fields, cursor)
	}
	return s, true, nil
}
