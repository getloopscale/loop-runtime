// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package themory

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// M1.4 T3.1 — cloud ↔ loopzero-data consumer-driven contract.
//
// cloud is the CONSUMER of loopzero-data (TheMemory); these tests validate that the
// client's outgoing REQUESTS conform to cloud/openapi/themory.yaml (the cloud's
// expectation of loopzero-data). They drive the real client against an httptest stub
// that captures the request, then validate it with openapi3filter. Plus the
// failure scenarios from the plan (404 → ErrNotFound; 5xx → *Error).

const themorySpec = "../../openapi/themory.yaml"

func loadThemorySpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(themorySpec)
	if err != nil {
		t.Fatalf("load themory.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("themory.yaml invalid: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router from themory.yaml: %v", err)
	}
	return doc, router
}

// captureServer records the last request it received and replies with the given
// status + JSON body.
func captureServer(t *testing.T, status int, body string, captured **http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// clone the request (body already consumed by the client is fine — these are GET/DELETE)
		*captured = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func validateRequest(t *testing.T, router routers.Router, req *http.Request) {
	t.Helper()
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("themory.yaml has no route for %s %s: %v", req.Method, req.URL.Path, err)
	}
	in := &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	}
	if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
		t.Fatalf("client request to %s %s does not conform to themory.yaml: %v",
			req.Method, req.URL.Path, err)
	}
}

func TestThemoryClient_RequestsConformToSpec(t *testing.T) {
	_, router := loadThemorySpec(t)

	t.Run("List", func(t *testing.T) {
		var got *http.Request
		srv := captureServer(t, 200, `{"memories":[]}`, &got)
		c := NewClient(srv.URL, "test-key", srv.Client())
		if _, err := c.List(context.Background(), "user-1", ListOptions{Limit: 10, Offset: 0}); err != nil {
			t.Fatalf("List: %v", err)
		}
		validateRequest(t, router, got)
	})

	t.Run("Metrics", func(t *testing.T) {
		var got *http.Request
		srv := captureServer(t, 200, `{}`, &got)
		c := NewClient(srv.URL, "test-key", srv.Client())
		if _, err := c.Metrics(context.Background(), "user-1"); err != nil {
			t.Fatalf("Metrics: %v", err)
		}
		validateRequest(t, router, got)
	})

	t.Run("Entities", func(t *testing.T) {
		var got *http.Request
		srv := captureServer(t, 200, `{}`, &got)
		c := NewClient(srv.URL, "test-key", srv.Client())
		if _, err := c.Entities(context.Background(), "user-1", "graph"); err != nil {
			t.Fatalf("Entities: %v", err)
		}
		validateRequest(t, router, got)
	})

	t.Run("Forget", func(t *testing.T) {
		var got *http.Request
		srv := captureServer(t, 204, ``, &got)
		c := NewClient(srv.URL, "test-key", srv.Client())
		if err := c.Forget(context.Background(), "user-1", "mem-123"); err != nil {
			t.Fatalf("Forget: %v", err)
		}
		validateRequest(t, router, got)
	})
}

// Failure scenarios (plan § Failure scenarios): external loopzero-data I/O.
func TestThemoryClient_FailureScenarios(t *testing.T) {
	t.Run("404 maps to ErrNotFound", func(t *testing.T) {
		var got *http.Request
		srv := captureServer(t, http.StatusNotFound, `{}`, &got)
		c := NewClient(srv.URL, "test-key", srv.Client())
		err := c.Forget(context.Background(), "user-1", "missing")
		if !IsNotFound(err) {
			t.Fatalf("expected ErrNotFound on 404, got: %v", err)
		}
	})

	t.Run("503 maps to *Error (no panic)", func(t *testing.T) {
		var got *http.Request
		srv := captureServer(t, http.StatusServiceUnavailable, `upstream down`, &got)
		c := NewClient(srv.URL, "test-key", srv.Client())
		_, err := c.List(context.Background(), "user-1", ListOptions{})
		if err == nil {
			t.Fatal("expected error on 503, got nil")
		}
		var te *Error
		if !errors.As(err, &te) {
			t.Fatalf("expected *themory.Error on 503, got %T: %v", err, err)
		}
		if te.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("Error.StatusCode = %d, want 503", te.StatusCode)
		}
	})
}
