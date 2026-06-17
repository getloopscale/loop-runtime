---
name: chaos-test-drill
description: Chaos resilience audit against drill prod-parity infrastructure — injects 5 scenarios (pod-kill, network-partition, IO-pressure, DO Managed PG failover sim, cert renewal under network fault) on drill cluster and validates SLO holds + recovery within budget. Cost ~$3 / ~60min. Use quarterly via cron OR when changing resilience-critical code.
---

# Chaos Test Drill — Prod-Parity Resilience Audit

Você é um **chaos engineer validando resilience semantics que local k3d não consegue reproduzir**.

## Regra Suprema — Inviolável

**Você NUNCA dispatches este skill sem:**
1. cleanroom-drill foundation (auto-invoked)
2. `--confirm-cost` flag (ou `CI=true`)
3. `LOOPZERO_DRILL_SKILL_SPONSOR=<initials>` env (ou `CI=true`)
4. Lock acquisition (flock previne concurrent dispatch)
5. MANDATORY teardown ao final
6. Chaos cleanup (kubectl delete podchaos/networkchaos/iochaos --all)

Proibido:
- `--skip-teardown` (não existe)
- Expandir blast radius sem validar smaller radius primeiro
- Deixar chaos resources órfãos pós-scenario
- Mocking falhas (real chaos = kernel-level via chaos-daemon)

## Identidade

Você **é**:
- Um chaos engineer com rigor FAANG
- Um operator validando prod-parity resilience invariants
- Um auditor de recovery time vs SLO budget

Você **não é**:
- Um stress test (use `/drill-test` L-series)
- Um user journey auditor (use `/dogfood-drill`)
- Um chaos engineer ad-hoc (use `/chaos-test` local primeiro para iterate)

## Quando usar

- **Quarterly cron** (1st Sunday Jan/Apr/Jul/Oct per ADR D7): catch resilience drift
- **Pós-mudanças em circuit breaker, retry policy, leader election, cert-manager config**
- **Pre-release validation** que `/chaos-test` local não cobre prod-parity (DO Managed PG failover, real cert renewal, real LB rotation)

## Quando NÃO usar

- Daily resilience smoke (use `/chaos-test` local — free)
- Bug investigation (iterate em `/chaos-test` local primeiro)
- Stress testing (use `/drill-test --level L4`)
- Pre-merge gate (cost prohibitive)

## 5 Chaos Scenarios

### Scenario 1: pod-kill-api (re-uses local manifest)

- **Hypothesis**: kill loopzero-api leader → surviving pod elects new leader within 90s, neither restarts
- **Manifest**: `infra/tests/chaos/manifests/pod-kill-api-leader.yaml`
- **SLO budget**: 90s recovery
- **What it validates**: leader election + standby HTTP server health endpoint (Bug #18 history)

### Scenario 2: build-network (re-uses local)

- **Hypothesis**: partition build→mgmt network → builds in-flight transition to FAILED, no zombie workflows
- **Manifest**: `build-cluster-network-partition.yaml`
- **SLO budget**: 60s detection, 30min cleanup
- **What it validates**: Argo Workflow cross-cluster timeout handling

### Scenario 3: buildkit-io (re-uses local)

- **Hypothesis**: BuildKit IO slowdown → build slows but no corruption, recovers after IO release
- **Manifest**: `buildkit-io-pressure.yaml`
- **SLO budget**: Build completes within 3x baseline duration
- **What it validates**: BuildKit IO resilience under disk pressure

### Scenario 4: pg-managed-failover (NEW — drill-only)

- **Hypothesis**: NetworkChaos partition api→DO Managed PG simulates failover → circuit breaker engages, mutations return 503, reads continue from pool cache, recovery <60s post-heal
- **Manifest**: `drill-pg-managed-failover.yaml` (NEW)
- **SLO budget**: 60s recovery; 5xx rate <5% sustained
- **What it validates**: PG circuit breaker LIVE against DO Managed PG failover semantics (~30-60s switchover) — local bitnami PG doesn't reproduce this timing

### Scenario 5: cert-renewal-fault (NEW — drill-only)

- **Hypothesis**: NetworkChaos partition cert-manager→CF API+ACME → cert-manager retries DNS-01 challenge, certificate issuance completes within 10min post-heal, no rate-limit errors, CF zone cleaned up
- **Manifest**: `drill-cert-renewal-under-network-fault.yaml` (NEW)
- **SLO budget**: 600s post-heal recovery
- **What it validates**: Let's Encrypt prod retry resilience under CF DNS-01 transient network fault — local self-signed CA doesn't reproduce this

## Pré-requisitos absolutos

### P0 — Credenciais

```bash
DIGITALOCEAN_TOKEN=...
CLOUDFLARE_API_TOKEN=...
PULUMI_CONFIG_PASSPHRASE=...
SPACES_ACCESS_KEY_ID=...
SPACES_SECRET_ACCESS_KEY=...
```

### P1 — Sponsor

```bash
export LOOPZERO_DRILL_SKILL_SPONSOR=<initials>
# OR
export CI=true
```

### P2 — Local tooling

- Same as /cleanroom-drill + /dogfood-drill
- Plus: `helm` v3.14+ (install chaos-mesh on runtime cluster)

### P3 — Repo state

- chaos-mesh Helm repo available (`charts.chaos-mesh.org`)
- 5 chaos manifests present
- `infra/scripts/lib/drill-skill-common.sh`

## Filosofia

**Real chaos > mocked failure.** chaos-mesh injects kernel-level faults via chaos-daemon. Mocks via toxiproxy/iptables app-level são instrumentation, not chaos.

**Hypothesis-driven.** Cada scenario tem hypothesis explícito + SLO query + recovery budget. Sem isso = "vamos ver o que quebra" = entertainment, not engineering.

**Blast radius controlled.** `mode: one` (kill single pod) PRIMEIRO. `mode: all` SÓ após smaller radius validated.

**Audit-grade cleanup.** Chaos resources NUNCA órfãos. cleanup_chaos function deletes podchaos/networkchaos/iochaos --all pós-scenario.

## Modos de execução

### Default (`--scenario all --confirm-cost`)

5 scenarios sequenciais (~60min, ~$3):
1. cleanroom-drill foundation
2. Install chaos-mesh on runtime cluster
3. Start sustained traffic (1 RPS curl loop)
4. Inject scenarios sequentially with SLO monitoring + cleanup between
5. MANDATORY teardown

### `--scenario <id>`

Single scenario (faster iteration). One of: `pod-kill-api`, `build-network`, `buildkit-io`, `pg-managed-failover`, `cert-renewal-fault`.

### `--dry-run`

Lista scenarios + SLO criteria sem provisioning.

### `--skip-cleanroom`

CI multi-step só. Não usar manual.

## Ciclo canônico

```bash
# Manual operator
LOOPZERO_DRILL_SKILL_SPONSOR=phn bash infra/scripts/drill/chaos-orchestrator.sh --confirm-cost --scenario pg-managed-failover

# CI quarterly cron (drill-skills-cron.yml)
CI=true bash infra/scripts/drill/chaos-orchestrator.sh --scenario all
```

## Anti-Patterns (PROIBIDOS)

1. **Não rodar `--scenario all` sem validar individual scenarios primeiro.** Smaller radius first principle.
2. **Não suprimir SLO violation findings.** Recovery > budget = real resilience gap.
3. **Não pular chaos-mesh install validation.** chaos-mesh CrashLoopBackOff is silent — daemon must be Running before inject.
4. **Não confundir transient flake com SLO breach.** 5xx rate >5% sustained = breach; isolated 1-2 5xx = noise.
5. **Não rodar paralelo com prod canary.** Drill stack + customer traffic compete por operator attention. Serialize.

## Output da skill

Audit trail `/tmp/drill-audit-chaos-test-drill-<timestamp>/`:
- `run.log` — full transcript
- `audit.md` — per-scenario findings (S0/S1) + SLO measurements + recovery times
- `traffic.log` — 1 RPS sample stream during chaos windows

Exit codes:
- `0` PASS — all scenarios held SLO + recovered within budget, teardown clean
- `1` usage error
- `2` cost/sponsor/lock not confirmed
- `3` cleanroom-drill foundation fail
- `4` chaos-mesh install fail
- `5` chaos inject fail (manifest missing OR apply error)
- `6` SLO violation (recovery exceeded budget OR 5xx rate >5%)
- `7` teardown fail

## Erros comuns + recovery

| Sintoma | Causa | Recovery |
|---------|-------|----------|
| Exit 3 cleanroom fail | Bootstrap broken | Run `/cleanroom-drill` standalone para diagnose |
| Exit 4 chaos-mesh install fail | chaos-mesh informer cache timeout | Wait 60s + retry; chaos-daemon restarts 2-3x é normal |
| Exit 5 manifest missing | Plan T3.1 files not committed | Verify `infra/tests/chaos/manifests/drill-*.yaml` exists |
| Exit 6 SLO violation pg-managed-failover | Circuit breaker not engaging | Bug investigation — check `loopzero_circuit_breaker_state{name="pg"}` metric |
| Exit 6 SLO violation cert-renewal-fault | cert-manager retry policy too aggressive | Tune cert-manager retry backoff OR Let's Encrypt rate limit hit |
| Exit 7 teardown fail | DO VPC unbind race | Manual `task drill:down` |
| Chaos pods órfãos | chaos-daemon kill mid-cleanup | restart chaos-daemon DaemonSet via `kubectl rollout restart` |

## References

- Plan: `.claude/knowledge-base/plans/2026-05-18-drill-skills-trio-plan.md`
- Governance: `MEET-ASYNC-AMENDMENT-2026-05-18-drill-skills-trio.md`
- Orchestrator: `infra/scripts/drill/chaos-orchestrator.sh`
- 2 NEW chaos manifests:
  - `infra/tests/chaos/manifests/drill-pg-managed-failover.yaml`
  - `infra/tests/chaos/manifests/drill-cert-renewal-under-network-fault.yaml`
- Foundation gate: `/cleanroom-drill`
- Local chaos-test pattern reference: `.claude/skills/chaos-test/SKILL.md`
