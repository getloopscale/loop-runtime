// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// T3.1 / VAL-003: first unit tests for the gitea/ package, closing
// part of the test pyramid inversion finding from the deep review.
//
// These tests exercise APIClient against an httptest.Server stub so we
// can validate URL construction, request shape, error handling, and
// JSON parsing without depending on a real Gitea instance.

func newClient(t *testing.T, handler http.HandlerFunc) (*APIClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewAPIClient(srv.URL, &http.Client{Timeout: 5 * time.Second}), srv
}

func TestCheckVersion_HappyPath(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/version" {
			t.Errorf("path = %q, want /api/v1/version", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"version":"1.22.6"}`))
	})

	if err := c.CheckVersion(context.Background()); err != nil {
		t.Fatalf("CheckVersion() error = %v, want nil", err)
	}
}

func TestCheckVersion_ServerError(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	err := c.CheckVersion(context.Background())
	if err == nil {
		t.Fatal("CheckVersion() error = nil, want non-nil for 500")
	}
}

func TestValidateToken_BearerHeaderSent(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		want := "token loopzero-test-token"
		if got != want {
			t.Errorf("Authorization header = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"username":"loopzero"}`))
	})

	if err := c.ValidateToken(context.Background(), "loopzero-test-token"); err != nil {
		t.Fatalf("ValidateToken() error = %v, want nil", err)
	}
}

func TestValidateToken_Unauthorized(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	err := c.ValidateToken(context.Background(), "wrong")
	if err == nil {
		t.Fatal("ValidateToken() error = nil, want non-nil for 401")
	}
}

func TestEnsureOrg_CreatesWhenMissing(t *testing.T) {
	createCalled := false
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// First call: org lookup returns 404 (not found)
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			createCalled = true
			if r.URL.Path != "/api/v1/orgs" {
				t.Errorf("create path = %q, want /api/v1/orgs", r.URL.Path)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	if err := c.EnsureOrg(context.Background(), "tok", "acme-org"); err != nil {
		t.Fatalf("EnsureOrg() error = %v", err)
	}
	if !createCalled {
		t.Error("EnsureOrg should POST /api/v1/orgs when org missing; create not invoked")
	}
}

func TestEnsureOrg_NoOpWhenExists(t *testing.T) {
	createCalled := false
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"username":"acme-org"}`))
		case http.MethodPost:
			createCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	if err := c.EnsureOrg(context.Background(), "tok", "acme-org"); err != nil {
		t.Fatalf("EnsureOrg() error = %v", err)
	}
	if createCalled {
		t.Error("EnsureOrg should NOT POST when org exists; create was invoked")
	}
}

func TestNewAPIClient_StripsTrailingSlash(t *testing.T) {
	c := NewAPIClient("https://gitea.example.com/", &http.Client{})
	if c.baseURL != "https://gitea.example.com" {
		t.Errorf("baseURL = %q, want trailing slash stripped", c.baseURL)
	}
}

func TestApiV1_URLConstruction(t *testing.T) {
	c := NewAPIClient("https://gitea.example.com", &http.Client{})
	got := c.apiV1("orgs", "acme", "repos")
	want := "https://gitea.example.com/api/v1/orgs/acme/repos"
	if got != want {
		t.Errorf("apiV1() = %q, want %q", got, want)
	}
}
