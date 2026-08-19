// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// PoolGate decide se uma mutacao pode prosseguir dado o estado do pool.
//
// A interface e' declarada AQUI, pelo consumidor, e nao no pacote que a implementa
// (`rules/architecture.md` § DIP): o middleware nao importa `buildplane/admission`, e o
// breaker de la' a satisfaz sem saber que ela existe.
type PoolGate interface {
	// Allow devolve false quando o portao esta' disparado.
	Allow() bool
}

// PGCircuitBreakerConfig controls the PG pool circuit breaker.
type PGCircuitBreakerConfig struct {
	// Gate decide. Ate' 2026-08-19 este middleware calculava um percentual instantaneo aqui
	// mesmo, enquanto o portao COM histerese (`buildplane/admission.PGPoolBreaker`) era
	// construido, iniciado, parado no shutdown — e nunca consultado (B-054).
	//
	// O efeito era o pior dos dois: o instrumento que resolveu a oscilacao estava desligado, e
	// o que decidia oscilava. Perto da linha de corte o pool sobe e desce a cada conexao
	// devolvida, e o cliente via 503 alternando com 200.
	//
	// Nil desabilita o portao — e' o que mantem valido o caminho em que a stack de admissao
	// nao esta' montada.
	Gate PoolGate
}

// PGCircuitBreaker rejects mutation HTTP requests when the PostgreSQL
// connection pool is above the configured saturation threshold. Read
// requests (GET, HEAD, OPTIONS) are always allowed.
type PGCircuitBreaker struct {
	cfg PGCircuitBreakerConfig
}

// NewPGCircuitBreaker creates a circuit breaker with the given config.
func NewPGCircuitBreaker(cfg PGCircuitBreakerConfig) *PGCircuitBreaker {
	return &PGCircuitBreaker{cfg: cfg}
}

// Middleware returns an HTTP middleware that applies the circuit breaker.
func (cb *PGCircuitBreaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Reads always pass.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if cb.cfg.Gate != nil && !cb.cfg.Gate.Allow() {
				metrics.PGCircuitBreakerTripped.WithLabelValues(r.Method).Inc()
				w.Header().Set("Retry-After", "5")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":       "service temporarily unavailable — database pool at capacity",
					"retry_after": 5,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
