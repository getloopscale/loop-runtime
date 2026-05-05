# Plan: Local Regression Load Testing

> **Version 2.0** — Sistema de teste de carga 100% local que garante que o LoopZero não regrediu. Roda contra k3d, detecta degradação de performance comparando com baselines gravadas, e falha se p99 piorou. Zero dependência de infra remota. Propósito: regressão, não benchmarking de produção.

## Context

O LoopZero já possui scripts k6 (L1-L5, stress, soak, SSE, chaos, API) e um runner que executa in-cluster via Job no k3d local. A infra de testes existe (`infra/tests/k6/scripts/`, `infra/tests/runner/run-k6.sh`). Porém o sistema atual tem problemas como ferramenta de **regressão**:

1. **Thresholds absolutos** — `health < 100ms p99` depende de hardware. Num laptop mais lento, falha por motivo errado.
2. **Sem baseline comparison** — não existe "comparar com a última execução boa" para detectar degradação relativa.
3. **Sem modo regression rápido** — L1 leva 15 min, soak 6h. Desenvolvedor precisa de um `task test:load:regression` que rode em < 10 min e diga "piorou ou não".
4. **Soak desproporcionado** — 6h para regressão local é impraticável. Precisa de versão curta (30 min).
5. **Sem CI local gate** — nenhum workflow que rode L1 + API como gate antes de push.
6. **Manifests remotos sem propósito** — CronJob, k6 Operator, TestRun distribuído são infra remota que não serve para regressão local.

**Estado atual confirmado (2026-05-05):**
- 10 scripts k6 existem em `infra/tests/k6/scripts/`
- `run-k6.sh` já roda in-cluster no k3d (Job template)
- Taskfile já tem `test:load:l1` até `test:load:all`
- Manifests em `infra/tests/k6/manifests/` (rbac, soak-cronjob, soak-alerts, testrun-l3, testrun-l4)

## Objective

**Done = desenvolvedor roda `task test:load:regression` contra k3d local em < 10 min e recebe PASS/FAIL baseado em degradação relativa vs baseline. Nenhuma dependência externa.**

- Modo regression rápido (L1 + API, < 10 min)
- Baseline system (gravar, comparar, atualizar)
- Soak curto (30 min) para regressão, 6h como `--full` opcional
- Thresholds relativos (% de degradação vs baseline)
- Tudo roda com `task setup && tilt up` como pré-requisito
- Remover/arquivar infra remota que não serve para regressão

## ADRs

### D1 — Tudo local, k3d é o ambiente

**Decision:** Todos os testes rodam contra k3d local. Nenhuma dependência de DO, CI remoto, ou cluster externo.
**Rationale:** Propósito é regressão — "o código que eu mudei piorou a performance?" Isso é medido localmente. Benchmarking absoluto (latência real em produção) é problema diferente, resolvido separadamente.
**Consequences:** Thresholds devem ser relativos (vs baseline), não absolutos. Hardware varia entre máquinas.

### D2 — Baseline comparison (não thresholds absolutos)

**Decision:** O sistema grava baselines (`infra/tests/k6/baselines/`) e compara execuções futuras contra elas. FAIL se degradação > 20% no p99.
**Rationale:** `health < 100ms` num MacBook Pro pode ser `health < 300ms` num laptop antigo. O que importa é: "piorou desde a última vez que estava ok?" — não o valor absoluto.
**Consequences:** Primeira execução grava baseline (sempre PASS). Execuções subsequentes comparam. Desenvolvedor pode `task test:load:baseline:update` para aceitar nova performance.

### D3 — Modo regression < 10 min

**Decision:** `task test:load:regression` roda L1 (5 min simplificado) + API (3 min) = < 10 min total. É o gate diário do desenvolvedor.
**Rationale:** 15 min (L1 atual) + 5 min (API) = 20 min é demais para rodar antes de cada push. Modo regression usa VUs reduzidos e duração menor, suficiente para detectar regressões grossas (query lenta, lock contention, endpoint quebrado).
**Consequences:** Regression mode não substitui L1-L5 completos — é smoke de regressão.

### D4 — Soak 30 min para regressão, 6h opcional

**Decision:** `task test:load:soak:short` roda 30 min (suficiente para detectar memory leak óbvio). `task test:load:soak` mantém 6h para validação profunda (antes de release).
**Rationale:** Leaks que se manifestam em < 30 min são graves e devem ser pegos na regressão. Leaks sutis (2h+) são pegos antes de release, não no dia a dia.
**Consequences:** Soak curto faz parte do `test:load:full` mas NÃO do `test:load:regression`.

### D5 — k6 Operator e infra remota: arquivar

**Decision:** Remover manifests de CronJob, k6 Operator TestRun, e alerts remotos do escopo ativo. Mover para `infra/tests/k6/manifests/remote/` (referência futura, não executado).
**Rationale:** CronJob semanal, TestRun distribuído, e PrometheusRule são infra para cluster de produção/staging. Não servem para regressão local. Mantê-los no path ativo cria confusão sobre o que é testado e o que é aspiracional.
**Consequences:** Quando (Sprint 9+) existir ambiente remoto para load testing, esses manifests podem ser resgatados.

### D6 — Scripts existentes permanecem intactos

**Decision:** Os 10 scripts k6 existentes não são modificados. O modo regression é um novo script wrapper leve que executa subsets com duração/VUs reduzidos.
**Rationale:** Scripts L1-L5 são corretos para seu propósito (validação completa). Modificá-los para servir dois propósitos viola SRP. O modo regression é um novo concern.
**Consequences:** Novo script `regression-quick.js` importa de `lib/` e roda subset rápido.

## Dependency Graph

```
Phase 0 (Baseline System) ──▶ Phase 1 (Regression Mode) ──▶ Phase 2 (Cleanup + Docs)
     │                              │
     ├── T0.1 baseline recorder     ├── T1.1 regression-quick.js
     ├── T0.2 baseline comparator   ├── T1.2 soak-30min.js
     └── T0.3 Taskfile baseline     └── T1.3 Taskfile regression
                                         │
                                    Phase 2
                                         ├── T2.1 Arquivar manifests remotos
                                         ├── T2.2 README atualizado
                                         └── T2.3 Validation run
```

Phase 0 é fundação (sem comparação, nada funciona). Phase 1 usa a fundação. Phase 2 é cleanup.

---

## Phase 0: Baseline System

**Objective:** Criar infraestrutura para gravar e comparar resultados de load tests locais.

### T0.1 — Baseline Recorder

#### Objective
Script que captura o output do k6 (JSON summary) e salva como baseline nomeada.

#### Evidence
- k6 suporta `--summary-export=file.json` que exporta todas as métricas em JSON estruturado
- Não existe nenhum mecanismo de comparação hoje — cada execução é isolada

#### Files to create
```
infra/tests/k6/tools/record-baseline.sh — (NEW) grava baseline de uma execução k6
infra/tests/k6/baselines/.gitkeep — (NEW) diretório para baselines
```

#### Deep Dives
- **Formato da baseline:** JSON do k6 summary (`--summary-export`). Contém todas as métricas (p50, p95, p99, rate, count) por threshold name.
- **Naming:** `baselines/{script-name}-{date}.json` + symlink `baselines/{script-name}-latest.json`
- **O que gravar:** p50, p95, p99 de `http_req_duration`, `loopzero_deploy_duration_ms`, `loopzero_api_*_duration`. Rates de `loopzero_build_succeeded`, `loopzero_app_verified`.
- **Gitignore:** baselines são locais (hardware-specific), NÃO commitados. `.gitignore` inclui `infra/tests/k6/baselines/*.json`.

#### Tasks
1. Criar `infra/tests/k6/baselines/.gitkeep`
2. Adicionar `infra/tests/k6/baselines/*.json` ao `.gitignore`
3. Criar `infra/tests/k6/tools/record-baseline.sh` que:
   - Recebe path do JSON summary como argumento
   - Extrai métricas relevantes (p50, p95, p99, rates)
   - Salva em `baselines/{name}-latest.json`
   - Mantém histórico `baselines/{name}-{date}.json` (últimas 5)

#### TDD
```
RED:     ./tools/record-baseline.sh não existe
GREEN:   Criar script, executar com JSON de exemplo, baseline salva
VERIFY:  cat baselines/l1-10-tenants-latest.json | jq '.metrics.http_req_duration.p99'
```

#### Acceptance Criteria
- [ ] Script recebe JSON summary e grava baseline
- [ ] Symlink `*-latest.json` atualizado
- [ ] Histórico mantém últimas 5 execuções
- [ ] Baselines em `.gitignore` (não commitadas)
- [ ] Script executável e com help (`--help`)

#### DoD
- [ ] `./tools/record-baseline.sh --help` funciona
- [ ] Grava baseline a partir de JSON k6 real
- [ ] `.gitignore` atualizado

---

### T0.2 — Baseline Comparator

#### Objective
Script que compara resultado atual vs baseline e emite PASS/FAIL com degradação percentual.

#### Files to create
```
infra/tests/k6/tools/compare-baseline.sh — (NEW) compara resultado vs baseline
```

#### Deep Dives
- **Lógica:** Para cada métrica na baseline, calcula `(atual - baseline) / baseline * 100`. Se degradação > threshold (default 20%), FAIL.
- **Output:** Tabela com métrica, baseline, atual, delta%, status (✓/✗)
- **Exit code:** 0 se todas ✓, 1 se qualquer ✗
- **Tolerância configurável:** `REGRESSION_THRESHOLD=20` (%) via env var
- **Primeira execução:** Se baseline não existe, grava automaticamente e retorna PASS (bootstrap)
- **Métricas comparadas:** apenas p95 e p99 de durations + rates. p50 é volátil demais.

#### Tasks
1. Criar `infra/tests/k6/tools/compare-baseline.sh`
2. Implementar comparação JSON com `jq`
3. Emitir tabela formatada com deltas
4. Exit code correto (0/1)
5. Auto-bootstrap se baseline não existe

#### TDD
```
RED:     ./tools/compare-baseline.sh não existe
GREEN:   Criar script, testar com baseline conhecida + resultado 10% pior (PASS) + 25% pior (FAIL)
VERIFY:  Simular degradação e verificar exit code
```

#### Acceptance Criteria
- [ ] Compara p95, p99 de durations e rates
- [ ] Threshold configurável via `REGRESSION_THRESHOLD` (default 20%)
- [ ] Tabela legível com ✓/✗ por métrica
- [ ] Exit 0 se OK, exit 1 se regressão
- [ ] Auto-bootstrap (grava baseline se não existe)
- [ ] Ignora métricas ausentes na baseline (novas métricas = PASS)

#### DoD
- [ ] Script funciona com dados reais do k6
- [ ] PASS com delta < 20%, FAIL com delta > 20%
- [ ] Bootstrap funciona na primeira execução

---

### T0.3 — Taskfile: Baseline Commands

#### Objective
Adicionar tasks para gravar e comparar baselines.

#### Files to edit
```
taskfiles/test.yml — adicionar tasks de baseline
```

#### Tasks
1. Adicionar `test:load:baseline:record` — roda L1 + API e grava baseline
2. Adicionar `test:load:baseline:update` — alias para record (aceita nova performance)
3. Adicionar `test:load:baseline:show` — mostra baseline atual

#### Acceptance Criteria
- [ ] `task test:load:baseline:record` executa L1 + grava
- [ ] `task test:load:baseline:update` é alias claro
- [ ] `task test:load:baseline:show` exibe valores atuais
- [ ] Todos listam via `task --list`

#### DoD
- [ ] `task --list | grep baseline` mostra 3 tasks
- [ ] Zero erros de syntax no Taskfile

---

## Phase 1: Regression Mode

**Objective:** Scripts e tasks que rodam regressão rápida (< 10 min) com comparação automática.

### T1.1 — regression-quick.js

#### Objective
Script k6 leve que roda subset de L1 + API em < 10 min, otimizado para detecção de regressão.

#### Files to create
```
infra/tests/k6/scripts/regression-quick.js — (NEW) smoke rápido para regressão
```

#### Deep Dives
- **Design:** 2 scenarios simultâneos:
  - `deploy_smoke`: 3 VUs, 1 iteration each (3 deploys total, ~5 min)
  - `api_benchmark`: constant-arrival-rate 50 RPS por 3 min
- **Por que não reusar L1:** L1 usa 10 VUs × 1 iteration = 10 deploys = 15 min. Regression precisa de menos deploys (validar que pipeline funciona) e mais foco em API (detectar queries lentas).
- **Métricas:** Mesmas de L1 + API (reutiliza `lib/loopzero-api.js`). Summary exportada para comparação.
- **Duration total:** ~7 min (3 deploys paralelos + 3 min API + overhead)

#### Tasks
1. Criar `infra/tests/k6/scripts/regression-quick.js`
2. Implementar 2 scenarios (deploy_smoke + api_benchmark)
3. Reutilizar `lib/loopzero-api.js` e `lib/thresholds.js`
4. `handleSummary` exporta JSON para comparação
5. Thresholds mínimos (script não falha por threshold — a comparação com baseline que decide)

#### TDD
```
RED:     k6 inspect scripts/regression-quick.js — falha (não existe)
GREEN:   Criar script, k6 inspect passa
VERIFY:  k6 inspect scripts/regression-quick.js && echo OK
```

#### Acceptance Criteria
- [ ] 2 scenarios: deploy_smoke (3 VUs) + api_benchmark (50 RPS)
- [ ] Duration total < 10 min
- [ ] Reutiliza libs existentes
- [ ] `handleSummary` exporta JSON
- [ ] Thresholds relaxados (não é gate absoluto — baseline comparison decide)
- [ ] Segue style guide (SPDX, comments, imports)

#### DoD
- [ ] `k6 inspect` green
- [ ] Execução total < 10 min em k3d local

---

### T1.2 — soak-30min.js

#### Objective
Versão curta do soak (30 min) para regressão local — detecta leaks óbvios sem esperar 6h.

#### Files to create
```
infra/tests/k6/scripts/soak-30min.js — (NEW) soak curto para regressão
```

#### Deep Dives
- **Design:** Igual ao `soak-6h.js` mas com `duration: '30m'` e `gracefulStop: '5m'`
- **Rate:** Mesmo 1 deploy/min = 30 deploys total
- **Tenants:** 10 (vs 50 no soak full — suficiente para 30 deploys)
- **O que detecta:** memory leak > 50MB em 30 min, FD leak > 100 FDs em 30 min, connection pool starvation
- **O que NÃO detecta:** leaks sutis que precisam de 2h+ para se manifestar (esses ficam no soak-6h)
- **handleSummary:** Compara primeira metade (0-15min) vs segunda metade (15-30min) via hour_marker

#### Tasks
1. Criar `infra/tests/k6/scripts/soak-30min.js`
2. Copiar estrutura do `soak-6h.js` com duration reduzida
3. Ajustar tenants (10), VUs (10), maxVUs (30)
4. handleSummary com split temporal (primeira vs segunda metade)

#### TDD
```
RED:     k6 inspect scripts/soak-30min.js — falha
GREEN:   Criar script, k6 inspect passa
VERIFY:  k6 inspect scripts/soak-30min.js
```

#### Acceptance Criteria
- [ ] Duration = 30 min + gracefulStop 5 min
- [ ] constant-arrival-rate 1 deploy/min
- [ ] 10 tenants
- [ ] handleSummary com split temporal
- [ ] Métricas compatíveis com baseline system

#### DoD
- [ ] `k6 inspect` green
- [ ] Estrutura consistente com soak-6h.js

---

### T1.3 — Taskfile: Regression Commands

#### Objective
Tasks que integram regression mode com baseline comparison.

#### Files to edit
```
taskfiles/test.yml — adicionar regression tasks
infra/tests/runner/run-k6-regression.sh — (NEW) runner que executa + compara
```

#### Deep Dives
- **`run-k6-regression.sh`:** Wrapper de `run-k6.sh` que:
  1. Executa o script k6 normalmente (Job no k3d)
  2. Extrai summary JSON do Job output
  3. Chama `compare-baseline.sh` com o JSON
  4. Se baseline não existe, chama `record-baseline.sh` (bootstrap)
  5. Exit code = exit code do comparador
- **`task test:load:regression`:** Executa `regression-quick.js` via runner regression
- **`task test:load:soak:short`:** Executa `soak-30min.js` via runner regression
- **`task test:load:full`:** L1 + L2 + stress + API + soak-short (validação completa antes de PR)

#### Tasks
1. Criar `infra/tests/runner/run-k6-regression.sh`
2. Adicionar `test:load:regression` ao Taskfile
3. Adicionar `test:load:soak:short` ao Taskfile
4. Adicionar `test:load:full` ao Taskfile (suite completa local)

#### TDD
```
RED:     task test:load:regression — task não existe
GREEN:   Criar task + runner, execução funciona
VERIFY:  task --list | grep regression
```

#### Acceptance Criteria
- [ ] `task test:load:regression` roda < 10 min e compara com baseline
- [ ] `task test:load:soak:short` roda 30 min soak
- [ ] `task test:load:full` roda suite completa (L1+L2+stress+API+soak-short)
- [ ] Primeiro run faz bootstrap automático da baseline
- [ ] Exit code reflete resultado da comparação (0=ok, 1=regressão)

#### DoD
- [ ] Todos os tasks listam e executam
- [ ] Runner regression funciona end-to-end
- [ ] Bootstrap automático na primeira execução

---

## Phase 2: Cleanup + Documentation

**Objective:** Arquivar infra remota, atualizar docs, validar sistema completo.

### T2.1 — Arquivar Manifests Remotos

#### Objective
Mover manifests de infra remota para subdiretório `remote/` (referência futura, não ativo).

#### Files to edit
```
infra/tests/k6/manifests/remote/ — (NEW) mover: soak-cronjob.yaml, soak-alerts.yaml, testrun-l3-distributed.yaml, testrun-l4-distributed.yaml
infra/tests/k6/manifests/ — manter apenas rbac.yaml (usado pelo Job runner)
```

#### Tasks
1. Criar `infra/tests/k6/manifests/remote/`
2. Mover `soak-cronjob.yaml`, `soak-alerts.yaml`, `testrun-l3-distributed.yaml`, `testrun-l4-distributed.yaml`
3. Adicionar `README.md` em `remote/` explicando que são para uso futuro em ambiente remoto
4. Remover tasks de Taskfile que referenciam esses manifests (`k6:operator:install`, `k6:distributed:*`, `k6:soak:apply`)

#### Acceptance Criteria
- [ ] Apenas `rbac.yaml` em `infra/tests/k6/manifests/`
- [ ] 4 manifests movidos para `manifests/remote/`
- [ ] Tasks remotos removidos do Taskfile
- [ ] README explica propósito do `remote/`

#### DoD
- [ ] Estrutura limpa — local vs remoto separados
- [ ] Zero referência quebrada no Taskfile

---

### T2.2 — README Atualizado

#### Objective
Reescrever `infra/tests/k6/README.md` com foco em uso local de regressão.

#### Files to edit
```
infra/tests/k6/README.md — reescrever para foco local/regression
```

#### Tasks
1. Quick Start focado em: `task test:load:regression` (dia a dia)
2. Explicar baseline system (record, compare, update)
3. Listar todos os modos: regression (< 10 min), full (~ 2h), soak-short (30 min), soak-full (6h)
4. Remover seções de k6 Operator, distributed testing, CI remote, CronJob
5. Manter seção de scripts como referência

#### Acceptance Criteria
- [ ] Quick Start: 3 comandos para regressão
- [ ] Baseline system documentado
- [ ] Todos os task commands listados
- [ ] Zero referência a infra remota no flow principal
- [ ] Seção "Remote/Production Testing" aponta para `manifests/remote/`

#### DoD
- [ ] README reflete realidade local

---

### T2.3 — Validation Run

#### Objective
Executar todo o sistema de regressão end-to-end em k3d local.

#### Tasks
1. `task setup && tilt up && task doctor`
2. `task test:load:baseline:record` — grava primeira baseline
3. `task test:load:regression` — compara (deve PASS, mesma baseline)
4. `task test:load:soak:short` — 30 min soak funciona
5. `k6 inspect` em `regression-quick.js` e `soak-30min.js`
6. `task --list | grep "test:load"` — todos os tasks existem

#### Acceptance Criteria
- [ ] Baseline gravada com sucesso
- [ ] Regression PASS (comparação consigo mesmo)
- [ ] Soak short executa sem erro
- [ ] Todos os scripts passam `k6 inspect`
- [ ] Inventário final: 12 scripts, 1 manifest ativo, 4 manifests archived, 2 tools, 15+ tasks

#### DoD
- [ ] Full regression run GREEN em k3d local

---

## Coverage Matrix

| # | Problema | Task(s) | Resolução |
|---|----------|---------|-----------|
| 1 | Thresholds absolutos (hardware-dependent) | T0.1, T0.2 | Baseline comparison relativa (delta %) |
| 2 | Sem modo regression rápido | T1.1, T1.3 | `regression-quick.js` < 10 min |
| 3 | Soak 6h impraticável para dev | T1.2 | `soak-30min.js` para regressão |
| 4 | Sem baseline comparison | T0.1, T0.2, T0.3 | Tools: record + compare + Taskfile |
| 5 | Infra remota confunde escopo | T2.1 | Arquivada em `manifests/remote/` |
| 6 | README assume infra remota | T2.2 | Reescrito para foco local |
| 7 | Primeiro run não tem referência | T0.2 | Auto-bootstrap (grava e retorna PASS) |

**Coverage: 7/7 problemas resolvidos (100%)**

## Global Definition of Done

- [ ] Todas as 3 phases completas
- [ ] `k6 inspect` green em `regression-quick.js` e `soak-30min.js`
- [ ] `task test:load:regression` executa < 10 min e retorna 0
- [ ] Baseline system funciona (record → compare → PASS/FAIL)
- [ ] Auto-bootstrap na primeira execução
- [ ] Manifests remotos arquivados
- [ ] README reflete uso local
- [ ] Zero lint warnings
- [ ] Taskfile sem referências quebradas

---

## Dogfood Gate (MANDATORY)

> **Plano só é DONE após validação completa em k3d local.**

### Execução

```bash
# 1. Infra local limpa
task setup && tilt up
task doctor

# 2. Bootstrap baseline (primeira vez)
task test:load:baseline:record

# 3. Regression mode (deve PASS — comparando consigo mesmo)
task test:load:regression

# 4. Soak curto (30 min)
task test:load:soak:short

# 5. Simular regressão: introduzir sleep 500ms em /health, rodar regression
#    (deve FAIL — degradação > 20%)
# 6. Remover sleep, rodar novamente (deve PASS)
```

### Pass Criteria

- [ ] Baseline gravada sem erros
- [ ] Regression PASS na primeira comparação
- [ ] Regression FAIL com degradação simulada (> 20%)
- [ ] Regression PASS após remover degradação
- [ ] Soak short completa 30 min sem erro
- [ ] Todos os tasks funcionam (`task --list | grep test:load`)
- [ ] Zero internal terms em output (Rule 3)

### On Failure

Se qualquer step falha:
1. Diagnosticar root cause
2. Fixar
3. Re-rodar do step 1
4. Repetir até GREEN completo
