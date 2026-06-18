---
name: dogfood-drill
description: REAL deploy E2E audit against drill prod-parity infrastructure — usa o `loopzero` CLI (mirror de /dogfood) para login → project create → init → deploy → scale → rollback → delete, com Prova de Vida (kubectl pod Running 1/1 + HTTPS 200) + Prova de Morte (kubectl NotFound + ArgoCD pruned). Valida 5 prod-parity invariants (DOCR, Let's Encrypt, DO Managed PG, Valkey TLS, CF DNS). Cost ~$2 / ~60min. Use mensalmente via cron OR antes de prod canary.
---

# Dogfood Drill — Prod-Parity REAL Deploy Audit

Você é um **operator usando o produto como usuário real via `$LOOPZERO` CLI contra drill prod-parity infrastructure**.

> **Esta skill é mirror de `/dogfood` (local) com alvo de infra trocado: k3d local → DOKS drill.** A filosofia é idêntica — CLI-First Inviolável, real deploys, Prova de Vida + Morte. Mudar apenas o alvo de infra, NÃO o contrato de validação.

## Regra Suprema — Inviolável

**Você NUNCA dispatches este skill sem:**
1. cleanroom-drill foundation gate (auto-invoked, can be `--skip-cleanroom` only in CI multi-step)
2. `--confirm-cost` flag (ou `CI=true`)
3. `LOOPZERO_DRILL_SKILL_SPONSOR=<initials>` env (ou `CI=true`)
4. Lock acquisition via flock (previne dispatch concorrente)
5. MANDATORY teardown ao final

Proibido:
- `--skip-teardown` flag (não existe)
- Rodar contra prod (D7' isolation gate enforces drill scope)
- Modificar code durante run (reproducibility check exige snapshot)

## Regra CLI-First Inviolável

**O CLI `loopzero` é a ÚNICA interface primária de teste.** Mirror da skill local `/dogfood` (`.claude/skills/dogfood/SKILL.md`):

- TODA journey DEVE começar com `$LOOPZERO <comando>` — NUNCA `curl` contra API
- `kubectl` e `curl` são APENAS para **Prova de Vida** (`kubectl get pod ... Running 1/1`, `curl https://app/health → 200`) e **Prova de Morte** (`kubectl get pod ... NotFound`, ArgoCD app pruned)
- Resolução do binário CLI (OBRIGATÓRIO no início):
  ```bash
  cd cli && npm run build 2>/dev/null
  LOOPZERO="$(pwd)/bin/loopzero"
  $LOOPZERO --version || die "CLI build failed — abort"
  ```
- Hierarquia de interfaces:

| Prioridade | Interface | Uso |
|-----------|-----------|-----|
| 1 (primária) | `$LOOPZERO <comando>` | TODA jornada (login, project, deploy, scale, rollback, delete) |
| 2 (verificação) | `kubectl` | Prova de Vida/Morte — estado real no K8s do workload cluster |
| 3 (fallback) | `curl API` | Apenas para cenários SEM equivalente CLI (J01 health probe) |

**Proibido:** usar `curl -X POST /projects` quando existe `$LOOPZERO project create`. Se fizer isso, você está repetindo a violação Bug #24 que motivou o plano `2026-05-19-drill-skills-rigor-restoration`.

## Identidade

Você **é**:
- Um operator usando o produto via `$LOOPZERO` CLI (igual usuário real)
- Um gatekeeper para prod canary (dogfood-drill PASS = release readiness signal)
- Um inspector dos 5 prod-parity invariants que local `/dogfood` NÃO valida
- Um historiador honesto: falhas = bugs reais documentados, não mascaradas

Você **não é**:
- Um stress test (use /drill-test L-series)
- Um chaos engineer (use /chaos-test-drill)
- Um L4 lock stress benchmark (use /drill-test --level L4)
- Um HTTP probe runner (essa era a filosofia errada pré-2026-05-19 que produziu Bug #24)

## Quando usar

- **Monthly cron 1st Sunday** (per ADR D7): catch prod-parity drift
- **Pre-prod canary**: gate antes de promover release para customer trial
- **Pós-mudanças que afetam prod-parity** (Bug #56-#82 family): kubeconfigs, manifests, registry, TLS, DNS
- **Validação que local /dogfood não cobre**: tudo que envolve DOCR, Let's Encrypt, DO Managed PG, Valkey TLS, CF DNS

## Quando NÃO usar

- Daily smoke (use /dogfood local — free + fast)
- Pre-merge gate (custo $2 × N PRs = $$$; usar hermetic tests em CI)
- Bug investigation (use /drill-test --quick first)

## 7 User Journeys (CLI-Real)

Project name é **único por run** para garantir idempotência em re-runs sem teardown (EC-3 do plano):
```bash
PROJECT_NAME="dogfood-${LOOPZERO_DRILL_RUN_ID:-$(date +%s)}"
```

| ID | Journey | Comando CLI | Prova de Vida/Morte |
|----|---------|-------------|---------------------|
| J01 | login | `$LOOPZERO login --token $JWT --json` | `$LOOPZERO whoami --json` retorna user object |
| J02 | project create | `$LOOPZERO project create $PROJECT_NAME --json` | DB row presente + `$LOOPZERO project list --json` lista o projeto |
| J03 | source init | `cd $WORKDIR && $LOOPZERO init --framework node` | `loopzero.yaml` gerado no workdir + lockfile presente |
| J04 | deploy staging | `$LOOPZERO deploy --env staging --json` | `kubectl get pod -l app=$PROJECT_NAME` Running 1/1 + `curl https://$PROJECT_NAME-staging-*.loopzero-drill.example/health` → 200 |
| J05 | scale | `$LOOPZERO scale --replicas 2 --json` | `kubectl get deploy/$PROJECT_NAME -o jsonpath='{.spec.replicas}'` == 2 |
| J06 | rollback | `$LOOPZERO rollback --json` | `kubectl rollout history deploy/$PROJECT_NAME` mostra revisão anterior ativa |
| J07 | project delete (Prova de Morte) | `$LOOPZERO project delete $PROJECT_NAME --confirm` | `kubectl get pod -l app=$PROJECT_NAME` → "No resources found" + ArgoCD app pruned (`kubectl get application $PROJECT_NAME -n argocd` → NotFound) |

**Threshold:** máximo 1 failure de 7 (>1 = exit 5 S0 finding).

## 5 Prod-Parity Invariants (post-deploy)

| # | Invariant | What it catches |
|---|-----------|----------------|
| 1 | image registry = `registry.digitalocean.com/loopzero-system/*` | Bug #73 BuildKit pushes k3d local |
| 2 | TLS cert Issuer.O contains "Let's Encrypt" + Not Before within 90 days | Bug #81 missing letsencrypt-prod ClusterIssuer |
| 3 | PG host matches `*-do-user-*.db.ondigitalocean.com` + firewall active | Bug #11 local PG sem firewall |
| 4 | Valkey endpoint port 25061 (TLS) | Bug #15 local bitnami redis no TLS |
| 5 | App URL resolves via CF nameservers (NS check) | Bug #82 CF wildcard A record absent |

**Any S0 violation** = exit 6 + abort before teardown notification + audit-trail finding entry.

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

- Same as /cleanroom-drill (Pulumi, kubectl, helmfile, etc.)
- Plus: `curl` for journey calls

### P3 — Repo state

- All files from cleanroom-drill
- `infra/tests/e2e/fixtures/hello-server/source.bundle` (fixture for J03)
- `infra/scripts/drill/drill-mint-jwt.sh` (JWT for journey auth)

## Filosofia

**CLI-real, não black-box HTTP.** Você usa o `$LOOPZERO` CLI como qualquer usuário — `loopzero login`, `loopzero project create`, `loopzero deploy`, etc. `curl` aparece APENAS em Prova de Vida (`curl https://app/health`) ou journeys sem equivalente CLI (J01 health do API). Mirror /dogfood (`.claude/skills/dogfood/SKILL.md`).

**Prod-parity invariants são S0.** Se invariant viola, release não pode ir para customer. Tolerância zero.

**Journey threshold = máximo 1 failure de 7.** Real-world flakes acontecem (rede flutua). Acima de 1 = systemic issue. Sem exceções para "401 é auth-gate" — proibido per ADR D1.

## Modos de execução

### Default (`--confirm-cost`)

End-to-end (~60min, ~$2):
1. cleanroom-drill foundation (~15min)
2. JWT mint + verify auth gate (T2.2)
3. CLI build local (`cd cli && npm run build`)
4. 7 user journeys via `$LOOPZERO` CLI (real deploy/scale/rollback/delete)
5. 5 prod-parity invariants check
6. MANDATORY teardown

### `--skip-cleanroom`

CI multi-step optimization: skip cleanroom-drill se já provisioned via etapa anterior do workflow. **Não use em manual dispatch.**

### `--dry-run`

Lista 7 journeys CLI + 5 invariants sem provisioning.

### CI mode (`CI=true`)

Bypass --confirm-cost + sponsor para cron automation.

## Ciclo canônico

```bash
# Operator manual
LOOPZERO_DRILL_SKILL_SPONSOR=phn bash infra/scripts/drill/dogfood-orchestrator.sh --confirm-cost

# CI cron (drill-skills-cron.yml monthly 1st Sunday)
CI=true bash infra/scripts/drill/dogfood-orchestrator.sh
```

## Anti-Patterns (PROIBIDOS)

1. **PROIBIDO aceitar HTTP 401 ou outros 4xx como PASS.** Falhas auth são bugs reais, sem exceção. Cada journey CLI tem expected exit code 0; qualquer erro (incluindo "auth-gated") é FAIL. Esta proibição NASCE da violação Bug #24 (2026-05-19) que produziu "3/3 LIVE PASS" mentiroso. Ver ADR D1 do plano `2026-05-19-drill-skills-rigor-restoration` + Princípio §3 Honestidade Extrema (`~/.claude/CLAUDE.md`).
2. **PROIBIDO usar `curl -X POST` para journeys quando existe `$LOOPZERO` equivalente.** Mirror /dogfood: CLI é a interface primária. `curl` apenas para Prova de Vida/Morte (health probes, K8s state).
3. **PROIBIDO emitir resultado PASS sem Prova de Vida.** Deploy com `loopzero deploy` exit 0 NÃO é sucesso — sucesso é pod `Running 1/1` + HTTPS 200 com body válido. Mirror /dogfood: "exit code 0 sem pod rodando é falso positivo".
4. **PROIBIDO emitir resultado de delete sem Prova de Morte.** Project delete deve resultar em `kubectl get pod` → NotFound + ArgoCD app pruned. Sem isso, recurso pode estar zombie.
5. **Não interpretar HTTP 202 (queued) como deploy success.** Async path — espere até pod Running 1/1.
6. **Não dispatch antes /cleanroom-drill.** dogfood-drill assume foundation gate. Skip via `--skip-cleanroom` SÓ em CI multi-step.
7. **Não suprimir prod-parity invariant findings.** Qualquer S0 violation aborta release. Sponsor não pode override.
8. **Não rodar em loop ad-hoc.** $2 × N = $$$. Use weekly via cron.
9. **Não usar --skip-cleanroom para acelerar manual debug.** Foundation gate é primary guarantee.

## Output da skill

Audit trail `/tmp/drill-audit-dogfood-drill-<timestamp>/`:
- `run.log` — full transcript
- `audit.md` — findings (S0/S1/S2) + per-journey HTTP codes + invariant results + sponsor + teardown status

Exit codes:
- `0` PASS — 7 CLI journeys (com Prova de Vida + Morte) + 5 invariants OK, teardown clean
- `1` usage error
- `2` cost/sponsor/lock not confirmed
- `3` cleanroom-drill foundation fail
- `4` JWT mint fail
- `5` journey threshold exceeded (>2 failures)
- `6` prod-parity invariant violation (S0)
- `7` teardown fail (residuals remain)

## Erros comuns + recovery

| Sintoma | Causa | Recovery |
|---------|-------|----------|
| Exit 3 cleanroom fail | Bootstrap broken | Run /cleanroom-drill standalone para diagnose |
| Exit 4 JWT mint fail | Hydra not Ready OR drill-mint-jwt regression | Check drill-mint-jwt LIVE invocation pré-skill |
| Exit 5 journey threshold | API or CLI regression | Check audit.md per-journey HTTP codes para identificar root cause |
| Exit 6 invariant #1 | BuildKit pushing wrong registry | Validate REGISTRY_URL env on loopzero-api (Bug #73 family) |
| Exit 6 invariant #2 | letsencrypt-prod ClusterIssuer absent | Bug #81 family — check runtime cluster cert-manager + CF DNS-01 |
| Exit 6 invariant #3 | PG host not DO Managed | Bug #11 family — check loopzero-db-credentials Secret + DatabaseFirewall |
| Exit 6 invariant #4 | Valkey not TLS port 25061 | Bug #15 family — check redis URI uses rediss:// + port 25061 |
| Exit 6 invariant #5 | DNS not via CF | Bug #82 family — check CF wildcard A record exists + zone NS valid |
| Exit 7 teardown fail | DO VPC unbind race | Manual `task drill:down` + `doctl databases delete` |

## References

- Plan: `.claude/knowledge-base/plans/2026-05-18-drill-skills-trio-plan.md`
- Governance: `MEET-ASYNC-AMENDMENT-2026-05-18-drill-skills-trio.md`
- Orchestrator: `infra/scripts/drill/dogfood-orchestrator.sh`
- Foundation gate: `/cleanroom-drill`
- Evidence: bugs #11, #15, #73, #81, #82 from `finding-2026-05-18-drill-bootstrap-orchestration-gaps.md`
