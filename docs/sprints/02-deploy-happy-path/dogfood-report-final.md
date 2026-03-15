# Relatório de Dogfooding — Sprint 2: Deploy Happy Path

**Data:** 2026-03-15
**Auditor:** Claude (Dogfood Skill)
**Sprint/Escopo:** Sprint 2 — `loopzero deploy` happy path end-to-end

---

## 1. Resumo Executivo

- **Objetivo:** Validar que `loopzero login → loopzero deploy` produz app live com URL real, testado contra o dogfood-monorepo (4 apps, 3 tipos)
- **Escopo coberto:** Deploy completo, --json, --app, --no-wait, error paths (no git, no yaml, no auth, uppercase env, nonexistent app, concurrent deploy)
- **Resultado:** **EXIT GATE PASS** — Deploy completo funciona, URLs reais respondem com HTTP 200
- **Maiores riscos:** Deploy sem auth trava indefinidamente (S0), deploy parcial com --app falha health probe (S1)
- **Status:** Pronto com ressalvas importantes

## 2. Ambiente e Limitações

- **Ambiente:** k3d local (3 clusters: mgmt, build, workload) — Tilt dev loop
- **Perfil simulado:** Developer backend, monorepo Node.js, primeiro deploy
- **CLI:** `loopzero v0.1.0` via `cli/bin/loopzero`
- **API:** `loopzero-api` pod rodando no cluster mgmt
- **Limitações:** Cross-cluster via IPs hardcoded, DNS `.loopzero.local` requer Host header override

## 3. Metodologia

- Técnicas: Fluxo E2E, Erro realista, Consistência cruzada, Confiança, Exploração
- Jornadas: 7 cenários planejados, 12 executados (incluindo variantes)
- Critério: Spec §0.1 (Workflow 1), §0.3 (exit codes, --json), §4.4 (concurrent rejection)

## 4. Jornadas Testadas

| ID | Jornada | Objetivo | Status | Nota | Observações |
|----|---------|----------|--------|------|-------------|
| C01 | whoami | Verificar auth | PASS | 5/5 | Retorna email + team |
| C03 | Deploy completo | dogfood-monorepo → URLs | PASS | 4/5 | 396s, todas URLs ok |
| C04-05 | Health check | curl /health → 200 | PASS | 5/5 | api e gateway ambos ok |
| C06 | --json output | Schema §0.3 | PASS | 4/5 | 4 apps, deploy_id, duration |
| C07 | No .git | Exit 1 | PASS | 5/5 | Mensagem clara |
| C08 | No loopzero.yaml | Exit 1, sugere init | PASS | 5/5 | "Run: loopzero init" |
| C09 | No auth | Exit 4 rápido | **FAIL** | 1/5 | **Trava >20s** |
| C10 | Uppercase env | Exit 1 | PASS | 5/5 | "must be lowercase" |
| C11 | --app api | Deploy 1 app | **FAIL** | 2/5 | HEALTH_PROBE_FAILED |
| C12 | App inexistente | Exit 1 | PASS | 5/5 | Lista apps disponíveis |
| C13-14 | Concurrent + CI | 409 correto | PASS | 5/5 | Exit 3, mensagem actionable |
| C17 | --no-wait | Retorno imediato | PASS | 5/5 | build_id + stream_url |

## 5. Achados Detalhados

### [F-01] Deploy sem auth trava indefinidamente
- **Severidade:** S0 — Bloqueante
- **Categoria:** UX / Estado
- **Cenário:** C09
- **Objetivo do usuário:** Deploy sem estar logado
- **Pré-condição:** `~/.loopzero/config.json` removido
- **Passos:** `loopzero deploy` em diretório com loopzero.yaml e .git
- **Resultado esperado:** Exit 4 em < 1s com "Not authenticated. Run: loopzero login"
- **Resultado observado:** Trava por >20 segundos sem output, teve que ser killado
- **Impacto no usuário:** Usuário acha que o CLI bugou. Fecha terminal. Perda de confiança total.
- **Frequência provável:** Alta — todo dev novo sem login
- **Reprodutibilidade:** Sempre
- **Hipótese:** Auth check acontece DEPOIS de tentar conectar à API (upload source?), ou token null causa request HTTP que trava por timeout
- **Evidências:** Task killada após 20s sem resposta
- **Recomendação:** Auth guard no INÍCIO do deploy, antes de qualquer I/O

### [F-02] Deploy parcial com --app falha health probe
- **Severidade:** S1 — Grave
- **Categoria:** Funcional
- **Cenário:** C11
- **Objetivo do usuário:** Deploy apenas a app "api" do monorepo
- **Pré-condição:** Full deploy anterior completou com sucesso
- **Passos:** `loopzero deploy --app api --json`
- **Resultado esperado:** Exit 0 com api deployada
- **Resultado observado:** `{"error":"Deploy failed: HEALTH_PROBE_FAILED","exit_code":3,"deploy_id":"d-bed5"}`
- **Impacto no usuário:** Não consegue fazer deploy parcial. Feature anunciada não funciona.
- **Frequência provável:** Alta — deploy parcial é uso comum em monorepo
- **Reprodutibilidade:** Sempre (necessita confirmação em ambiente limpo)
- **Hipótese:** O workflow builda e gera manifests para apenas 1 app, mas ArgoCD sync pode estar sobrescrevendo os manifests das outras apps, ou o health probe URL está errada para deploy parcial
- **Evidências:** `exit_code: 3`, `deploy_id: d-bed5` — deploy existiu mas ativação falhou
- **Recomendação:** Investigar: (1) quais manifests o workflow gera com --app api, (2) se ArgoCD deleta recursos das apps não incluídas, (3) se health probe URL está correta

### [F-03] Worker app mostra `url: null` em --json
- **Severidade:** S3 — Leve
- **Categoria:** Consistência
- **Cenário:** C06
- **Objetivo do usuário:** Verificar JSON output do deploy
- **Resultado esperado:** worker sem campo `url` ou com `url: ""` (type: worker não tem URL)
- **Resultado observado:** `"url": null`
- **Impacto no usuário:** Scripts que parsam JSON podem quebrar em `null` vs empty string
- **Frequência provável:** Média
- **Evidências:** `{"name":"worker","status":"deployed","build_duration_seconds":440,"url":null}`
- **Recomendação:** Spec §0.3 não define explicitamente — decidir: omitir campo ou empty string. `null` é ambíguo.

### [F-04] Duração de build idêntica para todas as apps
- **Severidade:** S3 — Leve
- **Categoria:** Consistência / Confiança
- **Cenário:** C06
- **Objetivo do usuário:** Ver quanto tempo cada app levou para buildar
- **Resultado observado:** Todas 4 apps mostram `build_duration_seconds: 440`
- **Impacto no usuário:** Informação enganosa — sugere que cada app demorou 440s individualmente
- **Hipótese:** O workflow retorna duração total, não por app. O callback não diferencia.
- **Recomendação:** Se per-app timing não está disponível, remover `build_duration_seconds` por app e manter apenas `duration_seconds` no nível do deploy

### [F-05] Deploy completo leva 396-440s (>6 min)
- **Severidade:** S2 — Moderado
- **Categoria:** Performance Percebida
- **Cenário:** C03
- **Objetivo do usuário:** Deploy em < 5 minutos ("5-minute promise")
- **Resultado observado:** 396s (6min 36s) no primeiro deploy, 440s (7min 20s) no segundo
- **Impacto no usuário:** Violação da "5-minute promise" do §1 North Star
- **Frequência provável:** Alta — primeiro deploy sempre frio (sem cache)
- **Hipótese:** npm install + build de 4 apps + shared packages + Kaniko + ArgoCD sync. Cache cold.
- **Recomendação:** Medir e otimizar: (1) segundo deploy deve usar cache (verificar), (2) paralelização de builds no workflow, (3) warm cache no fixture

### [F-06] Help do deploy não menciona --json
- **Severidade:** S3 — Leve
- **Categoria:** Exploração / UX
- **Cenário:** C15
- **Objetivo do usuário:** Descobrir flags disponíveis
- **Resultado observado:** `--json` não aparece em `loopzero deploy --help`
- **Impacto no usuário:** Usuário não descobre --json sem ler docs
- **Recomendação:** Comportamento padrão do Commander.js (flag global). Não é bug, mas adicionar na description ajuda discoverability.

## 6. Padrões Sistêmicos

| Padrão | Achados | Impacto |
|--------|---------|---------|
| Auth check tardio | F-01 | Operações que deveriam falhar fast fazem I/O antes de verificar auth |
| Per-app deploy instável | F-02 | Feature prometida (--app) não funciona reliably |
| Timing impreciso | F-04 | Métricas per-app são copy do total, não granulares |
| Performance fria | F-05 | Primeiro deploy excede promise de 5min |

## 7. Matriz de Priorização

| ID | Severidade | Impacto Negócio | Impacto Usuário | Prioridade |
|----|-----------|-----------------|-----------------|------------|
| F-01 | S0 | Alto | Alto | **P0** |
| F-02 | S1 | Alto | Alto | **P1** |
| F-05 | S2 | Médio | Alto | P2 |
| F-03 | S3 | Baixo | Baixo | P3 |
| F-04 | S3 | Baixo | Baixo | P3 |
| F-06 | S3 | Baixo | Baixo | P4 |

## 8. Top 5 Correções Mais Valiosas

1. **F-01: Auth guard no início do deploy** — fast fail antes de I/O. Todo dev novo vai encontrar isso.
2. **F-02: Investigar e corrigir --app deploy** — feature core de monorepo que não funciona.
3. **F-05: Otimizar pipeline para 2o deploy** — cache warm deve trazer para < 5min.
4. **F-04: Timing per-app real ou remover campo** — informação falsa é pior que nenhuma.
5. **F-03: Definir contrato para url null vs omit** — clarificar no spec §0.3.

## 9. Veredito Final

- [ ] Não pronto para uso real
- [x] **Pronto com ressalvas importantes**
- [ ] Pronto para dogfooding ampliado
- [ ] Pronto para beta externo controlado

**Justificativa:** O fluxo principal funciona — `loopzero deploy` produz apps live com URLs reais que respondem HTTP 200. A concurrent rejection funciona (409 correto). Error paths são claros (exit codes corretos). O --json schema está completo. Porém: (1) deploy sem auth trava o CLI (S0), (2) --app deploy falha (S1), e (3) o primeiro deploy excede a promise de 5 minutos. Antes de ampliar dogfooding, F-01 e F-02 devem ser corrigidos.

## 10. Próximos Passos

**Imediato:**
- Corrigir F-01 (auth guard) — 30min
- Investigar F-02 (--app health probe) — precisa de diagnóstico no workflow + ArgoCD

**Curto prazo:**
- Medir segundo deploy com cache warm (F-05)
- Definir contrato url para workers (F-03)
- Per-app build timing (F-04)

**Próxima rodada de dogfooding:**
- Testar com repo externo (não fixture local)
- Testar Ctrl+C durante deploy
- Testar deploy após falha anterior
- Testar `loopzero deploy --env staging`
