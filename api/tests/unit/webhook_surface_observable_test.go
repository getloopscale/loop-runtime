// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/api/internal/server/surfaces"
)

// TestWebhookSurface_AusenciaDoSegredoEhObservavelSemLerLog closes the third DoD
// bullet of B-104: the absence of the webhook secret must be detectable WITHOUT
// reading the startup log.
//
// Measured on the droplet on 2026-08-14: with the secret absent,
// `POST /api/v1/webhooks/github` answered 404 while `POST /api/v1/health`
// answered 405 — and nothing else anywhere reported that the surface was off.
// In production that shape means a customer's `git push` builds nothing, silently.
func TestWebhookSurface_AusenciaDoSegredoEhObservavelSemLerLog(t *testing.T) {
	reg := surfaces.NewRegistry()
	_ = server.NewRouter(server.Config{Surfaces: reg, GitHubWebhookHandler: nil})
	metrics.PublishSurfaces(reg.All())

	if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues("github_webhook")); got != 0 {
		t.Errorf("com o segredo ausente a métrica vale %v, esperado 0 — a superfície "+
			"desligada tem de ser visível sem abrir o log", got)
	}

	off := reg.Disabled()
	var found bool
	for _, s := range off {
		if s.Name == "github_webhook" {
			found = true
			if s.Reason == "" {
				t.Error("a superfície está registrada como desligada sem razão — " +
					"quem vê o zero precisa saber o que provisionar")
			}
		}
	}
	if !found {
		t.Fatal("o webhook não aparece entre as superfícies desligadas")
	}
}

// TestWebhookSurface_LigadaQuandoOSegredoExiste is the other half. A metric that
// only ever reported zero would be as useless as the silence it replaced, and a
// metric that disagreed with the routing table would be worse — so the test pairs
// the gauge with the actual reachability of the route.
func TestWebhookSurface_LigadaQuandoOSegredoExiste(t *testing.T) {
	reg := surfaces.NewRegistry()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	router := server.NewRouter(server.Config{Surfaces: reg, GitHubWebhookHandler: handler})
	metrics.PublishSurfaces(reg.All())

	if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues("github_webhook")); got != 1 {
		t.Errorf("com o handler presente a métrica vale %v, esperado 1", got)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", nil))
	if rec.Code == http.StatusNotFound {
		t.Error("a rota do webhook devolveu 404 com o handler presente — a métrica " +
			"estaria mentindo sobre uma superfície que não existe")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("a requisição não alcançou o handler: código %d, esperado %d",
			rec.Code, http.StatusTeapot)
	}
}

// TestRouter_SemRegistroNaoEntraEmPanico protects the hundred-odd existing call
// sites that build a Config without a Registry. A nil registry means "nobody is
// observing", which is exactly what a focused unit test wants; forcing every one
// of them to wire a registry would be churn with no reader.
func TestRouter_SemRegistroNaoEntraEmPanico(t *testing.T) {
	router := server.NewRouter(server.Config{GitHubWebhookHandler: nil})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/health", nil))
	if rec.Code == 0 {
		t.Fatal("o roteador não respondeu")
	}
}
