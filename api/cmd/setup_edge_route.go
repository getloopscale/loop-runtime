// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/builds/cfkv"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/placement/placementhttp"
)

const (
	edgeRouteRefreshInterval = 30 * time.Second
	edgeRouteMissRefreshGap  = 5 * time.Second
)

// newEdgeRoute builds the host → cell index and keeps it fresh. Two consumers read it:
// the /internal/edge/route lookup (an edge that asks, like dev-public's Caddy — needs
// LOOPZERO_EDGE_ROUTE_TOKEN) and the Cloudflare KV publisher (the production Worker, which
// reads — needs the CF KV credentials, #448). With neither, nothing is built. It also
// needs the cross-tenant pool that reads tenants.cell_id and the in-cluster API.
// Returns the lookup handler, or nil when the route must stay unregistered.
func newEdgeRoute(cfg config.Config, crossTenant *db.PgxAdapter, shutdownCtx context.Context,
	bgWg *sync.WaitGroup, logger *slog.Logger) http.Handler {
	var publisher placement.EdgeRoutePublisher
	if kv := cfkv.NewClient(cfg.CFAccountID, cfg.CFKVNamespaceID, cfg.CFAPIToken); kv != nil {
		publisher = placement.NewEdgeKVPublisher(kv, logger)
	}
	if cfg.EdgeRouteToken == "" && publisher == nil {
		logger.Warn("edge routing disabled: neither LOOPZERO_EDGE_ROUTE_TOKEN nor the Cloudflare KV credentials are set",
			"component", "edge-router")
		return nil
	}
	if crossTenant == nil {
		logger.Warn("edge routing disabled: no cross-tenant database pool", "component", "edge-router")
		return nil
	}
	source, err := placement.NewK8sEdgeRouteSource(nil, "loopzero-system")
	if err != nil {
		logger.Error("edge routing disabled: cannot reach the cluster", "component", "edge-router", "error", err)
		return nil
	}
	cells := placement.NewRoundRobinEngine(nil, &store.PostgresCellStore{DB: crossTenant}, cfg.MaxTenantsPerCell)
	handler, router := edgeRouteHandler(cfg, source, cells, publisher, logger)
	if router == nil {
		return nil
	}
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		router.Start(shutdownCtx)
	}()
	return handler
}

// edgeRouteHandler composes the index and, with a token, its lookup handler. The router
// is nil only when nothing would read it; the handler is nil without a token.
func edgeRouteHandler(cfg config.Config, source placement.EdgeRouteSource, cells placement.TenantCellResolver,
	publisher placement.EdgeRoutePublisher, logger *slog.Logger) (http.Handler, *placement.EdgeRouter) {
	if cfg.EdgeRouteToken == "" && publisher == nil {
		return nil, nil
	}
	var public []string
	for _, d := range []string{cfg.PublicDomain, cfg.PreviewPublicDomain, cfg.BaseDomain} {
		if d != "" {
			public = append(public, d)
		}
	}
	router := placement.NewEdgeRouter(source, cells, placement.EdgeRouterConfig{
		InternalDomain: cfg.BaseDomain,
		PublicDomains:  public,
		Interval:       edgeRouteRefreshInterval,
		MinMissRefresh: edgeRouteMissRefreshGap,
		Publisher:      publisher,
	}, logger)
	if cfg.EdgeRouteToken == "" {
		return nil, router // an untyped nil: a typed-nil handler would register the route
	}
	return placementhttp.NewEdgeRouteHandler(router, cfg.EdgeRouteToken), router
}
