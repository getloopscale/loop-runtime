# Plan: Dogfood Real End-to-End — Fix-Systemic-On-Error, FAANG-grade

> **Version 1.0** — Executar dogfood real do LoopZero PaaS exercitando 100% dos comandos CLI (44 do spec) em fluxos lifecycle completos contra `k3d-standalone-multi`. Princípio inquebrável: **a cada bug ou divergência detectada, fix sistêmico em código antes de prosseguir — zero workaround, zero `kubectl patch` ad-hoc, zero `--set` lateral**. Cada bug vira código + teste de regressão + commit isolado + entry de CHANGELOG. O resultado é um relatório técnico que prova GO/NO-GO para release pré-Series-A com matriz `comando × {coberto / falha / gap}` 100% preenchida e zero asterisco.

## Context

### O que existe hoje

A sessão anterior (commits `a1220eb2`, `08ead881`) entregou:
- Phase 0 (preflight) ✅ — 10/10 checks <30s, infra/tests/dogfood/preflight.sh idempotente
- Phase 1 (baseline frontend+backend) ✅ parcial — HTTP 200 + TLS via api Express, 10 fixes sistêmicos em produção (CHG-2026-04-29-DOGFOOD-FB)
- Phase 6 read-only ✅ parcial — infra/tests/dogfood/cli-coverage.sh harness com 33 commands invocados live, 1 FAIL (admin tenants evict --json) + 3 achados UX

### O que ainda falta

- **33 comandos mutating** (project create/delete, scale, promote, rollback, secret set/delete, domain add/remove, env create/delete, team add/remove/role, connect/disconnect, preview delete, login, logout, init) ainda não exercitados em dogfood real. cli-coverage.sh marca como SKIP.
- **Static frontend Prova de Vida** (web app no monorepo) — `static_delivery.go` retorna nil quando CF KV não configurado (gap F7 do triage anterior). Em standalone-multi sem CF Worker, frontend nunca é servido.
- **Failure modes do spec §12** (invalid token, quota exceeded, build failure, deploy failure) não exercitados.
- **Lifecycle ops em sequência real** (deploy → scale → promote → rollback → cancel → delete) com Prova de Morte verificável (namespace gone, ArgoCD app removed, S3 prefix limpo).

### Evidência motivadora

Histórico desta sessão demonstra que dogfood disciplinado encontra bugs reais que cobertura unitária não pega:

| Bug | Discovered via | Production impact |
|---|---|---|
| NetworkPolicy aponta para `loopzero-system` mas Traefik vive em `traefik` | Phase 1 dogfood (HTTP 502) | **S0 — afeta dev, prod, standalone-multi** |
| Manifest builder fail-loud em frontend static | Phase 1 build pipeline | S0 — multi-app misto não funciona |
| CLI ignora LOOPZERO_TOKEN env var em deploy | Phase 1 deploy | S0 — CI/CD quebrado |
| S3 silent-success em local-dev (200 OK com upload falhando) | Phase 1 build pipeline | S0 — Rule 10 violation |
| MINIO_ENDPOINT sem scheme | Config drift Phase 1 | S1 — opaque error |
| 6 outros achados | Phase 0 + Phase 1 | S0/S1 mix |

Cobertura unitária + componente é necessária mas insuficiente. Dogfood é o **único** método que detecta:
- Drift entre código e helm/values (achado #8 issuer name)
- Silent-success em branches local-dev (achados #2, #4)
- Acoplamento entre namespaces (achado #1 NetworkPolicy)
- Contracts UX (Rule 5 violation em admin tenants evict --json)

### Por que "real e2e" não é o mesmo que "Phase 6 harness"

Phase 6 harness (cli-coverage.sh) chama cada comando isolado e valida 3 contratos. Útil mas insuficiente — comandos mutating exigem **estado pré-existente**:
- `loopzero scale` precisa de deploy ativo
- `loopzero promote` precisa de release em staging
- `loopzero rollback` precisa de 2+ releases
- `loopzero secret unset` precisa de secret prévio
- `loopzero project delete` é Prova de Morte de tudo acima

Dogfood real é a **sequência canônica de jornadas user-real** com cleanup determinístico entre cada uma.

## Objective

`bash infra/tests/dogfood/full-loop.sh` executa em sequência todas as 9 jornadas mutating do spec, cada uma com Prova de Vida (HTTP 200 + TLS + audit log) e Prova de Morte (cleanup ≤5min), retornando exit 0 sse 100% das jornadas passam SEM nenhum workaround durante a corrida.

Metas mensuráveis:
1. **44/44 comandos exercitados** — read-only (33) + mutating (11 jornadas que invocam os 33 restantes em sequência).
2. **Zero workarounds durante o loop** — qualquer divergência mata o loop, vira issue, fix em código, commit isolado, regression test, retry.
3. **Prova de Morte verificável** — cada jornada limpa ≤5min, deixa cluster idêntico ao estado pré-jornada (idempotência verificada por preflight rerun).
4. **Static frontend HTTP 200** — F7 (static_delivery silent-success) resolvido via fix de produto, não workaround.
5. **3 failure modes spec §12** validados explicitamente — invalid token (exit 4), build failure (exit 2), deploy failure (exit 3).
6. **Audit log completo** — toda mutação aparece em `loopzero audit` com action + actor + target + timestamp.
7. **Rule 3/4/5 zero violações** após Phase 6 expandido (incluir mutating).
8. **Relatório técnico FAANG-grade** publicado em `docs/dogfood/2026-04-29-real-e2e.md` com matriz 100% preenchida (zero N/A vazio), GO/NO-GO honesto.

## ADRs

### D1 — Fix-systemic-on-error é absoluto: zero workarounds durante o loop

**Decision:** No instante que o dogfood detecta divergência entre comportamento real e spec — qualquer divergência: bug, drift de config, mensagem com termo interno, exit code errado, JSON quebrado — o loop **para**, abre issue, fix em código (operator/api/cli/helm/values), commit isolado, regression test, depois reinicia. Nenhum `kubectl patch`, nenhum `helm upgrade --set` lateral, nenhum sed em /tmp, nenhum env var temporário injetado no shell.

**Rationale:** A sessão anterior (CHG-DOGFOOD-FB) provou empiricamente que cada workaround mascara um bug que vai morder em produção. Dos 10 fixes daquela sessão, 5 só foram detectados porque alguém **insistiu em não dar workaround**. ADR D5 do plano anterior já formalizava isso, mas precisa virar princípio inquebrável aqui.

**Consequences:** Loop é interrompido com frequência. Aceito — cada interrupção é valor entregue. Velocidade é lateral à rigorosidade. Mitigação: cada interrupção produz issue + commit + retry, então a próxima iteração já vai mais longe.

### D2 — Mutating commands rodam em jornadas, não isolados

**Decision:** Comandos mutating não são testados como unidades; são testados como **sequências canônicas** (jornadas) com setup → mutation → assertion → teardown. Cada jornada tem precondição clara, observable result, e Prova de Morte determinística.

**Rationale:** `loopzero rollback --to v1` sem 2 releases prévios é teste vazio. `loopzero scale --replicas 5` sem deploy é teste vazio. Jornadas refletem como user real usa o produto. Phase 6 harness já cobre invocação isolada (parser/JSON/exit code); jornadas cobrem semântica.

**Consequences:** Suite mais demorada (cada jornada = build cycle real, ~10min). Aceito — alternativa (mock toda Hidra Argo+ArgoCD+Gitea) é miragem que não detecta os bugs que importam.

### D3 — Cada jornada tem Prova de Vida + Prova de Morte explícitas

**Decision:** Toda jornada que cria estado deve terminar limpando esse estado. Prova de Vida = HTTP 200 + audit log entry. Prova de Morte = namespace removido + ArgoCD app gone + S3 prefix limpo + DNS entry purgado (CF KV) + `preflight.sh` re-passa após cleanup.

**Rationale:** Sem cleanup determinístico, jornada N+1 herda lixo de jornada N e debug fica impossível. Sem Prova de Morte verificada, "delete vazado" só aparece em produção (refund nightmare).

**Consequences:** Cada jornada paga custo de cleanup. Aceito — é o produto real, e cleanup é feature, não overhead.

### D4 — Failure modes são fluxos primários, não asterisco

**Decision:** Spec §12 lista 5 failure modes. Cada um é uma jornada explícita com setup propositalmente quebrado (token expirado, Dockerfile inválido, manifest com erro, plan cap excedido, source upload falha). Asserção é mensagem **product-language** + exit code correto + audit entry.

**Rationale:** Failure path costuma ter mensagem com termo interno (Rule 3 violation) ou silent-success. Sem testar explicitamente, o produto pode "funcionar" no happy path e quebrar quando user erra — pior UX possível.

**Consequences:** 5 jornadas adicionais (~50min suite). Aceito.

### D5 — Static frontend exige fix de produto, não workaround

**Decision:** F7 (gap `static_delivery.go` retornando nil em local-dev quando CF KV ausente) bloqueia Prova de Vida do frontend. Resolução é fix de produto, não inventar IngressRoute manual em standalone-multi. Opções aceitáveis: (a) Traefik fallback dentro do api/operators que monta MinIO como S3 backend para static; (b) CF Worker rodando em local-dev (container); (c) deletar branch `if kvClient == nil` e exigir kvClient configurado em todos os ambientes (Rule 10 strict).

**Rationale:** Permitir "frontend não funciona em local mas funciona em prod" viola Rule 10 explicitamente. Workaround para destravar o teste mascara o gap, e a próxima pessoa que rodar o dogfood pisa no mesmo bug.

**Consequences:** Phase 2 não conclui sem decisão de produto + fix. Mitigação: ADR explícito + decisão tomada antes de Phase 2 começar.

### D6 — Cada bug encontrado vira commit isolado

**Decision:** Não bundlar fixes. Cada divergência detectada → 1 commit isolado contendo (a) fix em código, (b) regression test, (c) entry CHANGELOG `### Fixed`, (d) optional check em preflight.sh. Mensagem do commit cita o ID da issue e o flow que detectou.

**Rationale:** Bisect-friendly. Code review mais rápido. Histórico explica "por que" cada mudança aconteceu. Bundle commits ofusca causalidade.

**Consequences:** Mais commits no log (esperado: 10-30 commits durante o loop). Aceito — `git log --oneline` é o documento técnico mais rico.

### D7 — Loop é stateful entre iterações via dogfood_state.json

**Decision:** Estado entre iterações (qual jornada falhou, qual issue foi aberta, qual commit fechou) persiste em `~/.loopzero/dogfood_state.json`. Próxima iteração lê estado, retoma da jornada que falhou após confirmar fix.

**Rationale:** Loop pode rodar dias. Sem estado, perde-se onde parou. Estado também alimenta o relatório final.

**Consequences:** State file é dependência cruzada do harness. Mitigação: schema versionado, garantir que pode ser deletado para reset clean.

### D8 — Operator-only commands testados com token de service auth

**Decision:** §14 admin commands (admin tenants evict, admin loops, etc.) testados com `LOOPZERO_SERVICE_AUTH` real (não JWT user). Skipping é trapaceiro — operator commands são parte do produto e merecem mesma rigorosidade.

**Rationale:** F-CLI-1 (admin tenants evict --json retorna stdout vazio) só foi achado porque rodei o comando. Sem rodar, bug fica invisível.

**Consequences:** Suite precisa de `LOOPZERO_SERVICE_AUTH` configurado. Mitigação: preflight.sh adiciona check e seed-script gera o secret se ausente.

## Dependency Graph

```
Phase 0 (preflight expand)
    │
    ▼
Phase 1 (F7 static_delivery decision + fix)  ◀────── BLOCKER for full coverage
    │
    ▼
Phase 2 (full-loop.sh harness skeleton)
    │
    ├──▶ Phase 3 (Journey 1: Solo deploy + delete) ──▶ each phase below
    ├──▶ Phase 4 (Journey 2: Multi-app misto)
    ├──▶ Phase 5 (Journey 3: Secrets lifecycle)
    ├──▶ Phase 6 (Journey 4: Scale + plan caps)
    ├──▶ Phase 7 (Journey 5: Promote staging→prod)
    ├──▶ Phase 8 (Journey 6: Rollback)
    ├──▶ Phase 9 (Journey 7: Custom domain v2)
    ├──▶ Phase 10 (Journey 8: GitHub App auto-deploy)
    ├──▶ Phase 11 (Journey 9: Preview env)
    ├──▶ Phase 12 (Journey 10: Failure modes spec §12)
    └──▶ Phase 13 (Journey 11: Admin operator-only)
                            │
                            ▼
                     Phase 14 (Final report + matriz GO/NO-GO)
```

Phase 0/1 sequenciais (Phase 1 bloqueia tudo). Phases 3-13 ordenadas por dependência semântica:
- Phase 3 baseline; Phases 4-13 dependem dele estar verde.
- Phase 5 (secrets) precisa de Phase 3 (deploy ativo).
- Phase 7 (promote) precisa de Phase 5 (secret) ou pode ser stand-alone.
- Phase 8 (rollback) precisa de 2 releases — Phase 3 + uma re-deploy.

Não há paralelização real — fix-systemic-on-error (D1) torna paralelo perigoso (mesmo cluster, mesmo tenant).

---

## Phase 0: Pre-flight expansion (continuação)

**Objective:** estender `preflight.sh` para cobrir os 5 achados desta sessão como checks contínuos + adicionar verificação de `LOOPZERO_SERVICE_AUTH`.

### T0.6 — Verificar NetworkPolicy `allow-ingress-traefik` aponta para namespace correto

#### Objective
preflight.sh detecta se NetworkPolicy gerada por operator está usando `traefik` (correto, pós-fix) ou `loopzero-system` (pré-fix, S0 production bug).

#### Evidence
Achado desta sessão (commit `a1220eb2`): operator gerava ingress policy apontando para `loopzero-system` mas Traefik vive em `traefik`. Causou HTTP 502 em todo deploy. Preflight precisa pegar essa regressão se algum dia voltar.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — adicionar check_networkpolicy_traefik_target()
```

#### Deep file dependency analysis
- `preflight.sh`: já tem padrão `check_*` → record. Adicionar nova função na seção T0.3.
- Depende do tenant `tn-e2etest` ter um Environment ativo (criado pelo seed script anterior). Se não, pula com SKIP record.

#### Deep Dives
- **CEL-free check**: `kubectl get networkpolicy allow-ingress-traefik -n loopzero-tn-e2etest-production -o jsonpath='{.spec.ingress[0].from[0].namespaceSelector.matchLabels.kubernetes\.io/metadata\.name}'`
- **Expected**: `traefik`
- **Edge case**: Environment ainda não reconciliada → namespace não existe → record SKIP, não FAIL.

#### Tasks
1. Adicionar `check_networkpolicy_traefik_target` em preflight.sh.
2. Pula com SKIP se namespace tenant não existe.
3. FAIL se valor != `traefik`.

#### TDD
```
RED:    test_preflight_fails_when_networkpolicy_targets_loopzero_system() — patch np para loopzero-system, preflight detect
RED:    test_preflight_skips_when_no_tenant_namespace() — sem ns, retorna SKIP
GREEN:  Implementar function
REFACTOR: None
VERIFY: bash infra/tests/dogfood/preflight.sh && grep T0.6 /tmp/preflight.json | jq -r .status == "PASS"
```

#### Acceptance Criteria
- [ ] Function detecta drift correto (loopzero-system → FAIL, traefik → PASS)
- [ ] Skip gracioso quando namespace ausente
- [ ] Tempo total preflight ainda <30s

#### DoD
- [ ] Function comitada
- [ ] Passa em cluster atual (regression test rodado live)

### T0.7 — Verificar `LOOPZERO_SERVICE_AUTH` secret disponível

#### Objective
preflight.sh detecta se admin commands são testáveis (`LOOPZERO_SERVICE_AUTH` env var ou Secret no cluster).

#### Evidence
ADR D8: admin commands não podem ser SKIP. Sem service auth, harness pula §14 silenciosamente.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — check_service_auth()
```

#### Deep Dives
- API tem `LOOPZERO_SERVICE_AUTH_SECRET` env (visto em loopzero-api deploy). Cliente externo precisa do **valor** desse secret.
- Em standalone-multi, valor é gerado pelo bootstrap (`loopzero-e2e-test-service-secret-1777424606` — random UUID).
- Pode ser exportado para `~/.loopzero/local-service-auth.txt` por seed script.

#### Tasks
1. preflight.sh detecta secret value via `kubectl -n loopzero-system get deploy loopzero-api -o jsonpath='{...LOOPZERO_SERVICE_AUTH_SECRET...}'`.
2. Salva em `~/.loopzero/local-service-auth.txt` (chmod 600).
3. record PASS com truncated hash, FAIL se ausente.

#### TDD
```
RED:    test_preflight_fails_when_no_service_auth() — remove env var, expect FAIL
GREEN:  Implementar
REFACTOR: None
VERIFY: bash preflight.sh && cat ~/.loopzero/local-service-auth.txt | wc -c > 32
```

#### Acceptance Criteria
- [ ] Service auth descoberto e salvo
- [ ] Permissão 0600 no arquivo
- [ ] Não loga valor em stdout

#### DoD
- [ ] Function comitada
- [ ] Phase 0 do relatório atualizada

### T0.8 — Verificar cluster issuer = `loopzero-ca-issuer` em standalone-multi

#### Objective
Detectar drift se alguém mudar `LOOPZERO_CLUSTER_ISSUER` env do loopzero-api.

#### Evidence
Achado #8 desta sessão: env tinha `letsencrypt-prod`, certs nunca emitiam, HTTP 502. Fix em values + live env.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — check_cluster_issuer()
```

#### Tasks
1. Read `LOOPZERO_CLUSTER_ISSUER` env do deploy loopzero-api.
2. Validar valor existe como ClusterIssuer no workload cluster.
3. FAIL se ClusterIssuer inexistente.

#### TDD
```
RED:    test_preflight_fails_when_issuer_missing()
GREEN:  Implementar
REFACTOR: None
VERIFY: bash preflight.sh
```

#### Acceptance Criteria
- [ ] Detecta issuer drift
- [ ] Output legível em modo human

#### DoD
- [ ] Comitado

---

## Phase 1: Static Frontend (F7) — fix-systemic, não workaround

**Objective:** F7 (`static_delivery.go` silent-success quando CF KV ausente) impede Prova de Vida do frontend em standalone-multi. Resolver por fix de produto antes de prosseguir.

### T1.1 — Decisão arquitetural via meeting (D5)

#### Objective
Convocar meeting (Meeting Orchestrator) com platform-architect + production-design-reviewer + dx-engineer para decidir fix de produto.

#### Evidence
- F7 do triage: `static_delivery.go:170` retorna nil silenciosamente em local-dev.
- Rule 10 (CLAUDE.md): Local must equal production.
- 3 opções listadas em ADR D5: (a) Traefik fallback para MinIO, (b) CF Worker em container local, (c) deletar branch `if kvClient == nil`.

#### Files to edit
```
docs/decisions/ADR-0019-static-delivery-local-dev.md — (NEW) decision record formal
```

#### Deep Dives
- **Opção (a) Traefik+MinIO**: novo IngressRoute apontando para MinIO bucket via path-based routing. Pros: zero dependência externa. Cons: precisa de auth pré-signed URL ou public read em MinIO.
- **Opção (b) CF Worker local**: `cloudflared` ou `wrangler dev` rodando em sidecar. Pros: comportamento idêntico a prod. Cons: complexidade, port mgmt.
- **Opção (c) strict**: `kvClient` é dependência obrigatória, dev cluster precisa de mock CF KV (ex: container Cloudflare Workers Wrangler). Pros: alinha com Rule 10. Cons: força bootstrap mais pesado.

#### Tasks
1. Convocar meeting via `Skill: meeting`.
2. Produzir ADR-0019 com decisão final.
3. Implementação fica como T1.2.

#### TDD
```
N/A — decision task. ADR é o artefato.
VERIFY: docs/decisions/ADR-0019-static-delivery-local-dev.md existe + status APPROVED
```

#### Acceptance Criteria
- [ ] ADR escrito com 3 opções, prós/contras, decisão, justificativa
- [ ] Reviewers especificados
- [ ] Rollback plan documentado

#### DoD
- [ ] ADR APPROVED comitado
- [ ] Implementation plan handed-off para T1.2

### T1.2 — Implementar fix conforme ADR-0019

#### Objective
Aplicar a decisão da T1.1 em código, com testes regression.

#### Evidence
T1.1 conclui com decisão técnica. Esta tarefa é execução.

#### Files to edit
Depende de qual opção a ADR-0019 escolher. Hipótese inicial (assumindo opção (c) strict):
```
api/internal/services/static_delivery.go — remover branch `if kvClient == nil`, fail-loud
api/internal/services/static_delivery_test.go — atualizar testes
infra/helmfile/standalone-multi.yaml — declarar CF_KV_BACKEND=mock (ou similar)
infra/helmfile/scripts/bootstrap-mock-kv.sh — (NEW) provisiona mock KV em standalone-multi
```

#### Deep file dependency analysis
- `static_delivery.go:phaseBDeliver`: hoje retorna nil se kvClient nil. Pós-fix: returna error fatal.
- `static_delivery_test.go`: testes atuais cobrem nil-kvClient como happy-path. Atualizar.
- Helmfile / bootstrap: provê mock KV (ex: container nginx servindo `/kv/` como endpoint compatível).

#### Deep Dives
- **Mock KV strategy**: HTTP service que aceita PUT/GET/DELETE em `/kv/{key}` e persiste em filesystem ou Redis interno.
- **Migration path**: pre-existing standalone clusters precisam do mock provisionado antes do upgrade api.

#### Tasks
1. Implementar mock KV (se opção (c) ou (a)).
2. Update static_delivery.go conforme ADR.
3. Atualizar testes.
4. Update helmfile + bootstrap script.
5. Helm upgrade live.

#### TDD
```
RED:    test_static_delivery_fails_loud_without_kv_client() — antes do fix, retornava nil; após fix, retorna error
RED:    test_static_delivery_writes_via_mock_kv() — mock kv configurado, write succeed, GET via Worker retorna asset
RED:    test_static_delivery_phaseB_does_not_silent_skip() — explicit
GREEN:  Implementar fix
REFACTOR: Extract test fixture for mock KV setup
VERIFY: go test ./api/internal/services/ -run StaticDelivery
```

#### Acceptance Criteria
- [ ] Sem branch silent-success em production code
- [ ] Mock KV provisionado em standalone-multi
- [ ] HTTP 200 em web app deployado em standalone-multi
- [ ] code-audit complexity <= 10
- [ ] code-audit coverage >= 90% para static_delivery.go

#### DoD
- [ ] Fix comitado
- [ ] Live: dogfood-fb web responde HTTP 200 em standalone-multi
- [ ] Regression test em CI

---

## Phase 2: full-loop.sh harness skeleton

**Objective:** harness orquestrador que executa as 11 jornadas em sequência, com state persistence, retry após fix, e relatório incremental.

### T2.1 — Esqueleto `full-loop.sh` com state machine

#### Objective
Script idempotente que orquestra Phases 3-13 com state persistence em `~/.loopzero/dogfood_state.json`.

#### Evidence
ADR D7. Sem state, loop reinicia do zero a cada interrupção.

#### Files to edit
```
infra/tests/dogfood/full-loop.sh — (NEW) orquestrador
infra/tests/dogfood/journeys/ — (NEW) directory com 1 .sh por jornada
infra/tests/dogfood/journeys/_common.sh — (NEW) helpers compartilhados
```

#### Deep file dependency analysis
- `full-loop.sh`: novo. Lê state, decide qual jornada rodar próxima, invoca journey, atualiza state, decide retry vs continue.
- `_common.sh`: helpers (token regen, kubectl wait, curl com TLS+resolve, JSON parse, audit query).
- Cada `journey-NN-name.sh`: setup → mutate → assert → teardown.

#### Deep Dives
- **State schema** (`~/.loopzero/dogfood_state.json`):
  ```json
  {
    "version": "1.0",
    "started_at": "2026-04-29T16:00:00Z",
    "current_journey": 3,
    "journeys": [
      {"id": 1, "name": "preflight-expand", "status": "PASS", "duration_sec": 11},
      {"id": 3, "name": "solo-deploy-delete", "status": "FAIL", "blocker_issue": "...", "fix_commit": null},
      ...
    ],
    "issues_opened": ["F-J3-001", "F-J5-002"],
    "fixes_committed": ["a1220eb2", "08ead881"]
  }
  ```
- **Retry semantics**: state mostra `FAIL` + `blocker_issue` com `fix_commit=null` → loop bloqueia até `fix_commit` populado (humano ou script de finalize). Após fix, retoma de `current_journey`.
- **Idempotência**: cada journey deve poder rodar do zero (re-cria precondições se faltam).

#### Tasks
1. Criar diretório `journeys/`.
2. Escrever `_common.sh` com helpers.
3. Escrever `full-loop.sh` com state machine.
4. Schema validation do state file (opcional: jsonschema).

#### TDD
```
RED:    test_full_loop_resumes_from_state() — state com current_journey=5, full-loop deve pular 1-4
RED:    test_full_loop_blocks_on_fail_without_fix() — journey FAIL sem fix_commit, loop não continua
RED:    test_full_loop_clears_state_on_reset() — ./full-loop.sh --reset apaga state
GREEN:  Implementar
REFACTOR: Extract jq queries para functions
VERIFY: bash infra/tests/dogfood/full-loop.sh --dry-run
```

#### Acceptance Criteria
- [ ] State persistence funcional
- [ ] Retry semantics corretas
- [ ] `--reset` flag funcional
- [ ] `--dry-run` mostra próxima jornada sem rodar
- [ ] shellcheck clean

#### DoD
- [ ] Script comitado
- [ ] State file documentado em journeys/README.md

---

## Phase 3: Journey 1 — Solo deploy + Prova de Vida + Prova de Morte

**Objective:** baseline mínima (single-app server) deploy → HTTP 200 → delete → cluster idêntico ao início.

### T3.1 — `journey-01-solo-deploy.sh`

#### Objective
Cria projeto, deploy 1 app server, valida HTTP 200, deleta projeto, valida cleanup.

#### Evidence
Phase 1 da sessão anterior fez parte disso (api-only). Agora formaliza como suite, com Prova de Morte estrita.

#### Files to edit
```
infra/tests/dogfood/journeys/journey-01-solo-deploy.sh — (NEW)
infra/tests/dogfood/fixtures/solo-server/ — (NEW) projeto express minimal
```

#### Deep file dependency analysis
- Fixture: api Express (similar ao /tmp/dogfood-fb usado na sessão anterior, mas committed no repo).
- Journey: setup_workdir → loopzero init → loopzero deploy → wait HTTP 200 → audit assert → loopzero project delete --confirm → wait cleanup → preflight rerun.

#### Deep Dives
- **HTTP 200 polling**: até 5min (build cycle), exit 1 se timeout.
- **Prova de Morte**: 4 checks paralelos (namespace gone, ArgoCD app gone, S3 prefix limpo, MinIO bundle removed). Tempo limite 5min total.
- **Idempotência**: journey roda do zero se fixture project já existe.

#### Tasks
1. Criar fixture solo-server.
2. Implementar journey-01-solo-deploy.sh.
3. Adicionar entrada em `full-loop.sh`.

#### TDD
```
RED:    test_journey_01_creates_project()
RED:    test_journey_01_reaches_http_200()
RED:    test_journey_01_audit_log_has_deploy_entry()
RED:    test_journey_01_project_delete_cleans_namespace()
RED:    test_journey_01_project_delete_cleans_argocd_app()
RED:    test_journey_01_project_delete_cleans_s3_prefix()
RED:    test_journey_01_postcondition_preflight_clean()
GREEN:  Implementar journey
REFACTOR: Extract HTTP-200 wait helper para _common.sh
VERIFY: bash journey-01-solo-deploy.sh && [[ $? -eq 0 ]]
```

#### Acceptance Criteria
- [ ] HTTP 200 em <5min após loopzero deploy
- [ ] Audit entry com action=deploy, project=solo-server, actor=test-user
- [ ] Cleanup ≤5min após loopzero project delete
- [ ] Preflight clean após journey
- [ ] Zero workaround durante journey (D1)
- [ ] Zero kubectl patch
- [ ] code-audit complexity <= 10

#### DoD
- [ ] Journey passa green em standalone-multi
- [ ] Comitado
- [ ] Phase 3 do relatório atualizada

---

## Phase 4: Journey 2 — Multi-app misto (server + static frontend)

**Objective:** deploy api Express + web Vite SPA (após F7 fixed em T1.2). Valida ambos HTTP 200.

### T4.1 — `journey-02-multi-app.sh`

#### Objective
Cobre cenário real do produto: monorepo com server + static.

#### Evidence
Sessão anterior fez deploy mas web ficou pendente (F7). Após T1.2 fix, este journey valida ambos.

#### Files to edit
```
infra/tests/dogfood/journeys/journey-02-multi-app.sh — (NEW)
infra/tests/dogfood/fixtures/multi-app/ — (NEW) monorepo express + vite
```

#### Deep Dives
- Fixture sem packages/shared (evita F3 do triage anterior).
- Build path: api via container, web via static delivery (CF Worker mock após F7 fix).
- HTTP 200 em **ambos** os hosts.

#### Tasks
1. Criar fixture multi-app.
2. Implementar journey-02.
3. Validar build pipeline com `build-image-api` + `deliver-artifact-web` ambos Succeeded.

#### TDD
```
RED: test_journey_02_api_http_200()
RED: test_journey_02_web_http_200()
RED: test_journey_02_argocd_app_synced()
RED: test_journey_02_cf_kv_entry_for_web()
RED: test_journey_02_delete_cleans_both()
GREEN: Implementar
REFACTOR: None
VERIFY: bash journey-02-multi-app.sh
```

#### Acceptance Criteria
- [ ] api HTTP 200
- [ ] web HTTP 200 retornando index.html da Vite
- [ ] CF KV (mock) tem entry para subdomain do web
- [ ] Cleanup completo

#### DoD
- [ ] Journey passa green
- [ ] F7 confirmed fixed em produção (mock KV)

---

## Phase 5: Journey 3 — Secrets lifecycle

**Objective:** secret set → app vê env → secret delete → app não vê env.

### T5.1 — `journey-03-secrets.sh`

#### Objective
Cobre §9 do spec inteiro (set, list, delete).

#### Evidence
Phase 6 harness lista `secret list` mas não exercita set/delete (mutating).

#### Files to edit
```
infra/tests/dogfood/journeys/journey-03-secrets.sh — (NEW)
infra/tests/dogfood/fixtures/secret-app/ — (NEW) app que lê SECRET_PING
```

#### Deep Dives
- Fixture api Express com endpoint `/secret-check` retornando `process.env.SECRET_PING || "unset"`.
- Flow: deploy → secret set → re-deploy ou pod restart → curl /secret-check retorna valor → secret delete → re-deploy → curl retorna "unset".
- ESO sync: ExternalSecret CR criado, Secret materializado em namespace tenant, env var injetada.

#### Tasks
1. Criar fixture secret-app.
2. Implementar journey-03.
3. Verificar `loopzero secret list --json` shape.

#### TDD
```
RED: test_journey_03_secret_set_creates_externalsecret()
RED: test_journey_03_pod_sees_env_var()
RED: test_journey_03_secret_delete_removes_env()
RED: test_journey_03_list_shows_only_names_never_values()
GREEN: Implementar
REFACTOR: None
VERIFY: bash journey-03-secrets.sh
```

#### Acceptance Criteria
- [ ] Set/list/delete cobertos
- [ ] App vê env após set, não vê após delete
- [ ] List nunca expõe valor (Rule 9 spec §9)

#### DoD
- [ ] Journey green
- [ ] §9 commands marcados como exercised na matriz

---

## Phase 6: Journey 4 — Scale + plan caps

**Objective:** scale fixo + autoscaling, valida plan caps (Pro=5).

### T6.1 — `journey-04-scale.sh`

#### Objective
Cobre §3 scale + §13 billing/plan integration.

#### Evidence
CLAUDE.md: loopzero scale tem stub DB persistence; "use loopzero.yaml + deploy real". Journey precisa adaptar para isso.

#### Files to edit
```
infra/tests/dogfood/journeys/journey-04-scale.sh — (NEW)
```

#### Deep Dives
- Cenário 1: scale 1→3 réplicas (within Pro cap=5), expect deploy reflect.
- Cenário 2: scale 1→11 (over cap), expect 4xx + product message.
- Cenário 3: autoscaling (HPA) target_cpu=80%, min=2, max=5.

#### Tasks
1. Implementar journey-04 com 3 cenários.
2. Cada cenário fail-fast se behavior errado.

#### TDD
```
RED: test_journey_04_scale_within_cap_succeeds()
RED: test_journey_04_scale_over_cap_rejected()
RED: test_journey_04_hpa_created_when_autoscaling()
GREEN: Implementar
REFACTOR: None
VERIFY: bash journey-04-scale.sh
```

#### Acceptance Criteria
- [ ] Scale fixo aplicado em deploy
- [ ] Over-cap rejected com mensagem clara
- [ ] HPA criado quando autoscaling declarado

#### DoD
- [ ] Journey green

---

## Phase 7: Journey 5 — Promote staging → production

**Objective:** deploy em staging → promote para production → ambos hosts respondem com mesma versão.

### T7.1 — `journey-05-promote.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-05-promote.sh — (NEW)
```

#### Deep Dives
- Fixture: app com endpoint `/version` retornando build-id.
- Deploy staging → curl staging → version=A → loopzero promote → curl prod → version=A (mesma).
- State machine release: validating → committing → syncing → completed.

#### Tasks
1. Implementar.

#### TDD
```
RED: test_journey_05_promote_creates_prod_release_with_same_buildid()
RED: test_journey_05_state_machine_completes()
GREEN: Implementar
VERIFY: bash journey-05-promote.sh
```

#### Acceptance Criteria
- [ ] Promote bem-sucedido
- [ ] Staging + prod respondem mesma version

#### DoD
- [ ] Journey green

---

## Phase 8: Journey 6 — Rollback

**Objective:** deploy v1 → deploy v2 → rollback to v1 → app responde v1.

### T8.1 — `journey-06-rollback.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-06-rollback.sh — (NEW)
```

#### Deep Dives
- App com /version retornando file-version (incrementado entre deploys).
- v1, v2, rollback to v1, curl, expect v1.
- Audit entry para rollback action.

#### Tasks
1. Implementar.

#### TDD
```
RED: test_journey_06_rollback_reactivates_v1()
RED: test_journey_06_v2_marked_superseded()
RED: test_journey_06_audit_entry_has_rollback_action()
GREEN: Implementar
VERIFY: bash journey-06-rollback.sh
```

#### Acceptance Criteria
- [ ] HTTP retorna v1 após rollback
- [ ] v2 marcado SUPERSEDED no DB

#### DoD
- [ ] Journey green

---

## Phase 9: Journey 7 — Custom domain v2 (CF Zone Mgmt)

**Objective:** loopzero domain add → status pending_ns → simulate DNS update → status ns_active → provisioning → active.

### T9.1 — `journey-07-custom-domain.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-07-custom-domain.sh — (NEW)
```

#### Deep Dives
- Em standalone-multi: CF mock requirido (ADR D5 ou novo ADR). Possivelmente blocked por gap de produto.
- Plan B documented: skip with explicit gap se CF API real exigida.

#### Tasks
1. Investigar CF mock viability.
2. Implementar OR document gap formally.

#### TDD
```
RED: test_journey_07_domain_add_returns_pending_ns()
RED: test_journey_07_status_transitions_after_dns()
GREEN: Implementar OR explicit skip
VERIFY: bash journey-07-custom-domain.sh
```

#### Acceptance Criteria
- [ ] OR full validation OR gap documented in ADR

#### DoD
- [ ] Journey green OR gap formally documented

---

## Phase 10: Journey 8 — GitHub App auto-deploy

**Objective:** push commit → webhook → build → HTTP 200, em <4min.

### T10.1 — `journey-08-github-autodeploy.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-08-github-autodeploy.sh — (NEW)
```

#### Deep Dives
- Repo: `getloopscale/loopzero-repotest` (memory).
- Webhook: precisa de túnel para localhost (ngrok ou cloudflared).
- Auth: GitHub App `LoopZero Platform` (ID 3319955 per CLAUDE.md).

#### Tasks
1. Implementar com ngrok detection.
2. Skip with reason if tunnel unavailable.

#### TDD
```
RED: test_journey_08_webhook_triggers_build()
RED: test_journey_08_http_200_in_4min()
GREEN: Implementar
VERIFY: bash journey-08-github-autodeploy.sh
```

#### Acceptance Criteria
- [ ] Webhook recebido
- [ ] Build started <60s após push
- [ ] HTTP 200 final em <4min

#### DoD
- [ ] Journey green OR gap (no tunnel) documented

---

## Phase 11: Journey 9 — Preview environments

**Objective:** open PR → preview env created → HTTP 200 → close PR → cleanup.

### T11.1 — `journey-09-preview.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-09-preview.sh — (NEW)
```

#### Deep Dives
- Depende de journey-08 (GitHub App configurado).
- PreviewEnvironment CR + namespace `preview-{tenant}-{project}-{prnumber}`.
- TTL expiry forced via annotation patch.

#### Tasks
1. Implementar.

#### TDD
```
RED: test_journey_09_pr_creates_preview_namespace()
RED: test_journey_09_isolation_primitives()
RED: test_journey_09_ttl_cleanup()
GREEN: Implementar
VERIFY: bash journey-09-preview.sh
```

#### Acceptance Criteria
- [ ] Preview namespace criada
- [ ] NetworkPolicy + ResourceQuota + Certificate aplicados
- [ ] HTTP 200 no host preview
- [ ] TTL expiry → cleanup completo

#### DoD
- [ ] Journey green

---

## Phase 12: Journey 10 — Failure modes spec §12

**Objective:** explicitly test 5 failure modes do spec, cada um produz mensagem product-language + exit code correto.

### T12.1 — `journey-10-failures.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-10-failures.sh — (NEW)
```

#### Deep Dives
5 sub-jornadas:
1. **Invalid token** — `LOOPZERO_TOKEN=garbage loopzero whoami` → exit 4
2. **Quota exceeded** — Pro plan, criar 4º projeto → 403
3. **Build failure** — Dockerfile com `RUN false` → exit 2 + audit entry
4. **Deploy failure** — manifest com PVC inválido → exit 3
5. **Source upload silent-success regression** — verificar S3 fail agora retorna 5xx (já fix anterior, regression check)

#### Tasks
1. Implementar 5 sub-jornadas.

#### TDD
```
RED: test_journey_10_REDACTED_SECRET_063_exit_4()
RED: test_journey_10_quota_exceeded_403()
RED: test_journey_10_build_failure_exit_2()
RED: test_journey_10_deploy_failure_exit_3()
RED: test_journey_10_source_upload_no_silent_success()
GREEN: Implementar
VERIFY: bash journey-10-failures.sh
```

#### Acceptance Criteria
- [ ] 5/5 failure modes corretos
- [ ] Mensagens product-language (Rule 3)
- [ ] Exit codes corretos (Rule 4)

#### DoD
- [ ] Journey green
- [ ] §12 spec coverage 100%

---

## Phase 13: Journey 11 — Admin operator-only

**Objective:** test §14 admin commands com LOOPZERO_SERVICE_AUTH (não pular).

### T13.1 — `journey-11-admin.sh`

#### Files to edit
```
infra/tests/dogfood/journeys/journey-11-admin.sh — (NEW)
```

#### Deep Dives
- ADR D8: admin commands não podem ser SKIP.
- F-CLI-1 (admin tenants evict --json stdout vazio) deve ser FIX antes do journey passar.

#### Tasks
1. Implementar admin tenants evict --dry-run.
2. Implementar outros admin commands se houver.

#### TDD
```
RED: test_journey_11_admin_evict_dry_run_returns_json()
RED: test_journey_11_admin_evict_no_yes_does_not_mutate()
GREEN: Implementar (após F-CLI-1 fix em código)
VERIFY: bash journey-11-admin.sh
```

#### Acceptance Criteria
- [ ] admin tenants evict --dry-run --json retorna JSON válido
- [ ] §14 commands cobertos

#### DoD
- [ ] Journey green
- [ ] F-CLI-1 fix comitado

---

## Phase 14: Final report + matriz GO/NO-GO

**Objective:** consolidar resultado em relatório FAANG-grade, emitir veredito.

### T14.1 — Relatório final

#### Files to edit
```
docs/dogfood/2026-04-29-real-e2e.md — (NEW) relatório consolidado
```

#### Tasks
1. Tabela 44 comandos × {coberto, gap, falha} — zero N/A.
2. Tabela 14 seções funcionais × resultado.
3. Lista cronológica de bugs encontrados (com commits).
4. Lista de PRs upstream em loopzero-stacks.
5. GO/NO-GO veredito honesto com evidência.

#### Acceptance Criteria
- [ ] Matriz 100% preenchida (zero N/A vazio)
- [ ] Cada gap tem owner/follow-up
- [ ] Veredito apoiado em evidências

#### DoD
- [ ] Relatório comitado
- [ ] Push para develop
- [ ] Tagged como dogfood-real-e2e-v1

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Preflight pega regression NetworkPolicy | T0.6 | check_networkpolicy_traefik_target |
| 2 | Preflight pega ausência LOOPZERO_SERVICE_AUTH | T0.7 | check_service_auth |
| 3 | Preflight pega cluster issuer drift | T0.8 | check_cluster_issuer |
| 4 | F7 static_delivery silent-success em local-dev | T1.1, T1.2 | ADR-0019 + fix produto |
| 5 | Orquestração de jornadas com state | T2.1 | full-loop.sh |
| 6 | Solo deploy + Prova de Vida + Prova de Morte | T3.1 | journey-01 |
| 7 | Multi-app misto (server + static) | T4.1 | journey-02 |
| 8 | Secrets lifecycle (set/list/delete) | T5.1 | journey-03 |
| 9 | Scale + plan caps + autoscaling | T6.1 | journey-04 |
| 10 | Promote staging→prod | T7.1 | journey-05 |
| 11 | Rollback v2→v1 | T8.1 | journey-06 |
| 12 | Custom domain v2 | T9.1 | journey-07 |
| 13 | GitHub App auto-deploy | T10.1 | journey-08 |
| 14 | Preview environments lifecycle | T11.1 | journey-09 |
| 15 | Failure modes spec §12 (5 cenários) | T12.1 | journey-10 |
| 16 | Admin operator-only commands | T13.1 | journey-11 |
| 17 | F-CLI-1 admin tenants evict --json broken | T13.1 setup | fix em cli antes do journey |
| 18 | F-CLI-2 platform status leaks kubectl error | T6.1 fix | normalize stderr |
| 19 | F-CLI-3 logs (no builds) silent exit 1 | T3.1 fix | product-language message |
| 20 | Final matriz + GO/NO-GO | T14.1 | relatório consolidado |
| 21 | Zero workarounds enforced | D1 + all journeys | journey aborta em divergência |
| 22 | Cada bug = 1 commit isolado | D6 + all phases | git log linear |
| 23 | State persistence cross-iteration | D7 + T2.1 | dogfood_state.json |
| 24 | Audit log validation por mutação | D3 + journeys 3-11 | each journey asserts audit entry |
| 25 | Rule 3/4/5 zero violation post-fix | journeys 3-13 | harness Phase 6 + journeys |
| 26 | Static frontend HTTP 200 | T1.2 + T4.1 | F7 fix + journey-02 |
| 27 | 44/44 commands exercised | journeys 1-11 | matriz |

**Coverage: 27/27 requirements covered (100%).**

## Global Definition of Done

- [ ] All 14 phases completed
- [ ] All TDD red tests green
- [ ] preflight.sh expandido com 3 novos checks (T0.6/T0.7/T0.8)
- [ ] ADR-0019 APPROVED + fix produto comitado
- [ ] full-loop.sh roda 11 journeys end-to-end exit 0
- [ ] **Zero `kubectl patch` ad-hoc no histórico do loop** (verificável via `git log --grep="patch\|hack\|workaround" --since=loop-start`)
- [ ] Cada bug detectado tem commit isolado (D6)
- [ ] CHANGELOG.md atualizado com `### Fixed` para cada bug
- [ ] Relatório final em `docs/dogfood/2026-04-29-real-e2e.md` com matriz 100% preenchida
- [ ] Veredito GO/NO-GO honesto com evidências
- [ ] 44/44 comandos CLI exercitados (read-only via cli-coverage + mutating via journeys)
- [ ] 5/5 failure modes spec §12 validados
- [ ] Audit log entry assertions em todas as jornadas mutating
- [ ] cli-coverage.sh PASS sem FAILs
- [ ] preflight.sh re-passa após cada Prova de Morte (idempotência)
- [ ] Push para develop, tagged dogfood-real-e2e-v1
