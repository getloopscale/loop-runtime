// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placementhttp

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/placement"
)

// EdgeResolver answers which cell serves a public host.
type EdgeResolver interface {
	Resolve(ctx context.Context, host string) (string, error)
}

// EdgeRouteHandler serves GET /internal/edge/route: the edge's subrequest before it
// forwards a user's request, or before it issues a certificate for a host. It reads the
// host from the domain query, else from X-Forwarded-Host, and answers with
// the cell in X-LoopZero-Cell (200), or refuses (404 unknown host, 503 index unavailable).
// A non-2xx answer is relayed to the user by the edge, so its body is product copy.
type EdgeRouteHandler struct {
	resolver EdgeResolver
	token    []byte
}

// NewEdgeRouteHandler returns nil when token is empty: an unauthenticated routing
// oracle is never served (the route is then not registered).
func NewEdgeRouteHandler(resolver EdgeResolver, token string) *EdgeRouteHandler {
	if token == "" || resolver == nil {
		return nil
	}
	return &EdgeRouteHandler{resolver: resolver, token: []byte(token)}
}

func (h *EdgeRouteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) ||
		subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), h.token) != 1 {
		metrics.EdgeRouteLookups.WithLabelValues("unauthorized").Inc()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// An edge that issues certificates on demand asks with ?domain=<host> (Caddy's
	// `ask`), and may reach this API through a proxy that sets X-Forwarded-Host to the
	// API's own name. The query names the host being asked about, so it wins.
	host := r.URL.Query().Get("domain")
	if host == "" {
		host = r.Header.Get("X-Forwarded-Host")
	}
	if host == "" {
		metrics.EdgeRouteLookups.WithLabelValues("bad_request").Inc()
		http.Error(w, "missing host: send ?domain= or X-Forwarded-Host", http.StatusBadRequest)
		return
	}
	cell, err := h.resolver.Resolve(r.Context(), host)
	switch {
	case err == nil:
		metrics.EdgeRouteLookups.WithLabelValues("routed").Inc()
		w.Header().Set("X-LoopZero-Cell", cell)
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, placement.ErrEdgeRouteNotFound):
		metrics.EdgeRouteLookups.WithLabelValues("not_found").Inc()
		http.Error(w, "No service is deployed at this address.", http.StatusNotFound)
	default:
		metrics.EdgeRouteLookups.WithLabelValues("unavailable").Inc()
		http.Error(w, "This service is temporarily unavailable. Try again shortly.", http.StatusServiceUnavailable)
	}
}
