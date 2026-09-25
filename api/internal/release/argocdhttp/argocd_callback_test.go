// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocdhttp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Fake ReleaseActivator ---

type fakeReleaseActivator struct {
	err     error
	called  bool
	appName string
}

func (f *fakeReleaseActivator) OnSyncCompleted(appName, _ string) error {
	f.called = true
	f.appName = appName
	return f.err
}

// --- Tests ---

func TestArgoCallback_SyncedHealthy(t *testing.T) {
	activator := &fakeReleaseActivator{}
	h := NewArgoCallbackHandler(activator, slog.Default(), "")

	payload := ArgoCallbackPayload{
		AppName:      "tn-loopzero-production",
		SyncStatus:   "Synced",
		HealthStatus: "Healthy",
		Revision:     "abc123",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/internal/argocd-callback", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !activator.called {
		t.Fatal("want activator.OnSyncCompleted to be called")
	}
	if activator.appName != "tn-loopzero-production" {
		t.Fatalf("want app_name tn-loopzero-production, got %s", activator.appName)
	}
}

func TestArgoCallback_NotSynced_Ignored(t *testing.T) {
	activator := &fakeReleaseActivator{}
	h := NewArgoCallbackHandler(activator, slog.Default(), "")

	payload := ArgoCallbackPayload{
		AppName:      "tn-loopzero-production",
		SyncStatus:   "OutOfSync",
		HealthStatus: "Progressing",
		Revision:     "abc123",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/internal/argocd-callback", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if activator.called {
		t.Fatal("want activator NOT called for non-synced state")
	}
}

func TestArgoCallback_InvalidPayload(t *testing.T) {
	h := NewArgoCallbackHandler(&fakeReleaseActivator{}, slog.Default(), "")

	req := httptest.NewRequest(http.MethodPost, "/internal/argocd-callback", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestArgoCallback_SecretRequired_Unauthorized(t *testing.T) {
	h := NewArgoCallbackHandler(&fakeReleaseActivator{}, slog.Default(), "my-secret")

	payload := ArgoCallbackPayload{AppName: "app", SyncStatus: "Synced", HealthStatus: "Healthy"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/internal/argocd-callback", bytes.NewReader(body))
	// No Authorization header
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestArgoCallback_SecretRequired_WrongSecret(t *testing.T) {
	h := NewArgoCallbackHandler(&fakeReleaseActivator{}, slog.Default(), "my-secret")

	payload := ArgoCallbackPayload{AppName: "app", SyncStatus: "Synced", HealthStatus: "Healthy"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/internal/argocd-callback", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-secret")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestArgoCallback_SecretRequired_CorrectSecret(t *testing.T) {
	activator := &fakeReleaseActivator{}
	h := NewArgoCallbackHandler(activator, slog.Default(), "my-secret")

	payload := ArgoCallbackPayload{AppName: "app", SyncStatus: "Synced", HealthStatus: "Healthy", Revision: "abc"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/internal/argocd-callback", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer my-secret")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !activator.called {
		t.Fatal("want activator called with correct secret")
	}
}
