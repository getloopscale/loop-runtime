// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package audit

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// --- Audit middleware tests ---

func TestAuditLog_SkipsGET(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	writerCalled := false
	writer := AuditWriter(func(_, _, _, _, _, _ string, _ map[string]any) {
		writerCalled = true
	})

	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if writerCalled {
		t.Error("audit writer should not be called for GET requests")
	}
}

func TestAuditLog_LogsMutationWithContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedAction string
	writer := AuditWriter(func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
		capturedAction = action
	})

	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-app", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKeyPublic(), &auth.UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedAction == "" {
		t.Error("audit writer should be called for POST requests with tenant context")
	}
}

// B-059: the three tests that stood here — EnrichResourceID_BuildSubmitted,
// _SecretSet_Query and _DomainAdded — asserted that this middleware mined
// `resource_id` out of the request body and the query string. That mechanism is
// deleted, so they could not be repaired, only moved: each route now declares
// its own id and each declaration has its own test beside the handler that
// makes it.
//
//	POST /api/v1/builds   → routes/builds   TestBuildsCreateDeclaresProjectAsResourceID
//	POST /api/v1/secrets  → routes/secrets  TestSecretsSetDeclaresProjectAsResourceID
//	POST /api/v1/domains  → routes/domains  TestDomainsCreateDeclaresProjectAsResourceID
//
// What the middleware still owes is in audit_declared_id_test.go: it records
// what the handler declared, falls back to the URL when nothing was declared,
// and never reads the body.

// Malformed JSON body must not crash the middleware nor corrupt the
// downstream handler — resource_id stays "" and the handler still sees
// the original (broken) bytes for its own error reporting.
func TestAuditLog_EnrichResourceID_MalformedBody(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	var capturedResourceID string
	writer := AuditWriter(func(_, _, _, _, resourceID, _ string, _ map[string]any) {
		capturedResourceID = resourceID
	})

	body := `{not valid json`
	var handlerSaw string
	handler := AuditLog(logger, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		handlerSaw = string(buf)
		w.WriteHeader(http.StatusBadRequest)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	ctx := context.WithValue(req.Context(), auth.UserContextKeyPublic(), &auth.UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedResourceID != "" {
		t.Errorf("resource_id = %q, want empty for malformed body", capturedResourceID)
	}
	if handlerSaw != body {
		t.Errorf("handler saw %q, want %q (body restoration)", handlerSaw, body)
	}
}

// --- Audit actions tests ---

func TestExtractResourceID(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"/api/v1/projects/my-app", "my-app"},
		{"/api/v1/secrets/DB_URL", "DB_URL"},
		{"/api/v1/teams/members/paulo", "paulo"},
	}
	for _, tt := range tests {
		got := extractResourceID(tt.path)
		if got != tt.want {
			t.Errorf("extractResourceID(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
