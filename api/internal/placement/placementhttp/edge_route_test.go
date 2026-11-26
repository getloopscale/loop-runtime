// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placementhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/placement"
)

type fakeEdgeResolver map[string]string

func (f fakeEdgeResolver) Resolve(_ context.Context, host string) (string, error) {
	switch host {
	case "boom.example":
		return "", placement.ErrEdgeRoutesNotLoaded
	case "odd.example":
		return "", errors.New("something else")
	}
	if c, ok := f[host]; ok {
		return c, nil
	}
	return "", placement.ErrEdgeRouteNotFound
}

const edgeToken = "edge-token-for-tests"

func edgeRequest(host, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/internal/edge/route", nil)
	if host != "" {
		req.Header.Set("X-Forwarded-Host", host)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func serveEdge(req *http.Request) *httptest.ResponseRecorder {
	h := NewEdgeRouteHandler(fakeEdgeResolver{"app.prod-dev.getloopscale.app": "cell-1"}, edgeToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The edge asks which cell serves the host it received and forwards there; the
// answer travels in a header the edge copies into its own request.
func TestEdgeRouteAnswersTheCellForAPublishedHost(t *testing.T) {
	rec := serveEdge(edgeRequest("app.prod-dev.getloopscale.app", edgeToken))
	if rec.Code != http.StatusOK || rec.Header().Get("X-LoopRuntime-Cell") != "cell-1" {
		t.Errorf("status=%d cell=%q", rec.Code, rec.Header().Get("X-LoopRuntime-Cell"))
	}
}

// An unknown host is refused with a 404 the edge relays as-is, in product language.
func TestEdgeRouteRefusesAnUnpublishedHost(t *testing.T) {
	rec := serveEdge(edgeRequest("nobody.prod-dev.getloopscale.app", edgeToken))
	if rec.Code != http.StatusNotFound || rec.Header().Get("X-LoopRuntime-Cell") != "" {
		t.Errorf("status=%d cell=%q", rec.Code, rec.Header().Get("X-LoopRuntime-Cell"))
	}
	for _, internal := range []string{"cell", "tenant", "ExposedRoute"} {
		if strings.Contains(rec.Body.String(), internal) {
			t.Errorf("body names an internal term %q: %q", internal, rec.Body.String())
		}
	}
}

// Before the index has ever loaded, the edge gets 503 — a retryable answer — never a
// 404 that would tell a user their live service does not exist.
func TestEdgeRouteIsUnavailableBeforeTheIndexLoads(t *testing.T) {
	if rec := serveEdge(edgeRequest("boom.example", edgeToken)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", rec.Code)
	}
}

// A resolver failure that is neither of the typed outcomes is a 503, not a 404.
func TestEdgeRouteTreatsAnUnexpectedErrorAsUnavailable(t *testing.T) {
	if rec := serveEdge(edgeRequest("odd.example", edgeToken)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", rec.Code)
	}
}

// Only the edge may ask: a missing or wrong bearer is refused before any lookup.
func TestEdgeRouteRefusesACallerWithoutTheToken(t *testing.T) {
	for _, token := range []string{"", "wrong"} {
		rec := serveEdge(edgeRequest("app.prod-dev.getloopscale.app", token))
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-LoopRuntime-Cell") != "" {
			t.Errorf("token %q: status=%d cell=%q", token, rec.Code, rec.Header().Get("X-LoopRuntime-Cell"))
		}
	}
}

// Without the forwarded host there is nothing to look up.
func TestEdgeRouteRejectsARequestWithNoForwardedHost(t *testing.T) {
	if rec := serveEdge(edgeRequest("", edgeToken)); rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

// An empty configured token must never mean "anyone may ask".
func TestEdgeRouteHandlerIsNotBuiltWithoutAToken(t *testing.T) {
	if h := NewEdgeRouteHandler(fakeEdgeResolver{}, ""); h != nil {
		t.Error("a handler was built with an empty token")
	}
}

// An edge that issues certificates on demand asks with ?domain=<host>, and it reaches
// the API through a proxy that sets X-Forwarded-Host to the API's own name. The query
// names the host that is being asked about, so it wins.
func TestEdgeRouteReadsTheHostFromTheDomainQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/internal/edge/route?domain=app.prod-dev.getloopscale.app", nil)
	req.Header.Set("X-Forwarded-Host", "api.example.com")
	req.Header.Set("Authorization", "Bearer "+edgeToken)

	rec := serveEdge(req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-LoopRuntime-Cell") != "cell-1" {
		t.Errorf("status=%d cell=%q; want 200 and cell-1", rec.Code, rec.Header().Get("X-LoopRuntime-Cell"))
	}
}

func TestEdgeRouteRefusesADomainQueryNoRouteOwns(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/internal/edge/route?domain=unknown.example", nil)
	req.Header.Set("X-Forwarded-Host", "app.prod-dev.getloopscale.app")
	req.Header.Set("Authorization", "Bearer "+edgeToken)

	if rec := serveEdge(req); rec.Code != http.StatusNotFound {
		t.Errorf("status=%d; want 404: the query names the host, not the header", rec.Code)
	}
}
