// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
	"github.com/getloopscale/loopzero/api/internal/builds/buildshttp"
	"github.com/getloopscale/loopzero/api/internal/lock"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/builds"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/pkg/logging"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- Route-level adapter: bridges build.BuildOrchestrator to routes.BuildOrchestratorService ---

// buildOrchestratorAdapter adapts the real build.BuildOrchestrator to the
// routes.BuildOrchestratorService interface used by the HTTP handler.
type buildOrchestratorAdapter struct {
	orch       *builds.BuildOrchestrator
	buildStore db.BuildStore
}

func (a *buildOrchestratorAdapter) SubmitBuild(ctx context.Context, req buildshttp.SubmitBuildInput) (*buildshttp.SubmitBuildResult, error) {
	var apps []deploycontracts.AppSpec
	for _, app := range req.Apps {
		appType := app.Type
		if appType == "" {
			appType = "server"
		}
		appPort := app.Port
		if appPort == 0 && appType == "server" {
			appPort = 8080
		}
		apps = append(apps, deploycontracts.AppSpec{
			Name: app.Name,
			Type: appType,
			Port: appPort,
		})
	}

	resp, err := a.orch.SubmitBuild(ctx, deploycontracts.SubmitBuildRequest{
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

	return &buildshttp.SubmitBuildResult{
		BuildID:   resp.BuildID,
		StreamURL: resp.StreamURL,
	}, nil
}

func (a *buildOrchestratorAdapter) GetBuildStatus(ctx context.Context, buildID string) (*buildshttp.BuildStatusResponse, error) {
	build, err := a.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return nil, err
	}
	return &buildshttp.BuildStatusResponse{
		BuildID:     build.ID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		Status:      build.Status,
		CommitSHA:   build.CommitSHA,
		CreatedBy:   build.CreatedBy,
	}, nil
}

func (a *buildOrchestratorAdapter) CancelBuild(ctx context.Context, buildID string) error {
	return a.orch.CancelBuild(ctx, buildID)
}

func (a *buildOrchestratorAdapter) GetBuildTenantID(ctx context.Context, buildID string) (string, error) {
	build, err := a.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return "", err
	}
	return build.TenantID, nil
}

// GetRedeploySource — Plan J-17 T6.4. Stub for integration tests; not
// exercised by concurrent-builds scenarios. The production adapter
// (api/internal/builds/buildsadapter/build_adapter.go) carries the real impl.
func (a *buildOrchestratorAdapter) GetRedeploySource(_ context.Context, _, _ string) (*buildshttp.RedeploySource, error) {
	return nil, nil
}

// --- Fake external services (Argo Workflows, Gitea, ArgoCD, health probes) ---

type fakeWorkflowSubmitter struct{}

func (f *fakeWorkflowSubmitter) Submit(ctx context.Context, spec deploycontracts.WorkflowSpec) (string, error) {
	return "wf-" + spec.BuildID, nil
}

func (f *fakeWorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	return nil
}

func (f *fakeWorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*deploycontracts.WorkflowStatus, error) {
	return &deploycontracts.WorkflowStatus{Phase: "Succeeded"}, nil
}

type fakeGitOpsPusher struct{}

func (f *fakeGitOpsPusher) Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	return "fake-commit-sha", nil
}

func (f *fakeGitOpsPusher) Remove(ctx context.Context, opts deploycontracts.GitOpsRemoveOpts) (string, error) {
	return "fake-commit-sha", nil
}

type fakeSyncClient struct{}

func (f *fakeSyncClient) TriggerSync(ctx context.Context, appName string) error {
	return nil
}

func (f *fakeSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error {
	return nil
}

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. Integration concurrent-builds tests exercise only the
// HTTP-layer lock semantics; ArgoCD-side recovery is out of scope here.
func (f *fakeSyncClient) ForceReconcile(ctx context.Context, appName string) error {
	return nil
}

func (f *fakeSyncClient) GetSyncRevision(ctx context.Context, appName string) (string, error) {
	return "", nil
}

func (f *fakeSyncClient) TerminateOperation(ctx context.Context, appName string) error {
	return nil
}

func (f *fakeSyncClient) GetAppStatus(ctx context.Context, appName string, deployedApps []string) (bool, bool, error) {
	return true, true, nil
}

// --- Fake auth for HTTP layer ---

type fakeTokenValidator struct{}

func (f *fakeTokenValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	if token == "valid-token" {
		return &auth.TokenValidationResult{
			UserID: "user-test",
			Email:  "test@example.com",
		}, nil
	}
	return nil, auth.ErrInvalidToken
}

type fakeMembershipStore struct{}

func (f *fakeMembershipStore) GetMemberships(ctx context.Context, userID string) ([]auth.TenantMembership, error) {
	return []auth.TenantMembership{
		{TenantID: "tenant-test", Role: "owner"},
	}, nil
}

// --- Test setup ---

// testIntegrationSetup holds all the components needed for integration tests.
//
// Two pools, because production has two roles. `seed` is the cross-tenant
// fixture pool (seedDBURL): it writes and inspects fixture rows the way the
// background loops do. `adapter` is the request pool the server runs on,
// tenant-scoped like requestPoolConfig — the code under test never touches
// `seed`, so no fixture's scope can leak into it.
type testIntegrationSetup struct {
	seed       *pgxpool.Pool
	server     *httptest.Server
	buildStore db.BuildStore
	adapter    *db.PgxAdapter
}

// setupIntegrationTest creates a real PostgreSQL-backed test server over an
// already-migrated database (the harness applies the migrations; CI provisions
// them in the tenant-isolation job), seeds the required data, and serves the
// build routes on a tenant-scoped request pool.
func setupIntegrationTest(t *testing.T) *testIntegrationSetup {
	t.Helper()

	seed := seedPool(t)
	seedTestData(t, seed)

	// Enough connections for the concurrent submissions to contend on the
	// environment lock rather than on the pool.
	adapter := tenantScopedRequestPool(t, 8)
	buildStore := store.NewBuildStoreSqlc(adapter)                             // T1.2b
	releaseStore := store.NewReleaseStoreSqlc(adapter, store.NoopEventStore{}) // T1.1c2c (plan 2026-05-29-sqlc-migration v1.3)
	envTargetStore := store.NewPostgresEnvironmentTargetStore(adapter, adapter)
	eventStore := store.NewPostgresEventStore(adapter, adapter)
	idempotencyStore := &store.PostgresIdempotencyStore{DB: adapter}

	// Create real locker backed by the request pool, as setupStores does.
	locker := &lock.PoolEnvironmentLocker{Pool: adapter.Pool}

	// Create real orchestrator with fake external services
	orch := builds.NewBuildOrchestrator(builds.BuildOrchestratorDeps{
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

	buildHandler := buildshttp.NewBuildHandler(orchAdapter, logger)
	router := server.NewRouter(server.Config{
		Port:           "0",
		Logger:         logger,
		DB:             adapter,
		TokenValidator: &fakeTokenValidator{},
		MemberStore:    &fakeMembershipStore{},
		BuildHandler:   buildHandler,
		EventStore:     eventStore,
		Guards:         server.RouteGuards{IdempotencyStore: idempotencyStore},
	})

	srv := httptest.NewServer(router)
	t.Cleanup(func() {
		srv.Close()
	})

	return &testIntegrationSetup{
		seed:       seed,
		server:     srv,
		buildStore: buildStore,
		adapter:    adapter,
	}
}

// seedTestData creates the test tenant, user, project, and membership records.
//
// Through the cross-tenant fixture pool (seedDBURL), the way the bootstrap path
// creates them, and the Account through its owner (execAsAccountsOwner): since
// migration 000062 the tables are fail-closed and 000064 removed the owner's
// standing bypass, so a fixture that is not written by a role the policies name
// is refused.
func seedTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Clean up any existing test data (idempotent)
	cleanupTestData(t, pool)

	// Create test user (username NOT NULL + UNIQUE per migration 000034 —
	// user-test is stable so the ON CONFLICT still catches re-seeds).
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, github_id, display_name, username)
		 VALUES ('user-test', 'test@example.com', 12345, 'Test User', 'user-test')
		 ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	// Create the account the tenant belongs to. `tenants.account_id` became NOT
	// NULL when accounts were introduced, and this fixture predates that — it is
	// the same class of drift the CI comment names for the cell_id trigger: a
	// fixture frozen at an older schema. `accounts` carries no tenant column and
	// is deliberately outside RLS (it is the parent of the tenant, so scoping it
	// per tenant would be circular).
	if err := execAsAccountsOwner(ctx,
		`INSERT INTO accounts (id, display_name, owner_id)
		 VALUES ('acct-test', 'Test Account', 'user-test')
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("failed to seed account: %v", err)
	}

	// Create test tenant
	_, err = pool.Exec(ctx,
		`INSERT INTO tenants (id, display_name, slug, owner_id, account_id)
		 VALUES ('tenant-test', 'Test Tenant', 'test-tenant', 'user-test', 'acct-test')
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

	// Clean up in reverse dependency order, through the cross-tenant fixture
	// pool: DELETE is filtered by RLS too, and a role no policy names deletes
	// zero rows in silence — the next test would inherit this one's state.
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
			t.Fatalf("cleanup query %q: %v", q, err)
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
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return body
}

// --- Tests ---

// POST /api/v1/builds now REQUIRES `Idempotency-Key` (middleware
// MISSING_IDEMPOTENCY_KEY). These fixtures sent `nil` and got 400 before
// exercising the lock — another fixture frozen on an earlier contract. Each
// submission carries ITS OWN key: a shared key would turn the second call into
// an idempotent replay, which is the opposite of what these tests measure.
func TestConcurrentBuildSubmission(t *testing.T) {
	// Two goroutines both call POST /builds simultaneously for the same
	// (tenant, project, environment). Exactly one should get 202, the other 409.
	// Only one Build record should exist.

	setup := setupIntegrationTest(t)

	// Clean builds before each test
	cleanupTestData(t, setup.seed)

	reqBody := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "production",
		"apps":        []map[string]any{{"name": "api"}},
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

			resp, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, map[string]string{"Idempotency-Key": fmt.Sprintf("concurrent-%d-%d", time.Now().UnixNano(), idx)}) //nolint:bodyclose // closed via readBody helper below
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
			// map[string]any, not map[string]string: the error body is RFC 7807 and
			// carries a numeric `status`. With map[string]string the Unmarshal fails
			// entirely and the test dies BEFORE looking at the error code — another
			// fixture frozen on an earlier response format.
			var errResp map[string]any
			if err := json.Unmarshal(r.body, &errResp); err != nil {
				t.Fatalf("failed to parse 409 response body: %v — body: %s", err, string(r.body))
			}
			if errResp["code"] != "DEPLOY_IN_PROGRESS" {
				t.Fatalf("goroutine %d: expected error code DEPLOY_IN_PROGRESS, got %v", i, errResp["code"])
			}
			break
		}
	}

	// Assert: only one Build record exists with status QUEUED or RUNNING
	var count int
	err := setup.seed.QueryRow(context.Background(),
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

// The POST /api/v1/builds body declares `apps` as a list of OBJECTS
// (`[]AppInput`, with a required `name`), not of strings. These fixtures stayed
// in the old shape and the handler rejected them with 400 INVALID_REQUEST before
// touching any rule — the tests measured the parser, not the behaviour.
// Same family of drift as the cell_id trigger: a fixture frozen on an earlier
// contract.
func TestIdempotencyReplay(t *testing.T) {
	// First call with Idempotency-Key gets 202.
	// Second call with SAME key + SAME payload gets 202 (replay).
	// Both responses should be identical.

	setup := setupIntegrationTest(t)
	cleanupTestData(t, setup.seed)

	reqBody := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "staging",
		"apps":        []map[string]any{{"name": "web"}},
	}

	idempotencyKey := fmt.Sprintf("idem-replay-%d", time.Now().UnixNano())
	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}

	// First call
	resp1, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, headers) //nolint:bodyclose // closed via readBody helper below
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	body1 := readBody(t, resp1)

	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d. Body: %s", resp1.StatusCode, body1)
	}

	// Second call with same key + same payload
	resp2, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, headers) //nolint:bodyclose // closed via readBody helper below
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
	err = setup.seed.QueryRow(context.Background(),
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
	cleanupTestData(t, setup.seed)

	payloadA := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "dev",
		"apps":        []map[string]any{{"name": "api"}},
	}

	payloadB := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "staging",
		"apps":        []map[string]any{{"name": "web"}},
	}

	idempotencyKey := fmt.Sprintf("idem-mismatch-%d", time.Now().UnixNano())
	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}

	// First call with payload A
	resp1, err := doPost(setup.server.URL, "/api/v1/builds", payloadA, headers) //nolint:bodyclose // closed via readBody helper below
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	body1 := readBody(t, resp1)

	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d. Body: %s", resp1.StatusCode, body1)
	}

	// Second call with payload B (different payload, same key)
	resp2, err := doPost(setup.server.URL, "/api/v1/builds", payloadB, headers) //nolint:bodyclose // closed via readBody helper below
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
	cleanupTestData(t, setup.seed)

	reqBody := map[string]interface{}{
		"project_id":  "proj-test1234",
		"environment": "production",
		"apps":        []map[string]any{{"name": "api"}},
	}

	// Submit build 1
	resp1, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, map[string]string{"Idempotency-Key": fmt.Sprintf("lock-1-%d", time.Now().UnixNano())}) //nolint:bodyclose // closed via readBody helper below
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
	_, err = setup.seed.Exec(ctx,
		`UPDATE builds SET status = $1, finished_at = NOW()
		 WHERE id = $2`,
		string(statemachine.BPhSucceeded), buildID,
	)
	if err != nil {
		t.Fatalf("failed to transition build to SUCCEEDED: %v", err)
	}

	// Verify build is terminal
	var status string
	err = setup.seed.QueryRow(ctx,
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&status)
	if err != nil {
		t.Fatalf("failed to read build status: %v", err)
	}
	if status != string(statemachine.BPhSucceeded) {
		t.Fatalf("expected build status SUCCEEDED, got %s", status)
	}

	// Submit build 2: should succeed because build 1 is no longer active
	resp2, err := doPost(setup.server.URL, "/api/v1/builds", reqBody, map[string]string{"Idempotency-Key": fmt.Sprintf("lock-2-%d", time.Now().UnixNano())}) //nolint:bodyclose // closed via readBody helper below
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
	err = setup.seed.QueryRow(ctx,
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
	err = setup.seed.QueryRow(ctx,
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
