// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	planpkg "github.com/getloopscale/contracts/plan"
	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// The DISABLED cap must show as system state, not only as a log line at another
// process's boot (B-033, third bullet — it says exactly that).
//
// `CostCapDecisionsTotal` already existed and already told `unmeasured` apart —
// "the gate evaluated and there was no counter". But it is incremented AFTER the
// nil-receiver bypass, so the most serious case — **the gate does not exist** —
// emitted nothing. Measured 2026-08-19: a `nil` passed straight through, silently,
// and the only signal was the boot log the item itself rejects as sufficient.
//
// The difference matters to whoever is on call: `unmeasured` can be a new tenant;
// `disabled` is the protection missing for EVERYONE.
func TestDisabledCapEmitsADecisionInsteadOfPassingSilently(t *testing.T) {
	const metric = planpkg.CostMetric("build_minutes")

	before := testutil.ToFloat64(metrics.CostCapDecisionsTotal.WithLabelValues(string(metric), "disabled"))

	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true })

	// Nil receiver: the real state measured on app-dev — the aggregator did not
	// come up and the enforcer was never wired.
	var e *CostEnforcer
	h := e.Middleware(metric)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !reached {
		t.Fatal("the nil-receiver bypass stopped passing through — that BREAKS the tolerant " +
			"boot it exists to allow; the change asked for is to observe, not to block")
	}
	after := testutil.ToFloat64(metrics.CostCapDecisionsTotal.WithLabelValues(string(metric), "disabled"))
	if after <= before {
		t.Errorf("the disabled cap passed without emitting a decision (before=%v after=%v). "+
			"While that holds, 'the protection does not exist' is indistinguishable from 'there was no request', "+
			"and the only signal is a boot log nobody reads after the day the process started.",
			before, after)
	}
}
