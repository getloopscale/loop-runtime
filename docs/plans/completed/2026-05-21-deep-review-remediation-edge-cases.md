---
type: plan
title: "Edge Case Review — 2026-05-21-deep-review-remediation-plan"
status: deprecated
---

# Edge Case Review — 2026-05-21-deep-review-remediation-plan

**Data:** 2026-05-21
**Plano:** [`docs/plans/2026-05-21-deep-review-remediation-plan.md`](./2026-05-21-deep-review-remediation-plan.md)
**Tasks analisadas:** 52
**Edge cases encontrados:** 14 (MUST FIX: 5, SHOULD TEST: 6, DOCUMENT: 3)
**Veredito:** PLANO PRECISA DE AJUSTE — 5 fixes aplicados no plano v1.1

---

## MUST FIX (5) — incorporados como sub-tasks no plano v1.1

### EC-01: T1.3 não cobre `provision-api-tokens.sh:23` — 11ª instância hardcoded `loopzero-gitea-dev-2026`
- **Task afetada:** T1.3 → estendido para 11 arquivos
- **Cenário:** `infra/scripts/provision-api-tokens.sh:23` contém `GITEA_PASS="${GITEA_PASS:-loopzero-gitea-dev-2026}"` — mesma credencial hardcoded que T1.4 remove do Pulumi TS, mas no lado shell ficaria órfã. Render-time grep gate `loopzero-dev-password` não pega `loopzero-gitea-dev-2026`.
- **Fix aplicado (plan v1.1):** T1.3 lista 11 files; grep gate inclui pattern `loopzero-gitea-dev-`.

### EC-02: T1.2 cria janela de Secret incompleto + silent empty token
- **Task afetada:** T1.2 → adiciona TDD `test_provision_api_tokens_fails_on_empty_gitea_token`
- **Cenário:** Entre step 1 (generate-secrets sem GITEA_TOKEN) e step 2 (provision-api-tokens), Secret incompleto por ~30-60s. Pior: `provision-api-tokens.sh:51` `.get('sha1','')` retorna string vazia silenciosamente se Gitea retorna erro. `GITEA_TOKEN=""` grava no Secret sem falhar.
- **Fix aplicado:** TDD novo + fail-fast: `[[ -z "$GITEA_TOKEN" ]] && { echo "ERROR: Gitea token empty"; exit 1; }`.

### EC-03: T2.1/T6.1 exempt scope ignora `infra/helmfile/scripts/lib/*.sh`
- **Task afetada:** T2.1 + T6.1
- **Cenário:** 5 sourced libs em `infra/helmfile/scripts/lib/` (argocd-register, clustercredential-create, scoped-kubeconfig, hydra-mint, pulumi-output). Glob `lib/*.sh` no exemption só pega `infra/scripts/lib/`. Sweep adicionaria `set -euo pipefail` quebrando callers que usam `set +e` local.
- **Fix aplicado:** T6.1 exempt scope estendido para `*/lib/*.sh` + auto-detecção via `grep -l 'BASH_SOURCE\[0\]'`.

### EC-04: T7.5 Pulumi rename sem alias obrigatório cria migration window com falhas silenciosas
- **Task afetada:** T7.5
- **Cenário:** `pulumi-output.sh:77` suprime stderr com `2>/dev/null` — key inexistente retorna string vazia silenciosamente. KUBECONFIG="" propaga para kubectl. Sem alias, qualquer script entre rename TS e redeploy stack quebra obscuramente.
- **Fix aplicado:** Alias obrigatório no MESMO PR do rename (não sugestão): `export const runtime2ClusterName = runtimeCell2ClusterName`. Remoção apenas em PR seguinte.

### EC-05: T3.5 escolhe `loopzero-system` como Ory namespace SEM evidência factual
- **Task afetada:** T3.5
- **Cenário CRÍTICO:** `infra/helm/loopzero-cloud/values.yaml:47-50` comentário B49 diz literal: "Default values pointed to `loopzero-system` which fails DNS resolution." `loopzero-system` é o valor que JÁ QUEBROU em 2026-04-28. Plano sugere reverter para esse valor exato.
- **Fix aplicado:** Pre-condição DoD obrigatória: `kubectl get ns | grep -E 'loopzero-auth|ory'` em k3d standalone pós `task setup`. Confirmar namespace REAL antes de escolher canonical. Se for `loopzero-auth`, padronizar para `loopzero-auth`, NÃO `loopzero-system`.

---

## SHOULD TEST (6) — adicionados ao TDD das tasks afetadas

| # | Task | Teste a adicionar |
|---|---|---|
| EC-06 | T1.2 | `test_provision_api_tokens_fails_on_empty_gitea_token` (também é fix do EC-02) |
| EC-07 | T2.2 | `test_bootstrap_oauth_clients_dry_run_no_http_writes` — stub curl, assertar 0 POST/PUT/DELETE |
| EC-08 | T3.1 | `test_chart_rejects_empty_string_password` — `helm template --set ...=""` deve exit 1 |
| EC-09 | T5.1 | `TestDoctorOutputJSON_FieldsStable` — Go test asserindo campos JSON do binary fixos |
| EC-10 | T4.1 | `test_operator_split_soak_workflow_has_cost_gate` — grep YAML por `environment:` ou `DRILL_SPEND_ENABLED` |
| EC-11 | T6.5 | `test_environment_lock_gate_false_positive_indirect_call` — Go fixture com call chain depth=1 |

---

## DOCUMENT (3) — riscos aceitos registrados no plano

| # | Risco |
|---|---|
| EC-12 | T2.1 sweep vai expor silent failures dormentes em ~110 scripts. Budget flex de +8-16h não contabilizado nas 80h. Documentar; aceitar. |
| EC-13 | T4.6 stagger de cron cria adjacência decay-scan + watchdog. Risco baixo porque watchdog é idempotente + flock-protected. Sem mudança. |
| EC-14 | T1.5 placeholder `secure:` exige `PULUMI_CONFIG_PASSPHRASE` para qualquer dev clonar e rodar prod preview. UX confuso (erro "failed to decrypt" em vez de fail-fast). Documentar em [`docs/operations/credentials-required.md`](../../operations/credentials-required.md). |

---

## Padrões sistêmicos detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Sim | EC-01 (T1.4 cobre Pulumi TS mas não script shell consumer); EC-02 (T1.2 não detecta GITEA_TOKEN="") |
| Correct code in wrong place | Sim | EC-03 (exempt scope ignora `infra/helmfile/scripts/lib/`) |
| Stale name | Já era o trigger original do plano | cell2-soaks + 4 outros workflows |

---

## Veredito final

**PLANO PRECISA DE AJUSTE → AJUSTE APLICADO → PLANO v1.1 OK**

5 MUST FIX corrigidos sem alterar escopo ou cronograma. 6 SHOULD TEST adicionados ao TDD respectivo. 3 DOCUMENT registrados como riscos aceitos.
