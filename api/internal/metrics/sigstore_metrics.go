// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// T4.1 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
// CR-8 (MEET-2026-05-28-001 SRE + Production Design): "Zero loopzero_cosign_*
// / loopzero_sign_* / loopzero_attest_* / loopzero_rekor_* / loopzero_fulcio_* metrics
// in the codebase. You cannot detect Sigstore brownouts in prod." This
// file ships the 8 metrics that close that gap.
//
// Naming: loopzero_<subsystem>_<name> per CLAUDE.md spec §15.2.
// EC-6 fix (plan v1.1): NO circuit_breaker_state metric — Production
// Design Review flagged that as a separate workstream (no circuit
// breaker exists yet). Re-add when the circuit breaker lands.
// EC-7 fix (plan v1.1): sign_total + attest_total counters added as
// denominators for the T4.2 error-rate alerts.

// signHistogramBuckets — cosign sign duration buckets.
//
// Default Prometheus buckets (0.005s..10s) are inappropriate: cosign
// sign typically takes 1-30s (Fulcio cert issue + Rekor record). We
// use linear buckets 1s..56s (1, 6, 11, 16, ..., 56) to give 12 bins
// across the realistic range. Operators can tune via Helm later
// (EC-16 generalized — histogram buckets are operator-tunable).
var signHistogramBuckets = prometheus.LinearBuckets(1, 5, 12)

var (
	// 1. CosignSignDuration — cosign sign step duration in seconds.
	CosignSignDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_cosign_sign_duration_seconds",
		Help:    "Duration of cosign sign-image step in seconds.",
		Buckets: signHistogramBuckets,
	}, []string{"keyless"})

	// 2. CosignAttestDuration — cosign attest step duration in seconds.
	CosignAttestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_cosign_attest_duration_seconds",
		Help:    "Duration of cosign attest-image step in seconds.",
		Buckets: signHistogramBuckets,
	}, []string{"keyless"})

	// 3. CosignSignTotal — total cosign sign attempts by result.
	// Denominator for LoopZeroCosignSignErrorRate alert (T4.2 / EC-7).
	CosignSignTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_cosign_sign_total",
		Help: "Total cosign sign-image invocations by result.",
	}, []string{"result"}) // result="success" | "failure"

	// 4. CosignAttestTotal — total cosign attest attempts by result.
	// Denominator for LoopZeroCosignAttestErrorRate alert (T4.2 / EC-7).
	CosignAttestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_cosign_attest_total",
		Help: "Total cosign attest-image invocations by result.",
	}, []string{"result"}) // result="success" | "failure"

	// 5. CosignSignErrors — sign errors attributed to a Sigstore component.
	// Labels: component="fulcio|rekor|ctlog|other".
	CosignSignErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_cosign_sign_errors_total",
		Help: "Cosign sign errors by upstream Sigstore component.",
	}, []string{"component"})

	// 6. CosignAttestErrors — attest errors attributed to a Sigstore component.
	CosignAttestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_cosign_attest_errors_total",
		Help: "Cosign attest errors by upstream Sigstore component.",
	}, []string{"component"})

	// 7. ReleaseAttestationRekorURLPersist — Rekor URL persistence outcome.
	// Labels: result="null" (no URL persisted) | "populated" (URL persisted).
	// The ratio populated/(null+populated) is the SLI for the T4.2
	// LoopZeroReleaseAttestationRekorURLNullRate alert.
	ReleaseAttestationRekorURLPersist = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_release_attestation_rekor_url_null_total",
		Help: "Releases bucketed by whether image_attestation_rekor_url was persisted.",
	}, []string{"result"}) // result="null" | "populated"

	// 8. FulcioCertIssuanceDuration — cert issuance latency from Fulcio.
	// Distinct from CosignSignDuration: the sign step is end-to-end
	// (cert issue + Rekor record). This isolates the Fulcio call so
	// operators can root-cause "sign slow but Rekor fast" or vice versa.
	FulcioCertIssuanceDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "loopzero_fulcio_cert_issuance_duration_seconds",
		Help:    "Fulcio certificate issuance call duration (subset of cosign sign).",
		Buckets: prometheus.LinearBuckets(0.5, 1, 10), // 0.5s..9.5s
	})
)

// ObserveCosignSign records the outcome of a sign step and, on failure, the
// Sigstore component it is attributed to. It observes no latency: a caller
// that did not time the step must not put a 0s sample into the histogram,
// where it reads as a very fast signature.
func ObserveCosignSign(success bool, errorComponent string) {
	if success {
		CosignSignTotal.WithLabelValues("success").Inc()
		return
	}
	CosignSignTotal.WithLabelValues("failure").Inc()
	if errorComponent != "" {
		CosignSignErrors.WithLabelValues(errorComponent).Inc()
	}
}

// ObserveCosignSignDuration records a sign step's measured latency, labelled
// by signing mode. Call it only with a duration that was actually measured.
func ObserveCosignSignDuration(keyless bool, seconds float64) {
	mode := strconv.FormatBool(keyless)
	CosignSignDuration.WithLabelValues(mode).Observe(seconds)
}

// ObserveCosignAttest mirrors ObserveCosignSign for the attest step.
func ObserveCosignAttest(success bool, errorComponent string) {
	if success {
		CosignAttestTotal.WithLabelValues("success").Inc()
		return
	}
	CosignAttestTotal.WithLabelValues("failure").Inc()
	if errorComponent != "" {
		CosignAttestErrors.WithLabelValues(errorComponent).Inc()
	}
}

// ObserveCosignAttestDuration mirrors ObserveCosignSignDuration for attest.
func ObserveCosignAttestDuration(keyless bool, seconds float64) {
	mode := strconv.FormatBool(keyless)
	CosignAttestDuration.WithLabelValues(mode).Observe(seconds)
}

// ObserveRekorURLPersist increments the appropriate bucket of
// ReleaseAttestationRekorURLPersist based on whether a non-empty URL
// landed in the row. Empty (including the "unknown" sentinel removed
// in M07 Ralph iter 5) counts as "null".
func ObserveRekorURLPersist(rekorURL string) {
	if rekorURL == "" || rekorURL == "unknown" {
		ReleaseAttestationRekorURLPersist.WithLabelValues("null").Inc()
		return
	}
	ReleaseAttestationRekorURLPersist.WithLabelValues("populated").Inc()
}

// ObserveFulcioCertIssuance records a Fulcio cert issuance duration.
//
// CURRENT WIRING STATE (2026-05-28): no production code calls this
// wrapper. The metric was declared + dashboarded (T4.1 Panel 32 of
// sigstore-pipeline.json) under the assumption that the cosign sign
// script would surface a `fulcio_cert_issuance_ms` output param —
// that's the operator follow-up listed in the T4.1 commit message
// (f3ea35d2) as tasks #3+#4 of plan
// 2026-05-28-meet-001-engineering-closure.
//
// The wrapper exists today so:
//  1. The metrics-to-emit-mirror defensive test
//     (TestSigstoreMetrics_AllDeclaredHaveWrapper) finds a wrapper
//     for every declared metric, catching the orphan-metric class.
//  2. Operator can wire `ObserveFulcioCertIssuance(seconds)` at the
//     cosign script output extraction site once the duration is
//     surfaced — no need to invent the API at LIVE-instrumentation
//     time.
//
// Negative or implausible durations are silently dropped per the
// "trust the caller's guard at the emission site" contract documented
// for the other Observe* wrappers (EC-NEW of plan T4.1).
func ObserveFulcioCertIssuance(durationSeconds float64) {
	if durationSeconds < 0 {
		return
	}
	FulcioCertIssuanceDuration.Observe(durationSeconds)
}
