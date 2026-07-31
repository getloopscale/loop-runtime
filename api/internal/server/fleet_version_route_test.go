// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Wiring: a rota existe no router REAL, nao apenas o handler isolado.
//
// O defeito que isto previne e' o classico "implementado mas nao ligado": os
// testes de handler em internal/routes passariam identicos com a rota nunca
// montada, e o painel /status mostraria o engine como `unreachable` — indistinguivel
// de servico fora do ar. Um handler sem caller nao esta' entregue.
func TestRouter_MountsFleetVersionRoute(t *testing.T) {
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/version = %d, want 200 (rota montada?)", rec.Code)
	}
	var got struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version == "" || got.BuiltAt == "" {
		t.Errorf("payload incompleto pelo router: version=%q built_at=%q", got.Version, got.BuiltAt)
	}
}

// A rota e' PUBLICA: o coletor sonda sem sessao. Se cair atras do requireJWT, o
// painel leria 401 como servico indisponivel.
func TestRouter_FleetVersionNeedsNoAuth(t *testing.T) {
	r := NewRouter(Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil) // sem Authorization
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("GET /v1/version exigiu auth (%d) — o coletor sonda sem sessao", rec.Code)
	}
}
