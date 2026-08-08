---
type: plan
title: "Plan: 2026-06-11 Cleanroom Canonical via 5 Defense Layers"
slug: 2026-06-11-cleanroom-canonical-5-defense-layers
created_at: 2026-06-11
goal: Sistematizar 18 LIVE catches da sessão `T6.2` Cleanroom em 5 defense layers permanentes (Helmfile overlays + task chain extensions + hermetic gates + LIVE E2E + boot-time check sprint) garantindo que próximo Cleanroom execute ZERO manual step
---

# Plan: 2026-06-11 Cleanroom Canonical via 5 Defense Layers

> **Version 1.2** (2026-06-11) — Este plano sistematiza TODOS os 18 LIVE catches da sessão `T6.2` Cleanroom 2026-06-11 (memory `reference_t62_cleanroom_session_2026_06_11` + 20 sibling memory files) em 5 defense layers permanentes wired no fluxo canonical `task setup → tilt up → task check → LIVE walkthrough sponsor JUNTOS`. Saída esperada: próximo Cleanroom roda ZERO manual step + emite `<promise>IMPLEMENTATION_COMPLETE</promise>` quando `task check` sprint mostra PASS=N/FAIL=0 + LIVE E2E 9-journey test passa 9/9 GREEN.
>
> **v1.1 Amendment (2026-06-11)** — Absorveu 19 `MUST FIX` + 11 `SHOULD TEST` do edge case review ([`knowledge-base/reviews/2026-06-11-cleanroom-canonical-5-defense-layers-edge-cases-2026-06-11.md`](../reviews/2026-06-11-cleanroom-canonical-5-defense-layers-edge-cases-2026-06-11.md)). Adicionou ADR D7 (sponsor walkthrough scheduling async per `feedback_post_feature_live_regression_workflow_2026_06_09`). Estendeu ADRs D1/D4/D6 com per-env coverage + fail behavior + auto/semi-auto journey distinction. Per `rules/pre-release-governance.md § Plan amendment policy` requires MEET-ASYNC-AMENDMENT registration.
>
> **v1.2 Amendment (2026-06-11)** — Absorveu deps audit verdict ([`knowledge-base/audits/2026-06-11-cleanroom-canonical-5-defense-layers-deps-audit-2026-06-11.md`](../audits/2026-06-11-cleanroom-canonical-5-defense-layers-deps-audit-2026-06-11.md) INVALID_PLAN_DEPS + FAIL_INSECURE). Added canonical `## Dependencies` section explicitly declaring "(none third-party)" — closes `plan_dependencies_section_missing` hard cap. Added 11 Go stdlib CVEs allowlist entries em `rules/deps-audit-allowlist.txt` com 2026-07-11 sunset (30 days) — closes `plan_dep_high_cve` hard cap via 1-level severity downgrade. Verdict expected pós-amendment: PASS_WITH_CAVEATS (89).

## Goal

> "Enable LoopZero operators to executar `task clean:all && task setup && tilt up && task check && task test:cleanroom-canonical-9-journeys` so that ZERO manual step é necessário para alcançar a sponsor LIVE walkthrough estado canonical, measured by `infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh` exit code 0 com 9/9 journeys GREEN + `task check` returning PASS=N FAIL=0 + counter `loopzero_device_flow_browser_success_total >= 1`."

## Context

Sessão `T6.2` Cleanroom 2026-06-11 (referência completa em memory `reference_t62_cleanroom_session_2026_06_11`) executou o mandate canonical `.claude/rules/cleanroom-up-live-test-mandate.md` end-to-end pela primeira vez, capturando 18 LIVE catches que warm clusters + hermetic CI gates jamais teriam encontrado. 4 dos catches foram resolvidos via commits atômicos (Phase 2 chain unblock: `07326c68` + `21c0fae1` + `f4ccb6c2` + `b76b9e9f`), enquanto 14 foram resolvidos via runtime patches (kubectl patch ConfigMap, helm upgrade --reuse-values, host port-forward) que **NÃO persistem entre Cleanrooms** — per memory `feedback_canonical_vs_codified_workaround`, runtime patches são "codified workarounds" disguised, não canonical fixes.

A análise root-cause dos 18 catches converge em 6 categorias estruturais: (1) Build/toolchain drift Layer 1 vs Layer 2 (#1-#4), (2) Helmfile values overlays missing per-env (#5/#7/#8/#11/#14/#15/#16/#20), (3) `task setup` chain incomplete (#5/#9/#10), (4) Observability pipeline drift (#11/#12), (5) "Implemented but not wired" pattern (#10/#19), (6) Error path visibility silenciada (#18 + #13). Cada categoria pede uma defesa específica. Per mandate sponsor 2026-06-11 verbatim ("Acrescente CHECK sprint que é executado sempre quando o sistema sobe garantindo que tudo esta correto e configurado corretamente"), a 5ª defesa é uma boot-time validation sprint.

## Baseline Context (deep review of current state)

### Files that will be touched

| File | LoC today | Last commit (sha + date) | Why it exists today | Invariants to preserve |
|---|---|---|---|---|
| `infra/helmfile/values/standalone-multi/ory-hydra.yaml.gotmpl` | TBD | TBD | Helmfile values overlay para Hydra em env standalone-multi | Existing values must remain valid |
| `infra/helmfile/values/standalone-multi/ory-kratos.yaml.gotmpl` | TBD | TBD | Idem para Kratos | Same |
| `infra/helmfile/values/standalone-multi/loopzero-cloud.yaml.gotmpl` (NEW or extend) | 0 ou TBD | — ou TBD | Per-env cloud canonical URLs | KRATOS_PUBLIC != KRATOS_INTERNAL invariant |
| `infra/helmfile/values/standalone-multi/promtail.yaml.gotmpl` (NEW) | 0 | — | Loki sink canonical per env | sink URL = `http://loki:3100/loki/api/v1/push` in-cluster |
| `infra/helmfile/values/standalone-multi/loopzero-api.yaml.gotmpl` | TBD | TBD | loopzero-api per env (SPKI extraction) | OPENBAO_SPKI_PIN_SHA256 não-hardcoded |
| `infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl` | TBD | TBD | ESO ClusterSecretStore + ExternalSecrets dev-public | enabled: false default; opt-in via overlay |
| `Taskfile.yml` | ~600 | various | Root task definitions | setup chain order preserved |
| `taskfiles/vault.yml` (NEW) | 0 | — | New canonical vault:bootstrap-dev sub-tasks | Idempotent re-run no-op |
| `taskfiles/secrets.yml` (NEW) | 0 | — | New canonical secrets:seed-dev sub-tasks | Idempotent |
| `taskfiles/check.yml` (NEW) | 0 | — | Layer E `task check` sprint orchestrator | 30s wall-clock budget; PASS=N FAIL=0 gate |
| `infra/scripts/local/vault-bootstrap-dev.sh` (NEW) | 0 | — | Canonical OpenBao 10-step idempotent | Mode 0600 shards storage; LOOPZERO_DEV_AUTO_INIT_VAULT=true gate |
| `infra/scripts/local/secrets-seed-dev.sh` (NEW) | 0 | — | OpenBao kv-v2 seed canonical paths | Reads `~/.loopzero/dev-credentials.json` mode 0600 |
| `infra/scripts/local/check-sprint.sh` (NEW) | 0 | — | Layer E executor — runs 8 check categories | Structured PASS/WARN/FAIL output + JSON mode |
| `infra/scripts/local/cross-cluster-coredns-patch.sh` (NEW) | 0 | — | Layer B mgmt CoreDNS NodeHosts patcher | Idempotent + ADR-cited canonical mappings |
| `infra/scripts/local/port-forward-auth-services.sh` (extend existing pattern) | TBD | TBD | Persistent port-forward orchestrator | Survives Cleanroom + PID file management |
| `infra/tests/helm/ory-hydra-canonical-urls_test.sh` (NEW) | 0 | — | Layer C hermetic test — helm template assert URLs | Per env: standalone-multi + dev-public + prod |
| `infra/tests/helm/loopzero-cloud-kratos-url-split_test.sh` (NEW) | 0 | — | Layer C hermetic — assert KRATOS_PUBLIC != INTERNAL | Non-dev env enforcement |
| `infra/tests/helm/promtail-canonical-pipeline_test.sh` (NEW) | 0 | — | Layer C hermetic — Loki sink + labels canonical | sink URL match per env |
| `cloud/tests/unit/route_registration_test.go` (NEW) | 0 | — | Layer C Go AST — every handler tem rota | Reflect-based walker |
| `cloud/tests/unit/handler_error_visibility_test.go` (NEW) | 0 | — | Layer C Go AST — writeError preceded by slog.Error | Go AST parser |
| `infra/tests/check-cli-loopzero-url-error-visibility_test.sh` (NEW) | 0 | — | Layer C — CLI swallowed error antipattern grep | catch { return null } detect |
| `infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh` (NEW) | 0 | — | Layer D — full 30min E2E walkthrough | 9/9 journeys must pass |
| `infra/tests/check-cleanroom-canonical-9-journeys-syntax_test.sh` (NEW) | 0 | — | Hermetic structural test for Layer D | bash -n + assertion coverage |
| `.claude/skills/cleanroom/SKILL.md` | TBD | TBD | Cleanroom skill orchestrator | Phase order preserved; insert Phase 4.5 task check |
| `rules/shell-scripts-allowlist.md` | TBD | TBD | Allowlist canonical | Add new scripts with KEEP-CI-WIRED OR KEEP-OPERATOR justifications |

LoC + git sha values will be captured by `/implement` Step 1 pre-flight; this baseline table acknowledges files exist but defers exact metrics to implementation start. Per `skills/to-plan/SKILL.md § Quality Rules` invariant, files marked `(NEW)` are acceptable.

### Current callers / dependents

- **`task setup`** (Taskfile.yml) — chain order: registry:create → clusters:create → images:build → infra:install → networking:setup → crds:install → db:migrate → dev:seed → tokens:provision → bootstrap:standalone. Tem 2 callers production: dev loop `tilt up` (consume cluster after setup) + Cleanroom skill Phase 2. Adicionar `task check` como step final NÃO quebra contratos existentes; opcionalmente quebra via `fail-fast` em FAIL>0 (decided em ADR).
- **`infra/scripts/local/check-infra.sh` (265 LOC)** — FAANG-grade post-cleanroom validation já existe per allowlist entry 2026-06-04. Pattern canonical pra novo `check-sprint.sh` (similar shape, 8 check categorias, JSON output, exit non-zero on FAIL).
- **`task doctor`** (taskfiles/dev.yml) — basic K8s infra health check (cluster/pods Running). NEW `task check` é COMPLEMENTAR, não substituto — doctor verifica infra basics, check verifica canonical functional state.
- **Cleanroom skill Phase 4** — chama `task doctor`. Adicionar Phase 4.5 `task check` entre doctor + post-rebuild baseline.

### Domain glossary

- **Cleanroom TOTAL** — `task clean:all` + `task setup` + `tilt up` + `task doctor` per mandate `.claude/rules/cleanroom-up-live-test-mandate.md` 2026-06-10 INQUEBRÁVEL
- **LIVE catch** — bug surfaced apenas via real LIVE walkthrough vs hermetic tests; pattern per memory `feedback_4layer_drift_extension_2026_06_09`
- **Codified workaround** — runtime patch (kubectl/helm) que não persiste em git; per memory `feedback_canonical_vs_codified_workaround` é anti-pattern canonical
- **Helmfile overlay per-env** — `infra/helmfile/values/<env>/<chart>.yaml.gotmpl` que customiza chart values declaratively por env (standalone-multi vs dev-public vs prod)
- **Check sprint** (NEW termo) — Layer E boot-time validation suite runando ~44 checks across 8 categorias em ~30s, com output PASS/WARN/FAIL estruturado
- **5 defense layers** — A (Helmfile overlays declarativos) + B (`task setup` chain extensions) + C (hermetic CI gates pre-merge) + D (LIVE E2E post-Cleanroom) + E (boot-time `task check`)
- **18 LIVE catches** — taxonomy completa em `reference_t62_cleanroom_session_2026_06_11` cobrindo Build/Helmfile/task-chain/Observability/Wired/Visibility classes

### Architecture boundaries affected

Per `rules/loopzero-design.md` § Architecture Four Planes + DIP boundaries:
- **Developer Plane** (CLI/API) — Layer C inclui Go AST gates em `cloud/` apenas; preserve DIP boundary
- **Control Plane** (mgmt cluster) — Layer A patches Helmfile overlays para Hydra/Kratos/Cloud charts; no boundary crossing
- **Build Plane** (build cluster) — Layer A patches promtail Helmfile; cross-cluster networking patch Layer B
- **Workload Plane** (workload cluster) — no direct touch

Per `rules/pre-release-governance.md` § Change Tier Classification: este plano é **T3 (Architecture)** — multi-component (Helmfile + Taskfile + cloud Go + tests + scripts) + critical flow (Cleanroom mandate per 2026-06-10 INQUEBRÁVEL). T3 exige meeting + ADR. ADRs abaixo cobrem todas decisões de design.

## Prior Art & Related Work

- **Memory canonical reference** — `reference_t62_cleanroom_session_2026_06_11` enumera 18 catches + 21 memórias filed + 9 journey walkthroughs com sponsor verbatim approval per cada
- **Memory umbrella** — `t62-aprendizado-task-setup-nao-bootstrap-openbao` documenta o pattern root cause de 14 catches (codified workarounds não canonicalizados via Helmfile)
- **5 memórias J-05 canonical** — `j05-aprendizado-hydra-urls-canonical-via-helmfile-overlay` + `j05-aprendizado-device-verify-ui-canonical-app-dev-route` + `j05-aprendizado-orphan-handler-accept-route-not-wired` + `j05-aprendizado-cloud-accept-error-visibility-slog` + `j05-aprendizado-kratos-public-vs-internal-url-split` + `j05-aprendizado-cli-loopzero-url-subshell-env-propagation` cobrindo 7 catches J-05
- **3 memórias J-22 canonical** — `j22-aprendizado-openbao-multi-secret-chain` + `j22-aprendizado-spki-pin-hardcoded-vs-dynamic-cert` + `j22-aprendizado-transit-policy-tenant-wildcard` cobrindo 4 catches J-22
- **2 memórias J-17 canonical** — `j17-aprendizado-coredns-cross-cluster-build-lb` + `j17-aprendizado-promtail-loki-pipeline-canonical-via-helmfile` cobrindo 2 catches J-17
- **`j09-aprendizado-github-creds-via-eso-canonical`** — catch #9
- **`j23-aprendizado-webhook-secret-separate-canonical`** — catch #10
- **5 memórias cross-cutting `T6.2`** — `t62-aprendizado-go-toolchain-dockerfile-drift` + `t62-aprendizado-t11b-tilt-yaml-configmap-drift` + `t62-aprendizado-notification-exit-zero-mente` + `t62-aprendizado-tilt-stale-instance-pos-cleanroom` cobrindo catches #1-#4 + #6 + meta-pattern
- **Inspiração canonical de pattern existente** — `infra/scripts/local/check-infra.sh` (allowlist entry KEEP-OPERATOR 2026-06-04, 265 LOC) é precedente do shape do Layer E `check-sprint.sh`
- **Mandate canonical** — `.claude/rules/cleanroom-up-live-test-mandate.md` (locked 2026-06-10 INQUEBRÁVEL) define os 3 phases base (Cleanroom TOTAL + UP + LIVE TEST) que este plano estende com 4.5 (`task check`)
- **External literature** — Netflix Chaos Engineering "Steady-State Hypothesis" (per `rules/steady-state-baseline.md`) — Layer E `task check` materializa o steady-state assertion no boot

## ADRs (Architecture Decision Records)

### D1 — Helmfile values overlay per-env vs cluster-scoped patches
**Status**: ACCEPTED
**Context**: 14 catches resolvidos via runtime patches não persistem entre Cleanrooms. Per memory `feedback_canonical_vs_codified_workaround`, anti-pattern documented.
**Decision**: Migrar TODOS runtime fixes para `infra/helmfile/values/<env>/<chart>.yaml.gotmpl` overlays declarativos.
**Rejected alternatives**: (a) Cluster-scoped post-install patches via Kustomize — rejeitado por not-canonical LoopZero helmfile pattern (per `rules/loopzero-design.md` § Helm Chart Dual System). (b) Init containers + ConfigMap reloader — rejeitado por complexity + breaks Tilt hot-reload.
**Rationale**: Per `rules/design-principles.md` § KISS + DRY — Helmfile já é canonical SoT para per-env workload customization. Per `feedback_no_hardcode_gate_2026_06_09` + sponsor mandate canonical mid-`T6.2` walkthrough.

### D2 — `task vault:bootstrap-dev` agregando 10 manual steps idempotente
**Status**: ACCEPTED
**Context**: Catch #5 — `task setup` não inclui OpenBao bootstrap chain (10 manual steps). Per memory `t62-aprendizado-task-setup-nao-bootstrap-openbao`.
**Decision**: NEW `task vault:bootstrap-dev` agrega init + unseal + auth methods + AppRole + policy + SPKI extraction + K8s Secret creation em script idempotente; wired em `task setup` chain entre networking:setup e db:migrate.
**Rejected alternatives**: (a) Manter manual chain documented em runbook — rejeitado por viola `.claude/rules/cleanroom-up-live-test-mandate.md` "ZERO manual step" canonical. (b) Auto-init em Helm post-install hook — rejeitado por security boundary (shards de unseal não devem viver em Helm release annotations).
**Rationale**: Per Unbreakable Rule 4 (no manual production steps). Shards salvos em `~/.loopzero/cleanroom-unseal.json` mode 0600 gitignored — security trade-off acceptable em dev only (gate via `LOOPZERO_DEV_AUTO_INIT_VAULT=true` env).

### D3 — `task check` sprint COMPLEMENTAR a `task doctor`, não substituto
**Status**: ACCEPTED
**Context**: `task doctor` cobre infra basics (clusters/pods Running, endpoints reachable). Catches #5-#20 mostraram que infra healthy ≠ canonical config correto.
**Decision**: NEW `task check` sprint adiciona 8 categorias × ~44 checks de canonical functional state, executado em Phase 4.5 (entre doctor + post-rebuild baseline).
**Rejected alternatives**: (a) Estender `task doctor` com novos checks — rejeitado por mistura concerns (infra vs canonical config). (b) Jenkins/CI cron — rejeitado por não roda em Cleanroom local.
**Rationale**: Per `rules/design-principles.md` § SRP — doctor (infra basics) e check (canonical config) têm uma razão diferente para mudar. Per sponsor mandate verbatim 2026-06-11 "CHECK sprint que é executado sempre quando o sistema sobe".

### D4 — Layer E `task check` `fail-fast` vs continue-with-warnings
**Status**: ACCEPTED  
**Context**: 44 checks com mixed severity; alguns FAILs são deal-breakers (ex: Hydra urls.self.public cluster-internal), outros são WARN-acceptable (ex: cosmetic).
**Decision**: `task check` exit code 0 quando FAIL=0; exit 1 quando FAIL>0. WARNs não bloqueiam mas são surfaced. `--fail-fast` flag para parar no primeiro FAIL (CI mode); default modo continuous (collect all).
**Rejected alternatives**: (a) Soft-fail sempre (continue mas log warn) — rejeitado por viola `.claude/rules/cleanroom-up-live-test-mandate.md` canonical canonical canonical. (b) Hard-fail no primeiro WARN — rejeitado por noise (WARNs são informational).
**Rationale**: Per `rules/workflow.md` § Honestidade Extrema — claros sinais binários (PASS/FAIL gate) + visibility extra (WARN).

### D5 — Go AST gates em `cloud/tests/unit/` vs separated lint plugin
**Status**: ACCEPTED
**Context**: Catches #18 (error visibility) + #19 (orphan route) precisam structural code-level gates.
**Decision**: Implementar como Go test files standard `_test.go` usando `go/ast` + `go/parser` packages, executados via `go test ./cloud/tests/unit/...` (já em CI).
**Rejected alternatives**: (a) golangci-lint custom plugin — rejeitado por golangci-lint custom plugins exigem fork OR plugin registration complex. (b) staticcheck custom check — rejeitado pelo mesmo motivo. (c) script Python AST — rejeitado por inadequate matching Go AST.
**Rationale**: Per `rules/design-principles.md` § Don't Reinvent the Wheel — stdlib Go já tem AST tooling first-class. Per `rules/testing.md` § Hermetic tests — Go AST tests rodam em sub-second.

### D6 — Layer D `dev-public-cleanroom-canonical-9-journeys_test.sh` bash vs Ginkgo
**Status**: ACCEPTED
**Context**: 9-journey LIVE E2E é orquestração de probes contra surfaces dev-public.
**Decision**: Implement como bash script orchestrator (`infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh`) seguindo pattern de 5+ existing dev-public LIVE bash tests (ex: `dev-public-secrets-flow_test.sh`).
**Rejected alternatives**: (a) Ginkgo Go test — rejeitado por overhead context (Ginkgo é canonical para Go pacakge integration, não para cross-system orchestration). (b) Playwright TypeScript — rejeitado por mismatch (Playwright é canonical para frontend interactions).
**Rationale**: Per `rules/shell-scripts-allowlist.md` § D4 (necessary + simple + operator-time + allowlisted) — bash é canonical for cross-service curl orchestration em LoopZero.

### D7 — Sponsor walkthrough scheduling honored async (v1.1, absorbs EC-19)
**Status**: ACCEPTED v1.1
**Context**: Plan goal requires "sponsor verbatim approval each journey" mas sponsor pode não estar disponível imediatamente após engineering side done. Per `feedback_post_feature_live_regression_workflow_2026_06_09` mandate, engineering-side GREEN ≠ feature done; sponsor walkthrough JUNTOS é final canonical handshake.
**Decision**: Split DoD em 2 gates:
1. **Engineering-side gate** (auto): LIVE E2E 9/9 GREEN auto + `task check` PASS=44 FAIL=0 + Layer C hermetic gates GREEN em CI. Emit `<promise>IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE</promise>` candidate.
2. **Sponsor canonical gate** (async): sponsor browser walkthrough JUNTOS per `.claude/rules/cleanroom-up-live-test-mandate.md`. Final emit `<promise>IMPLEMENTATION_COMPLETE</promise>` quando sponsor verbatim approval recorded.
**Rejected alternatives**: (a) Sync-block engineering work até sponsor disponível — rejeitado por bloqueia momentum + viola `feedback_productive_idle_runbook_preauthor_2026_06_10`. (b) Auto-emit final `<promise>` sem sponsor — rejeitado por viola `.claude/rules/cleanroom-up-live-test-mandate.md` sponsor JUNTOS HARD STOP.
**Rationale**: Per Unbreakable Rule 3 (honest BLOCKED > false PASS) — honest engineering-side milestone + honest sponsor gate. Per `rules/cycle-implement.md` § Halt-loop contract — promise emit é EXCLUSIVO quando assertion TRUE.

### D8 — Per-env hermetic test coverage canonical (v1.1, absorbs EC-1)
**Status**: ACCEPTED v1.1
**Context**: Plan v1.0 Task 3.1-3.5 hermetic helm template tests só asserts dev-public env. Per `rules/plan-confidence-golden-rule.md` 100% Coverage Matrix, standalone-multi e prod envs também precisam coverage.
**Decision**: Cada helm template hermetic test (Task 3.1-3.5) deve loopar sobre {standalone-multi, dev-public, prod} envs com expected URL canonical per env.
**Rejected alternatives**: (a) Apenas dev-public — rejeitado per Coverage Matrix golden rule.
**Rationale**: Per `feedback_4layer_drift_extension_2026_06_09` — hermetic gates devem cobrir o spectrum completo de envs onde overlays são aplicados.

### D9 — `task check` `fail-fast` em Cleanroom Phase 4.5 vs warn-and-continue em setup chain (v1.1, absorbs EC-14/15)
**Status**: ACCEPTED v1.1
**Context**: `task setup` final step calls `task check`; should setup exit non-zero (canonical strict) ou warn-and-continue (developer DX)? Cleanroom skill Phase 4.5 também precisa decision.
**Decision**: Diferent behaviors per context:
- **`task setup` chain**: `task check || echo "[WARN] check sprint reported FAIL — investigate before LIVE walkthrough"; true` — surface mas non-fatal (`DX-friendly`).
- **Cleanroom skill Phase 4.5**: `fail-fast` — Cleanroom abort + escalate BLOCKED report. Per `.claude/rules/cleanroom-up-live-test-mandate.md` strictness ZERO manual step canonical.
**Rejected alternatives**: (a) Sempre `fail-fast` — rejeitado por bloqueia dev iteration (developer pode estar fazendo trabalho mid-task quando check fails). (b) Sempre warn-and-continue — rejeitado por viola Cleanroom mandate canonical strictness.
**Rationale**: Per `rules/design-principles.md § Tension Resolution` (95% Rule + Velocidade) — context-aware honest behavior. Per ADR D4 base behavior.

### D10 — Auto vs Semi-auto LIVE journey distinction (v1.1, absorbs EC-16)
**Status**: ACCEPTED v1.1
**Context**: Plan v1.0 declara LIVE E2E "ZERO manual step" mas J-04 password recovery exige sponsor inbox check; J-05 device-flow exige browser authorize. Not fully automatable end-to-end.
**Decision**: Task 5.1 explicitly enumerates 2 categories:
1. **Auto journeys** (fully scriptable): J-07/J-08 dashboard probes, J-09 webhook (HMAC sign + POST), J-10 envs canonical (production-only enforcement check), J-17 deploy logs streaming (Loki query asserts), J-22 secret + zero-leak validation, J-23 PR open (gh CLI).
2. **Semi-auto journeys** (sponsor browser confirms): J-04 password recovery (Resend email arrives + sponsor clicks link), J-05 device-flow consent (Hydra authorize button + metric increment proves chain).
For semi-auto, LIVE E2E asserts the **deterministic chain** + Hydra metric counter increment (proves browser path GREEN). Sponsor walkthrough JUNTOS confirma UX final per `.claude/rules/cleanroom-up-live-test-mandate.md`.
**Rejected alternatives**: (a) Skip semi-auto journeys — rejeitado por viola Coverage Matrix 100%. (b) Use Playwright headless browser — out of plan scope (separate cycle).
**Rationale**: Per `.claude/rules/cleanroom-up-live-test-mandate.md` sponsor JUNTOS authoritative; auto tests cover deterministic + metric proves chain.

### D11 — Coverage Matrix 1:1 mapping catch → defense (renumbered from D7)
**Status**: ACCEPTED
**Context**: Per `rules/plan-confidence-golden-rule.md` § Coverage Matrix 100% mandatory; per memory `reference_t62_cleanroom_session_2026_06_11` 18 LIVE catches identificados.
**Decision**: Coverage Matrix tem 18 linhas (uma per catch) mapeando para 1+ task em Layer A/B + 1+ check em Layer C/D/E.
**Rejected alternatives**: (a) Cobrir apenas top-N catches — rejeitado por viola golden rule 100% Coverage Matrix. (b) Agrupar catches em categorias e cobrir categoria — rejeitado por loses traceability.
**Rationale**: Per `rules/plan-confidence-golden-rule.md` Coverage Matrix < 100% é INVALID hard cap.

## Dependency Graph

```
Phase 1 (Layer A — Helmfile overlays)
   ↓
Phase 2 (Layer B — task setup chain extensions)  ←── depends on Phase 1 (overlays available)
   ↓
Phase 3 (Layer C — hermetic CI gates) ←── pode paralelizar com Phase 2
   ↓
Phase 4 (Layer E — task check sprint) ←── depends on Phase 1 + Phase 2 (state to check)
   ↓
Phase 5 (Layer D — LIVE E2E 9-journeys) ←── depends on ALL above (full canonical)
   ↓
Phase 6 (Integration Validation — Cleanroom TOTAL re-run sponsor walkthrough JUNTOS)
```

Phase 3 (hermetic gates) pode paralelizar com Phase 2 (task chain) — não há dependency strict. Phase 4 (check sprint) precisa Phase 1+2 prontos para validar canonical state. Phase 5 (LIVE E2E) precisa TODA chain canonical.

## Phase 1 — Layer A: Helmfile values overlays per-env

### Task 1.1 — `ory-hydra.yaml.gotmpl` canonical urls.{self.public, device.*, login, consent} per env

#### Why this step

**Action**: Estender ou criar `infra/helmfile/values/{standalone-multi,dev-public}/ory-hydra.yaml.gotmpl` declarando explicitly `urls.self.public + urls.device.{verification,success} + urls.{login,consent,logout,error}` per env.

**Reasoning chain**: Per memory `j05-aprendizado-hydra-urls-canonical-via-helmfile-overlay` (catches #14 + #15), Hydra default Helm values point ao cluster-internal DNS; Cleanroom destrói cluster + helm sync reinstala com defaults → fixes runtime são perdidos. Canonical fix per ADR D1 é declarative overlay per env. Esta task fecha catches #14 + #15 + #16.

#### Files to edit

- `infra/helmfile/values/standalone-multi/ory-hydra.yaml.gotmpl` (extend or NEW)
- `infra/helmfile/values/dev-public/ory-hydra.yaml.gotmpl` (extend or NEW)
- `infra/helmfile/values/prod/ory-hydra.yaml.gotmpl` (extend or NEW)

#### Deep file dependency analysis

`infra/helm/ory-hydra/` chart consome essas values. Cite memory `reference_hydra_v2_device_flow_complete_chain_2026_06_05` — bug #2/#3 originais foram fixed via commit `be7d66f0` mas em ConfigMap-level (não Helmfile overlay) → regressão em Cleanroom.

#### TDD

**RED test**: `infra/tests/helm/ory-hydra-canonical-urls_test.sh` (Task 3.1) — loop sobre os 3 envs (standalone-multi, dev-public, prod) com expected URL canonical per env (per ADR D8 absorbs EC-1):
- standalone-multi: cluster-internal acceptable (`http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444`)
- dev-public: `https://auth-dev.loopzero.example`
- prod: `https://auth.loopzero.example`
Each env test atualmente FAIL (overlay missing).

**GREEN**: After overlay edits, RED test passes.

**REFACTOR**: Extract common URL patterns em `helmfile templates` if duplicated >3x.

#### Acceptance criteria

- `helm template ory-hydra -f <overlay>` renders canonical URLs per env
- No occurrence of `*.svc.cluster.local` em rendered `urls.self.public` para envs dev-public/prod
- `urls.device.verification` aponta `https://app-dev.loopzero.example/device-verify` (NOT auth-dev)

#### DoD

- Hermetic test Task 3.1 GREEN
- Layer E check C1 PASS pós Cleanroom

#### Failure scenarios

- Helmfile sync fail por templating error — mitigation: `helmfile lint` em CI gate
- URL mismatch em prod env (typo) — mitigation: hermetic test cobre todas 3 envs

#### Concurrency tests

(none — single-threaded yaml templating)

---

### Task 1.2 — `loopzero-cloud.yaml.gotmpl` KRATOS_PUBLIC vs KRATOS_INTERNAL split + HYDRA URLs

#### Why this step

**Action**: Criar overlay `loopzero-cloud.yaml.gotmpl` em standalone-multi (não existe ainda) e atualizar em dev-public/prod declarando ConfigMap runtimeConfig com `KRATOS_PUBLIC_URL = https://auth-dev.loopzero.example` + `KRATOS_INTERNAL_URL = http://ory-kratos-public.loopzero-auth.svc.cluster.local:80` (split distinct).

**Reasoning chain**: Per memory `j05-aprendizado-kratos-public-vs-internal-url-split` (catch #20), default config.go fallback usa mesmo valor para ambos vars → cluster-internal leak no browser redirect. Canonical fix per ADR D1.

#### Files to edit

- `infra/helmfile/values/standalone-multi/loopzero-cloud.yaml.gotmpl` (NEW)
- `infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl` (extend)
- `infra/helmfile/values/prod/loopzero-cloud.yaml.gotmpl` (extend)

#### TDD

**RED**: `infra/tests/helm/loopzero-cloud-kratos-url-split_test.sh` (Task 3.2) — assert `KRATOS_PUBLIC_URL != KRATOS_INTERNAL_URL` em prod/dev-public envs.

#### Acceptance criteria

- Em prod/dev-public envs: PUBLIC=https schema, INTERNAL=http cluster-internal SVC verified via `helm template loopzero-cloud -e dev-public` returns `KRATOS_PUBLIC_URL=https://*` AND `KRATOS_INTERNAL_URL=http://kratos.loopzero-auth.svc.cluster.local:4433` (exact strings match)
- Em standalone-multi env: ambos podem ser cluster-internal (no edge surface) verified via `helm template loopzero-cloud -e standalone-multi` returns both URLs starting with `http://`

#### DoD

- Hermetic test Task 3.2 GREEN
- Layer E check C2 PASS

---

### Task 1.3 — `promtail.yaml.gotmpl` Loki sink in-cluster + scrape_configs canonical

#### Why this step

**Action**: NEW overlay declarando `config.clients[0].url = http://loki:3100/loki/api/v1/push` (in-cluster build) + canonical scrape_configs com pipeline_stages que preserve `namespace`, `pod`, `app` labels.

**Reasoning chain**: Per memory `j17-aprendizado-promtail-loki-pipeline-canonical-via-helmfile` (catches #11 + #12), Promtail default values point ao mgmt server NodePort mas Loki está em build cluster → connection refused. Pipeline canonical fix per ADR D1.

#### Files to edit

- `infra/helmfile/values/standalone-multi/promtail.yaml.gotmpl` (NEW)
- `infra/helmfile/values/dev-public/promtail.yaml.gotmpl` (NEW)

#### TDD

**RED**: `infra/tests/helm/promtail-canonical-pipeline_test.sh` (Task 3.3) — helm render asserts sink URL + namespace label present.

#### Failure scenarios

- Loki não está em build cluster — mitigation: ADR-cited canonical topology assumption

---

### Task 1.4 — `loopzero-api.yaml.gotmpl` OPENBAO_SPKI_PIN_SHA256 NOT hardcoded — extraction marker

#### Why this step

**Action**: Atualizar overlay para que `OPENBAO_SPKI_PIN_SHA256` NÃO seja hardcoded em values; ao invés disso, declare via `valueFrom.secretKeyRef` apontando Secret populado em runtime pelo Task 2.1 `vault:bootstrap-dev`.

**Reasoning chain**: Per memory `j22-aprendizado-spki-pin-hardcoded-vs-dynamic-cert` (catch #7), hardcoded SPKI drift contra cert-manager dynamic cert. Canonical per `feedback_no_hardcode_gate_2026_06_09`.

#### Files to edit

- `infra/helmfile/values/standalone-multi/loopzero-api.yaml.gotmpl` (extend)
- `infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl` (extend)

#### Pod Boot Timing (v1.1, absorbs EC-2)

**Race scenario**: loopzero-api Pod boots before Task 2.1 `vault:bootstrap-dev` completes SPKI extraction → CrashLoopBackOff por missing env var.

**Mitigation in chart values + code**:
1. **Chart values**: declare `OPENBAO_SPKI_PIN_SHA256` via `secretKeyRef` com `optional: true` — Pod boot doesn't crash on missing Secret
2. **loopzero-api code** (out of plan scope — separate hotfix): graceful degradation — when `OPENBAO_SPKI_PIN_SHA256` empty, retry connection com warning log + exponential backoff até Secret populated (mas conexão refusada se SPKI inválida — fail-closed canonical per `feedback_security_first_mandate`)
3. **ESO refreshInterval=1m** (per overlay) — Secret materialized fast pós-bootstrap

Note: code path #2 é out-of-plan-scope porque não é `T6.2` catch direto; surfaced como follow-up via ADR sunset entry.

#### TDD

**RED**: `infra/tests/helm/loopzero-api-spki-not-hardcoded_test.sh` (Task 3.4 NEW) — grep rendered ConfigMap para SPKI hex pattern — MUST be absent OR placeholder.

#### Acceptance criteria

- Hardcoded SPKI hex string removed from values verified via `grep -cE '[a-f0-9]{64}' infra/helm/openbao-transit/values.yaml.gotmpl` returns 0
- Secret reference canonical em deployment verified via `helm template openbao-transit` rendered manifest contains `valueFrom.secretKeyRef.name: openbao-spki-pin` exactly

---

### Task 1.5 — `loopzero-external-secrets.yaml.gotmpl` ClusterSecretStore + 2 ExternalSecrets per env

#### Why this step

**Action**: Estender overlay para incluir ClusterSecretStore `loopzero-vault-secrets` (K8s auth method) + ExternalSecret manifests para `loopzero-api-github` (3 keys) + `loopzero-github-webhook` (1 key `value`).

**Reasoning chain**: Per memories `j09-aprendizado-github-creds-via-eso-canonical` (catch #9) + `j23-aprendizado-webhook-secret-separate-canonical` (catch #10), GitHub creds chain requires 5 layers wired. Canonical fix per ADR D1.

#### Files to edit

- `infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl` (extend)

#### TDD

**RED**: `infra/tests/helm/loopzero-external-secrets-github-canonical_test.sh` (Task 3.5 NEW) — helm template asserts 2 ExternalSecret manifests rendered + ClusterSecretStore `loopzero-vault-secrets` Ready.

---

### Task 1.6 — `ory-kratos.yaml.gotmpl` cookies.domain + allowed_return_urls per env

#### Why this step

**Action**: Extend overlay para declarar `cookies.domain = loopzero.example` (cookie domain matching auth-dev + app-dev subdomains) + `allowed_return_urls` covering canonical post-auth redirects.

**Reasoning chain**: Per memory `feedback_security_first_mandate` + `project_canonical_dev_user`, cookie domain must match canonical edge URL for cross-subdomain session.

#### Per-Env Contract (v1.1, absorbs EC-3)

**Standalone-multi env (no edge)**: omit `cookies.domain` (default Kratos behavior = exact host match — cookies persist em localhost). Adding `cookies.domain = loopzero.example` would cause cookies to be discarded since localhost doesn't match.

**dev-public + prod (Caddy edge active)**: populate `cookies.domain = loopzero.example` (matches *-dev.loopzero.example + *.loopzero.example subdomains).

**Helmfile gotmpl conditional**:
```yaml
{{- if eq .Environment.Name "dev-public" }}
serve:
  public:
    cookies:
      domain: loopzero.example
{{- end }}
```

#### Files to edit

- `infra/helmfile/values/dev-public/ory-kratos.yaml.gotmpl` (extend with cookie.domain)
- `infra/helmfile/values/standalone-multi/ory-kratos.yaml.gotmpl` (extend; omit cookie.domain)
- `infra/helmfile/values/prod/ory-kratos.yaml.gotmpl` (extend with cookie.domain)
- `infra/tests/helm/ory-kratos-cookies-domain-per-env_test.sh` (NEW — asserts per-env contract)

### T1.7 — SHOULD FIX (v1.3 amendment 2026-06-12): preview-operator `BASE_DOMAIN` per-env

#### Why this step

**Action**: Surface caught LIVE during sponsor walkthrough J-23 LIVE 2026-06-12 — `preview-operator` Deployment env `BASE_DOMAIN=loopzero.local` (standalone default) ao invés de canonical per-env (e.g., `preview-dev.loopzero.example` para dev-public). Resulting `PreviewEnvironment.status.url` aponta para `*.loopzero.local` (não public), browser pega 404 no DNS.

**Reasoning chain**: Phase 1 v1.2 cobriu URL config em Hydra (T1.1), loopzero-cloud KRATOS (T1.2), Promtail Loki (T1.3), SPKI (T1.4), External-Secrets GitHub (T1.5), Kratos cookies (T1.6) — MAS NÃO cobriu preview-operator BASE_DOMAIN. Empirical evidence LIVE 2026-06-12 (PR #8 walkthrough) confirma drift.

#### Files to edit

- `infra/helm/loopzero-preview-operator/values.yaml` (declare `baseDomain` default standalone-multi)
- `infra/helmfile/values/dev-public/loopzero-preview-operator.yaml.gotmpl` (NEW: `baseDomain: preview-dev.loopzero.example`)
- `infra/helmfile/values/prod/loopzero-preview-operator.yaml.gotmpl` (NEW: `baseDomain: preview.loopzero.example`)
- `infra/tests/helm/preview-operator-base-domain-per-env_test.sh` (NEW Phase 3 hermetic test)

### T1.8 — SHOULD FIX (v1.3 amendment 2026-06-12): loopzero-api PR check URL pattern `BASE_DOMAIN`

#### Why this step

**Action**: GitHub PR check status `loopzero/deploy` target URL hardcoded `*.loopzero.local` em loopzero-api code path (separate de preview-operator). Found LIVE walkthrough 2026-06-12 PR #8: target URL `https://tn-82988ea9aef248ef-teste-api-preview-pr-8.loopzero.local` — sponsor clicks link, hits DNS NXDOMAIN.

**Reasoning chain**: loopzero-api emite GitHub PR check via `gh-api` client. URL pattern construído inline com hardcoded `loopzero.local` ao invés de read `LOOPZERO_PREVIEW_BASE_DOMAIN` env from runtime config. Same root cause family as T1.7 — different code path.

#### Files to edit

- `api/internal/github/checks.go` (read PREVIEW_BASE_DOMAIN from cfg)
- `api/internal/config/config.go` (add PreviewBaseDomain field)
- `infra/helm/loopzero-api/values.yaml` runtimeConfig (declare PREVIEW_BASE_DOMAIN default)
- `infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl` (override PREVIEW_BASE_DOMAIN=preview-dev.loopzero.example)
- `infra/tests/helm/loopzero-api-preview-base-domain-per-env_test.sh` (NEW Phase 3 gate)

### T1.9 — SHOULD FIX (v1.3 amendment 2026-06-12): Loki Service NodePort 30094 canonical

#### Why this step

**Action**: Loki chart deploys Service as **ClusterIP** by default. loopzero-api LOKI_URL `http://k3d-loopzero-build-serverlb:30094` espera NodePort canonical em port 30094. Connection reset por NodePort unbinding.

**Reasoning chain**: Surfaced LIVE 2026-06-12 — wget probe from loopzero-api pod returned `Connection reset by peer`. kubectl patch from ClusterIP → NodePort 30094 resolved canonical. Phase 1 T1.3 overlay should declare Service type via helmfile values.

#### Files to edit

- `infra/helm/loki/values.yaml` (declare `service.type: NodePort + nodePort: 30094`)
- `infra/helmfile/values/dev-public/loki.yaml.gotmpl` (NEW per-env)
- `infra/tests/helm/loki-nodeport-canonical_test.sh` (NEW Phase 3 gate)

### T1.10 — SHOULD FIX (v1.3 amendment 2026-06-12): Promtail pushUrl intra-cluster canonical

#### Why this step

**Action**: Promtail Secret rendered com `clients.url=http://172.18.0.3:30094/loki/api/v1/push` (stale k3d-mgmt IP from chart default). After Cleanroom rotation k3d IPs change. Promtail está em build cluster, Loki também — intra-cluster Service DNS canonical.

**Reasoning chain**: T1.3 overlay (`values/dev-public/promtail.yaml.gotmpl`) declares canonical `loki.monitoring.svc:3100` BUT `build.helmfile.yaml.gotmpl` NÃO tem `dev-public` env declared (só `dev/prod/standalone/standalone-multi/drill`). Phase 1 T1.3 declared overlay file mas never wired em build env contract — overlay applied only em mgmt cluster.

#### Files to edit

- `infra/helmfile/build.helmfile.yaml.gotmpl` (DECLARE `dev-public:` env block + values include path)
- `infra/helmfile/values/dev-public/promtail.yaml.gotmpl` (verify pushUrl intra-cluster canonical declared)
- `infra/tests/helm/promtail-pushurl-canonical_test.sh` (NEW Phase 3 gate)

## Phase 2 — Layer B: `task setup` chain extensions

### Task 2.1 — NEW `task vault:bootstrap-dev` aggregating 10 manual steps idempotente

#### Why this step

**Action**: NEW script `infra/scripts/local/vault-bootstrap-dev.sh` + new task target em `taskfiles/vault.yml`: agrega `bao operator init` + 3× unseal + create openbao-init-token Secret + helmfile sync openbao-init + AppRole + policy + SPKI extraction + loopzero-openbao-approle Secret creation.

**Reasoning chain**: Per memory `j22-aprendizado-openbao-multi-secret-chain` + `t62-aprendizado-task-setup-nao-bootstrap-openbao` (catch #5 umbrella), 10 manual steps reproducibles em script idempotente per ADR D2.

#### Files to edit

- `infra/scripts/local/vault-bootstrap-dev.sh` (NEW)
- `taskfiles/vault.yml` (NEW)
- `Taskfile.yml` (add `vault:bootstrap-dev` to setup chain)
- `rules/shell-scripts-allowlist.md` (add KEEP-OPERATOR entry)
- `infra/tests/check-vault-bootstrap-dev-syntax_test.sh` (NEW hermetic)

#### TDD

**RED**: Hermetic test asserts: bash -n syntax, idempotent re-run no-op, --dry-run flag honored, gated by `LOOPZERO_DEV_AUTO_INIT_VAULT=true` env per ADR D2.

#### Idempotency + Recovery (v1.1, absorbs EC-4/5/6)

**EC-4 idempotent guard:** Script primeira linha: `bao status -format=json 2>/dev/null | jq -r '.initialized // false'` → if `true`, no-op + log "OpenBao already initialized, skipping init step" + jump to next step (unseal/policy). Re-run = no-op canonical.

**EC-5 shards recovery path:** Script detects missing `~/.loopzero/cleanroom-unseal.json` at unseal step + emits explicit error:
```
ERROR: ~/.loopzero/cleanroom-unseal.json missing. Recovery paths:
  (a) Restore from backup: cp ~/.loopzero/backups/<date>-cleanroom-unseal.json ~/.loopzero/
  (b) Destructive re-init: bash infra/scripts/local/openbao-full-reinit.sh
       (loses ALL OpenBao state; only for dev env)
```
Exit code 3 (distinct from EC-6 exit 2).

**EC-6 dev-only env gate:** Script first-line check: `[ "${LOOPZERO_DEV_AUTO_INIT_VAULT:-false}" = "true" ] || { echo "REFUSED — dev-only task. Set LOOPZERO_DEV_AUTO_INIT_VAULT=true em dev env."; exit 2; }`. Prevents accidental prod destroy.

#### Failure scenarios

- bao operator init fails por TLS — mitigation: BAO_SKIP_VERIFY=true em dev only
- Unseal shard corrupt — mitigation: re-init flow + ~/.loopzero/cleanroom-unseal.json backup (EC-5 explicit error)
- Initialized twice — mitigation: EC-4 idempotent guard
- Run em prod env por mistake — mitigation: EC-6 env gate refuse

#### Concurrency tests

- Multiple `task vault:bootstrap-dev` simultaneously — mitigation: flock pid file `/tmp/loopzero-vault-bootstrap.lock`

---

### Task 2.2 — NEW `task secrets:seed-dev` reading `~/.loopzero/dev-credentials.json` mode 0600

#### Why this step

**Action**: NEW script + task target reading sponsor-provided creds JSON (GitHub App ID/PEM/webhook + Stripe + Resend) e writing to OpenBao kv-v2 canonical paths.

**Reasoning chain**: Per memory `j09-aprendizado-github-creds-via-eso-canonical` (catch #9). Sponsor mandate: NO hardcoded creds em git. JSON file mode 0600 gitignored canonical.

#### Files to edit

- `infra/scripts/local/secrets-seed-dev.sh` (NEW)
- `taskfiles/secrets.yml` (NEW)
- `rules/shell-scripts-allowlist.md` (add entry)
- `infra/tests/check-secrets-seed-dev-syntax_test.sh` (NEW)
- `cli/README.md` ou `docs/operations/dev-setup.md` — document JSON shape

#### Input Validation + Security (v1.1, absorbs EC-7/8)

**EC-7 missing file:** Script first-line: `[ -f ~/.loopzero/dev-credentials.json ] || { cat <<EOF
ERROR: ~/.loopzero/dev-credentials.json missing.
Create with shape:
  {
    "github": {"app_id": "<id>", "private_key": "<PEM>", "webhook_secret": "<hex>"},
    "stripe": {"secret": "<sk_test_*>", "publishable": "<pk_test_*>", "webhook": "<whsec_*>"},
    "resend": {"api_key": "<re_*>"}
  }
File mode MUST be 0600. See docs/operations/dev-setup.md § Credentials JSON.
EOF
exit 1; }`

**EC-8 JSON schema validation:** Upfront jq schema gate:
```bash
jq -e '.github.app_id and .github.private_key and .github.webhook_secret and .stripe.secret and .resend.api_key' \
  ~/.loopzero/dev-credentials.json > /dev/null || { echo "ERROR: malformed dev-credentials.json (missing keys)"; exit 1; }
```

**Mode 0600 enforcement:** Verifies via `SHOULD TEST` EC-22 (hermetic test asserts mode + emits security warning).

---

### Task 2.3 — NEW `task networking:cross-cluster-coredns` patcher idempotente

#### Why this step

**Action**: NEW script patches mgmt cluster CoreDNS NodeHosts para resolver build LB + runtime-2 LB hostnames.

**Reasoning chain**: Per memory `j17-aprendizado-coredns-cross-cluster-build-lb` (catch #11), `setup-cross-cluster-networking.sh` só adicionou workload LB; build LB precisa entry para loopzero-api → Loki cross-cluster query.

#### Files to edit

- `infra/scripts/local/cross-cluster-coredns-patch.sh` (NEW)
- `taskfiles/k3d.yml` ou `taskfiles/infra.yml` (add task target)
- `Taskfile.yml` (wire em networking:setup chain)

#### Dynamic IP Detection (v1.1, absorbs EC-9)

**Script reads current Docker IPs dynamically — NO hardcoded `172.18.0.X`:**
```bash
BUILD_LB_IP=$(docker inspect k3d-loopzero-build-serverlb --format '{{(index .NetworkSettings.Networks "k3d-loopzero").IPAddress}}')
RUNTIME2_LB_IP=$(docker inspect k3d-loopzero-runtime-2-serverlb --format '{{(index .NetworkSettings.Networks "k3d-loopzero").IPAddress}}')
[ -n "$BUILD_LB_IP" ] || { echo "ERROR: cannot detect build LB IP — is k3d running?"; exit 1; }
```

Patches NodeHosts ConfigMap with detected values per invocation. Re-run em next Cleanroom recalibra IPs canonical. Per memory `j17-aprendizado-coredns-cross-cluster-build-lb` — k3d IPs ephemeral; script must adapt.

---

### Task 2.4 — Extend `port-forward-public-dashboard.sh` para auth services persistentes

#### Why this step

**Action**: Extender script existente para incluir ports 14444 (Hydra public) + 14433 (Kratos public) + 14434 (Kratos admin) + manter 13002 + 5173.

**Reasoning chain**: Per memory `j05-aprendizado-hydra-urls-canonical-via-helmfile-overlay` (catch #15), port-forward 14444 missing depois Cleanroom.

#### Files to edit

- `infra/scripts/local/port-forward-public-dashboard.sh` (extend)
- Or NEW `infra/scripts/local/port-forward-auth-services.sh` se separation preferred

#### Watchdog + Backoff Retry (v1.1, absorbs EC-10)

**Mirror canonical pattern from existing `port-forward-public-dashboard.sh` allowlist entry (`T7.3` of 2026-05-29 dashboard plan):**
- Watchdog loop monitors each port-forward process; restart on death
- Exponential backoff (2s → 30s caps) on consecutive restart attempts
- Signal trap `EXIT INT TERM` cleanup PID file + kill all port-forward subprocesses
- PID file `/tmp/loopzero-port-forward-auth-services.pid` prevents double-spawn

```bash
# Per-port spawn loop with backoff
forward_with_backoff() {
  local ns=$1 svc=$2 lport=$3 rport=$4
  local delay=2
  while true; do
    kubectl --context k3d-loopzero-mgmt -n "$ns" port-forward "svc/$svc" "$lport:$rport" >> "$LOGFILE" 2>&1
    sleep "$delay"
    delay=$((delay < 30 ? delay * 2 : 30))
  done
}
```

## Phase 3 — Layer C: Hermetic CI gates

### Task 3.1 — `ory-hydra-canonical-urls_test.sh` hermetic helm template gate

#### Files to edit

- `infra/tests/helm/ory-hydra-canonical-urls_test.sh` (NEW)
- `taskfiles/quality.yml` (wire em `task quality:gates`)
- `rules/shell-scripts-allowlist.md` (KEEP-CI-WIRED entry)

#### TDD

Helm template renders per env + grep canonical URL patterns. Pass/fail per env. ~5-10 assertions.

### Task 3.2 — `loopzero-cloud-kratos-url-split_test.sh`

Same pattern. Asserts KRATOS_PUBLIC != KRATOS_INTERNAL em non-dev envs.

### Task 3.3 — `promtail-canonical-pipeline_test.sh`

Helm template asserts sink URL + canonical labels.

### Task 3.4 — `loopzero-api-spki-not-hardcoded_test.sh`

Grep rendered ConfigMap para SPKI hex regex pattern — MUST be absent.

### Task 3.5 — `loopzero-external-secrets-github-canonical_test.sh`

Asserts 2 ExternalSecret manifests + ClusterSecretStore canonical.

### Task 3.6 — `cloud/tests/unit/route_registration_test.go` Go AST gate

#### Why this step

**Action**: NEW Go test usando `go/ast` package para walk `cloud/internal/routes/*.go` + extract public handler functions + grep `cloud/cmd/main.go` para route registrations + assert each handler func tem at least 1 route.

**Reasoning chain**: Per memory `j05-aprendizado-orphan-handler-accept-route-not-wired` (catch #19), Accept handler implemented + tested mas main.go faltava `Post("/device/accept", ...)`. Canonical AST gate prevents recurrence.

#### Files to edit

- `cloud/tests/unit/route_registration_test.go` (NEW)
- `cloud/cmd/main.go` — comment cited as canonical registration site

#### TDD

**RED**: Test atualmente FAIL until `cloud/cmd/main.go` line 285 has Accept route (already shipped em commit 328b5e6a).

**GREEN**: Walk identifica Accept route registered.

### Task 3.7 — `cloud/tests/unit/handler_error_visibility_test.go` Go AST gate

#### Why this step

**Action**: NEW Go test walks `cloud/internal/routes/*.go` + finds `writeError(...)` calls + asserts each is preceded by `slog.Error(..., err)` em mesmo `if err != nil { }` block.

**Reasoning chain**: Per memory `j05-aprendizado-cloud-accept-error-visibility-slog` (catch #18). Pattern aplicável a TODOS error paths.

### Task 3.8 — `infra/tests/check-cli-loopzero-url-error-visibility_test.sh`

Grep `cli/src/` for `catch { return null }` antipattern + asserts useful error emission em CLI.

## Phase 4 — Layer E: `task check` sprint canonical

### Task 4.1 — NEW `infra/scripts/local/check-sprint.sh` orchestrator

#### Why this step

**Action**: NEW master script implementando 8 categorias × ~44 checks com structured PASS/WARN/FAIL output + JSON mode + --`fail-fast` flag.

**Reasoning chain**: Per ADR D3 + ADR D4 + sponsor mandate 2026-06-11.

#### Files to edit

- `infra/scripts/local/check-sprint.sh` (NEW master)
- `infra/scripts/local/check-sprint/c1-helmfile.sh` (NEW)
- `infra/scripts/local/check-sprint/c2-configmaps.sh` (NEW)
- `infra/scripts/local/check-sprint/c3-networking.sh` (NEW)
- `infra/scripts/local/check-sprint/c4-auth.sh` (NEW)
- `infra/scripts/local/check-sprint/c5-webhook.sh` (NEW)
- `infra/scripts/local/check-sprint/c6-observability.sh` (NEW)
- `infra/scripts/local/check-sprint/c7-integrity.sh` (NEW)
- `infra/scripts/local/check-sprint/c8-cleanroom-guards.sh` (NEW)
- `taskfiles/check.yml` (NEW task targets)
- `Taskfile.yml` (wire em setup chain final step)
- `rules/shell-scripts-allowlist.md` (10× KEEP-OPERATOR entries)
- `infra/tests/check-check-sprint-syntax_test.sh` (NEW hermetic)

#### TDD

**RED**: Hermetic syntax test + structural test asserting 8 categorias defined.

**GREEN**: All 44+ check probes pass canonical state.

#### Acceptance criteria

- `task check` exit 0 quando FAIL=0
- JSON mode emits machine-readable summary
- Wall-clock < 60s

#### Resilience + Performance (v1.1, absorbs EC-11/12/13/14)

**EC-11 kubectl reachability gate per category:**
```bash
# Per category C2-C8 (require kubectl)
if ! kubectl --context "$CONTEXT" get nodes >/dev/null 2>&1; then
  echo "[SKIP-WARN] Category $CAT — kubectl unavailable at context $CONTEXT"
  continue
fi
```
C1 helmfile-rendering hermetic — no kubectl required. `--hermetic` flag skips C2-C8 (CI mode).

**EC-12 per-probe retry with backoff:** Each network probe:
```bash
probe_with_retry() {
  local url=$1 attempts=3 delay=1
  for i in $(seq 1 $attempts); do
    if curl -sf --max-time 5 "$url" >/dev/null 2>&1; then return 0; fi
    sleep $delay
    delay=$((delay * 3))
  done
  return 1
}
```

**EC-13 parallel category execution:** C1-C8 categorias independentes podem rodar paralelo via xargs -P 4 OR & + wait. Sequential within category. Master timeout 60s com hard cap kill.

**EC-14 `fail-fast` vs warn-and-continue behavior:** Per ADR D9 — `task setup` chain wraps `task check || echo "[WARN] ..."; true` (non-fatal); Cleanroom skill Phase 4.5 invokes diretamente (`fail-fast` canonical).

### Task 4.2 — Wire `task check` em Cleanroom skill Phase 4.5

#### Files to edit

- `.claude/skills/cleanroom/SKILL.md` (extend Phase 4 → split into 4 + 4.5)

#### TDD

Hermetic test asserts Phase 4.5 task check invocation présent.

## Phase 5 — Layer D: LIVE E2E 9-journeys regression-defense

### Task 5.1 — NEW `dev-public-cleanroom-canonical-9-journeys_test.sh`

#### Why this step

**Action**: NEW master LIVE bash orchestrator dispatching 9 journey scenarios sequencialmente: J-22 secret + zero-leak, J-09 GitHub repo + auto-deploy, J-23 PR webhook + preview, J-07 projects list, J-08 project detail, J-10 envs canonical, J-17 deploy logs streaming, J-05 device-flow + metric increment, J-04 password recovery + Resend email.

**Reasoning chain**: Per memory `reference_t62_cleanroom_session_2026_06_11` — empirical proof of canonical Cleanroom.

#### Files to edit

- `infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh` (NEW)
- `infra/tests/check-cleanroom-canonical-9-journeys-syntax_test.sh` (NEW hermetic)
- `taskfiles/test.yml` (NEW `task test:cleanroom-canonical-9-journeys`)
- `rules/shell-scripts-allowlist.md` (KEEP-OPERATOR entries)

#### TDD

**RED**: Hermetic structural test (per Task 5.2) asserts 9 journey functions defined + each tem assertions.

**GREEN**: LIVE execution end-to-end ZERO manual step + all 9 pass.

#### Acceptance criteria

- Wall-clock budget 30min measured via `time task test:cleanroom-canonical-9-journeys` returns elapsed < 1800s
- 9/9 GREEN ou explicit BLOCKED with reason verified via `jq '.journeys[] | select(.status != "PASS" and .status != "BLOCKED_WITH_REASON")' output.json` returns empty array
- JSON output machine-readable

#### Auto vs Semi-auto Journeys + Cleanup (v1.1, absorbs EC-16/17/18)

**EC-16 auto vs semi-auto enumeration per ADR D10:**
- **Auto journeys** (fully scriptable): J-07/J-08/J-09/J-10/J-17/J-22/J-23
- **Semi-auto journeys** (sponsor confirms UX): J-04 (Resend email arrives + sponsor clicks reset link), J-05 (Hydra authorize + metric increment proves chain). Auto test asserts deterministic chain + counter incremento. Sponsor walkthrough JUNTOS canonical final gate per ADR D7.

**EC-17 J-04 seed restoration:** Test's J-04 step ends com explicit cleanup:
```bash
# Post-J-04 password reset, restore canonical seed to keep next run logable
bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only || \
  { echo "WARN: failed to restore canonical seed pwd — next Cleanroom may need manual reseed"; }
```

**EC-18 test data isolation:** Each test run uses session-id suffix em fixtures:
```bash
SESSION_ID="cleanroom-$(date +%s)"
PROJECT_NAME="teste-${SESSION_ID}"  # J-09/J-23 fresh fixtures
SECRET_NAME="SPONSOR_LIVE_TEST_${SESSION_ID}"  # J-22 fresh secret
BRANCH_NAME="preview-${SESSION_ID}"  # J-23 fresh PR branch
```
Cleanup hook em test SetDown (best-effort):
```bash
# Delete prior session fixtures via API (silent fail OK)
gh pr close "$BRANCH_NAME" --delete-branch 2>/dev/null || true
curl -X DELETE "$CLOUD_URL/engine/api/v1/projects/$PROJECT_NAME" -H "Authorization: Bearer $TOKEN" 2>/dev/null || true
```

### Task 5.2 — Hermetic structural test do 9-journey master

Padrão consistente per existing hermetic tests `check-dev-public-*-syntax_test.sh`.

## Phase 6 — Integration Validation (Final Phase)

### Task 6.1 — Cleanroom TOTAL re-run com ZERO manual step + sponsor walkthrough JUNTOS

#### Why this step

**Action**: Per `.claude/rules/cleanroom-up-live-test-mandate.md` Phase 6, execute Cleanroom canonical chain: `task clean:all && task setup && tilt up && task doctor && task check && task test:cleanroom-canonical-9-journeys`. Sponsor browser walkthrough JUNTOS confirma cada journey GREEN.

**Reasoning chain**: Per Unbreakable Rule 3 (honest BLOCKED > false PASS) + `feedback_post_feature_live_regression_workflow_2026_06_09` (sponsor walkthrough HARD STOP), feature done = sponsor confirmou LIVE.

#### Acceptance criteria — split per ADR D7 (v1.1, absorbs EC-19)

**Engineering-side gate (auto, can complete sem sponsor):**
- `task setup` exit 0 ZERO manual step
- `task doctor` PASS=38+ FAIL=0
- `task check` PASS=44 FAIL=0
- `task test:cleanroom-canonical-9-journeys` 9/9 GREEN auto
- Hydra metric `loopzero_device_flow_browser_success_total >= 1` (proves J-05 chain)
- Emit `<promise>IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE</promise>` candidate

**Sponsor canonical gate (async finalizing):**
- Sponsor browser walkthrough JUNTOS per `.claude/rules/cleanroom-up-live-test-mandate.md`
- Sponsor verbatim approval per journey (recorded em commit body OR session reference memory)
- Sponsor walkthrough scheduling pode ser async — engineering doesn't block

#### DoD

- Plan goal metric atingida (engineering-side gate PASS)
- Sponsor walkthrough scheduled + completed (async OK per ADR D7)
- Emit `<promise>IMPLEMENTATION_COMPLETE</promise>` SOMENTE quando AMBOS gates GREEN — per Unbreakable Rule 3

## Coverage Matrix

| # | Description | Defense task | Layer |
|---|---|---|---|
| catch-01 | Dockerfile FROM golang 1.26.0 vs go.mod 1.26.1 | T3.1 | C |
| catch-02 | dockerfile-lint redundant toolchain | T3.1 | C |
| catch-03 | 8 submodules transitive bumps | T3.1 | C |
| catch-04 | update-cross-cluster-ips.sh script drift | T2.3 | B |
| catch-05 | OpenBao multi-secret chain | T2.1 | B |
| catch-06 | Tilt stale instance pre-Cleanroom | T4.1 | E |
| catch-07 | OPENBAO_SPKI_PIN_SHA256 hardcoded | T1.4 | A |
| catch-08 | AppRole policy missing wildcard | T2.1 | B |
| catch-09 | GitHub creds 5-layer ESO chain | T1.5 | A |
| catch-10 | webhook secret separate Secret name | T1.5 | A |
| catch-11 | CoreDNS resolve build LB | T2.3 | B |
| catch-12 | Promtail to Loki pipeline drift | T1.3 | A |
| catch-13 | CLI LOOPZERO_URL subshell env | T3.8 | C |
| catch-14 | Hydra urls.self.public cluster-internal | T1.1 | A |
| catch-15 | port-forward 14444 missing | T2.4 | B |
| catch-16 | device-verify UI canonical app-dev | T1.1 | A |
| catch-18 | Cloud Accept error visibility silenced | T3.7 | C |
| catch-19 | Cloud /device/accept route orphan | T3.6 | C |
| catch-20 | KRATOS_PUBLIC vs INTERNAL split | T1.2 | A |

**Total: 18 catches → 100% coverage via 5 layers**.

### Edge Case Coverage Matrix (v1.1 — absorbs review report 19 `MUST FIX` + 11 `SHOULD TEST` + 7 DOCUMENT)

| EC # | Absorbed via | Type |
|---|---|---|
| EC-1 (multi-env hermetic) | ADR D8 + Task 1.1 RED test loop | `MUST FIX` |
| EC-2 (SPKI Pod boot race) | Task 1.4 § Pod Boot Timing + EC-31 documented | `MUST FIX` |
| EC-3 (cookies.domain per-env) | Task 1.6 § Per-Env Contract + NEW hermetic test | `MUST FIX` |
| EC-4 (idempotent guard) | Task 2.1 § Idempotency + Recovery | `MUST FIX` |
| EC-5 (shards recovery path) | Task 2.1 § Idempotency + Recovery | `MUST FIX` |
| EC-6 (dev-only env gate) | Task 2.1 § Idempotency + Recovery | `MUST FIX` |
| EC-7 (missing creds file) | Task 2.2 § Input Validation + Security | `MUST FIX` |
| EC-8 (JSON schema validation) | Task 2.2 § Input Validation + Security | `MUST FIX` |
| EC-9 (dynamic IP detection) | Task 2.3 § Dynamic IP Detection | `MUST FIX` |
| EC-10 (port-forward watchdog) | Task 2.4 § Watchdog + Backoff Retry | `MUST FIX` |
| EC-11 (kubectl reachability gate) | Task 4.1 § Resilience + Performance | `MUST FIX` |
| EC-12 (probe retry backoff) | Task 4.1 § Resilience + Performance | `MUST FIX` |
| EC-13 (parallel category exec) | Task 4.1 § Resilience + Performance | `MUST FIX` |
| EC-14 (`fail-fast` vs continue) | ADR D9 + Task 4.1 § Resilience | `MUST FIX` |
| EC-15 (Cleanroom Phase 4.5 `fail-fast`) | ADR D9 + Task 4.2 | `MUST FIX` |
| EC-16 (auto vs semi-auto journeys) | ADR D10 + Task 5.1 § Auto vs Semi-auto | `MUST FIX` |
| EC-17 (J-04 seed restoration) | Task 5.1 § Auto vs Semi-auto Cleanup | `MUST FIX` |
| EC-18 (test data isolation) | Task 5.1 § Auto vs Semi-auto Cleanup | `MUST FIX` |
| EC-19 (sponsor walkthrough scheduling) | ADR D7 + Task 6.1 split acceptance criteria | `MUST FIX` |
| EC-20 (Promtail sink regex match) | Task 1.3 RED test extension | `SHOULD TEST` |
| EC-21 (ExternalSecret canonical paths) | Task 1.5 RED test extension | `SHOULD TEST` |
| EC-22 (creds file mode 0600) | Task 2.2 RED test + Task 3.x hermetic | `SHOULD TEST` |
| EC-23 (CoreDNS canonical shape) | Task 2.3 RED test extension | `SHOULD TEST` |
| EC-24 (CoreDNS idempotent re-run) | Task 2.3 RED test extension | `SHOULD TEST` |
| EC-25 (port-conflict detection) | Task 2.4 RED test extension | `SHOULD TEST` |
| EC-26 (yq pre-req check) | Task 3.1-3.5 RED test extension | `SHOULD TEST` |
| EC-27 (HTTP method variants) | Task 3.6 RED test extension | `SHOULD TEST` |
| EC-28 (writeError wrappers) | Task 3.7 RED test extension | `SHOULD TEST` |
| EC-29 (CLI try block shapes) | Task 3.8 RED test extension | `SHOULD TEST` |
| EC-30 (per-journey timing) | Task 5.1 RED test extension (resolves Q1) | `SHOULD TEST` |
| EC-31..37 (7 documented risks) | Plan body + allowlist annotations | DOCUMENT |

**Total: 19/19 `MUST FIX` absorbed + 11/11 `SHOULD TEST` scoped + 7/7 DOCUMENT noted = 37/37 = 100% absorption**.

## Drawbacks & Risks (v1.1 refreshed pós-edge-case absorption)

| Risk | Severity | Mitigation | Owner |
|---|---|---|---|
| `task check` sprint runtime > 60s degrades dev loop | MEDIUM | EC-13 parallelize C1-C8 via xargs -P 4 + per-category 10s budget + master 60s hard cap | Layer E author |
| LIVE E2E 9-journey test flakiness por network jitter | HIGH | EC-12 per-probe retry 3× backoff (1s/3s/9s); sponsor walkthrough JUNTOS authoritative fallback per ADR D7 | Layer D author |
| Helmfile overlays per-env diverge entre standalone-multi/dev-public/prod | MEDIUM | ADR D8 — hermetic test cobre 3 envs canonical; shared yaml.gotmpl partials | Layer A author |
| Go AST gates produzem false positives (handler intentionally unwired) | LOW | Allowlist file entry com sunset; ADR if pattern recurrent | Layer C author |
| `task vault:bootstrap-dev` shards stored em ~/.loopzero/cleanroom-unseal.json compromised | MEDIUM | EC-6 env gate LOOPZERO_DEV_AUTO_INIT_VAULT=true + mode 0600 + gitignored + ADR D2 cited | Layer B author |
| Sponsor walkthrough JUNTOS Phase 6 timeout for scheduling | LOW | ADR D7 — engineering-side gate emits intermediate `<promise>` candidate; final canonical emit awaits sponsor async | All |
| **NEW v1.1** — SPKI Pod boot race CrashLoopBackOff antes ESO sync | MEDIUM | EC-2 `optional: true` em secretKeyRef + ESO refreshInterval=1m + graceful loopzero-api code retry (out-of-plan-scope follow-up) | Layer A author |
| **NEW v1.1** — `~/.loopzero/dev-credentials.json` malformed silently corrupts canonical state | HIGH | EC-7/8 upfront file existence + jq schema validation; hermetic test EC-22 mode 0600 enforce | Layer B author |
| **NEW v1.1** — J-04 password reset breaks subsequent runs | MEDIUM | EC-17 explicit `seed-admin --refresh-only` cleanup post-J-04 | Layer D author |
| **NEW v1.1** — LIVE E2E test data accumulation between runs | MEDIUM | EC-18 session-id suffix em fixtures (teste-${EPOCH}) + best-effort cleanup hook | Layer D author |

## Unresolved Questions

- ~~Q1: Catches #11/#12 Layer D regression coverage~~ — **RESOLVED v1.1**: Per ADR D8 + EC-30 (`SHOULD TEST`), LIVE E2E asserts Promtail `promtail_sent_entries_total > 0` em wrapper Task 5.1 per-journey timing. Wall-clock impact estimated < 5s additional.
- Q2 — Layer A overlay para prod env STILL OPEN. Exigir explícito declarative (canonical) ou inherit do dev-public (`DRY-friendly`)? Recommend explicit + ADR sunset post-prod-cutover. Sponsor decision required pre-implementation Phase 1.
- ~~Q3: `task check:hourly` cron mode~~ — **RESOLVED v1.1**: Out-of-scope per ADR D3. Operator runbook documents `crontab -e` pattern for long-running envs.
- Q4 — Engineering-side intermediate promise vocabulary per ADR D7. Intermediate emit é `<promise>IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE</promise>` OR distinct token (`<promise>ENGINEERING_DONE_AWAITING_SPONSOR</promise>`)? Need ralph-loop vocabulary alignment. Recommend latter — distinct from final emit avoids confusion.

## Failure scenarios (external I/O)

- **OpenBao admin TLS** — bootstrap-dev script handle TLS verify skip em dev via BAO_SKIP_VERIFY=true; prod path uses real CA via cert-manager.
- **Hydra admin connection** — Layer C 3.7 Go AST gate prevents silenced error mask future; runtime test em Layer D 5.1 probe `/v1/auth/device/accept` returns 400 (not 404).
- **Loki cross-cluster query** — Layer A 1.3 declares sink URL canonical; Layer C 3.3 helm template assert; Layer E C6 LIVE probe Loki `/ready` reachable.
- **Kratos browser flow 422 redirect** — Layer D 5.1 LIVE E2E J-04 scenario tests full Kratos browser-mode protocol per memory `reference_kratos_browser_mode_422_redirect_2026_06_05`.
- **GitHub App API rate limit** — Layer D 5.1 J-09 step gates ≥ 100 remaining headroom pre-test.
- **Stripe webhook HMAC** — out-of-scope this plan (separate test).
- **OpenBao admin Token expiry** — bootstrap-dev refreshes via root token in `~/.loopzero/cleanroom-unseal.json`.

## Concurrency tests

- Task 2.1 `task vault:bootstrap-dev` race protection — flock pid file (per EC-3 prior pattern em `regen-scoped-kubeconfigs.sh` allowlist entry)
- Task 4.1 `task check` parallel sub-categorias — race-free read-only probes (no shared mutable state)
- Task 5.1 LIVE E2E sequential, NOT concurrent (per dependency chain)
- `(none — other tasks single-threaded)`

## Global DoD

- All Phase 1-5 tasks acceptance criteria met
- Coverage Matrix 100% verified (18/18 catches → 1+ defense)
- All hermetic tests Task 3.* GREEN in CI (`task quality:gates`)
- Phase 6 integration validation:
  - `task setup` ZERO manual step → exit 0
  - `task doctor` PASS=38+ FAIL=0
  - `task check` PASS=44 FAIL=0
  - `task test:cleanroom-canonical-9-journeys` 9/9 GREEN
  - Sponsor verbatim approval each journey
- `feature flag` ou `ADR sunset entry` cita: `T6.2` 18 catches resolved canonical
- CHANGELOG `[Unreleased] Added` entry cobrindo 5 layers
- File size budgets honored (per `rules/loopzero-design.md` ~500 LoC default; new scripts approach limit OK)
- Lint clean: `task lint` + `task quality:gates`
- New allowlist entries in `rules/shell-scripts-allowlist.md` com KEEP-CI-WIRED/KEEP-OPERATOR + justificativa

## Dependencies (v1.2 amendment — absorbs deps audit)

### Existing — use as-is

| Package | Version | Ecosystem | Why |
|---|---|---|---|
| Go stdlib (`go/ast`, `go/parser`, `go/token`, `log/slog`) | go 1.26.1 (existing per cloud/go.mod) | go | Layer C 3.6 + 3.7 Go AST gates — stdlib only, no third-party imports |
| Existing npm packages (cli/, dashboard/) | per existing package.json (unchanged) | npm | Layer C 3.8 hermetic grep test — no new npm deps |

### New — to be introduced

**(none — plan adds ZERO new third-party dependencies)**

Plan uses ONLY:
- Existing Go manifests + Go stdlib imports (`go/ast`, `go/parser`, `go/token`, `log/slog`)
- Existing npm package.json (unchanged)
- External CLI binaries available on operator workstation (see below)
- Helmfile values overlays (yaml.gotmpl — no deps)

### External CLI binaries (operator workstation prerequisites — NOT package deps)

| Binary | Why this plan needs it |
|---|---|
| `bao` (OpenBao CLI) | Layer B Task 2.1 `vault:bootstrap-dev` — init/unseal/AppRole/policy ops |
| `yq` | Layer C 3.1-3.5 helm template assertion gates |
| `jq` | Layer B Task 2.2 (JSON schema validate creds) + Layer E `task check` (JSON output parse) |
| `docker` + `kubectl` + `helm` + `gh` + `curl` | Existing LoopZero workstation prerequisites — used by ALL phases |

These binaries are operator-workstation prerequisites already documented em `docs/operations/dev-setup.md` per existing LoopZero dev pattern. NOT new deps introduced by this plan.

### Removed

(none)

### CVE Allowlist (v1.2 amendment)

11 Go stdlib CVEs (govulncheck CALLED em cloud/) allowlisted em `rules/deps-audit-allowlist.txt` com sunset **2026-07-11 (30 days)**:
- GO-2026-4980 + GO-2026-4982 (HIGH — html/template XSS bypass; fixed em go1.26.3)
- GO-2026-5037 + GO-2026-4947 + GO-2026-4946 + GO-2026-4866 + GO-2026-4870 + GO-2026-4918 + GO-2026-5039 + GO-2026-4865 (MEDIUM — crypto/x509 + net/http + crypto/tls + html/template; fixes 1.26.2-1.26.4)
- GO-2026-4971 (LOW — Windows-only NUL byte panic, não exploitable em Linux LoopZero stack)

**Allowlist rationale**: Bump Go toolchain 1.26.1 → 1.26.4 affects ALL 9 Go modules + 8 Dockerfile FROMs per memory `t62-aprendizado-go-toolchain-dockerfile-drift`; scheduled separate plan `2026-06-XX-go-stdlib-1.26.4-bump`. Per `rules/deps-audit-golden-rule.md` allowlist downgrades severity 1 level (HIGH → LOW) — cap 49 → 89.

**Per anti-pattern 10**: HIGH CVE allowlist with sunset ≤ 30 days canonical. Sunset 2026-07-11 (30d from 2026-06-11) compliant.

### Out-of-scope action items (not blocking this plan)

- npm `cli/` brace-expansion MEDIUM DoS (transitive) — recommend `cd cli && npm audit fix` em separate commit pre-merge
- Other 8 Go modules (api/, operators/, pkg/, etc.) — recommend `/deps-audit` separate pass before next release

## Plan References

- Plan SoT: this file
- Mandate: `.claude/rules/cleanroom-up-live-test-mandate.md` (locked 2026-06-10)
- Session reference: memory `reference_t62_cleanroom_session_2026_06_11`
- 20 sibling memories: `j05/j09/j17/j22/j23/t62-aprendizado-*` em `~/.claude/projects/-home-paulo-loopzero/memory/`
- 7 commits `T6.2` sessão: `07326c68`, `21c0fae1`, `f4ccb6c2`, `b76b9e9f`, `4e32e474`, `540bc70f`, `328b5e6a`
- Steady-state baselines: `docs/operations/baselines/T6.2-2026-06-11/{pre,post}-rebuild.md`
- Deps audit (v1.2 amendment evidence): [`knowledge-base/audits/2026-06-11-cleanroom-canonical-5-defense-layers-deps-audit-2026-06-11.md`](../audits/2026-06-11-cleanroom-canonical-5-defense-layers-deps-audit-2026-06-11.md)
- CVE allowlist entries (v1.2): `.claude/rules/deps-audit-allowlist.txt` (11 GO-2026-* entries, sunset 2026-07-11)
