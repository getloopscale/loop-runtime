// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeEdgeRoutes struct {
	routes []EdgeRoute
	err    error
	calls  atomic.Int32
}

func (f *fakeEdgeRoutes) ListEdgeRoutes(context.Context) ([]EdgeRoute, error) {
	f.calls.Add(1)
	return f.routes, f.err
}

type fakeTenantCells map[string]string

func (f fakeTenantCells) GetCell(_ context.Context, tenantID string) (string, error) {
	if c, ok := f[tenantID]; ok {
		return c, nil
	}
	return "", errors.New("no such tenant")
}

const (
	internalDomain = "loopzero.local"
	prodDomain     = "prod-dev.loopzero.example"
	previewDomain  = "preview-dev.loopzero.example"
)

func newTestEdgeRouter(src *fakeEdgeRoutes, cells fakeTenantCells) *EdgeRouter {
	return NewEdgeRouter(src, cells, EdgeRouterConfig{
		InternalDomain: internalDomain,
		PublicDomains:  []string{prodDomain, previewDomain},
		Interval:       time.Minute,
		MinMissRefresh: time.Hour, // a test that wants a miss refresh sets its own
	}, nil)
}

// The public host a user reaches is resolved to the cell that runs the app: the
// route's first label is unique (tenant-project-app-env) and the tenant's placement
// is tenants.cell_id. This is the lookup the edge makes before forwarding (#447).
func TestEdgeRouterResolvesAPublicHostToTheTenantsCell(t *testing.T) {
	src := &fakeEdgeRoutes{routes: []EdgeRoute{
		{Hostname: "tn-admin-sample-app-production.loopzero.local", TenantID: "tn-admin"},
		{Hostname: "tn-e2e-web-api-production.loopzero.local", TenantID: "tn-e2e"},
	}}
	r := newTestEdgeRouter(src, fakeTenantCells{"tn-admin": "cell-1", "tn-e2e": "cell-0"})

	for host, want := range map[string]string{
		"tn-admin-sample-app-production.prod-dev.loopzero.example":    "cell-1",
		"TN-E2E-web-api-production.prod-dev.loopzero.example:443":     "cell-0",
		"tn-admin-sample-app-production.preview-dev.loopzero.example": "cell-1",
	} {
		got, err := r.Resolve(context.Background(), host)
		if err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", host, got, err, want)
		}
	}
}

// A custom domain is indexed by its full name, so its first label ("www") can never
// be mistaken for an app whose label happens to be "www".
func TestEdgeRouterResolvesACustomDomainByItsFullName(t *testing.T) {
	src := &fakeEdgeRoutes{routes: []EdgeRoute{
		{Hostname: "www.customer.example", TenantID: "tn-cust"},
		{Hostname: "www.loopzero.local", TenantID: "tn-other"},
	}}
	r := newTestEdgeRouter(src, fakeTenantCells{"tn-cust": "cell-1", "tn-other": "cell-0"})

	if got, err := r.Resolve(context.Background(), "www.customer.example"); err != nil || got != "cell-1" {
		t.Errorf("custom domain resolved to %q, %v; want cell-1", got, err)
	}
	if got, err := r.Resolve(context.Background(), "www.prod-dev.loopzero.example"); err != nil || got != "cell-0" {
		t.Errorf("platform host resolved to %q, %v; want cell-0", got, err)
	}
}

// A host nobody publishes is not routed anywhere.
func TestEdgeRouterRefusesAnUnknownHost(t *testing.T) {
	r := newTestEdgeRouter(&fakeEdgeRoutes{}, fakeTenantCells{})
	for _, host := range []string{"nobody.prod-dev.loopzero.example", "evil.example", "", "prod-dev.loopzero.example"} {
		if _, err := r.Resolve(context.Background(), host); !errors.Is(err, ErrEdgeRouteNotFound) {
			t.Errorf("Resolve(%q) err = %v, want ErrEdgeRouteNotFound", host, err)
		}
	}
}

// Two tenants publishing the same label must not send one tenant's traffic to the
// other's cell: the label is refused rather than guessed.
func TestEdgeRouterRefusesALabelTwoTenantsPublish(t *testing.T) {
	src := &fakeEdgeRoutes{routes: []EdgeRoute{
		{Hostname: "dup-app-production.loopzero.local", TenantID: "tn-a"},
		{Hostname: "dup-app-production.loopzero.local", TenantID: "tn-b"},
	}}
	r := newTestEdgeRouter(src, fakeTenantCells{"tn-a": "cell-0", "tn-b": "cell-1"})
	if _, err := r.Resolve(context.Background(), "dup-app-production.prod-dev.loopzero.example"); !errors.Is(err, ErrEdgeRouteNotFound) {
		t.Errorf("an ambiguous label resolved (err=%v) — it must be refused", err)
	}
}

// A route published after the last refresh is found by refreshing on the miss, and a
// burst of unknown hosts does not refresh more often than the configured gap.
func TestEdgeRouterRefreshesOnAMissAtMostOncePerGap(t *testing.T) {
	src := &fakeEdgeRoutes{}
	r := NewEdgeRouter(src, fakeTenantCells{"tn-new": "cell-1"}, EdgeRouterConfig{
		InternalDomain: internalDomain, PublicDomains: []string{prodDomain},
		Interval: time.Minute, MinMissRefresh: time.Hour,
	}, nil)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	src.routes = []EdgeRoute{{Hostname: "tn-new-p-app-production.loopzero.local", TenantID: "tn-new"}}

	// The gap has not elapsed since the refresh above: a miss does not refresh yet.
	if _, err := r.Resolve(context.Background(), "tn-new-p-app-production.prod-dev.loopzero.example"); !errors.Is(err, ErrEdgeRouteNotFound) {
		t.Fatalf("resolved before any refresh saw the route: %v", err)
	}
	callsBefore := src.calls.Load()

	r.minMissRefresh = 0
	got, err := r.Resolve(context.Background(), "tn-new-p-app-production.prod-dev.loopzero.example")
	if err != nil || got != "cell-1" {
		t.Fatalf("after the gap, the miss refresh should find the new route: %q, %v", got, err)
	}
	if src.calls.Load() != callsBefore+1 {
		t.Errorf("the miss refreshed %d times, want once", src.calls.Load()-callsBefore)
	}
}

// Before the first successful load the router says so, rather than answering
// "not found" for a host that may well exist.
func TestEdgeRouterReportsNotLoadedWhenTheSourceNeverAnswered(t *testing.T) {
	r := newTestEdgeRouter(&fakeEdgeRoutes{err: errors.New("apiserver down")}, fakeTenantCells{})
	r.minMissRefresh = 0
	if _, err := r.Resolve(context.Background(), "x.prod-dev.loopzero.example"); !errors.Is(err, ErrEdgeRoutesNotLoaded) {
		t.Errorf("err = %v, want ErrEdgeRoutesNotLoaded", err)
	}
}

// A tenant whose placement cannot be read is left out, and the other tenants still
// route: one bad row does not take the edge down.
func TestEdgeRouterSkipsATenantWhoseCellCannotBeRead(t *testing.T) {
	src := &fakeEdgeRoutes{routes: []EdgeRoute{
		{Hostname: "tn-gone-p-app-production.loopzero.local", TenantID: "tn-gone"},
		{Hostname: "tn-ok-p-app-production.loopzero.local", TenantID: "tn-ok"},
	}}
	r := newTestEdgeRouter(src, fakeTenantCells{"tn-ok": "cell-0"})
	if got, err := r.Resolve(context.Background(), "tn-ok-p-app-production.prod-dev.loopzero.example"); err != nil || got != "cell-0" {
		t.Errorf("tn-ok resolved to %q, %v", got, err)
	}
	if _, err := r.Resolve(context.Background(), "tn-gone-p-app-production.prod-dev.loopzero.example"); !errors.Is(err, ErrEdgeRouteNotFound) {
		t.Errorf("a tenant with no readable cell must not resolve, err=%v", err)
	}
}

type recordingPublisher struct {
	index    map[string]string
	complete bool
	calls    int
}

func (r *recordingPublisher) Publish(_ context.Context, index map[string]string, complete bool) error {
	r.index, r.complete = index, complete
	r.calls++
	return nil
}

// Each refresh hands the fresh index to the edge publisher, and says whether it is
// complete: a tenant whose placement could not be read makes the round incomplete, so
// the publisher keeps that tenant's routes instead of deleting them (#448).
func TestEdgeRouterPublishesEachRefreshAndFlagsAnIncompleteOne(t *testing.T) {
	src := &fakeEdgeRoutes{routes: []EdgeRoute{
		{Hostname: "tn-ok-p-app-production.loopzero.local", TenantID: "tn-ok"},
		{Hostname: "tn-gone-p-app-production.loopzero.local", TenantID: "tn-gone"},
	}}
	pub := &recordingPublisher{}
	r := NewEdgeRouter(src, fakeTenantCells{"tn-ok": "cell-0"}, EdgeRouterConfig{
		InternalDomain: internalDomain, PublicDomains: []string{prodDomain},
		Interval: time.Minute, MinMissRefresh: time.Hour, Publisher: pub,
	}, nil)

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pub.calls != 1 || pub.index["label:tn-ok-p-app-production"] != "cell-0" {
		t.Errorf("publisher got %v after %d calls", pub.index, pub.calls)
	}
	if pub.complete {
		t.Error("a round with an unreadable tenant was reported complete")
	}

	src.routes = src.routes[:1]
	_ = r.Refresh(context.Background())
	if !pub.complete {
		t.Error("a round where every tenant resolved was reported incomplete")
	}
}
