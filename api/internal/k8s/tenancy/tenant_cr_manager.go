// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package tenancy owns the Tenant custom resource and tenant eviction.
//
// It is one of the three homes the B-058 split gave the responsibilities that used
// to share api/internal/k8s because they shared client-go. Its admission criterion
// is a subject, not a library: a file belongs here when it reads or writes the
// Tenant CR, or removes a tenant's footprint from a cluster.
package tenancy

import (
	"context"
	"fmt"
	"log/slog"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	pkgdomain "github.com/getloopscale/loop-runtime/pkg/domain"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// tenantNamespace is the canonical namespace for all Tenant + Environment CRs.
//
// ADR-0018 v2 (MEET-ADR-0018 condition C1): hard-coded `const`, NOT
// `var`/env-overridable. The previous `TENANT_NAMESPACE` env var was the
// misplacement vector that allowed iter 7-8 of dogfood to land Tenants in
// `default`. Removed entirely; future multi-namespace topology requires a
// new ADR superseding this one (ADR-0018 §Constraints C3).
const tenantNamespace = "loopruntime-system"

// tenantsGroupResource is used to synthesize Conflict errors for RetryOnConflict.
var tenantsGroupResource = schema.GroupResource{Group: "runtime.getloopscale.com", Resource: "tenants"}

// TenantCRManager manages Tenant custom resources in Kubernetes.
// It implements ports.TenantCRManager (DIP) using the TYPED controller-runtime
// client (M8): the api is now the second Go consumer of the operators/api
// contract module, so the compiler checks every CR field at build time.
type TenantCRManager struct {
	Client client.Client
	// BuildConcurrency is written to every Tenant CR's
	// spec.limits.maxBuildConcurrency: the same per-tenant limit the API
	// enforces (config.MaxConcurrentBuildsPerTenant).
	BuildConcurrency int
}

// NewTenantCRManager creates a TenantCRManager connected to the management cluster.
// It tries in-cluster config first (for when the API runs inside the cluster),
// then falls back to kubeconfig with the specified context.
func NewTenantCRManager(kubeconfig, kubeContext string, buildConcurrency int) (*TenantCRManager, error) {
	if buildConcurrency <= 0 {
		return nil, fmt.Errorf("k8s: tenant CR manager: build concurrency must be positive, got %d", buildConcurrency)
	}
	cfg, err := k8s.RestConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("k8s: tenant CR manager config: %w", err)
	}
	c, err := k8s.NewTypedClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: tenant CR manager typed client: %w", err)
	}
	return &TenantCRManager{Client: c, BuildConcurrency: buildConcurrency}, nil
}

func tenantKey(tenantID string) client.ObjectKey {
	return client.ObjectKey{Namespace: tenantNamespace, Name: tenantID}
}

// EnsureTenant creates or updates a Tenant CR in the loopruntime-system namespace.
// It is idempotent: calling it multiple times with the same tenantID is safe.
// If the CR already exists, it updates the spec to match the desired state.
// It uses retry-on-conflict to handle concurrent modifications by the TenantController.
func (m *TenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec ports.TenantCRSpec) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existing apiv1.Tenant
		err := m.Client.Get(ctx, tenantKey(tenantID), &existing)
		if k8serrors.IsNotFound(err) {
			desired := buildTenant(tenantID, spec, m.BuildConcurrency)
			createErr := m.Client.Create(ctx, desired)
			if k8serrors.IsAlreadyExists(createErr) {
				// Another caller created it between our Get and Create — retry as update.
				return k8serrors.NewConflict(tenantsGroupResource, tenantID, createErr)
			}
			if createErr != nil {
				return fmt.Errorf("k8s: create tenant CR %q: %w", tenantID, createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("k8s: get tenant CR %q: %w", tenantID, err)
		}

		// CR exists — apply desired spec onto the fetched object, preserving its
		// resourceVersion (carried by `existing`) for optimistic concurrency.
		applyTenantSpec(&existing, spec, m.BuildConcurrency)
		if err := m.Client.Update(ctx, &existing); err != nil {
			return err // RetryOnConflict handles Conflict errors automatically
		}
		return nil
	})
}

// isEnvMatch reports whether an environments[] entry matches the (name, project)
// tuple. M2 v1.2 (2026-06-07): post-refactor the per-(project, env) tuple is the
// uniqueness key; legacy entries carry empty project and match only when the
// lookup project is also empty (production legacy — ADR D2).
func isEnvMatch(entry apiv1.TenantEnvironmentEntry, envName, project string) bool {
	return entry.Name == envName && entry.Project == project
}

// AddEnvironment adds an environment entry to the Tenant CR spec.environments[].
// When the same (envName, project) pair is already present it changes nothing
// and returns ports.ErrEnvironmentExists, so a caller can tell a create from a
// no-op: the create route answers 409, the first-deploy ensurer carries on.
//
// M2 v1.2 (2026-06-07): writes the canonical `project` field so the operator
// can compute the 3-segment namespace `loopruntime-<tenant>-<project>-<env>` via
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
	// the legacy `loopruntime-<tenant>-<env>` path is enforced elsewhere.
	if project != "" {
		ref, refErr := pkgdomain.NewEnvRef(tenantID, project, envName)
		if refErr != nil {
			return fmt.Errorf("k8s: AddEnvironment(%q, %q, %q) EnvRef: %w", tenantID, project, envName, refErr)
		}
		if _, err := ref.Namespace(); err != nil {
			return fmt.Errorf("k8s: AddEnvironment(%q, %q, %q): %w", tenantID, project, envName, err)
		}
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var t apiv1.Tenant
		if err := m.Client.Get(ctx, tenantKey(tenantID), &t); err != nil {
			return fmt.Errorf("k8s: get tenant CR %q for AddEnvironment: %w", tenantID, err)
		}

		// Idempotent on (name, project) tuple — same env name in different
		// projects is intentional in M2 (each lands in its own namespace).
		for _, e := range t.Spec.Environments {
			if isEnvMatch(e, envName, project) {
				return ports.ErrEnvironmentExists
			}
		}

		t.Spec.Environments = append(t.Spec.Environments, apiv1.TenantEnvironmentEntry{
			Name:    envName,
			Cluster: "workload",
			Project: project,
		})
		return m.Client.Update(ctx, &t) // RetryOnConflict handles Conflict errors
	})
}

// ErrEnvironmentNotFound is returned by RemoveEnvironment when the named
// environment is not present in the Tenant CR. Callers can map to HTTP 404.
// AUDIT-12 (CHG-2026-04-23-AUDIT-12): previously this was silently idempotent,
// causing `loopscale env delete nonexistent` to return "deleted" misleadingly.
var ErrEnvironmentNotFound = ports.ErrEnvironmentNotFound

// RemoveEnvironment removes an environment entry from the Tenant CR spec.environments[].
// Returns ErrEnvironmentNotFound if the (envName, project) tuple is not present —
// callers should treat as HTTP 404 rather than silent success.
// Returns an error if removing would leave zero environments.
//
// M2 v1.2 (2026-06-07): filters by (name, project) tuple. The same env
// name in different projects lives in different namespaces — removing
// `staging+web` MUST NOT touch `staging+api`.
//
// EC-7: when the env being removed still has an ACTIVE release in
// env_targets, a warning is logged pointing the operator at the orphan
// ArgoCD App that the operator's reaper will clean up asynchronously.
func (m *TenantCRManager) RemoveEnvironment(ctx context.Context, tenantID, project, envName string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var t apiv1.Tenant
		if err := m.Client.Get(ctx, tenantKey(tenantID), &t); err != nil {
			return fmt.Errorf("k8s: get tenant CR %q for RemoveEnvironment: %w", tenantID, err)
		}

		// Filter out the matching (name, project) tuple. Other entries
		// (including same name in different projects) stay.
		filtered := make([]apiv1.TenantEnvironmentEntry, 0, len(t.Spec.Environments))
		for _, e := range t.Spec.Environments {
			if !isEnvMatch(e, envName, project) {
				filtered = append(filtered, e)
			}
		}

		// AUDIT-12: not found → explicit error (no more silent idempotence).
		if len(filtered) == len(t.Spec.Environments) {
			return ErrEnvironmentNotFound
		}

		// Guard: cannot remove if it would leave zero environments.
		if len(filtered) == 0 {
			return fmt.Errorf("k8s: cannot remove environment %q (project %q) from tenant %q: at least one environment is required", envName, project, tenantID)
		}

		// EC-7: forensic breadcrumb if the (tenant, project, env) namespace
		// might still be in flight at ArgoCD. Best-effort log; errors silenced.
		if project != "" {
			if ref, refErr := pkgdomain.NewEnvRef(tenantID, project, envName); refErr == nil {
				if ns, nerr := ref.Namespace(); nerr == nil {
					slog.Warn("k8s: env removed; orphan ArgoCD App may exist briefly",
						"tenant_id", tenantID, "project", project, "env", envName,
						"namespace", ns,
						"hint", "run `kubectl get application -A -l tenant="+tenantID+"` to verify")
				}
			}
		}

		t.Spec.Environments = filtered
		return m.Client.Update(ctx, &t)
	})
}

// ListEnvironments reads spec.environments[] from the Tenant CR and returns
// them as EnvironmentEntry values with status "active".
//
// M2 v1.2 (2026-06-07): each entry carries the project field; legacy entries
// (production) have empty project per ADR D2.
//
// A tenant with no Tenant CR has no environments: the CR is created with the
// tenant's first project (EnsureTenant), so a tenant provisioned at sign-up
// lists as empty (#17). Every other read failure is returned.
func (m *TenantCRManager) ListEnvironments(ctx context.Context, tenantID string) ([]ports.EnvironmentEntry, error) {
	var t apiv1.Tenant
	if err := m.Client.Get(ctx, tenantKey(tenantID), &t); err != nil {
		if k8serrors.IsNotFound(err) {
			return []ports.EnvironmentEntry{}, nil
		}
		return nil, fmt.Errorf("k8s: get tenant CR %q for ListEnvironments: %w", tenantID, err)
	}

	result := make([]ports.EnvironmentEntry, 0, len(t.Spec.Environments))
	for _, e := range t.Spec.Environments {
		if e.Name == "" {
			continue
		}
		result = append(result, ports.EnvironmentEntry{
			Name:    e.Name,
			Status:  "active",
			Project: e.Project, // empty for legacy entries
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
func (m *TenantCRManager) GetEnvironmentDetail(ctx context.Context, tenantID, project, envName string) (*ports.EnvironmentDetail, error) {
	var t apiv1.Tenant
	if err := m.Client.Get(ctx, tenantKey(tenantID), &t); err != nil {
		return nil, fmt.Errorf("k8s: get tenant CR %q for GetEnvironmentDetail: %w", tenantID, err)
	}

	// Issue #139 — a project-less lookup (the CLI's `loopscale env inspect <name>`
	// has no --project flag) used to require entry.Project == "", which the
	// #124 backfill made unsatisfiable. resolveEnvEntry documents the full
	// order; it returns an error only for the genuinely ambiguous case.
	idx, _, rerr := resolveEnvEntry(t.Spec.Environments, envName, project)
	if rerr != nil {
		return nil, rerr
	}
	if idx < 0 {
		return nil, nil // not found
	}
	e := t.Spec.Environments[idx]
	return &ports.EnvironmentDetail{
		Name:     e.Name,
		Status:   "active",
		Projects: []ports.EnvProjectInfo{},
	}, nil
}

// buildTenant constructs a typed Tenant CR matching the CRD schema defined in
// operators/api/v1/tenant_types.go. The compiler now enforces the spec shape.
func buildTenant(tenantID string, spec ports.TenantCRSpec, buildConcurrency int) *apiv1.Tenant {
	t := &apiv1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantID,
			Namespace: tenantNamespace,
		},
	}
	applyTenantSpec(t, spec, buildConcurrency)
	// Only a new CR takes the cell: applyTenantSpec also runs on update, and a project
	// create must never move a tenant (#412). Empty keeps the CRD default.
	t.Spec.CellID = spec.CellID
	return t
}

// applyTenantSpec writes the desired TenantCRSpec onto a Tenant object. Used by
// both create (fresh object) and update (fetched object, resourceVersion kept).
// Environments are intentionally NOT set here: the canonical chain in
// routes/projects.go appends the production env via EnvironmentEnsurer right
// after EnsureTenant (CRD allows MinItems=0). Pre-populating an env without a
// project caused the LIVE admission rejection 2026-06-09.
func applyTenantSpec(t *apiv1.Tenant, spec ports.TenantCRSpec, buildConcurrency int) {
	plan := spec.Plan
	if plan == "" {
		plan = "starter"
	}
	t.Spec.DisplayName = t.Name
	t.Spec.Plan = apiv1.PlanTier(plan)
	t.Spec.GitOps = apiv1.GitOpsSpec{
		RepoName: naming.TenantGitOpsRepoName(t.Name),
		Branch:   "main",
	}
	t.Spec.Preview = apiv1.PreviewSpec{
		MaxEnvironments: 3,
		TTLHours:        72,
	}
	t.Spec.Limits = apiv1.ResourceLimits{
		MaxCPU:              resource.MustParse("4"),
		MaxMemory:           resource.MustParse("8Gi"),
		MaxStorage:          resource.MustParse("20Gi"),
		MaxBuildConcurrency: buildConcurrency,
	}
	// Environments: the CRD declares `environments` in spec.required with MinItems=0.
	// A nil slice without omitempty serialises as "environments":null and admission
	// rejects it (required + null is invalid). Initialising an empty slice forces
	// "environments":[] — valid per MinItems=0 and satisfies required.
	// EnvironmentEnsurer appends the production entry right after EnsureTenant.
	if t.Spec.Environments == nil {
		t.Spec.Environments = []apiv1.TenantEnvironmentEntry{}
	}
}
