// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	cloudmetrics "github.com/loopzero/loopzero/cloud/internal/metrics"
)

// GitHubIntegrationsHandler implements the dashboard install handshake:
//
//	(1) GET  /v1/integrations/github/install
//	    Sets a CSRF state cookie + 302 redirects to the GitHub App install URL.
//	(2) GET  /v1/integrations/github/callback?installation_id=N&state=X&setup_action=...
//	    Validates state, distinguishes install vs update, redirects browser
//	    back to the dashboard with installation_id in the query.
//
// Plan J-09 T1.1 (2026-06-05). The handler does NOT persist the installation
// — dashboard's first POST /api/v1/projects/{name}/connect carries the
// installation_id through to the engine which calls SaveInstallation as a
// side-effect of LinkRepository. This keeps the callback free of engine RPC
// (no extra auth dance) while the runtime invariant holds: by the time any
// `auto_deploy` webhook fires, the installation is in the DB.
type GitHubIntegrationsHandler struct {
	appSlug        string // GitHub App slug, e.g. "loopzero-platform"
	dashboardURL   string // e.g. "https://app-dev.loopzero.example"
	loginURL       string // e.g. "https://app-dev.loopzero.example/login" — used on session expiry (EC-15)
	cookieDomain   string // explicit Domain= on the state cookie (EC-2 host-only)
	cookieSecure   bool   // false only in tests
	sessionResolver func(r *http.Request) bool
}

// NewGitHubIntegrationsHandler wires the handler. sessionResolver returns
// true if the inbound request carries a valid Kratos session — used to
// decide between callback completion and the EC-15 login-redirect path.
// Production wires this to Kratos /sessions/whoami; tests stub.
func NewGitHubIntegrationsHandler(appSlug, dashboardURL, loginURL, cookieDomain string, sessionResolver func(r *http.Request) bool) *GitHubIntegrationsHandler {
	return &GitHubIntegrationsHandler{
		appSlug:        appSlug,
		dashboardURL:   dashboardURL,
		loginURL:       loginURL,
		cookieDomain:   cookieDomain,
		cookieSecure:   true,
		sessionResolver: sessionResolver,
	}
}

// WithInsecureCookieForTests disables the Secure attribute so cookies survive
// httptest's plain HTTP. Production callers MUST NOT touch this.
func (h *GitHubIntegrationsHandler) WithInsecureCookieForTests() *GitHubIntegrationsHandler {
	h.cookieSecure = false
	return h
}

const (
	stateCookieName = "loopzero_gh_install_state"
	stateCookieTTL  = 600 // seconds (10 minute install window)
)

// InitiateInstall sets the state cookie + 302 redirects to GitHub.
// GET /v1/integrations/github/install?return_to=/projects/{name}/connect
func (h *GitHubIntegrationsHandler) InitiateInstall(w http.ResponseWriter, r *http.Request) {
	state, err := newStateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not start the install. Try again.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		Domain:   h.cookieDomain,
		MaxAge:   stateCookieTTL,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	// return_to is informational — we store it as a second cookie so the
	// callback knows where to land in the dashboard. Falls back to the
	// connect page index when absent.
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "/projects"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName + "_return_to",
		Value:    returnTo,
		Path:     "/",
		Domain:   h.cookieDomain,
		MaxAge:   stateCookieTTL,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	installURL := "https://github.com/apps/" + h.appSlug + "/installations/new?state=" + url.QueryEscape(state)
	http.Redirect(w, r, installURL, http.StatusFound)
}

// Callback handles the post-install redirect from GitHub.
//
// Two acceptance paths:
//
//  1. Cookie + state match (Install button -> Install URL -> Callback URL).
//     This is the primary path: the user clicks Install on the dashboard,
//     we set the CSRF state cookie, GitHub returns it on redirect, and we
//     verify exact match. Classic OAuth-style CSRF defense.
//
//  2. Cookie missing OR cookie expired BUT installation_id + recognised
//     setup_action present. This handles the Setup URL flow, where GitHub
//     redirects users with NO state parameter — the user installs the App
//     directly from github.com/apps/<slug> or from an account that already
//     trusts the App. The callback persists NOTHING on its own (T1.1
//     deliberately defers persistence to POST /connect), so an attacker
//     forging this path achieves nothing more than landing the user on
//     /projects?installed=1&installation_id=<X> — which still requires
//     the user to actively click "Connect" in the dashboard and POST a
//     valid {repo, installation_id} pair. The engine then validates the
//     installation_id against the GitHub API via a JWT-authenticated
//     installation token mint; a forged ID returns 401/404 from GitHub.
//     So Path 2 is safe to accept: it's an attractive-nuisance defence
//     boundary at most, and rejecting it makes the legitimate Setup URL
//     flow unusable.
func (h *GitHubIntegrationsHandler) Callback(w http.ResponseWriter, r *http.Request) {
	installationIDStr := r.URL.Query().Get("installation_id")
	if installationIDStr == "" {
		writeError(w, http.StatusBadRequest, "MISSING_INSTALLATION_ID",
			"Install did not complete. Please retry.")
		return
	}
	// Validate installation_id is a positive integer -- defends against
	// path-traversal / SQL-injection probes piggy-backing the query.
	if _, parseErr := strconv.ParseInt(installationIDStr, 10, 64); parseErr != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INSTALLATION_ID",
			"Install did not complete. Please retry.")
		return
	}

	// EC-1: distinguish setup_action.
	setupAction := r.URL.Query().Get("setup_action")
	resultQuery := "installed=1"
	switch setupAction {
	case "", "install":
		resultQuery = "installed=1"
	case "update":
		resultQuery = "updated=1"
	default:
		writeError(w, http.StatusBadRequest, "UNKNOWN_SETUP_ACTION",
			"Unrecognised install state. Please retry.")
		return
	}

	// Path 1: cookie + state match. Most defensive, no behaviour change.
	statePath := false
	stateCookie, cookieErr := r.Cookie(stateCookieName)
	queryState := r.URL.Query().Get("state")
	switch {
	case cookieErr == nil && stateCookie.Value != "" && queryState != "":
		if queryState != stateCookie.Value {
			writeError(w, http.StatusBadRequest, "STATE_MISMATCH",
				"Install session does not match. Please retry the GitHub App install.")
			return
		}
		statePath = true
	case cookieErr == nil && stateCookie.Value != "" && queryState == "":
		// Cookie present but no state in URL: ambiguous. Could be Setup URL
		// fired in the middle of a Callback URL session. Prefer safety --
		// reject and ask user to retry.
		writeError(w, http.StatusBadRequest, "MISSING_STATE",
			"Install session expired. Please retry the GitHub App install.")
		return
	default:
		// Path 2: Setup URL flow. Cookie absent OR expired AND no state in URL.
		// Allowed under the safety analysis above. Counter still increments
		// so SLO can observe install rate; the counter is bucketed by
		// setup_action which already lets us slice install vs update.
	}

	// EC-15: if the Kratos session has expired, push the user through login
	// preserving the installation_id so the connect flow resumes after sign-in.
	if h.sessionResolver != nil && !h.sessionResolver(r) {
		// loginURL?return_to=<dashboard>/projects?installation_id=<id>
		returnTo := h.returnToOrDefault(r) + "?" + resultQuery + "&installation_id=" + url.QueryEscape(installationIDStr)
		dest := h.loginURL + "?return_to=" + url.QueryEscape(returnTo)
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}

	// Happy path — record metric + redirect dashboard.
	cloudmetrics.IncGitHubAppInstallation(setupAction)

	// Clear the state cookies only if we actually consumed them. Setup URL
	// path leaves stale cookies untouched so a subsequent Install button
	// click (rare) still works without an extra reload.
	if statePath {
		clearCookie(w, stateCookieName, h.cookieDomain, h.cookieSecure)
		clearCookie(w, stateCookieName+"_return_to", h.cookieDomain, h.cookieSecure)
	}

	dest := h.returnToOrDefault(r) + "?" + resultQuery + "&installation_id=" + url.QueryEscape(installationIDStr)
	http.Redirect(w, r, dest, http.StatusFound)
}

func (h *GitHubIntegrationsHandler) returnToOrDefault(r *http.Request) string {
	if c, err := r.Cookie(stateCookieName + "_return_to"); err == nil && c.Value != "" {
		if path, ok := safeDashboardPath(c.Value); ok {
			return h.dashboardURL + path
		}
	}
	return h.dashboardURL + "/projects"
}

// safeDashboardPath returns the value as a dashboard-anchored path if it is
// a plain relative path (e.g. "/projects/demo/connect"). Anything that
// could parse as a URL with scheme or host -- including protocol-relative
// URLs like "//evil.com/x" -- is rejected so the caller falls back to the
// safe default. EC-4 of plan J-09 (open-redirect defense via return_to
// cookie injection).
func safeDashboardPath(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	// Reject anything that smells like a URL or backslash escape.
	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\x00") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if u.Scheme != "" || u.Host != "" {
		return "", false
	}
	path := u.RequestURI() // strips fragment/normalises
	if path == "" {
		return "", false
	}
	if path[0] != '/' {
		path = "/" + path
	}
	return path, true
}

func clearCookie(w http.ResponseWriter, name, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func newStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
