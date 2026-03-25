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
func (c *Config) defaults() {
	if c.DatabaseURL == "" {
		c.DatabaseURL = "postgres://loopzero:loopzero@localhost:5432/loopzero?sslmode=disable"
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
func SeedDatabase(dbURL string) error {
	sql := fmt.Sprintf(`
INSERT INTO users (id, email, github_id, display_name)
VALUES ('%s', '%s', %d, 'e2e-user')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenants (id, display_name, plan, owner_id)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('%s', '%s', '%s')
ON CONFLICT (tenant_id, user_id) DO NOTHING;

INSERT INTO projects (id, tenant_id, name, created_by)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT (id) DO NOTHING;
`,
		UserID, Email, GitHubID,
		TenantID, DisplayName, Plan, UserID,
		TenantID, UserID, Role,
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
			"namespace": "default",
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

// WaitForReconciliation polls for namespace existence with the given timeout.
func WaitForReconciliation(kubeContext string, timeout time.Duration) error {
	expectedNamespaces := ExpectedNamespaces()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		missing := findMissingNamespaces(kubeContext, expectedNamespaces)
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

// ExpectedNamespaces returns the list of namespaces that should exist
// after TenantController reconciliation.
func ExpectedNamespaces() []string {
	return []string{
		TenantID + "-production",
		TenantID + "-staging",
		"build-" + TenantID,
	}
}

// findMissingNamespaces returns namespace names that don't exist yet.
func findMissingNamespaces(kubeContext string, expected []string) []string {
	var missing []string
	for _, ns := range expected {
		cmd := exec.Command("kubectl", "--context", kubeContext, "get", "namespace", ns, "--no-headers", "-o", "name")
		if err := cmd.Run(); err != nil {
			missing = append(missing, ns)
		}
	}
	return missing
}
