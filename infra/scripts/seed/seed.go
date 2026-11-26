// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package seed provides idempotent E2E test data seeding for LoopRuntime.
//
// It can be used from E2E BeforeEach blocks or as a standalone setup step.
// All operations are idempotent: running multiple times produces the same result.
//
// Usage from tests:
//
//	seed.Run(seed.Config{
//	    DatabaseURL: "postgres://...",
//	    KubeContext: "k3d-loopruntime-core",
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

	"github.com/getloopscale/loop-runtime/infra/scripts/bootstrapuser"
	"github.com/getloopscale/loop-runtime/pkg/naming"
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
	// helpers_test.go which prioritizes process env LOOPRUNTIME_TOKEN, then
	// ~/.loopruntime/local-dogfood-token.txt, and falls back to AuthToken last.
	// AuthToken kept here as that final fallback only.
	AuthToken = "e2e-token-dev"
	// Username is the unique handle for the E2E user (users.username NOT NULL).
	Username = "e2e-user"
	// AccountID is the E2E user's account. tenants.account_id and
	// subscriptions.account_id are NOT NULL (migrations 000044 and 000043).
	AccountID = "acct-e2e"
)

// bootstrapUser is the Hydra JWT subject the dogfood token carries. The E2E
// seed leaves it a member of the E2E tenant only; the rule is the shared one
// the local and drill seeds apply (infra/scripts/bootstrapuser).
var bootstrapUser = bootstrapuser.User{
	ID:          "loopruntime-bootstrap",
	Email:       "bootstrap@loopruntime.local",
	DisplayName: "Bootstrap User",
	Username:    "bootstrap",
	AccountID:   "acct-bootstrap",
	TenantID:    TenantID,
	Role:        "owner",
}

// Config holds configuration for the seed operation.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// KubeContext is the kubectl context to use (default: k3d-loopruntime-core).
	KubeContext string
	// ReconcileTimeout is how long to wait for namespace reconciliation (default: 60s).
	ReconcileTimeout time.Duration
}

// defaults fills in zero-value fields with sensible defaults.
//
// DatabaseURL resolution order (first non-empty wins):
//  1. Config.DatabaseURL (programmatic override)
//  2. LOOPRUNTIME_E2E_DATABASE_URL env var (operator override — Tilt port-forward
//     uses 127.0.0.1:15432 with password "loopruntime-dev-password")
//  3. fallback "postgres://loopruntime:loopruntime@localhost:5432/loopruntime?sslmode=disable"
//     (legacy default; matches the old in-process PG that no longer exists)
func (c *Config) defaults() {
	if c.DatabaseURL == "" {
		if env := os.Getenv("LOOPRUNTIME_E2E_DATABASE_URL"); env != "" {
			c.DatabaseURL = env
		} else {
			c.DatabaseURL = "postgres://loopruntime:loopruntime@localhost:5432/loopruntime?sslmode=disable"
		}
	}
	if c.KubeContext == "" {
		c.KubeContext = "k3d-loopruntime-core"
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

// SeedDatabase inserts the E2E user, account, tenant, subscription, project and
// the bootstrap user's single membership through pgx. Every statement is
// idempotent.
func SeedDatabase(dbURL string) error {
	sql, err := seedSQL()
	if err != nil {
		return err
	}
	// Use pgx directly (same driver as loopruntime-api) instead of `psql` binary.
	// Plan 2026-05-19-drill-skills-rigor-restoration: drill validation uses
	// managed DOKS PG (no psql binary needed on host). Eliminates dependency
	// on system psql client — matches production posture (loopruntime-api uses pgx,
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

// seedSQL is the script SeedDatabase runs, in foreign-key order.
//
// LoopRuntime's CLI does not yet support the X-LoopRuntime-Tenant header (T2 — see plan
// MEET-ASYNC-AMENDMENT-002). Therefore the JWT subject (loopruntime-bootstrap) MUST
// resolve to a single tenant or every deploy fails with TENANT_REQUIRED. That
// rule is appended last, once the E2E tenant it points at exists; the dogfood
// seed-local-user.sh re-creates the tn-e2etest membership on demand, so moving
// the user here is reversible.
func seedSQL() (string, error) {
	sql := fmt.Sprintf(`
-- Strip orphan app_scaling_config rows for this tenant. The project_deleter
-- cascade was added 2026-05-06 (CHG-AUDIT-RESOURCE-ID + scaling cascade),
-- so historical sessions may have left rows behind. Clean before seed so
-- ScalingResolver sees pristine state when journeys (re)deploy fixtures.
DELETE FROM app_scaling_config WHERE tenant_id = '%s';

-- Defensive: ensure cell-0 exists in cells before tenant INSERT
-- (dogfood-2026-05-08-fixes T3.1, ADR D4 + EC-4 mitigation). Idempotent.
INSERT INTO cells (cell_id, display_name, status, region)
VALUES ('cell-0', 'Default cell (local)', 'active', 'local')
ON CONFLICT (cell_id) DO NOTHING;

INSERT INTO users (id, email, github_id, display_name, username)
VALUES ('%s', '%s', %d, 'e2e-user', '%s')
ON CONFLICT (id) DO NOTHING;

-- One account per owner (UNIQUE owner_id, migration 000034); an account the
-- user already owns is kept.
INSERT INTO accounts (id, display_name, plan, owner_id)
VALUES ('%s', '%s', 'starter', '%s')
ON CONFLICT (owner_id) DO NOTHING;

-- COALESCE on conflict preserves any manual pin (e.g., from loopruntime admin
-- tenants pin) on re-seed (EC-11). account_id is resolved by owner so the
-- tenant lands on whichever account the E2E user owns.
INSERT INTO tenants (id, display_name, slug, owner_id, cell_id, account_id)
SELECT '%s', '%s', '%s', '%s', 'cell-0', a.id FROM accounts a WHERE a.owner_id = '%s'
ON CONFLICT (id) DO UPDATE SET cell_id = COALESCE(tenants.cell_id, EXCLUDED.cell_id);

-- Pin the e2e tenant to the enterprise plan so the suite is not capped
-- by builds_per_month / projects / seats limits. The plan_limits row for
-- enterprise is NULL,NULL (unlimited) per migration 000002. Without this
-- subscriptions row, the tenant defaults to starter (50 builds/month) and
-- a long session of journey reruns trips PLAN_LIMIT_EXCEEDED.
--
-- ORDER MATTERS: subscriptions references tenants and accounts, so this
-- INSERT MUST follow both. The unique key is account_id since migration
-- 000042 dropped subscriptions_tenant_id_key.
INSERT INTO subscriptions (id, tenant_id, account_id, plan, status)
SELECT 'sub-e2e-enterprise', '%s', a.id, 'enterprise', 'active' FROM accounts a WHERE a.owner_id = '%s'
ON CONFLICT (account_id) DO UPDATE SET plan = 'enterprise', status = 'active', updated_at = now();

INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('%s', '%s', '%s')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

INSERT INTO projects (id, tenant_id, name, created_by)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT (id) DO NOTHING;
`,
		TenantID, // strip orphan app_scaling_config rows
		UserID, Email, GitHubID, Username,
		AccountID, DisplayName, UserID,
		TenantID, DisplayName, TenantID, UserID, UserID, // slug = TenantID
		TenantID, UserID, // pin tenant to enterprise plan
		TenantID, UserID, Role,
		ProjectID, TenantID, ProjectName, UserID,
	)
	bootstrap, err := bootstrapUser.SQL()
	if err != nil {
		return "", fmt.Errorf("bootstrap user seed: %w", err)
	}
	return sql + bootstrap, nil
}

// kubectlArgs returns ["kubectl", "--kubeconfig", path] when DRILL_KUBECONFIG
// is set (drill validation against DOKS), otherwise ["kubectl", "--context", ctx]
// for local k3d. Plan 2026-05-19-drill-skills-rigor-restoration.
//
// Per-cluster kubeconfig resolution (drill multi-cluster):
//   - mgmt: $DRILL_KUBECONFIG (default for any context not in DRILL_KUBECONFIG_DIR)
//   - k3d-loopruntime-build / k3d-loopruntime-cell-0: $DRILL_KUBECONFIG_DIR/build.yaml /
//     $DRILL_KUBECONFIG_DIR/runtime.yaml if DRILL_KUBECONFIG_DIR set, else
//     defaults to mgmt kubeconfig.
func kubectlArgs(kubeContext string) []string {
	dir := os.Getenv("DRILL_KUBECONFIG_DIR")
	if dir != "" {
		// Map well-known local k3d contexts to drill cluster kubeconfigs.
		switch kubeContext {
		case "k3d-loopruntime-build":
			return []string{"kubectl", "--kubeconfig", dir + "/build.yaml", "--insecure-skip-tls-verify"}
		case "k3d-loopruntime-cell-0":
			return []string{"kubectl", "--kubeconfig", dir + "/runtime.yaml", "--insecure-skip-tls-verify"}
		case "k3d-loopruntime-core", "":
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
// dynamically via the API (`loopruntime deploy --env <jX>` → AddEnvironment).
//
// Sprint A trio-fix 2026-06-02 (continuation of c892e355): seedE2E is called
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
		"apiVersion": "runtime.getloopscale.com/v1",
		"kind":       "Tenant",
		"metadata": map[string]interface{}{
			"name": TenantID,
			// Tenant-operator only reconciles CRs in loopruntime-system namespace;
			// "default" is invisible to its watch (verified empirically
			// 2026-05-06 — Tenant in "default" stays with empty PHASE while
			// tenants in "loopruntime-system" reconcile to Active within seconds).
			"namespace": "loopruntime-system",
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
		"get", "tenant", TenantID, "-n", "loopruntime-system",
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
	envs, err := parseEnvironments(trimmed)
	if err != nil {
		return nil, true, err
	}
	return envs, true, nil
}

// parseEnvironments reads a Tenant's spec.environments. An unreadable list is
// an error: re-applying the Tenant without it would strip every environment
// added since the seed last ran.
func parseEnvironments(raw []byte) ([]map[string]string, error) {
	var rawEnvs []map[string]interface{}
	if err := json.Unmarshal(raw, &rawEnvs); err != nil {
		return nil, fmt.Errorf("parse tenant %s spec.environments %q: %w", TenantID, raw, err)
	}
	envs := make([]map[string]string, 0, len(rawEnvs))
	for _, e := range rawEnvs {
		name, _ := e["name"].(string)
		if name == "" {
			continue
		}
		cluster, _ := e["cluster"].(string)
		if cluster == "" {
			cluster = "workload" // baseline default
		}
		project, _ := e["project"].(string)
		envs = append(envs, map[string]string{"name": name, "cluster": cluster, "project": project})
	}
	return envs, nil
}

// mergeEnvs unions existing envs with the baseline [production, staging],
// preserving dynamic-added entries (j-solo, j-secrets, etc) while ensuring
// the baseline is always present.
func mergeEnvs(existing []map[string]string) []map[string]string {
	baseline := []map[string]string{
		{"name": "production", "cluster": "workload", "project": ProjectName},
		{"name": "staging", "cluster": "workload", "project": ProjectName},
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
//   - workload cluster: loopruntime-{tenant}-{project}-production and -staging
//     (canonical naming pkg adds "loopruntime-" prefix per ADR-0018)
//   - build cluster:    build-{tenant}
//
// The kubeContext arg is kept for back-compat (was the mgmt context) but is
// now ignored — each expected namespace declares its own target context.
func WaitForReconciliation(kubeContext string, timeout time.Duration) error {
	_ = kubeContext // legacy parameter, retained for back-compat
	expected, err := ExpectedNamespacesByContext()
	if err != nil {
		return err
	}

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

// ExpectedNamespacesByContext returns each expected namespace paired with the
// kubectl context where it should exist. Names come from pkg/naming, the
// formula the operators create them with.
func ExpectedNamespacesByContext() ([]expectedNamespace, error) {
	var expected []expectedNamespace
	for _, env := range []string{"production", "staging"} {
		ns, err := naming.Namespace(TenantID, ProjectName, env)
		if err != nil {
			return nil, fmt.Errorf("expected %s namespace: %w", env, err)
		}
		expected = append(expected, expectedNamespace{context: "k3d-loopruntime-cell-0", name: ns})
	}
	build, err := naming.BuildNamespace(TenantID)
	if err != nil {
		return nil, fmt.Errorf("expected build namespace: %w", err)
	}
	return append(expected, expectedNamespace{context: "k3d-loopruntime-build", name: build}), nil
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
