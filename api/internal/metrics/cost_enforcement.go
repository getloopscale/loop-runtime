// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CostCapDecisionsTotal counts every cost-cap decision, labelled by its REASON.
//
// # Why it exists
//
// B-033, measured on app-dev on 2026-08-11: the aggregator that fills `cost_counters` was
// off, the table had zero rows, and the gate treats a missing row as zero consumption.
// Result: **every** cap let everything through, the three cost metrics were unbounded in
// the environment serving the public dashboard, and the only signal was a log line at the
// boot of another process — which nobody reads after the day the process started.
//
// # What this metric makes queryable
//
// The distinction the verdict alone does not carry:
//
//	reason="under_cap"    the protection EVALUATED a counter and it was below the cap
//	reason="unmeasured"   there was no counter — a new tenant, or a dead aggregator
//	reason="store_error"  the read failed and the gate let it through (deliberate fail-open)
//	reason="unlimited"    the plan has no cap for this metric
//	reason="blocked"      the counter reached the cap
//	reason="disabled"     the gate DOES NOT EXIST — the enforcer was never wired (nil receiver)
//
// `disabled` was added on 2026-08-19 and is the most serious of the six, although it looks
// the most harmless. The other five describe a protection that ran; this one describes its
// absence, for everyone at once. It was missing because the counter was incremented AFTER
// the nil-receiver short-circuit — so the worst case was the only one emitting nothing, and
// the only signal was the boot log line this item refuses as sufficient.
//
// For whoever is on call the distinction decides the action: `unmeasured` may be a
// legitimate new tenant; `disabled` is the protection being down.
//
// A high `unmeasured` means the protection is evaluating nothing. `under_cap` at zero over
// a whole period means it **never** evaluated — exactly the state the item measured and
// that nothing made visible.
//
// The metric changes NO verdict: it allows and blocks exactly as before. It changes what the
// system can say about its own protection.
var CostCapDecisionsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "loopzero_cost_cap_decisions_total",
		Help: "Cost cap decisions by metric and reason. reason=unmeasured means there was no counter to evaluate.",
	},
	[]string{"metric", "reason"},
)
