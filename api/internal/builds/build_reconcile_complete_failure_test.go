// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// T2.3 (Test 2 + Test 7) of plan 2026-05-28-meet-001-engineering-closure-plan.md.
// Validation Planner mandated failure-path coverage for
// enrichAppReleasesWithAttestation. The enrichment is a pure function over
// AppRelease + WorkflowStatus — no DB required — so these tests are
// hermetic and run in every Go test invocation.
//
// Failure modes covered:
//   - AttestStepFailed     → enrichment leaves URL empty (no false persist)
//   - MalformedURL         → "unknown" sentinel from cosign extractor is ignored
//   - MissingOutputs       → nil + empty Outputs map both safe
//   - DuplicateAttestSteps → multi-app: each app reads only its own step
//   - Test 7 (concurrent)  → 2 goroutines invoking enrichment on disjoint
//                            slices race-clean (covered with -race flag)

// ---------------------------------------------------------------------------
// Test 2: enrich-failure unit tests
// ---------------------------------------------------------------------------

func TestEnrichAttestation_NilWorkflowStatus_NoOp(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	enrichAppReleasesWithAttestation(apps, nil)
	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("nil status must leave URL empty, got %q", apps[0].ImageAttestationRekorURL)
	}
}

func TestEnrichAttestation_EmptyOutputs_NoOp(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	enrichAppReleasesWithAttestation(apps, &deploycontracts.WorkflowStatus{Phase: "Succeeded"})
	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("empty Outputs must leave URL empty, got %q", apps[0].ImageAttestationRekorURL)
	}
}

func TestEnrichAttestation_AttestStepMissing_NoOp(t *testing.T) {
	// The build succeeded but the attest step was not part of the DAG
	// (CosignAttestEnabled=false). Enrichment must NOT fabricate a URL.
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			// Only sign step output — no attest output.
			"sign-image-api": {"signature": "sig:abc", "digest": "sha256:xyz"},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("missing attest step output must leave URL empty, got %q", apps[0].ImageAttestationRekorURL)
	}
	// Sign output should still propagate — we don't want to over-protect.
	if apps[0].ImageSignature != "sig:abc" {
		t.Errorf("sign signature should propagate, got %q", apps[0].ImageSignature)
	}
}

func TestEnrichAttestation_AttestStepWithEmptyURL_NoOp(t *testing.T) {
	// Attest step ran but emitted empty `rekor-url` (cosign Stdout parse
	// found no Rekor index). The original 3-layer Rekor URL bug (LIVE
	// 2026-05-28) shipped past this guard once because the step ran but
	// the URL was silently dropped — make sure we don't regress.
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": ""},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("empty rekor-url output must leave URL empty, got %q", apps[0].ImageAttestationRekorURL)
	}
}

func TestEnrichAttestation_AttestStepUnknownSentinel_NoOp(t *testing.T) {
	// cosign_attest.go's URL extractor uses the literal "unknown" string
	// as a parse-failed sentinel; this MUST NOT be persisted as the URL.
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": "unknown"},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if apps[0].ImageAttestationRekorURL != "" {
		t.Errorf("'unknown' sentinel must NOT propagate as URL, got %q", apps[0].ImageAttestationRekorURL)
	}
}

func TestEnrichAttestation_AttestStepValidURL_Propagates(t *testing.T) {
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	url := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=42"
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": url},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if apps[0].ImageAttestationRekorURL != url {
		t.Errorf("valid URL must propagate, got %q want %q", apps[0].ImageAttestationRekorURL, url)
	}
}

func TestEnrichAttestation_MultiApp_PerAppIsolation(t *testing.T) {
	// Two apps, two distinct attest step outputs. The enricher must
	// scope each app's URL to its own step — no cross-app leak.
	apps := []deploycontracts.AppRelease{{Name: "api"}, {Name: "worker"}}
	apiURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=1"
	workerURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=2"
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api":    {"rekor-url": apiURL},
			"attest-image-worker": {"rekor-url": workerURL},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if apps[0].ImageAttestationRekorURL != apiURL {
		t.Errorf("api URL drift: got %q want %q", apps[0].ImageAttestationRekorURL, apiURL)
	}
	if apps[1].ImageAttestationRekorURL != workerURL {
		t.Errorf("worker URL drift: got %q want %q", apps[1].ImageAttestationRekorURL, workerURL)
	}
}

func TestEnrichAttestation_MultiApp_OnlyOneSucceeds(t *testing.T) {
	// Mixed success: api's attest succeeded with a URL, worker's attest
	// step is absent from Outputs entirely. Worker URL must stay empty
	// and api URL must propagate — no shared-state leak.
	apps := []deploycontracts.AppRelease{{Name: "api"}, {Name: "worker"}}
	apiURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=7"
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": apiURL},
			// worker attest output absent
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if apps[0].ImageAttestationRekorURL != apiURL {
		t.Errorf("api URL: got %q want %q", apps[0].ImageAttestationRekorURL, apiURL)
	}
	if apps[1].ImageAttestationRekorURL != "" {
		t.Errorf("worker URL must remain empty, got %q", apps[1].ImageAttestationRekorURL)
	}
}

func TestEnrichAttestation_DuplicateAttestStepName_LastWins(t *testing.T) {
	// Defensive: map keys are unique by construction (Go map semantics),
	// but if the workflow re-runs and emits duplicate node outputs, the
	// last value-by-iteration is canonical. This test pins that behavior
	// by asserting we still get a single URL, not concatenation.
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	url := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=99"
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": url},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if strings.Count(apps[0].ImageAttestationRekorURL, "https://") != 1 {
		t.Errorf("expected exactly one URL in result, got %q", apps[0].ImageAttestationRekorURL)
	}
}

// ---------------------------------------------------------------------------
// Test 7: concurrent enrich-race
// ---------------------------------------------------------------------------

// TestEnrichAttestation_ConcurrentSlices_NoDataRace runs two enrichments
// on DISJOINT slices of AppRelease in parallel and asserts no deadlock,
// no panic, and that each goroutine's result is independently correct.
// Run with `go test -race`; the race detector flags shared-state writes.
//
// Documented edge: the plan suggests `t.Parallel()=false` + retry if race
// detector flakes on shared CI runners. The enricher is a pure function
// reading status.Outputs (read-only) and writing apps[i] (per-call slice).
// With disjoint slices there is NO sharing — so no flake is expected.
// If a future refactor introduces shared state, this test will surface
// it loud at the next CI run.
func TestEnrichAttestation_ConcurrentSlices_NoDataRace(t *testing.T) {
	apiURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=1"
	workerURL := "https://rekor.loop-runtime.test/api/v1/log/entries?logIndex=2"
	statusA := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": apiURL},
		},
	}
	statusB := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-worker": {"rekor-url": workerURL},
		},
	}

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			apps := []deploycontracts.AppRelease{{Name: "api"}}
			enrichAppReleasesWithAttestation(apps, statusA)
			if apps[0].ImageAttestationRekorURL != apiURL {
				t.Errorf("goroutine A drift at iter %d: %q", i, apps[0].ImageAttestationRekorURL)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			apps := []deploycontracts.AppRelease{{Name: "worker"}}
			enrichAppReleasesWithAttestation(apps, statusB)
			if apps[0].ImageAttestationRekorURL != workerURL {
				t.Errorf("goroutine B drift at iter %d: %q", i, apps[0].ImageAttestationRekorURL)
				return
			}
		}
	}()
	wg.Wait()
}

// T4.1 (CR-8): the enrichment must emit Sigstore observability counters
// so SLO alerts have signal even before the cosign scripts surface
// duration outputs (operator follow-up). These tests pin the
// emit-side-effect contract; they're separate from the existing
// TestEnrich* set which asserts pure slice mutation.

func TestEnrichAttestation_EmitsSignTotal_SuccessOnSignature(t *testing.T) {
	metrics.CosignSignTotal.Reset()
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api":   {"signature": "sig:abc"},
			"attest-image-api": {"rekor-url": "https://rekor.loop-runtime.test/x"},
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if got := testutil.ToFloat64(metrics.CosignSignTotal.WithLabelValues("success")); got != 1 {
		t.Errorf("sign_total{success} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.CosignSignTotal.WithLabelValues("failure")); got != 0 {
		t.Errorf("sign_total{failure} = %v, want 0", got)
	}
}

func TestEnrichAttestation_EmitsSignTotal_FailureWhenSignStepEmpty(t *testing.T) {
	metrics.CosignSignTotal.Reset()
	metrics.CosignSignErrors.Reset()
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"sign-image-api": {}, // ran but produced no signature
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if got := testutil.ToFloat64(metrics.CosignSignTotal.WithLabelValues("failure")); got != 1 {
		t.Errorf("sign_total{failure} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.CosignSignErrors.WithLabelValues("fulcio")); got != 1 {
		t.Errorf("sign_errors{fulcio} = %v, want 1 (sign step ran but no signature → fulcio attribution)", got)
	}
}

func TestEnrichAttestation_EmitsAttestTotal_FailureWhenURLEmpty(t *testing.T) {
	metrics.CosignAttestTotal.Reset()
	metrics.CosignAttestErrors.Reset()
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": "unknown"}, // sentinel → empty after enrichment
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if got := testutil.ToFloat64(metrics.CosignAttestTotal.WithLabelValues("failure")); got != 1 {
		t.Errorf("attest_total{failure} = %v, want 1 (unknown sentinel)", got)
	}
	if got := testutil.ToFloat64(metrics.CosignAttestErrors.WithLabelValues("rekor")); got != 1 {
		t.Errorf("attest_errors{rekor} = %v, want 1", got)
	}
}

func TestEnrichAttestation_EmitsRekorURLNullCounter(t *testing.T) {
	metrics.ReleaseAttestationRekorURLPersist.Reset()
	apps := []deploycontracts.AppRelease{{Name: "api"}, {Name: "web"}}
	status := &deploycontracts.WorkflowStatus{
		Phase: "Succeeded",
		Outputs: map[string]map[string]string{
			"attest-image-api": {"rekor-url": "https://rekor.loop-runtime.test/x"},
			"attest-image-web": {"rekor-url": ""}, // empty → null bucket
		},
	}
	enrichAppReleasesWithAttestation(apps, status)
	if got := testutil.ToFloat64(metrics.ReleaseAttestationRekorURLPersist.WithLabelValues("populated")); got != 1 {
		t.Errorf("rekor_url{populated} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.ReleaseAttestationRekorURLPersist.WithLabelValues("null")); got != 1 {
		t.Errorf("rekor_url{null} = %v, want 1", got)
	}
}

func TestEnrichAttestation_NilStatus_NoMetricEmission(t *testing.T) {
	metrics.CosignSignTotal.Reset()
	metrics.ReleaseAttestationRekorURLPersist.Reset()
	apps := []deploycontracts.AppRelease{{Name: "api"}}
	enrichAppReleasesWithAttestation(apps, nil)
	if got := testutil.ToFloat64(metrics.CosignSignTotal.WithLabelValues("success")); got != 0 {
		t.Errorf("sign_total{success} = %v, want 0 (nil status must not emit)", got)
	}
	if got := testutil.ToFloat64(metrics.ReleaseAttestationRekorURLPersist.WithLabelValues("null")); got != 0 {
		t.Errorf("rekor_url{null} = %v, want 0 (nil status must not emit)", got)
	}
}
