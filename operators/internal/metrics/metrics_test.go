// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	cb "github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

// A breaker's state change is visible on /metrics, not only in the log.
func TestObserveCircuitBreakerTransition_PublishesTheNewState(t *testing.T) {
	ObserveCircuitBreakerTransition("gitea", cb.Closed, cb.Open)
	if got := testutil.ToFloat64(TenantCircuitBreakerState.WithLabelValues("gitea")); got != float64(cb.Open) {
		t.Fatalf("loopruntime_tenant_cb_state{dep=gitea} = %v, want %v (open)", got, float64(cb.Open))
	}

	ObserveCircuitBreakerTransition("gitea", cb.Open, cb.HalfOpen)
	if got := testutil.ToFloat64(TenantCircuitBreakerState.WithLabelValues("gitea")); got != float64(cb.HalfOpen) {
		t.Fatalf("loopruntime_tenant_cb_state{dep=gitea} = %v, want %v (half-open)", got, float64(cb.HalfOpen))
	}
}
