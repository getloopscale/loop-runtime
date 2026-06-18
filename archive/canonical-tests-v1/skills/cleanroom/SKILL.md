---
name: cleanroom
description: |
  Cleanroom Bootstrap — destroi a infra k3d local e a recria do zero, deixando o
  cluster em estado pristine para auditoria rigorosa (/dogfood) ou validacao live de
  mudancas que nao podem confiar em state residual. Sequencia canonica:
  task clean:all -> task setup -> tilt up (background) -> task doctor (gate 38/0).
  Tempo total: ~30-45min. Use ANTES de /dogfood ou de qualquer validacao que
  precise garantir que nenhum estado de teste anterior contamina o resultado.
user-invocable: true
allowed-tools: Read, Grep, Glob, Bash
argument-hint: "[--env=dev-public|drill|prod] [--skip-tilt | --no-doctor | --skip-baseline — opcional, padrao executa tudo]"
---

# Cleanroom Bootstrap

Voce e um **operador de infra que recria o ambiente local k3d do zero**. Nao audita,
nao testa o produto, nao gera relatorio. Sua unica responsabilidade e produzir uma
infra pristine pronta para outras skills (/dogfood, /test, validacao manual)
operarem em cima.

## Quando usar

- Antes de `/dogfood full` quando o estado warm pode mascarar problemas reais
- Apos commits que mudam infra (Helm charts, operators, migrations) e voce quer
  ver o efeito do zero
- Apos longas sessoes de desenvolvimento que poluiram o cluster (orphans, builds
  stuck, JWTs expirados, multi-tenant memberships)
- Em loops de regressao (Ralph loops) onde cada iteracao precisa de pristine baseline

## Quando NAO usar

- Se voce esta no meio de debug ativo (cleanroom destroi state que pode conter pistas)
- Se a sessao tem trabalho nao-commitado em `~/.loopzero` ou em PVCs (cleanroom
  destroi tudo que nao foi gitado)
- Se ja rodou `/cleanroom` ha menos de 30min sem mudanca relevante

## Filosofia

**Pristine > rapido.** O custo de 30-45min e o preco da confianca em qualquer
relatorio que rode em cima. Se voce nao tem 30-45min, voce nao tem orcamento
para rigor — assuma o trade-off explicitamente e siga em frente sem cleanroom.

## Sequencia canonica

A skill executa **6 fases sequenciais** (T1.2 of plan
`2026-06-08-pre-release-test-rigor` adicionou Fase 0 + Fase 5 ao redor das
4 fases historicas). Cada fase tem gate de saida claro. Falha em qualquer
fase aborta a sequencia (NAO mascara erro continuando).

Quando `--skip-baseline` flag e passado, Fase 0 + Fase 5 sao puladas — back-compat
escape hatch para callers legacy que nao precisam do drift gate. Default executa
todas as 6 fases.

### Fase 0 — Baseline pre-rebuild (~10-30s) — T1.2 of pre-release-test-rigor

```bash
bash infra/scripts/local/baseline-capture.sh --output /tmp/baseline-pre-rebuild.json
```

O que faz:
- Captura snapshot 4-layer do estado atual (pre-destroy) per
  `.claude/rules/steady-state-baseline.md`
- Layer 1 code state: HEAD + branch + ahead/behind
- Layer 2 runtime state: pods + helm releases + ArgoCD apps + tenant CRs
- Layer 3 config state: PG migrations + plan caps + cosign mode
- Layer 4 LIVE probes: 6 HTTP curl checks contra dev-public
- Volatile fields (metadata.resourceVersion, generation, managedFields,
  creationTimestamp, uid, status.conditions[].lastTransitionTime)
  normalizados via EC-7 absorption

Gate de saida: arquivo `/tmp/baseline-pre-rebuild.json` existe e contem 4 layers
estruturados. Skip quando `--skip-baseline` passado.

### Fase 1 — Destruir infra existente (~3-5min)

```bash
task clean:all
```

O que faz:
- Roda `task clean` (clean-incremental.sh — remove builds + tenants + projects orfaos)
- Deleta os 4 clusters k3d (mgmt + build + workload + runtime-2) via `task clusters:delete`
- Remove o registry `k3d-loopzero-registry`

Gate de saida: `k3d cluster list` retorna vazio para clusters `loopzero-*`.

### Fase 2 — Recriar infra (~15-20min)

```bash
task setup
```

O que faz (idempotente):
- Cria 4 clusters k3d com rede partilhada
- Instala Helm dependencies (cert-manager, gitea, postgresql, minio, argocd, harbor, etc.)
- Aplica CRDs do LoopZero
- Roda `bash infra/helmfile/scripts/seed-local-user.sh` (mint JWT inicial + seed
  bootstrap user/tenant) — JA INCLUI o strip de pre-membership do bootstrap (commit `e0634f4f`)

Gate de saida: `task setup` retorna exit 0.

### Fase 3 — Iniciar dev loop (~5-10min)

```bash
tilt up
```

`tilt up` e um processo de longa duracao (loop de hot-reload). NAO bloqueia
ate o servidor encerrar. Estrategia:

```bash
# Dispatch detached (sobrevive ao agente):
nohup setsid tilt up > /tmp/cleanroom-tilt.log 2>&1 &
disown

# Esperar tilt construir + deployar API/operators
# Tipicamente 5-10min em rede media

# Poll tilt readiness via task doctor (proxima fase)
```

Componentes que tilt sobe:
- loopzero-api (Go, mgmt cluster, port-forward localhost:13001)
- 4 split operators (tenant, app, preview, platform)
- Migrations runner
- Image hot-reload via k3d registry

Gate de saida: `task doctor` reporta loopzero-api Running (proxima fase).

### Fase 4 — Health check (gate 38/0)

```bash
task doctor
```

O que faz:
- 38+ checks: clusters, pods criticos, API health, tokens, Ory Auth, ESO, ArgoCD,
  Argo Workflows, operators split-binary, etc.

Gate de saida (para considerar cleanroom OK):
- `PASS: 38` (numero exato pode variar +/-, ajustar para a baseline atual)
- `FAIL: 0`
- WARN aceitavel apenas para optional tools (golangci-lint, gotestsum, ginkgo CLI)

Se `task doctor` reporta FAIL > 0, **NAO declarar cleanroom OK**. Investigar
qual check falhou. Cenarios comuns:
- API pod ainda nao subiu: re-rodar `task doctor` apos 1-2min
- ArgoCD repo creds nao propagaram: rodar `task k3d:heal` (codifica H1-H15)
- Tilt ainda nao buildou imagens: tail `/tmp/cleanroom-tilt.log` para diagnose

### Fase 4.5 — `task check` sprint canonical (~30-60s) — T4.2 of cleanroom-canonical-5-defense-layers

```bash
bash infra/scripts/local/check-sprint.sh --fail-fast
```

O que faz: aggregates 8 check categorias (c1 helmfile + c2 configmaps + c3 networking + c4 auth + c5 webhook + c6 observability + c7 integrity + c8 cleanroom-guards) com ~44 probes totais. Per ADR D3, COMPLEMENTAR a `task doctor` (Phase 4): doctor valida K8s infra; check valida LoopZero canonical state.

**Por que separado de Phase 4 (`task doctor`):**
- `task doctor` é genérico K8s health (38+ checks pod/svc/api).
- `task check` é LoopZero-specific canonical: helmfile overlays declaratively shipped (Phase 1), task chain extensions wired (Phase 2), hermetic gates GREEN (Phase 3), Cleanroom guards healthy.

**Gate de saída**: `FAIL=0`. Per ADR D4 + D9, Cleanroom invoca com `--fail-fast` (canonical) — qualquer FAIL aborta e escala BLOCKED. Setup chain (não-Cleanroom) invoca com warn-and-continue.

**Wire**: Cleanroom Phase 4.5 invoca diretamente. `task setup` wraps com `task check || echo "[WARN]; true"`.

Se `task check` reporta FAIL > 0, **NAO declarar cleanroom OK**. Investigar qual categoria falhou via:
- `bash infra/scripts/local/check-sprint.sh --category=c<N>` (isolado)
- `bash infra/scripts/local/check-sprint.sh --json` (machine-readable)

### Fase 5 — Baseline post-rebuild + diff gate (~30-60s) — T1.2 of pre-release-test-rigor

```bash
bash infra/scripts/local/baseline-capture.sh --output /tmp/baseline-post-rebuild.json
bash infra/scripts/local/baseline-diff.sh \
  /tmp/baseline-pre-rebuild.json \
  /tmp/baseline-post-rebuild.json \
  --markdown > /tmp/baseline-diff.json 2> /tmp/baseline-diff.md
```

O que faz:
- Captura snapshot 4-layer do estado pos-rebuild (apos `task setup` + tilt up + doctor)
- Compara pre vs post via `baseline-diff.sh`:
  - Drift classificado como **additive** (apenas em B, OK), **missing** (apenas
    em A, FAIL), **mismatch** (mesma chave, valor diferente, FAIL), ou
    **identical** (OK silente)
- Volatile fields normalizados em ambas as captures (EC-7 absorption)

Gate de saida (HARD):
- `baseline-diff.sh` exit 0 → verdict OK → cleanroom declarada SUCESSO
- `baseline-diff.sh` exit 1 → verdict FAIL → cleanroom **aborta com exit 1**
  e operador investiga `/tmp/baseline-diff.md` para entender drift

Skip quando `--skip-baseline` passado — back-compat para callers legacy. O
operador assume entao o trade-off de nao ter o drift gate explicito.

## Output da skill

Ao final, emitir relatorio compacto:

```markdown
# Cleanroom Bootstrap — STATUS

**Data:** YYYY-MM-DDTHH:MM:SSZ
**Duracao total:** Xm Ys

## Fases

| # | Fase | Status | Duracao | Gate |
|---|------|--------|---------|------|
| 1 | task clean:all | PASS/FAIL | Xs | k3d loopzero-* clusters removed |
| 2 | task setup | PASS/FAIL | Xs | exit 0 |
| 3 | tilt up (background) | PASS/FAIL | Xs | tilt PID alive + image build complete |
| 4 | task doctor | PASS/FAIL | Xs | PASS=N FAIL=0 |

## Cleanroom invariants

- [ ] 4 clusters k3d Ready
- [ ] loopzero-api Running em mgmt
- [ ] 4 split operators Running
- [ ] BSL/Schedules pristine (zero backups historicos)
- [ ] Bootstrap user single-tenant (loopzero-bootstrap em apenas tn-e2etest)
- [ ] DB sem projetos orfaos (projects table = 0 ou apenas seed)

## Proximos passos

Cleanroom OK -> agora invoque a skill que precisa de pristine state:
  /dogfood full       # auditoria full (mandatory fresh ginkgo)
  /to-plan {topic}    # planejamento sobre baseline limpo
  Ralph loop          # ciclo de regressao iterativo

Cleanroom FAIL -> ler /tmp/cleanroom-tilt.log + task doctor output, abrir issue.
```

## Argumentos opcionais

- `--env=<env>` — qual ambiente exercitar (T7.1 of plan
  2026-06-04-dev-public-domain-canonicalization). Valores: `dev-public`
  (default), `drill`, `prod`. Plumb via `LOOPZERO_HELMFILE_ENV=$env` em
  todos os subprocess (helmfile, kubectl context, curl URLs).

  Default `dev-public` exercita auth-dev/app-dev/cloud-dev.loopzero.example
  via Caddy local. Para usar legacy localhost-only, passar
  `--env=standalone-multi`.

  **Plumbing concreto por env**:

  ```bash
  # --env=dev-public (default canonical)
  task clean:all
  task dev-public:dns                                  # extra dev-public Phase 0
  task dev-public:caddy                                # extra dev-public Phase 1
  LOOPZERO_HELMFILE_ENV=dev-public task setup              # helmfile env override
  LOOPZERO_HELMFILE_ENV=dev-public tilt up --hud           # Tilt env override
  task dev-public:patch-kratos --verify                # regression-defense
  task doctor
  task dev-public:trio                                 # validation (15s)

  # --env=standalone-multi (legacy localhost)
  task clean:all
  task setup                                           # default env
  tilt up
  task doctor

  # --env=drill (DOKS prod-parity)
  task clean:all
  task drill:provision                                 # Pulumi up
  LOOPZERO_HELMFILE_ENV=drill task setup
  task doctor
  ```
- `--skip-tilt` — pular Fase 3 (ex: cluster + helm OK, mas tilt nao precisa
  rodar para o teste subsequente). Util para validacoes que nao tocam API/operators.
- `--no-doctor` — pular Fase 4 (ex: voce vai rodar uma sequencia customizada
  de health check). Nao recomendado.

## Anti-Patterns

1. **NAO use durante /dogfood.** A Regra Suprema do dogfood proibe alteracoes na
   infra. /cleanroom deve rodar ANTES, nao DURANTE. Se /dogfood ja foi invocado,
   complete-o (ou aborte) primeiro.

2. **NAO pule Fase 1 ("ja esta limpo o suficiente").** Se voce esta invocando
   /cleanroom, e porque desconfia do estado atual. Pular destruicao = nao e cleanroom.

3. **NAO declare OK com FAIL > 0.** task doctor e o gate. Se um check falha, o
   cleanroom nao convergiu. Investigar antes de prosseguir.

4. **NAO rode em paralelo com `/dogfood` ou outros consumidores de state.** Os
   processos vao competir pelo cluster e mascarar erros mutuamente.

5. **NAO automatize cleanroom em loop sem supervisao.** Ele consome ~30-45min,
   recursos de disco (imagens, PVCs), e pode falhar por motivos de rede/registry.
   Operador deve estar disponivel para diagnose.

## Erros comuns e recovery

| Sintoma | Causa provavel | Recovery |
|---------|----------------|----------|
| `task clean:all` falha em deletar registry | container ainda em uso | `docker stop k3d-loopzero-registry; docker rm -f k3d-loopzero-registry` |
| `task setup` trava em "waiting for postgres" | helmfile lint pre-existing error | rodar `helmfile -e standalone-multi list` para diagnose |
| `tilt up` fica em loop de erro | imagem nao buildou | tail `/tmp/cleanroom-tilt.log` ; verificar Dockerfile + cache |
| `task doctor` retorna FAIL: 1 (loopzero-api) | API pod ainda subindo | aguardar 1-2min, re-rodar |
| JWT expira durante setup | task setup demorou mais que TTL=1h | rodar `bash infra/helmfile/scripts/seed-local-user.sh` (ja idempotente) |

## Integracao com outras skills

```
                   [operador invoca]
                          |
                          v
                   /cleanroom
                   |
                   |  (~30-45min)
                   |
                   v
            cleanroom OK + handover msg
                   |
            [operador invoca proxima skill]
                          |
                          v
        /dogfood full | /to-plan | Ralph loop
```

Cleanroom e PRE-requisito mas NAO e auto-encadeado. O operador e responsavel
por invocar a skill seguinte. Isso forca pausa explicita para validar o estado
intermediario antes de prosseguir.
