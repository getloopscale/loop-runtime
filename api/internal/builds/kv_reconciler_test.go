// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
)

type mockKVRoutingSource struct {
	keys map[string]bool
	err  error
}

func (m *mockKVRoutingSource) ExpectedStaticKeys(_ context.Context) (map[string]bool, error) {
	return m.keys, m.err
}

func TestKVReconciler_NilClient(t *testing.T) {
	// When kvClient is nil (local dev), reconcile should be a no-op
	r := NewKVReconciler(nil, &mockKVRoutingSource{keys: map[string]bool{"key1": true}}, nil, 0)
	// Should not panic
	r.reconcile(context.Background())
}

type fakeKVKeys struct {
	keys []string
	err  error
}

func (f fakeKVKeys) ListKeys(context.Context) ([]string, error) { return f.keys, f.err }

func driftAfter(t *testing.T, kv KVKeyLister, expected map[string]bool) float64 {
	t.Helper()
	metrics.CFKVDriftDetected.Set(-1)
	r := NewKVReconciler(kv, &mockKVRoutingSource{keys: expected}, slog.Default(), 0)
	r.reconcile(context.Background())
	return testutil.ToFloat64(metrics.CFKVDriftDetected)
}

func TestKVReconciler_MatchingKeysReportNoDrift(t *testing.T) {
	got := driftAfter(t, fakeKVKeys{keys: []string{"a", "b"}}, map[string]bool{"a": true, "b": true})

	if got != 0 {
		t.Errorf("drift gauge = %v, want 0", got)
	}
}

func TestKVReconciler_MissingKeyReportsDrift(t *testing.T) {
	got := driftAfter(t, fakeKVKeys{keys: []string{"a"}}, map[string]bool{"a": true, "b": true})

	if got != 1 {
		t.Errorf("drift gauge = %v, want 1", got)
	}
}

func TestKVReconciler_ExtraKeyReportsDrift(t *testing.T) {
	got := driftAfter(t, fakeKVKeys{keys: []string{"a", "stray"}}, map[string]bool{"a": true})

	if got != 1 {
		t.Errorf("drift gauge = %v, want 1", got)
	}
}

// The edge's cell routing shares this store: route:<host> is the engine's edge
// publisher's and ingress:<cell> the cell provisioner's (#448). Neither is static
// delivery's drift.
func TestKVReconciler_EdgeRoutingKeysAreIgnored(t *testing.T) {
	got := driftAfter(t, fakeKVKeys{keys: []string{"a", "route:tn-1-web-production", "ingress:cell-1"}}, map[string]bool{"a": true})

	if got != 0 {
		t.Errorf("drift gauge = %v, want 0", got)
	}
}

func TestKVReconciler_ListFailureLeavesTheGaugeUntouched(t *testing.T) {
	got := driftAfter(t, fakeKVKeys{err: errors.New("cf api down")}, map[string]bool{"a": true})

	if got != -1 {
		t.Errorf("drift gauge = %v, want the pre-run sentinel -1", got)
	}
}
