// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package admission_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B-054, measured 2026-08-18. The `api` built TWO instruments over the same
// Postgres pool, with the same declared policy (reject mutations under
// saturation, let reads through):
//
//	api/internal/middleware/pgcircuitbreaker.go         percentage gate WITHOUT hysteresis
//	api/internal/builds/admission/pg_breaker.go     high/low-water WITH hysteresis
//
// The hysteresis is not decoration: the admission file itself explains that
// without it the breaker flaps on every tick near the cut-off line.
//
// The measurement found more than "two diverge". The admission breaker was
// built, started and stopped on shutdown — and `Allow()` was never called. It
// measured saturation, tripped with hysteresis and logged; nobody asked it
// whether to allow. What actually rejected requests was the other one, the one
// WITHOUT hysteresis.
//
// This test pins the fix: the breaker is built and handed to the router as the
// pool gate, and the router consults it.
func TestPoolGateIsWiredIntoTheMiddleware(t *testing.T) {
	// A scan for IMPORTERS no longer works, and the reason is the fix itself:
	// the middleware consults this breaker through an INTERFACE it declares
	// (`middleware.PoolGate`), so it does not import this package. A guard
	// looking for importers would keep reporting "no caller" with the breaker
	// wired — green on the wrong question.
	//
	// What proves the wiring is the assignment: something builds the breaker and
	// hands it over as the gate the router consults.
	root := repositoryRoot(t)

	wiring, err := os.ReadFile(filepath.Join(root, "api", "cmd", "setup_server.go"))
	if err != nil {
		t.Fatalf("reading the wiring: %v", err)
	}
	w := string(wiring)
	if !strings.Contains(w, "NewPGPoolBreaker(") {
		t.Error("`setup_server.go` does not build the PGPoolBreaker: without it the pool gate falls " +
			"back to the instantaneous percentage, which flaps at the cut-off line (B-054)")
	}
	if !strings.Contains(w, "sc.Guards.PGPoolGate = ") {
		t.Error("the breaker is built but not handed over as `sc.Guards.PGPoolGate` — built, started, " +
			"stopped on shutdown and never consulted is exactly the state B-054 measured")
	}

	router, err := os.ReadFile(filepath.Join(root, "api", "internal", "server", "router.go"))
	if err != nil {
		t.Fatalf("reading the router: %v", err)
	}
	if !strings.Contains(string(router), "cfg.Guards.PGPoolGate") {
		t.Error("the router does not consult `cfg.Guards.PGPoolGate`: the gate exists and nobody asks it")
	}
}

// repositoryRoot walks up to the directory holding `api/go.mod`, independent of
// how deep the package sits — exactly the fragility that broke the previous
// version of this test.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "api", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repository root not found walking up from the cwd (looked for api/go.mod)")
	return ""
}
