// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package k8s provides Kubernetes client implementations.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	"github.com/getloopscale/loopzero/api/internal/domain"
	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"
)

// tenantNamespace is the canonical namespace for all Tenant + Environment CRs.
//
// ADR-0018 v2 (MEET-ADR-0018 condition C1): hard-coded `const`, NOT
// `var`/env-overridable. The previous `TENANT_NAMESPACE` env var was the
// misplacement vector that allowed iter 7-8 of dogfood to land Tenants in
// `default`. Removed entirely; future multi-namespace topology requires a
// new ADR superseding this one (ADR-0018 §Constraints C3).
const tenantNamespace = "loopzero-system"

var tenantCRGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "tenants",
}

// TenantCRManager manages Tenant custom resources in Kubernetes.
// It implements domain.TenantCRManager (DIP) using the dynamic client
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
func (m *TenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec domain.TenantCRSpec) error {
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

// getEnvironments extracts spec.environments from an unstructured Tenant CR.
// Returns nil slice if environments is missing or not a slice.
func getEnvironments(obj *unstructured.Unstructured) []interface{} {
	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	envs, ok := spec["environments"].([]interface{})
	if !ok {
		return nil
	}
	return envs
}

// setEnvironments writes spec.environments back to an unstructured Tenant CR.
func setEnvironments(obj *unstructured.Unstructured, envs []interface{}) {
	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		spec = map[string]interface{}{}
		obj.Object["spec"] = spec
	}
	spec["environments"] = envs
}

// isEnvMatch reports whether an environments[] entry matches the
// (name, project) tuple. M2 v1.2 (2026-06-07): post-refactor the
// per-(project, env) tuple is the uniqueness key; legacy entries
// carry empty project and match only when the lookup project is
// also empty (production legacy — ADR D2).
func isEnvMatch(entry map[string]interface{}, envName, project string) bool {
	entryName, _ := entry["name"].(string)
	if entryName != envName {
		return false
	}
	entryProject, _ := entry["project"].(string)
	return entryProject == project
}

// AddEnvironment adds an environment entry to the Tenant CR spec.environments[].
// Idempotent on (name, project) tuple: if the same (envName, project) pair
// already exists, it returns nil without changes.
//
// M2 v1.2 (2026-06-07): writes the canonical `project` field so the operator
// can compute the 3-segment namespace `loopzero-<tenant>-<project>-<env>` via
// pkg/naming.Namespace. Empty project preserves the legacy 2-segment
// pathway for production legacy (ADR D2).
//
// EC-4: the combined namespace is validated against the 63-char RFC 1123
// ceiling BEFORE the API server mutation. Failure returns a typed error
// pointing the operator to project/env name choice — the operator-side
// CRD validation provides the second defence-in-depth layer.
func (m *TenantCRManager) AddEnvironment(ctx context.Context, tenantID, project, envName string) error {
	// EC-4 pre-flight: reject combined namespace > 63 chars before the
	// k8s round-trip. Legacy callers (project="") bypass this check —
	// the legacy `loopzero-<tenant>-<env>` path is enforced elsewhere.
	if project != "" {
		// T1.3.f of plan 2026-06-11-code-review-remediation v1.2 — canonical
		// EnvRef pattern per ADR D2. NewEnvRef adds construction-time
		// empty-field defense in addition to the Namespace formula
		// length/RFC-1123 validation that EC-4 pre-flight enforces.
		ref, refErr := pkgdomain.NewEnvRef(tenantID, project, envName)
		if refErr != nil {
			return fmt.Errorf("k8s: AddEnvironment(%q, %q, %q) EnvRef: %w", tenantID, project, envName, refErr)
		}
		if _, err := ref.Namespace(); err != nil {
			return fmt.Errorf("k8s: AddEnvironment(%q, %q, %q): %w", tenantID, project, envName, err)
		}
	}

	resource := m.Client.Resource(tenantCRGVR).Namespace(tenantNamespace)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := resource.Get(ctx, tenantID, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("k8s: get tenant CR %q for AddEnvironment: %w", tenantID, err)
		}

		envs := getEnvironments(existing)

		// Idempotent on (name, project) tuple — same env name in different
		// projects is intentional in M2 (each lands in its own namespace).
		for _, e := range envs {
			entry, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if isEnvMatch(entry, envName, project) {
				return nil // tuple already present
			}
		}

		// Append new entry with project field (omitempty in the CRD —
		// empty value is acceptable for legacy production entries).
		entry := map[string]interface{}{
			"name":    envName,
			"cluster": "workload",
		}
		if project != "" {
			entry["project"] = project
		}
		envs = append(envs, entry)
		setEnvironments(existing, envs)

		_, err = resource.Update(ctx, existing, metav1.UpdateOptions{})
		return err // RetryOnConflict handles Conflict errors
	})
}

// ErrEnvironmentNotFound is returned by RemoveEnvironment when the named
// environment is not present in the Tenant CR. Callers can map to HTTP 404.
// AUDIT-12 (CHG-2026-04-23-AUDIT-12): previously this was silently idempotent,
// causing `loopzero env delete nonexistent` to return "deleted" misleadingly.
var ErrEnvironmentNotFound = errors.New("k8s: environment not found in tenant CR")

// RemoveEnvironment removes an environment entry from the Tenant CR spec.environments[].
// Returns ErrEnvironmentNotFound if the (envName, project) tuple is not present —
// callers should treat as HTTP 404 rather than silent success.
// Returns an error if removing would leave zero environments (MinItems=1).
//
// M2 v1.2 (2026-06-07): filters by (name, project) tuple. The same env
// name in different projects lives in different namespaces — removing
// `staging+web` MUST NOT touch `staging+api`.
//
// EC-7: when the env being removed still has an ACTIVE release in
// env_targets, a warning is logged pointing the operator at the orphan
// ArgoCD App that the operator's reaper will clean up asynchronously.
// (The actual env_target / release lookup lives at the service layer;
// this method logs the namespace pattern for operator forensics.)
func (m *TenantCRManager) RemoveEnvironment(ctx context.Context, tenantID, project, envName string) error {
	resource := m.Client.Resource(tenantCRGVR).Namespace(tenantNamespace)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := resource.Get(ctx, tenantID, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("k8s: get tenant CR %q for RemoveEnvironment: %w", tenantID, err)
		}

		envs := getEnvironments(existing)

		// Filter out the matching (name, project) tuple. Other entries
		// (including same name in different projects) stay.
		filtered := make([]interface{}, 0, len(envs))
		for _, e := range envs {
			entry, ok := e.(map[string]interface{})
			if !ok {
				filtered = append(filtered, e)
				continue
			}
			if !isEnvMatch(entry, envName, project) {
				filtered = append(filtered, e)
			}
		}

		// AUDIT-12: not found → explicit error (no more silent idempotence).
		if len(filtered) == len(envs) {
			return ErrEnvironmentNotFound
		}

		// Guard: cannot remove if it would leave zero environments
		if len(filtered) == 0 {
			return fmt.Errorf("k8s: cannot remove environment %q (project %q) from tenant %q: at least one environment is required", envName, project, tenantID)
		}

		// EC-7: surface a forensic breadcrumb if the (tenant, project, env)
		// namespace might still be in flight at ArgoCD. The operator's
		// reaper handles orphan cleanup; this log helps operators correlate.
		if project != "" {
			// T1.3.f forensic-breadcrumb path: build EnvRef + derive namespace
			// canonically. Errors silenced intentionally — this is a best-
			// effort log on the deletion side, not a hard-fail gate.
			if ref, refErr := pkgdomain.NewEnvRef(tenantID, project, envName); refErr == nil {
				if ns, nerr := ref.Namespace(); nerr == nil {
					slog.Warn("k8s: env removed; orphan ArgoCD App may exist briefly",
						"tenant_id", tenantID, "project", project, "env", envName,
						"namespace", ns,
						"hint", "run `kubectl get application -A -l tenant="+tenantID+"` to verify")
				}
			}
		}

		setEnvironments(existing, filtered)

		_, err = resource.Update(ctx, existing, metav1.UpdateOptions{})
		return err
	})
}

// ListEnvironments reads spec.environments[] from the Tenant CR and returns
// them as EnvironmentEntry values with status "active".
//
// M2 v1.2 (2026-06-07): each entry carries the project field (omitempty);
// legacy entries (production) have empty project per ADR D2.
func (m *TenantCRManager) ListEnvironments(ctx context.Context, tenantID string) ([]domain.EnvironmentEntry, error) {
	resource := m.Client.Resource(tenantCRGVR).Namespace(tenantNamespace)

	obj, err := resource.Get(ctx, tenantID, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s: get tenant CR %q for ListEnvironments: %w", tenantID, err)
	}

	envs := getEnvironments(obj)
	result := make([]domain.EnvironmentEntry, 0, len(envs))
	for _, e := range envs {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		project, _ := entry["project"].(string) // empty for legacy entries
		result = append(result, domain.EnvironmentEntry{
			Name:    name,
			Status:  "active",
			Project: project,
		})
	}
	return result, nil
}

// GetEnvironmentDetail returns detail for a single environment from the Tenant CR.
// Returns nil (without error) if the (envName, project) tuple is not found.
//
// M2 v1.2 (2026-06-07): filter by (name, project) tuple. Legacy lookups
// with project="" target the production-legacy entry that was created
// before the M2 migration (ADR D2).
func (m *TenantCRManager) GetEnvironmentDetail(ctx context.Context, tenantID, project, envName string) (*domain.EnvironmentDetail, error) {
	resource := m.Client.Resource(tenantCRGVR).Namespace(tenantNamespace)

	obj, err := resource.Get(ctx, tenantID, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s: get tenant CR %q for GetEnvironmentDetail: %w", tenantID, err)
	}

	envs := getEnvironments(obj)
	for _, e := range envs {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if isEnvMatch(entry, envName, project) {
			name, _ := entry["name"].(string)
			return &domain.EnvironmentDetail{
				Name:     name,
				Status:   "active",
				Projects: []domain.EnvProjectInfo{},
			}, nil
		}
	}

	return nil, nil // not found
}

// buildTenantCR constructs the unstructured Tenant CR matching the CRD schema
// defined in operators/api/v1/tenant_types.go.
func buildTenantCR(tenantID string, spec domain.TenantCRSpec) *unstructured.Unstructured {
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
				// Empty by construction (no hardcode per
				// feedback_no_hardcode_gate_2026_06_09): the canonical T4.1
				// chain in routes/projects.go appends the production env
				// via EnvironmentEnsurer immediately after EnsureTenant
				// returns. The CRD constraint relaxed to MinItems=0
				// (operators/api/v1/tenant_types.go) — shape validates,
				// API enforces invariant. Pre-populating "production" here
				// without the project field caused T1.1 admission rejection
				// LIVE 2026-06-09 (T6.1 LIVE bash catch).
				"environments": []interface{}{},
				"gitops": map[string]interface{}{
					"repoName": fmt.Sprintf("%s-gitops", tenantID),
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
