// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
)

// idempotencyKeyRequired lists the mutations whose client MUST send an
// Idempotency-Key: each one creates something a retry would duplicate, and
// every client of each sends the key (loopzero-cli deploy and domain add).
var idempotencyKeyRequired = map[string]bool{
	"POST /api/v1/builds":                      true,
	"POST /api/v1/domains":                     true,
	"POST /api/v1/projects/{project}/previews": true,
}

// idempotencyKeyNotRequired records, per mutation, the decision NOT to
// require an Idempotency-Key and what it rests on. The shared basis: no
// client of these routes sends the header (loopzero-cli sends it only for deploy
// and domain add), so requiring it would refuse every call; each is rate
// limited instead, and the reason says what makes a retry tolerable.
var idempotencyKeyNotRequired = map[string]string{
	"POST /api/v1/source/upload":                        "rate limited; no client retries it automatically",
	"POST /api/v1/projects":                             "a retry of a created project answers 409 PROJECT_EXISTS",
	"DELETE /api/v1/projects/{name}":                    "deleting what is already gone leaves the same state",
	"DELETE /api/v1/projects/{name}/environments/{env}": "deleting what is already gone leaves the same state",
	"POST /api/v1/secrets":                              "an upsert under the environment lock: a retry rewrites the same value",
	"POST /api/v1/secrets/bulk":                         "an upsert under the environment lock: a retry rewrites the same values",
	"DELETE /api/v1/secrets/{name}":                     "deleting what is already gone leaves the same state",
	"POST /api/v1/builds/{id}/cancel":                   "rate limited; no client retries it automatically",
	"POST /api/v1/projects/{name}/scale":                "writes an absolute replica count",
	"POST /api/v1/projects/{name}/rollback":             "rate limited; no client retries it automatically",
	"POST /api/v1/projects/{name}/promote":              "rate limited; no client retries it automatically",
	"POST /api/v1/teams/members":                        "rate limited; no client retries it automatically",
	"DELETE /api/v1/teams/members/{username}":           "deleting what is already gone leaves the same state",
	"PATCH /api/v1/teams/members/{username}":            "writes an absolute role",
	"DELETE /api/v1/tenants/{id}":                       "deleting what is already gone leaves the same state",
	"POST /api/v1/tenants/{id}/restore":                 "rate limited; no client retries it automatically",
	"POST /api/v1/billing/portal":                       "rate limited; no client retries it automatically",
	"POST /api/v1/environments":                         "a retry of a created environment answers 409",
	"DELETE /api/v1/environments/{name}":                "deleting what is already gone leaves the same state",
	"POST /api/v1/domains/{domain}/verify":              "rate limited; no client retries it automatically",
	"DELETE /api/v1/domains/{domain}":                   "deleting what is already gone leaves the same state",
	"POST /api/v1/projects/{name}/connect":              "rate limited; no client retries it automatically",
	"DELETE /api/v1/projects/{name}/connect":            "deleting what is already gone leaves the same state",
	"DELETE /api/v1/projects/{project}/previews/{id}":   "deleting what is already gone leaves the same state",
	"POST /api/v1/auth/team/switch":                     "writes the active team absolutely",
}

type memoryIdempotencyStore struct{}

func (memoryIdempotencyStore) Get(context.Context, string, string) (*policy.IdempotencyRecord, error) {
	return nil, nil
}
func (memoryIdempotencyStore) Store(context.Context, *policy.IdempotencyRecord) error { return nil }

func guardedRouter(t *testing.T, limiter *ratelimit.RateLimiter) http.Handler {
	t.Helper()
	cfg := fullProtectedConfig()
	cfg.Guards.RateLimiter = limiter
	cfg.Guards.IdempotencyStore = memoryIdempotencyStore{}
	cfg.ActiveTeamStore = stubActiveTeamStore{}
	// The per-domain verify limiter is exercised by its own tests; here the
	// tenant limiter must be the one that answers.
	cfg.Guards.VerifyRateLimiter = nil
	cfg.AuditWriter = func(string, string, string, string, string, string, map[string]any) {}
	return NewRouter(cfg)
}

func mutatingProtectedRoutes(t *testing.T) []string {
	t.Helper()
	var out []string
	for rt := range routeSet(t, NewRouter(guardedConfigForWalk())) {
		method := strings.SplitN(rt, " ", 2)[0]
		if method == http.MethodGet || !strings.HasPrefix(strings.SplitN(rt, " ", 2)[1], "/api/v1/") ||
			strings.Contains(rt, "/admin/") || strings.Contains(rt, "/webhooks/") {
			continue
		}
		out = append(out, rt)
	}
	sort.Strings(out)
	return out
}

func guardedConfigForWalk() Config {
	cfg := fullProtectedConfig()
	cfg.ActiveTeamStore = stubActiveTeamStore{}
	return cfg
}

// serveRecovering drives one request. The registration-only handlers panic
// when reached; the guards under test answer before the handler, so only the
// status they write is asserted.
func serveRecovering(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(rec, req)
	}()
	return rec
}

func mutationRequest(route string, withKey bool) *http.Request {
	parts := strings.SplitN(route, " ", 2)
	req := httptest.NewRequest(parts[0], concretePath(parts[1]), strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer fixed")
	req.Header.Set("Content-Type", "application/json")
	if withKey {
		req.Header.Set("Idempotency-Key", "11111111-1111-4111-8111-111111111111")
	}
	return req
}

// Every mutation carries a written idempotency decision.
func TestMutatingRoutes_EachHasAnIdempotencyDecision(t *testing.T) {
	for _, rt := range mutatingProtectedRoutes(t) {
		_, required := idempotencyKeyRequired[rt]
		_, notRequired := idempotencyKeyNotRequired[rt]
		if required == notRequired {
			t.Errorf("%s: declare it in exactly one of idempotencyKeyRequired / idempotencyKeyNotRequired", rt)
		}
	}
}

// The routes that require a key refuse a request without one.
func TestMutatingRoutes_KeyRequiredRoutesRefuseAMissingKey(t *testing.T) {
	h := guardedRouter(t, ratelimit.NewRateLimiter(1000, time.Hour))
	for rt := range idempotencyKeyRequired {
		rec := serveRecovering(h, mutationRequest(rt, false))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MISSING_IDEMPOTENCY_KEY") {
			t.Errorf("%s without Idempotency-Key: want 400 MISSING_IDEMPOTENCY_KEY, got %d %s", rt, rec.Code, rec.Body.String())
		}
	}
}

// Every mutation is rate limited: with a budget of one, the second request
// is refused before the handler.
func TestMutatingRoutes_EveryMutationIsRateLimited(t *testing.T) {
	for _, rt := range mutatingProtectedRoutes(t) {
		h := guardedRouter(t, ratelimit.NewRateLimiter(1, time.Hour))
		serveRecovering(h, mutationRequest(rt, true))
		if rec := serveRecovering(h, mutationRequest(rt, true)); rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s: second request under a budget of one got %d, want 429", rt, rec.Code)
		}
	}
}

type stubActiveTeamStore struct{}

func (stubActiveTeamStore) GetActiveTenant(context.Context, string) (string, error) { return "", nil }
func (stubActiveTeamStore) SetActiveTenant(context.Context, string, string) error   { return nil }
func (stubActiveTeamStore) ClearActiveTenant(context.Context, string) error         { return nil }
