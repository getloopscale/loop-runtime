# Plan: Real PaaS Cleanroom Dogfood Loop

> **Version 1.0** — Plano executável que valida o produto LoopZero end-to-end **contra infraestrutura local destruída e recriada do zero**, executando todas as 12 jornadas de dogfood (`preflight` + `journey-01..11`) e iterando o ciclo `destroy → bring-up → dogfood → fix → restart` até atingir **12/12 GREEN com zero S0/S1**. O plano não introduz código novo de produto — exercita o sistema existente sob condições de regressão limpa, transformando cada falha em fix sistêmico (não workaround) antes de retomar o loop.

## Context

**Estado atual (2026-05-05):**
- Maturity 3.9/5.0. Dogfood 12/12 GREEN documentado em 2026-04-30 (`reference_dogfood_12_12_milestone_2026_04_30`), mas após:
  - **Operators SOTA Phases 0-6 parcial** (4 binários split shipped LOCAL — `tenant-operator`, `app-operator`, `preview-operator`, `platform-operator`)
  - **Cell-2 ativada** (3/3 cells LIVE; ADR gates 12/12)
  - **Charts SOTA full plan implementation** (commit `bc95ca34`, 2026-05-04)
  - **API/SSE Stage C emit migration → RFC 9457** (commits `6c4b5f69`, `00382439`)
- Nenhuma execução de dogfood completo após esses três sprints. **Risco real de regressão acumulada não detectada** — testes unitários e envtest cobrem componentes isolados, mas não pegam falhas de cadeia (Rule sistêmica "implemented but not wired").
- Memória `feedback_no_tribal_knowledge` registra: cada workaround manual deve virar script idempotente + entrada de matriz antes do fim da sessão. O ciclo `clean:all → setup → heal → dogfood` é a forma mais agressiva de detectar workarounds não-codificados.

**Por que agora, não depois:**
- Sprint 9 (polish + launch) requer evidência de que o produto sobrevive a um cold-start completo. Sem essa evidência, qualquer canary em produção é cego.
- ADR-0022 exige plano formal para mudanças que tocam pipeline/state/contratos — este plano explicita a regra do loop (não é "rodar dogfood até dar verde"; é "fix-systemic-on-error com governance proporcional ao bug").
- A dependência tribal mais comum em PaaS é "funciona porque sempre tive infra rodando há 2 dias com fixes manuais que esqueci de codificar". Cleanroom mata essa classe de bug.

**Evidências motivadoras:**
- Heal H1-H15 já codificados em `task k3d:heal` (374 LOC) — provam que o ciclo destroy/setup destrava bugs invisíveis.
- `infra/tests/dogfood/full-loop.sh` já tem semântica `fix_commit` que **bloqueia retry sem fix em código** — exatamente o contrato que o usuário pediu.
- 102+ bugs encontrados/corrigidos durante 13 iterações de dogfood em 2026-04-29 (CHG-2026-04-29); evidência de que o método produz valor mensurável.

## Objective

**Done state:** Uma execução completa do ciclo `destroy → setup → tilt up → heal → doctor → full-loop.sh` termina com:
1. `infra/tests/dogfood/full-loop.sh` exit code **0** (todas as 12 entradas com `status=PASS`)
2. **Zero achados S0/S1** classificados como bug do sistema (S2-S4 documentados, não bloqueiam)
3. **Rule 3 clean** — zero termos internos em qualquer output CLI capturado
4. **Prova de Vida** passou para todo deploy executado nas 11 journeys
5. **Prova de Morte** passou para todo delete executado
6. Relatório final commitado em `docs/reports/dogfood-cleanroom-{date}.md` com resumo da convergência

**Goals mensuráveis:**
- ≤ 5 iterações do loop até convergência (cap explícito; iter 6+ → meeting T2 obrigatório)
- Cada iteração FAIL produz exatamente UM fix sistêmico commitado (não bundle de fixes)
- Cada fix vira test de regressão (TDD) ou entry em matriz de validação
- Tempo total esperado por iteração: ~25 min (5min destroy + 8min setup + 12min dogfood)

## ADRs

### D1 — Cleanroom é destroy COMPLETO, não reset parcial

**Decisão:** Cada iteração começa com `task clean:all` (deleta clusters + registry + artefatos), não com `--reset` do dogfood state ou `kubectl delete` seletivo.

**Rationale:** State residual em postgres/gitea/argocd/minio mascara classes inteiras de bugs (SA tokens cacheados, ArgoCD discovery cache, Gitea repo state, ClusterCredential refresh). Memória `feedback_live_test_existing_db` mostra que migrations silenciosamente skip em DB existente. Cleanroom é o único modo de pegar isso.

**Consequences:**
- (+) Detecta bugs de bootstrap, ordem de criação, idempotência real, secret rotation, RBAC bootstrap
- (+) Força que cada workaround manual vire script (memória `feedback_no_tribal_knowledge`)
- (−) Custo: ~5min de destroy + ~8min de setup por iteração; aceitável dado o ROI (3.9→4.0 maturity gate)
- (−) Não testa "upgrade in place" — esse cenário é coberto por outro plano (não escopo aqui)

### D2 — Fix-systemic-on-error é não-negociável; workaround é proibido

**Decisão:** Toda journey FAIL exige fix em código/Helm/manifests/heal-script + commit em `develop` antes do `fix_commit` ser populado em `~/.loopzero/dogfood_state.json`. Workaround manual (`kubectl patch` ad-hoc, `helm install` direto, edição de Secret in-cluster) está banido.

**Rationale:** Rule 4 (Git inviolable) + Rule "Não Reinvente"/"KISS" da CLAUDE.md global. Workaround manual não codificado vira tribal knowledge — exatamente o que o `task k3d:heal` foi criado para extirpar. Memória `feedback_no_tribal_knowledge` documenta o anti-pattern.

**Consequences:**
- (+) Toda iteração deixa o repo melhor que encontrou (regression-test antes do fix, fix commitado, matriz updated)
- (+) Convergência é monotônica — bugs não voltam
- (−) Iter pode levar mais tempo (tem que escrever teste + fix + matriz, não só "patch e segue")

### D3 — TDD obrigatório dentro do loop (RED antes de fix)

**Decisão:** Cada FAIL gera primeiro um teste de regressão (envtest, vitest, jest, ou matriz validate-matrix entry) que **falha hoje**. Só então o fix é escrito. Test passa → commit do fix com `Fixes #NN` ou referência ao journey.

**Rationale:** Rule 1 do CLAUDE.md (tests before code) + filosofia de testes da CLAUDE.md global ("se não tem teste, não funciona"). Sem RED-test antes do fix, o bug pode voltar invisível em refactor futuro.

**Consequences:**
- (+) Suite de regressão cresce com cada iteração — proteção composta
- (+) Documenta os bugs encontrados em forma executável
- (−) Em alguns casos (bug de Helm chart, bug de kubeconfig race) o "teste" é uma entry na matriz `task validate:matrix` em vez de teste unitário; aceitável

### D4 — Cap de 5 iterações; iter 6+ → meeting T2

**Decisão:** Se o loop não convergir em 5 iterações, **parar** e invocar `meeting-orchestrator` com escopo "dogfood não converge — investigar root-cause sistêmico". Não iterar infinitamente.

**Rationale:** Loop infinito é red flag de bug arquitetural (não bug local de implementação). Memória de Sprint 8 mostra que iter 7+ tipicamente revela problema de design que precisa de ADR (não fix tactical).

**Consequences:**
- (+) Limite forçado de honestidade: se 5 fixes locais não convergem, problema é maior
- (+) Não consome dias de wall-clock em loop infinito
- (−) Pode parar antes do "natural" se 5 fixes seguidos forem todos legítimos e ortogonais; aceitável (humano pode resetar contador com justificativa documentada)

### D5 — `task k3d:heal` roda APÓS `task setup`, NÃO no lugar

**Decisão:** Sequência canônica: `clean:all → setup → tilt up → k3d:heal → doctor → full-loop.sh`. Heal NÃO substitui setup; complementa para os 15 workarounds de cross-cluster que `setup` ainda não codifica.

**Rationale:** `setup` chama `crds:install + db:migrate + dev:seed + tokens:provision` — é o caminho oficial. Heal é "guard-rail" para os bugs históricos cross-cluster (RBAC, host aliases, kubeconfig IPs). Inverter ordem não funciona (heal depende de Secret que setup cria).

**Consequences:**
- (+) Cada iteração que ativa um novo H-handler significa novo workaround foi descoberto e codificado — sinal positivo de convergência
- (−) Se um fix local quebra `task setup` em si, heal não recupera; mas isso é caught no `task doctor` (gate antes do dogfood)

### D6 — Pass criterion é multidimensional, não só exit 0

**Decisão:** Convergência exige 5 condições simultâneas (full-loop exit 0 + zero S0/S1 + Rule 3 + Prova de Vida + Prova de Morte). Exit 0 sozinho **não** é suficiente.

**Rationale:** Memória `reference_audit_loop_session` documenta o anti-pattern "silent-success" — exit 0 com pod CrashLoop. Já vimos isso em produção. Skill `dogfood` exige Prova de Vida/Morte explicitamente.

**Consequences:**
- (+) Bar consistente com a skill `dogfood` (não relaxa contrato)
- (+) Pega o caso "ArgoCD sync OK mas pod não roda" que exit code da journey perderia
- (−) Acceptance criteria mais rico de validar; mitigado por relatório estruturado obrigatório no Phase 4

### D7 — Plan amendments seguem ADR-0022 (não silentes)

**Decisão:** Se durante o loop descobrir-se que uma journey precisa scope-change (e.g., journey-09-preview falha porque PreviewEnvironment requires new ADR), parar, invocar `meeting-orchestrator`, registrar `MEET-ASYNC-AMENDMENT-NNN.md`, e só então retomar. Não relaxar DoD localmente.

**Rationale:** ADR-0022 é regra de pre-release governance. Lessons-learned do plano `infra-normalize` (memória `reference_infra_normalize_2026_04_30`) — relax silente de DoD destrói confiança no plano.

**Consequences:**
- (+) Auditoria preservada
- (+) Convergência forçada a ser real (não negociada via DoD-relaxation)
- (−) Custo de meeting; aceitável

## Dependency Graph

```
Phase 0: Preconditions
   │
   ▼
Phase 1: Cleanroom orchestrator script (NEW)
   │
   ▼
Phase 2: First execution (cold-start; baseline)
   │
   ├─── PASS (12/12 GREEN) ──────────────▶ Phase 4: Sign-off
   │
   └─── FAIL ────▶ Phase 3: Convergence loop (iter 1..N, N≤5)
                       │
                       ├─── iter K converges ─▶ Phase 4: Sign-off
                       │
                       └─── iter 5 still FAIL ─▶ STOP + meeting T2 (D4)
```

**Sequencial obrigatório:** Phase 0 → 1 → 2. Phase 3 só inicia se Phase 2 falhar. Phase 4 é gate final único.

**Paralelismo:** dentro de Phase 3, cada iter pode internamente paralelizar análise de logs / regression-test writing / fix coding, mas o loop em si é estritamente sequencial (não rodar 2 iterations simultaneamente — corromperia state).

---

## Phase 0: Preconditions

**Objective:** Garantir que pré-requisitos estão prontos antes do loop começar (binaries, `.env`, working tree limpo, baseline tag).

### T0.1 — Verify host tooling

#### Objective
Confirmar binaries necessários estão instalados nas versões esperadas.

#### Evidence
CLAUDE.md lista pré-requisitos: Go 1.25+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, helm-diff plugin, Helmfile v1.0+, kubectl, kubeconform, golangci-lint v2.11+, cosign opcional. Falta de qualquer um faz `task setup` falhar de modo confuso.

#### Files to edit
```
(no edits — verification only)
```

#### Deep file dependency analysis
N/A — somente comandos de leitura.

#### Deep Dives
- **helm-diff** é a única dep "fácil de esquecer" (memória `feedback_no_tribal_knowledge` registra falhas históricas)
- **Helmfile v1.0+** é exigência nova de CHG-2026-04-28-LAYERED-BOOTSTRAP
- Verificar Docker daemon ativo (k3d falha silentemente se Docker está parado)

#### Tasks
1. Rodar `task --version`, `k3d version`, `kubectl version --client`, `helm version`, `helmfile --version`, `tilt version`, `go version`, `node --version`, `docker info`
2. Rodar `helm plugin list | grep diff`
3. Rodar `which kubeconform` e `kubeconform -v`
4. Anotar versões em scratch para incluir no relatório final

#### TDD
```
N/A — pre-flight check; no code changes
VERIFY: bash one-liner script returns 0 if all binaries present at min version
```

#### Acceptance Criteria
- [ ] Todos os binaries respondem com versão
- [ ] helm-diff plugin listed
- [ ] Docker daemon responsivo
- [ ] Versões anotadas para o relatório

#### DoD
- [ ] Comando único `task doctor` (existente) executa sem ERRO em pré-condição
- [ ] Versões coletadas

### T0.2 — Verify `.env` and `.env.local`

#### Objective
Garantir que credenciais locais (GH_TOKEN para journey-08-github-autodeploy + DB password override se necessário) estão presentes.

#### Evidence
Skill `dogfood` SKILL.md linha 110: "se GH_TOKEN não estiver em .env, journey-08 vai skipar — registrar como limitação". Para passar 12/12, `.env` deve estar populado.

#### Files to edit
```
.env — deve existir com GH_TOKEN=ghp_xxxx (NÃO commitar; gitignored)
.env.local — opcional, override de PG_USER/PG_PASS/PG_DB
```

#### Deep file dependency analysis
- `.env` lido por `infra/tests/dogfood/preflight.sh` linha ~110 e por `cli login --token`
- `.env.local` carregado por `Taskfile.yml:13-14` via `dotenv:` directive — override transparente

#### Deep Dives
- GH_TOKEN precisa de scopes `repo` + `read:user` no mínimo para journey-08
- Se token expirou, falha aparece em journey-08 como 401 — não em preflight; daí preflight precisa validar com `whoami`

#### Tasks
1. `test -f .env || echo "MISSING"`
2. `grep -E '^GH_TOKEN=' .env | head -1`
3. Validar token com `gh auth status` ou `curl -H "Authorization: token $GH_TOKEN" https://api.github.com/user`

#### TDD
```
N/A — env validation; no code change
VERIFY: GH_TOKEN responds 200 in GitHub API call
```

#### Acceptance Criteria
- [ ] `.env` existe e contém `GH_TOKEN=`
- [ ] Token responde GitHub API com 200

#### DoD
- [ ] Pré-condição validada antes de iniciar Phase 1

### T0.3 — Working tree clean + baseline tag

#### Objective
Garantir que o git tree está limpo (sem mudanças não-commitadas) e tag `dogfood-cleanroom-baseline-{date}` existe para diff posterior.

#### Evidence
Sem baseline limpa não há como medir "quantos commits o loop produziu" nem "qual o delta do plano". Memória `feedback_no_tribal_knowledge` enfatiza auditabilidade.

#### Files to edit
```
(no file edits — git operations only)
```

#### Deep file dependency analysis
N/A.

#### Deep Dives
- Tag local-only inicialmente; promove para origin se loop convergir (deixa rastro auditável)
- Branch atual deve ser `develop` (nunca `main` — Rule 4 git)

#### Tasks
1. `git status --porcelain` → exit 0 + saída vazia
2. `git rev-parse --abbrev-ref HEAD` → confirmar `develop`
3. `git tag -a "dogfood-cleanroom-baseline-$(date +%Y%m%d)" -m "Pre-loop baseline"` (local)

#### TDD
```
N/A — repo state check
VERIFY: git status clean + tag created
```

#### Acceptance Criteria
- [ ] `git status --porcelain` vazio
- [ ] Branch atual é `develop` (ou feature branch deliberado, anotado)
- [ ] Tag baseline criada

#### DoD
- [ ] Working tree clean
- [ ] Baseline tag existe

---

## Phase 1: Cleanroom Orchestrator Script

**Objective:** Construir um script único, idempotente, que executa `destroy → setup → heal → doctor → dogfood` em sequência, com logging estruturado e exit-code que distingue tipo de falha.

### T1.1 — Build `infra/scripts/dogfood-cleanroom-loop.sh`

#### Objective
Criar o orquestrador shell que encapsula a sequência canônica e produz output estruturado para auditoria.

#### Evidence
Sequência atual exige 6 comandos manuais em terminais separados (destroy, setup, tilt, esperar, heal, doctor, full-loop) — alta probabilidade de erro humano + difícil reprodução. Codificação resolve.

#### Files to edit
```
infra/scripts/dogfood-cleanroom-loop.sh — (NEW) orquestrador
infra/scripts/dogfood-cleanroom-loop.sh — chmod +x
```

#### Deep file dependency analysis
- **Calls:** `task clean:all`, `task setup`, `tilt up` (background), `task k3d:heal`, `task doctor`, `bash infra/tests/dogfood/full-loop.sh`
- **Reads:** `~/.loopzero/dogfood_state.json` (após full-loop, para extrair exit info)
- **Writes:** `.test-reports/dogfood-cleanroom-{ts}.log` (full stdout/stderr capture per iteration)
- **No edits** em `Taskfile.yml`, `full-loop.sh`, `journeys/*` — script só **chama**, não modifica.

#### Deep Dives
- **Tilt up em background:** `tilt up --port 0 --hud=false` em `&` + capturar PID em arquivo. Trap EXIT para `kill $TILT_PID` ao final (limpo).
- **Aguardar tilt convergir:** poll `task doctor` até retornar 0 ou timeout 180s. Sem isso, full-loop roda contra API ainda subindo.
- **Exit codes:**
  - `0` — full-loop exit 0 + zero S0/S1
  - `10` — destroy falhou (improvável; sinaliza Docker/k3d quebrado)
  - `20` — setup falhou (bug de bootstrap)
  - `30` — heal falhou (bug em heal script)
  - `40` — doctor falhou após 180s (API/operator não subiu)
  - `50` — full-loop falhou (journey FAIL com fix_commit pendente)
  - `60` — full-loop passou mas Rule 3 / Prova de Vida / Prova de Morte falhou
- **Idempotência:** rodar 2x seguido sem mudanças deve produzir mesmo resultado.
- **Trap on Ctrl-C:** mata tilt + grava state parcial.

#### Tasks
1. Criar `infra/scripts/dogfood-cleanroom-loop.sh` com header padrão (SPDX, copyright, set -euo pipefail)
2. Implementar funções: `phase_destroy`, `phase_setup`, `phase_tilt_up`, `phase_wait_doctor`, `phase_heal`, `phase_dogfood`, `phase_validate_post`
3. Implementar exit-code map (ver Deep Dives)
4. Implementar `--dry-run` (imprime o que seria feito; não executa)
5. Implementar `--phase=N` (executa fases N..6 só; útil para retomar após fix)
6. Implementar log capture: `tee -a .test-reports/dogfood-cleanroom-${TIMESTAMP}.log`
7. Implementar trap EXIT para limpar tilt PID
8. Tornar executável: `chmod +x`

#### TDD
```
RED:     test_dogfood_cleanroom_loop_unknown_arg_returns_3() — bash -n + arg "--bogus" returns 3
RED:     test_dogfood_cleanroom_loop_dry_run_does_not_invoke_destroy() — --dry-run sem efeitos colaterais; nenhum cluster deletado
RED:     test_dogfood_cleanroom_loop_phase_filter_skips_earlier_phases() — --phase=4 não chama clean:all
RED:     test_exit_code_map_complete() — script source contém todos os 6 exit codes (10/20/30/40/50/60)
GREEN:   Implementar shell + arg parser + phase functions
REFACTOR: Extrair helpers comuns (log_phase, run_or_exit) se duplicação > 3
VERIFY:  bash -n infra/scripts/dogfood-cleanroom-loop.sh && shellcheck infra/scripts/dogfood-cleanroom-loop.sh
```

Os 4 testes vivem em `infra/tests/scripts/dogfood-cleanroom-loop_test.sh` (bats-style ou shell asserts simples).

#### Acceptance Criteria
- [ ] `shellcheck infra/scripts/dogfood-cleanroom-loop.sh` zero warnings
- [ ] `bash -n` clean
- [ ] `--help` lista todas as flags + exit codes
- [ ] `--dry-run` é hermético (não toca k3d/docker/tilt)
- [ ] Trap EXIT mata tilt mesmo em Ctrl-C
- [ ] Pass: code-audit complexity check (script <= 300 linhas; cada função <= 50 linhas)
- [ ] Pass: code-audit lint check (shellcheck zero)

#### DoD
- [ ] Script existe + executável + shellcheck-clean
- [ ] 4 testes em `infra/tests/scripts/` passam
- [ ] `--dry-run` documenta sequência completa

### T1.2 — Wire orchestrator into Taskfile (optional convenience)

#### Objective
Adicionar `task dogfood:cleanroom` que invoca o script — atalho para a equipe.

#### Evidence
Equipe está acostumada a `task X` como entrada única; adicionar um alias reduz o atrito de digitação e padroniza com outros workflows (`task setup`, `task doctor`).

#### Files to edit
```
taskfiles/test.yml — adicionar entry `dogfood:cleanroom` que chama o script
```

#### Deep file dependency analysis
- `taskfiles/test.yml` é incluído em `Taskfile.yml:23-24` com `flatten: true` — entry vira `task dogfood:cleanroom` no namespace raiz.
- Não há colisão de nome (grep confirmou: zero ocorrências `dogfood:cleanroom` no repo).

#### Deep Dives
- Entry deve aceitar args (`{{.CLI_ARGS}}`) para passar `--dry-run`, `--phase=N` adiante
- `desc:` deve ser explícito + linkar ao plano

#### Tasks
1. Editar `taskfiles/test.yml` adicionando `dogfood:cleanroom` task
2. Validar: `task --list-all | grep cleanroom` retorna entry
3. Validar: `task dogfood:cleanroom -- --dry-run` chama o script com flag

#### TDD
```
N/A — Taskfile entry; behavior validado via integration check (task --list-all + invocation)
VERIFY: task dogfood:cleanroom -- --help imprime help do script
```

#### Acceptance Criteria
- [ ] `task --list-all | grep dogfood:cleanroom` retorna 1 linha
- [ ] `task dogfood:cleanroom -- --help` imprime help do script
- [ ] `desc:` referencia este plano por path

#### DoD
- [ ] Entry adicionada
- [ ] Args propagados via `{{.CLI_ARGS}}`

---

## Phase 2: First Execution (Cold-Start Baseline)

**Objective:** Executar o orquestrador uma vez, do zero, para estabelecer baseline. Resultado define se entramos em Phase 3 (loop) ou Phase 4 (sign-off direto).

### T2.1 — Run the orchestrator (iteration 0)

#### Objective
Executar `task dogfood:cleanroom` end-to-end e capturar resultado completo.

#### Evidence
Sem rodar, não saberemos quantos bugs realmente existem. Memória registra 12/12 GREEN em 2026-04-30, mas 4 sprints aconteceram desde então — regressão é provável.

#### Files to edit
```
(execução; gera artefatos em .test-reports/)
```

#### Deep file dependency analysis
- **Reads:** todo o repo (Helm charts, Pulumi, taskfiles, scripts)
- **Writes:** `.test-reports/dogfood-cleanroom-${TS}.log`, `~/.loopzero/dogfood_state.json`
- **K8s state:** clusters destruídos e recriados; namespaces criados; CRs aplicados

#### Deep Dives
- Antes de rodar, garantir que **NENHUM** outro processo está usando os clusters (`k3d cluster list` mostra estado atual; sem outros usuários)
- Tempo esperado: ~25min total (5min destroy + 8min setup + ~12min full-loop)
- Capturar `kubectl get events -A --sort-by=.lastTimestamp | tail -100` ao final como diagnóstico complementar

#### Tasks
1. Confirmar que tilt **não está** rodando em outro terminal (`pgrep tilt` deve ser vazio ou matá-lo)
2. Executar: `task dogfood:cleanroom 2>&1 | tee .test-reports/iter-0.log`
3. Capturar exit code: `echo $?`
4. Snapshot final state: `~/.loopzero/dogfood_state.json` → copiar para `.test-reports/iter-0-state.json`
5. Snapshot eventos K8s: `kubectl --context k3d-loopzero-mgmt get events -A | tee .test-reports/iter-0-events-mgmt.log` (idem para build/workload/runtime-2)

#### TDD
```
N/A — execução de validação, não código novo
VERIFY: exit code é 0 (path otimista) OR registra falha estruturada (path pessimista)
```

#### Acceptance Criteria
- [ ] Orquestrador executou todas as fases (até falhar ou todas passarem)
- [ ] Log completo em `.test-reports/iter-0.log`
- [ ] State JSON snapshot em `.test-reports/iter-0-state.json`
- [ ] Eventos K8s capturados para os 4 clusters

#### DoD
- [ ] Iteration 0 documentada
- [ ] Decision point alcançado: Phase 4 (se exit 0) OR Phase 3 (se exit ≠ 0)

### T2.2 — Decision gate: pass or loop?

#### Objective
Avaliar resultado de T2.1 e bifurcar para Phase 4 (pass) ou Phase 3 (convergence loop).

#### Evidence
Sem decision gate explícito, há risco de aceitar PASS parcial (e.g., journey-09 PENDING porque script ausente). D6 exige multidimensional pass.

#### Files to edit
```
(no edits — decision logic)
```

#### Deep file dependency analysis
N/A — leitura de state JSON.

#### Deep Dives
- 5 condições D6 conferidas em ordem: full-loop exit 0 → todas journeys PASS (não PENDING) → zero S0/S1 nos logs → Rule 3 grep clean → PV/PM passaram
- Falha em qualquer uma → Phase 3
- Sucesso em todas → Phase 4

#### Tasks
1. Ler `.test-reports/iter-0-state.json`
2. Para cada condição D6, anotar PASS/FAIL com evidência
3. Imprimir tabela de decisão
4. Bifurcar

#### TDD
```
N/A — decision review
VERIFY: tabela de 5 condições explícita, cada uma com PASS/FAIL + evidência
```

#### Acceptance Criteria
- [ ] Tabela das 5 condições D6 preenchida
- [ ] Decisão registrada: "PROCEED TO PHASE 4" ou "ENTER PHASE 3"

#### DoD
- [ ] Decisão documentada com evidência por condição

---

## Phase 3: Convergence Loop (iter 1..N, N≤5)

**Objective:** Para cada FAIL detectado em Phase 2 ou em iteração anterior, fix sistêmico (TDD) → commit → re-execute orchestrator. Loop até convergência ou cap (D4).

> **NOTA:** Esta phase contém **um único template de iteração** que se repete até 5x. Cada iteração é instanciada como T3.K com K = 1..5. Aqui descrevo o template; ao executar, instancie K conforme necessário.

### T3.K — Iteration K (template)

#### Objective
Identificar 1 falha sistêmica, escrever teste de regressão (RED), corrigir (GREEN), commit, re-executar orquestrador.

#### Evidence
Toda iteration K começa com state file de iter K-1 mostrando FAIL. Sem iteração anterior identificável → primeiro K=1 lê de `.test-reports/iter-0-state.json`.

#### Files to edit
```
Variável — depende do bug encontrado. Exemplos típicos:
  api/internal/services/<service>.go — bug de orquestração / lock / state
  operators/internal/controllers/<ctrl>_controller.go — bug de reconcile
  cli/src/commands/<cmd>.ts — bug de CLI / Rule 3 / exit code
  charts/<chart>/templates/<file>.yaml — bug de Helm
  infra/scripts/k3d-cross-cluster-heal.sh — novo H-handler
  infra/tests/dogfood/journeys/<journey>.sh — bug no journey, não no produto (raro)
```

#### Deep file dependency analysis
Faz-se **por bug** durante a iteração:
1. Identificar journey FAIL → ler script da journey + extrair comando que falhou + exit code + último output
2. Ler logs do componente envolvido (`task logs:api` / `task logs:operator` / `kubectl get events`)
3. Identificar root-cause (não sintoma)
4. Listar arquivos a tocar com justificativa por arquivo
5. Identificar testes existentes que passariam mesmo com o bug presente (gap de cobertura)

#### Deep Dives
**Heurísticas de classificação do bug** (memória `reference_audit_loop_session`):
- **Silent-success:** exit 0 + pod CrashLoop / health-check 500 → probably API marca release ACTIVE antes de ArgoCD confirmar
- **Semantic-forgery:** CLI imprime "Deploy succeeded" mas DB tem release.status=FAILED → discrepância de fonte
- **Contract-drift:** journey espera campo X em `--json` mas API mudou para Y → spec atrás do código (Rule 5 violation)
- **Implemented-but-not-wired:** unit test passa, dogfood falha → integração ausente (memória `Known Systemic Patterns`)
- **Tribal-knowledge:** funciona depois de fazer X manualmente → codificar X em `task k3d:heal` (memória `feedback_no_tribal_knowledge`)
- **Migration-skip:** schema_migrations.version > N mas tabela ausente → migration silently skipped (memória `feedback_live_test_existing_db`)

**Classificação T0/T1/T2/T3** (rule pre-release-governance):
- T0/T1 → fix direto, segue para commit
- T2/T3 → STOP, invocar `change-governor`, esperar meeting (D7)

#### Tasks
1. Ler state JSON de iteration K-1; identificar primeira journey FAIL
2. Capturar logs relevantes: `kubectl logs`, `kubectl events`, `git log --oneline -20`
3. **Diagnose:** classificar bug (heurísticas acima), listar arquivos afetados
4. **Tier:** classificar T0/T1/T2/T3
5. Se T2/T3 → invocar `meeting-orchestrator`; STOP iteration; humano decide se segue após meeting
6. Se T0/T1 → escrever teste RED que reproduz o bug
7. Rodar teste → confirmar FAIL
8. Implementar fix mínimo
9. Rodar teste → confirmar PASS
10. Rodar suite afetada (`go test ./...` ou `npm test`) → zero regressão
11. `git add -p` + `git commit -m "fix(<area>): <one-line> — iter K of dogfood-cleanroom"`
12. Popular `~/.loopzero/dogfood_state.json`: setar `fix_commit` para o SHA do commit
13. Re-executar `task dogfood:cleanroom` (iteration K+1 baseline)
14. Comparar `.test-reports/iter-${K-1}-state.json` vs `.test-reports/iter-K-state.json` — confirmar journey antes FAIL agora PASS, sem regressão em outras
15. Se nova FAIL apareceu (não esperada) → tratar como bug separado em iter K+1

#### TDD
```
RED:     <test específico do bug encontrado> — falha contra o sistema atual
GREEN:   Implementar fix mínimo
REFACTOR: Conforme padrões de design (KISS/SRP); sem mudar comportamento
VERIFY:  pacote afetado: `go test ./internal/<pkg>/...` OR `npm test -- <suite>`
VERIFY:  re-run: `task dogfood:cleanroom` exit 0 OR exit em journey diferente (= progresso)
```

#### Acceptance Criteria
- [ ] Bug classificado (categoria + tier T0-T3)
- [ ] Se T2/T3, meeting realizado e ADR registrado antes do código
- [ ] Test RED escrito antes do fix
- [ ] Test GREEN após fix
- [ ] Suite afetada passa sem regressão
- [ ] Commit em develop com mensagem padrão `fix(<area>): ...`
- [ ] `fix_commit` populado em state JSON
- [ ] Re-run mostra journey antes FAIL agora PASS
- [ ] Pass: code-audit lint check (zero warnings nos arquivos tocados)
- [ ] Pass: code-audit coverage check (cobertura do código novo >= 90%)

#### DoD
- [ ] Bug fixado e commitado
- [ ] Regression test no repo
- [ ] Re-run produziu progresso mensurável
- [ ] Iteration K documentada em `docs/reports/dogfood-cleanroom-iter-K.md` (1 parágrafo: bug, fix, test, commit)
- [ ] Se K = 5 e ainda FAIL: STOP, invocar meeting (D4)

---

## Phase 4: Sign-off

**Objective:** Quando convergiu (qualquer iteration K com 12/12 GREEN + zero S0/S1 + Rule 3 + PV/PM), produzir relatório final + atualizar artefatos de governance.

### T4.1 — Final dogfood report

#### Objective
Consolidar evidência de convergência em relatório commitado.

#### Evidence
Skill `dogfood` exige relatório formal. Plan-as-code requer rastro auditável (memória `reference_infra_normalize_2026_04_30`).

#### Files to edit
```
docs/reports/dogfood-cleanroom-{YYYYMMDD}.md — (NEW) relatório final
```

#### Deep file dependency analysis
- **Reads:** todos `.test-reports/iter-*.{log,state.json}`, `~/.loopzero/dogfood_state.json` final
- **References:** ADRs criados em Phase 3 (se T2/T3 surgiu), MEET-ASYNC-AMENDMENT records (se houve)

#### Deep Dives
**Conteúdo obrigatório:**
1. Sumário executivo (3 linhas: data, # iterações, resultado)
2. Tabela de iterações: K, journey FAIL inicial, root-cause, fix commit, duração
3. Tabela final 12/12 das journeys com `status: PASS` + evidência
4. Achados S2-S4 (não bloqueantes; documentados)
5. Rule 3 audit: comando `grep -E "ArgoCD|Gitea|MinIO|tenant_id|advisory lock|Kaniko|kubebuilder|envtest|GitOps|gitops-push|CRD" .test-reports/iter-final.log` deve retornar zero matches em CLI output
6. Prova de Vida summary: para cada deploy nas journeys, PV-1..PV-8 PASS (tabela)
7. Prova de Morte summary: idem para deletes
8. Lições aprendidas: padrões sistêmicos detectados (silent-success? tribal-knowledge?), recomendações para próximo dogfood
9. Diff: `git log --oneline dogfood-cleanroom-baseline-${DATE}..HEAD` mostrando todos commits do loop

#### Tasks
1. Criar `docs/reports/dogfood-cleanroom-${DATE}.md` com schema acima
2. Preencher cada seção com evidência dos `.test-reports/`
3. Validar com `mdformat` ou similar (sem regressão de markdown)
4. Commit: `docs(reports): dogfood cleanroom convergence — N iterations, 12/12 GREEN`

#### TDD
```
N/A — documento de relatório
VERIFY: relatório referencia tudo: state JSON, logs, commits, MEET records
VERIFY: Rule 3 grep retorna zero
```

#### Acceptance Criteria
- [ ] Relatório existe em `docs/reports/`
- [ ] 9 seções obrigatórias preenchidas
- [ ] Cada finding tem evidência clicável (linha de log / commit SHA / nome de teste)
- [ ] Rule 3 audit zero matches
- [ ] PV / PM tabelas completas

#### DoD
- [ ] Relatório commitado
- [ ] Diff baseline→HEAD anexado

### T4.2 — Update plan + memory

#### Objective
Marcar plano como complete, atualizar memória user com lições.

#### Evidence
ADR-0022 + memória `feedback_no_tribal_knowledge` exigem closure formal de planos. Sem update, lições viram tribal knowledge.

#### Files to edit
```
docs/plans/real-paas-cleanroom-dogfood-loop-plan.md — adicionar seção "Convergence Log" no fim
/home/paulo/.claude/projects/-home-paulo-loopzero/memory/MEMORY.md — adicionar entrada
/home/paulo/.claude/projects/-home-paulo-loopzero/memory/reference_dogfood_cleanroom_${DATE}.md — (NEW) detalhes da iteration
```

#### Deep file dependency analysis
- Plano edit: append-only (não modificar tasks já concluídas — preservar evidência)
- Memory file: seguir schema `name/description/type` da CLAUDE.md global
- MEMORY.md: 1-line entry conforme convention

#### Deep Dives
- Memória categoria `reference` (snapshot temporal) — não `feedback` (regra geral)
- Capturar pelo menos: # iter, padrões mais comuns vistos, links para fix commits, recomendação para próximo run

#### Tasks
1. Append "Convergence Log" no plano (data, # iterações, exit final, link para relatório)
2. Criar memória `reference_dogfood_cleanroom_${DATE}.md`
3. Adicionar 1 linha em MEMORY.md
4. Commit: `docs(plan,memory): close dogfood-cleanroom-loop plan — N iterations`

#### TDD
```
N/A — governance update
VERIFY: plano + memória + MEMORY.md commitados juntos
```

#### Acceptance Criteria
- [ ] Plano tem seção "Convergence Log"
- [ ] Memory file criado seguindo schema
- [ ] MEMORY.md updated com 1 linha

#### DoD
- [ ] Plano formalmente fechado
- [ ] Memória atualizada

### T4.3 — Tag + diff snapshot

#### Objective
Criar tag git `dogfood-cleanroom-converged-${DATE}` apontando para o HEAD pós-convergência. Permite reprodução exata.

#### Evidence
Memória `reference_e2e_iter12_2026_04_29` mostra valor de tags em audits posteriores ("o que estava no commit X quando dogfood passou?").

#### Files to edit
```
(no file edits — git tag)
```

#### Deep file dependency analysis
N/A.

#### Deep Dives
- Tag annotated (não lightweight) — anexa autor, data, mensagem com referência ao plano
- Tag local-only por padrão; promove a origin se humano confirmar (ADR-0022 exige mudanças visíveis cross-team passar por meeting; tag de relatório de teste local não passa esse threshold por default)

#### Tasks
1. `git tag -a "dogfood-cleanroom-converged-$(date +%Y%m%d)" -m "Plan closeout — see docs/reports/dogfood-cleanroom-$(date +%Y%m%d).md"`
2. `git tag --list "dogfood-cleanroom-*"` validar baseline + converged ambos existem

#### TDD
```
N/A — tag operation
VERIFY: 2 tags presentes (baseline + converged)
```

#### Acceptance Criteria
- [ ] Tag converged criada
- [ ] Tag annotated (não lightweight)
- [ ] Mensagem aponta para relatório

#### DoD
- [ ] Snapshot tagged

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Destruir infra local de forma reproduzível | T1.1 (phase_destroy chama `task clean:all`) | Encapsulado em script idempotente; D1 enforça destroy completo, não reset parcial |
| 2 | Recriar infra do zero (clusters + Helm + CRDs + DB + tokens) | T1.1 (phase_setup chama `task setup`); T1.1 (phase_heal chama `task k3d:heal`) | D5 estabelece ordem: setup → tilt → heal → doctor |
| 3 | Validar que infra está saudável antes do dogfood | T1.1 (phase_wait_doctor) | Poll `task doctor` até PASS ou timeout 180s; gate antes do full-loop |
| 4 | Executar todas as 12 jornadas dogfood | T1.1 (phase_dogfood chama `bash infra/tests/dogfood/full-loop.sh`) | Reusa orquestrador existente (DRY) — não reinventa |
| 5 | A cada falha, corrigir e iniciar plano novamente | T3.K iteration template (Phase 3) | Fix-systemic-on-error (D2); TDD (D3); commit antes de retomar |
| 6 | Loop até execução TOTALMENTE sem erros | Phase 3 + cap D4 (5 iterações) + decision gate T2.2 | Convergência multidimensional (D6); cap impede loop infinito |
| 7 | Rastrear progresso entre iterações | T2.1 + T3.K (snapshots `.test-reports/iter-K-state.json`) | State JSON do dogfood + log capture per iter |
| 8 | Evitar workarounds tribal-knowledge | D2 + heurística "tribal-knowledge → codificar em heal" em T3.K | Memória `feedback_no_tribal_knowledge` enforced |
| 9 | Auditabilidade pós-loop | T4.1 (relatório) + T4.2 (memória) + T4.3 (tag) | Phase 4 inteira |
| 10 | Pre-conditions claras antes de começar | Phase 0 (T0.1, T0.2, T0.3) | Tooling + .env + git tree clean |
| 11 | Atalho ergonômico para a equipe | T1.2 (`task dogfood:cleanroom`) | Reuso do padrão Taskfile |

**Coverage: 11/11 requirements covered (100%)**

## Global Definition of Done

- [ ] Phase 0 completa (preconditions verificadas)
- [ ] Phase 1 completa (orchestrator script + Taskfile entry, shellcheck-clean, 4 tests passando)
- [ ] Phase 2 completa (iteration 0 executada, decisão registrada)
- [ ] Phase 3 convergiu em K ≤ 5 iterações OU meeting T2 invocado para iter 6+
- [ ] Phase 4 completa (relatório + memória + tag)
- [ ] Todos os tests Go + TS passam (`task test:unit` exit 0)
- [ ] Zero lint warnings (`task lint` exit 0; `shellcheck` em scripts novos)
- [ ] Sem termos internos em CLI output capturado (Rule 3 audit)
- [ ] Backward compat preservada (pre-release → quebras justificadas via ADR)
- [ ] code-audit checks passing nos arquivos tocados pela iteration loop
- [ ] **`/dogfood full` PASSED** (Dogfood Gate abaixo)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **Este plano É o dogfood gate.** Não há "rodar dogfood depois" — o plano inteiro é a execução do dogfood sob condições de cleanroom. A gate é satisfeita quando Phase 4 completa.

### Execution

```bash
# Pré-requisito: Phase 0 OK
task --version && k3d version && helmfile --version && docker info >/dev/null
test -f .env && grep -q '^GH_TOKEN=' .env
git status --porcelain | wc -l  # esperado: 0

# Phase 1: build orchestrator (one-time)
# (após implementar T1.1, T1.2)
shellcheck infra/scripts/dogfood-cleanroom-loop.sh
bash infra/tests/scripts/dogfood-cleanroom-loop_test.sh

# Phase 2 + 3 + 4: o loop em si
task dogfood:cleanroom 2>&1 | tee .test-reports/iter-0.log
# se exit 0 → Phase 4 (sign-off)
# se exit ≠ 0 → Phase 3 iteration: diagnose, fix, commit, repeat
```

### Pass Criteria

- [ ] `task dogfood:cleanroom` (final iteration) executou em infra fresh-from-`task clean:all`
- [ ] Pre-Flight Check: TODOS os checks PASS (T0.1 da skill `dogfood`)
- [ ] Zero achados S0 (Bloqueante)
- [ ] Zero achados S1 (Grave) atribuíveis ao escopo deste plano
- [ ] Prova de Vida (PV-1..PV-8) passou para CADA deploy nas 11 journeys
- [ ] Prova de Morte (PM-1..PM-5) passou para CADA delete
- [ ] Sem regressão em journeys já PASS — comparação iter-K vs iter-(K-1) state JSONs
- [ ] Observabilidade check passou (logs, metrics, CRs em estado correto)
- [ ] Rule 3 clean: zero termos internos em qualquer output CLI capturado nos logs

### On Failure

Se `task dogfood:cleanroom` final ainda apresenta S0/S1:
1. Classificar tier (T0/T1/T2/T3) per pre-release-governance
2. Se T0/T1 → fix em código + commit + re-run (segue Phase 3 template)
3. Se T2/T3 → STOP, invocar `meeting-orchestrator`, registrar ADR + MEET, retomar após APPROVED
4. Se 5 iterações já foram executadas e ainda FAIL → STOP, escalar como problema arquitetural (D4)

S2-S4 findings documentados em `docs/reports/dogfood-cleanroom-{date}.md` mas NÃO bloqueiam closeout.

---

## Convergence Log (preencher durante execução)

> Append-only durante Phase 3+. Cada iteração adiciona 1 entrada.

| Iter | Date | Failed journey | Root-cause | Fix commit | Re-run result |
|------|------|----------------|------------|------------|----------------|
| 0 | TBD | TBD | (baseline) | n/a | TBD |
| 1 | TBD | TBD | TBD | TBD | TBD |
| ... | | | | | |

**Final outcome:** TBD (PASS / FAILED-AT-CAP / ESCALATED-TO-MEETING)
