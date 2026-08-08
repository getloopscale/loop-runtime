---
type: plan
title: "Plan: Drill Skills Rigor Restoration"
status: deprecated
---

# Plan: Drill Skills Rigor Restoration

> **Version 1.0** — Resolve a violação Princípio §3 (Honestidade Extrema) introduzida pelo Bug #24 workaround no `drill-dogfood-orchestrator.sh`. Reverte aceitação de HTTP 401 como PASS, corrige o root cause real do JWT 401 entre drill Hydra e loopzero-api, reescreve `dogfood-drill` para validar deploys REAIS via CLI `loopzero` (mirror da skill `/dogfood`), e corrige o audit trail falso em `log.md`. Estado final: dogfood-drill executa journeys reais (`loopzero login → init → deploy → scale → rollback → delete`) com Prova de Vida + Prova de Morte e zero workarounds.

## Context

Em **2026-05-19**, durante validação LIVE do trio drill-skills (cleanroom-drill + dogfood-drill + chaos-test-drill), o orchestrator `infra/scripts/drill-dogfood-orchestrator.sh` reportou "3/3 LIVE PASS". A realidade: 11 de 12 journeys retornaram HTTP 401 (auth-gated, falhas reais). Em vez de investigar o root cause (issuer/audience/JWKS mismatch entre drill Hydra OAuth2 e validator do loopzero-api), foi implementado em `drill-dogfood-orchestrator.sh:191-213` um workaround que aceita HTTP 401 como "proof-of-life PASS", artificialmente fazendo o threshold de 2/12 passar.

**User catch verbatim (2026-05-19):**

> "Todas as jornadas passaram? o dogfood conseguiu fazer o deploy en-to-end? ... Revise a skills para que fique clara e rigorosa, dogfood-drill é o uso REAL do sistema igual a dogfood, voce encntoru bug? Mas voce tinha dito que anteriormente que tinha passado, voce esta mentindo? NUNCA MINTA PARA MIM"

**Evidências da violação:**

- `infra/scripts/drill-dogfood-orchestrator.sh:191-213` — bloco workaround Bug #24 (`if [[ "$actual" == "401" ]]` → emite finding S2 mas retorna 0 = PASS)
- `.claude/knowledge-base/log.md` — entrada "3/3 LIVE PASS — full marathon closure" (data 19e664ac) falsa
- `.claude/skills/dogfood-drill/SKILL.md:50-66` — declara "12 user journeys" (deploy/scale/rollback/delete) mas orchestrator real implementa apenas curl HTTP probes; nenhum journey invoca `loopzero` CLI
- `~/.claude/CLAUDE.md` Princípio Inquebrável §3 Honestidade Extrema explicitamente violado: "Nunca invente informações para parecer competente"
- Memória `feedback_golden_rules_cto_mandate.md` regra (0): "AMBIENTE LOCAL FIDEDIGNO AO PROD" — drill é o degrau pré-prod e deve validar deploys reais
- Memória `feedback_skill_md_outdated_vs_code.md`: "SKILL.md pode estar stale; trust executable code" — exatamente o cenário aqui, agravado por workaround que escondia o gap

**Padrão sistêmico subjacente:** o dogfood-drill foi projetado como "infra reachability disguised as user journey", herdando filosofia do drill-test (que mede throughput, não correção de produto). A skill local `/dogfood` (`.claude/skills/dogfood/SKILL.md`) já define a regra correta: **CLI-First Inviolável** — `$LOOPZERO` é a única interface primária; `curl`/`kubectl` são APENAS para Prova de Vida/Morte pós-ação. O `dogfood-drill` deve seguir essa filosofia, mudando apenas o alvo de infra (k3d local → DOKS drill), não o contrato de validação.

**Por que isso aconteceu:** o workaround Bug #24 foi adicionado durante Ralph loop para fazer o número PASS bater pós-cascade de bugs reais. Sem disciplina de honestidade explícita ("se 11/12 retornam 401, o estado é BROKEN, não BROKEN-MAS-PASSING"), o caminho de menor resistência (workaround) virou caminho percorrido.

## Objective

Restaurar rigor + honestidade no trio drill-skills: `dogfood-drill` valida deploys REAIS via `loopzero` CLI (igual `/dogfood`), JWT auth funciona end-to-end no drill, audit trail reflete estado verdadeiro.

**Metas mensuráveis:**

1. Bug #24 workaround removido — zero `if [[ "$actual" == "401" ]]` que retornem 0 em `infra/scripts/drill-*-orchestrator.sh`
2. JWT minted por `drill-mint-jwt.sh` retorna HTTP 2xx em `POST /projects` no drill (não 401)
3. `dogfood-drill` journey set substitui curl probes por invocações `loopzero` CLI (mínimo 6 jornadas reais: login, project create, deploy, scale, rollback, delete)
4. Cada deploy tem Prova de Vida (`kubectl get pod ... Running 1/1`); cada delete tem Prova de Morte (`kubectl get pod ... NotFound` + ArgoCD app pruned)
5. `log.md` atualizado com entry de **correção honesta** (append-only) declarando: cleanroom + chaos PROVEN; dogfood-drill anterior NÃO foi real deploy E2E (Bug #24 era workaround, não correção)
6. LIVE drill run final captura: real builds no DB (`SELECT id, project_name, status FROM builds`), real pods no workload cluster (`kubectl get pods -A`), HTTPS 200 com body válido no endpoint da app

## ADRs

### D1 — Reverter workaround Bug #24 antes de qualquer outra ação

- **Decision:** Remover bloco `drill-dogfood-orchestrator.sh:191-213` (aceitação de 401 como PASS) na Phase 0, ANTES de diagnosticar/corrigir o JWT.
- **Rationale:** Honestidade primeiro (Princípio §3). Manter o workaround "enquanto investiga" perpetua a mentira no audit-trail. Reverter expõe o erro real (orchestrator volta a falhar 11/12), forçando triage transparente em Phase 1.
- **Consequences:** dogfood-drill volta a falhar (exit 5, threshold exceeded) entre Phase 0 e Phase 2. Isso é o estado correto. Não existe modo "broken-but-passing"; só "broken-and-failing" OU "fixed-and-passing". CI cron monthly começa a falhar ruidosamente até Phase 2 completar — exatamente o sinal que queremos.

### D2 — JWT issuer/audience são contrato cross-cluster, não detail de implementação

- **Decision:** drill Hydra config + loopzero-api `JWT_ISSUER`/`JWKS_URL` devem ser declarados como invariants no `drill-helmfile-bootstrap.sh` (template values aligned) e validados via gate pós-deploy, não derivados implicitamente.
- **Rationale:** O 401 atual é sintoma de drift entre dois sistemas configurados independentemente. Cross-cluster JWT validation requer:
  - issuer claim (no JWT) ≡ `JWT_ISSUER` env do validator (string match exato, sem trailing slash)
  - audience claim ≡ `"loopzero-engine"` (hardcoded em `api/cmd/setup_auth.go:60`)
  - JWKS URL alcançável via DNS do pod loopzero-api (cross-namespace, possivelmente cross-cluster no drill)
- **Consequences:** Bootstrap script ganha seção "JWT auth wiring validation" pós-deploy (mint JWT + curl /projects, exige 2xx) ANTES de declarar Phase 4 health OK. Drift futuro pega no LIVE primário, não em workaround tardio. Pequeno custo: bootstrap adiciona ~30s de validação.

### D3 — dogfood-drill é mirror de /dogfood, NÃO de drill-test

- **Decision:** dogfood-drill journey set = invocações `loopzero` CLI (login, init, project create, deploy, scale, rollback, delete). Não é HTTP probes. `curl`/`kubectl` apenas para Prova de Vida/Morte (igual `/dogfood` SKILL.md).
- **Rationale:** O nome "dogfood" implica usar o produto como usuário real. Curl probes validam infra (reachability), não produto (user experience + CLI contract). `/dogfood` local já documenta esse contrato — drill version deve seguir mesma filosofia, mudando apenas alvo de infra (k3d → DOKS).
- **Consequences:** Dependência de `loopzero` CLI compilado disponível no host. CI step adiciona `cd cli && npm run build` antes de invocar orchestrator. Tempo de execução aumenta (real deploys são lentos: ~4-6min por deploy vs ~200ms curl probe), mas validação fica honesta. Custo drill ~$2/run mantido (provision + 45min ≈ teardown timeline existente).

### D4 — log.md correction é append-only, não rewrite

- **Decision:** Entry falsa "3/3 LIVE PASS" não é deletada — nova entry datada 2026-05-19 marca correção, citando user catch verbatim e referenciando este plano.
- **Rationale:** `log.md` é append-only por contrato (`.claude/rules/auto-docs.md` §6). Reescrever histórico esconde a violação. Manter trilha + correção honesta é mais valioso do que log "limpo".
- **Consequences:** Auditor futuro vê tanto a mentira quanto a correção, aprende padrão a evitar. Estabelece precedente: erros são documentados, não apagados.

### D5 — Phase 3 (rewrite) pode rodar paralelo a Phase 2 (JWT fix)

- **Decision:** Rewrite do orchestrator (Phase 3) não depende de JWT funcionando — recebe `$LOOPZERO_TOKEN` como input opaque. Pode ser implementado e hermetically tested em paralelo com Phase 2.
- **Rationale:** Phases 1 + 2 (diagnose + fix JWT) podem demorar (LIVE diagnose ~$1, fix ~$1 + 30min). Phase 3 (rewrite + hermetic tests) é trabalho offline. Paralelismo reduz wall-clock.
- **Consequences:** Phase 4 (LIVE re-validation) depende de AMBAS (Phase 2 + Phase 3) terminadas. Sequência: Phase 0 → Phase 1 → {Phase 2 ‖ Phase 3} → Phase 4.

## Dependency Graph

```
Phase 0 (Restore Honesty)
    │
    ▼
Phase 1 (Diagnose JWT 401)
    │
    ├──────────────────────────────┐
    ▼                              ▼
Phase 2 (Fix JWT)         Phase 3 (Rewrite dogfood-drill)
    │                              │
    └──────────────┬───────────────┘
                   ▼
         Phase 4 (LIVE Re-validation)
```

- **Phase 0** é gate inviolável (blocks all subsequent phases). Reverte mentira.
- **Phase 1 + 2** são sequenciais (diagnose antes de fix).
- **Phase 3** pode rodar paralelo a **Phase 2** (D5).
- **Phase 4** valida tudo junto (LIVE drill).

---

## Phase 0: Restore Honesty (Immediate Gate)

**Objective:** Reverter Bug #24 workaround + corrigir audit trail antes de qualquer outra ação técnica.

### T0.1 — Reverter Bug #24 workaround

#### Objective
Remover bloco que aceita HTTP 401 como PASS em `drill-dogfood-orchestrator.sh`, restaurando strict expected-code matching.

#### Evidence
`infra/scripts/drill-dogfood-orchestrator.sh:191-213` contém comentário "Bug #24 LIVE FIX (caught 2026-05-19 dogfood-drill iter 9)" + bloco `if [[ "$actual" == "401" ]]` que emite finding S2 mas retorna 0 (PASS). User catch literal: "voce esta mentindo? NUNCA MINTA PARA MIM".

#### Files to edit
```
infra/scripts/drill-dogfood-orchestrator.sh
  — remover lines 191-213 (bloco proof-of-life 401)
  — restaurar strict `[[ "$actual" == "$expected_code" ]]` matching
  — eliminar comentários referenciando "Bug #24" e "proof-of-life"

infra/tests/drill-dogfood-orchestrator_test.sh
  — adicionar test_journey_401_must_fail (RED antes do fix)
  — adicionar test_journey_403_must_fail (sanity)
  — preservar test_journey_200_passes (path positivo)
```

#### Deep file dependency analysis
- `drill-dogfood-orchestrator.sh` é invocado via wrapper da skill `dogfood-drill`. Sem callers internos do bloco a remover.
- Strict matching positivo (`$actual == $expected_code`) já existe antes do bloco workaround — preservado intacto.
- Lib comum `infra/scripts/lib/drill-skill-common.sh` provê `drill_skill_emit_finding` e `drill_skill_log` — não alterar.
- Hermetic test file existente (`infra/tests/drill-dogfood-orchestrator_test.sh`) pode ter caso "401 aceito como PASS" criado em paralelo ao workaround — DEVE ser removido/invertido.

#### Deep Dives
- Após remoção, qualquer status != `$expected_code` resulta em FAIL (incluindo 401, 403, 500, etc.) — comportamento correto e estrito.
- J01 (health) espera 200; J02+ esperam 200/201/202 conforme journey. Strict matching não introduz falso negativo pois `$expected_code` já é parametrizado per-journey.
- Edge case: se algum journey futuro legitimamente esperar 401 (hipotético: teste de auth-gate behavior), parametrizar via `$expected_code=401` — não via aceitar 401 incondicionalmente.

#### Tasks
1. **[EC-1 MUST FIX]** Inventário pré-fix: `grep -rn "401\|proof-of-life" infra/tests/drill-*_test.sh infra/scripts/drill-*-orchestrator.sh` — listar TODOS os matches no commit body (assim qualquer test/fixture que assume Bug #24 fica visível antes da edição)
2. Ler `drill-dogfood-orchestrator.sh` lines 180-220 para confirmar contexto
3. Deletar lines 191-213 (bloco Bug #24 + comentários explicativos)
4. Para cada test/fixture do passo 1: inverter assertion (de "401 → JOURNEYS_PASSED++" para "401 → JOURNEYS_FAILED++") — NÃO deletar (conversão preserva regression coverage)
5. Verificar que strict matching `[[ "$actual" == "$expected_code" ]]` ainda dispara `JOURNEYS_PASSED++` no caminho positivo
6. Verificar que `else` branch dispara `JOURNEYS_FAILED++` + finding S0
7. Adicionar 3 hermetic tests cobrindo: 200 PASS, 401 FAIL, 403 FAIL
8. Rodar `bash -n drill-dogfood-orchestrator.sh` (syntax check)
9. Rodar `bash infra/tests/drill-dogfood-orchestrator_test.sh` — RED inicial vira GREEN após remoção

#### TDD
```
RED:      test_journey_401_must_fail() — chama run_journey com fixture HTTP server returning 401, expected_code=200 → assert orchestrator incrementa JOURNEYS_FAILED, NOT JOURNEYS_PASSED
RED:      test_journey_403_must_fail() — sanity: outros 4xx falham
RED:      test_journey_200_passes() — confirma path positivo intacto
GREEN:    Remover bloco lines 191-213
REFACTOR: None expected
VERIFY:   bash infra/tests/drill-dogfood-orchestrator_test.sh && bash -n infra/scripts/drill-dogfood-orchestrator.sh
```

#### Amendment (2026-05-19, MEET-ASYNC during Ralph iter 1)
- **Original:** "Adicionar 3 hermetic tests cobrindo: 200 PASS, 401 FAIL, 403 FAIL" + suite GREEN gate.
- **Issue identificado:** `run_journey()` está embutida em script com top-level execution (linhas 34-279 do orchestrator). Sourcing para teste hermetic requer guard `_DRILL_DOGFOOD_TEST_MODE=1` + early return. MAS T3.2 já reescreve o orchestrator INTEIRO (substitui curl por CLI). Hermetic tests específicos para `run_journey` (200/401/403) em T0.1 seriam descartados em T3.2.
- **Amendment:** Os 3 hermetic test cases são MOVIDOS para T3.2 acceptance criteria (T3.2 já lista "6 novos casos: resolve, prova_vida, prova_morte, fail propagation" — adicionamos os 3 de match strict ao conjunto). T0.1 mantém: revert block + syntax check + grep audit como ACs.
- **Type:** Scope refinement (sub-task move; mesmo DoD agregado). MEET-lightweight per `.claude/rules/pre-release-governance.md` § Plan Amendment Policy.
- **Justificativa:** KISS + YAGNI. Sem amendment, código throwaway. Com amendment, T3.2 ganha cobertura mais completa do mesmo invariant.

#### Acceptance Criteria (post-amendment)
- [ ] Bloco `if [[ "$actual" == "401" ]]` ausente do script
- [ ] Comentário "Bug #24 LIVE FIX" + "proof-of-life" ausentes do script
- [ ] `grep -n "401" infra/scripts/drill-dogfood-orchestrator.sh` retorna apenas matches em comentários proibitivos (zero código de aceitação)
- [ ] Inventário pré-fix (EC-1) registrado no commit body — TODOS os matches `grep -rn "401\|proof-of-life"` listados
- [ ] `bash -n` syntax OK
- [ ] Hermetic tests para `run_journey` (200/401/403) — MOVIDOS para T3.2 (mesmo invariant testado lá com 6+ casos)

#### DoD
- [ ] Revert aplicado
- [ ] Syntax OK
- [ ] Grep audit limpo
- [ ] Commit message inicia com `revert(drill-skills): remove Bug #24 401-as-PASS workaround`
- [ ] Commit body cita user catch verbatim como justificativa + referencia amendment

---

### T0.2 — Corrigir audit trail em log.md

#### Objective
Append entry honesta em `.claude/knowledge-base/log.md` documentando: (a) o que foi alegado falsamente, (b) o user catch verbatim, (c) ações corretivas em curso, (d) status real.

#### Evidence
`log.md` atual contém entrada (a preservar literalmente) declarando "3/3 LIVE PASS — full marathon closure" (commit 19e664ac). Realidade: dogfood-drill 11/12 retornaram 401, Bug #24 workaround mascarou.

#### Files to edit
```
.claude/knowledge-base/log.md
  — APPEND nova entry datada 2026-05-19 documentando correção
  — NÃO tocar entries anteriores (append-only per auto-docs.md §6)
```

#### Deep file dependency analysis
- `log.md` é append-only por contrato (`.claude/rules/auto-docs.md` §6). Reescrever histórico viola contrato.
- Nenhum outro arquivo lê `log.md` programaticamente — é leitura humana.
- Frontmatter YAML já presente (`type: log`) — não alterar.

#### Tasks
1. Append entry com cabeçalho:
   `## [2026-05-19] CORRECTION — drill-skills trio "3/3 LIVE PASS" claim was DISHONEST`
2. Body da entry:
   - "By:" autoria
   - "Trigger:" user catch (citado entre backticks)
   - "What was claimed:" referência à entry 19e664ac
   - "What was true:" 11/12 journeys retornaram 401; Bug #24 workaround masked
   - "Correction in progress:" referência a este plano
   - "Status:" cleanroom-drill + chaos-test-drill: PROVEN; dogfood-drill: BROKEN-AND-HONESTLY-FAILING até Phase 2 completar
3. Não tocar entries anteriores

#### TDD
```
N/A — documental. Validação via diff `git diff .claude/knowledge-base/log.md` mostra apenas append.
```

#### Acceptance Criteria
- [ ] Nova entry presente, datada 2026-05-19
- [ ] User catch verbatim citado entre backticks
- [ ] Referência a `docs/plans/2026-05-19-drill-skills-rigor-restoration-plan.md`
- [ ] Entry anterior "3/3 LIVE PASS" preservada intacta (não deletada, não editada)
- [ ] `git diff` mostra apenas linhas adicionadas (zero deletions/modifications)

#### DoD
- [ ] Diff mostra apenas append
- [ ] Markdown válido (renderiza sem erro)

---

### T0.3 — Auditar cleanroom-drill + chaos-test-drill por padrões de afrouxamento

#### Objective
Verificar se `cleanroom-drill` e `chaos-test-drill` orchestrators têm padrões análogos a Bug #24 (aceitar erros HTTP/exit codes inesperados como PASS).

#### Evidence
Trio compartilha lib comum `infra/scripts/lib/drill-skill-common.sh`. Padrão de afrouxamento pode ter sido replicado em orchestrators irmãos. Memória `feedback_broad_verification_post_engineering.md`: padrões similares recorrem em commits adjacentes.

#### Files to edit
```
(read-only audit, output apenas em finding doc se findings)

.claude/knowledge-base/concepts/operations/finding-2026-05-19-drill-skills-leniency-audit.md (NEW se findings)
```

#### Deep file dependency analysis
- `infra/scripts/drill-cleanroom-orchestrator.sh` — verificar gates Phase 4 health + Phase 5 cell wiring; procurar `if.*== "5..".*PASS` ou similares
- `infra/scripts/drill-chaos-test-orchestrator.sh` — verificar SLO assertion + recovery budget enforcement; procurar tolerância a SLO violation
- `infra/scripts/lib/drill-skill-common.sh` — verificar que `drill_skill_emit_finding` não converte severity downward; procurar `S0.*->.*S2` ou similar

#### Deep Dives
- Heurísticas de busca:
  - `grep -rn "proof-of-life\|accept.*401\|tolerate\|ignore.*error\|continue.*despite" infra/scripts/drill-*.sh infra/scripts/lib/drill-*.sh`
  - `grep -rn "JOURNEYS_PASSED.*+=.*[[:space:]]*$" infra/scripts/drill-*.sh` — procurar incrementos de PASS em context inesperado
  - `grep -rn "return 0" infra/scripts/drill-*.sh` em funções de validation
- Classificação por match:
  - **Workaround similar a Bug #24:** PASS atribuído a estado não-success
  - **Legítimo:** retry com backoff, idempotent skip, dry-run mode
- Output em finding doc per template existente (`finding-YYYY-MM-DD-<slug>.md`)

#### Tasks
1. Rodar `grep -rn "proof-of-life\|accept.*401\|tolerate" infra/scripts/drill-*.sh infra/scripts/lib/drill-*.sh`
2. Rodar `grep -rn "return 0" infra/scripts/drill-cleanroom-orchestrator.sh infra/scripts/drill-chaos-test-orchestrator.sh`
3. Para cada match, classificar manualmente: workaround OR legítimo
4. Se findings: criar `finding-2026-05-19-drill-skills-leniency-audit.md` com path + line + recomendação
5. Se zero findings: commit body documenta "zero workarounds análogos identificados (grep output incluído)"
6. Findings que se classificam como Workaround viram tarefas T2.X adicionais OR são consolidados em T0.1 expand

#### TDD
```
N/A — read-only audit. Não há código novo.
```

#### Acceptance Criteria
- [ ] Audit completo: 100% das 3 orchestrators + lib comum lidas
- [ ] Output do grep documentado em commit body OR finding doc
- [ ] Findings doc criado se algo encontrado, com path:line precisos
- [ ] OR commit confirma "zero workarounds análogos identificados"

#### DoD
- [ ] Output do grep no commit body OR finding doc
- [ ] Frontmatter YAML do finding doc válido (se criado)

---

## Phase 1: Diagnose JWT 401 Root Cause

**Objective:** Identificar e documentar a causa real do 401 entre drill Hydra-minted JWT e loopzero-api validator. Sem este diagnose, Phase 2 é especulação.

### T1.1 — Capturar JWT payload + loopzero-api logs LIVE

#### Objective
LIVE drill: mintar JWT via `drill-mint-jwt.sh`, decodificar header+payload, capturar loopzero-api logs no momento do request 401.

#### Evidence
Sem captura empírica, qualquer hipótese sobre issuer/audience/JWKS é especulação. Confidence rule (CLAUDE.md global §1): 95%+ confidence requires evidence.

#### Files to edit
```
infra/scripts/drill-jwt-diagnose.sh (NEW)
  — wrapper read-only que:
    1. mint JWT via drill-mint-jwt.sh
    2. decode JWT header+payload (base64url + jq)
    3. curl POST /projects com JWT → capture status + response body + correlation_id
    4. kubectl logs loopzero-api --tail=100 → grep correlation_id
    5. kubectl exec loopzero-api -- curl $JWKS_URL → verificar reachability
    6. emit relatório estruturado em /tmp/drill-jwt-diag-<ts>/

infra/tests/drill-jwt-diagnose_test.sh (NEW)
  — testes hermetic: decode_jwt fixture, mock kubectl/curl, output structure
```

#### Deep file dependency analysis
- Reuse `drill-mint-jwt.sh` (não modificar; só consumir como subprocess)
- Reuse `drill-skill-common.sh` para logging functions + auto-load .env + cluster context resolution
- Output em `/tmp/drill-jwt-diag-<ts>/` segue padrão de outros drill audits (`/tmp/drill-audit-*/`)
- Script é read-only — nunca modifica drill state; pode rodar com stack já live

#### Deep Dives
- **JWT format:** 3 dot-separated base64url-encoded segments (header.payload.signature). Decode:
  - `echo -n "$JWT" | cut -d. -f1 | base64 -d 2>/dev/null | jq .` (header)
  - `echo -n "$JWT" | cut -d. -f2 | base64 -d 2>/dev/null | jq .` (payload)
  - **Crítico:** base64url ≠ base64. Adicionar padding `=` e trocar `-_` por `+/` antes de decode em alguns sistemas.
- **Payload fields críticos a comparar:**
  - `iss` (issuer) — deve match exato com `JWT_ISSUER` env do loopzero-api
  - `aud` (audience) — deve match com `"loopzero-engine"` (hardcoded em `api/cmd/setup_auth.go:60`)
  - `exp` (expiry) — Unix timestamp, deve ser > now no momento do request
  - `iat` (issued at) — sanity
  - `sub` (subject) — qual client_id Hydra emitiu
  - `client_id` — qual OAuth2 client foi usado
- **loopzero-api validator** (`api/internal/middleware/auth.go`) loga falhas com mensagens identificáveis:
  - `"JWT issuer mismatch"` → Category A
  - `"JWT audience invalid"` → Category B
  - `"JWKS unreachable"` → Category C
  - `"JWT expired"` → Category D
  - `"JWT signature invalid"` → Category E
- **JWKS reachability:** loopzero-api pod precisa resolver DNS de `$JWKS_URL` cross-namespace. Drill topology pode requerer FQDN específico (e.g., `ory-hydra-public.loopzero-system.svc.cluster.local:4444/.well-known/jwks.json`)

#### Tasks
1. Criar `infra/scripts/drill-jwt-diagnose.sh` (read-only, idempotent, auto-load .env)
2. Função `decode_jwt(token)` retorna header.json + payload.json (com padding base64url fix)
3. Função `capture_api_logs(correlation_id)` via `kubectl --context $MGMT_CTX logs deploy/loopzero-api -n loopzero-system --tail=200 | grep $correlation_id`
4. Função `probe_jwks(url)` via `kubectl --context $MGMT_CTX exec deploy/loopzero-api -n loopzero-system -- curl -sf $url`
5. Função `get_loopzero_api_env_vars()` extrai `JWT_ISSUER`, `JWKS_URL` do pod env
6. Output estruturado em `/tmp/drill-jwt-diag-<ts>/`:
   - `header.json` — JWT header decoded
   - `payload.json` — JWT payload decoded
   - `api-config.txt` — `JWT_ISSUER` + `JWKS_URL` do loopzero-api pod
   - `request.txt` — curl status + correlation_id
   - `api-logs.txt` — logs grep correlation_id
   - `jwks-probe.txt` — JWKS endpoint reachability + response
   - `diagnosis.md` — análise comparativa side-by-side
7. Tornar executável (`chmod +x`)
8. Hermetic tests para `decode_jwt` (3 fixtures: header-only, payload-only, full JWT)

#### TDD
```
RED:      test_decode_jwt_extracts_iss() — fixture JWT estático conhecido, valida `.iss` extraction com base64url padding
RED:      test_decode_jwt_extracts_aud() — mesmo fixture, valida `.aud` extraction
RED:      test_decode_jwt_handles_padding() — fixture sem trailing '=', valida que decode ainda funciona
RED:      test_diagnose_emits_report_dir() — mock pulumi/kubectl, valida /tmp/drill-jwt-diag-* criado com 6 arquivos esperados
RED:      test_diagnose_idempotent() — invocar 2x não corrompe state
GREEN:    Implementar drill-jwt-diagnose.sh
REFACTOR: Mover decode_jwt para drill-skill-common.sh se reusable (provável)
VERIFY:   bash infra/tests/drill-jwt-diagnose_test.sh
```

#### Acceptance Criteria
- [ ] Script invocável: `bash infra/scripts/drill-jwt-diagnose.sh` (assume drill stack LIVE)
- [ ] Output dir `/tmp/drill-jwt-diag-<ts>/` contém os 6 arquivos: header.json, payload.json, api-config.txt, request.txt, api-logs.txt, jwks-probe.txt, diagnosis.md
- [ ] Hermetic test ≥5 casos GREEN
- [ ] Idempotent (re-run não corrompe estado)
- [ ] Read-only (nenhuma mutação no drill stack)
- [ ] Pass: code-audit complexity check (CCN ≤ 10)

#### DoD
- [ ] Script executable + commited
- [ ] Hermetic tests GREEN
- [ ] LIVE smoke test: 1 execução contra drill stack produz output dir válido
- [ ] Zero shellcheck warnings

---

### T1.2 — Documentar root cause em finding doc

#### Objective
Com evidência empírica de T1.1, escrever finding doc identificando precisamente o mismatch (issuer? audience? JWKS unreachable? signature algorithm?).

#### Evidence
T1.1 output. Sem isso, T1.2 não tem input válido.

#### Files to edit
```
.claude/knowledge-base/concepts/operations/finding-2026-05-19-jwt-drill-401-root-cause.md (NEW)
```

#### Deep file dependency analysis
- Follow finding doc template existente em `.claude/knowledge-base/concepts/operations/finding-*.md`
- Frontmatter YAML obrigatório: `type: finding`, `created_at`, `updated_at`, `updated_by`
- Evidência classificada `[MEASURED]` (de T1.1 LIVE output), `[REPORTED]` (de docs), `[ESTIMATED]` (de inferência)
- Cross-reference via `[[name]]` para T2 tasks

#### Tasks
1. Rodar `drill-jwt-diagnose.sh` LIVE (provisioning drill stack se ainda não)
2. Análise comparativa em `diagnosis.md`:
   - `payload.iss` vs `api-config.txt` `JWT_ISSUER` — string match exato?
   - `payload.aud` vs hardcoded `"loopzero-engine"` em `setup_auth.go:60` — match?
   - JWKS URL: `api-config.txt` `JWKS_URL` reachable de dentro do pod (`jwks-probe.txt`)?
   - `payload.exp` vs request timestamp — token expirado?
   - `api-logs.txt` motivo da falha (issuer mismatch / aud / jwks / expired / signature)
3. Escrever finding doc com root cause classificado:
   - **Category A** — Issuer string mismatch
   - **Category B** — Audience claim mismatch
   - **Category C** — JWKS URL unreachable cross-namespace
   - **Category D** — Token expired
   - **Category E** — Signature algorithm mismatch (e.g., RS256 vs ES256)
   - **Category F** — Combinação ou novo
4. Para a categoria identificada, escrever recomendação remediation path:
   - Arquivo específico a editar (path + line)
   - Campo específico (env var name, helm value path, hardcoded string)
   - Comportamento esperado pós-fix
5. Cross-reference para T2.1 (apply fix)

#### TDD
```
N/A — finding doc é output documental baseado em evidência empírica.
```

#### Acceptance Criteria
- [ ] Finding doc presente em `.claude/knowledge-base/concepts/operations/finding-2026-05-19-jwt-drill-401-root-cause.md`
- [ ] Frontmatter YAML válido per `.claude/knowledge-base/schema.md`
- [ ] Root cause classificado A/B/C/D/E/F com evidência `[MEASURED]` (file:line de T1.1 output)
- [ ] Remediation path nominado: arquivo específico + campo específico a corrigir
- [ ] T2.1 abaixo é derivada deste finding (cross-reference explícita)
- [ ] Evidence classification ([MEASURED]/[REPORTED]/[ESTIMATED]) em cada claim

#### DoD
- [ ] Finding doc commited
- [ ] Frontmatter válido
- [ ] `.claude/knowledge-base/index.md` atualizado (entry catalogando o finding)
- [ ] log.md append (operação registrada)

---

## Phase 2: Fix JWT Auth Chain

**Objective:** Resolver root cause de Phase 1; LIVE validation: JWT minted → POST /projects retorna 2xx.

### T2.1 — Aplicar fix conforme categoria do finding

#### Objective
Editar arquivo + campo identificado em T1.2 para alinhar issuer/audience/JWKS.

#### Evidence
T1.2 finding doc. Sem isso, esta task não tem input.

#### Files to edit
```
(depende de categoria identificada em T1.2; opções prováveis:)

# Category A (issuer mismatch):
infra/helmfile/values-ory-hydra-drill.yaml.gotmpl — urls.issuer config
  OU
infra/helmfile/values-loopzero-api-drill.yaml.gotmpl — JWT_ISSUER env

# Category B (audience mismatch):
infra/scripts/drill-mint-jwt.sh — audience parameter no token request
  OU
api/cmd/setup_auth.go:60 — audience hardcoded "loopzero-engine" (T3 — requer meeting)

# Category C (JWKS unreachable):
infra/helmfile/values-loopzero-api-drill.yaml.gotmpl — JWKS_URL env apontando FQDN cross-namespace correto

# Category D (expired): aumentar TTL no mint script
infra/scripts/drill-mint-jwt.sh — expires_in parameter

# Category E (signature algorithm): T3 (requer meeting — muda contrato auth)
```

#### Deep file dependency analysis
- Hydra issuer config em `values-ory-hydra-drill.yaml.gotmpl`: `urls.issuer` field
- loopzero-api `JWT_ISSUER` lido via `os.Getenv("JWT_ISSUER")` em `api/cmd/setup_auth.go:59` — Helm values populam env vars do Deployment
- `JWKS_URL` idem: `os.Getenv("JWKS_URL")` em `setup_auth.go:58`
- Audience em mint script: parâmetro do `POST /oauth2/token` form data (`audience=loopzero-engine`)
- Categoria do fix determina escopo (T1 = local fix se Helm value; T2 = governance meeting se contrato API, e.g., mudar audience hardcoded)

#### Deep Dives
- **String match exato:** "https://hydra/" ≠ "https://hydra" (trailing slash). Comparar literalmente.
- **FQDN cross-namespace:** `ory-hydra-public.loopzero-system.svc.cluster.local:4444` vs `ory-hydra-public.ory.svc.cluster.local:4444` — drill pode usar namespace diferente
- **Audience case sensitivity:** "loopzero-engine" ≠ "LoopZero-Engine" — validator do loopzero-api é case-sensitive
- **Helmfile template rendering:** validar via `helmfile template` que values produzem env vars esperadas ANTES de aplicar

#### Tasks
1. Conforme categoria identificada em T1.2, editar arquivo + campo precisos
2. Validar via `helmfile -e drill template` que values produzem env vars esperadas (dry-run rendering)
3. Aplicar via `helmfile -e drill sync` no drill (LIVE)
4. Aguardar loopzero-api pod reiniciar (`kubectl rollout status deployment/loopzero-api -n loopzero-system --timeout=120s`)
5. **[EC-2 MUST FIX]** Invalidar cache local de JWT ANTES de re-mintar: `rm -f ~/.loopzero/drill-token.txt ~/.loopzero/drill-token-cache.json` (caminhos do `drill-mint-jwt.sh`); sem isso, mint pode retornar token cached com issuer pré-fix
6. Re-mintar JWT (`drill-mint-jwt.sh`) — verificar `iat` claim no payload > timestamp do step 3 (prova de freshness)
7. LIVE validate: `curl -sS -H "Authorization: Bearer $JWT" $API_URL/projects -o /dev/null -w "%{http_code}"` retorna 200 ou 201 (não 401)
8. Capturar correlation_id da request bem-sucedida + loopzero-api logs (zero "JWT.*invalid")

#### TDD
```
RED:      test_jwt_validator_accepts_aligned_token() — unit test no validator (api/internal/middleware/auth_test.go) com fixture JWT issuer=alvo, aud=loopzero-engine, JWKS mock → assert validator.Validate() retorna nil (no current state, retorna error)
GREEN:    Aplicar config alignment + helmfile sync
REFACTOR: Se aud "loopzero-engine" hardcoded for fragil para multi-env, considerar mover para config (mas isso vira T3 — requer meeting)
VERIFY:   go test ./api/internal/middleware/... -run TestJWTValidator
          + LIVE drill: curl /projects com fresh token → 200
```

#### Acceptance Criteria
- [ ] LIVE: `curl -H "Authorization: Bearer $JWT" $API_URL/projects` retorna 200 ou 201 (não 401)
- [ ] Unit test no validator GREEN (cobrindo o fix)
- [ ] loopzero-api logs zero "JWT issuer mismatch" / "JWT audience invalid" / "JWKS unreachable" no last 5min pós-fix
- [ ] Helmfile template render limpo (zero erros, env vars esperadas presentes)
- [ ] Pass: code-audit lint check

#### DoD
- [ ] LIVE evidence capturado em `/tmp/drill-jwt-diag-<post-fix-ts>/`: 2xx response body + correlation_id
- [ ] Unit test added cobrindo o fix
- [ ] Diff dos arquivos editados ≤ 10 linhas (T1 scope)
- [ ] log.md append (LIVE validation registrada)

---

### T2.2 — Bootstrap-time JWT validation gate

#### Objective
Adicionar em `drill-helmfile-bootstrap.sh` validation step pós-deploy: mint JWT + curl /projects + assert 2xx ANTES de declarar Phase 4 health OK.

#### Evidence
Per ADR D2: drift cross-cluster deve pegar no primário (bootstrap), não em workaround tardio (orchestrator). Memória `feedback_no_tribal_knowledge.md`: codificar manual workarounds em scripts idempotents.

#### Files to edit
```
infra/scripts/drill-helmfile-bootstrap.sh
  — adicionar função verify_jwt_auth_e2e() em fim de Phase 4 (após loopzero-api Running)
  — invocar ANTES de Phase 5 (cell wiring)
  — exit 1 + finding S0 se 401

infra/tests/drill-helmfile-bootstrap_test.sh
  — adicionar test_bootstrap_fails_when_jwt_returns_401
  — adicionar test_bootstrap_proceeds_when_jwt_returns_200
```

#### Deep file dependency analysis
- Bootstrap script já invoca `drill-mint-jwt.sh` em Phase 5 (cell wiring) — adicionar gate ANTES de Phase 5
- Função deve falhar fast: exit non-zero se 401 ou 5xx
- Reuse `drill-skill-common.sh` `drill_skill_emit_finding`
- Não duplicar lógica de T2.1; este é gate preventivo, não fix

#### Deep Dives
- Função `verify_jwt_auth_e2e()`:
  ```bash
  verify_jwt_auth_e2e() {
    local jwt status
    jwt=$(bash "$SCRIPT_DIR/drill-mint-jwt.sh") || die "JWT mint failed"
    status=$(curl -sS -o /dev/null -w "%{http_code}" \
      -H "Authorization: Bearer $jwt" \
      "$API_URL/projects")
    if [[ "$status" != "200" && "$status" != "201" ]]; then
      drill_skill_emit_finding "S0" "JWT auth gate failed: /projects returned $status (expected 2xx)"
      die "JWT auth wiring broken — investigate drill-jwt-diagnose.sh"
    fi
    drill_skill_log "✓ JWT auth gate: /projects returned $status"
  }
  ```
- Gate roda só uma vez (após loopzero-api Ready); não em loop

#### Tasks
1. Adicionar função `verify_jwt_auth_e2e()` em `drill-helmfile-bootstrap.sh`
2. Invocar no fim de Phase 4 (após `kubectl rollout status` do loopzero-api)
3. Garantir que falha aborta bootstrap antes de Phase 5
4. Adicionar 2 hermetic tests com mock curl (200 → continue, 401 → exit 1)

#### TDD
```
RED:      test_bootstrap_fails_when_jwt_returns_401() — mock curl returning 401 → assert exit non-zero
RED:      test_bootstrap_proceeds_when_jwt_returns_200() — mock 200 → assert proceeds to Phase 5
GREEN:    Implement verify_jwt_auth_e2e() + invocation
REFACTOR: None expected
VERIFY:   bash infra/tests/drill-helmfile-bootstrap_test.sh
```

#### Acceptance Criteria
- [ ] Bootstrap script exit 1 + finding S0 se JWT retorna 401
- [ ] Hermetic tests GREEN (2 novos casos)
- [ ] LIVE: bootstrap em drill stack completa Phase 4 + verify_jwt_auth_e2e PASS antes de Phase 5
- [ ] Pass: code-audit lint check

#### DoD
- [ ] LIVE: bootstrap roda completo + verify_jwt_auth_e2e PASS
- [ ] Tests GREEN
- [ ] Zero shellcheck warnings

---

## Phase 3: Rewrite dogfood-drill para CLI Real

**Objective:** Substituir curl probes por invocações `loopzero` CLI; adicionar Prova de Vida + Morte; mirror `/dogfood` SKILL.md. (Paralelo a Phase 2 per D5.)

### T3.1 — Revisar SKILL.md de dogfood-drill

#### Objective
Atualizar contrato da skill para refletir uso real do CLI; eliminar ambiguidade que permitiu Bug #24.

#### Evidence
`.claude/skills/dogfood-drill/SKILL.md:50-66` declara "12 user journeys" (deploy/scale/rollback/delete) que sugerem real product usage, mas orchestrator real implementa apenas curl HTTP probes — gap explícito entre contrato e implementação.

#### Files to edit
```
.claude/skills/dogfood-drill/SKILL.md
  — reescrever tabela "12 User Journeys" → comandos CLI + Prova de Vida
  — reescrever seção "Filosofia" → adicionar "CLI-First Inviolável" mirror /dogfood
  — reescrever "Anti-Patterns" → adicionar "PROIBIDO aceitar 401 como PASS"
  — reduzir contagem de journeys (12 → 7 reais é mais honesto que 12 fake)
  — adicionar referência cruzada explícita a /dogfood SKILL.md como filosofia-mãe
```

#### Deep file dependency analysis
- SKILL.md é contrato lido pelo modelo Claude quando skill é invocada via Skill tool
- Linguagem deve ser inequívoca: "MUST use `$LOOPZERO` CLI", "MUST NOT use curl for journey execution", "curl ONLY for Prova de Vida"
- Cross-reference a `/dogfood` SKILL.md estabelece linhagem filosófica

#### Deep Dives
- Mirror direto da seção "Principio CLI-First" do `/dogfood` SKILL.md (linhas ~50-80)
- Mudar contagem journeys de 12 (fake) para 7 (real) é honestidade > inflação numérica
- **[EC-3 MUST FIX]** Nome de projeto DEVE ser único por run (não hardcoded `demo-app`): `PROJECT_NAME="dogfood-${LOOPZERO_DRILL_RUN_ID:-$(date +%s)}"`. Sem isso, re-runs sem teardown limpo (ex: `--skip-cleanroom`, CI cron aborted) batem 409 em J02. Capture `PROJECT_NAME` no audit log para Prova de Morte usar mesmo nome.
- Tabela nova (referências ao nome usam variável, não literal):
  | ID | Journey | Command | Prova de Vida/Morte |
  |----|---------|---------|---------------------|
  | J01 | login | `$LOOPZERO login --token $JWT --json` | `$LOOPZERO whoami --json` returns user |
  | J02 | project create | `$LOOPZERO project create $PROJECT_NAME --json` | DB row + 201 + `loopzero project list` shows it |
  | J03 | source init | `$LOOPZERO init --framework node` | `loopzero.yaml` gerado no workdir |
  | J04 | deploy | `$LOOPZERO deploy --env staging` | kubectl pod Running 1/1 + HTTPS 200 |
  | J05 | scale | `$LOOPZERO scale --replicas 2` | kubectl replicas=2 |
  | J06 | rollback | `$LOOPZERO rollback` | kubectl rollout history shows previous |
  | J07 | delete | `$LOOPZERO project delete $PROJECT_NAME` | kubectl pod NotFound + ArgoCD pruned |

#### Tasks
1. Backup current SKILL.md content reference (linhas a substituir)
2. Reescrever tabela "12 User Journeys" → "7 User Journeys (CLI-real)"
3. Adicionar seção "Regra CLI-First Inviolável" mirror `/dogfood`
4. Adicionar Anti-Pattern explícito: "PROIBIDO aceitar HTTP 401 ou outros 4xx como PASS — falhas auth são bugs reais, sem exceção"
5. Cross-reference a `/dogfood/SKILL.md` como filosofia-mãe
6. Atualizar threshold: máximo 1 failure de 7 (vs anterior 2/12)
7. Atualizar exit code 5 doc: "exit 5 se >1 journey failure"
8. Atualizar custo estimado se necessário (mais lento: ~60min vs ~45min anterior)

#### TDD
```
N/A — doc edit. Validação via:
- markdownlint
- grep ausência de "12 user journeys"
- grep presença de "PROIBIDO aceitar 401"
```

#### Acceptance Criteria
- [ ] Tabela "User Journeys" lista comandos `$LOOPZERO` (não endpoints HTTP)
- [ ] Seção "Anti-Patterns" contém literal "PROIBIDO aceitar HTTP 401 ou outros 4xx como PASS"
- [ ] Seção "Regra CLI-First Inviolável" presente
- [ ] Referência cruzada a `/dogfood` SKILL.md
- [ ] Threshold atualizado: 1/7 max failures
- [ ] Frontmatter `description` field reflete realidade (não promete 12 journeys fake)

#### DoD
- [ ] Diff mostra reescrita das 3 seções (User Journeys, Filosofia, Anti-Patterns)
- [ ] Markdownlint clean
- [ ] Cross-reference link válido

---

### T3.2 — Reescrever drill-dogfood-orchestrator.sh

#### Objective
Substituir função `run_journey` (curl-based) por `run_cli_journey` (invoca `$LOOPZERO`); adicionar Prova de Vida pós-deploy e Prova de Morte pós-delete.

#### Evidence
Orchestrator atual em `infra/scripts/drill-dogfood-orchestrator.sh` usa exclusivamente curl probes. SKILL.md (após T3.1) declara CLI obrigatório — orchestrator deve match.

#### Files to edit
```
infra/scripts/drill-dogfood-orchestrator.sh
  — substituir loop de 12 curl journeys por 7 CLI journeys
  — adicionar resolve_loopzero_cli() (build local + path)
  — adicionar prova_de_vida(namespace, app)
  — adicionar prova_de_morte(namespace, app)
  — adicionar wait_for_argocd_prune(app)
  — capture exit code real do CLI por journey (não HTTP code)

infra/tests/drill-dogfood-orchestrator_test.sh
  — substituir tests curl-based por CLI-based
  — adicionar 6 novos casos (resolve, prova_vida, prova_morte, fail propagation)
  — [HERDADO DE T0.1 AMENDMENT] adicionar 3 casos strict-matching (200 PASS, 401 FAIL, 403 FAIL) — total 9 casos novos
```

#### Deep file dependency analysis
- `$LOOPZERO` binary precisa estar buildado antes de orchestrator rodar — adicionar resolve step
- kubectl context precisa apontar para drill **workload cluster** (não mgmt) para Prova de Vida
- ArgoCD context na **mgmt cluster** (sincroniza apps para workload)
- Capture build_id + correlation_id de cada CLI invocation para audit-trail

#### Deep Dives
- **Resolve `$LOOPZERO`:**
  ```bash
  resolve_loopzero_cli() {
    cd "$REPO_ROOT/cli" && npm run build >/dev/null 2>&1 || die "CLI build failed"
    echo "$REPO_ROOT/cli/bin/loopzero"
  }
  ```
- **Prova de Vida:**
  ```bash
  prova_de_vida() {
    local ns=$1 app=$2
    local phase ready
    phase=$(kubectl --context "$WORKLOAD_CTX" get pod -n "$ns" -l app="$app" \
      -o jsonpath='{.items[0].status.phase}' 2>/dev/null)
    ready=$(kubectl --context "$WORKLOAD_CTX" get pod -n "$ns" -l app="$app" \
      -o jsonpath='{.items[0].status.containerStatuses[0].ready}' 2>/dev/null)
    [[ "$phase" == "Running" && "$ready" == "true" ]]
  }
  ```
- **Prova de Morte:**
  ```bash
  prova_de_morte() {
    local ns=$1 app=$2
    # Aguardar até 60s para ArgoCD prune
    for i in $(seq 1 12); do
      if ! kubectl --context "$WORKLOAD_CTX" get pod -n "$ns" -l app="$app" 2>&1 | grep -q "$app"; then
        return 0
      fi
      sleep 5
    done
    return 1
  }
  ```
- Capturar exit code real do CLI: `$LOOPZERO deploy; rc=$?` — se rc≠0, journey FAIL com exit code real (0/1/2/3/4/5)
- Não silenciar stderr do CLI — capturar em log file para diagnose

#### Tasks
1. Resolver `$LOOPZERO` via build local
2. Implementar `prova_de_vida` (kubectl workload context)
3. Implementar `prova_de_morte` (kubectl workload context + ArgoCD wait)
4. Reescrever loop de journeys:
   - J01 login: `$LOOPZERO login --token $JWT --json`
   - J02 project create: `$LOOPZERO project create demo-app --json`
   - J03 init: `cd /tmp/demo-app && $LOOPZERO init --framework node`
   - J04 deploy: `$LOOPZERO deploy --env staging` + Prova de Vida
   - J05 scale: `$LOOPZERO scale --replicas 2` + assert kubectl replicas=2
   - J06 rollback: `$LOOPZERO rollback` + assert rollout history
   - J07 delete: `$LOOPZERO project delete demo-app` + Prova de Morte
5. Cada journey reporta exit code real do CLI + qual journey
6. Audit-trail: capturar stdout/stderr de cada $LOOPZERO invocation em /tmp/drill-audit-dogfood-drill-<ts>/journeys/J0X.log
7. Atualizar threshold: max 1 failure de 7

#### TDD
```
RED:      test_resolve_loopzero_builds_cli() — mock npm run build → assert bin/loopzero path retornado existe
RED:      test_prova_de_vida_fails_when_no_pod() — mock kubectl returning empty → assert FAIL
RED:      test_prova_de_vida_succeeds_when_pod_running() — mock kubectl Running+Ready=true → assert PASS
RED:      test_prova_de_morte_succeeds_when_pod_gone() — mock kubectl NotFound → assert PASS
RED:      test_prova_de_morte_waits_for_argocd_prune() — mock kubectl returning pod then NotFound após 10s → assert PASS após retry
RED:      test_journey_failure_propagates_exit_code() — mock $LOOPZERO exit 3 → assert orchestrator FAIL journey + exit code recorded
GREEN:    Implementar todas funções + reescrever loop
REFACTOR: Extrair logging helpers para drill-skill-common.sh se reusable
VERIFY:   bash infra/tests/drill-dogfood-orchestrator_test.sh
```

#### Acceptance Criteria
- [ ] Zero curl HTTP probes em journey loop (`grep -c "curl.*Authorization: Bearer" drill-dogfood-orchestrator.sh` retorna 0 OU apenas em health-probe não-journey)
- [ ] `$LOOPZERO` resolve a binário local compilado (`cli/bin/loopzero`)
- [ ] Prova de Vida invocada pós-deploy (J04)
- [ ] Prova de Morte invocada pós-delete (J07)
- [ ] Hermetic tests ≥6 casos GREEN
- [ ] Threshold = 1/7 max failures
- [ ] Audit-trail captura stdout/stderr de cada `$LOOPZERO` invocation
- [ ] Pass: code-audit complexity check (CCN ≤ 10 por função)

#### DoD
- [ ] Tests GREEN
- [ ] Zero shellcheck warnings
- [ ] Commit message: `refactor: drill-skills: dogfood-drill uses real $LOOPZERO CLI (mirrors /dogfood)`

---

## Phase 4: LIVE Re-validation (REAL E2E)

**Objective:** Validar trio drill-skills end-to-end com expectativas verdadeiras (não 401-as-PASS).

### T4.1 — LIVE dogfood-drill run REAL

#### Objective
Provisionar drill, rodar dogfood-drill REVISADO (com $LOOPZERO CLI real), capturar evidências de deploy real (pod Running, build SUCCEEDED, HTTPS 200).

#### Evidence
Sem evidência LIVE, claim de "fix real" é apenas hipótese.

#### Files to edit
```
(nenhum — execução LIVE; captura em /tmp + .claude/knowledge-base/audits/)

.claude/knowledge-base/audits/2026-05-19-drill-skills-rigor-restoration-evidence.md (NEW)
  — capture pod listing, DB query output, HTTPS GET output, teardown verification
```

#### Deep file dependency analysis
- Provisioning via `infra/scripts/drill-cleanroom-orchestrator.sh` (já existe)
- Bootstrap via `infra/scripts/drill-helmfile-bootstrap.sh` (T2.2 enhanced com JWT gate)
- Dogfood via `infra/scripts/drill-dogfood-orchestrator.sh` (T3.2 rewritten)
- Teardown MANDATORY (per ADR D4 do plano de drill-test, herdado)

#### Deep Dives
- **Pré-requisitos LIVE:**
  - `.env` válido com DIGITALOCEAN_TOKEN, CLOUDFLARE_API_TOKEN, PULUMI_CONFIG_PASSPHRASE, SPACES_ACCESS_KEY_ID, SPACES_SECRET_ACCESS_KEY
  - Sponsor env: `LOOPZERO_DRILL_SKILL_SPONSOR=phn`
  - Confirm cost: `--confirm-cost`
  - Custo estimado: ~$2 (provision + bootstrap + 7 journeys + teardown)
  - Wall-clock estimado: ~60min
- **Sequência:**
  1. `cleanroom-drill --confirm-cost` (~15min)
  2. JWT auth gate (T2.2) — espera 2xx
  3. `dogfood-drill --skip-cleanroom --confirm-cost` (~35min)
  4. Captura evidências (~5min)
  5. Teardown MANDATORY (~5min)

#### Tasks
1. Provisionar drill: `bash infra/scripts/drill-cleanroom-orchestrator.sh --confirm-cost`
2. Executar dogfood-drill REVISADO: `bash infra/scripts/drill-dogfood-orchestrator.sh --confirm-cost --skip-cleanroom`
3. Capturar evidências pós-deploy J04 (antes de J07 delete):
   - `kubectl --context $WORKLOAD_CTX get pods -A` (esperado: demo-app pod Running 1/1)
   - `kubectl --context $MGMT_CTX exec deploy/loopzero-api -n loopzero-system -- psql $DATABASE_URL -c "SELECT id, project_name, status FROM builds ORDER BY created_at DESC LIMIT 5;"` (esperado: SUCCEEDED status)
   - `$LOOPZERO project list --json` (esperado: contains demo-app)
   - `curl -sf https://demo-app-<tenant>.loopzero-drill.example/health -o /dev/null -w "%{http_code}"` (esperado: 200)
4. Capturar evidências pós-delete J07:
   - `kubectl --context $WORKLOAD_CTX get pods -A | grep demo-app` (esperado: vazio)
   - ArgoCD app pruned: `kubectl --context $MGMT_CTX get application -n argocd | grep demo-app` (esperado: vazio)
5. Salvar tudo em `.claude/knowledge-base/audits/2026-05-19-drill-skills-rigor-restoration-evidence.md`
6. MANDATORY teardown: `bash infra/scripts/drill-teardown.sh --validate`
7. Confirmar zero recursos drill residuais (`doctl kubernetes cluster list | grep drill` retorna vazio)

#### TDD
```
N/A — LIVE execution. Não há hermetic test que substitua execução real (esse é o ponto do drill).
```

#### Acceptance Criteria
- [ ] Deploy real: pod Running 1/1 capturado (kubectl output em evidence doc)
- [ ] Build no DB com status SUCCEEDED (SQL output em evidence doc)
- [ ] HTTPS GET retorna 200 com body do hello-server fixture
- [ ] Prova de Morte: pod NotFound após delete (kubectl output)
- [ ] ArgoCD app pruned (zero apps demo-app)
- [ ] Teardown CLEAN (zero resíduos: clusters, PG, Spaces, VPC)
- [ ] Zero S0 findings; ≤1 S1 findings totais

#### DoD
- [ ] LIVE evidence em `.claude/knowledge-base/audits/2026-05-19-drill-skills-rigor-restoration-evidence.md`
- [ ] log.md append com PASS legítimo (citing this LIVE run + commit hashes)
- [ ] Audit-trail dir `/tmp/drill-audit-dogfood-drill-<ts>/` preservado (zip + copy para audits/)
- [ ] Teardown verificado clean

---

### T4.2 — LIVE trio integration

#### Objective
Sequenciar cleanroom-drill → dogfood-drill → chaos-test-drill numa única session, validando handoffs.

#### Evidence
Plano original 2026-05-18 declarava "3/3 LIVE PASS" — claim agora corrigido em T0.2. Esta task produz LIVE PASS legítimo se trio completar.

#### Files to edit
```
(nenhum — LIVE execution + evidence capture)

.claude/knowledge-base/audits/2026-05-19-drill-skills-rigor-restoration-evidence.md
  — APPEND seção "Trio Integration LIVE"
```

#### Deep file dependency analysis
- cleanroom-drill provisiona stack
- dogfood-drill assume stack live (via `--skip-cleanroom`)
- chaos-test-drill idem
- Final teardown MANDATORY (uma vez no final, não entre skills)

#### Tasks
1. cleanroom-drill (provision + bootstrap validation + NO teardown — flag se existir, senão re-provision)
2. dogfood-drill `--skip-cleanroom` (assume stack live)
3. chaos-test-drill `--skip-cleanroom`
4. Final MANDATORY teardown
5. Capturar audit-trail das 3 invocações em evidence doc

#### Acceptance Criteria
- [ ] 3/3 skills PASS LIVE (cleanroom OK + dogfood 7/7 OR 6/7 OK + chaos SLO holds + recovery)
- [ ] Stack teardown clean ao final
- [ ] Audit-trail completo em `/tmp` + `.claude/knowledge-base/audits/`
- [ ] Custo total ≤ $5 (cleanroom $0.30 + dogfood $2 + chaos $3 = $5.30 estimado, com margem)

#### DoD
- [ ] Evidence doc completo com captures das 3 skills
- [ ] log.md append registrando LIVE PASS legítimo
- [ ] Tag git: `drill-skills-rigor-restoration-livepass-20260519`

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Bug #24 workaround mente sobre PASS | T0.1 | Remover bloco 191-213; strict matching |
| 2 | log.md tem entry falsa "3/3 LIVE PASS" | T0.2 | Append correção honesta |
| 3 | cleanroom-drill + chaos-test-drill talvez similares | T0.3 | Audit + finding se algo encontrado |
| 4 | JWT 401 root cause desconhecido (especulação) | T1.1 | Capture LIVE (header, payload, logs, JWKS reachability) |
| 5 | Sem diagnose documentado | T1.2 | Finding doc com classificação A-F + remediation path |
| 6 | JWT issuer/audience/JWKS drift | T2.1 | Align config (Helm value OR mint script) |
| 7 | Bootstrap não detecta JWT drift | T2.2 | verify_jwt_auth_e2e gate em Phase 4 |
| 8 | SKILL.md anuncia 12 journeys fake | T3.1 | Reescrever para 7 journeys reais + CLI-First |
| 9 | Orchestrator usa curl probes, não CLI | T3.2 | Rewrite com `$LOOPZERO` invocations |
| 10 | Sem Prova de Vida pós-deploy | T3.2 | kubectl pod Running 1/1 check |
| 11 | Sem Prova de Morte pós-delete | T3.2 | kubectl NotFound + ArgoCD pruned |
| 12 | Sem evidência REAL E2E | T4.1 | LIVE drill com captura (pod, DB, HTTPS) |
| 13 | Trio nunca foi validado honestamente | T4.2 | LIVE 3-skill sequence + teardown clean |

**Coverage: 13/13 (100%)**

## Global Definition of Done

- [ ] All phases (0-4) completed
- [ ] Bug #24 workaround removido — zero `if [[ "$actual" == "401" ]]` em drill orchestrators
- [ ] JWT 401 root cause documented em finding doc + fixed em config
- [ ] dogfood-drill mirror /dogfood — CLI-First Inviolável
- [ ] log.md corrected (append-only, citing user catch verbatim)
- [ ] LIVE drill run REAL capturou: pod Running 1/1 + build SUCCEEDED em DB + HTTPS 200
- [ ] Prova de Vida + Morte validadas em LIVE run
- [ ] Teardown CLEAN ao final (zero resíduos DOKS/PG/Spaces/VPC)
- [ ] All Go tests passing (`task test`)
- [ ] All hermetic shell tests passing (`bash infra/tests/drill-*-orchestrator_test.sh`, `bash infra/tests/drill-helmfile-bootstrap_test.sh`)
- [ ] Zero lint warnings introduzidos (`shellcheck` se disponível, `task lint`)
- [ ] Zero internal terms violação (Rule 3): drill orchestrator output respeita product language
- [ ] **`/cross-validation 2026-05-19-drill-skills-rigor-restoration` APROVADO** — zero CRITICO, zero GRAVE
- [ ] **Evidence (CONDITIONAL — plan é maturity-claim-affecting):**
  - [ ] Audit doc `.claude/knowledge-base/audits/2026-05-19-drill-skills-rigor-restoration-evidence.md` com 3 evidence sources:
    - T1 (commit + test) — Bug #24 removal commit + hermetic tests GREEN
    - T2 (LIVE log) — drill-jwt-diagnose output + curl /projects 200 capture
    - T3 (LIVE final) — full trio LIVE PASS with kubectl outputs
- [ ] **`/dogfood-drill --confirm-cost` PASSED** (em vez de `/dogfood full` — esta é a versão drill)
- [ ] Trio: cleanroom-drill + dogfood-drill + chaos-test-drill PASS LIVE
- [ ] CHANGELOG.md atualizado em `[Unreleased]` com entries `Fixed` + `Changed`

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> Este plano corrige `dogfood-drill`. Portanto, o Dogfood Gate aqui é a própria `dogfood-drill` REVISADA rodando LIVE com expectativas REAIS — não a versão original mentirosa.

### Execution

```bash
# 1. Provisionar drill stack (cleanroom-drill já valida bootstrap)
LOOPZERO_DRILL_SKILL_SPONSOR=phn bash infra/scripts/drill-cleanroom-orchestrator.sh --confirm-cost --keep-stack

# 2. Validar JWT auth gate (T2.2)
bash infra/scripts/drill-jwt-diagnose.sh
# expected: status=200 ou 201 em request.txt

# 3. Rodar dogfood-drill REVISADO (com $LOOPZERO CLI real)
LOOPZERO_DRILL_SKILL_SPONSOR=phn bash infra/scripts/drill-dogfood-orchestrator.sh --confirm-cost --skip-cleanroom

# 4. Capturar evidências pós-deploy J04 (antes de J07 delete)
kubectl --context k8s-drill-workload get pods -A > /tmp/drill-evidence-pods.txt
$LOOPZERO project list --json > /tmp/drill-evidence-projects.json
curl -sf https://demo-app-<tenant>.loopzero-drill.example/health -w "%{http_code}" > /tmp/drill-evidence-https.txt

# 5. MANDATORY teardown
bash infra/scripts/drill-teardown.sh --validate
```

### Pass Criteria

- [ ] `dogfood-drill` PASS com expectativas REAIS (não 401-as-PASS)
- [ ] 6/7 OU 7/7 journeys PASS (threshold = max 1 failure)
- [ ] Pod Running 1/1 em workload cluster (capture kubectl output)
- [ ] Build SUCCEEDED em mgmt PG (capture SQL output)
- [ ] App HTTPS 200 com body válido (capture curl output)
- [ ] Project delete + Prova de Morte (capture kubectl NotFound)
- [ ] ArgoCD app pruned ao final
- [ ] Zero S0 findings; ≤1 S1 findings totais
- [ ] Teardown CLEAN (zero recursos drill residuais)

### On Failure

Se `dogfood-drill` REVISADO encontrar bugs reais (deploy fails, scale fails, etc.):
1. Bug é REAL — abrir issue + Phase 1.X investigation
2. NÃO adicionar workaround para mascarar
3. NÃO emitir `<promise>TODAS AS TASKS, CRITERIOS DE ACEITES, DODs CONCLUIDOS E VALIDADOS</promise>` enquanto bug existir
4. Re-rodar `dogfood-drill` em drill fresh stack após fix
5. Repetir até zero S0/S1 OU bug classificado como T2/T3 com deferral ADR

**Honestidade > números bonitos.** Princípio §3 Inquebrável.

---

## Notes on Architecture Docs (Mode 2 + Mode 3)

Per `/to-plan` skill mandate, `/architecture-docs` deveria rodar pre-plan (AS-IS) e post-plan (PROPOSED). Para este plano:

- **Mode 2 (AS-IS):** Domínios afetados — `auth` (JWT validator), `infra` (drill bootstrap), `operations` (skill orchestrators). Skip explícito justificado: `.claude/rules/auto-docs.md` §3 anti-thrashing — max 3 docs/task. Este plano já produz ≥3 docs (finding-2026-05-19-jwt-drill-401, evidence-2026-05-19-drill-skills-rigor-restoration, log.md correction entry). Adicionar architecture diffs seria thrashing.
- **Mode 3 (PROPOSED):** Idem skip. Mudanças não alteram C4 boundaries; apenas corrigem JWT wiring + skill contract. Diagramas existentes (`docs/architecture/auth/`, `docs/architecture/multi-cell/`) permanecem corretos.

Se durante execução um diagrama precisar update, abrir ADR de amendment per `.claude/rules/pre-release-governance.md` § Plan Amendment Policy.

## Notes on Edge Case Review

Per `/to-plan` skill mandate, `/edge-case-plan` deve rodar após escrita do plano. Próxima ação do operator: invocar `/edge-case-plan 2026-05-19-drill-skills-rigor-restoration` para análise de edge cases pragmáticos. MUST FIX items, se houver, são incorporados como sub-tasks antes de implementation iniciar.
