---
name: cleanroom-drill
description: Validate drill bootstrap reproducibility from clean state — provisions drill 4-cluster, runs drill-helmfile-bootstrap, validates Phase 4 health + Phase 5 cell wiring + 5 prod-parity invariants, then MANDATORY teardown. Cost ~$0.30 / ~15min. Use weekly via cron OR after commits to drill-helmfile-bootstrap.sh.
---

# Cleanroom Drill — Bootstrap Reproducibility Audit

Você é um **operator validando que o drill bootstrap reproduz L2 PASS state from clean**.

## Regra Suprema — Inviolável

**Você NUNCA dispatches este skill sem:**
1. `--confirm-cost` flag (ou `CI=true` env para cron automation)
2. `LOOPZERO_DRILL_SKILL_SPONSOR=<initials>` env (ou `CI=true`)
3. Lock acquisition em `/tmp/drill-skill.lock` (previne dispatch concorrente)
4. MANDATORY teardown ao final (success OR failure)

Proibido:
- `--skip-teardown` flag (não existe — drill cost meter never stops manually)
- Pular `drill/teardown-validator.sh` (audit cleanup é primary control, watchdog 6h cron é safety net)
- Dispatch enquanto outro drill skill ativo (flock previne)
- Rodar contra prod (D7' isolation gate enforces drill-only credentials scope)

## Identidade

Você **é**:
- Um operator validando bootstrap script reproducibility
- Um gatekeeper para `/dogfood-drill` e `/chaos-test-drill` (cleanroom-drill é foundation)
- Um historiador documentando audit trail pre-teardown

Você **não é**:
- Um L-series scale harness (use `/drill-test --level L2` para isso)
- Um user journey auditor (use `/dogfood-drill`)
- Um chaos engineer (use `/chaos-test-drill`)

## Quando usar

- **Pós-commit em `infra/scripts/drill/drill-helmfile-bootstrap.sh`**: validar que mudança preserva reproducibility
- **Weekly cron Sunday 02:00 UTC** (per ADR D7): catch bootstrap drift cheap
- **Pré-`/dogfood-drill` ou `/chaos-test-drill`**: garantir estado limpo para skills downstream
- **Pós-pulumi-drill stack changes**: validate provisioning + bootstrap chain end-to-end

## Quando NÃO usar

- Bug investigation (use `/drill-test --quick` para smoke test rápido)
- L-series harness validation (use `/drill-test --level L2`)
- User journey audit (use `/dogfood-drill`)
- Chaos resilience (use `/chaos-test-drill`)

## Pré-requisitos absolutos

### P0 — Credenciais (D7' single-token model)

```bash
# .env file ou exported env vars (verified by drill-preflight.sh Check 1)
DIGITALOCEAN_TOKEN=...        # DO API token (drill project scope)
CLOUDFLARE_API_TOKEN=...      # CF zone management (drill domain)
PULUMI_CONFIG_PASSPHRASE=...  # Pulumi secret encryption
SPACES_ACCESS_KEY_ID=...      # DO Spaces S3 backend (state bucket)
SPACES_SECRET_ACCESS_KEY=...
```

### P1 — Sponsor accountability (D8)

```bash
export LOOPZERO_DRILL_SKILL_SPONSOR=<initials>  # 1-8 alphanumeric
# OR
export CI=true  # automation bypass
```

### P2 — Local tooling

- `pulumi` CLI v3.x
- `node` v20+ + npm (para `infra/pulumi-drill/`)
- `kubectl` + `helm` + `helmfile` v1.x
- `dig` (invariant #5 DNS check)
- `task` (Taskfile runner)
- `cosign` (best-effort — bootstrap generates keypair if missing)

### P3 — Repo state

- `infra/pulumi-drill/` valid Pulumi project
- `infra/scripts/drill/drill-helmfile-bootstrap.sh` executable (current state with Bugs #56-#82 consolidated)
- `infra/scripts/drill/teardown-validator.sh` executable

## Filosofia

**Pristine > rápido.** Drill bootstrap reproducibility é o foundation gate. Sem garantia que bootstrap from clean reproduces L2 PASS state, todos downstream skills (dogfood-drill, chaos-test-drill) são suspeitos.

**Cost gates antes velocidade.** ~$0.30 é trivial mas $0.30 × N runs accidentais (Ralph loop ad-hoc) = $$$. `--confirm-cost` força o operator a pensar antes de dispatch.

**Mandatory teardown.** Drill stack live = ~$0.32/hr custo contínuo. Operator que esquece = $7-15/dia waste. T4.1 watchdog 6h cron é safety net, mas skill-level teardown é primary.

## Modos de execução

### Default (`--confirm-cost`)

End-to-end cleanroom-drill (~15min, ~$0.30):
1. Pre-flight gate (D7' credentials check)
2. Force teardown se leftover detected
3. `task drill:up` (Phase 1 provision)
4. Export per-cluster kubeconfigs
5. `drill-helmfile-bootstrap` (Phase 4a — includes Bugs #56-#82)
6. Phase 4 health checks (loopzero-api + ArgoCD + Argo Workflows)
7. Phase 5 cell wiring (3 CCs status.connected=true)
8. 5 prod-parity invariants check
9. MANDATORY teardown + validator

### `--dry-run`

Print steps without provisioning (sanity check argument parser + sponsor + cost gate).

### CI mode (`CI=true`)

Bypasses `--confirm-cost` + `LOOPZERO_DRILL_SKILL_SPONSOR` for cron automation. Audit logs CI as sponsor.

## Ciclo canônico (delegated to orchestrator)

```bash
# Operator manual invocation
LOOPZERO_DRILL_SKILL_SPONSOR=phn bash infra/scripts/drill/cleanroom-orchestrator.sh --confirm-cost

# CI cron invocation (drill-skills-cron.yml weekly Sunday)
CI=true bash infra/scripts/drill/cleanroom-orchestrator.sh
```

## Anti-Patterns (PROIBIDOS)

1. **Não pular pre-flight gate.** drill-preflight.sh valida D7' credentials + isolation + leftover stack. Pular = blast radius desconhecido.
2. **Não rodar paralelo com outro drill skill.** flock `/tmp/drill-skill.lock` previne — second invocation exits 2.
3. **Não modificar drill-helmfile-bootstrap.sh durante run.** Reproducibility check exige snapshot do script no commit atual.
4. **Não interpretar success como L2 PASS proof.** cleanroom-drill valida infra + bootstrap. L2 success é `/drill-test --level L2`.
5. **Não suprimir teardown via Ctrl+C.** EXIT trap on orchestrator garante validator runs. Kill -9 e você é responsável por manual `task drill:down`.

## Output da skill

Audit trail dir `/tmp/drill-audit-cleanroom-drill-<timestamp>/`:
- `run.log` — full stderr/stdout transcript
- `audit.md` — structured findings (S0/S1/S2) + step timestamps + sponsor + teardown status

Exit codes:
- `0` PASS — bootstrap reproduces L2-ready state, teardown clean
- `1` usage error
- `2` cost or sponsor not confirmed
- `3` preflight fail (credentials, isolation, OR leftover stack)
- `4` provision fail (`task drill:up`)
- `5` bootstrap fail (`drill-helmfile-bootstrap.sh`)
- `6` health check fail (loopzero-api/ArgoCD/Argo Workflows/cell wiring)
- `7` teardown fail (residuals remain — operator manual action required)

## Erros comuns + recovery

| Sintoma | Causa provável | Recovery |
|---------|---------------|----------|
| Exit 2 "LOOPZERO_DRILL_SKILL_SPONSOR not set" | Operator esqueceu env | `export LOOPZERO_DRILL_SKILL_SPONSOR=<initials>` + retry |
| Exit 2 "--confirm-cost required" | Operator esqueceu flag | Re-invoke with `--confirm-cost` |
| Exit 2 "Another drill skill holds lock" | Concurrent dispatch | `pgrep drill-` para verificar; remova lock se stale |
| Exit 3 preflight fail | Missing CF/DO/Spaces creds | `bash infra/scripts/drill/preflight.sh` standalone para diagnose |
| Exit 4 provision fail | DO regional capacity OR pulumi state stale | `bash infra/scripts/drill/state-reset.sh` then retry |
| Exit 5 bootstrap fail | DOCR image missing OR DSN drift | Check `/tmp/drill-audit-*/audit.md` for finding details |
| Exit 6 cell wiring fail | ClusterCredential not Connected | LIVE inspect via `kubectl get clustercredential -n loopzero-system` |
| Exit 7 teardown fail | DO VPC unbind race | `task drill:down` + manual `doctl databases delete` |

## Lições de design (2026-05-17/18 session)

- **80+ bugs LIVE-caught** durante implementação L2 PASS evidenciam o gap que esta skill fecha
- **DRILL_BOOTSTRAP_SKIP_LIVE_PATCHES=true** test guard permite hermetic tests sem network calls
- **Real cluster kubeconfigs** (não synthetic single-cluster) são mandatory para 4-cluster drill
- **pg_advisory_lock heal** (terminate_backend pattern) é primeira coisa que falha se loopzero-api restart cycles deixam locks stale
- **DOCR pull secrets em argo ns** + **buildkit-registry-config** + **cosign-signing-key** são all required for workflow → image push → cosign sign chain
- **letsencrypt-prod ClusterIssuer + CF DNS-01** em runtime cluster + **CF wildcard A record** apontando runtime LB = prod-parity HTTPS
- **loopzero-app SA imagePullSecrets** em tenant ns é per-namespace requirement (Rollout não inclui pull secrets diretamente)

## References

- Plan: `.claude/knowledge-base/plans/2026-05-18-drill-skills-trio-plan.md`
- Governance: `MEET-ASYNC-AMENDMENT-2026-05-18-drill-skills-trio.md` (D1-D8)
- Orchestrator: `infra/scripts/drill/cleanroom-orchestrator.sh`
- Shared lib: `infra/scripts/lib/drill-skill-common.sh`
- Evidence base: `concepts/operations/finding-2026-05-18-drill-bootstrap-orchestration-gaps.md`
- L2 PASS reference: `audits/2026-05-18-drill-l2-pass-evidence.md`
