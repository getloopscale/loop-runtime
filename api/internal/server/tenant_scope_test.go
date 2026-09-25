// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
)

// scopeCapturingLookup records the database scope of the context a handler
// hands to its store.
type scopeCapturingLookup struct {
	scope  string
	scoped bool
}

func (l *scopeCapturingLookup) GetUsernameByID(ctx context.Context, _ string) (string, error) {
	l.scope, l.scoped = dbtx.TenantScope(ctx)
	return "fixed", nil
}

// Every tenant table is fail-closed under Row Level Security: a store sees
// only the rows of the tenant its connection is scoped to. The authenticated
// group must therefore hand every handler a context scoped to the tenant the
// caller was authenticated for — otherwise each store reads nothing.
func TestAnAuthenticatedRequestReachesItsStoresScopedToTheCallersTenant(t *testing.T) {
	lookup := &scopeCapturingLookup{}
	r := NewRouter(Config{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TokenValidator: fixedTokenValidator{},
		MemberStore:    fixedMembership{},
		AuthUserLookup: lookup,
		Guards: RouteGuards{
			RateLimiter:       ratelimit.NewRateLimiter(1000, time.Minute),
			IPRateLimit:       ratelimit.NewIPRateLimiter(ratelimit.IPRateLimitConfig{}),
			VerifyRateLimiter: ratelimit.NewVerifyRateLimiter(1000, time.Minute),
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer any")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("whoami = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !lookup.scoped || lookup.scope != "tn-fixed" {
		t.Fatalf("the store saw scope %q (scoped=%v); want tn-fixed", lookup.scope, lookup.scoped)
	}
}
