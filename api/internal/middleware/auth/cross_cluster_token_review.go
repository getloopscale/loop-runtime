// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth

// cross_cluster_token_review.go — the identity of a caller from a cell
// runtime cluster (ESO fetching a secret through the broker), carried in the
// request context.
//
// Who establishes that identity is the broker's bearer middleware
// (api/cmd/setup_secret_fetch.go): it verifies the broker token and injects a
// TokenReviewResult whose CellID is derived server-side. A per-cell Kubernetes
// TokenReview path does not exist; the middleware that was written for it had
// no reviewer implementation and no caller, and was removed rather than kept
// as an unwired promise. Building it means implementing a reviewer against
// each cell's API server and mounting it on the broker route.

import "context"

// TokenReviewResult is the validated identity of a cross-cluster caller.
type TokenReviewResult struct {
	Authenticated bool
	Username      string // e.g. "system:serviceaccount:external-secrets:eso"
	UID           string
	Groups        []string
	CellID        string // cell that authenticated this token
}

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
// Callers MUST ensure the result is genuinely authenticated — this function
// performs no verification of its own.
func InjectCrossClusterResultForBroker(ctx context.Context, result *TokenReviewResult) context.Context {
	return context.WithValue(ctx, crossClusterCtxKey{}, result)
}
