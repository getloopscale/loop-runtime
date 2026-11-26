// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth

import (
	"context"
	"testing"
)

func TestCrossClusterFromContext_NoValue_ReturnsFalse(t *testing.T) {
	_, ok := CrossClusterFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for empty context")
	}
}

func TestCrossClusterFromContext_ReturnsTheInjectedIdentity(t *testing.T) {
	want := &TokenReviewResult{Authenticated: true, Username: "broker:eso-webhook", CellID: "cell-0"}

	got, ok := CrossClusterFromContext(InjectCrossClusterResultForBroker(context.Background(), want))

	if !ok || got != want {
		t.Errorf("CrossClusterFromContext = (%v, %v), want the injected identity", got, ok)
	}
}
