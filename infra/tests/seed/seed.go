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
//	    KubeContext: "k3d-loopzero-core",
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

	"github.com/jackc/pgx/v5"
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
	//
	// DEPRECATED for HTTP API tests since dogfood-2026-05-08-fixes T0.2:
	// `e2e-token-dev` is only valid in dev-mode API stub; against real
	// Hydra-minted JWT API it returns 401. Use `resolveToken()` from
	// helpers_test.go which prioritizes process env LOOPZERO_TOKEN, then
	// ~/.loopzero/local-dogfood-token.txt, and falls back to AuthToken last.
	// AuthToken kept here as that final fallback only.
	AuthToken = "e2e-token-dev"
	// Username is the unique handle for the E2E user (users.username NOT NULL).
	Username = "e2e-user"
)

// Config holds configuration for the seed operation.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// KubeContext is the kubectl context to use (default: k3d-loopzero-core).
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
		c.KubeContext = "k3d-loopzero-core"
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

-- Strip orphan app_scaling_config rows for this tenant. The project_deleter
-- cascade was added 2026-05-06 (CHG-AUDIT-RESOURCE-ID + scaling cascade),
-- so historical sessions may have left rows behind. Clean before seed so
-- ScalingResolver sees pristine state when journeys (re)deploy fixtures.
DELETE FROM app_scaling_config WHERE tenant_id = '%s';
`+`
-- Defensive: ensure cell-default exists in cells before tenant INSERT
-- (dogfood-2026-05-08-fixes T3.1, ADR D4 + EC-4 mitigation). Currently
-- tenants.cell_id has DEFAULT 'cell-default' so the INSERT below works
-- even without an explicit value, but if a future migration adds a FK
-- to cells.cell_id this guard prevents a violation. Idempotent.
INSERT INTO cells (cell_id, display_name, status, region)
VALUES ('cell-default', 'Default cell (local)', 'active', 'local')
ON CONFLICT (cell_id) DO NOTHING;

INSERT INTO users (id, email, github_id, display_name, username)
VALUES ('%s', '%s', %d, 'e2e-user', '%s')
ON CONFLICT (id) DO NOTHING;

-- tenants.cell_id is NOT NULL with DEFAULT 'cell-default' so this INSERT
-- works even without an explicit value. Explicit cell_id improves clarity
-- and ensures behaviour is identical regardless of future schema changes.
-- COALESCE on conflict preserves any manual pin (e.g., from loopzero admin
-- tenants pin) on re-seed (EC-11).
INSERT INTO tenants (id, display_name, slug, owner_id, cell_id)
VALUES ('%s', '%s', '%s', '%s', 'cell-default')
ON CONFLICT (id) DO UPDATE SET cell_id = COALESCE(tenants.cell_id, EXCLUDED.cell_id);

-- Pin the e2e tenant to the enterprise plan so the suite is not capped
-- by builds_per_month / projects / seats limits. The plan_limits row for
-- enterprise is NULL,NULL (unlimited) per migration 000002. Without this
-- subscriptions row, the tenant defaults to starter (50 builds/month) and
-- a long session of journey reruns trips PLAN_LIMIT_EXCEEDED.
--
-- ORDER MATTERS: subscriptions.tenant_id has FK to tenants.id, so this
-- INSERT MUST follow INSERT INTO tenants above. Cleanroom dogfood 2026-05-07
-- caught the inverted ordering (F-01) when pristine DB had no e2e-tenant row.
INSERT INTO subscriptions (id, tenant_id, plan, status)
VALUES ('sub-e2e-enterprise', '%s', 'enterprise', 'active')
ON CONFLICT (tenant_id) DO UPDATE SET plan = 'enterprise', status = 'active', updated_at = now();

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
		TenantID, // strip orphan app_scaling_config rows
		UserID, Email, GitHubID, Username,
		TenantID, DisplayName, TenantID, UserID, // slug = TenantID
		TenantID, // pin tenant to enterprise plan (subscriptions FK → tenants.id)
		TenantID, UserID, Role,
		TenantID, // bootstrap E2E membership
		ProjectID, TenantID, ProjectName, UserID,
	)

	// Use pgx directly (same driver as loopzero-api) instead of `psql` binary.
	// Plan 2026-05-19-drill-skills-rigor-restoration: drill validation uses
	// managed DOKS PG (no psql binary needed on host). Eliminates dependency
	// on system psql client — matches production posture (loopzero-api uses pgx,
	// not psql shell-out).
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("pgx connect failed: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("pgx exec failed: %w", err)
	}
	return nil
}

// kubectlArgs returns ["kubectl", "--kubeconfig", path] when DRILL_KUBECONFIG
// is set (drill validation against DOKS), otherwise ["kubectl", "--context", ctx]
// for local k3d. Plan 2026-05-19-drill-skills-rigor-restoration.
//
// Per-cluster kubeconfig resolution (drill multi-cluster):
//   - mgmt: $DRILL_KUBECONFIG (default for any context not in DRILL_KUBECONFIG_DIR)
//   - k3d-loopzero-build / k3d-loopzero-cell-0: $DRILL_KUBECONFIG_DIR/build.yaml /
//     $DRILL_KUBECONFIG_DIR/runtime.yaml if DRILL_KUBECONFIG_DIR set, else
//     defaults to mgmt kubeconfig.
func kubectlArgs(kubeContext string) []string {
	dir := os.Getenv("DRILL_KUBECONFIG_DIR")
	if dir != "" {
		// Map well-known local k3d contexts to drill cluster kubeconfigs.
		switch kubeContext {
		case "k3d-loopzero-build":
			return []string{"kubectl", "--kubeconfig", dir + "/build.yaml", "--insecure-skip-tls-verify"}
		case "k3d-loopzero-cell-0":
			return []string{"kubectl", "--kubeconfig", dir + "/runtime.yaml", "--insecure-skip-tls-verify"}
		case "k3d-loopzero-core", "":
			return []string{"kubectl", "--kubeconfig", dir + "/mgmt.yaml", "--insecure-skip-tls-verify"}
		}
	}
	if kc := os.Getenv("DRILL_KUBECONFIG"); kc != "" {
		return []string{"kubectl", "--kubeconfig", kc, "--insecure-skip-tls-verify"}
	}
	return []string{"kubectl", "--context", kubeContext}
}

// EnsureTenantCR creates the Tenant CR if absent, OR merges baseline envs
// (production + staging) into an existing CR without stripping envs added
// dynamically via the API (`loopzero deploy --env <jX>` → AddEnvironment).
//
// Sprint A trio-fix 2026-06-02 (continuação do c892e355): seedE2E is called
// from every journey's BeforeAll. Pre-fix, this `kubectl apply` overwrote
// `spec.environments` with [production, staging] every time, stripping the
// `j-solo`/`j-secrets`/`j-rollback`/etc envs that earlier journeys had
// added via the API. The tenant-controller's GC loop then cascade-deleted
// the orphan Environment CRs mid-flight, causing the downstream journey's
// pods to never start.
//
// LIVE evidence (commit log entry 2026-06-02):
//   - 00:38:33 — Journey 05 Promote BeforeAll → seedE2E → kubectl apply
//   - 00:38:34 — env-controller: "environment is being deleted" (j-solo)
//   - 00:38:54 — env-controller: "environment deletion complete"
//   - Test: pod label app=solo-server-api never appeared → 300s timeout FAIL
//
// Fix: if Tenant CR already exists, fetch its current envs, union with the
// baseline [production, staging], and submit the merged set. First-run
// (CR absent) preserves the legacy creation behavior.
func EnsureTenantCR(kubeContext string) error {
	// Probe for existing CR — get current envs to preserve.
	existingEnvs, exists, err := getExistingEnvs(kubeContext)
	if err != nil {
		// Surface unexpected errors; absent-CR is signaled by exists=false.
		return fmt.Errorf("probe existing tenant CR: %w", err)
	}

	envs := mergeEnvs(existingEnvs)
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
			"displayName":  DisplayName,
			"plan":         Plan,
			"environments": envs,
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
	_ = exists // currently unused but kept for symmetry; future: skip apply when no merge needed.

	crJSON, err := json.Marshal(tenantCR)
	if err != nil {
		return fmt.Errorf("failed to marshal tenant CR: %w", err)
	}

	args := append(kubectlArgs(kubeContext), "apply", "-f", "-")
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(crJSON)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w\noutput: %s", err, output)
	}
	return nil
}

// getExistingEnvs fetches `spec.environments` from the existing Tenant CR.
// Returns (envs, true, nil) when CR exists, (nil, false, nil) when absent,
// or (nil, false, err) on unexpected error.
func getExistingEnvs(kubeContext string) ([]map[string]string, bool, error) {
	args := append(kubectlArgs(kubeContext),
		"get", "tenant", TenantID, "-n", "loopzero-system",
		"-o", "jsonpath={.spec.environments}",
	)
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// `kubectl get` returns non-zero when CR doesn't exist; treat as absent.
		if bytes.Contains(out, []byte("not found")) || bytes.Contains(out, []byte("NotFound")) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("kubectl get tenant: %w\noutput: %s", err, out)
	}
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, true, nil // CR exists but no envs (edge case during teardown)
	}
	var rawEnvs []map[string]interface{}
	if err := json.Unmarshal(trimmed, &rawEnvs); err != nil {
		// Non-fatal: log and fall back to baseline. Better safe than strip.
		return nil, true, nil
	}
	out2 := make([]map[string]string, 0, len(rawEnvs))
	for _, e := range rawEnvs {
		name, _ := e["name"].(string)
		cluster, _ := e["cluster"].(string)
		if name == "" {
			continue
		}
		if cluster == "" {
			cluster = "workload" // baseline default
		}
		out2 = append(out2, map[string]string{"name": name, "cluster": cluster})
	}
	return out2, true, nil
}

// mergeEnvs unions existing envs with the baseline [production, staging],
// preserving dynamic-added entries (j-solo, j-secrets, etc) while ensuring
// the baseline is always present.
func mergeEnvs(existing []map[string]string) []map[string]string {
	baseline := []map[string]string{
		{"name": "production", "cluster": "workload"},
		{"name": "staging", "cluster": "workload"},
	}
	if len(existing) == 0 {
		return baseline
	}
	have := make(map[string]bool, len(existing))
	for _, e := range existing {
		have[e["name"]] = true
	}
	merged := make([]map[string]string, 0, len(existing)+len(baseline))
	merged = append(merged, existing...)
	for _, b := range baseline {
		if !have[b["name"]] {
			merged = append(merged, b)
		}
	}
	return merged
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
		{context: "k3d-loopzero-cell-0", name: "loopzero-" + TenantID + "-production"},
		{context: "k3d-loopzero-cell-0", name: "loopzero-" + TenantID + "-staging"},
		// Build namespace is created on the build cluster (no "loopzero-" prefix).
		{context: "k3d-loopzero-build", name: "build-" + TenantID},
	}
}

// findMissingNamespacesByContext returns "ctx/name" identifiers for any
// expected namespace that does not yet exist on its target context.
func findMissingNamespacesByContext(expected []expectedNamespace) []string {
	var missing []string
	for _, e := range expected {
		args := append(kubectlArgs(e.context), "get", "namespace", e.name, "--no-headers", "-o", "name")
		cmd := exec.Command(args[0], args[1:]...)
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
		args := append(kubectlArgs(kubeContext), "get", "namespace", ns, "--no-headers", "-o", "name")
		cmd := exec.Command(args[0], args[1:]...)
		if err := cmd.Run(); err != nil {
			missing = append(missing, ns)
		}
	}
	return missing
}
