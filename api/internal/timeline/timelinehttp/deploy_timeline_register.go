// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package timelinehttp

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// TenantResolver pulls the tenant ID from the request context, matching
// the shape of api/internal/middleware.Auth. Injected so the timeline
// handler stays decoupled from middleware internals.
type TenantResolver func(ctx context.Context) (string, bool)

// RegisterDeployTimelineRoute mounts GET /api/v1/deploys/{deployID}/timeline
// on r. Production wires it inside the authenticated route group with
// the canonical TenantResolver; tests pass a stub.
//
// Plan T5.1: this helper is the canonical mount point — server.go calls
// it once during route registration. Direct chi.Get inline at the
// call-site is an anti-pattern (no test coverage of the wiring).
func RegisterDeployTimelineRoute(r chi.Router, store DeployTimelineEventStore, resolver TenantResolver, opts ...DTOption) {
	handler := NewDeployTimelineHandler(store, opts...)
	r.Get("/api/v1/deploys/{deployID}/timeline", func(w http.ResponseWriter, req *http.Request) {
		tenantID, ok := resolver(req.Context())
		if !ok || tenantID == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(req.Context(), deployTimelineTenantCtxKey, tenantID)
		handler.Stream(w, req.WithContext(ctx))
	})
}
