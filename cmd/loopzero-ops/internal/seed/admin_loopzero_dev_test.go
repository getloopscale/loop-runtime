// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// kratosAdminStub is a minimal Kratos admin+public API stub for hermetic tests.
type kratosAdminStub struct {
	mu           sync.Mutex
	existingID   string
	created      bool
	putSetPw     bool
	loginFlowID  string
	sessionValue string
}

func newKratosStub(t *testing.T, existingIdentityID string) (*kratosAdminStub, *httptest.Server) {
	t.Helper()
	stub := &kratosAdminStub{existingID: existingIdentityID, sessionValue: "session-cookie-abc123"}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/admin/identities") && r.URL.RawQuery != "" && r.Method == http.MethodGet:
			// List by credentials_identifier
			if stub.existingID != "" {
				_ = json.NewEncoder(w).Encode([]kratosIdentity{{ID: stub.existingID}})
			} else {
				_, _ = w.Write([]byte("[]"))
			}
		case r.URL.Path == "/admin/identities" && r.Method == http.MethodPost:
			stub.created = true
			stub.existingID = "id-newly-created"
			_ = json.NewEncoder(w).Encode(kratosIdentity{ID: stub.existingID})
		case strings.HasPrefix(r.URL.Path, "/admin/identities/") && r.Method == http.MethodGet:
			// Fetch identity for PUT base
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     stub.existingID,
				"traits": map[string]any{"email": "admin@loopzero.example"},
			})
		case strings.HasPrefix(r.URL.Path, "/admin/identities/") && r.Method == http.MethodPut:
			stub.putSetPw = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(kratosIdentity{ID: stub.existingID})
		case r.URL.Path == "/self-service/login/browser" && r.Method == http.MethodGet:
			// Emit a flow with action URL pointing back to this server.
			stub.loginFlowID = "flow-abc"
			resp := map[string]any{
				"id": stub.loginFlowID,
				"ui": map[string]any{
					"action": srv.URL + "/self-service/login",
					"nodes":  []any{},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/self-service/login" && r.Method == http.MethodPost:
			// Set the ory_kratos_session cookie in the response.
			http.SetCookie(w, &http.Cookie{Name: "ory_kratos_session", Value: stub.sessionValue, Path: "/"})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"session":{"id":"s1"}}`))
		case r.URL.Path == "/sessions/whoami" && r.Method == http.MethodGet:
			c, err := r.Cookie("ory_kratos_session")
			if err != nil || c.Value != stub.sessionValue {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"identity": map[string]any{
					"traits": map[string]string{"email": "admin@loopzero.example"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return stub, srv
}

func TestLoadAdminConfig_UsesDefaults(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{})
	cfg := loadAdminConfig(host)
	if cfg.Email != adminDefaultEmail {
		t.Errorf("Email = %q, want %q", cfg.Email, adminDefaultEmail)
	}
	if cfg.SeedPassword != adminSeedPassword {
		t.Errorf("SeedPassword default not applied")
	}
	if cfg.PublicKratosURL != adminDefaultPublicKratos {
		t.Errorf("PublicKratosURL default = %q", cfg.PublicKratosURL)
	}
}

func TestLoadAdminConfig_EnvOverride(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		adminEmailEnv:        "other@loopzero.example",
		adminSeedPasswordEnv: "custom-pw",
		adminPublicKratosEnv: "https://other-kratos",
		adminKratosAdminEnv:  "http://127.0.0.1:54434",
	})
	cfg := loadAdminConfig(host)
	if cfg.Email != "other@loopzero.example" {
		t.Errorf("Email = %q", cfg.Email)
	}
	if cfg.SeedPassword != "custom-pw" {
		t.Errorf("SeedPassword override not applied")
	}
	if cfg.KratosAdminURL != "http://127.0.0.1:54434" {
		t.Errorf("KratosAdminURL not applied")
	}
}

func TestAdminLoopzeroDev_SkipWhenKratosAdminUnset(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{})
	got := NewAdminLoopzeroDevSeed().Run(Deps{Host: host, Ctx: context.Background()})
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestAdminLoopzeroDev_DryRunReports(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{adminKratosAdminEnv: "http://x"})
	got := NewAdminLoopzeroDevSeed().Run(Deps{Host: host, Ctx: context.Background(), DryRun: true})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestKratosResolveOrCreateIdentity_ReusesExisting(t *testing.T) {
	stub, server := newKratosStub(t, "id-existing")
	defer server.Close()
	cfg := AdminLoopzeroDevConfig{Email: "admin@loopzero.example", KratosAdminURL: server.URL}
	id, err := kratosResolveOrCreateIdentity(context.Background(), http.DefaultClient, cfg)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "id-existing" {
		t.Errorf("id = %q, want id-existing", id)
	}
	if stub.created {
		t.Error("stub reports created=true when identity was existing")
	}
}

func TestKratosResolveOrCreateIdentity_CreatesWhenAbsent(t *testing.T) {
	stub, server := newKratosStub(t, "")
	defer server.Close()
	cfg := AdminLoopzeroDevConfig{
		Email:          "admin@loopzero.example",
		DisplayName:    "LoopZero Admin",
		KratosAdminURL: server.URL,
	}
	id, err := kratosResolveOrCreateIdentity(context.Background(), http.DefaultClient, cfg)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id == "" {
		t.Fatal("empty id after create")
	}
	if !stub.created {
		t.Error("stub reports created=false when identity should have been POSTed")
	}
}

func TestKratosSetIdentityPassword_PUTHashes(t *testing.T) {
	stub, server := newKratosStub(t, "id-x")
	defer server.Close()
	cfg := AdminLoopzeroDevConfig{
		SeedPassword:   "pw",
		KratosAdminURL: server.URL,
	}
	if err := kratosSetIdentityPassword(context.Background(), http.DefaultClient, cfg, "id-x"); err != nil {
		t.Fatalf("password reset: %v", err)
	}
	if !stub.putSetPw {
		t.Error("PUT was not called (Kratos v26 requires PUT — JSON Patch stores cleartext)")
	}
}

func TestKratosMintSessionCookie_ReturnsCookie(t *testing.T) {
	_, server := newKratosStub(t, "id-x")
	defer server.Close()
	cfg := AdminLoopzeroDevConfig{
		Email:           "admin@loopzero.example",
		SeedPassword:    "pw",
		PublicKratosURL: server.URL,
		KratosAdminURL:  server.URL,
	}
	cookie, err := kratosMintSessionCookie(context.Background(), http.DefaultClient, cfg)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if cookie != "session-cookie-abc123" {
		t.Errorf("cookie = %q, want session-cookie-abc123", cookie)
	}
}

func TestAdminWriteSessionFile_Mode0600(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "sub2", "session.txt")
	if err := adminWriteSessionFile(target, "cookie-value"); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
	body, _ := os.ReadFile(target)
	if string(body) != "cookie-value" {
		t.Errorf("content = %q", body)
	}
}

// engineBootstrapStub is the engine's POST /internal/bootstrap, recording what
// the seed sent and answering status with body.
func engineBootstrapStub(t *testing.T, status int, body string) (*httptest.Server, *map[string]string, *string) {
	t.Helper()
	sent := map[string]string{}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/bootstrap" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &sent, &auth
}

func TestAdminProvisionTenantAsksTheEngineToProvisionTheIdentity(t *testing.T) {
	srv, sent, auth := engineBootstrapStub(t, http.StatusOK, `{"status":"ok","user_id":"kratos-id-1","tenant_id":"tn-admin","created":true}`)
	cfg := AdminLoopzeroDevConfig{
		Email: "admin@loopzero.example", Username: "admin", DisplayName: "LoopZero Admin", TenantSlug: "admin",
		APIURL: srv.URL, BootstrapSecret: "boot-secret",
	}

	tenantID, err := adminProvisionTenant(context.Background(), srv.Client(), cfg, "kratos-id-1")

	if err != nil || tenantID != "tn-admin" {
		t.Fatalf("adminProvisionTenant = %q, %v", tenantID, err)
	}
	if *auth != "Bearer boot-secret" {
		t.Errorf("Authorization = %q, want the bootstrap secret", *auth)
	}
	want := map[string]string{"admin_user_id": "kratos-id-1", "admin_login": "admin", "admin_email": "admin@loopzero.example", "tenant_slug": "admin", "plan": "starter"}
	for k, v := range want {
		if (*sent)[k] != v {
			t.Errorf("request %s = %q, want %q", k, (*sent)[k], v)
		}
	}
	if _, cell := (*sent)["cell_id"]; cell {
		t.Error("the seed must not choose a cell; placement is the engine's")
	}
}

func TestAdminProvisionTenantSurfacesTheEngineRefusal(t *testing.T) {
	srv, _, _ := engineBootstrapStub(t, http.StatusConflict, `{"code":"TENANT_SLUG_TAKEN"}`)
	cfg := AdminLoopzeroDevConfig{TenantSlug: "admin", APIURL: srv.URL, BootstrapSecret: "boot-secret"}

	_, err := adminProvisionTenant(context.Background(), srv.Client(), cfg, "kratos-id-1")

	if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "TENANT_SLUG_TAKEN") {
		t.Fatalf("error = %v, want the HTTP status and the engine's code", err)
	}
}

func TestAdminProvisionTenantRefusesToRunWithoutTheBootstrapSecret(t *testing.T) {
	srv, _, auth := engineBootstrapStub(t, http.StatusOK, `{"tenant_id":"tn-admin"}`)
	cfg := AdminLoopzeroDevConfig{TenantSlug: "admin", APIURL: srv.URL}

	_, err := adminProvisionTenant(context.Background(), srv.Client(), cfg, "kratos-id-1")

	if err == nil || *auth != "" {
		t.Fatalf("error = %v, request sent with auth %q; want a refusal before any request", err, *auth)
	}
}

func TestIdentityHandleIsTheSanitizedEmailLocalPart(t *testing.T) {
	for email, want := range map[string]string{
		"admin@loopzero.example":            "admin",
		"paulohenrique.java@gmail.com": "paulohenrique-java",
		"Some_User+qa@example.test":    "some-user-qa",
	} {
		if got := identityHandle(email); got != want {
			t.Errorf("identityHandle(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestAdminLoopzeroDev_Verify_PassWhenCookieValid(t *testing.T) {
	var host Host
	_, server := newKratosStub(t, "id-x")
	defer server.Close()
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "session.txt")
	_ = os.WriteFile(sessionFile, []byte("session-cookie-abc123"), 0o600)
	host.Getenv = envOf(map[string]string{
		adminSessionFileEnv:  sessionFile,
		adminPublicKratosEnv: server.URL,
	})
	got := NewAdminLoopzeroDevSeed().Verify(Deps{Host: host, Ctx: context.Background()})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestAdminLoopzeroDev_Verify_FailWhenSessionFileEmpty(t *testing.T) {
	var host Host
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "session.txt")
	_ = os.WriteFile(sessionFile, []byte(""), 0o600)
	host.Getenv = envOf(map[string]string{adminSessionFileEnv: sessionFile})
	got := NewAdminLoopzeroDevSeed().Verify(Deps{Host: host, Ctx: context.Background()})
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestAdminLoopzeroDev_Name(t *testing.T) {
	if got := NewAdminLoopzeroDevSeed().Name(); got != "admin-loopzero-dev" {
		t.Errorf("Name = %q", got)
	}
}

func TestAdminLoopzeroDevProvisionsTheTenantThroughTheEngine(t *testing.T) {
	_, kratos := newKratosStub(t, "kratos-id-1")
	defer kratos.Close()
	engine, sent, _ := engineBootstrapStub(t, http.StatusOK, `{"tenant_id":"tn-admin"}`)
	var host Host
	host.Getenv = envOf(map[string]string{
		adminKratosAdminEnv:  kratos.URL,
		adminPublicKratosEnv: kratos.URL,
		adminSessionFileEnv:  filepath.Join(t.TempDir(), "session.txt"),
		adminAPIURLEnv:       engine.URL,
		adminBootstrapEnv:    "boot-secret",
	})

	got := NewAdminLoopzeroDevSeed().Run(Deps{Host: host, Ctx: context.Background()})

	if got.Verdict != PASS || !strings.Contains(got.Reason, "tenant tn-admin") {
		t.Fatalf("Run = %s (%s), want PASS naming the engine's tenant", got.Verdict, got.Reason)
	}
	if (*sent)["admin_user_id"] != "kratos-id-1" {
		t.Errorf("engine provisioned %q, want the Kratos identity", (*sent)["admin_user_id"])
	}
}
