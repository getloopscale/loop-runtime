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
	"strings"
)

// AdminLoopzeroDevSeed is the Go port of
// infra/scripts/local/seed-admin-loopzero-dev.sh.
//
// Canonical Kratos session seed for the dev/dev-public canonical user
// admin@loopzero.example. End-to-end 4-phase chain:
//
//  1. Ensure Kratos identity via admin API (create if absent).
//  2. Provision the identity's tenant through the engine — POST
//     /internal/bootstrap, authenticated with the bootstrap secret. The
//     engine places the tenant in a cell and writes user, Account, tenant,
//     membership and subscription in its placement transaction, so ids,
//     placement and the tenancy schema have one owner. See
//     adminProvisionTenant.
//  3. Reset identity password via PUT (Kratos v26 quirk: PUT hashes,
//     JSON Patch stores cleartext) + mint session via browser login
//     flow + persist session cookie at mode 0600.
//  4. Optional --verify probes /sessions/whoami + JWT exchange.
//
// SKIP-graceful when the Kratos admin endpoint is not configured, and
// phase 2 is skipped (and said so) when the engine API is not — hermetic
// tests use httptest for both.
type AdminLoopzeroDevSeed struct{}

func NewAdminLoopzeroDevSeed() *AdminLoopzeroDevSeed { return &AdminLoopzeroDevSeed{} }
func (s *AdminLoopzeroDevSeed) Name() string        { return "admin-loopzero-dev" }

// Defaults mirror bash constants line-by-line.
const (
	adminDefaultEmail        = "admin@loopzero.example"
	adminDefaultDisplayName  = "LoopZero Admin"
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
	adminAPIURLEnv        = "LOOPZERO_API_URL"          // engine API base, e.g. a port-forward to svc/loopzero-api
	adminBootstrapEnv     = "LOOPZERO_BOOTSTRAP_SECRET" // the engine's /internal/bootstrap secret
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
	APIURL           string
	BootstrapSecret  string
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
	// Phase 2 — tenant provisioning through the engine (skipped, and said so,
	// when the engine API is not configured).
	tenantNote := fmt.Sprintf("tenant provisioning skipped: %s not set", adminAPIURLEnv)
	if cfg.APIURL != "" {
		tenantID, err := adminProvisionTenant(d.Ctx, d.Host.devHTTP(), cfg, identityID)
		if err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Phase 2 tenant provisioning: %v", err)}
		}
		tenantNote = "tenant " + tenantID
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
	return Result{Verdict: PASS, Reason: fmt.Sprintf("identity %s (%s) + session cookie persisted (%d bytes) to %s", identityID, tenantNote, len(cookie), cfg.SessionFile)}
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
	email := h.envOr(adminEmailEnv, adminDefaultEmail)
	handle := identityHandle(email)
	return AdminLoopzeroDevConfig{
		Email:            email,
		DisplayName:      adminDefaultDisplayName,
		Username:         handle,
		TenantSlug:       handle,
		SeedPassword:     h.envOr(adminSeedPasswordEnv, adminSeedPassword),
		PublicKratosURL:  h.envOr(adminPublicKratosEnv, adminDefaultPublicKratos),
		PublicCloudURL:   h.envOr(adminPublicCloudEnv, adminDefaultPublicCloud),
		KratosAdminURL:   h.getenv(adminKratosAdminEnv),
		SessionFile:      sessionFile,
		SessionJWTFile:   sessionJWT,
		APIURL:           strings.TrimRight(h.getenv(adminAPIURLEnv), "/"),
		BootstrapSecret:  h.getenv(adminBootstrapEnv),
		CanonicalProject: adminCanonicalProject,
	}
}

// identityHandle derives the username and tenant slug of a seeded identity
// from its email's local part (admin@loopzero.example → "admin"), so two seeded
// identities never ask for the same unique username or slug.
func identityHandle(email string) string {
	local, _, _ := strings.Cut(strings.ToLower(email), "@")
	var b strings.Builder
	for _, r := range local {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
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

// --- Phase 2: tenant provisioning ---

// adminBootstrapResponse is the engine's POST /internal/bootstrap answer.
type adminBootstrapResponse struct {
	TenantID string `json:"tenant_id"`
}

// adminProvisionTenant asks the engine to provision the identity's tenant:
// POST /internal/bootstrap with the identity (users.id is the Kratos id),
// its handle as username and slug, and the Starter plan. The engine owns the
// id derivation, the cell placement and every tenancy write; this seed only
// names who is being provisioned. Idempotent — a re-run answers the same
// tenant. Returns the tenant id the engine chose.
func adminProvisionTenant(ctx context.Context, client *http.Client, cfg AdminLoopzeroDevConfig, kratosID string) (string, error) {
	if cfg.BootstrapSecret == "" {
		return "", fmt.Errorf("%s is required with %s (the engine refuses an unauthenticated bootstrap)", adminBootstrapEnv, adminAPIURLEnv)
	}
	body, err := json.Marshal(map[string]string{
		"admin_user_id":       kratosID,
		"admin_login":         cfg.Username,
		"admin_email":         cfg.Email,
		"admin_display_name":  cfg.DisplayName,
		"tenant_slug":         cfg.TenantSlug,
		"tenant_display_name": cfg.DisplayName,
		"plan":                "starter",
	})
	if err != nil {
		return "", fmt.Errorf("encode bootstrap request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIURL+"/internal/bootstrap", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.BootstrapSecret)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /internal/bootstrap: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read bootstrap response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("POST /internal/bootstrap: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out adminBootstrapResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode bootstrap response: %w", err)
	}
	if out.TenantID == "" {
		return "", fmt.Errorf("POST /internal/bootstrap answered no tenant_id: %s", strings.TrimSpace(string(raw)))
	}
	return out.TenantID, nil
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
