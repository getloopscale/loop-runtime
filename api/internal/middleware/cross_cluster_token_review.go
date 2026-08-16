// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

// cross_cluster_token_review.go — T4.2 of plan 2026-06-02-openbao-adoption v1.3.
//
// Middleware that validates the Bearer token from ESO running in a cell
// runtime cluster against THAT cluster's K8s API via TokenReview.
//
// Flow:
//  1. Extract `Authorization: Bearer <token>` header.
//  2. Extract `X-Cell-ID` header (which cell the request claims to come from).
//  3. Look up the per-cell K8s client (kubeconfig from ClusterCredential per
//     cellmgr/client_manager.go pattern).
//  4. Submit TokenReview to that cluster's API.
//  5. On success: extract user info + cache for short TTL (default 60s).
//  6. Inject reviewed identity into request context for handlers.
//
// EC-18 (SHOULD TEST): when the per-cell client's underlying kubeconfig
// Secret is rotated (H13 SA token rotation), the cache MUST drop stale
// entries. Implementation: cache key embeds the client's version stamp so
// any reconfigure invalidates the cache atomically.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenReviewResult is the validated identity returned by a successful review.
type TokenReviewResult struct {
	Authenticated bool
	Username      string // e.g. "system:serviceaccount:external-secrets:eso"
	UID           string
	Groups        []string
	CellID        string // cell that authenticated this token
}

// CrossClusterTokenReviewer is the interface T4.2 middleware needs to
// validate tokens against per-cell K8s API servers. Production impl uses
// `kubernetes.TokenReview` via clientset; tests mock cleanly.
type CrossClusterTokenReviewer interface {
	// Review submits a TokenReview to the given cell's K8s API.
	Review(ctx context.Context, cellID, token string) (*TokenReviewResult, error)

	// ClientVersion returns the current version stamp for the cell's client.
	// Used by the cache to detect kubeconfig rotation (EC-18).
	ClientVersion(cellID string) string
}

// Common errors
var (
	ErrCrossClusterMissingAuthHeader     = errors.New("cross-cluster: missing Authorization header")
	ErrCrossClusterMissingCellHeader     = errors.New("cross-cluster: missing X-Cell-ID header")
	ErrCrossClusterBadTokenFormat        = errors.New("cross-cluster: malformed Bearer token")
	ErrCrossClusterTokenNotAuthenticated = errors.New("cross-cluster: TokenReview returned authenticated=false")
)

// crossClusterCtxKey is a private context key.
type crossClusterCtxKey struct{}

// CrossClusterFromContext extracts the validated review result, if any, from ctx.
func CrossClusterFromContext(ctx context.Context) (*TokenReviewResult, bool) {
	r, ok := ctx.Value(crossClusterCtxKey{}).(*TokenReviewResult)
	return r, ok
}

// InjectCrossClusterResultForBroker injects a pre-validated TokenReviewResult
// into ctx using the same private key that CrossClusterFromContext reads from.
//
// This helper exists so the broker endpoint's bearer-token middleware in
// api/cmd/setup_secret_fetch.go can produce the same context shape the
// existing handler expects, WITHOUT also having to live inside this
// package. Callers MUST ensure the result is genuinely authenticated —
// this function performs no verification of its own.
//
// Built 2026-06-10 J-22 walkthrough to wire the Phase 4 broker LIVE in
// single-cluster dev-public, where the prod cross-cluster TokenReview
// chain is not yet shipped.
func InjectCrossClusterResultForBroker(ctx context.Context, result *TokenReviewResult) context.Context {
	return context.WithValue(ctx, crossClusterCtxKey{}, result)
}

// CrossClusterMiddlewareConfig holds tunables.
type CrossClusterMiddlewareConfig struct {
	Reviewer  CrossClusterTokenReviewer
	CacheTTL  time.Duration                    // default 60s
	OnFailure func(http.ResponseWriter, error) // default: 401 JSON
}

type crossClusterCache struct {
	mu  sync.RWMutex
	ttl time.Duration
	m   map[string]crossClusterEntry
}

type crossClusterEntry struct {
	result    *TokenReviewResult
	expiresAt time.Time
}

func newCrossClusterCache(ttl time.Duration) *crossClusterCache {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &crossClusterCache{
		ttl: ttl,
		m:   make(map[string]crossClusterEntry),
	}
}

// crossClusterCacheKey embeds clientVersion so kubeconfig rotation invalidates entries (EC-18).
func crossClusterCacheKey(cellID, token, clientVersion string) string {
	return cellID + "|" + clientVersion + "|" + token
}

func (c *crossClusterCache) get(key string) (*TokenReviewResult, bool) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		c.mu.Lock()
		delete(c.m, key)
		c.mu.Unlock()
		return nil, false
	}
	return e.result, true
}

func (c *crossClusterCache) put(key string, result *TokenReviewResult) {
	c.mu.Lock()
	c.m[key] = crossClusterEntry{result: result, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// NewCrossClusterMiddleware returns an http.Handler middleware function.
// On success the handler chain sees the validated identity via
// CrossClusterFromContext(ctx).
func NewCrossClusterMiddleware(cfg CrossClusterMiddlewareConfig) func(http.Handler) http.Handler {
	if cfg.Reviewer == nil {
		panic("cross-cluster middleware: Reviewer required")
	}
	cache := newCrossClusterCache(cfg.CacheTTL)
	onFailure := cfg.OnFailure
	if onFailure == nil {
		onFailure = defaultCrossClusterFailureWriter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearerToken(r)
			if err != nil {
				onFailure(w, err)
				return
			}
			cellID := r.Header.Get("X-Cell-ID")
			if cellID == "" {
				onFailure(w, ErrCrossClusterMissingCellHeader)
				return
			}

			clientVersion := cfg.Reviewer.ClientVersion(cellID)
			key := crossClusterCacheKey(cellID, token, clientVersion)

			if cached, ok := cache.get(key); ok {
				ctx := context.WithValue(r.Context(), crossClusterCtxKey{}, cached)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			result, err := cfg.Reviewer.Review(r.Context(), cellID, token)
			if err != nil {
				onFailure(w, err)
				return
			}
			if !result.Authenticated {
				onFailure(w, ErrCrossClusterTokenNotAuthenticated)
				return
			}
			result.CellID = cellID
			cache.put(key, result)

			ctx := context.WithValue(r.Context(), crossClusterCtxKey{}, result)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractBearerToken pulls the token from "Authorization: Bearer <t>".
func extractBearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", ErrCrossClusterMissingAuthHeader
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", ErrCrossClusterBadTokenFormat
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", ErrCrossClusterBadTokenFormat
	}
	return tok, nil
}

func defaultCrossClusterFailureWriter(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// json.Encoder e nao `%q`: o verbo do fmt produz uma string entre aspas no estilo
	// Go, que NAO e JSON. Um caractere de controle em err.Error() vira `\x00`, que
	// nenhum parser JSON aceita — o cliente receberia 401 com corpo ilegivel, e o
	// motivo real da recusa se perderia exatamente quando ele importa. E o detalhe vem
	// de entrada de rede (formato do token), entao a codificacao precisa ser a de
	// verdade, nao uma que se parece com ela (gosec G705).
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "auth_failed",
		"code":   "AUTH_TOKEN_INVALID",
		"detail": err.Error(),
	})
}
