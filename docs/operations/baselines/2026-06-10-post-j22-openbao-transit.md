# Steady-State Baseline — Post J-22 OpenBao Transit + ESO Sync (engineering-side)

**Date:** 2026-06-10
**Trigger:** T6.1 of plan `2026-06-10-j22-openbao-transit-eso-sync` v1.2 — Final Phase Integration Validation
**Companion JSON:** [`2026-06-10-post-j22-openbao-transit.json`](./2026-06-10-post-j22-openbao-transit.json)
**Pre-fix baseline:** [`2026-06-10-pre-j22-openbao-transit.md`](./2026-06-10-pre-j22-openbao-transit.md)

This baseline closes the engineering side of the OpenBao Transit + ESO Sync chain. Diff against the pre-fix baseline shows what shipped via 15 ralph-loop iterations across Phases 0-5 (T0.1 through T5.2). Phase 6 LIVE gates (T4.1 zero-leak 6/6 + sponsor browser walkthrough) remain operator HARD STOPs.

## Layer 1 — Code state

| Field | Pre-fix | Post-fix |
|---|---|---|
| HEAD commit | `3a393d4f` (T0.1 ship-the-gate) | `31d798c9` (T5.2 taskfile) |
| Branch | worktree-glowing-growing-babbage | worktree-glowing-growing-babbage |
| Commits ahead of develop | 352 | **365 (+13)** |
| Commits behind develop | 47 | 47 |

**Commits shipped during this plan (Phase 0 → Phase 5, in order):**

```
3a393d4f feat(openbao-transit): T0.1 OpenBao prereqs check script + hermetic test
6f692737 docs(openbao-transit): T0.2 steady-state baseline pre-implementation
58fc733c feat(openbao-transit): T1.1 Bulk wires k8sSync per-env + EC-01/EC-02 absorption
5eaf3ec3 feat(openbao-transit): T1.2 cross-tenant binding 404 + EC-03 absorption
7756e21f feat(openbao-transit): T3.1 zeroize + T2.1 transit_client + EC-04 singleflight
70e27f19 feat(openbao-transit): T2.2 secret_wrapper dispatch tests + adapter bridge
c021ba87 feat(openbao-transit): T2.3 Bulk transit.Encrypt + EC-06 auto-create key
850ba3db feat(openbao-transit): T2.4 ESO ClusterSecretStore openbao-transit + hermetic test
397b8930 feat(openbao-transit): T2.5 Config fail-loud + EC-05 crypto-mode precedence
f0475e58 feat(openbao-transit): T3.2 log redaction filter for vault patterns
216cb3a0 feat(openbao-transit): T3.3 NetworkPolicy egress loopzero-api → vault-system
caa688ba feat(openbao-transit): T3.4 per-tenant rate limiter + EC-08 LRU eviction
0f2ce5c4 feat(openbao-transit): T4.1 LIVE 6-scenario zero-leak + T4.2 hermetic test
31d798c9 feat(openbao-transit): T5.1 + T5.2 dogfood wrapper + taskfile entry
```

## Layer 2 — Runtime state (unchanged on engineering side)

| Resource | Pre-fix | Post-fix |
|---|---:|---:|
| Helm releases (mgmt cluster) | 20 | 20 |
| Running pods | 60 | 60 |
| ArgoCD Apps | 4 | 4 |
| Tenants (PG) | 3 | 3 |

Engineering shipped CODE + manifests but the operator-side LIVE apply (helmfile sync of the new ClusterSecretStore `openbao-transit`, env-var flip of `OPENBAO_TRANSIT_ENABLED=true` on loopzero-api, AppRole credential provisioning) has NOT happened in this baseline run. The post-fix runtime state is the same as pre-fix; this is expected.

## Layer 3 — Config state

| Setting | Pre-fix | Post-fix |
|---|---|---|
| `cosign_mode` | keyed | keyed (unchanged) |
| `pg_max_migration` | 32 | 32 (no schema migrations) |
| `plan_limits_present` | true | true |
| `LOOPZERO_SECRET_ENCRYPTION_KEY` (loopzero-api env) | UNSET | UNSET — same state; T2.5 D6 ADR fail-loud at boot still permits empty in dev mode |
| `OPENBAO_TRANSIT_ENABLED` (loopzero-api env) | UNSET | **schema added** — Config.OpenBaoTransitEnabled exists in code; env var not yet set on running deployment (operator step) |

## Layer 4 — LIVE functional probes (unchanged)

5 of 6 HTTPS probes returned 200 — same as pre-fix:

| Endpoint | Pre-fix | Post-fix |
|---|---:|---:|
| `https://app-dev.loopzero.example/` | 200 | 200 |
| `https://app-dev.loopzero.example/api/v1/projects` | 200 | 200 |
| `https://app-dev.loopzero.example/api/v1/environments` | 200 | 200 |
| `https://app-dev.loopzero.example/health/deep` | 200 | 200 |
| `https://cloud-dev.loopzero.example/api/v1/projects` | 200 | 200 |
| `https://cloud-dev.loopzero.example/api/v1/webhooks/github` | 401 | 401 (HMAC by design) |

## Phase prereqs verification (T0.1 hermetic re-check)

`bash infra/scripts/local/check-openbao-prereqs.sh --hermetic --json` post-fix output:

```json
{"verdict":"FAIL","phase1":"committed","phase2":"committed","phase3":"committed","phase4":"absent"}
```

**Delta from pre-fix:** Phase 3 moved from `absent` → `committed` (transit_client.go now exists in pkg/vault/). Phase 4 stays `absent` because the ClusterSecretStore template exists but no `helmfile sync` has applied it to the workload cluster yet — that's an operator step.

## Engineering closure

All 18 tasks of plan v1.2 shipped engineering-side:

| Phase | Tasks | Status |
|---|---|---|
| 0 | T0.1 + T0.2 | ✅ Closed |
| 1 | T1.1 + T1.2 (EC-01/02/03) | ✅ Closed |
| 2 | T2.1 + T2.2 + T2.3 + T2.4 + T2.5 (EC-04/05/06) | ✅ Closed |
| 3 | T3.1 + T3.2 + T3.3 + T3.4 (EC-07/08) | ✅ Closed |
| 4 | T4.1 + T4.2 | ✅ Closed |
| 5 | T5.1 + T5.2 | ✅ Closed |
| 6 | T6.1 (this baseline) | 🟡 **Engineering closure complete; operator HARD STOPs pending** |

## Operator HARD STOPs (cannot complete in this session)

The plan's `<promise>IMPLEMENTATION_COMPLETE</promise>` per Final Phase Integration Validation requires the following operator-side validations that cannot run hermetic / cannot run without sponsor:

1. **`helmfile sync` of ClusterSecretStore openbao-transit** to the workload cluster, with AppRole role_id + secret_id provisioned
2. **Flip `OPENBAO_TRANSIT_ENABLED=true`** on loopzero-api Deployment env + restart pods
3. **Run T4.1 LIVE 6-scenario zero-leak** against dev-public: `bash infra/tests/e2e/dev-public-secrets-zero-leak_test.sh` → must return `verdict=PASS pass=6 fail=0`
4. **Sponsor LIVE browser walkthrough**: add secret via dashboard UI → confirm pod env reflects plaintext via `kubectl exec` → confirm PG row is vault:v1: prefix

Per Unbreakable Rule 3 (extreme honesty): the `IMPLEMENTATION_COMPLETE` promise is NOT emitted by this iteration. Engineering side is complete; LIVE gates are the canonical operator decision.

## Hermetic test coverage (CI-grade green)

`bash infra/scripts/local/dogfood-secrets-openbao.sh --hermetic-only --json` post-fix output:

```json
{
  "verdict": "PASS",
  "pass": 3,
  "fail": 0,
  "suites": [
    {"name": "syntax", "verdict": "PASS", "detail": "(38 assertions)"},
    {"name": "pkg-vault", "verdict": "PASS", "detail": "(31 specs race-clean)"},
    {"name": "api-routes", "verdict": "PASS", "detail": "(14 TestSecretBulk specs)"}
  ]
}
```

This is the canonical CI gate. The 4th suite (T4.1 LIVE) is gated by operator HARD STOP and short-circuits to SKIPPED in CI mode.

## Cross-references

- Rule: [`.claude/rules/steady-state-baseline.md`](../../../.claude/rules/steady-state-baseline.md)
- Plan: [`.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md`](../../../.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md) v1.2 § T6.1
- Edge-case review: [`.claude/knowledge-base/reviews/2026-06-10-j22-openbao-transit-eso-sync-edge-cases-2026-06-10.md`](../../../.claude/knowledge-base/reviews/2026-06-10-j22-openbao-transit-eso-sync-edge-cases-2026-06-10.md)
- Deps audit: [`.claude/knowledge-base/audits/2026-06-10-j22-openbao-transit-eso-sync-deps-audit-2026-06-10.md`](../../../.claude/knowledge-base/audits/2026-06-10-j22-openbao-transit-eso-sync-deps-audit-2026-06-10.md)
- Pre-fix baseline: [`2026-06-10-pre-j22-openbao-transit.md`](./2026-06-10-pre-j22-openbao-transit.md)
- Companion JSON: [`2026-06-10-post-j22-openbao-transit.json`](./2026-06-10-post-j22-openbao-transit.json)
- Index: [`index.md`](./index.md)
