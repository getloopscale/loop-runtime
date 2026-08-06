# Implementation Summary — m14-kserve-gaps-closure

**Milestone:** M14 — KServe Cross-Validation Gaps Closure
**Plan:** `.claude/knowledge-base/plans/m14-kserve-gaps-closure-plan.md` (v1.1, SHIPPABLE 98.8)
**Date:** 2026-07-06
**Branch:** develop
**Verdict:** IMPLEMENTATION_COMPLETE (6/6 gaps GREEN, integration validated)

## Per-gap wiring triad

| Gap | Task | Commit | Caller (pillar a) | Integration test (pillar b) | Runtime signal (pillar c) |
|---|---|---|---|---|---|
| #1 runtime registry | T3.1 | `bdcf79f0` | `ResolveStrategy` (4 prod callers: scripts/buildkit/workflow_builder/build_plan) resolve via `runtimeRegistry` | `TestRuntimeRegistry_IsDataDriven`, `TestResolveStrategy_ExactMatchParityWithLegacy` | `slog.Warn` on unsupported runtime (fail-closed observability, preexisting) |
| #2 admission VAP | T1.1 | `474adc26` | VAP rendered by admission-policies chart (Helmfile L2); enforced by API server | `TestTenantVAP_*` (7 tests) evaluate the REAL CEL via cel-go, mutation-proven | VAP Deny action = admission audit event |
| #3 chart content-diff | T2.1 | `3b45b8c9` | `Check` (consumed by `quality_gates_helm.go:66`) calls `diffChartContent` | `TestContentDiff_*` (4 tests); gate PASS on real repo | gate exit code + printed diff |
| #4 conversion Hub | T6.1 | `9492e176` | `Tenant.Hub()` marker (controller-runtime conversion contract); harness `roundTripThroughHub` | `TestTenantConversion_*` (3 tests) | (dormant — activation runbook `docs/operations/crd-conversion-procedure.md`) |
| #5 ObjectStore | T4.1 | `57989e50` | `NewS3ObjectStore(*s3.Uploader)` seam (domain interface, DIP) | `TestS3Adapter_*` (4 tests) | error propagation (no swallow) verified EC-4 |
| #6 payload logger | T5.1 | `90397e92` | `Dispatcher.Collector()` seam (dormant wiring, Unresolved Q2) | `TestDispatcher_*` (5 tests, -race) | `loopzero_payload_logger_dropped_total` counter (metric proof: == 2 in full-queue test) |

## Edge cases absorbed (from edge-case review)

- **EC-1** (missing environments): CEL `has()`-guard — `TestTenantVAP_HandlesMissingEnvironments`
- **EC-2** (chart missing one side): asymmetry-only violation — `TestContentDiff_OneSideMissing`
- **EC-3** (exact-match parity): registry preserves switch semantics — `TestResolveStrategy_ExactMatchParityWithLegacy`
- **EC-4** (error propagation): thin adapter no-swallow — `TestS3Adapter_UploadPropagatesError`
- **EC-5** (scrub no-op): identity scrub — `TestDispatcher_ScrubNoPIINoOp`
- **EC-6** (atomic counter): 200 goroutines under -race — `TestDispatcher_AtomicCounterInvariant`
- **EC-7** (nil optionals round-trip): zero-value preservation — `TestTenantConversion_RoundTripWithNilOptionals`

## Deviations from plan (honest)

1. **`languages_test.go` was NOT (NEW)** — the plan marked it new but the file existed; the registry tests were appended to it. No behavior impact.
2. **Gap #2 tested via cel-go, not envtest** — the plan T1.1 said envtest. I evaluated the ACTUAL CEL expressions extracted from the rendered VAP YAML using `cel-go` (the exact engine the API server runs), which (a) is hermetic (no apiserver ValidatingAdmissionPolicy feature-gate config dependency), (b) is coupled to the real artifact (mutation-proven: breaking the regex in the YAML fails the test), and (c) directly exercises EC-1. This tests the security-critical logic more reliably than envtest would. The existing operators/internal/tenant envtest suite still runs green alongside (35s), proving coexistence.
3. **Metric wiring for gap #6 is dormant** — per plan Unresolved Q2 + ADR D3, the payload logger ships the mechanism + tests; real request-path wiring is gated on a compliance requirement (same posture as the gap #4 conversion webhook). The `Collector()` seam is the documented activation point.

## Integration Validation (Phase 7)

- `cd api && go build ./...` → exit 0
- `cd api && go test ./internal/{argo,objectstore,payloadlog}/` → all PASS
- `cd operators && go build ./...` → exit 0
- `cd operators && go test ./api/v1/ ./internal/tenant/` → all PASS (incl. existing envtest suite)
- `cd cmd/loopzero-ops && go test ./internal/quality/helmoverlapparity/` → PASS
- `go vet` on all changed packages → clean
- `loopzero-ops quality helm-overlap-parity --root <repo>` → PASS (with new content-diff)
- Metric proof: `loopzero_payload_logger_dropped_total == 2` in `TestDispatcher_FullQueueDropsCounted`

## Wiring framing correction (per review-tests-wiring finding)

Honest correction to any "6/6 wired" reading: **3/6 gaps are wired to production**
(gap #1 ResolveStrategy registry with real callers; gap #2 VAP rendered+enforced;
gap #3 quality gate). **3/6 are dormant-by-design with tested seams** (gap #4
conversion Hub, gap #5 ObjectStore interface, gap #6 payload logger) — each has a
test exercising its seam but NO production caller yet, gated on future
requirements per ADRs D4/D5/Q2. The full wiring triad (prod caller + runtime
metric in prod) is intentionally NOT claimed for those three.

Also corrected during review: gap #3's securityContext parity was a values.yaml
no-op (securityContext is hardcoded in templates, not values.yaml); fixed to
compare helm-RENDERED output (`rendered_diff.go`). See the review report.

## Deps

Zero new external dependencies (deps-audit PASS_WITH_CAVEATS). `cel-go` (gap #2) and `controller-runtime`/`prometheus` were already in the module graph; `go mod tidy` only promoted existing indirect deps to direct.
