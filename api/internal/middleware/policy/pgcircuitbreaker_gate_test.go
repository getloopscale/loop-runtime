// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- PG Circuit Breaker: sem portao configurado ---

// Era `ZeroTotal_PassesThrough`, e guardava a divisao por zero do calculo de percentual que este
// middleware fazia. Desde o B-054 quem mede saturacao e' o portao com histerese, e a degenerescencia
// que sobra aqui e' outra: nenhum portao configurado. A propriedade e' a mesma — estado degenerado
// nao pode barrar trafego — no lugar onde ela agora vive.
func TestPGCircuitBreaker_SemPortao_DeixaPassar(t *testing.T) {
	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := cb.Middleware()(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST with 0/0 pool: got %d, want 200", w.Code)
	}
}
