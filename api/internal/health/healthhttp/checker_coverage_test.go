// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Coverage tests for the HTTP service checker, moved out of the parent package's
// coverage_boost_test.go when the health family was extracted (B-080).
//
// A test belongs with the code it exercises: left behind it would have had to
// reach into the subpackage from the parent, which is the import direction the
// routes fence forbids.
package healthhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// =============================================================================
// HTTPServiceChecker
// =============================================================================

func TestHTTPServiceChecker_HealthyService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "test-token", server.Client())
	if checker.Name() != "test-svc" {
		t.Errorf("expected name 'test-svc', got %q", checker.Name())
	}

	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("expected healthy, got error: %v", err)
	}
}

func TestHTTPServiceChecker_AuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "bad-token", server.Client())
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !errors.Is(err, ErrServiceAuthFailed) {
		t.Errorf("expected ErrServiceAuthFailed, got: %v", err)
	}
}

func TestHTTPServiceChecker_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "", server.Client())
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestHTTPServiceChecker_NilClient(t *testing.T) {
	checker := NewHTTPServiceChecker("test-svc", "http://localhost:1/test", "", nil)
	if checker.client == nil {
		t.Error("expected default client when nil passed")
	}
}

func TestHTTPServiceChecker_ForbiddenIsAuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "token", server.Client())
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !errors.Is(err, ErrServiceAuthFailed) {
		t.Errorf("expected ErrServiceAuthFailed for 403, got: %v", err)
	}
}
