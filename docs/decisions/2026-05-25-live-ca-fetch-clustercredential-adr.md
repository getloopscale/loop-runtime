# ADR — Live CA Fetch from `kube-root-ca.crt` on ClusterCredential Reconcile

**Date:** 2026-05-25 (engineering); 2026-05-26 (LIVE evidence)
**Tier:** T2
**Status:** RATIFIED (sponsor-direct)
**Meeting:** [MEET-2026-05-25-op-b-live-ca-fetch](../../.claude/knowledge-base/meetings/MEET-2026-05-25-op-b-live-ca-fetch.md)
**Plan:** [docs/plans/2026-05-25-live-ca-fetch-clustercredential-plan.md](../plans/2026-05-25-live-ca-fetch-clustercredential-plan.md) v1.2
**LIVE evidence:** [reviews/2026-05-26-op-b-live-evidence](../../.claude/knowledge-base/reviews/2026-05-26-op-b-live-evidence.md)

## Context

Issue #67 surfaced that the `ClusterCredentialReconciler` writes the ArgoCD cluster Secret's `caData` from the kubeconfig snapshot stored in Secret `cell-X-kubeconfig` at bootstrap time. When the target cluster's API-server CA rotates (k3s recreate, prod CA rollover, k3d server-ca rebuild), the snapshot becomes stale and ArgoCD app-controller hits `x509: certificate signed by unknown authority` until the snapshot is manually refreshed via heal block H19 or `k3d:heal`.

Op A (commit `40580a0b`, pre-this-ADR) ships a declarative workaround: setting `spec.tls.insecure=true` for k3d-local ClusterCredentials makes ArgoCD ignore `caData` entirely. This solves the immediate Issue #67 but disables TLS verification — acceptable in dev, unacceptable in prod where the same operator code runs.

**The problem this ADR solves:** allow the operator to **self-heal** the staleness without disabling TLS, on every reconcile.

## Decisions

### D1 — Live fetch from `default/kube-root-ca.crt` on every reconcile

The reconciler fetches the target cluster's CA from the auto-rotated `default/kube-root-ca.crt` ConfigMap (Kubernetes 1.21+, signed by the same CA that signs SA tokens — guaranteed to be the API-server CA), instead of using the snapshot from the kubeconfig Secret.

**Rationale:**
- `kube-root-ca.crt` is K8s' own auto-rotated source of truth — always reflects the current CA.
- No cache + TTL = no staleness window, no invalidation logic, KISS.
- Cost: one in-cluster ConfigMap GET per reconcile (≤200 bytes, ≤5s timeout). Reconcile interval is 60s steady-state, so worst-case is 1 GET/min/cluster. Negligible.

**Rejected alternatives:**
- (i) Cache + TTL — adds invalidation logic, doesn't help during rotation window.
- (ii) Watch on `default/kube-root-ca.crt` — adds informer cost, doesn't help if operator restarts mid-rotation.
- (iii) Manual operator command (`loopzero admin cells refresh-ca`) — preserves tribal knowledge, doesn't auto-heal.

### D2 — Graceful fallback to snapshot caData

If the live fetch fails (RBAC denial, network partition, ConfigMap missing in target cluster), the reconciler falls back to the existing kubeconfig snapshot caData. Fallback emits a structured `slog.Warn` with `audit:"ca_fetch_fallback"` field + increments counter `loopzero_clustercredential_ca_fetch_total{result="fallback"}`.

**Rationale:**
- Degrades to current (pre-Op-B) behavior — no regression risk.
- SRE observability: a sustained `result="fallback"` rate indicates the target cluster's RBAC or topology is misconfigured, surfaces as Grafana alert.

### D3 — Prometheus counter with 3 result labels

`loopzero_clustercredential_ca_fetch_total{cluster, result}` where `result ∈ {success, fallback, skipped-insecure}`.

**Rationale:**
- SRE can pivot by cluster + result.
- `skipped-insecure` distinguishes Op A short-circuit (declarative bypass) from Op B success.
- Counter is the data source for T5.0 pre-flight gate that unblocks H19/H19a heal-block deletion in T5.1.

### D4 — Op A precedence (short-circuit when `insecure=true`)

When `cr.Spec.TLS.Insecure==true`, the reconciler MUST short-circuit `resolveCAData` without invoking the fetcher (returns `"", "skipped-insecure"`). Counter is incremented for observability.

**Rationale:**
- ArgoCD ignores `caData` entirely when `insecure=true` — the fetch would be wasted I/O.
- In dev clusters where RBAC may legitimately block `kube-root-ca.crt` GET, the fetch would emit noisy `slog.Warn` for no value.
- Op A is the deliberate operator decision per Issue #67 closure — Op B is the long-term self-heal, Op A is the current sponsor-approved precedence for k3d-local.

### D5 — Bootstrap circular dependency mitigation (EC-13)

The `kubeRootCAFetcher.FetchClusterCA` implementation MUST clone the incoming `restConfig` with `TLSClientConfig{Insecure: true}` for the GET. Otherwise:
- Stale snapshot CA in restConfig.TLSClientConfig.CAData → TLS handshake against target API-server fails first → fetch never executes → fetcher cannot refresh the very snapshot that's blocking it.

This is the "X fetcher uses X" trap (now codified as a Known Systemic Pattern in CLAUDE.md).

## Consequences

**Positive:**
- Operator self-heals CA rotation transparently. No manual `k3d:heal` H19/H19a or `loopzero admin cells refresh-ca` needed steady-state.
- Op A and Op B coexist: insecure=true short-circuits, insecure=false uses live fetch + fallback.
- Counter is binary observability into reconcile health for SRE.

**Negative / Trade-offs:**
- One ConfigMap GET per reconcile per cluster (negligible cost, but adds to budget if `RequeuePeriodicCheck` is tuned aggressively).
- Fallback path on RBAC-denial dev clusters: log noise (mitigated by `audit_insecure_bootstrap:"true"` field — operators can filter).
- Bootstrap insecure GET is a per-fetch trust trade — fetcher MUST limit scope to default-namespace kube-root-ca.crt only.

**Constraints introduced:**
- New file `operators/internal/platform/ca_fetcher.go` MUST be kept ≤100 lines (KISS — see plan T1.1 acceptance criteria).
- `clustercredential_controller.go::resolveCAData` cyclomatic complexity MUST stay ≤10.
- Any future operator that fetches credentials from a target cluster MUST follow the EC-13 pattern (insecure bootstrap clone).

## Status of Phase 5.1 (H19/H19a heal deletion)

**Deferred to operator HARD STOP gate.** Must satisfy:
1. T5.0 pre-flight script (`infra/scripts/local/validate-op-b-soak.sh --days 7`) reports zero `result="fallback"` increments in the past 7 days.
2. Cleanroom + `/dogfood full` re-validation post-deletion.

7-day soak is real elapsed-time evidence; cannot be short-circuited.

## Cross-references

- Plan: `docs/plans/2026-05-25-live-ca-fetch-clustercredential-plan.md` v1.2
- Edge review: `docs/plans/2026-05-25-live-ca-fetch-clustercredential-edge-review.md`
- LIVE evidence: `.claude/knowledge-base/reviews/2026-05-26-op-b-live-evidence.md`
- Concept: `.claude/knowledge-base/concepts/operations/live-vs-snapshot-credentials.md`
- Meeting: `.claude/knowledge-base/meetings/MEET-2026-05-25-op-b-live-ca-fetch.md`
- Code: commits `37df88c1` (impl) + `0deec39b` (metrics fix) + `8fb7fb4a` (plan v1.2) + `73c54a59` (LIVE evidence)
- Pattern in CLAUDE.md: "Live vs snapshot credentials" + "Bootstrap circular dependency (X-fetches-X)" + "Validation procedure proves nothing"
- Related sponsor-direct pattern: MEET-2026-05-13 orphan-crd-deletion
