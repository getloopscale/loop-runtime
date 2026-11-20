// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"crypto/ed25519"
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/getloopscale/contracts/serviceauth"
)

// Config is the authentication configuration. The composition root reads it
// ONCE at boot (cmd/setup_auth.go) and hands it to NewAuthenticator; nothing in
// this package reads the environment or package-level state per request, so a
// test builds the exact configuration it needs and can run in parallel.
type Config struct {
	// ServiceSecret authenticates the cloud proxy's X-Service-Auth on user
	// routes; ServiceSecretPrevious is honoured during a rotation. An empty
	// ServiceSecret disables service auth.
	ServiceSecret         string
	ServiceSecretPrevious string

	// AdminServiceSecret authenticates operators on /api/v1/admin/* and on
	// admin modes of user routes; AdminServiceSecretPrevious during a
	// rotation. It is never the proxy secret: the proxy attaches that one to
	// every forwarded user request, so honouring it here would hand any user
	// the admin routes (loopzero-cloud#16).
	AdminServiceSecret         string
	AdminServiceSecretPrevious string

	// AdminAPIKey is the platform-admin key for cross-tenant operations
	// (tenant force-delete / restore); AdminAPIKeyPrevious during a rotation.
	AdminAPIKey         string
	AdminAPIKeyPrevious string

	// Ed25519PublicKeys is the trust set for signed service-auth (current and,
	// during a rotation, previous). Empty makes every SIGNED request fail
	// closed (RFC Q5).
	Ed25519PublicKeys []ed25519.PublicKey
	// RequireSignature rejects unsigned service-auth (Phase 2 of the ADR
	// 2026-05-13 cut-over). False accepts both (Phase 1).
	RequireSignature bool
	// NonceStore dedupes signature nonces. Nil means an in-memory store,
	// which does not protect across replicas (production wires Redis).
	NonceStore serviceauth.NonceStore
	// ReplayWindow bounds how far a signature's timestamp may drift. Nil
	// means 60 seconds.
	ReplayWindow *serviceauth.ReplayWindow

	// Auditor records authentication failures in the audit log. Nil records
	// nothing (tests, and boots without a database).
	Auditor *AuthFailureAuditor
}

// Authenticator applies one Config to every authentication decision the API
// makes: the user-route middleware, the admin gate, and the admin-key check.
type Authenticator struct {
	cfg Config
}

// NewAuthenticator returns an Authenticator for cfg, filling the documented
// defaults (in-memory nonce store, 60-second replay window).
func NewAuthenticator(cfg Config) *Authenticator {
	if cfg.NonceStore == nil {
		cfg.NonceStore = serviceauth.NewInMemoryNonceStore(5 * time.Minute)
	}
	if cfg.ReplayWindow == nil {
		cfg.ReplayWindow = serviceauth.NewReplayWindow(60 * time.Second)
	}
	return &Authenticator{cfg: cfg}
}

// Auth is the user-route authentication middleware: it validates Bearer
// tokens (cached for 5 minutes) or the proxy's service auth under a's
// configuration, and resolves tenant membership. It is a package function,
// not a method, so the router's auth group is recognisable as `<auth>.Auth`
// to the route-auth-coverage gate, which reads router.go without types.
func Auth(a *Authenticator, validator TokenValidator, store MembershipStore) func(http.Handler) http.Handler {
	return a.MiddlewareWithTTL(validator, store, 5*time.Minute)
}

// RequireAdminService refuses a request that does not carry the admin
// service-auth secret in X-Service-Auth. It is the single gate for every
// /api/v1/admin/* route (T2.1: the routes used to inline their own checks
// against divergent secrets).
func (a *Authenticator) RequireAdminService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Service-Auth") == "" {
			a.emitFailure(r, AuthFailureServiceMismatch, "missing X-Service-Auth header on admin endpoint", nil)
			writeError(w, http.StatusForbidden, "FORBIDDEN",
				"Admin service authentication required. Set X-Service-Auth header.")
			return
		}
		if a.AdminServiceAuthorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		a.emitFailure(r, AuthFailureServiceMismatch, "invalid X-Service-Auth secret on admin endpoint", nil)
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Invalid admin service-auth secret.")
	})
}

// AdminServiceAuthorized reports whether the request carries the admin
// service-auth secret. It is the one definition of "an operator is calling":
// RequireAdminService gates whole routes with it, and a handler that gates
// only a MODE of a user route (logs ?mode=debug) asks the same question.
func (a *Authenticator) AdminServiceAuthorized(r *http.Request) bool {
	return matchesSecret(r.Header.Get("X-Service-Auth"), a.cfg.AdminServiceSecret, a.cfg.AdminServiceSecretPrevious)
}

// ValidateAdminAPIKey reports whether provided is the platform-admin key
// (current or previous). Handlers serving a self-service AND an admin mode
// (tenants.Delete) validate the key's value — never its mere presence, which
// any caller can send (find_sec_001).
func (a *Authenticator) ValidateAdminAPIKey(provided string) bool {
	return matchesSecret(provided, a.cfg.AdminAPIKey, a.cfg.AdminAPIKeyPrevious)
}

// validateServiceAuth checks the proxy's X-Service-Auth (S1.2 dual-key).
func (a *Authenticator) validateServiceAuth(provided string) bool {
	return matchesSecret(provided, a.cfg.ServiceSecret, a.cfg.ServiceSecretPrevious)
}

// emitFailure records an authentication failure through the configured
// auditor; a nil auditor records nothing.
func (a *Authenticator) emitFailure(r *http.Request, action AuthFailureAction, reason string, metadata map[string]any) {
	a.cfg.Auditor.Emit(r.Context(), r, action, reason, metadata)
}

// matchesSecret compares provided against a current and a previous secret in
// constant time (find_sec_002). An empty provided value or an empty current
// secret never matches, so "no secret configured" cannot be probed for.
func matchesSecret(provided, current, previous string) bool {
	if provided == "" || current == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(current)) == 1 {
		return true
	}
	return previous != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(previous)) == 1
}
