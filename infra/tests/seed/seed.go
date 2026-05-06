// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package seed provides idempotent E2E test data seeding for LoopZero.
//
// It can be used from E2E BeforeEach blocks or as a standalone setup step.
// All operations are idempotent: running multiple times produces the same result.
//
// Usage from tests:
//
//	seed.Run(seed.Config{
//	    DatabaseURL: "postgres://...",
//	    KubeContext: "k3d-loopzero-mgmt",
//	})
package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// TenantID is the E2E test tenant identifier.
	TenantID = "e2e-tenant"
	// DisplayName is the human-readable name for the E2E tenant.
	DisplayName = "E2E Test Tenant"
	// UserID is the E2E test user identifier.
	UserID = "github-e2e-user"
	// GitHubID is the fake GitHub ID for the E2E test user.
	GitHubID = 999999
	// Email is the E2E test user email.
	Email = "e2e@test.local"
	// Role is the E2E test user's role in the tenant.
	Role = "owner"
	// Plan is the E2E test tenant's subscription plan.
	Plan = "starter"
	// ProjectName is the E2E test project name.
	ProjectName = "e2e-project"
	// ProjectID is the E2E test project identifier.
	ProjectID = "proj-e2e-project"
	// AuthToken is the fixed auth token for E2E tests.
	AuthToken = "e2e-token-dev"
	// Username is the unique handle for the E2E user (users.username NOT NULL).
	Username = "e2e-user"
)

// Config holds configuration for the seed operation.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// KubeContext is the kubectl context to use (default: k3d-loopzero-mgmt).
	KubeContext string
	// ReconcileTimeout is how long to wait for namespace reconciliation (default: 60s).
	ReconcileTimeout time.Duration
}

// defaults fills in zero-value fields with sensible defaults.
//
// DatabaseURL resolution order (first non-empty wins):
//  1. Config.DatabaseURL (programmatic override)
//  2. LOOPZERO_E2E_DATABASE_URL env var (operator override — Tilt port-forward
//     uses 127.0.0.1:15432 with password "loopzero-dev-password")
//  3. fallback "postgres://loopzero:loopzero@localhost:5432/loopzero?sslmode=disable"
//     (legacy default; matches the old in-process PG that no longer exists)
func (c *Config) defaults() {
	if c.DatabaseURL == "" {
		if env := os.Getenv("LOOPZERO_E2E_DATABASE_URL"); env != "" {
			c.DatabaseURL = env
		} else {
			c.DatabaseURL = "postgres://loopzero:loopzero@localhost:5432/loopzero?sslmode=disable"
		}
	}
	if c.KubeContext == "" {
		c.KubeContext = "k3d-loopzero-mgmt"
	}
	if c.ReconcileTimeout == 0 {
		c.ReconcileTimeout = 60 * time.Second
	}
}

// Run executes the full E2E seed: database inserts, Tenant CR creation,
// and reconciliation wait. Returns nil on success, error on timeout or failure.
func Run(cfg Config) error {
	cfg.defaults()

	if err := SeedDatabase(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("database seed failed: %w", err)
	}

	if err := EnsureTenantCR(cfg.KubeContext); err != nil {
		return fmt.Errorf("tenant CR creation failed: %w", err)
	}

	if err := WaitForReconciliation(cfg.KubeContext, cfg.ReconcileTimeout); err != nil {
		return fmt.Errorf("reconciliation wait failed: %w", err)
	}

	return nil
}

// SeedDatabase inserts user, tenant, membership, and project via psql.
// All statements use ON CONFLICT DO NOTHING for idempotency.
//
// LoopZero's CLI does not yet support the X-LoopZero-Tenant header (T2 — see plan
// MEET-ASYNC-AMENDMENT-002). Therefore the JWT subject (loopzero-bootstrap)
// MUST resolve to a single tenant or every deploy fails with TENANT_REQUIRED.
// We enforce that invariant by stripping any non-E2E memberships of
// loopzero-bootstrap before re-asserting the canonical e2e-tenant membership.
//
// The dogfood seed-local-user.sh re-creates tn-e2etest membership on demand,
// so this cleanup is safe and reversible.
func SeedDatabase(dbURL string) error {
	sql := fmt.Sprintf(`
-- Strip any prior tn-e2etest membership of bootstrap so the JWT resolves to
-- exactly one tenant (e2e-tenant) — required because the CLI cannot send
-- the X-LoopZero-Tenant header today.
DELETE FROM tenant_members
WHERE user_id = 'loopzero-bootstrap' AND tenant_id <> '%s';
`+`
INSERT INTO users (id, email, github_id, display_name, username)
VALUES ('%s', '%s', %d, 'e2e-user', '%s')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenants (id, display_name, slug, owner_id)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('%s', '%s', '%s')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- Grant loopzero-bootstrap (Hydra JWT subject) membership in the E2E tenant.
-- The dogfood JWT in ~/.loopzero/local-dogfood-token.txt has sub=loopzero-bootstrap;
-- without this row, deploys against e2e-tenant fail with UNAUTHORIZED.
-- The bootstrap user is created by infra/helmfile/scripts/seed-local-user.sh.
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('%s', 'loopzero-bootstrap', 'owner')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

INSERT INTO projects (id, tenant_id, name, created_by)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT (id) DO NOTHING;
`,
		TenantID, // strip non-canonical bootstrap memberships
		UserID, Email, GitHubID, Username,
		TenantID, DisplayName, TenantID, UserID, // slug = TenantID
		TenantID, UserID, Role,
		TenantID, // bootstrap E2E membership
		ProjectID, TenantID, ProjectName, UserID,
	)

	cmd := exec.Command("psql", dbURL, "-c", sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql failed: %w\noutput: %s", err, output)
	}
	return nil
}

// EnsureTenantCR creates or updates the Tenant CR via kubectl apply.
func EnsureTenantCR(kubeContext string) error {
	tenantCR := map[string]interface{}{
		"apiVersion": "loopzero.io/v1",
		"kind":       "Tenant",
		"metadata": map[string]interface{}{
			"name":      TenantID,
			// Tenant-operator only reconciles CRs in loopzero-system namespace;
			// "default" is invisible to its watch (verified empirically
			// 2026-05-06 — Tenant in "default" stays with empty PHASE while
			// tenants in "loopzero-system" reconcile to Active within seconds).
			"namespace": "loopzero-system",
		},
		"spec": map[string]interface{}{
			"displayName": DisplayName,
			"plan":        Plan,
			"environments": []map[string]string{
				{"name": "production", "cluster": "workload"},
				{"name": "staging", "cluster": "workload"},
			},
			"gitops": map[string]string{
				"repoName": TenantID + "-gitops",
				"branch":   "main",
			},
			"preview": map[string]interface{}{
				"maxEnvironments": 3,
				"ttlHours":        72,
			},
			"limits": map[string]interface{}{
				"maxCPU":              "4",
				"maxMemory":           "8Gi",
				"maxStorage":          "20Gi",
				"maxBuildConcurrency": 2,
			},
		},
	}

	crJSON, err := json.Marshal(tenantCR)
	if err != nil {
		return fmt.Errorf("failed to marshal tenant CR: %w", err)
	}

	cmd := exec.Command("kubectl", "--context", kubeContext, "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(crJSON)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w\noutput: %s", err, output)
	}
	return nil
}

// expectedNamespace describes a namespace that must exist on a specific
// kubectl context after TenantController reconciliation.
type expectedNamespace struct {
	context string
	name    string
}

// WaitForReconciliation polls for namespace existence with the given timeout.
//
// The TenantController materializes namespaces across multiple clusters:
//   - workload cluster: loopzero-{tenant}-production, loopzero-{tenant}-staging
//     (canonical naming pkg adds "loopzero-" prefix per ADR-0018)
//   - build cluster:    build-{tenant}
//
// The kubeContext arg is kept for back-compat (was the mgmt context) but is
// now ignored — each expected namespace declares its own target context.
func WaitForReconciliation(kubeContext string, timeout time.Duration) error {
	_ = kubeContext // legacy parameter, retained for back-compat
	expected := ExpectedNamespacesByContext()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		missing := findMissingNamespacesByContext(expected)
		if len(missing) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout after %s waiting for namespaces: %s", timeout, strings.Join(missing, ", "))
		case <-ticker.C:
			// continue polling
		}
	}
}

// ExpectedNamespaces returns just the namespace names (back-compat).
// Prefer ExpectedNamespacesByContext for cross-cluster correctness.
func ExpectedNamespaces() []string {
	out := make([]string, 0, 3)
	for _, e := range ExpectedNamespacesByContext() {
		out = append(out, e.name)
	}
	return out
}

// ExpectedNamespacesByContext returns each expected namespace paired with the
// kubectl context where it should exist.
func ExpectedNamespacesByContext() []expectedNamespace {
	return []expectedNamespace{
		// Workload namespaces are created by the operator with "loopzero-" prefix
		// on the workload cluster (cell context).
		{context: "k3d-loopzero-workload", name: "loopzero-" + TenantID + "-production"},
		{context: "k3d-loopzero-workload", name: "loopzero-" + TenantID + "-staging"},
		// Build namespace is created on the build cluster (no "loopzero-" prefix).
		{context: "k3d-loopzero-build", name: "build-" + TenantID},
	}
}

// findMissingNamespacesByContext returns "ctx/name" identifiers for any
// expected namespace that does not yet exist on its target context.
func findMissingNamespacesByContext(expected []expectedNamespace) []string {
	var missing []string
	for _, e := range expected {
		cmd := exec.Command("kubectl", "--context", e.context,
			"get", "namespace", e.name, "--no-headers", "-o", "name")
		if err := cmd.Run(); err != nil {
			missing = append(missing, e.context+"/"+e.name)
		}
	}
	return missing
}

// findMissingNamespaces is the legacy single-context probe, retained for
// back-compat with callers that pass a specific context.
func findMissingNamespaces(kubeContext string, expected []string) []string {
	var missing []string
	for _, ns := range expected {
		cmd := exec.Command("kubectl", "--context", kubeContext,
			"get", "namespace", ns, "--no-headers", "-o", "name")
		if err := cmd.Run(); err != nil {
			missing = append(missing, ns)
		}
	}
	return missing
}
