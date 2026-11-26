// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argocd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// newHTTPTestSyncClient creates a SyncClient pointing at a test server.
// Uses NewSyncClient so the serializer + every constructor-managed invariant
// is in place (vs struct-literal bypass that breaks subsequent invariant
// additions — caught regression iter Ralph 1 commit 088c721d). The httptest
// server is plain HTTP so cfg.Insecure / CACertPath don't matter.
func newHTTPTestSyncClient(t testing.TB, serverURL string) *SyncClient {
	t.Helper()
	return mustNewSyncClient(t, SyncClientConfig{
		BaseURL: serverURL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
		// HTTPClient deliberately nil — let NewSyncClient build the default
		// (with serializer + OTel transport wired correctly).
	})
}

func TestTriggerSync_HappyPath(t *testing.T) {
	// Arrange
	var syncCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MEET-2026-04-22-058 (B12): TriggerSync must NEVER issue PATCH anymore.
		// The operator is the sole writer of Application CR metadata.
		if r.Method == http.MethodPatch {
			t.Errorf("PATCH must not be called after skip-reconcile removal (path=%s)", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync") {
			syncCalled = true
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing auth header")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.TriggerSync(context.Background(), "my-app")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !syncCalled {
		t.Error("sync endpoint should have been called")
	}
}

func TestTriggerSync_AppNotFound(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sync") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.TriggerSync(context.Background(), "nonexistent")

	// Assert
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
}

func TestTriggerSync_AuthFailed(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sync") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.TriggerSync(context.Background(), "my-app")

	// Assert
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if err != loopruntimeerrors.ErrDeployServiceAuthFailed {
		t.Errorf("expected ErrDeployServiceAuthFailed, got: %v", err)
	}
}

func TestTriggerSync_ConflictReturnsTypedError(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sync") {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.TriggerSync(context.Background(), "my-app")

	// Assert
	if err != loopruntimeerrors.ErrSyncOperationInProgress {
		t.Errorf("expected ErrSyncOperationInProgress, got: %v", err)
	}
}

func TestGetAppStatus_Synced_Healthy(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced", "revision": "abc123"},
				"health": map[string]any{"status": "Healthy"},
			},
		})
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	synced, healthy, err := client.GetAppStatus(context.Background(), "my-app", nil)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !synced {
		t.Error("expected synced=true")
	}
	if !healthy {
		t.Error("expected healthy=true")
	}
}

func TestGetAppStatus_PerResourceHealth(t *testing.T) {
	// Arrange: two deployments, web healthy, api not healthy
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced"},
				"health": map[string]any{"status": "Degraded"},
				"resources": []map[string]any{
					{"kind": "Deployment", "name": "web", "health": map[string]any{"status": "Healthy"}},
					{"kind": "Deployment", "name": "api", "health": map[string]any{"status": "Progressing"}},
					{"kind": "Service", "name": "web"},
				},
			},
		})
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act: check only "web"
	synced, healthy, err := client.GetAppStatus(context.Background(), "my-app", []string{"web"})

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !synced {
		t.Error("expected synced=true")
	}
	if !healthy {
		t.Error("web is healthy, should report healthy=true")
	}

	// Act: check "api" which is Progressing
	_, healthy2, err := client.GetAppStatus(context.Background(), "my-app", []string{"api"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if healthy2 {
		t.Error("api is Progressing, should report healthy=false")
	}
}

// TestGetAppStatus_RolloutCounted_AsWorkloadPrimitive locks in the F-19
// fix: per-resource health must accept BOTH Deployment (legacy) AND
// Rollout (progressive delivery, default-on per
// argo-rollouts-progressive-delivery-plan T1.3). Without this, releases
// backed by Rollouts stay PENDING forever — observed during the post-
// canary-plan dogfood gate run as J05/J06 timeout failures.
func TestGetAppStatus_RolloutCounted_AsWorkloadPrimitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced"},
				"health": map[string]any{"status": "Healthy"},
				"resources": []map[string]any{
					{"kind": "Rollout", "name": "myproj-api", "health": map[string]any{"status": "Healthy"}},
					{"kind": "Service", "name": "myproj-api-stable"},
					{"kind": "TraefikService", "name": "myproj-api"},
				},
			},
		})
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)
	synced, healthy, err := client.GetAppStatus(context.Background(), "my-app", []string{"myproj-api"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !synced {
		t.Error("expected synced=true")
	}
	if !healthy {
		t.Error("Rollout myproj-api is Healthy → release should be considered healthy=true")
	}
}

// TestGetAppStatus_RolloutDegraded_BlocksHealthy verifies a Rollout in
// Degraded state correctly blocks release activation (mirror of the
// Deployment Progressing case in TestGetAppStatus_PerResourceHealth).
func TestGetAppStatus_RolloutDegraded_BlocksHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced"},
				"health": map[string]any{"status": "Degraded"},
				"resources": []map[string]any{
					{"kind": "Rollout", "name": "bad-app-api", "health": map[string]any{"status": "Degraded"}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)
	_, healthy, err := client.GetAppStatus(context.Background(), "my-app", []string{"bad-app-api"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if healthy {
		t.Error("Rollout bad-app-api is Degraded → release must NOT be considered healthy")
	}
}

func TestTerminateOperation_404IsIdempotent(t *testing.T) {
	// Arrange: 404 means no operation running
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.TerminateOperation(context.Background(), "my-app")

	// Assert
	if err != nil {
		t.Fatalf("404 should be treated as success, got: %v", err)
	}
}

func TestGetSyncRevision(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{
				"sync": map[string]any{"revision": "abc123def456"},
			},
		})
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	rev, err := client.GetSyncRevision(context.Background(), "my-app")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if rev != "abc123def456" {
		t.Errorf("revision = %q, want abc123def456", rev)
	}
}

func TestValidateToken_Success(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/userinfo" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"loggedIn":true}`))
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.ValidateToken(context.Background())

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateToken_InvalidReturnsAuthError(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newHTTPTestSyncClient(t, srv.URL)

	// Act
	err := client.ValidateToken(context.Background())

	// Assert
	if err != loopruntimeerrors.ErrDeployServiceAuthFailed {
		t.Errorf("expected ErrDeployServiceAuthFailed, got: %v", err)
	}
}

// mustNewSyncClient builds a SyncClient or fails the test.
func mustNewSyncClient(t testing.TB, cfg SyncClientConfig) *SyncClient {
	t.Helper()
	c, err := NewSyncClient(cfg)
	if err != nil {
		t.Fatalf("NewSyncClient: %v", err)
	}
	return c
}
