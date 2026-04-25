# CHG-2026-04-25-PR-GUARDRAIL — Executable Plan

> **Status:** DRAFT — pending T2 governance meeting + 3 explicit approvals
> **Owner:** TBD (assigned post-meeting)
> **Target completion:** ~6-8 weeks of work (vide §4 Fases)
> **Tier:** **T2** (CI/CD impact, governance gate, contract de qualidade pre-release)
> **Decision records:** §3 deste documento (ADRs inline). Promover para `docs/decisions/` se T2 meeting solicitar.

---

## 0. Goal

Construir um **agente guardrail local** que dispara automaticamente quando um PR `develop → main` é aberto/atualizado, executando:

1. **Deep review** estruturado do diff (segurança, contratos, regras CLAUDE.md, regressão arquitetural).
2. **Suite E2E real em k3d** (3 clusters reais, mesma stack Helm de produção).
3. **Bateria completa dos 35 comandos do `loopzero` CLI** contra a stack k3d viva (cobertura escrita pelo próprio projeto).
4. **Veredito vinculante**: comentário no PR + commit status check (required) + bloqueio de merge se falhar.

A intenção é eliminar regressões antes de chegarem em `main` — em pre-release, `main` é candidata a release.

### Non-goals

- **Rodar em GitHub Actions na nuvem.** O agente roda nesta máquina (Linux 6.8 paulo@loopzero). Cloud é alternativa futura, não escopo.
- **Cobrir PRs `feature → develop`.** Apenas `develop → main`. Volume previsto: 1-3 PRs/semana.
- **Substituir CI atual** (`.github/workflows/ci.yml`). O guardrail é gate adicional, não substituto.
- **Testes em produção real DigitalOcean.** Custo proibitivo por PR. k3d local é o teto.
- **Regression suite para versões antigas** (`v1.x` etc.). Apenas `develop → main` no estado atual.
- **Análise de performance / load test.** Já coberto por `infra-load-test.yml`. Fora do guardrail.

---

## 1. Decisões (registro consolidado)

| # | Decisão | Status | Justificativa | Aprovador |
|---|---|---|---|---|
| D-1 | **Trigger:** polling cron a cada 5 min via `gh pr list` | ✅ APROVADO | KISS, sem endpoint público, robusto a reboot (cron é systemd) | usuário |
| D-2 | **Output:** comentário no PR + commit status check (required) | ✅ APROVADO | Visibilidade humana + bloqueio mecânico | usuário |
| D-3 | **Cobertura inicial:** projeto inclui escrever E2E para os 35 comandos do CLI antes de virar required check | ✅ APROVADO | Required check sem cobertura é teatro de segurança | usuário |
| D-4 | **k3d strategy:** ⚠️ DEFAULT PROPOSTO — cluster fresco em Docker dedicado isolado por PR (variante A') | ⏳ AGUARDA APROVAÇÃO | Critério "mais próximo de produção" exige reset zero-state. Docker isolado evita colisão com k3d de dev local | TBD |
| D-5 | **Política offline:** bloqueia merge até guardrail rodar (status check obrigatório) | ✅ APROVADO | Gate é vinculante por design. Single point of failure aceito por ora | usuário |
| D-6 | **Modelo Claude:** ⚠️ DEFAULT PROPOSTO — híbrido por fase (Opus 4.7 para review/análise; Sonnet 4.6 para execução de testes/relatório) | ⏳ AGUARDA APROVAÇÃO | Custo/qualidade ótimo: ~$5-15/PR vs $30-50 com Opus puro | TBD |
| D-7 | **Override de bypass:** ⚠️ DEFAULT PROPOSTO — apenas CODEOWNERS de `main` podem bypass via label `guardrail/bypass` + comentário com justificativa, registrado em audit log | ⏳ AGUARDA APROVAÇÃO | Single-machine SPOF exige escape hatch auditável | TBD |

**Gate de implementação:** D-4, D-6 e D-7 precisam ser explicitamente aprovados pelo usuário (ou substituídos por escolhas alternativas) antes de Fase 0 começar. Plano não autoriza código até três ✅.

---

## 2. Pre-conditions (verificar antes da Fase 0)

| # | Pre-condition | Como verificar |
|---|---|---|
| P-1 | T2 governance meeting concluído com APPROVED | `docs/meetings/records/MEET-YYYY-MM-DD-NNN.md` existe |
| P-2 | D-4, D-6, D-7 explicitamente aprovados | Tabela §1 sem ⏳ |
| P-3 | `gh` CLI autenticado com escopo `repo` + `workflow` | `gh auth status` mostra ambos |
| P-4 | Branch protection em `main` permite adicionar required status check `loopzero-pr-guardrail` | `gh api repos/:owner/:repo/branches/main/protection` |
| P-5 | Working tree limpo na branch `develop` no momento do start | `git status --short` vazio |
| P-6 | Espaço em disco ≥ 50 GB livre (k3d + imagens build acumulam) | `df -h /` |
| P-7 | `claude` CLI no PATH e autenticado (ou Anthropic API key disponível para modo headless) | `which claude && claude --version` |

Se qualquer P-* falhar: STOP. Plano não autoriza começar.

---

## 3. ADRs inline (decisões T2)

### ADR-PG-001 — Polling cron como mecanismo de trigger

**Contexto:** GitHub não pode chamar diretamente um CLI local. Três opções: polling, webhook+tunnel, sessão persistente.

**Decisão:** polling via systemd timer a cada 5 min, executando `gh pr list --base main --head develop --state open --json` e disparando `claude -p` quando detectar PR não-processado.

**Alternativas rejeitadas:**
- **Webhook + CF Tunnel.** Real-time, mas adiciona endpoint público nesta máquina (superfície de ataque), exige rotação de webhook secret, e duplica complexidade do CF Tunnel já existente para produção.
- **Sessão Claude persistente com `CronCreate`.** Morre se você fechar o terminal ou rebootar. Frágil.

**Trade-offs aceitos:** latência média 2.5 min (5min/2). Em pre-release com 1-3 PRs/sem, irrelevante. Migrar para webhook é trivial depois (mesmo prompt, mesmo invocador).

**Reversibilidade:** ALTA. Trocar cron por webhook = trocar trigger script, manter resto.

---

### ADR-PG-002 — k3d em Docker isolado por PR (variante A') — ⚠️ AGUARDA APROVAÇÃO

**Contexto:** "Mais próximo de produção" exige zero-state. k3d local de dev pode estar contaminado pelo seu trabalho diário.

**Decisão proposta:** cada execução do guardrail cria 3 clusters k3d **dentro de um Docker container dedicado** (`loopzero-guardrail-runner`), com seu próprio Docker daemon (`dind` ou Docker socket isolado), montando os Helm charts do PR e rodando setup completo (`task setup`).

**Alternativas:**
- **(A) k3d direto na máquina, fresco.** Mais simples. Risco: colide com k3d de dev (`task dev:up`). Você não consegue trabalhar em outro PR enquanto guardrail roda.
- **(B) k3d warm reutilizado.** Rápido. Mascarra regressões — descartado pelo critério #4.
- **(C) VM Pulumi efêmera DigitalOcean.** Próximo de prod, mas $5/PR + 10 min de provision. Caro para 1-3 PRs/sem que pode crescer.

**Trade-offs aceitos:** ~10-15 min de overhead de setup por PR. Disco/CPU pesados durante run. Aceito porque PRs `develop → main` são raros.

**Reversibilidade:** MÉDIA. Mudar para (A) puro = remover camada Docker. Mudar para (C) = projeto separado.

---

### ADR-PG-003 — Modelo híbrido Opus + Sonnet — ⚠️ AGUARDA APROVAÇÃO

**Contexto:** Opus 4.7 é o modelo mais capaz mas custa ~5x mais que Sonnet 4.6.

**Decisão proposta:**
| Fase do guardrail | Modelo | Razão |
|---|---|---|
| Deep review do diff | Opus 4.7 | Detecção de regressão sutil, decisões arquiteturais |
| Setup k3d + execução de testes | Sonnet 4.6 | Tool calls intensivo, baixa criatividade |
| Análise de root cause de falhas | Opus 4.7 | Debug profundo |
| Geração do relatório PR | Sonnet 4.6 | Estruturado, baseado em outputs |

**Alternativas:**
- **Opus puro.** $30-50/PR. Mais profundo em todas as fases. Justificável se cobertura de bugs > custo.
- **Sonnet puro.** $3-5/PR. Pode perder regressão sutil em PRs grandes.
- **Haiku puro.** Não recomendado — guardrail é o último gate.

**Trade-offs aceitos:** complexidade de orquestração (script precisa saber qual modelo invocar quando).

**Reversibilidade:** ALTA. Mudar é trocar 1 flag por fase no script de invocação.

---

### ADR-PG-004 — Override CODEOWNERS via label — ⚠️ AGUARDA APROVAÇÃO

**Contexto:** D-5 bloqueia merge se guardrail não rodou. Máquina offline = release travado. Precisa escape hatch.

**Decisão proposta:**
- Label `guardrail/bypass` em PR força status check para SUCCESS
- Apenas usuários listados em `.github/CODEOWNERS` para `main` podem aplicar (validado por GitHub Actions auxiliar)
- Comentário no PR é obrigatório com justificativa estruturada (template)
- Toda aplicação de bypass é registrada em `docs/audits/guardrail-bypass-log.md`

**Alternativas:**
- **Sem bypass.** Inviável — uma falha de máquina trava a empresa.
- **Bypass por qualquer um.** Trivializa o gate.
- **Bypass com expiração automática (24h).** Complexo, fora do escopo inicial.

**Trade-offs aceitos:** confia em CODEOWNERS para autorização. Auditoria post-hoc, não real-time.

**Reversibilidade:** ALTA. Remover label é trivial.

---

### ADR-PG-005 — Cobertura E2E primeiro, gate required depois

**Contexto:** Tornar o guardrail required check com cobertura incompleta dos 35 comandos CLI seria teatro — falsos positivos minariam confiança e bypasses viraria default.

**Decisão:** o guardrail entra em produção em **modo advisor** (apenas comenta, não bloqueia) até que a matriz de cobertura E2E (`pr-guardrail-coverage-matrix.md`) esteja 100% verde. Apenas então o status check vira required.

**Reversibilidade:** ALTA. Flipar uma config no GitHub branch protection.

---

### ADR-PG-006 — Estado por PR em arquivo local

**Contexto:** O cron pode disparar várias vezes para o mesmo PR. Precisa idempotência.

**Decisão:** estado por PR persiste em `.claude/pr-guardrail/state/PR-<number>.json` com:
- `pr_number`, `pr_head_sha`, `started_at`, `finished_at`, `verdict`, `attempts`
- Cron compara `pr_head_sha` atual com o registrado. Se igual e verdict existe, pula.
- Se diferente, dispara nova execução (commit novo no PR re-trigger).

**Trade-offs:** estado é local — perde em reboot do disco. Aceitável (próxima rodada de cron reconcilia).

---

## 4. Phases (executável)

Cada fase tem **Tasks**, **Acceptance Criteria (AC)** e **DoD** com comandos mecânicos. Não avança até DoD passar.

### Phase 0 — Discovery & viabilidade (1-2 sessões)

**Objetivo:** confirmar que o que está planejado é tecnicamente viável. Output: relatório + matriz de cobertura inicial.

**Tasks:**

- T-0.1 — Inventariar `cli/src/commands/` e produzir lista exaustiva dos comandos CLI (esperado: ~35).
- T-0.2 — Mapear `infra/tests/e2e/` por comando: marcar cada um como `COVERED` / `PARTIAL` / `MISSING` na matriz `docs/plans/CHG-2026-04-25-PR-GUARDRAIL-coverage-matrix.md`.
- T-0.3 — Medir tempo wall-clock real de `task setup` + `task dev:up` + `task test:e2e` em estado limpo (3 execuções, mediana).
- T-0.4 — Validar `claude -p "<prompt>"` em modo headless: confirmar que tools `Agent`, `Bash`, `Read`, `Write`, `Edit`, `Grep` estão disponíveis e funcionam sem TTY. Documentar limitações.
- T-0.5 — Validar `gh` CLI: scopes `repo`, `pull-request:write`, `statuses:write` funcionam com token atual.
- T-0.6 — Avaliar plugin `review-deep-loop`: descobrir se cobre os critérios de review necessários (regras CLAUDE.md, contratos, locks, exit codes, internal terms). Output: gap list.
- T-0.7 — Verificar disco/CPU/RAM disponíveis nesta máquina sob carga simultânea de k3d guardrail + Claude session ativa.

**Acceptance Criteria:**

- AC-0.1: lista de 35 comandos confirmada (ou número correto descoberto e documentado).
- AC-0.2: matriz preenchida com status por comando + caminho do test file existente (quando houver).
- AC-0.3: tempo total estimado de E2E full registrado (T-0.3) com erro de margem ≤ 20%.
- AC-0.4: prova-de-conceito: `claude -p "list files in /tmp"` em headless completou e retornou output esperado.
- AC-0.5: gap list do `review-deep-loop` documentado: o que cobre, o que falta cobrir.

**DoD (Phase 0):**

```bash
# Matriz preenchida (não-vazia, todos os comandos categorizados)
test -s /home/paulo/loopzero/docs/plans/CHG-2026-04-25-PR-GUARDRAIL-coverage-matrix.md && \
! grep -q "TBD - Phase 0" /home/paulo/loopzero/docs/plans/CHG-2026-04-25-PR-GUARDRAIL-coverage-matrix.md

# Headless Claude funciona
claude --help | grep -q -- '-p\|--print'

# gh tem scopes necessários
gh auth status 2>&1 | grep -E "repo|workflow"

# Espaço suficiente
[ "$(df -BG / | awk 'NR==2{print $4}' | tr -d 'G')" -ge 50 ]
```

**Saída esperada:** revisão dos números reais pode invalidar D-2/D-4/D-5. Se acontecer, voltar a §1 antes de Fase 1.

---

### Phase 1 — Watcher + state management (1 sessão)

**Objetivo:** detectar PRs e disparar ações idempotentes. Sem rodar review/testes ainda.

**Tasks:**

- T-1.1 — Criar `scripts/pr-guardrail/watch.sh`: chama `gh pr list --base main --head develop --state open --json number,headRefOid,updatedAt`, compara contra estado em `.claude/pr-guardrail/state/`, decide ação.
- T-1.2 — Criar `scripts/pr-guardrail/state.sh`: helpers para ler/escrever JSON de estado por PR (PR-<n>.json).
- T-1.3 — Criar systemd timer `loopzero-pr-guardrail.timer` (5 min) + service `loopzero-pr-guardrail.service`. Documentar como instalar via `systemctl --user enable`.
- T-1.4 — Lock global: `flock` em `/tmp/loopzero-pr-guardrail.lock` para evitar runs simultâneos.
- T-1.5 — Logs estruturados: cada run produz log JSON em `.claude/pr-guardrail/logs/run-<timestamp>-PR-<n>.jsonl`.
- T-1.6 — Mecanismo de "smoke run" — modo `--dry-run` que detecta PR e loga, sem disparar review.

**Acceptance Criteria:**

- AC-1.1: dado um PR fake `develop → main` aberto, watch.sh detecta e cria `state/PR-<n>.json` em ≤ 3s.
- AC-1.2: rodar watch.sh duas vezes seguidas com mesmo `head_sha` resulta em apenas 1 entry de estado (idempotência).
- AC-1.3: rodar 3 watchers simultâneos: 1 progride, 2 esperam pelo lock e saem com exit 0 (não erro).
- AC-1.4: `systemctl --user status loopzero-pr-guardrail.timer` retorna `active (waiting)`.
- AC-1.5: log JSON contém `pr_number`, `head_sha`, `decision` (skip/trigger), `duration_ms`.

**DoD (Phase 1):**

```bash
# Smoke test contra repo real (PR fake numerado 9999 manualmente colocado em state)
bash /home/paulo/loopzero/scripts/pr-guardrail/watch.sh --dry-run | tee /tmp/pr-guard.log
grep -q '"decision":"skip"\|"decision":"trigger"' /tmp/pr-guard.log

# Lock funciona
( bash /home/paulo/loopzero/scripts/pr-guardrail/watch.sh --dry-run & \
  bash /home/paulo/loopzero/scripts/pr-guardrail/watch.sh --dry-run & \
  wait )
# Ambos devem terminar com exit 0; apenas 1 produz log de execução

# Timer está agendado
systemctl --user list-timers | grep loopzero-pr-guardrail
```

---

### Phase 2 — Headless Claude integration (1-2 sessões)

**Objetivo:** o watcher consegue invocar `claude -p` com o prompt do guardrail, capturar output, e reportar.

**Tasks:**

- T-2.1 — Criar `scripts/pr-guardrail/prompts/master.md` — prompt master que recebe PR number + head SHA + diff e produz veredito JSON.
- T-2.2 — Criar `scripts/pr-guardrail/trigger.sh` que monta o prompt completo (prompt master + contexto do PR) e chama `claude -p --output-format json`.
- T-2.3 — Definir contrato JSON de saída do guardrail: `verdict` (`pass`/`fail`/`error`), `phases` (array com result por fase), `findings` (severidade), `evidence_paths`.
- T-2.4 — Validar que prompt + tools necessárias funciona em headless: `claude -p` precisa ter acesso a `Bash`, `Read`, `Write`, `Edit`, `Grep`, `Agent` para executar review e tests.
- T-2.5 — Timeout configurável (default 3h). Se exceder, kill + reportar erro estruturado.
- T-2.6 — Capturar exit code da Claude session e mapear: 0 → continua; ≠0 → falha o run com erro registrado.

**Acceptance Criteria:**

- AC-2.1: `bash trigger.sh --pr <fake>` invoca claude headless e retorna JSON estruturado em ≤ timeout.
- AC-2.2: prompt master é versionado em git (não hardcoded em script).
- AC-2.3: stdout/stderr da claude são capturados em `.claude/pr-guardrail/logs/run-<ts>-claude-stdout.log`.
- AC-2.4: timeout dispara SIGTERM → SIGKILL + cleanup de processos filhos (k3d daemons).

**DoD (Phase 2):**

```bash
# Prompt versionado
test -s /home/paulo/loopzero/scripts/pr-guardrail/prompts/master.md

# Trigger smoke
bash /home/paulo/loopzero/scripts/pr-guardrail/trigger.sh --pr-fake --no-execute > /tmp/trigger.json
jq -e '.verdict and .phases and .findings' /tmp/trigger.json

# Timeout testável (mock claude com sleep > timeout)
LOOPZERO_GUARD_FAKE_CLAUDE=1 LOOPZERO_GUARD_TIMEOUT=5 bash /home/paulo/loopzero/scripts/pr-guardrail/trigger.sh --pr-fake | \
  jq -e '.verdict == "error" and .error_kind == "timeout"'
```

---

### Phase 3 — k3d harness em Docker isolado (2-3 sessões) — depende de D-4 aprovado

**Objetivo:** ambiente k3d limpo, isolado, reprodutível, descartável.

**Tasks:**

- T-3.1 — Criar imagem Docker `loopzero/guardrail-runner:dev` com k3d, kubectl, helm, task, docker-cli pré-instalados.
- T-3.2 — Script `scripts/pr-guardrail/k3d-up.sh`: roda dentro do container, executa `task setup` + `task dev:up` + aguarda Tilt converge.
- T-3.3 — Script `scripts/pr-guardrail/k3d-down.sh`: derruba clusters, remove volumes, limpa imagens cacheadas.
- T-3.4 — Volume montado para resultados: `<workspace>/.guardrail/run-<ts>/` recebe outputs persistentes.
- T-3.5 — Health check pré-suite: `loopzero whoami` contra a stack subida funciona; `task doctor` retorna verde.
- T-3.6 — Rede: container precisa de `host.docker.internal` ou similar para a Claude session ler estado e logs (decidir se usa bind mount ou syscalls).
- T-3.7 — Métricas de runtime: tempo de up, tempo de teste, tempo de down — tudo logado.

**Acceptance Criteria:**

- AC-3.1: `bash k3d-up.sh` num runner novo termina com `kubectl get nodes` mostrando 3 clusters Ready em ≤ 15 min.
- AC-3.2: `task doctor` dentro do container retorna 0.
- AC-3.3: `bash k3d-down.sh` deixa zero containers k3d running, zero volumes órfãos.
- AC-3.4: rodar k3d-up.sh enquanto `task dev:up` de dev local está ativo NÃO interfere — k3d names diferentes, Docker network distinta.
- AC-3.5: imagens build cacheadas em volume separado para acelerar runs subsequentes.

**DoD (Phase 3):**

```bash
# Imagem buildada
docker image inspect loopzero/guardrail-runner:dev > /dev/null

# Up + smoke + down em 30 min total
time (
  bash scripts/pr-guardrail/k3d-up.sh --runner-name guard-test && \
  docker exec guard-test task doctor && \
  docker exec guard-test loopzero whoami && \
  bash scripts/pr-guardrail/k3d-down.sh --runner-name guard-test
)

# Sem leak
docker ps -a | grep -E "k3d-|guard-" || echo "clean"
```

---

### Phase 4 — Deep review pipeline (2-3 sessões)

**Objetivo:** primeira fase do guardrail (review) funciona end-to-end e produz findings estruturados.

**Tasks:**

- T-4.1 — Definir checklist de review canônico em `scripts/pr-guardrail/prompts/review-checklist.md`: regras CLAUDE.md (1-10), contratos (--json schema, exit codes), state machine integrity, internal terms, advisory lock usage, idempotency middleware, error handling.
- T-4.2 — Wire `review-deep-loop` plugin como sub-agent quando review detectar área crítica (security, schema, controller). Se gap list de Fase 0 mostrou cobertura suficiente.
- T-4.3 — Mapeamento severidade: `critical` (bloqueia), `major` (bloqueia), `minor` (warn no comment), `info` (FYI).
- T-4.4 — Output: `findings.json` por fase, agregado em `verdict.json` final.
- T-4.5 — Falsos positivos: prompt instrui "se incerto, classifique como `info`, nunca `critical`". Calibragem em Fase 7.
- T-4.6 — Performance: review limitado a diff (não scanear repo todo). Para PRs > 5000 LOC, lista arquivos e prioriza por risco.

**Acceptance Criteria:**

- AC-4.1: dado PR sintético com violação clara (ex: `errors.New("...")` em vez de LoopZeroError), review marca `major` finding.
- AC-4.2: dado PR sintético sem violações, review retorna 0 findings críticos.
- AC-4.3: review completa em ≤ 10 min para PR de 1000 LOC.
- AC-4.4: `findings.json` valida contra JSON schema versionado em `scripts/pr-guardrail/schemas/findings.schema.json`.

**DoD (Phase 4):**

```bash
# Review contra branch atual da develop como smoke
bash scripts/pr-guardrail/run-review.sh --pr-mock --base main --head develop > /tmp/review.json
jq -e '.findings | type == "array"' /tmp/review.json
jq -e '.duration_seconds < 600' /tmp/review.json
```

---

### Phase 5 — 35 comandos CLI E2E suite (3-5 sessões — fase mais longa)

**Objetivo:** cada um dos 35 comandos do CLI tem ao menos 1 cenário E2E real exercitado em k3d.

> **Nota:** esta fase tem suas próprias sub-DoDs por grupo de comando. Não é uma fase única — é um trabalho **incremental por comando**. A matriz `pr-guardrail-coverage-matrix.md` é o tracker.

**Tasks (organizadas por grupo de comando):**

- T-5.1 — **Auth (3 comandos):** `login`, `logout`, `whoami` → cobertura E2E confirmada (existe parcial em `infra/tests/e2e/auth/`).
- T-5.2 — **Tenant lifecycle (5):** `init`, `team create/list/invite/remove`, `project create/list/delete`.
- T-5.3 — **Build/Deploy (4):** `deploy`, `build status/list/cancel`.
- T-5.4 — **Env / Secret (5):** `env list/set/unset`, `secret list/set/unset`.
- T-5.5 — **Promote / Rollback (3):** `promote`, `rollback`, `release list`.
- T-5.6 — **Scale (2):** `scale`, `scale --json`.
- T-5.7 — **Logs / Status / Inspect (3):** `logs`, `status`, `inspect`.
- T-5.8 — **Domain (4):** `domain add/list/verify/remove`.
- T-5.9 — **Preview (3):** `preview create/list/destroy`. **NOTA:** Sprint 5 ainda não está em produção. Marcar como SKIP no advisor mode até backend estar pronto.
- T-5.10 — **Audit (1):** `audit`.
- T-5.11 — **Outros (~3):** `doctor`, `version`, `help` — cobertura mínima (`exit 0`, JSON shape).

**Padrão por teste E2E (template):**

```go
// infra/tests/e2e/cli/<command>_test.go (Ginkgo)
var _ = Describe("loopzero <command>", func() {
  It("succeeds in happy path", ...)
  It("fails with exit code N when <preconditions violated>", ...)
  It("produces --json matching schema §0.3", ...)
  It("does not leak internal terms", ...) // Rule 3
  It("sets correct exit code", ...) // Rule 4
})
```

**Acceptance Criteria:**

- AC-5.1: matriz `pr-guardrail-coverage-matrix.md` com 100% das linhas em status `COVERED` (exceto comandos legitimamente bloqueados, ex: `preview` se Sprint 5 não pronto).
- AC-5.2: cada teste roda standalone em < 90s (média).
- AC-5.3: suite completa termina em < 90 min total.
- AC-5.4: cada teste cobre os 5 contratos: happy path, falha esperada, --json schema, internal terms, exit code.

**DoD (Phase 5):**

```bash
# Matriz 100% coberta (ou items SKIP justificados)
! grep -E "^\| .* \| MISSING \|" docs/plans/CHG-2026-04-25-PR-GUARDRAIL-coverage-matrix.md

# Suite completa passa em runner isolado
docker exec guard-test bash -c "cd /workspace && task test:e2e:cli" \
  && echo "ALL CLI E2E PASS"

# Tempo total
time docker exec guard-test bash -c "cd /workspace && task test:e2e:cli" 2>&1 | tee /tmp/cli-e2e.log
grep -E "real\s+([0-8][0-9]|[0-9])m" /tmp/cli-e2e.log
```

---

### Phase 6 — Report + status check (1-2 sessões)

**Objetivo:** o veredito chega visivelmente no PR.

**Tasks:**

- T-6.1 — `scripts/pr-guardrail/report.sh`: lê `verdict.json`, monta markdown rico com seções (review findings, test results, evidence links).
- T-6.2 — `gh pr comment <pr> --body-file <generated.md>` posta no PR.
- T-6.3 — `gh api -X POST /repos/:owner/:repo/statuses/:sha` cria status check com nome `loopzero-pr-guardrail`, state `success`/`failure`, target_url para artifacts.
- T-6.4 — Persistir artifacts em `.guardrail/run-<ts>/` e linkar no comment (paths locais; subir para Spaces opcional).
- T-6.5 — Atualização incremental: se PR recebe novo commit, comment é editado (não duplicado) via `gh pr comment --edit-last`.

**Acceptance Criteria:**

- AC-6.1: PR de teste recebe comment com seções obrigatórias (Verdict, Review, Tests, Coverage, Evidence).
- AC-6.2: status check aparece como required (em modo gate) ou pendente (advisor).
- AC-6.3: re-run em mesmo PR atualiza, não duplica.

**DoD (Phase 6):**

```bash
# Test contra PR sintético
gh pr create --base main --head develop --title "guardrail-smoke" --body "test" -l guardrail/smoke
GUARD_PR=$(gh pr list --label guardrail/smoke --json number -q '.[0].number')
bash scripts/pr-guardrail/run-full.sh --pr "$GUARD_PR"
gh pr view "$GUARD_PR" --json comments | jq -e '.comments[] | select(.author.login == "<your-bot>")'
gh api repos/:owner/:repo/statuses/$(gh pr view "$GUARD_PR" --json headRefOid -q .headRefOid) | \
  jq -e '.[] | select(.context == "loopzero-pr-guardrail")'
gh pr close "$GUARD_PR" --delete-branch
```

---

### Phase 7 — Hardening + override (1-2 sessões) — depende de D-7 aprovado

**Objetivo:** modo gate seguro com escape hatch auditável.

**Tasks:**

- T-7.1 — Workflow auxiliar `.github/workflows/guardrail-bypass.yml`: trigger em label `guardrail/bypass`. Valida que actor é CODEOWNER de `main`. Se sim, força status para success com context `loopzero-pr-guardrail (bypassed)`. Se não, remove label e comenta erro.
- T-7.2 — Template de comentário obrigatório no bypass (`.github/PULL_REQUEST_TEMPLATE/bypass.md`).
- T-7.3 — Audit log: cada bypass append em `docs/audits/guardrail-bypass-log.md` (commit no próprio PR como evidência).
- T-7.4 — Recovery automático: se Claude session morre mid-run, watch.sh detecta state stuck > 4h e marca como `error` + comenta no PR pedindo retrigger manual.
- T-7.5 — Métricas locais: contador de runs/successes/failures/bypasses em `.claude/pr-guardrail/metrics.jsonl` para análise.
- T-7.6 — Calibragem de falsos positivos: rodar contra últimos 10 PRs já mergeados em `main`, esperar 0 críticos.

**Acceptance Criteria:**

- AC-7.1: bypass por não-CODEOWNER falha; por CODEOWNER funciona; ambos auditados.
- AC-7.2: simulação de crash mid-run (kill -9 claude) → cron detecta em ≤ 5min + comenta no PR.
- AC-7.3: calibragem contra histórico: ≤ 1 falso positivo crítico em 10 PRs.

**DoD (Phase 7):**

```bash
# Bypass workflow lint
gh workflow view guardrail-bypass.yml --ref develop

# Audit log existe e tem schema
test -f docs/audits/guardrail-bypass-log.md
head -1 docs/audits/guardrail-bypass-log.md | grep -q "Guardrail Bypass Log"

# Calibração: rodar contra últimos 10 PRs merged em main
for sha in $(git log main --merges -10 --format=%H); do
  bash scripts/pr-guardrail/run-review.sh --commit "$sha"
done | jq -s 'map(.findings | map(select(.severity == "critical"))) | flatten | length' | \
  awk '{ exit ($1 > 1 ? 1 : 0) }'
```

---

### Phase 8 — Beta (advisor mode) + promotion to required (1 semana wall-clock + 1 sessão)

**Objetivo:** rodar em PRs reais por 1 semana em modo advisor, ajustar, depois ligar gate.

**Tasks:**

- T-8.1 — Configurar status check como **NOT required** em `main` branch protection.
- T-8.2 — Deixar guardrail rodar em todos os PRs `develop → main` da semana.
- T-8.3 — Para cada PR: medir falsos positivos / falsos negativos contra revisão humana.
- T-8.4 — Ajustar prompt master / checklist baseado em achados.
- T-8.5 — Após 5 PRs consecutivos com 0 falsos críticos: flipar para required.
- T-8.6 — Documentar runbook em `docs/runbooks/pr-guardrail.md` (operação, debug, atualização de prompt).

**Acceptance Criteria:**

- AC-8.1: 5 PRs consecutivos com guardrail rodando em advisor sem falso positivo crítico.
- AC-8.2: 0 PRs em que guardrail passou e revisão humana achou regressão crítica (falso negativo).
- AC-8.3: status check virou required em branch protection.
- AC-8.4: runbook completo com seções: arquitetura, troubleshooting, atualizar prompt, atualizar testes, override emergencial, métricas.

**DoD (Phase 8):**

```bash
# Branch protection com required
gh api repos/:owner/:repo/branches/main/protection \
  | jq -e '.required_status_checks.contexts | index("loopzero-pr-guardrail")'

# Runbook existe e é navegável
test -s docs/runbooks/pr-guardrail.md
grep -E "^## (Troubleshooting|Operação|Override|Métricas)" docs/runbooks/pr-guardrail.md | wc -l | \
  awk '{ exit ($1 >= 4 ? 0 : 1) }'

# Métricas de validação
jq -s 'map(select(.verdict == "pass")) | length' .claude/pr-guardrail/metrics.jsonl | \
  awk '{ exit ($1 >= 5 ? 0 : 1) }'
```

---

## 5. Risk register

| ID | Risco | Probabilidade | Impacto | Mitigação |
|---|---|---|---|---|
| R-1 | Máquina offline trava release | MÉDIA | ALTO | ADR-PG-004 bypass CODEOWNERS |
| R-2 | k3d Docker isolado consome 100% recursos durante run | ALTA | MÉDIO | Limite via `--cpus`, `--memory` no docker run |
| R-3 | Falso positivo crítico bloqueia merge legítimo | MÉDIA | ALTO | Phase 8 calibragem antes de virar required |
| R-4 | Falso negativo: bug passa pelo guardrail | BAIXA | CRÍTICO | Rodar suite completa, não só smoke; revisão humana ainda obrigatória |
| R-5 | Custos Claude API escalam (Opus puro acidental) | BAIXA | MÉDIO | ADR-PG-003 híbrido + alerta de spend |
| R-6 | Branch protection mal configurada permite merge sem gate | MÉDIA | ALTO | Audit script semanal verifica config |
| R-7 | Cron daemon morre, ninguém percebe | MÉDIA | ALTO | systemd + alert local + healthcheck endpoint |
| R-8 | Estado em `.claude/pr-guardrail/state/` corrompe | BAIXA | MÉDIO | JSON schema validation; reset manual = re-run |
| R-9 | 35 E2E levam > 90 min, gate fica lento demais | MÉDIA | MÉDIO | Paralelizar testes independentes; cache k3d warm pra subsets |
| R-10 | Plugin `review-deep-loop` muda API e quebra integração | BAIXA | MÉDIO | Pinning de versão; cobertura mínima dentro do guardrail próprio |

---

## 6. Done means done — Definition of Done global

O projeto **não está completo** até **TODOS** os items abaixo retornarem 0/pass:

```bash
# Plano fechado
test -s docs/plans/CHG-2026-04-25-PR-GUARDRAIL-plan.md
test -s docs/plans/CHG-2026-04-25-PR-GUARDRAIL-coverage-matrix.md
test -s docs/runbooks/pr-guardrail.md

# Todas as 8 fases com DoD pass (rodar DoD de cada fase em sequência)
for phase in 0 1 2 3 4 5 6 7 8; do
  bash scripts/pr-guardrail/dod-check.sh --phase "$phase" || exit 1
done

# Suite E2E dos 35 comandos verde
docker exec loopzero-guardrail-runner task test:e2e:cli

# Required status check ativo em main
gh api repos/:owner/:repo/branches/main/protection \
  | jq -e '.required_status_checks.contexts | index("loopzero-pr-guardrail")'

# 5 PRs consecutivos passaram em modo gate sem bypass
jq -s '
  sort_by(.timestamp) | reverse | .[0:5] |
  all(.verdict == "pass" and .bypassed == false)
' .claude/pr-guardrail/metrics.jsonl

# Audit log de bypass existe (mesmo se vazio)
test -f docs/audits/guardrail-bypass-log.md
```

Se qualquer comando retornar ≠ 0: **NÃO ESTÁ PRONTO**. Sem cerimônia, sem "praticamente pronto".

---

## 7. Rollout & rollback

**Rollout:**
1. Fase 0-7: trabalho de implementação. Guardrail não impacta nada em produção.
2. Fase 8 advisor: guardrail comenta em PRs, sem bloquear. ~1 semana.
3. Phase 8 required: status check ligado.

**Rollback (se virou required e está bloqueando indevidamente):**
1. Imediato: `gh api` PATCH branch protection removendo `loopzero-pr-guardrail` de required contexts. Tempo: < 1 min.
2. Médio prazo: desabilitar systemd timer (`systemctl --user disable loopzero-pr-guardrail.timer`).
3. Long-term: investigar falso positivo, ajustar prompt, religar gate.

**Reversibilidade total:** ALTA. Nada do guardrail toca código de produção. Tudo é metadado de PR + checks externos.

---

## 8. Open questions (precisam ser resolvidas antes de Fase 0)

1. ⏳ D-4 aprovado? (Docker isolado por PR vs k3d direto)
2. ⏳ D-6 aprovado? (modelo híbrido Opus+Sonnet)
3. ⏳ D-7 aprovado? (override CODEOWNERS via label)
4. ⏳ T2 governance meeting agendado? Quem convoca?
5. ⏳ Runbook tem owner? (em pre-release, default é o usuário paulo, mas precisa estar explícito)

Sem resposta para 1-3: plano fica em DRAFT. Sem 4: implementação não começa.

---

## 9. Trace para regras CLAUDE.md / pre-release-governance

| Regra | Como o plano cumpre |
|---|---|
| 95% confidence | Decisões T2/T3 marcadas ⏳ aguardam aprovação explícita |
| Test before code (TDD) | Phase 5 escreve testes E2E ANTES do guardrail virar required |
| No internal terms (Rule 3) | Phase 4 inclui check explícito de internal terms na review |
| Exit codes (Rule 4) | Phase 5 cobre exit codes 0/1/2/3/4/5 por comando |
| --json contract (Rule 5) | Phase 5 valida schema §0.3 por comando |
| Pre-release governance T2 | T2 meeting é P-1; ADRs registrados §3 |
| Não Reinvente | Reusa `gh`, `claude` CLI, `review-deep-loop`, k3d, task — nada novo |
| KISS | Polling cron + claude headless + docker run = 3 peças, não framework |
| YAGNI | Webhook, multi-machine, AWS — explicitamente fora |
| Honestidade | Riscos e SPOFs documentados §5 e §8 |
