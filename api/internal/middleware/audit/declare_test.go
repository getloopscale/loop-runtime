// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

type recordedRow struct{ action, resourceType string }

func auditedRequest(method string) *http.Request {
	req := httptest.NewRequest(method, "/api/v1/things/tn-123", nil)
	uc := &auth.UserContext{UserID: "u-1", TenantID: "tn-1"}
	return req.WithContext(context.WithValue(req.Context(), auth.UserContextKeyPublic(), uc))
}

func runAudited(t *testing.T, route func(http.Handler) http.Handler) recordedRow {
	t.Helper()
	var row recordedRow
	writer := AuditWriter(func(_, action, _, resourceType, _, _ string, _ map[string]any) {
		row = recordedRow{action, resourceType}
	})
	var inner http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	if route != nil {
		inner = route(inner)
	}
	AuditLog(slog.New(slog.NewTextHandler(io.Discard, nil)), writer)(inner).
		ServeHTTP(httptest.NewRecorder(), auditedRequest(http.MethodPost))
	return row
}

func TestAuditLog_RecordsTheIdentityTheRouteDeclared(t *testing.T) {
	got := runAudited(t, Declare("project.scaled", "project"))
	if got != (recordedRow{"project.scaled", "project"}) {
		t.Fatalf("got %+v, want project.scaled / project", got)
	}
}

func TestAuditLog_RecordsAnUndeclaredMutationUnderABoundedName(t *testing.T) {
	got := runAudited(t, nil)
	if got != (recordedRow{"undeclared.POST", "undeclared"}) {
		t.Fatalf("got %+v, want undeclared.POST / undeclared", got)
	}
}

func TestDeclare_IsInertOutsideAnAuditedRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	Declare("x.y", "x")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Declare must pass the request through, got %d", rec.Code)
	}
}
