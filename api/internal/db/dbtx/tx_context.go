// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dbtx

import "context"

// txContextKey is the context key for a transaction a caller opened and wants
// the statements it delegates to join.
type txContextKey struct{}

// ContextWithTx returns a context carrying tx. The placement serializer uses it
// so the tenant INSERT runs in the transaction that holds the placement lock
// (MEET-053 D-01). Declared in this port — not in placement — so the store that
// reads it depends only on the persistence port, never on a service package.
func ContextWithTx(ctx context.Context, tx DBTX) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// TxFromContext returns the transaction carried by ctx, or nil when the caller
// opened none and the store should use its own connection.
func TxFromContext(ctx context.Context) DBTX {
	tx, _ := ctx.Value(txContextKey{}).(DBTX)
	return tx
}
