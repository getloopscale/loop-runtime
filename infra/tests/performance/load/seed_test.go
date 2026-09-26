// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load provides load testing utilities for the LoopZero platform.
//
// This file contains the seed script that creates synthetic tenants,
// users, projects, and memberships for load testing.
//
// Approach:
//   - Seeds data directly in PostgreSQL (bypasses API auth flow)
//   - Uses X-Service-Auth for API calls (internal service auth)
//   - Creates Tenant CRs via API's EnsureTenant (triggers TenantController)
//   - All test data uses "loadtest-" prefix for identification and cleanup
//
// Usage:
//
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DB_URL=<pg-url> LOOPZERO_API_URL=<api-url> \
//	  LOOPZERO_SERVICE_AUTH=<secret> go test ./infra/tests/load/ -v -run TestSeed -timeout 30m
package load

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// SeedConfig controls how many synthetic resources to create.
type SeedConfig struct {
	TenantCount       int    // Number of synthetic tenants
	ProjectsPerTenant int    // Projects per tenant
	DBUrl             string // PostgreSQL connection string
	APIURL            string // LoopZero API endpoint
	ServiceAuthSecret string // X-Service-Auth shared secret
}

// SeedResult tracks what was created.
type SeedResult struct {
	UsersCreated       int
	TenantsCreated     int
	ProjectsCreated    int
	MembershipsCreated int
	Errors             []string
}

// Seed creates synthetic load test data in PostgreSQL.
// All resources use "loadtest-" prefix for identification.
//
// Schema:
//
//	users:          loadtest-user-{i}  / loadtest-{i}@loadtest.loopzero.dev / github_id=9000000+i
//	tenants:        loadtest-tn-{i}    / plan=starter / owner=loadtest-user-{i}
//	tenant_members: (loadtest-tn-{i}, loadtest-user-{i}) / role=owner
//	projects:       loadtest-proj-{i}-{j} / tenant=loadtest-tn-{i}
func Seed(ctx context.Context, cfg SeedConfig) (*SeedResult, error) {
	db, err := sql.Open("pgx", cfg.DBUrl)
	if err != nil {
		return nil, fmt.Errorf("connect to DB: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetConnMaxIdleTime(30 * time.Second)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping DB: %w", err)
	}

	result := &SeedResult{}

	for i := 0; i < cfg.TenantCount; i++ {
		userID := fmt.Sprintf("loadtest-user-%d", i)
		email := fmt.Sprintf("loadtest-%d@loadtest.loopzero.dev", i)
		githubID := 9000000 + i
		tenantID := fmt.Sprintf("loadtest-tn-%d", i)
		displayName := fmt.Sprintf("Load Test Tenant %d", i)

		// Insert user (idempotent)
		_, err := db.ExecContext(ctx, `
			INSERT INTO users (id, email, github_id, display_name)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING`,
			userID, email, githubID, fmt.Sprintf("LoadTest User %d", i))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("user %s: %v", userID, err))
			continue
		}
		result.UsersCreated++

		// Insert tenant (idempotent)
		_, err = db.ExecContext(ctx, `
			INSERT INTO tenants (id, display_name, plan, owner_id)
			VALUES ($1, $2, 'starter', $3)
			ON CONFLICT (id) DO NOTHING`,
			tenantID, displayName, userID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("tenant %s: %v", tenantID, err))
			continue
		}
		result.TenantsCreated++

		// Insert membership (idempotent)
		_, err = db.ExecContext(ctx, `
			INSERT INTO tenant_members (tenant_id, user_id, role)
			VALUES ($1, $2, 'owner')
			ON CONFLICT (tenant_id, user_id) DO NOTHING`,
			tenantID, userID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("membership %s/%s: %v", tenantID, userID, err))
			continue
		}
		result.MembershipsCreated++

		// Insert projects
		for j := 0; j < cfg.ProjectsPerTenant; j++ {
			projectID := fmt.Sprintf("loadtest-proj-%d-%d", i, j)
			projectName := fmt.Sprintf("loadtest-app-%d-%d", i, j)

			_, err = db.ExecContext(ctx, `
				INSERT INTO projects (id, tenant_id, name, created_by)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (id) DO NOTHING`,
				projectID, tenantID, projectName, userID)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("project %s: %v", projectID, err))
				continue
			}
			result.ProjectsCreated++
		}
	}

	return result, nil
}

// Cleanup removes all load test data (loadtest-* prefix).
// Order: projects → memberships → tenants → users (FK order).
func Cleanup(ctx context.Context, dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	queries := []struct {
		name  string
		query string
	}{
		{"environment_targets", `DELETE FROM environment_targets WHERE tenant_id LIKE 'loadtest-%'`},
		{"projects", `DELETE FROM projects WHERE tenant_id LIKE 'loadtest-%'`},
		{"memberships", `DELETE FROM tenant_members WHERE tenant_id LIKE 'loadtest-%'`},
		{"tenants", `DELETE FROM tenants WHERE id LIKE 'loadtest-%'`},
		{"users", `DELETE FROM users WHERE id LIKE 'loadtest-%'`},
	}

	for _, q := range queries {
		result, err := db.ExecContext(ctx, q.query)
		if err != nil {
			return fmt.Errorf("cleanup %s: %w", q.name, err)
		}
		rows, _ := result.RowsAffected()
		if rows > 0 {
			fmt.Printf("  Cleaned %s: %d rows\n", q.name, rows)
		}
	}

	return nil
}

// ServiceAuthClient wraps http.Client with X-Service-Auth headers.
// This bypasses GitHub OAuth for load testing, using the same internal
// auth mechanism that the cloud→engine proxy uses in production.
type ServiceAuthClient struct {
	client *http.Client
	apiURL string
	secret string
}

// NewServiceAuthClient creates an HTTP client that authenticates via X-Service-Auth.
func NewServiceAuthClient(apiURL, secret string) *ServiceAuthClient {
	return &ServiceAuthClient{
		client: &http.Client{Timeout: 60 * time.Second},
		apiURL: apiURL,
		secret: secret,
	}
}

// Do executes an HTTP request with service auth headers.
// The caller must provide userID, tenantID, and email for identity resolution.
func (c *ServiceAuthClient) Do(ctx context.Context, method, path string, body *http.Request, userID, tenantID, email string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, nil)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Body = body.Body
		req.Header.Set("Content-Type", body.Header.Get("Content-Type"))
	}
	req.Header.Set("X-Service-Auth", c.secret)
	req.Header.Set("X-LoopZero-User-ID", userID)
	req.Header.Set("X-LoopZero-Tenant", tenantID)
	req.Header.Set("X-LoopZero-Email", email)
	return c.client.Do(req)
}

// ---------------------------------------------------------------------------
// Test functions for seeding and cleanup
// ---------------------------------------------------------------------------

func requireSeedEnv(t *testing.T) SeedConfig {
	t.Helper()
	if os.Getenv("LOOPZERO_LOAD_TEST") != "1" {
		t.Skip("skipping: set LOOPZERO_LOAD_TEST=1")
	}

	dbURL := os.Getenv("LOOPZERO_DB_URL")
	if dbURL == "" {
		t.Fatal("LOOPZERO_DB_URL required (PostgreSQL connection string)")
	}

	apiURL := os.Getenv("LOOPZERO_API_URL")
	if apiURL == "" {
		apiURL = "https://api.dev.loopzero.example"
	}

	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	if serviceAuth == "" {
		t.Fatal("LOOPZERO_SERVICE_AUTH required (X-Service-Auth shared secret)")
	}

	tenants := 10 // default
	if v := os.Getenv("LOOPZERO_LOAD_TENANTS"); v != "" {
		fmt.Sscanf(v, "%d", &tenants)
	}

	return SeedConfig{
		TenantCount:       tenants,
		ProjectsPerTenant: 1,
		DBUrl:             dbURL,
		APIURL:            apiURL,
		ServiceAuthSecret: serviceAuth,
	}
}

// TestSeed_CreateTenants seeds synthetic load test data.
func TestSeed_CreateTenants(t *testing.T) {
	cfg := requireSeedEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Logf("Seeding %d tenants with %d projects each...", cfg.TenantCount, cfg.ProjectsPerTenant)

	result, err := Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("Seed failed: %v", err)
	}

	t.Logf("Seed complete:")
	t.Logf("  Users:       %d", result.UsersCreated)
	t.Logf("  Tenants:     %d", result.TenantsCreated)
	t.Logf("  Projects:    %d", result.ProjectsCreated)
	t.Logf("  Memberships: %d", result.MembershipsCreated)

	if len(result.Errors) > 0 {
		t.Logf("  Errors: %d", len(result.Errors))
		for _, e := range result.Errors {
			t.Logf("    - %s", e)
		}
	}

	if result.TenantsCreated != cfg.TenantCount {
		t.Errorf("expected %d tenants, created %d", cfg.TenantCount, result.TenantsCreated)
	}
}

// TestSeed_Cleanup removes all load test data.
func TestSeed_Cleanup(t *testing.T) {
	if os.Getenv("LOOPZERO_LOAD_TEST") != "1" {
		t.Skip("skipping: set LOOPZERO_LOAD_TEST=1")
	}

	dbURL := os.Getenv("LOOPZERO_DB_URL")
	if dbURL == "" {
		t.Fatal("LOOPZERO_DB_URL required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	t.Log("Cleaning up load test data (loadtest-* prefix)...")
	if err := Cleanup(ctx, dbURL); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	t.Log("Cleanup complete")
}

// TestSeed_VerifyServiceAuth verifies the X-Service-Auth bypass works.
func TestSeed_VerifyServiceAuth(t *testing.T) {
	cfg := requireSeedEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First seed at least 1 tenant
	seedCfg := cfg
	seedCfg.TenantCount = 1
	if _, err := Seed(ctx, seedCfg); err != nil {
		t.Fatalf("Seed failed: %v", err)
	}

	// Try API call with service auth
	client := NewServiceAuthClient(cfg.APIURL, cfg.ServiceAuthSecret)
	resp, err := client.Do(ctx, "GET", "/api/v1/projects",
		nil, "loadtest-user-0", "loadtest-tn-0", "loadtest-0@loadtest.loopzero.dev")
	if err != nil {
		t.Fatalf("Service auth request failed: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("Service auth response: HTTP %d", resp.StatusCode)
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d — service auth may not be working", resp.StatusCode)
	}
}
