// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// countingBody wraps a request body and records how many times the middleware
// chain read from it. The audit middleware must never be one of those readers:
// reading the body is what forced the hand-kept list of body keys to exist,
// and what this slice removes.
type countingBody struct {
	r     *strings.Reader
	reads int
}

func (b *countingBody) Read(p []byte) (int, error) { b.reads++; return b.r.Read(p) }
func (b *countingBody) Close() error               { return nil }

// auditRun drives one request through the real AuditLog middleware and returns
// the resourceID the AuditWriter received. handler runs inside the chain, so it
// sees exactly the context the middleware handed down.
func auditRun(t *testing.T, req *http.Request, handler http.HandlerFunc) string {
	t.Helper()

	var gotResourceID string
	writer := AuditWriter(func(_, _, _, _, resourceID, _ string, _ map[string]any) {
		gotResourceID = resourceID
	})

	// The middleware only writes an audit row when a tenant is known; without
	// this the assertions below would pass over zero rows (the B-040 defect the
	// 2026-08-19 fixture note names).
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKeyPublic(), &auth.UserContext{
		TenantID: "tn-b059",
		UserID:   "user-b059",
	}))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	AuditLog(logger, writer)(handler).ServeHTTP(httptest.NewRecorder(), req)

	return gotResourceID
}

// TestAuditLogRecordsResourceIDDeclaredByHandler — B-059 FR-001/FR-002.
//
// The whole point of the declared-id surface is that the handler, which knows
// the id, tells the middleware. This asserts the declaration actually arrives:
// a handler that calls SetResourceID on its own request context must see that
// value land in the audit row.
//
// The route here has no id in its URL path (`POST /api/v1/builds`), which is
// exactly the population that relied on body-snooping.
func TestAuditLogRecordsResourceIDDeclaredByHandler(t *testing.T) {
	// Arrange.
	const declared = "checkout-service"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	// Act.
	got := auditRun(t, req, func(w http.ResponseWriter, r *http.Request) {
		SetResourceID(r.Context(), declared)
		w.WriteHeader(http.StatusAccepted)
	})

	// Assert.
	if got != declared {
		t.Fatalf("audit row resource_id = %q; want %q declared by the handler", got, declared)
	}
}

// TestAuditLogDeclaredResourceIDBeatsURLExtraction — B-059 D2 fallback order.
//
// Context first, URL second. When the handler declares an id AND the URL also
// carries one, the handler wins: it is the only one of the two that knows which
// entity the mutation actually touched.
func TestAuditLogDeclaredResourceIDBeatsURLExtraction(t *testing.T) {
	// Arrange — the URL would yield "from-url" via extractResourceID.
	const declared = "from-handler"
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/from-url", nil)

	// Act.
	got := auditRun(t, req, func(w http.ResponseWriter, r *http.Request) {
		SetResourceID(r.Context(), declared)
		w.WriteHeader(http.StatusOK)
	})

	// Assert.
	if got != declared {
		t.Fatalf("audit row resource_id = %q; want the handler-declared %q to win over the URL", got, declared)
	}
}

// TestAuditLogFallsBackToURLWhenHandlerDeclaresNothing — B-059 D2 fallback order.
//
// The URL fallback is the one guess that stays, because the router bound it.
// A handler that declares nothing must not lose the id its path already carries.
func TestAuditLogFallsBackToURLWhenHandlerDeclaresNothing(t *testing.T) {
	// Arrange.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/from-url", nil)

	// Act.
	got := auditRun(t, req, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Assert.
	if got != "from-url" {
		t.Fatalf("audit row resource_id = %q; want %q from the URL path", got, "from-url")
	}
}

// TestAuditLogNeverReadsTheRequestBody — B-059 FR-003/FR-004.
//
// The 64 KiB body buffer is the cost this slice removes. The middleware must
// hand the body to the handler untouched: zero reads attributable to the audit
// layer, and no id conjured out of a `project_id` key it has no business
// knowing about.
func TestAuditLogNeverReadsTheRequestBody(t *testing.T) {
	// Arrange — a body that the old snooper would have happily mined.
	body := &countingBody{r: strings.NewReader(`{"project_id":"snooped-value"}`)}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(body.r.Len())

	// Act — the handler never reads the body either, so every read counted
	// below belongs to the middleware.
	got := auditRun(t, req, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	// Assert.
	if body.reads != 0 {
		t.Fatalf("audit middleware read the request body %d time(s); it must never read it", body.reads)
	}
	if got != "" {
		t.Fatalf("audit row resource_id = %q; want empty — the middleware must not guess it from the body", got)
	}
}
