// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package harbor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- EnsureProject ---

func TestEnsureProject_CreatesNewProject(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/projects":
			// Project does not exist — return empty list.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/projects":
			called.Add(1)
			// Verify request body.
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			projectName, ok := body["project_name"].(string)
			if !ok || projectName != "acme" {
				t.Errorf("expected project_name=acme, got %v", body["project_name"])
			}
			// Verify basic auth.
			user, pass, ok := r.BasicAuth()
			if !ok || user != "admin" || pass != "secret" {
				t.Errorf("expected basic auth admin:secret, got %s:%s", user, pass)
			}
			w.WriteHeader(http.StatusCreated)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.EnsureProject(context.Background(), "acme", ProjectOpts{StorageQuota: -1})
	if err != nil {
		t.Fatalf("EnsureProject failed: %v", err)
	}
	if called.Load() != 1 {
		t.Fatalf("expected 1 POST call, got %d", called.Load())
	}
}

func TestEnsureProject_IdempotentWhenExists(t *testing.T) {
	var postCalled atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/projects":
			// Project already exists — return it.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"project_id":42,"name":"acme"}]`))

		case r.Method == http.MethodPut && r.URL.Path == "/api/v2.0/projects/42":
			// Update is fine — idempotent.
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/projects":
			postCalled.Add(1)
			w.WriteHeader(http.StatusCreated)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.EnsureProject(context.Background(), "acme", ProjectOpts{StorageQuota: -1})
	if err != nil {
		t.Fatalf("EnsureProject failed: %v", err)
	}
	if postCalled.Load() != 0 {
		t.Fatalf("expected 0 POST calls (project exists), got %d", postCalled.Load())
	}
}

func TestEnsureProject_Conflict409IsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/projects":
			// First check says not found.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/projects":
			// Race: another reconcile created it first.
			w.WriteHeader(http.StatusConflict)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.EnsureProject(context.Background(), "acme", ProjectOpts{})
	if err != nil {
		t.Fatalf("EnsureProject should treat 409 as idempotent success, got: %v", err)
	}
}

// --- DeleteProject ---

func TestDeleteProject_SendsDeleteRequest(t *testing.T) {
	var deleteCalled atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"project_id":42,"name":"acme"}]`))

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2.0/projects/42":
			deleteCalled.Add(1)
			w.WriteHeader(http.StatusOK)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.DeleteProject(context.Background(), "acme")
	if err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
	if deleteCalled.Load() != 1 {
		t.Fatalf("expected 1 DELETE call, got %d", deleteCalled.Load())
	}
}

func TestDeleteProject_NotFoundIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.DeleteProject(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("DeleteProject should treat not-found as idempotent success, got: %v", err)
	}
}

// --- EnsureRobotAccount ---

func TestEnsureRobotAccount_CreatesNewRobot(t *testing.T) {
	var postCalled atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2.0/projects/") && strings.HasSuffix(r.URL.Path, "/robots"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			// No existing robot.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/robots":
			postCalled.Add(1)
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if body["name"] != "build-acme" {
				t.Errorf("expected name=build-acme, got %v", body["name"])
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			resp := map[string]interface{}{
				"id":            1,
				"name":          "robot$build-acme",
				"secret":        "robot-secret-token",
				"creation_time": time.Now().Format(time.RFC3339),
				"expires_at":    time.Now().Add(24 * time.Hour).Unix(),
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	creds, err := c.EnsureRobotAccount(context.Background(), "acme", RobotAccountOpts{
		Name:       "build-acme",
		TTL:        24 * time.Hour,
		PushAccess: true,
	})
	if err != nil {
		t.Fatalf("EnsureRobotAccount failed: %v", err)
	}
	if creds.Username != "robot$build-acme" {
		t.Errorf("expected username robot$build-acme, got %s", creds.Username)
	}
	if creds.Secret != "robot-secret-token" {
		t.Errorf("expected secret robot-secret-token, got %s", creds.Secret)
	}
	if postCalled.Load() != 1 {
		t.Fatalf("expected 1 POST call, got %d", postCalled.Load())
	}
}

func TestEnsureRobotAccount_ReturnsExistingIfNotExpired(t *testing.T) {
	var postCalled atomic.Int32
	expiresAt := time.Now().Add(12 * time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2.0/projects/") && strings.HasSuffix(r.URL.Path, "/robots"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			// Existing robot, not expired.
			w.Header().Set("Content-Type", "application/json")
			resp := []map[string]interface{}{
				{
					"id":         5,
					"name":       "robot$build-acme",
					"expires_at": expiresAt,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/robots":
			postCalled.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     99,
				"name":   "robot$build-acme",
				"secret": "new-secret",
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	creds, err := c.EnsureRobotAccount(context.Background(), "acme", RobotAccountOpts{
		Name:       "build-acme",
		TTL:        24 * time.Hour,
		PushAccess: true,
	})
	if err != nil {
		t.Fatalf("EnsureRobotAccount failed: %v", err)
	}
	// When existing and not expired, we get credentials back without creating new.
	if postCalled.Load() != 0 {
		t.Fatalf("expected 0 POST calls (robot exists and not expired), got %d", postCalled.Load())
	}
	if creds.Username != "robot$build-acme" {
		t.Errorf("expected username robot$build-acme, got %s", creds.Username)
	}
}

func TestEnsureRobotAccount_RecreatesWhenNearExpiry(t *testing.T) {
	var deleteCalled, postCalled atomic.Int32
	// Robot expires in 30 minutes — below the default renewal threshold.
	expiresAt := time.Now().Add(30 * time.Minute).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2.0/projects/") && strings.HasSuffix(r.URL.Path, "/robots"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			w.Header().Set("Content-Type", "application/json")
			resp := []map[string]interface{}{
				{
					"id":         5,
					"name":       "robot$build-acme",
					"expires_at": expiresAt,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2.0/robots/5":
			deleteCalled.Add(1)
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/robots":
			postCalled.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			resp := map[string]interface{}{
				"id":         10,
				"name":       "robot$build-acme",
				"secret":     "refreshed-secret",
				"expires_at": time.Now().Add(24 * time.Hour).Unix(),
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	creds, err := c.EnsureRobotAccount(context.Background(), "acme", RobotAccountOpts{
		Name:       "build-acme",
		TTL:        24 * time.Hour,
		PushAccess: true,
	})
	if err != nil {
		t.Fatalf("EnsureRobotAccount failed: %v", err)
	}
	if deleteCalled.Load() != 1 {
		t.Fatalf("expected 1 DELETE call to remove expired robot, got %d", deleteCalled.Load())
	}
	if postCalled.Load() != 1 {
		t.Fatalf("expected 1 POST call to create new robot, got %d", postCalled.Load())
	}
	if creds.Secret != "refreshed-secret" {
		t.Errorf("expected refreshed-secret, got %s", creds.Secret)
	}
}

// --- DeleteRobotAccount ---

func TestDeleteRobotAccount_SendsDeleteRequest(t *testing.T) {
	var deleteCalled atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2.0/projects/") && strings.HasSuffix(r.URL.Path, "/robots"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			w.Header().Set("Content-Type", "application/json")
			resp := []map[string]interface{}{
				{"id": 7, "name": "robot$build-acme"},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2.0/robots/7":
			deleteCalled.Add(1)
			w.WriteHeader(http.StatusOK)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.DeleteRobotAccount(context.Background(), "acme", "build-acme")
	if err != nil {
		t.Fatalf("DeleteRobotAccount failed: %v", err)
	}
	if deleteCalled.Load() != 1 {
		t.Fatalf("expected 1 DELETE call, got %d", deleteCalled.Load())
	}
}

func TestDeleteRobotAccount_NotFoundIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2.0/projects/") && strings.HasSuffix(r.URL.Path, "/robots"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.DeleteRobotAccount(context.Background(), "acme", "nonexistent")
	if err != nil {
		t.Fatalf("DeleteRobotAccount should be idempotent for missing robot, got: %v", err)
	}
}

// --- Healthy ---

func TestHealthy_ReturnsNilOnOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	if err := c.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy should return nil on 200, got: %v", err)
	}
}

func TestHealthy_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.Healthy(context.Background())
	if err == nil {
		t.Fatal("Healthy should return error on 503")
	}
}

// --- Error handling ---

func TestEnsureProject_ReturnsStructuredErrorOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/projects":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"code":"INTERNAL_ERROR","message":"db down"}]}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	err := c.EnsureProject(context.Background(), "acme", ProjectOpts{})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	// Verify the error contains useful context.
	errMsg := err.Error()
	if len(errMsg) == 0 {
		t.Fatal("error message should not be empty")
	}
}

func TestEnsureProject_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is done (simulates slow server).
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.EnsureProject(ctx, "acme", ProjectOpts{})
	if err == nil {
		t.Fatal("expected error on context timeout")
	}
}

func TestEnsureRobotAccount_PullOnlyPermissions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// CHG-002b 2026-04-28 — project-scoped robot list (preferred path).
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2.0/projects/") && strings.HasSuffix(r.URL.Path, "/robots"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2.0/robots":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Verify no push action in permissions.
			perms, ok := body["permissions"].([]interface{})
			if !ok || len(perms) == 0 {
				t.Errorf("expected permissions array, got %v", body["permissions"])
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			perm := perms[0].(map[string]interface{})
			accessList := perm["access"].([]interface{})
			for _, a := range accessList {
				access := a.(map[string]interface{})
				if access["action"] == "push" {
					t.Errorf("pull-only robot should not have push action")
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":         1,
				"name":       "robot$pull-acme",
				"secret":     "pull-secret",
				"expires_at": time.Now().Add(24 * time.Hour).Unix(),
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	creds, err := c.EnsureRobotAccount(context.Background(), "acme", RobotAccountOpts{
		Name:       "pull-acme",
		TTL:        24 * time.Hour,
		PushAccess: false,
	})
	if err != nil {
		t.Fatalf("EnsureRobotAccount failed: %v", err)
	}
	if creds.Username != "robot$pull-acme" {
		t.Errorf("expected username robot$pull-acme, got %s", creds.Username)
	}
}

// RequireContentTrust maps to Harbor's content-trust switch (only signed
// images may be pulled) — the setting the option name promises.
func TestEnsureProject_RequireContentTrustEnablesContentTrust(t *testing.T) {
	var metadata map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`[]`))
		case http.MethodPost:
			var body struct {
				Metadata map[string]string `json:"metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode: %v", err)
			}
			metadata = body.Metadata
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	if err := c.EnsureProject(context.Background(), "acme", ProjectOpts{RequireContentTrust: true}); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	if metadata["enable_content_trust"] != "true" {
		t.Fatalf("metadata = %v, want enable_content_trust=true", metadata)
	}
}
