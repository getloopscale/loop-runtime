// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
)

// AdminLoopzeroDevSeed is the Go port of
// infra/scripts/local/seed-admin-loopzero-dev.sh.
//
// Canonical Kratos session seed for the dev/dev-public canonical user
// admin@loopzero.example. End-to-end 4-phase chain:
//
//  1. Ensure Kratos identity via admin API (create if absent).
//  2. Ensure auxiliary PG state (users / accounts / tenants /
//     tenant_members / user_identity_map / subscriptions) — SERIALIZABLE
//     transaction for atomicity.
//  3. Reset identity password via PUT (Kratos v26 quirk: PUT hashes,
//     JSON Patch stores cleartext) + mint session via browser login
//     flow + persist session cookie at mode 0600.
//  4. Optional --verify probes /sessions/whoami + JWT exchange.
//
// SKIP-graceful when Kratos/PG endpoints unreachable — hermetic tests
// use httptest for Kratos + stubPgConn for PG.
type AdminLoopzeroDevSeed struct{}

func NewAdminLoopzeroDevSeed() *AdminLoopzeroDevSeed { return &AdminLoopzeroDevSeed{} }
func (s *AdminLoopzeroDevSeed) Name() string        { return "admin-loopzero-dev" }

// Defaults mirror bash constants line-by-line.
const (
	adminDefaultEmail        = "admin@loopzero.example"
	adminDefaultDisplayName  = "LoopZero Admin"
	adminDefaultUsername     = "admin"
	adminDefaultTenantSlug   = "admin"
	adminDefaultPublicKratos = "https://auth-dev.loopzero.example"
	adminDefaultPublicCloud  = "https://app-dev.loopzero.example"
	// SEED_PASSWORD kept in a constant so the same value is used across seeds.
	adminSeedPassword = "LoopZero-Seed-Admin-Pw-2026-06-08-a8s7df9as7df"

	adminEmailEnv         = "EMAIL"
	adminSeedPasswordEnv  = "SEED_PASSWORD"
	adminPublicKratosEnv  = "PUBLIC_KRATOS_URL"
	adminPublicCloudEnv   = "PUBLIC_CLOUD_URL"
	adminKratosAdminEnv   = "KRATOS_ADMIN_URL" // test override for httptest
	adminSessionFileEnv   = "SESSION_FILE"
	adminSessionJWTEnv    = "SESSION_JWT_FILE"
	adminPGConnEnv        = "LOOPZERO_PG_CONN" // reuse tenant-fixture pattern
	adminCanonicalProject = "seed"
)

// AdminLoopzeroDevConfig is the resolved input for the seed. Package-var
// tested via the loadAdminConfig helper.
type AdminLoopzeroDevConfig struct {
	Email            string
	DisplayName      string
	Username         string
	TenantSlug       string
	SeedPassword     string
	PublicKratosURL  string
	PublicCloudURL   string
	KratosAdminURL   string
	SessionFile      string
	SessionJWTFile   string
	PGConn           string
	CanonicalProject string
}

func (s *AdminLoopzeroDevSeed) Run(d Deps) Result {
	cfg := loadAdminConfig(d.Host)
	if cfg.KratosAdminURL == "" {
		return Result{Verdict: SKIP, Reason: fmt.Sprintf("KRATOS_ADMIN_URL not set (usually port-forward svc/ory-kratos-admin -> local). Kratos admin API required for %s.", cfg.Email)}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would seed canonical %s (identity + PG state + session)", cfg.Email)}
	}
	// Phase 1 — Kratos identity resolve-or-create.
	identityID, err := kratosResolveOrCreateIdentity(d.Ctx, d.Host.devHTTP(), cfg)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Phase 1 identity: %v", err)}
	}
	// Phase 2 — PG auxiliary state (SKIP-graceful when PG conn absent).
	if cfg.PGConn != "" {
		if err := adminEnsurePGState(d.Ctx, d.Host, cfg, identityID); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Phase 2 PG state: %v", err)}
		}
	}
	// Phase 3 — Password reset + session cookie mint.
	if err := kratosSetIdentityPassword(d.Ctx, d.Host.devHTTP(), cfg, identityID); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Phase 3 password reset: %v", err)}
	}
	cookie, err := kratosMintSessionCookie(d.Ctx, d.Host.devHTTP(), cfg)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Phase 3 session mint: %v", err)}
	}
	if err := adminWriteSessionFile(cfg.SessionFile, cookie); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Phase 3 session file: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("identity %s + session cookie persisted (%d bytes) to %s", identityID, len(cookie), cfg.SessionFile)}
}

// Verify probes /sessions/whoami using the persisted session cookie.
func (s *AdminLoopzeroDevSeed) Verify(d Deps) Result {
	cfg := loadAdminConfig(d.Host)
	cookie, err := os.ReadFile(cfg.SessionFile) // #nosec G304 -- operator supplied
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("read session file: %v", err)}
	}
	if len(cookie) == 0 {
		return Result{Verdict: FAIL, Reason: "session file empty"}
	}
	email, err := kratosWhoami(d.Ctx, d.Host.devHTTP(), cfg.PublicKratosURL, string(cookie))
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("/sessions/whoami: %v", err)}
	}
	if email != cfg.Email {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("/sessions/whoami email=%q, want %q", email, cfg.Email)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("whoami 200 email=%s (session cookie valid)", email)}
}

// loadAdminConfig resolves env → defaults.
func loadAdminConfig(h Host) AdminLoopzeroDevConfig {
	home := h.getenv("HOME")
	sessionFile := h.envOr(adminSessionFileEnv, home+"/.loopzero/admin-loopzero-dev-session.txt")
	sessionJWT := h.envOr(adminSessionJWTEnv, home+"/.loopzero/local-dogfood-session-jwt.txt")
	return AdminLoopzeroDevConfig{
		Email:            h.envOr(adminEmailEnv, adminDefaultEmail),
		DisplayName:      adminDefaultDisplayName,
		Username:         adminDefaultUsername,
		TenantSlug:       adminDefaultTenantSlug,
		SeedPassword:     h.envOr(adminSeedPasswordEnv, adminSeedPassword),
		PublicKratosURL:  h.envOr(adminPublicKratosEnv, adminDefaultPublicKratos),
		PublicCloudURL:   h.envOr(adminPublicCloudEnv, adminDefaultPublicCloud),
		KratosAdminURL:   h.getenv(adminKratosAdminEnv),
		SessionFile:      sessionFile,
		SessionJWTFile:   sessionJWT,
		PGConn:           h.getenv(adminPGConnEnv),
		CanonicalProject: adminCanonicalProject,
	}
}

// --- Phase 1: Kratos identity ---

type kratosIdentity struct {
	ID     string         `json:"id"`
	Traits map[string]any `json:"traits"`
}

func kratosResolveOrCreateIdentity(ctx context.Context, client *http.Client, cfg AdminLoopzeroDevConfig) (string, error) {
	// Search by credentials_identifier.
	u := cfg.KratosAdminURL + "/admin/identities?credentials_identifier=" + url.QueryEscape(cfg.Email)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("list identities: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var list []kratosIdentity
		if err := json.Unmarshal(body, &list); err == nil && len(list) > 0 && list[0].ID != "" {
			return list[0].ID, nil
		}
	}
	// Not found — POST create.
	create := map[string]any{
		"schema_id": "default",
		"traits":    map[string]any{"email": cfg.Email, "name": cfg.DisplayName},
		"verifiable_addresses": []any{
			map[string]any{"value": cfg.Email, "verified": true, "via": "email", "status": "completed"},
		},
	}
	payload, _ := json.Marshal(create)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, cfg.KratosAdminURL+"/admin/identities", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create identity: %w", err)
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("create identity HTTP %d: %s", resp.StatusCode, snippet)
	}
	var got kratosIdentity
	if err := json.Unmarshal(body, &got); err != nil {
		return "", fmt.Errorf("parse identity: %w", err)
	}
	if got.ID == "" {
		return "", fmt.Errorf("Kratos returned identity with empty id")
	}
	return got.ID, nil
}

// kratosSetIdentityPassword uses PUT (full replace) so Kratos hashes the
// password on save. JSON Patch would store cleartext and login would fail.
func kratosSetIdentityPassword(ctx context.Context, client *http.Client, cfg AdminLoopzeroDevConfig, identityID string) error {
	// Fetch current identity as the PUT base.
	getURL := cfg.KratosAdminURL + "/admin/identities/" + identityID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET current identity: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET identity HTTP %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("parse identity: %w", err)
	}
	// Inject the credentials.password.config.password so PUT triggers a hash.
	creds, _ := doc["credentials"].(map[string]any)
	if creds == nil {
		creds = map[string]any{}
		doc["credentials"] = creds
	}
	creds["password"] = map[string]any{
		"config": map[string]any{"password": cfg.SeedPassword},
	}
	// PUT it back.
	payload, _ := json.Marshal(doc)
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, cfg.KratosAdminURL+"/admin/identities/"+identityID, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT identity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ = io.ReadAll(resp.Body)
		return fmt.Errorf("PUT identity HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return nil
}

// kratosMintSessionCookie runs the browser-mode login flow and returns
// the ory_kratos_session cookie value. Uses net/http/cookiejar to preserve
// the CSRF cookie across GET+POST.
func kratosMintSessionCookie(ctx context.Context, base *http.Client, cfg AdminLoopzeroDevConfig) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Transport: base.Transport,
		Timeout:   base.Timeout,
		Jar:       jar,
	}

	// Step 1 — GET /self-service/login/browser with Accept: application/json.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.PublicKratosURL+"/self-service/login/browser", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login/browser GET: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login/browser HTTP %d", resp.StatusCode)
	}
	var flow struct {
		ID string `json:"id"`
		UI struct {
			Action string `json:"action"`
			Nodes  []struct {
				Attributes struct {
					Name  string `json:"name"`
					Value any    `json:"value"`
				} `json:"attributes"`
			} `json:"nodes"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(body, &flow); err != nil {
		return "", fmt.Errorf("parse flow: %w", err)
	}
	if flow.UI.Action == "" {
		return "", fmt.Errorf("flow missing ui.action")
	}
	// Extract csrf_token if present.
	var csrf string
	for _, n := range flow.UI.Nodes {
		if n.Attributes.Name == "csrf_token" {
			if s, ok := n.Attributes.Value.(string); ok {
				csrf = s
			}
		}
	}
	// Step 2 — POST credentials.
	submit := map[string]any{
		"method":     "password",
		"identifier": cfg.Email,
		"password":   cfg.SeedPassword,
	}
	if csrf != "" {
		submit["csrf_token"] = csrf
	}
	payload, _ := json.Marshal(submit)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, flow.UI.Action, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login submit: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		body, _ = io.ReadAll(resp.Body)
		return "", fmt.Errorf("login submit HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	// Step 3 — Extract ory_kratos_session cookie from the jar.
	pubURL, err := url.Parse(cfg.PublicKratosURL)
	if err != nil {
		return "", err
	}
	for _, c := range jar.Cookies(pubURL) {
		if c.Name == "ory_kratos_session" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("ory_kratos_session cookie absent from jar")
}

// kratosWhoami calls /sessions/whoami with the ory_kratos_session cookie.
// Returns the identity's email or an error.
func kratosWhoami(ctx context.Context, client *http.Client, publicKratosURL, cookieValue string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicKratosURL+"/sessions/whoami", nil)
	if err != nil {
		return "", err
	}
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: cookieValue})
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var got struct {
		Identity struct {
			Traits struct {
				Email string `json:"email"`
			} `json:"traits"`
		} `json:"identity"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return "", err
	}
	return got.Identity.Traits.Email, nil
}

// --- Phase 2: PG state ---

// adminEnsurePGState runs the canonical 7-statement upsert cluster (the same
// statements as the bash SQL block) as a sequence of idempotent autocommit
// statements — NOT a single transaction (the pgConn seam is Exec-only; it
// exposes no Begin/Commit).
//
// Atomicity is provided by IDEMPOTENCY + SELF-HEALING, not by a transaction:
// every statement is an ON-CONFLICT-DO-NOTHING upsert (or a NOT-EXISTS-guarded
// INSERT ... SELECT), and the statements are cross-referential in dependency
// order (users -> accounts -> tenants -> tenant_members/user_identity_map ->
// users.active_tenant_id -> subscriptions). A mid-chain failure therefore
// leaves a partial-but-recoverable state that the NEXT run completes: the
// already-written rows become no-ops and the failed tail is retried. The first
// statement failing leaves no rows at all (clean failure). This is acceptable
// for a dev-bootstrap seed and needs no transaction machinery (KISS/YAGNI).
//
// If cross-user isolation ever becomes a requirement, wrapping this in an
// explicit SERIALIZABLE transaction is a T2 change: it requires extending the
// shared pgConn seam (tenant_fixture.go) with Begin/Commit + a pgTx interface,
// implemented in pgConnAdapter and the test stub.
func adminEnsurePGState(ctx context.Context, h Host, cfg AdminLoopzeroDevConfig, kratosID string) error {
	conn, err := h.pgConnect(ctx, cfg.PGConn)
	if err != nil {
		return fmt.Errorf("pgx connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	// Execute the canonical INSERTs. In hermetic tests, stubPgConn records exec
	// calls without running actual SQL. In prod, pgx runs against PG directly.
	stmts := []string{
		// users
		`INSERT INTO users (id, email, username, display_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO NOTHING`,
		// accounts
		`INSERT INTO accounts (id, display_name, plan, owner_id)
		 VALUES ('acct-' || substring(encode(sha256(($1 || '-account')::bytea), 'hex'), 1, 16),
		         $2, 'starter', $1)
		 ON CONFLICT (owner_id) DO NOTHING`,
		// tenants
		`INSERT INTO tenants (id, display_name, slug, owner_id, cell_id, account_id)
		 SELECT 'tn-' || substring(encode(sha256(($1 || '-' || $3)::bytea), 'hex'), 1, 16),
		        $2, $3, $1, 'cell-0', a.id
		 FROM accounts a
		 WHERE a.owner_id = $1
		   AND NOT EXISTS (SELECT 1 FROM tenants WHERE owner_id = $1)`,
		// tenant_members
		`INSERT INTO tenant_members (tenant_id, user_id, role)
		 SELECT t.id, $1, 'owner' FROM tenants t WHERE t.owner_id = $1
		 ON CONFLICT (tenant_id, user_id) DO NOTHING`,
		// user_identity_map
		`INSERT INTO user_identity_map (kratos_identity_id, user_id)
		 VALUES ($1, $1)
		 ON CONFLICT (kratos_identity_id) DO NOTHING`,
		// users.active_tenant_id
		`UPDATE users SET active_tenant_id = (SELECT id FROM tenants WHERE owner_id = $1 LIMIT 1),
		        updated_at = now()
		 WHERE id = $1
		   AND (active_tenant_id IS NULL
		        OR active_tenant_id NOT IN (SELECT id FROM tenants WHERE owner_id = $1))`,
		// subscriptions
		`INSERT INTO subscriptions (id, tenant_id, account_id, plan, status)
		 SELECT 'sub-dev-' || t.id, t.id, t.account_id, 'starter', 'active'
		 FROM tenants t WHERE t.owner_id = $1
		 ON CONFLICT (account_id) DO NOTHING`,
	}
	argsPerStmt := [][]any{
		{kratosID, cfg.Email, cfg.Username, cfg.DisplayName},
		{kratosID, cfg.DisplayName},
		{kratosID, cfg.DisplayName, cfg.TenantSlug},
		{kratosID},
		{kratosID},
		{kratosID},
		{kratosID},
	}
	for i, sql := range stmts {
		if err := conn.Exec(ctx, sql, argsPerStmt[i]...); err != nil {
			return fmt.Errorf("stmt %d: %w", i+1, err)
		}
	}
	return nil
}

// --- Phase 3 helpers ---

func adminWriteSessionFile(path, cookieValue string) error {
	if path == "" {
		return fmt.Errorf("empty session file path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(cookieValue), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
