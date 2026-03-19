// Package integration tests concurrent build submission and idempotency behavior.
//
// These tests prove:
// 1. Two concurrent POST /builds for the same environment → exactly one 202, one 409
// 2. Idempotency replay: same key + same payload → same cached response
// 3. Idempotency mismatch: same key + different payload → 422
// 4. Lock released after first build completes → second build succeeds
//
// These tests require a real PostgreSQL instance. They skip gracefully if
// DATABASE_URL is not set and the default connection fails.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/sse"
	"github.com/loopzero/loopzero/pkg/logging"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- Route-level adapter: bridges services.BuildOrchestrator to routes.BuildOrchestratorService ---

// buildOrchestratorAdapter adapts the real services.BuildOrchestrator to the
// routes.BuildOrchestratorService interface used by the HTTP handler.
type buildOrchestratorAdapter struct {
	orch       *services.BuildOrchestrator
	buildStore db.BuildStore
}

func (a *buildOrchestratorAdapter) SubmitBuild(ctx context.Context, req routes.SubmitBuildInput) (*routes.SubmitBuildResult, error) {
	var apps []services.AppSpec
	for _, app := range req.Apps {
		appType := app.Type
		if appType == "" {
			appType = "server"
		}
		appPort := app.Port
		if appPort == 0 && appType == "server" {
			appPort = 8080
		}
		apps = append(apps, services.AppSpec{
			Name: app.Name,
			Type: appType,
			Port: appPort,
		})
	}

	resp, err := a.orch.SubmitBuild(ctx, services.SubmitBuildRequest{
		TenantID:    req.TenantID,
		ProjectID:   req.ProjectID,
		Environment: req.Environment,
		CommitSHA:   "abc1234", // Integration test default
		Apps:        apps,
		CreatedBy:   req.CreatedBy,
	})
	if err != nil {
		return nil, err
	}

	return &routes.SubmitBuildResult{
		BuildID:   resp.BuildID,
		StreamURL: resp.StreamURL,
	}, nil
}

func (a *buildOrchestratorAdapter) GetBuildStatus(ctx context.Context, buildID string) (*routes.BuildStatusResponse, error) {
	build, err := a.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return nil, err
	}
	return &routes.BuildStatusResponse{
		BuildID:     build.ID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		Status:      build.Status,
		CommitSHA:   build.CommitSHA,
		CreatedBy:   build.CreatedBy,
	}, nil
}

func (a *buildOrchestratorAdapter) OnBuildCompleted(ctx context.Context, buildID string, result routes.BuildCallbackInput) error {
	var apps []services.AppRelease
	for _, app := range result.Apps {
		apps = append(apps, services.AppRelease{
			Name:        app.Name,
			ImageDigest: app.ImageDigest,
			URL:         app.URL,
			DurationMs:  app.DurationMs,
		})
	}
	return a.orch.OnBuildCompleted(ctx, buildID, services.BuildCallbackRequest{
		Status: result.Status,
		Apps:   apps,
	})
}

func (a *buildOrchestratorAdapter) CancelBuild(ctx context.Context, buildID string) error {
	return a.orch.CancelBuild(ctx, buildID)
}

func (a *buildOrchestratorAdapter) IsBuildTerminal(ctx context.Context, buildID string) (bool, error) {
	build, err := a.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return false, err
	}
	switch statemachine.State(build.Status) {
	case statemachine.BuildSucceeded, statemachine.BuildFailed, statemachine.BuildCancelled:
		return true, nil
	}
	return false, nil
}

func (a *buildOrchestratorAdapter) GetBuildTenantID(ctx context.Context, buildID string) (string, error) {
	build, err := a.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return "", err
	}
	return build.TenantID, nil
}

// --- Fake external services (Argo Workflows, Gitea, ArgoCD, health probes) ---

type fakeWorkflowSubmitter struct{}

func (f *fakeWorkflowSubmitter) Submit(ctx context.Context, spec services.WorkflowSpec) (string, error) {
	return "wf-" + spec.BuildID, nil
}

func (f *fakeWorkflowSubmitter) Cancel(ctx context.Context, workflowID string) error {
	return nil
}

func (f *fakeWorkflowSubmitter) GetStatus(ctx context.Context, workflowID string) (*services.WorkflowStatus, error) {
	return &services.WorkflowStatus{Phase: "Succeeded"}, nil
}

type fakeGitOpsPusher struct{}

func (f *fakeGitOpsPusher) Push(ctx context.Context, opts services.GitOpsPushOpts) (string, error) {
	return "fake-commit-sha", nil
}

type fakeSyncClient struct{}

func (f *fakeSyncClient) TriggerSync(ctx context.Context, appName string) error {
	return nil
}

func (f *fakeSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration) error {
	return nil
}

// --- Fake auth for HTTP layer ---

type fakeTokenValidator struct{}

func (f *fakeTokenValidator) ValidateToken(ctx context.Context, token string) (*middleware.TokenValidationResult, error) {
	if token == "valid-token" {
		return &middleware.TokenValidationResult{
			UserID:   "user-test",
			Email:    "test@example.com",
			GitHubID: 12345,
		}, nil
	}
	return nil, middleware.ErrInvalidToken
}

type fakeMembershipStore struct{}

func (f *fakeMembershipStore) GetMemberships(ctx context.Context, userID string) ([]middleware.TenantMembership, error) {
	return []middleware.TenantMembership{
		{TenantID: "tenant-test", Role: "owner"},
	}, nil
}

// --- Test setup ---

// testIntegrationSetup holds all the components needed for integration tests.
type testIntegrationSetup struct {
	pool       *pgxpool.Pool
	server     *httptest.Server
	buildStore db.BuildStore
	adapter    *db.PgxAdapter
}

// setupIntegrationTest creates a real PostgreSQL-backed test server.
// It runs all migrations, seeds required data, and returns a teardown function.
func setupIntegrationTest(t *testing.T) *testIntegrationSetup {
	t.Helper()

	pool := testPool(t)

	// Run migrations
	runMigrations(t, pool)

	// Seed test data
	seedTestData(t, pool)

	// Create real stores backed by the pool
	adapter := &db.PgxAdapter{Pool: pool}
	buildStore := db.NewPostgresBuildStore(adapter)
	releaseStore := db.NewPostgresReleaseStore(adapter)
	envTargetStore := db.NewPostgresEnvironmentTargetStore(adapter, adapter)
	eventStore := sse.NewPostgresEventStore(adapter, adapter)
	idempotencyStore := &db.PostgresIdempotencyStore{DB: adapter}

	// Create real locker backed by the pool (reusing adapters package — no duplication)
	locker := &adapters.PoolEnvironmentLocker{Pool: pool}

	// Create real orchestrator with fake external services
	orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
		BuildStore:     buildStore,
		ReleaseStore:   releaseStore,
		EnvTargetStore: envTargetStore,
		EventStore:     eventStore,
		Workflow:       &fakeWorkflowSubmitter{},
		Sync:           &fakeSyncClient{},
		Locker:         locker,
		GitOps:         &fakeGitOpsPusher{},
	})

	// Create route-level adapter
	orchAdapter := &buildOrchestratorAdapter{
		orch:       orch,
		buildStore: buildStore,
	}

	// Create real server with real middleware
	logger := logging.NewLogger("test")

	buildHandler := routes.NewBuildHandler(orchAdapter, logger)
	router := server.NewRouter(server.Config{
		Port:               "0",
		Logger:             logger,
		DB:                 adapter,
		TokenValidator:     &fakeTokenValidator{},
		MemberStore:        &fakeMembershipStore{},
		IdempotencyStore:   idempotencyStore,
		BuildHandler:       buildHandler,
		ArgoCallbackSecret: "test-secret",
		EventStore:         eventStore,
	})

	srv := httptest.NewServer(router)
	t.Cleanup(func() {
		srv.Close()
	})

	return &testIntegrationSetup{
		pool:       pool,
		server:     srv,
		buildStore: buildStore,
		adapter:    adapter,
	}
}

// migrationsDir returns the absolute path to the migrations directory.
// Uses runtime.Caller to locate this test file and derives the path from there.
func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile: .../api/tests/integration/concurrent_builds_test.go
	// we want:  .../api/internal/db/migrations/
	apiDir := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(apiDir, "internal", "db", "migrations")
}

// runMigrations applies all SQL migration files to the test database.
func runMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	dir := migrationsDir()

	migrations := []string{
		filepath.Join(dir, "000001_initial_schema.up.sql"),
		filepath.Join(dir, "000002_auth_tenants.up.sql"),
		filepath.Join(dir, "000003_deploy_support.up.sql"),
	}

	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", path, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			// Ignore errors from IF NOT EXISTS / IF EXISTS statements
			t.Logf("migration %s: %v (may be expected for idempotent migrations)", path, err)
		}
	}
}

// seedTestData creates the test tenant, user, project, and membership records.
func seedTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Clean up any existing test data (idempotent)
	cleanupTestData(t, pool)

	// Create test user
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, github_id, display_name)
		 VALUES ('user-test', 'test@example.com', 12345, 'Test User')
		 ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	// Create test tenant
	_, err = pool.Exec(ctx,
		`INSERT INTO tenants (id, display_name, plan, owner_id)
		 VALUES ('tenant-test', 'Test Tenant', 'starter', 'user-test')
		 ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		t.Fatalf("failed to seed tenant: %v", err)
	}

	// Create tenant membership
	_, err = pool.Exec(ctx,
		`INSERT INTO tenant_members (tenant_id, user_id, role)
		 VALUES ('tenant-test', 'user-test', 'owner')
		 ON CONFLICT (tenant_id, user_id) DO NOTHING`)
	if err != nil {
		t.Fatalf("failed to seed membership: %v", err)
	}

	// Create test project
	_, err = pool.Exec(ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by)
		 VALUES ('proj-test1234', 'tenant-test', 'test-project', 'user-test')
		 ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		t.Fatalf("failed to seed project: %v", err)
	}
}

// cleanupTestData removes test records to ensure test isolation.
func cleanupTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Clean up in reverse dependency order
	tables := []string{
		"DELETE FROM sse_events WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = 'tenant-test')",
		"DELETE FROM idempotency_keys WHERE tenant_id = 'tenant-test'",
		"DELETE FROM environment_targets WHERE tenant_id = 'tenant-test'",
		"DELETE FROM build_apps WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = 'tenant-test')",
		"DELETE FROM releases WHERE tenant_id = 'tenant-test'",
		"DELETE FROM builds WHERE tenant_id = 'tenant-test'",
	}

	for _, q := range tables {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Logf("cleanup query %q: %v", q, err)
		}
	}
}

// --- Helper functions ---

// doPost sends a POST request to the test server with optional headers.
func doPost(serverURL, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer valid-token")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return http.DefaultClient.Do(req)
}

// readBody reads and closes the response body.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return body
}

// --- Tests ---

func TestConcurrentBuildSubmission(t *testing.T) {
	// Two goroutines both call POST /builds simultaneously for the same
	// (tenant, project, environment). Exactly one should get 202, the other 409.
	// Only one Build record should exist.

	setup := setupIntegrationTest(t)

	// Clean builds before each test
	cleanupTestData(t, setup.pool)

	reqBody := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "production",
		"apps":        []string{"api"},
	}

	// Use a barrier to synchronize both goroutines
	ready := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	results := make([]struct {
		status int
		body   []byte
	}, 2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			<-ready // Wait for barrier

			resp, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, nil)
			if err != nil {
				t.Errorf("goroutine %d: request failed: %v", idx, err)
				return
			}
			results[idx].status = resp.StatusCode
			results[idx].body = readBody(t, resp)
		}(i)
	}

	// Release both goroutines simultaneously
	close(ready)
	wg.Wait()

	// Assert: exactly one 202, one 409
	statuses := []int{results[0].status, results[1].status}
	has202 := false
	has409 := false
	for _, s := range statuses {
		switch s {
		case http.StatusAccepted:
			if has202 {
				t.Fatalf("both goroutines got 202 — lock did not serialize. Statuses: %v", statuses)
			}
			has202 = true
		case http.StatusConflict:
			if has409 {
				t.Fatalf("both goroutines got 409 — neither succeeded. Statuses: %v", statuses)
			}
			has409 = true
		default:
			t.Fatalf("unexpected status %d. Body: %s", s, string(results[0].body))
		}
	}

	if !has202 || !has409 {
		t.Fatalf("expected exactly one 202 and one 409, got statuses: %v", statuses)
	}

	// Assert: 409 response contains DEPLOY_IN_PROGRESS error code
	for i, r := range results {
		if r.status == http.StatusConflict {
			var errResp map[string]string
			if err := json.Unmarshal(r.body, &errResp); err != nil {
				t.Fatalf("failed to parse 409 response body: %v", err)
			}
			if errResp["code"] != "DEPLOY_IN_PROGRESS" {
				t.Fatalf("goroutine %d: expected error code DEPLOY_IN_PROGRESS, got %s", i, errResp["code"])
			}
			break
		}
	}

	// Assert: only one Build record exists with status QUEUED or RUNNING
	var count int
	err := setup.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM builds
		 WHERE tenant_id = 'tenant-test' AND project_id = 'proj-test1234'
		   AND environment = 'production' AND status IN ('QUEUED', 'RUNNING')`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count builds: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 active build, got %d", count)
	}
}

func TestIdempotencyReplay(t *testing.T) {
	// First call with Idempotency-Key gets 202.
	// Second call with SAME key + SAME payload gets 202 (replay).
	// Both responses should be identical.

	setup := setupIntegrationTest(t)
	cleanupTestData(t, setup.pool)

	reqBody := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "staging",
		"apps":        []string{"web"},
	}

	idempotencyKey := fmt.Sprintf("idem-replay-%d", time.Now().UnixNano())
	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}

	// First call
	resp1, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, headers)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	body1 := readBody(t, resp1)

	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d. Body: %s", resp1.StatusCode, body1)
	}

	// Second call with same key + same payload
	resp2, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, headers)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	body2 := readBody(t, resp2)

	// Assert: same status code (202)
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("second request: expected 202 (replay), got %d. Body: %s", resp2.StatusCode, body2)
	}

	// Assert: same response body
	var resp1JSON, resp2JSON map[string]interface{}
	if err := json.Unmarshal(body1, &resp1JSON); err != nil {
		t.Fatalf("failed to parse first response: %v", err)
	}
	if err := json.Unmarshal(body2, &resp2JSON); err != nil {
		t.Fatalf("failed to parse second response: %v", err)
	}

	if resp1JSON["build_id"] != resp2JSON["build_id"] {
		t.Fatalf("idempotency replay returned different build_id: %v vs %v",
			resp1JSON["build_id"], resp2JSON["build_id"])
	}
	if resp1JSON["stream_url"] != resp2JSON["stream_url"] {
		t.Fatalf("idempotency replay returned different stream_url: %v vs %v",
			resp1JSON["stream_url"], resp2JSON["stream_url"])
	}

	// Assert: only one Build record exists (not two)
	var count int
	err = setup.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM builds
		 WHERE tenant_id = 'tenant-test' AND project_id = 'proj-test1234'
		   AND environment = 'staging'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count builds: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 build (idempotency should prevent duplicate), got %d", count)
	}
}

func TestIdempotencyMismatch(t *testing.T) {
	// First call with key K and payload A gets 202.
	// Second call with key K and payload B gets 422.

	setup := setupIntegrationTest(t)
	cleanupTestData(t, setup.pool)

	payloadA := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "dev",
		"apps":        []string{"api"},
	}

	payloadB := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "staging",
		"apps":        []string{"web"},
	}

	idempotencyKey := fmt.Sprintf("idem-mismatch-%d", time.Now().UnixNano())
	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}

	// First call with payload A
	resp1, err := doPost(setup.server.URL, "/api/v1/builds", payloadA, headers)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	body1 := readBody(t, resp1)

	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d. Body: %s", resp1.StatusCode, body1)
	}

	// Second call with payload B (different payload, same key)
	resp2, err := doPost(setup.server.URL, "/api/v1/builds", payloadB, headers)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	body2 := readBody(t, resp2)

	// Assert: 422 Unprocessable Entity
	if resp2.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("second request: expected 422, got %d. Body: %s", resp2.StatusCode, body2)
	}

	// Assert: error code is IDEMPOTENCY_MISMATCH
	var errResp map[string]string
	if err := json.Unmarshal(body2, &errResp); err != nil {
		t.Fatalf("failed to parse 422 response body: %v", err)
	}
	if errResp["code"] != "IDEMPOTENCY_MISMATCH" {
		t.Fatalf("expected error code IDEMPOTENCY_MISMATCH, got %s", errResp["code"])
	}
}

func TestLockReleasedAfterBuildCompletes(t *testing.T) {
	// Submit build 1: gets 202.
	// Complete build 1 (transition to terminal state).
	// Submit build 2: gets 202 (not 409, because the first build is no longer active).

	setup := setupIntegrationTest(t)
	cleanupTestData(t, setup.pool)

	reqBody := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "production",
		"apps":        []string{"api"},
	}

	// Submit build 1
	resp1, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, nil)
	if err != nil {
		t.Fatalf("first build request failed: %v", err)
	}
	body1 := readBody(t, resp1)

	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first build: expected 202, got %d. Body: %s", resp1.StatusCode, body1)
	}

	var build1Resp map[string]string
	if err := json.Unmarshal(body1, &build1Resp); err != nil {
		t.Fatalf("failed to parse first build response: %v", err)
	}
	buildID := build1Resp["build_id"]
	if buildID == "" {
		t.Fatal("first build response missing build_id")
	}

	// Transition build 1 to terminal state (SUCCEEDED) directly in DB.
	// The orchestrator's OnBuildCompleted would do this, but it involves
	// external services (gitops, sync). For this test, we transition directly
	// to prove the concurrent-build check works based on DB state.
	ctx := context.Background()

	// QUEUED -> RUNNING is already done by SubmitBuild.
	// Now RUNNING -> SUCCEEDED.
	_, err = setup.pool.Exec(ctx,
		`UPDATE builds SET status = $1, finished_at = NOW()
		 WHERE id = $2`,
		string(statemachine.BuildSucceeded), buildID,
	)
	if err != nil {
		t.Fatalf("failed to transition build to SUCCEEDED: %v", err)
	}

	// Verify build is terminal
	var status string
	err = setup.pool.QueryRow(ctx,
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&status)
	if err != nil {
		t.Fatalf("failed to read build status: %v", err)
	}
	if status != string(statemachine.BuildSucceeded) {
		t.Fatalf("expected build status SUCCEEDED, got %s", status)
	}

	// Submit build 2: should succeed because build 1 is no longer active
	resp2, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, nil)
	if err != nil {
		t.Fatalf("second build request failed: %v", err)
	}
	body2 := readBody(t, resp2)

	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("second build: expected 202 (lock released), got %d. Body: %s",
			resp2.StatusCode, body2)
	}

	// Assert: two builds exist — one SUCCEEDED, one RUNNING
	var totalBuilds int
	err = setup.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM builds
		 WHERE tenant_id = 'tenant-test' AND project_id = 'proj-test1234'
		   AND environment = 'production'`,
	).Scan(&totalBuilds)
	if err != nil {
		t.Fatalf("failed to count builds: %v", err)
	}
	if totalBuilds != 2 {
		t.Fatalf("expected 2 total builds, got %d", totalBuilds)
	}

	var activeBuilds int
	err = setup.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM builds
		 WHERE tenant_id = 'tenant-test' AND project_id = 'proj-test1234'
		   AND environment = 'production' AND status IN ('QUEUED', 'RUNNING')`,
	).Scan(&activeBuilds)
	if err != nil {
		t.Fatalf("failed to count active builds: %v", err)
	}
	if activeBuilds != 1 {
		t.Fatalf("expected 1 active build, got %d", activeBuilds)
	}
}
