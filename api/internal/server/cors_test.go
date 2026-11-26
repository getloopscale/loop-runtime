// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A browser only sends a cross-origin request whose method the preflight
// advertised. Every method the router serves must therefore be advertised,
// or the dashboard's call fails before reaching the API.
func TestCORSPreflight_AdvertisesEveryMethodTheRouterServes(t *testing.T) {
	cfg := fullProtectedConfig()
	cfg.CORSAllowedOrigin = "https://cloud.example.test"
	h := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/teams/members/alice", nil)
	req.Header.Set("Origin", cfg.CORSAllowedOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	advertised := map[string]bool{}
	for _, m := range strings.Split(rec.Header().Get("Access-Control-Allow-Methods"), ",") {
		advertised[strings.TrimSpace(m)] = true
	}

	for rt := range routeSet(t, h) {
		method := strings.SplitN(rt, " ", 2)[0]
		if !advertised[method] {
			t.Errorf("router serves %s (%s) but the CORS preflight does not advertise it", method, rt)
		}
	}
}
