// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loop-runtime/api/internal/preview/previewhttp"
	"github.com/getloopscale/loop-runtime/api/internal/server"
)

// Each request must consume exactly ONE rate-limit token. The idempotent
// middleware chain already contains the rate limiter, so a route that
// prepends the rate-limit chain to it again spends two tokens per request:
// with a budget of one request, the very first call is refused.
func TestRouter_MutatingRoutesSpendOneRateLimitTokenPerRequest(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	for _, path := range []string{"/api/v1/domains", "/api/v1/projects/proj-x/previews"} {
		t.Run(path, func(t *testing.T) {
			cfg := t61BaseCfg(logger)
			cfg.Guards.RateLimiter = ratelimit.NewRateLimiter(1, time.Hour)
			cfg.PreviewHandler = previewhttp.NewPreviewHandler(nil, nil, logger)
			h := server.NewRouter(cfg)

			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Idempotency-Key", "11111111-1111-4111-8111-111111111111")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("first request under a budget of one was rate limited: POST %s spent two tokens", path)
			}
		})
	}
}
