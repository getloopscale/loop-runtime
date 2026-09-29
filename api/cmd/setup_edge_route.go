// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

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

// newEdgeRoute builds the /internal/edge/route handler and starts its index refresh.
// It returns nil — and the route stays unregistered — without a token, without the
// cross-tenant pool that reads tenants.cell_id, or without the in-cluster API.
func newEdgeRoute(cfg config.Config, crossTenant *db.PgxAdapter, shutdownCtx context.Context,
	bgWg *sync.WaitGroup, logger *slog.Logger) http.Handler {
	if cfg.EdgeRouteToken == "" {
		logger.Warn("edge route lookup disabled: LOOPZERO_EDGE_ROUTE_TOKEN not set", "component", "edge-router")
		return nil
	}
	if crossTenant == nil {
		logger.Warn("edge route lookup disabled: no cross-tenant database pool", "component", "edge-router")
		return nil
	}
	source, err := placement.NewK8sEdgeRouteSource(nil, "loopzero-system")
	if err != nil {
		logger.Error("edge route lookup disabled: cannot reach the cluster", "component", "edge-router", "error", err)
		return nil
	}
	cells := placement.NewRoundRobinEngine(nil, &store.PostgresCellStore{DB: crossTenant}, cfg.MaxTenantsPerCell)
	handler, router := edgeRouteHandler(cfg, source, cells, logger)
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		router.Start(shutdownCtx)
	}()
	return handler
}

// edgeRouteHandler composes the resolver and its handler; nil, nil without a token.
func edgeRouteHandler(cfg config.Config, source placement.EdgeRouteSource, cells placement.TenantCellResolver,
	logger *slog.Logger) (http.Handler, *placement.EdgeRouter) {
	if cfg.EdgeRouteToken == "" {
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
	}, logger)
	return placementhttp.NewEdgeRouteHandler(router, cfg.EdgeRouteToken), router
}
