// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package chaos implements T3.3 of plan canonical-tests-rewrite-v2 v1.0
// per ADR-2026-06-17-T3 D1 (Ginkgo BDD) + blueprint P3 (OpenTelemetry trace
// as chaos evidence).
//
// Wraps the existing chaos-mesh CRD manifests at archive/canonical-tests-v1/.../infra/tests/chaos/manifests/
// (note: chaos manifests themselves were NOT moved by T0.1 — they are
// CRDs declaratively applied, not bash scripts). The Go helpers here:
//
//   1. ApplyChaos      — kubectl apply -f <manifest>
//   2. WaitForFinished — poll chaos resource status until phase=Finished
//   3. AssertSLOHeld   — wrap existing loopzero-ops chaos slo-compare (preserved per ADR D8)
//   4. AssertSpanCoverage — NEW per blueprint P3: query OTel/Tempo for span patterns
//   5. CleanupChaos    — kubectl delete podchaos,networkchaos,iochaos,stresschaos --all
package chaos

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Manifest captures a chaos-mesh CRD ready to apply.
type Manifest struct {
	ID         string // C01..C12
	Path       string // path to YAML
	Kind       string // PodChaos / NetworkChaos / IOChaos / StressChaos
	Duration   time.Duration
	Hypothesis string // loopzero.io/hypothesis annotation summary
}

// ChaosResult captures the verdict per cycle-chaos.
type ChaosResult struct {
	Scenario      string
	Phase         string // applied / running / finished
	BaselineJSON  string
	DuringJSON    string
	PostJSON      string
	Verdict       string // PASS / FAIL / INCONCLUSIVE
	SLODegradation string
	RecoveryTime  time.Duration
}

// ApplyChaos kubectl apply's the manifest and records inject timestamp.
func ApplyChaos(ctx context.Context, kubeContext string, m Manifest) error {
	args := []string{}
	if kubeContext != "" {
		args = append(args, "--context", kubeContext)
	}
	args = append(args, "apply", "-f", m.Path)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply chaos %s: %w", m.ID, err)
	}
	return nil
}

// WaitForFinished polls chaos resource status.experiment.phase until "Finished" or context cancellation.
func WaitForFinished(ctx context.Context, kubeContext, kind, name, ns string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		args := []string{}
		if kubeContext != "" {
			args = append(args, "--context", kubeContext)
		}
		args = append(args, "get", kind, name, "-n", ns,
			"-o", "jsonpath={.status.experiment.phase}")
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		out, _ := cmd.Output()
		phase := strings.TrimSpace(string(out))
		if phase == "Finished" {
			return nil
		}
	}
}

// CleanupChaos deletes ALL chaos-mesh resources from the context.
// Per cycle-chaos cleanup mandatory rule (chaos-test SKILL Rule Suprema).
func CleanupChaos(ctx context.Context, kubeContext string) error {
	args := []string{}
	if kubeContext != "" {
		args = append(args, "--context", kubeContext)
	}
	args = append(args, "delete", "podchaos,networkchaos,iochaos,stresschaos", "--all", "-A")
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	return cmd.Run()
}

// AssertSLOHeld invokes the preserved Go helper at cmd/loopzero-ops/chaos_subcmd.go
// (per ADR-T3 D8: chaos_subcmd.go preserved verbatim).
func AssertSLOHeld(ctx context.Context, opsBinary, baselineJSON, duringJSON, postJSON string) (verdict string, err error) {
	cmd := exec.CommandContext(ctx, opsBinary, "chaos", "slo-compare",
		baselineJSON, duringJSON, postJSON)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Exit codes: 0=PASS, 2=FAIL, 5=INCONCLUSIVE per chaos_subcmd.go
		if cmd.ProcessState != nil {
			switch cmd.ProcessState.ExitCode() {
			case 2:
				return "FAIL", nil
			case 5:
				return "INCONCLUSIVE", nil
			}
		}
		return "", fmt.Errorf("loopzero-ops chaos slo-compare: %w (output: %s)", err, out)
	}
	return "PASS", nil
}

// TempoTraceQuery represents a query to the Tempo backend.
type TempoTraceQuery struct {
	TempoURL  string // http://tempo.monitoring.svc.cluster.local:3200 (default if empty)
	Service   string // e.g., "loopzero-api"
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
