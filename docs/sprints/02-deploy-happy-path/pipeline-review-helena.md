# Pipeline Review — Helena Duarte

**Data:** 2026-03-16
**Escopo:** `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml`, `api/internal/argo/workflow_submitter.go`, `api/internal/services/build_orchestrator.go`, `api/internal/routes/builds.go`, `infra/helm/argo-workflows/values.yaml`, `infra/tests/unit/workflow_template_test.go`
**Veredito:** APROVADO COM RESSALVAS

---

## Criterio 1 — Determinismo

| Check | Status | Detalhes |
|-------|--------|----------|
| Lockfiles presentes | **PASS** | `validate-workspace` exige lockfile (package-lock.json, yarn.lock, pnpm-lock.yaml). Falha com exit 1 se ausente. |
| Image tags fixos (sem :latest) | **PASS** | Todas as imagens usam tags pinadas: `alpine/git:2.43.0`, `node:20.11.1-alpine`, `gcr.io/kaniko-project/executor:v1.23.2`, `curlimages/curl:8.6.0`, `amazon/aws-cli:2.15.17`, `alpine:3.19.1`, `alpine:3.21`. Teste `TestWorkflowTemplateNoLatestImageTags` valida isso. |
| Build reproduzivel | **PASS** | `install-deps` usa `npm ci`, `--frozen-lockfile` (pnpm/yarn). Image tag usa commit-sha, nao timestamp. |
| Sem dependencias flutuantes | **WARN** | `validate-manifests` faz `apk add --no-cache curl` e `curl -sL` para baixar kubeconform em runtime. Isso depende de rede e da disponibilidade do GitHub release. |

---

## Criterio 2 — Separacao Build/Release

| Check | Status | Detalhes |
|-------|--------|----------|
| Pipeline nao cria release | **PASS** | Release e criada pela API no `build_orchestrator.go:475-493`, nao pelo workflow. |
| Pipeline nao altera GitOps state | **FAIL** | Step `gitops-push` clona o repo GitOps, faz git commit e git push. Isso e mutacao de control plane dentro do build pipeline. |
| Pipeline produz apenas artefatos | **FAIL** | Alem de `image:commit-sha`, o workflow gera manifests e faz push para Gitea. |
| Side effects isolados | **FAIL** | `gitops-push` e um side effect externo com retry proprio (3x rebase+push). Falha de Gitea invalida build inteiro. |

### Nota sobre a decisao arquitetural

A API **ja tem a infraestrutura para fazer gitops push** — o `pushToGitOps` em `build_orchestrator.go:499-521` existe mas assume que o workflow ja fez o push (comentario: "Gitops-push already completed by the workflow"). A API simplesmente transiciona READY→PENDING. Mover o gitops-push para a API significaria que a API faz o commit no Gitea **apos** receber o callback de sucesso, separando build de release.

O codigo tem um comentario dizendo que o GitOpsPusher "was REMOVED from the API" (L42-44). Isso sugere que o time ja considerou essa separacao e decidiu deliberadamente manter no workflow. Vale re-visitar essa decisao.

---

## Criterio 3 — Escala

| Check | Status | Detalhes |
|-------|--------|----------|
| CRDs minimizados | **PASS** | Workflow usa `workflowTemplateRef`, nao inline templates. Um CR por build. |
| Parallelism limits | **FAIL** | `values.yaml` nao configura `controller.workflowDefaults.spec.parallelism` nem `controller.workflowWorkers`. O WorkflowTemplate nao tem `spec.parallelism`. Sem limites, 100 builds concorrentes criam 100 workflows x ~14 pods cada = 1400 pods no cluster de build. |
| Cache habilitado | **PASS** | Kaniko usa `--cache=true --cache-repo={{registry-url}}/{{tenant-id}}/cache`. Cache de deps via MinIO S3 (save-cache/restore-cache). |
| Registry throughput | **WARN** | Nao ha rate limiting ou backoff para push/pull de imagens. Em alta concorrencia, o registry local pode saturar. |

---

## Criterio 4 — Observabilidade

| Check | Status | Detalhes |
|-------|--------|----------|
| Build duration | **PASS** | API calcula `durationSecs` em `finalizeActivation` e emite no `deploy.completed` SSE event. |
| Step duration | **WARN** | Steps individuais nao emitem duracao propria. So temos duracao total. Argo expoe metricas por step se `metricsConfig.enabled: true`, mas nao ha dashboards configurados para consumir isso. |
| Cache hit rate | **FAIL** | `restore-cache` loga "Cache miss" ou "Cache restored" mas nao emite metricas. Nao ha como medir hit rate agregado. |
| Artifact lineage | **PASS (parcial)** | Callback inclui: `image` (com commit-sha), `build_id`, `status`. Falta: `image_digest` real (SHA256) — usa tag, nao digest. `correlation-id` e parametro do workflow mas nao aparece no callback body. |

### Nota sobre image_digest

O campo `image_digest` no callback usa o formato `registry/tenant/project:commit-sha` — isso e uma **image reference**, nao um **digest**. Um digest real seria `registry/tenant/project@sha256:abc123...`. Kaniko pode produzir o digest via `--digest-file`, mas esse output nao e capturado. Sem o digest real, nao e possivel verificar que a imagem deployada e exatamente a que foi buildada (supply chain gap relevante para Sprint 6 — Security).

O `correlation-id` e passado como parametro do workflow mas **nao aparece no callback body**. Isso quebra a rastreabilidade end-to-end.

---

## Criterio 5 — Recovery

| Check | Status | Detalhes |
|-------|--------|----------|
| Steps idempotentes | **PASS** | `clone-repo` e idempotente (git clone overwrite). `build-image` usa Kaniko com tag fixa (commit-sha), push idempotente. `save-cache` e best-effort. |
| Correlation IDs | **PASS** | `correlation-id` propagado como workflow parameter, presente no git commit trailer do gitops-push. |
| Callback idempotente | **PASS** | `builds.go` detecta build terminal e retorna 200 no-op. `OnBuildCompleted` re-verifica estado dentro do lock. Duplicate callbacks sao safe. |
| Retry seguro | **WARN** | `gitops-push` retry (3x rebase+push) pode criar commits duplicados se o primeiro push foi parcialmente bem-sucedido. `notify-api` retry com `retryPolicy: "Always"` pode enviar callbacks duplicados — mas a API aceita (idempotente). |

### Nota sobre resiliencia do callback

O tratamento de callback resiliente (`Callback` handler em builds.go) e impressionantemente robusto. Ele nunca retorna 400 para payloads malformados (regra F-01), faz fallback parse de JSON quebrado, e trata status desconhecido como "failed". Isso mostra aprendizado real de incidentes — a chain F-01→F-04 do dogfood do Sprint 2.

---

## Alinhamento com Spec

| Secao | Alinhado | Divergencias |
|-------|----------|-------------|
| §5 Build state machine | **SIM** | QUEUED→RUNNING→SUCCEEDED/FAILED, com race guard EC-N (QUEUED callback). |
| §10 Build flow | **SIM** | Fluxo completo implementado: submit→workflow→callback→release→sync→active. |
| §12 Build failures | **PARCIAL** | Callback resilience excelente. Mas build reaper para workflows orphans nao encontrado. |

---

## Problemas Encontrados

1. ~~**[CRITICO] `validate-manifests` com `continueOn: failed: true`**~~ **CORRIGIDO** — Removido `continueOn: failed: true`. `gitops-push` agora depende de `validate-manifests` (nao de `generate-manifests`). Manifest invalido = build falha.

2. ~~**[CRITICO] Sem `parallelism` limits no controller ou workflow**~~ **CORRIGIDO** — Adicionado `spec.parallelism: 4` no WorkflowTemplate (limita 4 steps concorrentes por workflow). Adicionado `controller.workflowWorkers: 32` e `controller.podWorkers: 32` no values.yaml.

3. **[ALERTA] `gitops-push` dentro do workflow** — Mistura build com release materialization. Falha de Gitea invalida build caro ja completado. Retry de gitops-push tem perfil diferente de retry de install-deps. **Proposta:** Mover gitops-push para a API (pos-callback). **Status:** Requer decisao arquitetural — nao implementado.

4. ~~**[ALERTA] `validate-manifests` baixa kubeconform via curl**~~ **CORRIGIDO** — Substituido `alpine:3.21` + `curl` download por `ghcr.io/yannh/kubeconform:v0.6.7-alpine`. Eliminada dependencia de rede externa.

5. ~~**[ALERTA] `image_digest` no callback e tag, nao digest SHA256**~~ **CORRIGIDO** — Adicionado `--digest-file=/workspace/image-digest.txt` ao Kaniko. notify-api le o digest real e envia no formato `registry/tenant/project@sha256:...` no callback.

6. ~~**[ALERTA] `correlation-id` ausente no callback body**~~ **CORRIGIDO** — `correlation_id` adicionado ao JSON body do callback (todos os 3 branches: succeeded, failed, fallback). Tambem adicionado como header `Correlation-ID` na request HTTP.

7. **[MELHORIA] Cache hit rate nao e rastreavel** — Sem metricas de cache, nao e possivel otimizar. **Proposta:** emitir metricas ou annotations no workflow com cache hit/miss. **Status:** Nao implementado — baixa prioridade.

8. **[MELHORIA] Build reaper nao encontrado** — Se o workflow terminar sem enviar callback (crash, OOM, timeout), o build fica stuck em RUNNING. **Proposta:** implementar reaper que detecta workflows terminados sem callback (spec §12). **Status:** Nao implementado — pertence a sprint futuro.

---

## Resumo de Correcoes Implementadas (2026-03-16)

| # | Fix | Arquivo |
|---|-----|---------|
| 1 | Remover `continueOn: failed: true`, gitops-push depende de validate-manifests | `workflow-template-nodejs-v1.yaml` |
| 2 | `spec.parallelism: 4` + controller workers 32/32 | `workflow-template-nodejs-v1.yaml`, `values.yaml` |
| 4 | Imagem `ghcr.io/yannh/kubeconform:v0.6.7-alpine` | `workflow-template-nodejs-v1.yaml` |
| 5 | `--digest-file` no Kaniko + leitura do digest real no callback | `workflow-template-nodejs-v1.yaml` |
| 6 | `correlation_id` no body + header `Correlation-ID` no callback | `workflow-template-nodejs-v1.yaml` |

Testes: 6 novos testes adicionados em `infra/tests/unit/workflow_template_test.go`. 40/40 passando.

---

## Itens Pendentes

1. **[ALERTA] Mover `gitops-push` para a API** — requer decisao arquitetural
2. **[MELHORIA] Cache hit rate tracking** — baixa prioridade
3. **[MELHORIA] Build reaper** — sprint futuro (spec §12)

---

## Veredito Helena (atualizado)

> "Cinco dos oito problemas foram corrigidos. Os dois criticos (validate-manifests como hard gate e parallelism limits) estao resolvidos. A supply chain agora tem digest real, e a rastreabilidade end-to-end esta completa com correlation-id no callback. O unico alerta aberto significativo e o gitops-push dentro do workflow — decisao arquitetural que precisa de alinhamento do time."
