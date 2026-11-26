// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// ErrEdgeRouteNotFound means no published route owns the host: the edge must not
// forward it anywhere.
var ErrEdgeRouteNotFound = errors.New("placement: no route publishes this host")

// ErrEdgeRoutesNotLoaded means the route index has never loaded, so an unknown host
// cannot be told apart from a host that exists. The edge should answer 503, not 404.
var ErrEdgeRoutesNotLoaded = errors.New("placement: edge route index not loaded")

// EdgeRoute is one published hostname and the tenant that owns it.
type EdgeRoute struct {
	Hostname string
	TenantID string
}

// EdgeRouteSource lists every published route across all cells.
type EdgeRouteSource interface {
	ListEdgeRoutes(ctx context.Context) ([]EdgeRoute, error)
}

// EdgeRouteSources lists the routes of several sources as one. A source that fails
// fails the listing: the router keeps the index it had, where a partial listing
// would drop every host of the source that could not be read.
type EdgeRouteSources []EdgeRouteSource

// ListEdgeRoutes returns the routes of every source, in order.
func (s EdgeRouteSources) ListEdgeRoutes(ctx context.Context) ([]EdgeRoute, error) {
	var all []EdgeRoute
	for _, source := range s {
		routes, err := source.ListEdgeRoutes(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, routes...)
	}
	return all, nil
}

// TenantCellResolver returns the cell a tenant was placed on (tenants.cell_id).
type TenantCellResolver interface {
	GetCell(ctx context.Context, tenantID string) (string, error)
}

// EdgeRouterConfig says how hostnames map between the edge and the cells.
type EdgeRouterConfig struct {
	// InternalDomain is the suffix routes are published under inside a cell
	// (loopzero.local on dev-public). A route under it is indexed by its first label.
	InternalDomain string
	// PublicDomains are the suffixes users reach; the label in front of one is the
	// same label the route was published with.
	PublicDomains []string
	// Interval is the background refresh period.
	Interval time.Duration
	// MinMissRefresh bounds how often an unknown host may trigger a refresh, so a
	// scan of random names cannot turn into a list call per request.
	MinMissRefresh time.Duration
	// Publisher, when set, receives every refreshed index — the production edge store
	// the Cloudflare Worker reads (#448). Nil publishes nothing.
	Publisher EdgeRoutePublisher
}

// EdgeRoutePublisher mirrors the index somewhere an edge can read it. complete is false
// when some tenant's placement could not be read, so its routes must not be removed.
type EdgeRoutePublisher interface {
	Publish(ctx context.Context, index map[string]string, complete bool) error
}

// EdgeRouter answers which cell serves a public host. Placement is decided once and
// stored in tenants.cell_id; this is the lookup that lets the edge honour it without
// knowing anything about tenants (ADR-0008: placement once, route many).
//
// Thread-safe: Resolve reads a snapshot; refreshes are serialised.
type EdgeRouter struct {
	source         EdgeRouteSource
	cells          TenantCellResolver
	internalSuffix string
	publicSuffixes []string
	interval       time.Duration
	minMissRefresh time.Duration
	publisher      EdgeRoutePublisher
	logger         *slog.Logger

	refreshMu   sync.Mutex
	lastAttempt time.Time

	mu     sync.RWMutex
	index  map[string]string // key → cell id
	loaded bool
}

// NewEdgeRouter builds a router. Call Start to keep it fresh, or Refresh to load once.
func NewEdgeRouter(source EdgeRouteSource, cells TenantCellResolver, cfg EdgeRouterConfig, logger *slog.Logger) *EdgeRouter {
	if logger == nil {
		logger = slog.Default()
	}
	public := make([]string, 0, len(cfg.PublicDomains))
	for _, d := range cfg.PublicDomains {
		if d = normalizeHost(d); d != "" {
			public = append(public, "."+d)
		}
	}
	return &EdgeRouter{
		source:         source,
		cells:          cells,
		internalSuffix: "." + normalizeHost(cfg.InternalDomain),
		publicSuffixes: public,
		interval:       cfg.Interval,
		minMissRefresh: cfg.MinMissRefresh,
		publisher:      cfg.Publisher,
		logger:         logger.With("component", "edge-router"),
		index:          map[string]string{},
	}
}

// Start refreshes immediately and then every Interval until ctx ends.
func (r *EdgeRouter) Start(ctx context.Context) {
	if err := r.Refresh(ctx); err != nil {
		r.logger.Error("initial edge route refresh failed", "error", err)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Refresh(ctx); err != nil {
				r.logger.Error("edge route refresh failed", "error", err)
			}
		}
	}
}

// Resolve returns the cell id serving host, refreshing once on a miss when the last
// refresh is older than MinMissRefresh.
func (r *EdgeRouter) Resolve(ctx context.Context, host string) (string, error) {
	key := r.keyForRequestHost(host)
	if cell, ok := r.lookup(key); ok {
		return cell, nil
	}
	r.refreshOnMiss(ctx)
	if cell, ok := r.lookup(key); ok {
		return cell, nil
	}
	r.mu.RLock()
	loaded := r.loaded
	r.mu.RUnlock()
	if !loaded {
		return "", ErrEdgeRoutesNotLoaded
	}
	return "", ErrEdgeRouteNotFound
}

// Refresh rebuilds the index from the route source and the tenants' placement.
func (r *EdgeRouter) Refresh(ctx context.Context) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	return r.refreshLocked(ctx)
}

func (r *EdgeRouter) refreshOnMiss(ctx context.Context) {
	if !r.refreshMu.TryLock() {
		return // a refresh is already running; its result serves the next request
	}
	defer r.refreshMu.Unlock()
	if time.Since(r.lastAttempt) < r.minMissRefresh {
		return
	}
	if err := r.refreshLocked(ctx); err != nil {
		r.logger.Warn("edge route refresh on miss failed", "error", err)
	}
}

func (r *EdgeRouter) refreshLocked(ctx context.Context) error {
	r.lastAttempt = time.Now()
	routes, err := r.source.ListEdgeRoutes(ctx)
	if err != nil {
		return fmt.Errorf("list edge routes: %w", err)
	}

	owners := map[string]string{} // key → tenant
	ambiguous := map[string]bool{}
	for _, rt := range routes {
		key := r.keyForRoute(rt.Hostname)
		if key == "" || rt.TenantID == "" {
			continue
		}
		if prev, ok := owners[key]; ok && prev != rt.TenantID {
			ambiguous[key] = true
			continue
		}
		owners[key] = rt.TenantID
	}

	tenantCell := map[string]string{}
	index := make(map[string]string, len(owners))
	skipped := 0
	for key, tenant := range owners {
		if ambiguous[key] {
			r.logger.Warn("edge route label published by more than one tenant; not routed", "key", key)
			continue
		}
		cell, ok := tenantCell[tenant]
		if !ok {
			c, err := r.cells.GetCell(ctx, tenant)
			if err != nil || c == "" {
				r.logger.Warn("tenant placement unreadable; its routes are not routed", "tenant_id", tenant, "error", err)
				tenantCell[tenant] = ""
				skipped++
				continue
			}
			cell = c
			tenantCell[tenant] = c
		}
		if cell == "" {
			continue
		}
		index[key] = cell
	}

	r.mu.Lock()
	r.index = index
	r.loaded = true
	r.mu.Unlock()
	r.logger.Debug("edge routes refreshed", "routes", len(index), "ambiguous", len(ambiguous), "tenants_unplaced", skipped)
	if r.publisher != nil {
		snapshot := make(map[string]string, len(index))
		for k, v := range index {
			snapshot[k] = v
		}
		if err := r.publisher.Publish(ctx, snapshot, skipped == 0); err != nil {
			r.logger.Warn("edge route publish failed; retried on the next refresh", "error", err)
		}
	}
	return nil
}

func (r *EdgeRouter) lookup(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	cell, ok := r.index[key]
	return cell, ok
}

// keyForRoute indexes a platform route by its label and a custom domain by its full
// name, so the two namespaces can never collide.
func (r *EdgeRouter) keyForRoute(hostname string) string {
	h := normalizeHost(hostname)
	if label, ok := labelUnder(h, r.internalSuffix); ok {
		return "label:" + label
	}
	if h == "" {
		return ""
	}
	return "host:" + h
}

func (r *EdgeRouter) keyForRequestHost(host string) string {
	h := normalizeHost(host)
	for _, suffix := range r.publicSuffixes {
		if label, ok := labelUnder(h, suffix); ok {
			return "label:" + label
		}
	}
	if h == "" {
		return ""
	}
	return "host:" + h
}

// labelUnder returns the single DNS label in front of suffix ("a.b.c", ".b.c" → "a").
func labelUnder(host, suffix string) (string, bool) {
	if suffix == "." || !strings.HasSuffix(host, suffix) {
		return "", false
	}
	label := strings.TrimSuffix(host, suffix)
	if label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}

// normalizeHost lower-cases a host and drops a port and a trailing dot.
func normalizeHost(host string) string {
	h := strings.TrimSpace(host)
	if hp, _, err := net.SplitHostPort(h); err == nil {
		h = hp
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}
