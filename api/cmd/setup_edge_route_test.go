// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/placement"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
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
	h, r := edgeRouteHandler(config.Config{BaseDomain: "loopruntime.local"}, staticEdgeRoutes{}, staticTenantCells{}, nil, quietLogger())
	if h != nil || r != nil {
		t.Errorf("wired without a token: handler=%v router=%v", h, r)
	}
}

// The wired route resolves a public host under the configured public domain to the
// cell of the tenant that published it — the whole chain the edge depends on.
func TestEdgeRouteResolvesUnderTheConfiguredPublicDomain(t *testing.T) {
	cfg := config.Config{
		BaseDomain:          "loopruntime.local",
		PublicDomain:        "prod-dev.getloopscale.app",
		PreviewPublicDomain: "preview-dev.getloopscale.app",
		EdgeRouteToken:      "tok",
	}
	h, r := edgeRouteHandler(cfg,
		staticEdgeRoutes{{Hostname: "tn-a-p-web-production.loopruntime.local", TenantID: "tn-a"}},
		staticTenantCells{"tn-a": "cell-1"}, nil, quietLogger())
	if h == nil || r == nil {
		t.Fatal("not wired with a token")
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/edge/route", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-Forwarded-Host", "tn-a-p-web-production.prod-dev.getloopscale.app")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-LoopRuntime-Cell") != "cell-1" {
		t.Errorf("status=%d cell=%q", rec.Code, rec.Header().Get("X-LoopRuntime-Cell"))
	}
}

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, map[string]string, bool) error { return nil }

// Production has no edge token — its edge is the Cloudflare Worker — but it needs the
// router to publish host → cell into the Worker's store (#448). The router runs for the
// publisher alone, and the lookup route stays unregistered without a token.
func TestEdgeRouterRunsForThePublisherWithoutServingTheRoute(t *testing.T) {
	h, r := edgeRouteHandler(config.Config{BaseDomain: "loopruntime.local"}, staticEdgeRoutes{}, staticTenantCells{},
		nopPublisher{}, quietLogger())
	if r == nil {
		t.Fatal("no router with a publisher configured")
	}
	if h != nil {
		t.Error("the lookup route was served without a token")
	}
}

// On AWS the edge reads the routes from a CloudFront KeyValueStore: with its ARN set, the
// router gets a publisher even without the Cloudflare credentials.
func TestTheEdgeKeyValueStoreGetsAPublisher(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	cfg := config.Config{EdgeKVSARN: "arn:aws:cloudfront::000000000000:key-value-store/2b5a3c1e-0000-4000-8000-000000000001"}
	if edgeRoutePublisher(context.Background(), cfg, quietLogger()) == nil {
		t.Fatal("no publisher for a configured key value store")
	}
}

func TestNoEdgeStoreMeansNoPublisher(t *testing.T) {
	if p := edgeRoutePublisher(context.Background(), config.Config{}, quietLogger()); p != nil {
		t.Fatalf("publisher %v with no edge store configured", p)
	}
}

type staticDomainHosts []ports.DomainHost

func (s staticDomainHosts) ListActiveHosts(context.Context) ([]ports.DomainHost, error) {
	return s, nil
}

// An active custom domain is a route of its tenant: the edge resolves the host to
// the tenant's cell, and an edge that issues certificates on demand may ask for it.
func TestEdgeRouteResolvesAnActiveCustomDomain(t *testing.T) {
	cfg := config.Config{BaseDomain: "loopruntime.local", PublicDomain: "prod-dev.getloopscale.app", EdgeRouteToken: "tok"}
	source := placement.EdgeRouteSources{
		staticEdgeRoutes{{Hostname: "tn-a-p-web-production.loopruntime.local", TenantID: "tn-a"}},
		customDomainRoutes{hosts: staticDomainHosts{{Domain: "shop.customer.com", TenantID: "tn-a"}}},
	}
	h, r := edgeRouteHandler(cfg, source, staticTenantCells{"tn-a": "cell-1"}, nil, quietLogger())
	if h == nil || r == nil {
		t.Fatal("not wired with a token")
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/edge/route?domain=shop.customer.com", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-LoopRuntime-Cell") != "cell-1" {
		t.Errorf("status=%d cell=%q; want 200 and cell-1", rec.Code, rec.Header().Get("X-LoopRuntime-Cell"))
	}
}
