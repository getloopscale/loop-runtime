// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package chaos implements T3.3 of plan canonical-tests-rewrite-v2 v1.0
// per ADR-2026-06-17-T3 D1 (Ginkgo BDD) + blueprint P3 (OpenTelemetry trace
// as chaos evidence).
//
// Wraps the existing chaos-mesh CRD manifests at archive/canonical-tests-v1/.../infra/tests/chaos/manifests/
// (note: chaos manifests themselves were NOT moved by T0.1 — they are
// CRDs declaratively applied, not bash scripts). The Go helpers here:
//
//  1. ApplyChaos      — kubectl apply -f <manifest>
//  2. WaitForFinished — poll chaos resource status until phase=Finished
//  3. AssertSLOHeld   — wrap existing loopruntime-ops chaos slo-compare (preserved per ADR D8)
//  4. AssertSpanCoverage — NEW per blueprint P3: query OTel/Tempo for span patterns
//  5. CleanupChaos    — kubectl delete podchaos,networkchaos,iochaos,stresschaos --all
package chaos

import (
	"context"
	"time"
)

// Manifest captures a chaos-mesh CRD ready to apply.
type Manifest struct {
	ID         string // C01..C12
	Path       string // path to YAML
	Kind       string // PodChaos / NetworkChaos / IOChaos / StressChaos
	Duration   time.Duration
	Hypothesis string // runtime.getloopscale.com/hypothesis annotation summary
}

// ChaosResult captures the verdict per cycle-chaos.
type ChaosResult struct {
	Scenario       string
	Phase          string // applied / running / finished
	BaselineJSON   string
	DuringJSON     string
	PostJSON       string
	Verdict        string // PASS / FAIL / INCONCLUSIVE
	SLODegradation string
	RecoveryTime   time.Duration
}

// TempoTraceQuery represents a query to the Tempo backend.
type TempoTraceQuery struct {
	TempoURL  string // http://tempo.monitoring.svc.cluster.local:3200 (default if empty)
	Service   string // e.g., "loopruntime-api"
	Operation string // span name to look for
	Window    time.Duration
}

// AssertSpanCoverage queries Tempo and asserts that ≥ minCount spans matching
// the service/operation arrived during the chaos window. Per blueprint P3,
// this is the trace-as-evidence pattern alongside Prometheus SLO query.
//
// Returns (count, error). count=-1 indicates the helper could not reach Tempo
// (NOT a chaos failure — operator should verify Tempo deployment).
//
// M10 T4.1: de-skeletoned — delegates to the real Tempo HTTP client
// (AssertSpanCoverageReal in tempo_client.go), which is hermetically tested via
// httptest (tempo_client_test.go: parse / pass / fail / network-error). The
// query's TempoURL selects the endpoint (DefaultTempoURL in a LIVE cluster; an
// httptest server in hermetic tests), so this helper carries no LIVE dependency
// of its own — the LIVE Tempo query is exercised only when a real TempoURL is set.
func AssertSpanCoverage(ctx context.Context, q TempoTraceQuery, minCount int) (int, error) {
	return AssertSpanCoverageReal(ctx, q, minCount)
}
