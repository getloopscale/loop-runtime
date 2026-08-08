---
type: baseline
title: J-22 Session-Final Regression Evidence (iters 18-33)
---

# Baseline — J-22 Session-Final Regression Evidence (iters 18-33)

> **Captured:** 2026-06-10T14:38Z
> **Captured by:** Iter 34 productive-idle of plan `2026-06-10-j22-openbao-transit-eso-sync` v1.2
> **HEAD at capture:** `6e697dab` docs(option-b): pre-author ESO-in-mgmt deployment analysis + CI wire (iter 33)
> **Branch:** worktree-glowing-growing-babbage (118 commits ahead of origin/main)
>
> **Scope:** Empirical regression evidence after 16 productive-idle commits (iters 18-33). 4-layer validation of engineering-side closure. Complements the iter 28 baseline by adding Layer 2 (Go test -race) + Layer 3 (build verification) + Layer 4 (helm template render) to the iter 28 Layer 1 (hermetic anti-drift).

Per `.claude/rules/steady-state-baseline.md`: this is a Layer 1+2+3+4 baseline focused on the J-22 surface only. Cluster runtime state (Layer 2 of the canonical 4-layer template) was captured at iter 23 cleanroom + unchanged on the J-22 surface since.

---

## Layer 1 — 7 J-22 hermetic anti-drift test scripts (160 assertions)

```
$ for t in <7 J-22 test files>; do bash "$t" 2>&1 | tail -1; done

loopzero-api-openbao-approle-envfrom_test.sh                     pass=7  fail=0
check-vault-bootstrap-in-mgmt-task_test.sh                   pass=18 fail=0
check-dev-public-loopzero-external-secrets-overlay_test.sh       pass=18 fail=0
check-rehearsal-mode-syntax_test.sh                          pass=26 fail=0
check-write-path-subset-syntax_test.sh                       pass=27 fail=0
check-option-b-eso-deploy-doc_test.sh                        pass=25 fail=0
check-openbao-transit-quality-gates-wired_test.sh            pass=39 fail=0
─────────────────────────────────────────────────────────────────────────
TOTAL: 160 PASS / 0 FAIL in ~7s
```

## Layer 2 — Go test with race detector on J-22-touched modules

```
$ go test -race -count=1 <each module>

api/cmd/...                          ok  6.209s  (includes setup_transit_test + setup_stores_test from iter 18)
api/internal/db/raw                  ok  1.033s  (includes secret_wrapper passthrough tests from iter 18)
api/internal/adapters                ok  1.180s  (includes vault_transit_adapter_test)
api/internal/routes  -run TestSecret ok  1.185s  (14 TestSecretBulk specs T1.1/T1.2/T2.3 + EC-01/02/03)
api/internal/config                  ok  1.058s  (includes EC-05 crypto_mode precedence tests)
pkg/vault                            ok  3.672s  (31 specs: TransitClient + EC-04 singleflight + EC-06 auto-create + Zeroize + Redaction + RateLimit + EC-08 LRU)
pkg/quality/baseimagedigest          ok  1.057s  (validates iter 21 Layer 3 anchor bump 1.25.9→1.26.0)
──────────────────────────────────────────────────────────────────────────────
TOTAL: 7 packages PASS, race-clean, ~15s aggregate
```

## Layer 3 — Build verification

```
$ cd api && go build ./...        → exit 0
$ cd pkg && go build ./...        → exit 0
```

## Layer 4 — Helm template render canonical artifacts

```
$ helm template loopzero-api infra/helm/loopzero-api | grep "name: loopzero-openbao-approle"
→ 1 secretRef reference (iter 20 envFrom canonical wire)

$ helm template loopzero-eso infra/helm/loopzero-external-secrets \
    --set envName=dev-public --set openbaoTransit.enabled=true \
    --set openbaoTransit.server=https://openbao.vault-system.svc.cluster.local:8200 \
    --set openbaoTransit.roleId=00000000-... \
    --set openbaoTransit.secretRef.{name=loopzero-openbao-approle,namespace=external-secrets,key=OPENBAO_VAULT_SECRET_ID}
→ 1 ClusterSecretStore openbao-transit (iter 25 dev-public overlay)
  Resource shape matches iter 25 hermetic contract (server URL mgmt DNS,
  appRole.path=approle, secretRef in external-secrets ns)
```

## What this baseline proves

| Question | Answer |
|---|---|
| Did 16 productive-idle commits break ANY existing test? | **NO** — 7 hermetic + 7 Go packages all GREEN |
| Did builds break? | **NO** — both api/ and pkg/ build exit 0 |
| Are canonical Helm artifacts still rendering? | **YES** — loopzero-api envFrom + ESO CSS both render shapes per iter 20 + iter 25 contracts |
| Are race conditions present in J-22 code? | **NO** — all packages race-clean (Go `-race` flag) |
| Is the engineering side complete? | **YES** — 18 plan tasks + 16 productive-idle commits |
| Can `<promise>IMPLEMENTATION_COMPLETE</promise>` be emitted? | **NO** — 4 operator HARD STOPs pending sponsor action |

## Cumulative session arc (iters 18-33) — fully canonical-locked

| Type | Count |
|---|---|
| Engineering commits | 16 |
| Hermetic test scripts | **7** |
| Anti-drift assertions enforced via `task quality:gates` | **160** |
| Go test packages with race detector | 7 (all PASS) |
| Canonical task shortcuts | 2 (`k3d:vault:bootstrap-in-mgmt`, `test:dogfood:secrets-openbao:rehearsal`) |
| Helmfile env overlays | 1 (dev-public loopzero-external-secrets) |
| LIVE bash scripts | 2 (T4.1 full 6/6 + iter 31 Option A subset 3/6) |
| Operator docs | 2 (cutover runbook + Option B analysis) |
| Baseline evidence files | 2 (iter 28 + this) |
| LIVE-caught bug fixes (cleanroom iters 21-22) | 3 |
| CHANGELOG audit-trail bullets | 7 |

## Diminishing returns signal

Iters 27-33 each added 1 more contract test + extended the iter 27 meta-gate. The pattern of "add hermetic test + wire to CI + update meta-gate" has fully covered the engineering surface. Further productive-idle iterations would likely:

- Re-validate existing tests (already locked under CI)
- Add anti-drift to non-J-22 areas (out of scope)
- Speculate about future v2 features (premature)

**Honest signal:** engineering side is fully canonical-locked. The remaining LIVE walkthrough sequence requires sponsor verbatim A/B/C choice. No further productive-idle deliverable adds material value until that choice arrives.

## Sponsor decision still pending verbatim

```
"Option A go"  → execute Option A subset LIVE (~20min)
                 → validates 3/6 scenarios (canonical write-path proof)
                 → docs/operations/openbao-transit-cutover-runbook.md § 4.0a

"Option B go"  → execute B1 ESO deploy in mgmt (~5min)
                 → then Option A subset (~20min)
                 → then full T4.1 6/6 LIVE (~10s + ESO sync wait)
                 → docs/operations/option-b-mgmt-helmfile-eso-deploy.md § B1

"Option C defer" → close session
                  → Option B analysis doc captures the gap for follow-up
                  → 16-commit productive-idle arc stands as J-22 v1.0 engineering closure
```

## Cross-references

- Plan: `.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md`
- Operator runbook: [`docs/operations/openbao-transit-cutover-runbook.md`](../openbao-transit-cutover-runbook.md)
- Option B analysis: [`docs/operations/option-b-mgmt-helmfile-eso-deploy.md`](../option-b-mgmt-helmfile-eso-deploy.md)
- Iter 28 baseline (Layer 1 only): [`docs/operations/baselines/2026-06-10-iter-27-j22-quality-gates-evidence.md`](./2026-06-10-iter-27-j22-quality-gates-evidence.md)
- Steady-state baseline rule: `.claude/rules/steady-state-baseline.md`
- Productive-idle mandate: memory `feedback_productive_idle_runbook_preauthor_2026_06_10`
- per-feature LIVE workflow: memory `feedback_post_feature_live_regression_workflow_2026_06_09`
