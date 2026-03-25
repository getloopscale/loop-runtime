# Roadmap: Migração Kaniko → BuildKit

> Data: 2026-03-25
> Status: Planejado
> Escopo: Substituição completa do Kaniko por BuildKit como engine de build

---

## Contexto

Pipeline atual de build:

```
CLI → API → Argo Workflows → Kaniko → Container Registry → GitOps → Kubernetes
```

Pipeline alvo:

```
CLI → API → Argo Workflows → BuildKit → Container Registry → GitOps → Kubernetes
```

### Premissas

- BuildKit via `buildctl` + `buildkitd` (sidecar pattern, um daemon efêmero por build)
- Cache de layers armazenado no registry (OCI native)
- Cada build roda em um pod isolado (sem daemon compartilhado)
- Argo Workflows preservado como orquestrador
- Sem introduzir dependência em Docker daemon

### Componentes impactados

| Componente | Arquivo(s) principal(is) | Tipo de mudança |
|---|---|---|
| WorkflowBuilder | `api/internal/argo/workflow_builder.go` | Template de build |
| LanguageStrategies | `api/internal/argo/languages.go` | Digest parsing |
| InfraParams | `api/internal/argo/workflow_builder.go` | Flag de migração |
| TenantController | `operators/internal/controller/tenant_build.go` | Secret de registry |
| CWTs | `infra/helm/argo-workflows/templates/cwt-*.yaml` | Cache de layers (remover) |
| Config | `api/internal/config/` | Feature flag |

---

## Fase 1 — Infraestrutura BuildKit no Cluster

### 1.1 — Criar manifesto do Pod buildkitd

- **Ação:** Criar template de Pod/Container para `buildkitd` usando imagem `moby/buildkit:v0.18.2` com flag `--oci-worker-no-process-sandbox`, rootless mode (`--addr unix:///run/user/1000/buildkitd/buildkitd.sock`), e securityContext com `seccompProfile: Unconfined` + `apparmor: unconfined`.
- **Done:** Manifesto YAML existe, passa `kubeconform --strict`, e descreve um container `buildkitd` rootless sem `privileged: true`.

### 1.2 — Criar manifesto do container sidecar buildctl

- **Ação:** Criar template de container sidecar que executa `buildctl` conectando ao socket Unix do `buildkitd` via volume `emptyDir` compartilhado. O sidecar recebe como args: `--addr`, `--frontend`, `--local context`, `--output type=image,name=<tag>,push=true`.
- **Done:** Template de container sidecar existe com volume `emptyDir` montado em `/run/user/1000/buildkitd/` compartilhado entre `buildkitd` e `buildctl`.

### 1.3 — Criar Secret de registry para BuildKit

- **Ação:** No `TenantController` (`tenant_build.go`), adicionar criação de Secret contendo `config.json` do Docker registry no formato BuildKit, montável em `/home/user/.docker/config.json` dentro do container `buildkitd`.
- **Done:** O `TenantController` cria Secret `buildkit-registry-config` no namespace `build-{tenant}` com credenciais do registry, e teste unitário do controller cobre essa criação.

### 1.4 — Adicionar emptyDir para socket Unix no WorkflowBuilder

- **Ação:** No `WorkflowBuilder.buildWorkflow()`, adicionar volume `emptyDir` chamado `buildkit-socket` ao Workflow CR, para comunicação entre `buildkitd` e `buildctl` via socket Unix.
- **Done:** Volume `buildkit-socket` aparece no Workflow CR gerado. Teste unitário do `WorkflowBuilder` valida presença do volume no output serializado.

---

## Fase 2 — Template BuildKit no WorkflowBuilder

### 2.1 — Criar função buildkitTemplateForApp()

- **Ação:** Implementar função `buildkitTemplateForApp(app, infraParams)` em `workflow_builder.go` que retorna template Argo com dois containers: (1) `buildkitd` como sidecar daemon, (2) `buildctl` como container principal executando `buildctl build --frontend dockerfile.v0 --local context=/workspace --local dockerfile=/workspace --output type=image,name={registry}/{tenant}/{project}/{app}:{sha},push=true --export-cache type=registry,ref={registry}/{tenant}/cache/{app}:buildcache --import-cache type=registry,ref={registry}/{tenant}/cache/{app}:buildcache --metadata-file /workspace/metadata-{app}.json`.
- **Done:** Função existe, compila, e teste unitário valida que o template gerado contém dois containers com os args corretos, volumes compartilhados, e security context rootless.

### 2.2 — Extrair digest do metadata file do BuildKit

- **Ação:** O BuildKit grava o digest em `--metadata-file`. Alterar lógica de parsing para ler campo `containerimage.digest` do JSON em `/workspace/metadata-{app}.json` em vez de `/workspace/image-digest-{app}.txt` (formato Kaniko).
- **Done:** Teste unitário valida que dado JSON `{"containerimage.digest":"sha256:abc123"}`, o parser retorna `sha256:abc123`.

### 2.3 — Adicionar flag UseKaniko no InfraParams

- **Ação:** Adicionar campo `UseKaniko bool` (default `true`) na struct `InfraParams`. Quando `true`, chama `kanikoTemplateForApp()`; quando `false`, chama `buildkitTemplateForApp()`.
- **Done:** Campo existe, `WorkflowBuilder` faz branch baseado nele, e dois testes unitários validam que cada valor gera o template correto.

---

## Fase 3 — Cache OCI no Registry

### 3.1 — Configurar import-cache no buildctl

- **Ação:** No `buildkitTemplateForApp()`, incluir `--import-cache type=registry,ref={registry}/{tenant}/cache/{app}:buildcache` nos args do `buildctl`. Cache puxado automaticamente do registry antes do build.
- **Done:** Teste unitário valida que args do `buildctl` contêm `--import-cache` com ref correta para tenant/app.

### 3.2 — Configurar export-cache no buildctl

- **Ação:** No `buildkitTemplateForApp()`, incluir `--export-cache type=registry,ref={registry}/{tenant}/cache/{app}:buildcache,mode=max` nos args do `buildctl`. O `mode=max` exporta todas as layers intermediárias.
- **Done:** Teste unitário valida que args do `buildctl` contêm `--export-cache` com `mode=max` e ref correta.

### 3.3 — Remover CWTs de cache de layers do DAG para builds BuildKit

- **Ação:** No `WorkflowBuilder`, quando `UseKaniko == false`, não incluir tasks `restore-cache-{lang}` e `save-cache-{lang}` no DAG de build de imagens. Manter cache de dependências (node_modules, go modules) inalterado — usa MinIO e é independente do builder.
- **Done:** Teste unitário valida que com `UseKaniko=false`, DAG não contém tasks referenciando `loopzero-cache-restore` ou `loopzero-cache-save` para layers de imagem, mas mantém cache de dependências.

> **Nota:** O cache de dependências (node_modules, go modules, pip) via CWTs MinIO permanece. Apenas o cache de *layers de imagem* muda para registry-based inline no BuildKit.

---

## Fase 4 — Segurança e Isolamento

### 4.1 — Validar buildkitd rootless (user 1000)

- **Ação:** No template gerado por `buildkitTemplateForApp()`, definir `securityContext.runAsUser: 1000`, `runAsGroup: 1000`, `runAsNonRoot: true` no container `buildkitd`. Adicionar `--oci-worker-no-process-sandbox` aos args do `buildkitd`.
- **Done:** Teste unitário valida que securityContext do container `buildkitd` tem `runAsUser=1000`, `runAsNonRoot=true`, e não contém `privileged: true`.

### 4.2 — Aplicar resource limits aos containers BuildKit

- **Ação:** Definir resources no container `buildkitd`: requests `500m CPU / 512Mi memory`, limits `2000m CPU / 4Gi memory`. Definir resources no container `buildctl`: requests `100m / 128Mi`, limits `500m / 512Mi`.
- **Done:** Teste unitário valida resource requests e limits de ambos os containers no template gerado.

### 4.3 — Verificar NetworkPolicy para egress ao registry

- **Ação:** Revisar NetworkPolicy criada pelo `TenantController` em `tenant_build.go` e confirmar que egress para registry (porta 443) está permitido. BuildKit precisa do mesmo acesso que Kaniko já tinha.
- **Done:** NetworkPolicy existente cobre egress para registry. Comentário no código documenta que BuildKit usa mesmas regras de egress que Kaniko.

### 4.4 — Validar isolamento de builds concorrentes

- **Ação:** Validar que cada Workflow CR gera seus próprios containers `buildkitd` + `buildctl` como sidecar pattern (não daemon compartilhado). Cada build tem seu próprio daemon buildkitd efêmero que nasce e morre com o pod.
- **Done:** Teste unitário submete dois Workflow CRs distintos e valida que cada um tem seu próprio container `buildkitd` com nome e volumes independentes.

---

## Fase 5 — Integração End-to-End com BuildKit

### 5.1 — Ativar BuildKit no ambiente local

- **Ação:** Na configuração do ambiente local (`api/internal/config/`), setar `UseKaniko = false`. Manter `true` em produção temporariamente.
- **Done:** Ambiente local (k3d) gera Workflow CRs com templates BuildKit. Deploy `loopzero deploy` local completa com sucesso: build → push → manifest → ArgoCD sync → URL acessível.

### 5.2 — Executar fluxo completo local com BuildKit

- **Ação:** Executar `loopzero deploy` no ambiente k3d com `UseKaniko=false`. Validar: (1) Workflow CR contém containers buildkitd+buildctl, (2) imagem pushada ao registry local, (3) digest extraído corretamente do metadata file, (4) manifests gerados com digest correto, (5) ArgoCD sync completa, (6) aplicação responde na URL esperada.
- **Done:** Fluxo `loopzero deploy` completa end-to-end no ambiente local com BuildKit. Log do Workflow mostra `buildctl` executando build e push com sucesso.

### 5.3 — Executar fluxo com monorepo multi-app

- **Ação:** Executar `loopzero deploy` em monorepo com 2+ apps usando fixture `dogfood-monorepo`. Validar que cada app gera seu próprio par `buildkitd`/`buildctl`, cada imagem pushada com tag independente, e DAG respeita dependências entre steps.
- **Done:** Deploy de monorepo com 2+ apps completa. Cada app tem imagem distinta no registry com digest correto nos manifests.

### 5.4 — Executar fluxo no ambiente DigitalOcean (dev)

- **Ação:** Alterar `UseKaniko = false` no ambiente dev (DigitalOcean). Executar `loopzero deploy` contra cluster DOKS. Validar mesmo checklist da tarefa 5.2 no ambiente real.
- **Done:** Deploy completa no ambiente DigitalOcean com BuildKit. Imagem aparece no DO Container Registry com digest correto.

---

## Fase 6 — Remoção Completa do Kaniko

### 6.1 — Remover função kanikoTemplateForApp()

- **Ação:** Deletar função `kanikoTemplateForApp()` e todo código referenciando `gcr.io/kaniko-project/executor`. Remover branch condicional `UseKaniko` — `buildkitTemplateForApp()` passa a ser o único path.
- **Done:** `grep -r "kaniko" api/` retorna zero resultados. Código compila sem erros.

### 6.2 — Remover campo UseKaniko do InfraParams

- **Ação:** Deletar campo `UseKaniko bool` da struct `InfraParams` e todas as referências no codebase.
- **Done:** `grep -r "UseKaniko" .` retorna zero resultados. Código compila sem erros.

### 6.3 — Remover Secret docker-config no formato Kaniko do TenantController

- **Ação:** No `TenantController`, remover criação do Secret `docker-config` montado em `/kaniko/.docker/`. Manter apenas `buildkit-registry-config`.
- **Done:** `grep -r "kaniko" operators/` retorna zero resultados. Controller compila e testes envtest passam.

### 6.4 — Remover volume docker-config do WorkflowBuilder

- **Ação:** Remover volume Secret `docker-config` e seu mount do Workflow CR gerado pelo `WorkflowBuilder`.
- **Done:** Workflow CR gerado não contém referências a `docker-config`. Testes unitários passam.

### 6.5 — Revisar ResourceQuota do build namespace

- **Ação:** Revisar `ResourceQuota` criado pelo `TenantController` para namespace `build-{tenant}`. Ajustar memory limits se necessário (BuildKit rootless pode usar menos memória que Kaniko com `--snapshot-mode=redo`).
- **Done:** ResourceQuota revisado. Valores documentados em comentário no código com justificativa.

### 6.6 — Remover referências a Kaniko da documentação

- **Ação:** Buscar `Kaniko` e `kaniko` em `docs/technical.md`, `CLAUDE.md`, `CHANGELOG.md`, e todos os arquivos `.md`. Substituir por `BuildKit` onde descreve o builder. Manter referências históricas no CHANGELOG.
- **Done:** `grep -ri "kaniko" docs/ CLAUDE.md` retorna zero resultados exceto entradas históricas no CHANGELOG.

### 6.7 — Executar suite completa de testes

- **Ação:** Executar `task test`, `task lint`, `task validate:manifests`. Corrigir qualquer falha.
- **Done:** `task test` passa. `task lint` passa. `task validate:manifests` passa. Zero referências a Kaniko no codebase exceto CHANGELOG.

---

## Decisões arquiteturais

| Decisão | Justificativa |
|---|---|
| **Sidecar pattern** (buildkitd efêmero por pod) em vez de daemon compartilhado | Mantém isolamento por build sem ponto único de falha. Cada build nasce e morre com seu próprio buildkitd. |
| **Cache inline no registry (OCI)** em vez de cache MinIO para layers | BuildKit suporta nativamente `--import-cache`/`--export-cache` tipo registry, eliminando CWTs de cache para layers. Cache de dependências (node_modules, go modules) via MinIO permanece inalterado. |
| **Flag transitória `UseKaniko`** (Fases 2-5) | Permite rodar ambos os builders em paralelo durante migração, validar em dev antes de prod, e rollback rápido. Removida na Fase 6. |
| **Rootless buildkitd** com `--oci-worker-no-process-sandbox` | Elimina necessidade de `privileged: true` que o Kaniko exigia (root). Melhora postura de segurança dos builds. |
