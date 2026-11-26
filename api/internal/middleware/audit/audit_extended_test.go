// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's auth_extended_test.go (B-059).

package audit

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// --- Audit log extended tests ---

func TestAuditLog_NilWriter_LogsOnly(t *testing.T) {
	// When auditWriter is nil, should still process the request and log via slog
	logger := slog.Default()
	handler := AuditLog(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
}

func TestAuditLog_SkipsHEAD(t *testing.T) {
	writerCalled := false
	writer := AuditWriter(func(_, _, _, _, _, _ string, _ map[string]any) {
		writerCalled = true
	})

	handler := AuditLog(slog.Default(), writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodHead, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if writerCalled {
		t.Error("audit writer should not be called for HEAD requests")
	}
}

func TestAuditLog_SkipsOPTIONS(t *testing.T) {
	writerCalled := false
	writer := AuditWriter(func(_, _, _, _, _, _ string, _ map[string]any) {
		writerCalled = true
	})

	handler := AuditLog(slog.Default(), writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/builds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if writerCalled {
		t.Error("audit writer should not be called for OPTIONS requests")
	}
}

func TestAuditLog_IncludesCorrelationID(t *testing.T) {
	var capturedCorrelation string
	writer := AuditWriter(func(_, _, _, _, _, correlationID string, _ map[string]any) {
		capturedCorrelation = correlationID
	})

	handler := AuditLog(slog.Default(), writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKeyPublic(), &auth.UserContext{
		UserID: "u-1", TenantID: "tn-1", Role: "owner",
	})
	req.Header.Set("Correlation-ID", "corr-abc-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))

	if capturedCorrelation != "corr-abc-123" {
		t.Errorf("expected correlation ID corr-abc-123, got %q", capturedCorrelation)
	}
}

func TestAuditLog_NoTenant_SkipsDBWrite(t *testing.T) {
	writerCalled := false
	writer := AuditWriter(func(_, _, _, _, _, _ string, _ map[string]any) {
		writerCalled = true
	})

	handler := AuditLog(slog.Default(), writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST without UserContext (no tenant)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if writerCalled {
		t.Error("audit writer should not be called when no tenant in context")
	}
}
