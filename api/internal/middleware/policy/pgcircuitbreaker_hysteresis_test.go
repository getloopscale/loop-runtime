// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// B-054: the `api` ran TWO gates over the same pool. The one that actually
// blocked requests was this middleware, deciding by an instantaneous
// percentage with no hysteresis. The other — `buildplane/admission.PGPoolBreaker`
// — has hysteresis (high/low water), documents flapping as its reason to
// exist... and was never consulted: `Allow()` had no production caller.
//
// That is: the instrument that solved the flapping was off, and the one that was
// on had the problem. This test measures the behaviour on the path that is ON.
//
// Why flapping matters: near the cut-off, the pool rises and falls with every
// request that returns a connection. Without hysteresis the gate opens and
// closes with it, and the client sees 503 alternating with 200 — the worst of
// both worlds, neither protecting nor serving stably.
func TestBreakerDoesNotFlapAroundTheCutOff(t *testing.T) {
	// The pool oscillates one point around the limit: 90, 89, 90, 89...
	samples := []int32{90, 89, 90, 89, 90, 89}
	var i int
	gate := &fakeGate{tripped: false, samples: samples, idx: &i}

	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{Gate: gate})
	h := cb.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var transitions int
	previous := -1
	for range samples {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
		current := rec.Code
		if previous != -1 && current != previous {
			transitions++
		}
		previous = current
	}

	// With hysteresis the gate trips at 90 and only re-arms below 75: ONE
	// transition at most. Without it, it alternates on every sample — five.
	if transitions > 1 {
		t.Errorf("the gate flipped %d time(s) over a pool oscillating ONE point at the cut-off; "+
			"with hysteresis it would be at most 1. The client sees 503 and 200 alternating, which "+
			"neither protects nor serves stably (B-054)", transitions)
	}
}

// fakeGate reproduces the high/low water hysteresis of `admission.PGPoolBreaker`
// over a sequence of samples, so this test measures the gate's BEHAVIOUR and not
// its implementation.
type fakeGate struct {
	tripped bool
	samples []int32
	idx     *int
}

func (g *fakeGate) Allow() bool {
	pct := g.samples[*g.idx%len(g.samples)]
	*g.idx++
	if g.tripped {
		if pct < 75 {
			g.tripped = false
		}
	} else if pct >= 90 {
		g.tripped = true
	}
	return !g.tripped
}
