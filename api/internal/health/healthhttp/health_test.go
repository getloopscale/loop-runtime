// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package healthhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Fake DBHealthChecker ---

type fakeDBHealthChecker struct {
	err error
}

func (f *fakeDBHealthChecker) Ping() error {
	return f.err
}

// --- Fake ServiceHealthChecker ---

type fakeServiceChecker struct {
	name string
	err  error
}

func (f *fakeServiceChecker) Name() string                  { return f.name }
func (f *fakeServiceChecker) Check(_ context.Context) error { return f.err }

// --- Fake CellDiscoveryChecker ---

type fakeCellDiscovery struct {
	count   int
	dynamic bool
}

func (f *fakeCellDiscovery) DiscoveredCellCount() int { return f.count }
func (f *fakeCellDiscovery) IsDynamic() bool          { return f.dynamic }

// --- Tests ---

func TestHealth_Liveness(t *testing.T) {
	h := NewHealthHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("want status ok, got %s", resp["status"])
	}
}

func TestHealth_Deep_AllHealthy(t *testing.T) {
	db := &fakeDBHealthChecker{}
	svc := &fakeServiceChecker{name: "argocd"}
	h := NewHealthHandler(db, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("want status ok, got %s", resp["status"])
	}
	if resp["database"] != "connected" {
		t.Fatalf("want database connected, got %s", resp["database"])
	}
	if resp["argocd"] != "connected" {
		t.Fatalf("want argocd connected, got %s", resp["argocd"])
	}
}

func TestHealth_Deep_DBDown(t *testing.T) {
	db := &fakeDBHealthChecker{err: errors.New("connection refused")}
	h := NewHealthHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "degraded" {
		t.Fatalf("want status degraded, got %s", resp["status"])
	}
	if resp["database"] != "disconnected" {
		t.Fatalf("want database disconnected, got %s", resp["database"])
	}
}

func TestHealth_Deep_ServiceAuthFailed(t *testing.T) {
	db := &fakeDBHealthChecker{}
	svc := &fakeServiceChecker{name: "argocd", err: fmt.Errorf("argocd: %w", ErrServiceAuthFailed)}
	h := NewHealthHandler(db, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["argocd"] != "auth_failed" {
		t.Fatalf("want argocd auth_failed, got %s", resp["argocd"])
	}
}

func TestHealth_Deep_ServiceUnreachable(t *testing.T) {
	db := &fakeDBHealthChecker{}
	svc := &fakeServiceChecker{name: "gitea", err: errors.New("gitea: unreachable")}
	h := NewHealthHandler(db, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["gitea"] != "unreachable" {
		t.Fatalf("want gitea unreachable, got %s", resp["gitea"])
	}
}

func TestHealth_Deep_NilDB(t *testing.T) {
	h := NewHealthHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when db is nil, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHealth_Deep_WithCellDiscovery_Dynamic(t *testing.T) {
	db := &fakeDBHealthChecker{}
	h := NewHealthHandler(db)
	h.WithCellDiscovery(&fakeCellDiscovery{count: 3, dynamic: true})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHealth_Deep_WithCellDiscovery_ZeroCells(t *testing.T) {
	db := &fakeDBHealthChecker{}
	h := NewHealthHandler(db)
	h.WithCellDiscovery(&fakeCellDiscovery{count: 0, dynamic: true})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
	w := httptest.NewRecorder()

	h.DeepHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when 0 cells, got %d body=%s", w.Code, w.Body.String())
	}
}
