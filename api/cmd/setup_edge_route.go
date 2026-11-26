// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds/cfkv"
	"github.com/getloopscale/loop-runtime/api/internal/builds/cloudfrontkvs"
	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/placement"
	"github.com/getloopscale/loop-runtime/api/internal/placement/placementhttp"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

const (
	edgeRouteRefreshInterval = 30 * time.Second
	edgeRouteMissRefreshGap  = 5 * time.Second
)

// newEdgeRoute builds the host → cell index and keeps it fresh. Two consumers read it:
// the /internal/edge/route lookup (an edge that asks, like dev-public's Caddy; needs
// LOOPRUNTIME_EDGE_ROUTE_TOKEN) and the publisher to the store an edge reads (the CloudFront
// KeyValueStore on AWS, or the Cloudflare KV of the Worker, #448). With neither, nothing is built. It also
// needs the cross-tenant pool that reads tenants.cell_id and the in-cluster API.
// Returns the lookup handler, or nil when the route must stay unregistered.
func newEdgeRoute(cfg config.Config, crossTenant *db.PgxAdapter, shutdownCtx context.Context,
	bgWg *sync.WaitGroup, logger *slog.Logger) http.Handler {
	publisher := edgeRoutePublisher(shutdownCtx, cfg, logger)
	if cfg.EdgeRouteToken == "" && publisher == nil {
		logger.Warn("edge routing disabled: none of LOOPRUNTIME_EDGE_ROUTE_TOKEN, LOOPRUNTIME_EDGE_KVS_ARN or the Cloudflare KV credentials is set",
			"component", "edge-router")
		return nil
	}
	if crossTenant == nil {
		logger.Warn("edge routing disabled: no cross-tenant database pool", "component", "edge-router")
		return nil
	}
	source, err := placement.NewK8sEdgeRouteSource(nil, "loopruntime-system")
	if err != nil {
		logger.Error("edge routing disabled: cannot reach the cluster", "component", "edge-router", "error", err)
		return nil
	}
	cells := placement.NewRoundRobinEngine(nil, &store.PostgresCellStore{DB: crossTenant}, cfg.MaxTenantsPerCell)
	routes := placement.EdgeRouteSources{source, customDomainRoutes{hosts: &store.PostgresDomainStore{DB: crossTenant}}}
	handler, router := edgeRouteHandler(cfg, routes, cells, publisher, logger)
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

// activeDomainHosts lists the active custom domains of every tenant.
type activeDomainHosts interface {
	ListActiveHosts(ctx context.Context) ([]ports.DomainHost, error)
}

// customDomainRoutes is the route source for custom domains. The app operator
// publishes one ExposedRoute per application, under the platform host, so a custom
// domain reaches the index only from the domain store.
type customDomainRoutes struct {
	hosts activeDomainHosts
}

func (c customDomainRoutes) ListEdgeRoutes(ctx context.Context) ([]placement.EdgeRoute, error) {
	hosts, err := c.hosts.ListActiveHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("custom domain routes: %w", err)
	}
	routes := make([]placement.EdgeRoute, 0, len(hosts))
	for _, h := range hosts {
		routes = append(routes, placement.EdgeRoute{Hostname: h.Domain, TenantID: h.TenantID})
	}
	return routes, nil
}

// edgeRoutePublisher returns the store the edge reads the routes from: the CloudFront
// KeyValueStore on AWS (LOOPRUNTIME_EDGE_KVS_ARN), else the Cloudflare KV, else nil. A store
// that is configured but cannot be reached is logged as an error and publishes nothing.
func edgeRoutePublisher(ctx context.Context, cfg config.Config, logger *slog.Logger) placement.EdgeRoutePublisher {
	if cfg.EdgeKVSARN != "" {
		kv, err := cloudfrontkvs.NewFromEnvironment(ctx, cfg.EdgeKVSARN)
		if err != nil {
			logger.Error("edge routes are not published: the key value store client failed",
				"component", "edge-router", "kvs_arn", cfg.EdgeKVSARN, "error", err)
			return nil
		}
		return placement.NewEdgeKVPublisher(kv, logger)
	}
	if kv := cfkv.NewClient(cfg.CFAccountID, cfg.CFKVNamespaceID, cfg.CFAPIToken); kv != nil {
		return placement.NewEdgeKVPublisher(kv, logger)
	}
	return nil
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
