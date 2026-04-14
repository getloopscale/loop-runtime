// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGiteaRepoManager_EnsureRepo_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	m := NewGiteaRepoManager(srv.URL, "test-token", "loopzero", srv.Client())
	if err := m.EnsureRepo(context.Background(), "my-repo", false); err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}
}

func TestGiteaRepoManager_EnsureRepo_CreateNew(t *testing.T) {
	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost {
			createCalled = true
			w.WriteHeader(http.StatusCreated)
			return
		}
	}))
	defer srv.Close()

	m := NewGiteaRepoManager(srv.URL, "test-token", "loopzero", srv.Client())
	if err := m.EnsureRepo(context.Background(), "new-repo", true); err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}
	if !createCalled {
		t.Error("expected create API to be called")
	}
}

func TestGiteaRepoManager_EnsureRepo_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// 409 = already exists (race condition)
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	m := NewGiteaRepoManager(srv.URL, "test-token", "loopzero", srv.Client())
	if err := m.EnsureRepo(context.Background(), "race-repo", false); err != nil {
		t.Fatalf("EnsureRepo should handle 409 conflict: %v", err)
	}
}

func TestGiteaRepoManager_EnsureRepo_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := NewGiteaRepoManager(srv.URL, "test-token", "loopzero", srv.Client())
	err := m.EnsureRepo(context.Background(), "fail-repo", false)
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestGiteaRepoManager_RepoURL(t *testing.T) {
	m := NewGiteaRepoManager("http://gitea.local:3000", "token", "loopzero", nil)
	got := m.RepoURL("my-repo")
	want := "http://gitea.local:3000/loopzero/my-repo.git"
	if got != want {
		t.Errorf("RepoURL = %q, want %q", got, want)
	}
}
