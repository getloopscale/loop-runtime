// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dbtx

import (
	"context"
	"testing"
)

type markerTx struct{ DBTX }

func TestTxFromContext_ReturnsTheCarriedTransaction(t *testing.T) {
	tx := &markerTx{}
	if got := TxFromContext(ContextWithTx(context.Background(), tx)); got != tx {
		t.Fatalf("got %v, want the transaction the context carries", got)
	}
}

func TestTxFromContext_NoTransactionIsNil(t *testing.T) {
	if got := TxFromContext(context.Background()); got != nil {
		t.Fatalf("got %v, want nil so the store uses its own connection", got)
	}
}
