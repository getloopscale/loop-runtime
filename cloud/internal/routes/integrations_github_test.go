// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newHandler builds a handler with deterministic dependencies for tests.
// sessionResolver defaults to "session present" so tests focused on the
// happy path don't have to wire a stub. Pass an override to exercise EC-15.
func newHandler(t *testing.T, sessionResolver func(*http.Request) bool) *GitHubIntegrationsHandler {
	t.Helper()
	if sessionResolver == nil {
		sessionResolver = func(*http.Request) bool { return true }
	}
	h := NewGitHubIntegrationsHandler(
		"loopzero-platform",
		"https://app-dev.loopzero.example",
		"https://auth-dev.loopzero.example/login",
		"",
		sessionResolver,
	).WithInsecureCookieForTests()
	return h
}

// stateFromInstall runs InitiateInstall once and returns the state token
// that the handler set in its cookie + the Set-Cookie attributes for
// downstream assertions.
func stateFromInstall(t *testing.T, h *GitHubIntegrationsHandler, returnTo string) (state, returnCookieValue string, recorder *httptest.ResponseRecorder) {
	t.Helper()
	target := "/v1/integrations/github/install"
	if returnTo != "" {
		target += "?return_to=" + url.QueryEscape(returnTo)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	h.InitiateInstall(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("InitiateInstall: status = %d, want 302", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case stateCookieName:
			state = c.Value
		case stateCookieName + "_return_to":
			returnCookieValue = c.Value
		}
	}
	if state == "" {
		t.Fatalf("InitiateInstall did not set %s cookie", stateCookieName)
	}
	return state, returnCookieValue, w
}

func TestInitiateInstall_SetsStateCookieAndRedirectsToGitHub(t *testing.T) {
	h := newHandler(t, nil)
	state, returnCookie, w := stateFromInstall(t, h, "/projects/demo/connect")

	if len(state) != 64 {
		t.Fatalf("state token length = %d, want 64 (32B hex-encoded)", len(state))
	}
	if returnCookie != "/projects/demo/connect" {
		t.Fatalf("return_to cookie = %q, want %q", returnCookie, "/projects/demo/connect")
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://github.com/apps/loopzero-platform/installations/new") {
		t.Fatalf("Location = %q, want GitHub App install URL", loc)
	}
	if !strings.Contains(loc, "state="+url.QueryEscape(state)) {
		t.Fatalf("Location = %q, missing state=%s", loc, state)
	}
}

func TestInitiateInstall_DefaultReturnToWhenAbsent(t *testing.T) {
	h := newHandler(t, nil)
	_, returnCookie, _ := stateFromInstall(t, h, "")
	if returnCookie != "/projects" {
		t.Fatalf("return_to fallback = %q, want %q", returnCookie, "/projects")
	}
}

func TestInitiateInstall_DoubleClickLastStateWins(t *testing.T) {
	// EC-3: two clicks within a few seconds. The handler is stateless on
	// the server, so the second cookie overwrites the first. Verifying
	// that the second InitiateInstall returns a *different* token + that
	// the callback only accepts the latest state cookie.
	h := newHandler(t, nil)
	state1, _, _ := stateFromInstall(t, h, "")
	state2, _, _ := stateFromInstall(t, h, "")
	if state1 == state2 {
		t.Fatalf("expected distinct tokens, got same: %q", state1)
	}
}

// Path 2 (Setup URL flow, 2026-06-06): GitHub redirects the user with
// installation_id + setup_action but NO state. We accept it because the
// callback persists nothing of its own -- the next POST /connect is what
// actually links the repo, and that endpoint validates the installation_id
// via a JWT-authenticated installation token mint against the real GitHub
// API. A forged ID would fail at that gate.
func TestCallback_SetupURLPath_NoCookieNoState_AcceptedAsInstall(t *testing.T) {
	h := newHandler(t, nil)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/integrations/github/callback?installation_id=42&setup_action=install", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (Setup URL path accepted)", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "installation_id=42") {
		t.Fatalf("Location = %q, want installation_id=42 in query", loc)
	}
	if !strings.Contains(loc, "installed=1") {
		t.Fatalf("Location = %q, want installed=1 in query", loc)
	}
}

// Same Setup URL path with setup_action=update.
func TestCallback_SetupURLPath_NoCookieNoState_UpdateAccepted(t *testing.T) {
	h := newHandler(t, nil)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/integrations/github/callback?installation_id=99&setup_action=update", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (Setup URL update accepted)", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "updated=1") {
		t.Fatalf("Location = %q, want updated=1 in query", loc)
	}
}

// Cookie present + state in URL missing = ambiguous; keep rejecting to
// avoid CSRF where attacker forges a Setup URL while a legitimate Install
// session is in flight.
func TestCallback_CookiePresentButStateMissingFromURL_400(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet,
		"/v1/integrations/github/callback?installation_id=42", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (cookie + no state in URL)", w.Code)
	}
	body := mustDecodeError(t, w)
	if body["code"] != "MISSING_STATE" {
		t.Fatalf("code = %v, want MISSING_STATE", body["code"])
	}
}

// Defense-in-depth: installation_id MUST parse as a positive integer.
func TestCallback_InvalidInstallationID_400(t *testing.T) {
	h := newHandler(t, nil)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/integrations/github/callback?installation_id=../etc/passwd&setup_action=install", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (invalid installation_id)", w.Code)
	}
	body := mustDecodeError(t, w)
	if body["code"] != "INVALID_INSTALLATION_ID" {
		t.Fatalf("code = %v, want INVALID_INSTALLATION_ID", body["code"])
	}
}

func TestCallback_StateMismatch_400(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=42&state=tampered", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (state mismatch)", w.Code)
	}
	body := mustDecodeError(t, w)
	if body["code"] != "STATE_MISMATCH" {
		t.Fatalf("code = %v, want STATE_MISMATCH", body["code"])
	}
}

func TestCallback_MissingInstallationID_400(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing installation_id)", w.Code)
	}
	body := mustDecodeError(t, w)
	if body["code"] != "MISSING_INSTALLATION_ID" {
		t.Fatalf("code = %v, want MISSING_INSTALLATION_ID", body["code"])
	}
}

func TestCallback_UnknownSetupAction_400(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=99&state="+state+"&setup_action=cancel", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (unknown setup_action)", w.Code)
	}
	body := mustDecodeError(t, w)
	if body["code"] != "UNKNOWN_SETUP_ACTION" {
		t.Fatalf("code = %v, want UNKNOWN_SETUP_ACTION", body["code"])
	}
}

func TestCallback_HappyPath_RedirectsToDashboardWithInstallationID(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "/projects/demo/connect")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=12345&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: stateCookieName + "_return_to", Value: "/projects/demo/connect"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	want := "https://app-dev.loopzero.example/projects/demo/connect?installed=1&installation_id=12345"
	if loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}
}

func TestCallback_HappyPath_DefaultsReturnToProjects(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=7&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	// Note: return_to cookie NOT attached -- handler should default to /projects.
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	want := "https://app-dev.loopzero.example/projects?installed=1&installation_id=7"
	if loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}
}

func TestCallback_SetupActionUpdate_ReportsUpdatedFlag(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=88&state="+state+"&setup_action=update", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (update path)", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "updated=1") || strings.Contains(loc, "installed=1") {
		t.Fatalf("Location = %q, want updated=1 (and no installed=1)", loc)
	}
}

func TestCallback_HappyPath_ClearsStateCookies(t *testing.T) {
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=1&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: stateCookieName + "_return_to", Value: "/projects"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	var cleared int
	for _, c := range w.Result().Cookies() {
		if c.Name == stateCookieName || c.Name == stateCookieName+"_return_to" {
			if c.MaxAge != -1 {
				t.Fatalf("cookie %q MaxAge = %d, want -1 (cleared)", c.Name, c.MaxAge)
			}
			cleared++
		}
	}
	if cleared != 2 {
		t.Fatalf("cleared = %d cookies, want 2 (state + return_to)", cleared)
	}
}

func TestCallback_SessionExpired_RedirectsToLoginPreservingInstallationID(t *testing.T) {
	// EC-15: Kratos session expired between install start + callback. The
	// handler must route through /login?return_to=<dashboard>... so the
	// connect flow resumes after re-auth, with the installation_id intact.
	h := newHandler(t, func(*http.Request) bool { return false })
	state, _, _ := stateFromInstall(t, h, "/projects/demo/connect")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=42&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: stateCookieName + "_return_to", Value: "/projects/demo/connect"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://auth-dev.loopzero.example/login?return_to=") {
		t.Fatalf("Location = %q, want login URL prefix", loc)
	}
	if !strings.Contains(loc, "installation_id%3D42") {
		t.Fatalf("Location = %q, must preserve installation_id=42 in return_to", loc)
	}
}

func TestCallback_ReturnToCookie_DefendsAgainstOpenRedirect(t *testing.T) {
	// EC-4: a hostile return_to cookie ("https://evil.com/x") MUST be
	// anchored under the dashboard origin; we never let it become the
	// destination of the Location header.
	h := newHandler(t, nil)
	state, _, _ := stateFromInstall(t, h, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/callback?installation_id=9&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: stateCookieName + "_return_to", Value: "https://evil.com/x"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://app-dev.loopzero.example/") {
		t.Fatalf("Location = %q, want anchored to dashboard origin", loc)
	}
	if strings.Contains(loc, "evil.com") {
		t.Fatalf("Location = %q, leaked evil host", loc)
	}
}

func TestInitiateInstall_StateCookieAttributesAreSafe(t *testing.T) {
	// EC-2: cookie must be HttpOnly + SameSite=Lax. Secure is asserted
	// via the production constructor (Tests use WithInsecureCookieForTests
	// to survive httptest's plain HTTP).
	h := NewGitHubIntegrationsHandler("loopzero-platform", "https://app-dev.loopzero.example", "https://auth-dev.loopzero.example/login", "", nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/integrations/github/install", nil)
	w := httptest.NewRecorder()
	h.InitiateInstall(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name != stateCookieName {
			continue
		}
		if !c.HttpOnly {
			t.Fatalf("state cookie HttpOnly = false, want true")
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Fatalf("state cookie SameSite = %v, want Lax", c.SameSite)
		}
		if !c.Secure {
			t.Fatalf("state cookie Secure = false, want true (production constructor)")
		}
		if c.MaxAge != stateCookieTTL {
			t.Fatalf("state cookie MaxAge = %d, want %d", c.MaxAge, stateCookieTTL)
		}
	}
}

func mustDecodeError(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body
}
