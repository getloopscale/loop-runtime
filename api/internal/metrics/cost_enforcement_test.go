// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The silent degradation of the cost cap must become system state.
//
// B-033: when the aggregator does not come up, `cost_counters` stays empty and EVERY cap
// lets everything through — and the only signal was a log line at another process's boot.
// A metric with the reason as a label makes that queryable: a high `unmeasured` means the
// protection is evaluating nothing, and a zero `under_cap` means it never evaluated.
func TestCostCapDecisions_IsRegisteredWithReasonLabel(t *testing.T) {
	CostCapDecisionsTotal.WithLabelValues("build_minutes", "unmeasured").Inc()
	CostCapDecisionsTotal.WithLabelValues("build_minutes", "under_cap").Inc()

	n := testutil.CollectAndCount(CostCapDecisionsTotal)
	if n < 2 {
		t.Fatalf("expected both series to be collectable, got %d", n)
	}

	if v := testutil.ToFloat64(CostCapDecisionsTotal.WithLabelValues("build_minutes", "unmeasured")); v < 1 {
		t.Errorf("the `unmeasured` series did not record: %v", v)
	}
}

// The name and labels are an alerting contract: changing them silently breaks the
// dashboards that depend on them.
func TestCostCapDecisions_NameAndLabelsAreStable(t *testing.T) {
	desc := CostCapDecisionsTotal.WithLabelValues("x", "y").Desc().String()

	for _, expected := range []string{"loop_runtime_cost_cap_decisions_total", "metric", "reason"} {
		if !strings.Contains(desc, expected) {
			t.Errorf("descriptor lost %q: %s", expected, desc)
		}
	}
}
