---
type: review
title: "Edge Case Review — 2026-06-11-cleanroom-canonical-5-defense-layers"
---

# Edge Case Review — 2026-06-11-cleanroom-canonical-5-defense-layers

Date: 2026-06-11
Tasks analyzed: 22 (across Phases 1-6)
Edge cases found: 37 (MUST FIX: 19, SHOULD TEST: 11, DOCUMENT: 7)

## Philosophy applied

- Plano toca 6 categorias de boundary: yaml templating, shell scripts (Bash), Go AST static analysis, bash LIVE E2E orchestration, kubectl probes cross-cluster, sponsor walkthrough JUNTOS
- Edge cases concentrados em Phase 2 (script idempotency + recovery) + Phase 4 (check sprint runtime + reachability) + Phase 5 (LIVE E2E test data isolation)
- Phase 1 (Helmfile overlays) e Phase 3 (Go AST gates) têm poucos edges — declarative + AST tem boundaries claras

---

## MUST FIX (19 items — sub-tasks ou ADR amendments para plan v1.1)

### EC-1: Hermetic test must assert canonical URL exact match per env across all 3 envs

- **Affected task:** T1.1 (Hydra urls overlay)
- **Family:** Format / Boundary
- **Scenario:** Atualmente Task 1.1 lista 3 envs (standalone-multi, dev-public, prod) mas Task 3.1 hermetic test só asserts dev-public. Standalone-multi cluster-internal e prod canonical são distintos.
- **Impact:** Catch #14 regredido em standalone-multi ou prod sem detectar.
- **Suggested fix:** Task 3.1 hermetic test deve loopar sobre os 3 envs com expected URL canonical per env.

### EC-2: SPKI Secret population timing — Pod may boot before Secret materialized

- **Affected task:** T1.4 (SPKI extraction marker)
- **Family:** Timing / State
- **Scenario:** loopzero-api Pod inicia antes do `task vault:bootstrap-dev` completar SPKI extraction; ExternalSecret sync ainda Pending; pod crashes on missing env var.
- **Impact:** CrashLoopBackOff até manual restart pós-bootstrap.
- **Suggested fix:** Adicionar Task 1.4 sub-step: chart values declaram `optional: true` em secretKeyRef + loopzero-api code lida com env empty/missing graciosamente (retry com backoff).

### EC-3: Kratos cookies.domain mismatch with Caddy edge hostname

- **Affected task:** T1.6 (Kratos cookies.domain)
- **Family:** Format / Integration
- **Scenario:** Overlay declara `cookies.domain = loopzero.example` mas standalone-multi env não tem Caddy edge (rodou apenas em localhost). Cookies não persistem → session loss.
- **Impact:** Sponsor walkthrough fail intermitente em envs sem edge.
- **Suggested fix:** Task 1.6 deve gate cookies.domain per env: omit em standalone-multi (no edge), populate em dev-public + prod. Hermetic test Task 3.x asserts per-env contract.

### EC-4: `bao operator init` race — script may run twice em concurrent invocations

- **Affected task:** T2.1 (vault:bootstrap-dev)
- **Family:** Concurrency / State
- **Scenario:** Operator runs `task setup` twice in parallel; first call inits OpenBao, second crashes "already initialized".
- **Impact:** Confusing error, sponsor needs debug.
- **Suggested fix:** Script checks `bao status -format=json | jq .initialized` FIRST + no-op + log "already initialized, skipping init step". Flock pid file complement.

### EC-5: Shards file `~/.loopzero/cleanroom-unseal.json` corrupted or deleted

- **Affected task:** T2.1
- **Family:** State / Resource
- **Scenario:** Operator accidentally `rm ~/.loopzero/cleanroom-unseal.json`; subsequent `task vault:bootstrap-dev` cannot unseal.
- **Impact:** OpenBao permanently sealed without recovery path; full re-init required.
- **Suggested fix:** Script detects missing shards file + emits explicit error: "Shards missing. Recovery path: (a) restore from backup OR (b) `bash scripts/local/openbao-full-reinit.sh` (destructive)".

### EC-6: `LOOPZERO_DEV_AUTO_INIT_VAULT` not set in non-dev env triggers prod unsafe init

- **Affected task:** T2.1
- **Family:** Permission / Security
- **Scenario:** Operator runs `task vault:bootstrap-dev` in prod env without env gate.
- **Impact:** Production OpenBao destroyed + shards leaked to operator filesystem unprotected.
- **Suggested fix:** Script first-line check: `[ "$LOOPZERO_DEV_AUTO_INIT_VAULT" = "true" ] || { echo "REFUSED — dev-only task. Set LOOPZERO_DEV_AUTO_INIT_VAULT=true em dev env."; exit 2; }`.

### EC-7: `~/.loopzero/dev-credentials.json` missing — script crashes deep stack

- **Affected task:** T2.2 (secrets:seed-dev)
- **Family:** Input / Resource
- **Scenario:** Operator runs `task secrets:seed-dev` without first creating creds file.
- **Impact:** Unclear error in deep jq pipeline.
- **Suggested fix:** Script first-line: `[ -f ~/.loopzero/dev-credentials.json ] || { echo "Missing ~/.loopzero/dev-credentials.json. Create with shape: <link to docs>"; exit 1; }`.

### EC-8: `~/.loopzero/dev-credentials.json` malformed JSON / missing required keys

- **Affected task:** T2.2
- **Family:** Format / Input
- **Scenario:** JSON has typo, missing `github.app_id`, ou unexpected structure.
- **Impact:** Partial writes to OpenBao kv-v2 → broken downstream chain visible only at Cleanroom.
- **Suggested fix:** Validate JSON schema upfront via jq: `jq -e '.github.app_id and .github.private_key and .github.webhook_secret and .stripe.secret and .resend.api_key' ~/.loopzero/dev-credentials.json || exit 1`.

### EC-9: CoreDNS patch must read current Docker IPs dynamically — NOT hardcoded

- **Affected task:** T2.3 (cross-cluster-coredns)
- **Family:** State / Boundary
- **Scenario:** k3d ephemeral container IPs change between Cleanroom runs; hardcoded `172.18.0.X` invalidates next run.
- **Impact:** Catch #11 returns within 1 Cleanroom cycle.
- **Suggested fix:** Script reads each cluster LB IP via `docker inspect k3d-loopzero-<cluster>-serverlb --format '{{(index .NetworkSettings.Networks "k3d-loopzero").IPAddress}}'` at runtime + patches NodeHosts with detected values.

### EC-10: Port-forward dies mid-walkthrough — sponsor sees 502

- **Affected task:** T2.4 (port-forward auth services)
- **Family:** Timing / Resource
- **Scenario:** Network blip OR k3d pod restart drops port-forward; sponsor walks into 502 mid-test.
- **Impact:** Sponsor walkthrough blocked.
- **Suggested fix:** Mirror existing `port-forward-public-dashboard.sh` allowlist pattern: watchdog loop + exponential backoff (2s→30s caps) + signal trap + PID file (per allowlist entry justifying T7.3 of 2026-05-29 dashboard plan).

### EC-11: `task check` kubectl unavailable in CI environment

- **Affected task:** T4.1 (check-sprint orchestrator)
- **Family:** Resource / Integration
- **Scenario:** CI runner runs hermetic tests only (no k3d cluster); check sprint blows up assuming kubectl works.
- **Impact:** CI gate false-negative blocks merges.
- **Suggested fix:** Script per category checks kubectl availability + skips with WARN if absent. `--cluster-context=k3d-loopzero-mgmt` flag explicit, with `--hermetic` mode skipping all C2-C8 (helmfile-rendering only).

### EC-12: Check sprint flaky network probe → whole task check FAIL on single hiccup

- **Affected task:** T4.1
- **Family:** Timing / I/O
- **Scenario:** Single curl probe to `auth-dev.loopzero.example` flakes (DNS slow); whole `task check` reports FAIL.
- **Impact:** Sponsor sees noise; loses trust in sprint.
- **Suggested fix:** Per probe retry with 3× backoff (1s, 3s, 9s) before marking FAIL. Per category timeout 10s budget.

### EC-13: Check sprint runtime exceeds 30s budget — sponsor "boot-time" promise broken

- **Affected task:** T4.1
- **Family:** Timing
- **Scenario:** 44 sequential checks each ~2s = 88s sequential.
- **Impact:** Violates sponsor mandate canonical ~30s.
- **Suggested fix:** Parallelize independent categories C1-C8 via xargs -P or & + wait. Sequential within category. Master timeout 60s with hard cap kill.

### EC-14: `task setup` fail-fast vs warn-and-continue when `task check` FAILs

- **Affected task:** T4.1 + Taskfile.yml setup chain
- **Family:** State / Integration
- **Scenario:** `task setup` final step calls `task check`; check fails; should setup exit non-zero (canonical) ou warn-and-continue (developer DX)?
- **Impact:** Tradeoff between canonical strict + DX friendly inadequately specified.
- **Suggested fix:** ADR D4 extension: setup chain uses `task check || echo "[WARN] check sprint reported FAIL — investigate before LIVE walkthrough"; true` — surface but non-fatal. Cleanroom skill Phase 4.5 fail-fast.

### EC-15: Cleanroom skill Phase 4.5 task check fail behavior canonical

- **Affected task:** T4.2 (wire em Cleanroom skill)
- **Family:** State / Integration
- **Scenario:** Cleanroom Phase 4.5 task check returns FAIL>0; should Cleanroom skill abort + escalate ou warn-and-continue?
- **Impact:** Same as EC-14 but in Cleanroom context.
- **Suggested fix:** ADR D4 extension: Cleanroom Phase 4.5 fail-fast (mandate explicit ZERO FAIL canonical) per `cleanroom-up-live-test-mandate.md` strictness; surface BLOCKED report to human.

### EC-16: LIVE E2E test 9-journey — which journeys are auto vs require sponsor browser

- **Affected task:** T5.1 (LIVE E2E orchestrator)
- **Family:** Integration / Permission
- **Scenario:** Plano declara "ZERO manual step" mas J-04 password recovery exige sponsor inbox check; J-05 device-flow exige browser authorize. Not fully automatable.
- **Impact:** Promise broken; sponsor walkthrough JUNTOS authoritative gate.
- **Suggested fix:** ADR D6 extension: explicitly enumerate auto journeys (J-07/J-08/J-09 webhook/J-22 secret/J-23 PR open) vs semi-auto (J-04 needs email + J-05 needs browser approve). Sponsor walkthrough JUNTOS è canonical authoritative final gate per `cleanroom-up-live-test-mandate.md`. Auto E2E tests cover deterministic chain + LIVE counter increments (metric proves browser path GREEN).

### EC-17: J-04 password recovery resets canonical seed password

- **Affected task:** T5.1 (J-04 step)
- **Family:** State / Persistence
- **Scenario:** Test runs J-04 → resets password to test value → next test run logs in with stale canonical seed.
- **Impact:** Subsequent Cleanroom blocked by auth failure.
- **Suggested fix:** Test's J-04 step ends with `bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only` cleanup to restore canonical seed (per Plan Reference em memory `reference_t62_cleanroom_session_2026_06_11`).

### EC-18: Test data accumulation between LIVE E2E runs

- **Affected task:** T5.1
- **Family:** State / Persistence
- **Scenario:** Each run creates secrets (J-22 SPONSOR_LIVE_TEST_*), projects (J-09 teste), PRs (J-23 #N), envs (J-10 staging). Next run sees stale entities.
- **Impact:** Test asserts fail because counts changed; flaky test signal.
- **Suggested fix:** Test SetUp cleanup hook: delete prior session's entities via API OR use unique session-id suffix per run (`teste-${EPOCH}` etc) for fresh fixtures.

### EC-19: Sponsor unavailable for Phase 6 walkthrough scheduling

- **Affected task:** T6.1 (Integration Validation)
- **Family:** Permission
- **Scenario:** Plan goal requires "sponsor verbatim approval each journey" but sponsor may not be available immediately after engineering side done.
- **Impact:** Goal metric never reached; `<promise>IMPLEMENTATION_COMPLETE</promise>` blocked.
- **Suggested fix:** ADR D7 (new) — split DoD em engineering-side gate (LIVE E2E 9/9 GREEN auto + Layer C/E PASS) emits `<promise>` candidate; sponsor walkthrough JUNTOS is async finalizing handshake. Per `feedback_post_feature_live_regression_workflow_2026_06_09` mandate honors both: engineering-GREEN ≠ feature done, but engineering-side milestone is acceptable progress signal.

---

## SHOULD TEST (11 items — adicione tests ao TDD da task existente)

### EC-20: Promtail sink URL canonical regex match

- **Affected task:** T1.3 (promtail overlay)
- **Suggested test:** `test_promtail_overlay_sink_url_canonical` — `helm template promtail -f <env-overlay> | yq '.config.clients[0].url'` matches regex `^http://loki:3100/loki/api/v1/push$` para in-cluster OR `^http://[a-z0-9-]+:30094/...$` para cross-cluster prod.

### EC-21: Both ExternalSecret manifests reference canonical OpenBao kv-v2 paths

- **Affected task:** T1.5 (loopzero-external-secrets overlay)
- **Suggested test:** `test_external_secrets_canonical_paths` — helm template asserts both `loopzero-api-github` + `loopzero-github-webhook` ExternalSecret data[].remoteRef.key matches `^loopzero/<env>/api/secrets/github/(app_id|app_private_key|webhook_secret)$`.

### EC-22: `~/.loopzero/dev-credentials.json` mode 0600 enforced

- **Affected task:** T2.2 (secrets:seed-dev)
- **Suggested test:** `test_creds_file_mode_security` — `stat -c '%a' ~/.loopzero/dev-credentials.json` must be `600` OR script refuses + emits security warning.

### EC-23: CoreDNS NodeHosts canonical shape pre-patch

- **Affected task:** T2.3 (cross-cluster-coredns)
- **Suggested test:** `test_coredns_canonical_shape` — assert ConfigMap exists in expected ns + has `data.NodeHosts` key + canonical line format before patch.

### EC-24: CoreDNS patch idempotent re-run no-op

- **Affected task:** T2.3
- **Suggested test:** `test_coredns_idempotent_rerun` — run patch twice; assert second invocation no-ops + ConfigMap not re-mutated.

### EC-25: Port-conflict detection in auth-services port-forward

- **Affected task:** T2.4
- **Suggested test:** `test_port_forward_conflict_detection` — start sentinel listener on 14444; assert port-forward script detects + emits clear error OR kills stale + restarts.

### EC-26: Helmfile gates yq pre-requisite check

- **Affected task:** T3.1-3.5
- **Suggested test:** `test_helmfile_gate_yq_available` — script first checks `command -v yq` + warns if absent.

### EC-27: Go AST route_registration test covers HTTP method variants

- **Affected task:** T3.6
- **Suggested test:** `test_route_registration_covers_all_methods` — fixture with handlers using `Get`/`Post`/`Put`/`Delete`/`Method` patterns; assert all walked.

### EC-28: Go AST error_visibility test covers writeError wrapper functions

- **Affected task:** T3.7
- **Suggested test:** `test_error_visibility_covers_wrappers` — fixture com `r.WriteError(...)` (capital R wrapper) + `WriteError(w, ...)` helper; assert wrapper pattern detected OR allowlist annotation.

### EC-29: CLI loopzero-url error visibility — multiple try block shapes

- **Affected task:** T3.8
- **Suggested test:** `test_cli_swallowed_error_patterns` — fixture com `catch { return null }` + `.catch(() => null)` + `try { ... } catch { ... }`; assert all 3 antipatterns detected.

### EC-30: LIVE E2E timing per-journey + master assertions

- **Affected task:** T5.1
- **Suggested test:** `test_live_e2e_per_journey_timing` — wrapper emits start/end timestamps + master test asserts total wall-clock ≤ 30min. Useful para regression-defense em performance drift.

---

## DOCUMENT (7 items — risk consciously accepted)

### EC-31: cert-manager rotation triggers SPKI Secret refresh via ESO sync

- **Affected task:** T1.4
- **Accepted risk:** ESO refreshInterval=1h means SPKI mismatch window de até 1h após cert renew. Per `feedback_security_first_mandate` — TLS pin is defense-in-depth, mismatch causes connection refused (fail-closed), not security boundary breach. Operator runbook documents manual refresh `kubectl annotate externalsecret/loopzero-openbao-approle force-sync=$(date +%s)`.

### EC-32: ESO ClusterSecretStore retry canonical

- **Affected task:** T1.5
- **Accepted risk:** ESO controller já implementa exponential backoff retry per upstream chart. Manual intervention required only if Vault permanently unreachable. Out of plan scope.

### EC-33: secrets:seed-dev safe re-run overwrite semantic

- **Affected task:** T2.2
- **Accepted risk:** `bao kv put` overwrites version in kv-v2 (new version). Old versions retained per kv-v2 history retention (default 10). Safe pra dev re-runs. Prod canonical path uses ExternalSecret rotation policy (out of plan scope).

### EC-34: Go AST route_registration — dynamic routing patterns not caught

- **Affected task:** T3.6
- **Accepted risk:** If handler is registered via `for _, route := range routes { r.Method(route.Method, route.Path, route.Handler) }` AST won't trace literal `r.Post(...)`. Pattern raro em LoopZero; if introduced, ADR + allowlist annotation `// route-registration:dynamic` per pattern em other gates.

### EC-35: CLI source path refactor breaks grep gate

- **Affected task:** T3.8
- **Accepted risk:** Script greps `cli/src/` — if refactor moves CLI source elsewhere, grep returns empty + test PASS false-positive. Mitigation: test asserts grep returns ≥ 1 file (sanity). Acceptable trade-off for CI-runtime speed.

### EC-36: Check sprint env profile flag

- **Affected task:** T4.1
- **Accepted risk:** Plan declares `task check` runs em todo boot. Per-env nuances (dev-public has edge, standalone-multi local) gerenciados via `--env=<profile>` flag. Default detects env from cluster context. Operator can override.

### EC-37: Stripe webhook E2E test mode required

- **Affected task:** T5.1 (J-09 webhook step uses real GitHub webhook; J-22 doesn't touch Stripe)
- **Accepted risk:** Plano DOES NOT include Stripe webhook em 9-journey LIVE E2E. If sponsor mandate later adds Stripe journey, separate test required em test-mode keys. Out of current plan scope.

---

## Summary

| Task | Edges found | MUST FIX | SHOULD TEST | DOCUMENT |
|---|---|---|---|---|
| T1.1 (Hydra urls) | 1 | 1 | 0 | 0 |
| T1.2 (Cloud Kratos split) | 0 | 0 | 0 | 0 |
| T1.3 (Promtail) | 1 | 0 | 1 | 0 |
| T1.4 (SPKI extract) | 2 | 1 | 0 | 1 |
| T1.5 (loopzero-external-secrets) | 2 | 0 | 1 | 1 |
| T1.6 (Kratos cookies) | 1 | 1 | 0 | 0 |
| T2.1 (vault:bootstrap-dev) | 3 | 3 | 0 | 0 |
| T2.2 (secrets:seed-dev) | 4 | 2 | 1 | 1 |
| T2.3 (cross-cluster-coredns) | 3 | 1 | 2 | 0 |
| T2.4 (port-forward auth) | 2 | 1 | 1 | 0 |
| T3.1-3.5 (helm gates) | 1 | 0 | 1 | 0 |
| T3.6 (route AST) | 2 | 0 | 1 | 1 |
| T3.7 (error vis AST) | 1 | 0 | 1 | 0 |
| T3.8 (CLI error vis) | 2 | 0 | 1 | 1 |
| T4.1 (check-sprint) | 5 | 4 | 0 | 1 |
| T4.2 (wire Cleanroom skill) | 1 | 1 | 0 | 0 |
| T5.1 (LIVE E2E 9-journey) | 5 | 3 | 1 | 1 |
| T5.2 (hermetic syntax) | 0 | 0 | 0 | 0 |
| T6.1 (Integration Validation) | 1 | 1 | 0 | 0 |
| **TOTAL** | **37** | **19** | **11** | **7** |

**Verdict:** **PLAN NEEDS ADJUSTMENT**

Plan v1.0 é structurally sound + Coverage Matrix 100% per `plan-confidence-golden-rule.md`. Edge case review surface 19 MUST FIX items principalmente concentrados em Phase 2 (script idempotency + recovery paths) + Phase 4 (check sprint runtime + reachability + fail behavior) + Phase 5 (LIVE E2E test data isolation + sponsor walkthrough scheduling).

**Recommended absorption process per `cycle-plan.md` § Plan amendment policy**:

1. Human / `/plan-improve` revisa MUST FIX items
2. Para cada MUST FIX:
   - **EC-1/3/16/19**: amend ADRs (D1/D4/D6 extensions) + add ADR D7 for sponsor walkthrough scheduling
   - **EC-2/10**: add sub-tasks ao Task 1.4/2.4 com explicit fix step
   - **EC-4/5/6/7/8/9**: amend Task 2.1/2.2/2.3 com "Idempotency + Recovery" sub-section per task
   - **EC-11/12/13/14**: amend Task 4.1 com "Resilience + Performance" sub-section
   - **EC-15**: amend Task 4.2 ADR D4 extension
   - **EC-17/18**: amend Task 5.1 com "Test Data Isolation + Cleanup" sub-section
3. Bump plan v1.0 → v1.1
4. Re-run `/plan-confidence` em v1.1

SHOULD TEST items (11) podem ser absorvidos com bumpversão minor v1.0 → v1.0.1 — adicionam testes ao TDD existente sem reestruturar tasks.

DOCUMENT items (7) become notes no plan body OR allowlist entries sem version bump.

---

## Cross-references

- Plan SoT: [`knowledge-base/plans/2026-06-11-cleanroom-canonical-5-defense-layers-plan.md`](../plans/2026-06-11-cleanroom-canonical-5-defense-layers-plan.md)
- Per `cycle-plan.md` § Plan amendment policy — MUST FIX items require MEET + new ADR per category
- Memory cross-refs: `reference_t62_cleanroom_session_2026_06_11` (session reference) + 20 sibling memories
- Mandate: `.claude/rules/cleanroom-up-live-test-mandate.md` (locked 2026-06-10 INQUEBRÁVEL)
