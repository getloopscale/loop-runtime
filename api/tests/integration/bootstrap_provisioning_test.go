// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/bootstrap/bootstraphttp"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/pkg/domain"
)

// POST /internal/bootstrap is the engine's one provisioning path: placement
// picks the cell, and user, Account, tenant, membership and subscription are
// written in the placement transaction. These tests run it against the
// migrated schema, wired the way main wires it (on the cross-tenant pool).
//
//	DATABASE_URL=<loopzero_api> LOOPZERO_WORKER_DATABASE_URL=<loopzero_worker> \
//	  go test ./tests/integration/ -run Bootstrap -v
const bootstrapTestSecret = "bootstrap-test-secret"

func bootstrapHandler(t *testing.T) *bootstraphttp.BootstrapHandler {
	t.Helper()
	worker := &db.PgxAdapter{Pool: workerPool(t)}
	engine := placement.NewRoundRobinEngine(
		placement.NewPassthroughRouter(placement.CellConfig{CellID: domain.DefaultCellID}),
		&store.PostgresCellStore{DB: worker}, 0,
	).WithSerializer(placement.NewAdvisoryLockSerializer(worker.Pool))
	return bootstraphttp.NewBootstrapHandler(&store.PostgresBootstrapStore{DB: worker}, bootstrapTestSecret, engine)
}

func postBootstrap(t *testing.T, h *bootstraphttp.BootstrapHandler, body map[string]string) (*httptest.ResponseRecorder, bootstraphttp.BootstrapResponse) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+bootstrapTestSecret)
	rec := httptest.NewRecorder()
	h.Bootstrap(rec, req)
	var resp bootstraphttp.BootstrapResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

func bootstrapCleanup(t *testing.T, seed *sql.DB, owner string) {
	t.Helper()
	for _, s := range []string{
		`UPDATE users SET active_tenant_id = NULL WHERE id = $1`,
		`DELETE FROM subscriptions WHERE account_id IN (SELECT id FROM accounts WHERE owner_id = $1)`,
		`DELETE FROM tenant_members WHERE user_id = $1`,
		`DELETE FROM tenants WHERE owner_id = $1`,
		`DELETE FROM accounts WHERE owner_id = $1`,
		`DELETE FROM users WHERE id = $1`,
	} {
		if _, err := seed.Exec(s, owner); err != nil {
			t.Fatalf("cleanup %q: %v", s, err)
		}
	}
}

func openSeed(t *testing.T) *sql.DB {
	t.Helper()
	_ = testPool(t) // skips when PostgreSQL is unreachable
	seed, err := sql.Open("pgx", seedDBURL())
	if err != nil {
		t.Fatalf("open seed DB: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	return seed
}

func TestBootstrapProvisionsTheAccountTenantAndSubscriptionOfAnIdentity(t *testing.T) {
	seed := openSeed(t)
	const userID = "11111111-2222-3333-4444-555555555555"
	bootstrapCleanup(t, seed, userID)
	t.Cleanup(func() { bootstrapCleanup(t, seed, userID) })
	h := bootstrapHandler(t)

	rec, resp := postBootstrap(t, h, map[string]string{
		"admin_user_id": userID, "admin_login": "boot-admin", "admin_email": "boot-admin@e2e.test",
		"admin_display_name": "Boot Admin", "tenant_slug": "boot-admin", "plan": "team",
	})

	if rec.Code != http.StatusOK || resp.UserID != userID || resp.TenantID == "" {
		t.Fatalf("bootstrap = %d %s", rec.Code, rec.Body.String())
	}
	var cell, plan, activeTenant string
	var accountsOwned int
	if err := seed.QueryRow(`SELECT t.cell_id, s.plan, u.active_tenant_id,
	        (SELECT count(*) FROM accounts WHERE owner_id = $1)
	   FROM tenants t
	   JOIN subscriptions s ON s.account_id = t.account_id
	   JOIN users u ON u.id = t.owner_id
	  WHERE t.id = $2`, userID, resp.TenantID).Scan(&cell, &plan, &activeTenant, &accountsOwned); err != nil {
		t.Fatalf("read provisioned state: %v", err)
	}
	if cell != domain.DefaultCellID || plan != "team" || activeTenant != resp.TenantID || accountsOwned != 1 {
		t.Fatalf("provisioned cell=%q plan=%q active=%q accounts=%d", cell, plan, activeTenant, accountsOwned)
	}
}

func TestBootstrapIsIdempotentForTheSameIdentity(t *testing.T) {
	seed := openSeed(t)
	const userID = "11111111-2222-3333-4444-666666666666"
	bootstrapCleanup(t, seed, userID)
	t.Cleanup(func() { bootstrapCleanup(t, seed, userID) })
	h := bootstrapHandler(t)
	body := map[string]string{"admin_user_id": userID, "admin_login": "boot-again", "admin_email": "boot-again@e2e.test", "tenant_slug": "boot-again"}
	_, first := postBootstrap(t, h, body)

	rec, second := postBootstrap(t, h, body)

	if rec.Code != http.StatusOK || second.Created || second.TenantID != first.TenantID {
		t.Fatalf("second bootstrap = %d created=%v tenant=%q (first %q)", rec.Code, second.Created, second.TenantID, first.TenantID)
	}
}

// A slug another identity already owns is refused, never joined: the
// membership insert would otherwise make the caller an owner of someone
// else's tenant.
func TestBootstrapRefusesASlugOwnedByAnotherIdentity(t *testing.T) {
	seed := openSeed(t)
	const owner, intruder = "11111111-2222-3333-4444-777777777777", "11111111-2222-3333-4444-888888888888"
	for _, u := range []string{owner, intruder} {
		bootstrapCleanup(t, seed, u)
		t.Cleanup(func() { bootstrapCleanup(t, seed, u) })
	}
	h := bootstrapHandler(t)
	postBootstrap(t, h, map[string]string{"admin_user_id": owner, "admin_login": "boot-owner", "admin_email": "boot-owner@e2e.test", "tenant_slug": "boot-shared"})

	rec, _ := postBootstrap(t, h, map[string]string{"admin_user_id": intruder, "admin_login": "boot-intruder", "admin_email": "boot-intruder@e2e.test", "tenant_slug": "boot-shared"})

	var memberships int
	if err := seed.QueryRow(`SELECT count(*) FROM tenant_members WHERE user_id = $1`, intruder).Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if rec.Code != http.StatusConflict || memberships != 0 {
		t.Fatalf("intruder bootstrap = %d %s, memberships=%d; want 409 and none", rec.Code, rec.Body.String(), memberships)
	}
}

// The GitHub-identity form the platform bootstrap has always sent keeps
// working: the user is github-<login> and the Account is created with it.
func TestBootstrapProvisionsAGitHubIdentity(t *testing.T) {
	seed := openSeed(t)
	const userID = "github-boot-gh"
	bootstrapCleanup(t, seed, userID)
	t.Cleanup(func() { bootstrapCleanup(t, seed, userID) })
	h := bootstrapHandler(t)

	rec, resp := postBootstrap(t, h, map[string]string{"admin_github_id": "424242", "admin_login": "boot-gh", "tenant_slug": "boot-gh"})

	var accountID string
	if err := seed.QueryRow(`SELECT account_id FROM tenants WHERE id = $1`, resp.TenantID).Scan(&accountID); err != nil {
		t.Fatalf("bootstrap = %d %s; read tenant: %v", rec.Code, rec.Body.String(), err)
	}
	if rec.Code != http.StatusOK || resp.UserID != userID || accountID == "" {
		t.Fatalf("bootstrap = %d user=%q account=%q", rec.Code, resp.UserID, accountID)
	}
}
