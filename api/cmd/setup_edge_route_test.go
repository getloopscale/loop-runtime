// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/placement"
)

type staticEdgeRoutes []placement.EdgeRoute

func (s staticEdgeRoutes) ListEdgeRoutes(context.Context) ([]placement.EdgeRoute, error) {
	return s, nil
}

type staticTenantCells map[string]string

func (s staticTenantCells) GetCell(_ context.Context, tenant string) (string, error) {
	if c, ok := s[tenant]; ok {
		return c, nil
	}
	return "", errors.New("unplaced")
}


// No token, no route: the lookup is never served unauthenticated.
func TestEdgeRouteIsNotWiredWithoutAToken(t *testing.T) {
	h, r := edgeRouteHandler(config.Config{BaseDomain: "loopzero.local"}, staticEdgeRoutes{}, staticTenantCells{}, quietLogger())
	if h != nil || r != nil {
		t.Errorf("wired without a token: handler=%v router=%v", h, r)
	}
}

// The wired route resolves a public host under the configured public domain to the
// cell of the tenant that published it — the whole chain the edge depends on.
func TestEdgeRouteResolvesUnderTheConfiguredPublicDomain(t *testing.T) {
	cfg := config.Config{
		BaseDomain:          "loopzero.local",
		PublicDomain:        "prod-dev.loopzero.example",
		PreviewPublicDomain: "preview-dev.loopzero.example",
		EdgeRouteToken:      "tok",
	}
	h, r := edgeRouteHandler(cfg,
		staticEdgeRoutes{{Hostname: "tn-a-p-web-production.loopzero.local", TenantID: "tn-a"}},
		staticTenantCells{"tn-a": "cell-1"}, quietLogger())
	if h == nil || r == nil {
		t.Fatal("not wired with a token")
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/edge/route", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-Forwarded-Host", "tn-a-p-web-production.prod-dev.loopzero.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-LoopZero-Cell") != "cell-1" {
		t.Errorf("status=%d cell=%q", rec.Code, rec.Header().Get("X-LoopZero-Cell"))
	}
}
