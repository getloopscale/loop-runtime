# Sprint 2 — Relatório de Sessão

> **Data:** 2026-03-14
> **Duração:** ~8 horas de sessão contínua
> **Resultado:** Pod rodando no cluster com pipeline real end-to-end, zero fakes

---

## 1. O que foi feito

### Fase 1: Revisão de implementação e gap-plan

Revisão profunda do sistema (76 arquivos, 4 componentes) identificou 19 gaps categorizados por severidade. Todos os 19 foram implementados seguindo TDD (red → green → refactor):

- **G1-G4 (críticos):** PriorityClassReady no controller, isBuildTerminal unificado, callback error logging, db:migrate stale
- **G5-G9 (moderados):** Dead-end states em state machines, kubeconform scope, seed dedup, SSE client dedup, index.ts refactoring
- **G10-G19 (menores):** Dead code, typed errors, step numbering, type assertions, init warnings, activateRelease decomposition, source validator tests, adapter dedup, LoopZeroError Unwrap

### Fase 2: Dogfooding (3 rodadas)

Três rodadas de QA em caixa-preta simulando uso real:

- **Rodada 1 (10 findings):** init --json inconsistente, 409 como erro, duration 0s, worker URL vazia, port mapping
- **Rodada 2 (2 findings):** login --json error paths, init --json id field
- **Rodada 3 (9 findings):** Deploy silencioso quando API inacessível (S0), upload errors vazam JSON cru, fixtures name vs project, progress indicators, SSE termos internos, API project_id em mensagens

Todos os findings P0/P1 foram corrigidos. Total: 298 CLI tests passando.

### Fase 3: Eliminação de fakes (a lição mais importante)

**Descoberta grave:** O deploy inteiro era simulado. O `main.go` da API usava 5 `Fake*` structs em código de produção. O operator usava 2 `FakeProvisioner`. O exit gate do Sprint 2 ("app running with a real URL") nunca havia sido satisfeito.

Fakes removidos e substituídos por implementações reais:

| Componente | Fake removido | Implementação real | Testado contra |
|-----------|--------------|-------------------|---------------|
| API SourceStore | `FakeSourceStore` (MinIO, depois Gitea Contents API) | `GiteaSourceStore` — git push via git CLI | Gitea real no cluster |
| API WorkflowSubmitter | `FakeWorkflowSubmitter` | `argo.WorkflowSubmitter` — k8s dynamic client | Argo Workflows real no build cluster |
| API HealthProber | `FakeHealthProber` | `health.HTTPProber` — HTTP GET com retries | httptest.Server real |
| API SyncClient | `FakeSyncClient` | `argocd.SyncClient` — ArgoCD REST API | ArgoCD real no cluster |
| API GitOpsPusher | `FakeGitOpsPusher` | **REMOVIDO** — o workflow faz gitops-push | N/A |
| API TenantCRManager | stub (log + return nil) | `k8s.TenantCRManager` — k8s dynamic client | Tenant CRD real no cluster |
| API SecretStore | `EmptySecretStore` | `PostgresSecretStore` — query real | PostgreSQL real |
| API Cancel endpoint | stub (return 202) | Implementação real com state machine | Testes unitários |
| Operator GitProvisioner | `gitea.FakeProvisioner` | `gitea.RealProvisioner` — Gitea REST API | Gitea real |
| Operator ArgoCDProvisioner | `argocd.FakeProvisioner` | `argocd.RealProvisioner` — k8s dynamic client | ArgoCD Application CRs reais |

### Fase 4: Pipeline real end-to-end

Múltiplas iterações para resolver problemas de infraestrutura real:

1. **Cross-cluster networking:** Clusters k3d em Docker networks separadas. Conectei build cluster ao network do mgmt. Criei env vars `GITEA_CROSS_CLUSTER_URL` e `API_CROSS_CLUSTER_URL` com IPs de NodePort.

2. **SourceStore errado:** Primeira implementação usava MinIO (errado). Spec §7.1 diz source vai para Gitea. Segunda implementação usava Gitea Contents API (commitava blob binário, não clonável). Terceira implementação correta: git push real via git CLI.

3. **Volume sharing em Argo Workflows DAG:** `emptyDir` é per-pod, não compartilhado entre DAG tasks. Substituí por `volumeClaimTemplates` (PVCs reais persistentes durante o workflow).

4. **Kaniko:** Precisava de `--insecure` para registry HTTP e `runAsUser: 0` (root obrigatório para build de imagens).

5. **Heredoc YAML:** Template inline com `{{workflow.parameters}}` dentro de heredoc causava parse error no `kubectl apply`. Substituí por `printf` com variáveis shell.

6. **Callback payload:** Template enviava `apps` no formato de input (`{name, type, port}`) mas API esperava formato de output (`{name, image_digest}`). Construí payload correto com shell puro (sem jq — imagem curl não tem).

7. **ArgoCD Application naming:** API usava `{tenant}-{project}-{env}` mas controller criava `{tenant}-{env}`. Alinhei para `{tenant}-{env}`.

8. **ArgoCD repo credentials:** ArgoCD não tinha credenciais do Gitea configuradas. Adicionei via API.

9. **ArgoCD directory recurse:** Path `.` com manifests em subdiretórios requer `directory.recurse: true`.

10. **Manifest namespace:** generate-manifests usava `{tenant}-{project}-{env}` mas namespace real é `{tenant}-{env}`.

---

## 2. Estado final provado

```
CLI cria git bundle
  → API recebe e faz git push para Gitea source repo         (REAL)
  → API submete Argo Workflow no build cluster                (REAL)
    → clone-repo: git clone do Gitea                          (REAL, cross-cluster)
    → restore-cache: MinIO S3                                 (REAL)
    → install-deps: npm ci                                    (REAL, PVC compartilhado)
    → security-scan: npm audit                                (REAL)
    → build-apps: npm run build                               (REAL)
    → build-image: Kaniko → Docker image → k3d registry       (REAL)
    → generate-manifests: Deployment + Service YAML           (REAL)
    → gitops-push: commit no Gitea GitOps repo                (REAL, cross-cluster)
    → notify-api: callback HTTP 202 para API                  (REAL, cross-cluster)
    → save-cache: MinIO S3                                    (REAL)
  → API processa callback:
    → Build → SUCCEEDED                                       (REAL, PostgreSQL)
    → Release criada → ACTIVE                                 (REAL, PostgreSQL)
    → ArgoCD sync triggered                                   (REAL)
  → ArgoCD sincroniza manifests do Gitea                      (REAL)
  → Deployment + Service criados no namespace acme-production (REAL)
  → Pod hello-loopzero: 1/1 Running                               (REAL)
  → Imagem: k3d-loopzero-registry:5000/acme/hello-loopzero:dev-local (REAL)
```

---

## 3. Aprendizados

### 3.1. NUNCA usar fakes em código de produção

**O erro mais grave da sessão** foi ter afirmado que o Sprint 2 estava "aprovado com ressalvas" quando 5 interfaces no `main.go` e 2 no operator usavam fakes. O deploy inteiro era simulado.

**Regra permanente:** Fakes só existem em arquivos `_test.go`. O `main.go` e qualquer código de produção DEVEM usar implementações reais. Se uma implementação real não existe, o código não compila — RED intencional.

**Salvo como memória permanente:** `feedback_no_fakes.md`

### 3.2. Ler o spec ANTES de implementar

Implementei `MinIOSourceStore` sem ler o spec. O spec §7.1 diz claramente: "API pushes the bundle to the internal Gitea". Também implementei `GitOpsPusher` na API quando o spec diz que o workflow faz o gitops-push (§10.1, step 13i).

**Custo:** 3 implementações erradas (MinIO source, Gitea Contents API source, API GitOpsPusher) que tiveram que ser deletadas e refeitas.

### 3.3. Cross-cluster networking em k3d não é trivial

Clusters k3d rodam em Docker networks separadas. Pods em um cluster não alcançam serviços em outro via DNS interno. Soluções:

- `docker network connect` para conectar nodes de um cluster à network de outro
- NodePort nos serviços que precisam ser acessados cross-cluster
- IPs dos containers Docker nas env vars do workflow

### 3.4. Argo Workflows DAG tasks NÃO compartilham emptyDir

Cada DAG task roda em um pod separado. `emptyDir` volumes são per-pod e morrem quando o pod termina. A solução documentada pela comunidade é `volumeClaimTemplates` — PVCs reais que persistem durante todo o workflow.

### 3.5. ArgoCD precisa de repo credentials E directory.recurse

Dois problemas que causaram "0 resources":
1. ArgoCD não tinha credenciais do Gitea configuradas (repocreds via API)
2. Manifests em subdiretórios requerem `directory.recurse: true` na Application spec

### 3.6. Nomes devem ser alinhados entre componentes

Três desalinhamentos de naming causaram bugs:
- ArgoCD Application: controller usa `{tenant}-{env}`, API usava `{tenant}-{project}-{env}`
- Gitea repo URL: controller usava `/{tenant}/` como org, deveria ser `/loopzero/`
- Manifest namespace: workflow usava `{tenant}-{project}-{env}`, controller cria `{tenant}-{env}`

**Lição:** Quando múltiplos componentes precisam concordar sobre um nome, o nome deve ser definido em UM lugar e importado por todos.

### 3.7. O DIP funcionou perfeitamente para o wiring

A troca de fakes por implementações reais no `main.go` não alterou NENHUMA linha do `BuildOrchestrator`. As interfaces (`WorkflowSubmitter`, `SyncClient`, `HealthProber`, `SourceStore`) isolaram completamente a lógica de negócio da infraestrutura.

### 3.8. Problemas reais só aparecem com infraestrutura real

Nenhum dos problemas resolvidos na Fase 4 (cross-cluster, PVC sharing, Kaniko root, heredoc, callback payload, repo credentials, directory recurse, namespace alignment) teria sido encontrado com fakes. Fakes escondem a complexidade real.

---

## 4. O que ainda falta para o exit gate completo

O pod está rodando, mas para o exit gate completo ("running app with a real URL"):

1. **Health probe no pod** — O `HealthProber` real existe mas precisa ser chamado com a URL real do pod/service
2. **URL acessível** — Precisa de Ingress (Traefik) configurado no workload cluster para gerar URL pública
3. **`loopzero deploy` (com wait)** — O CLI deve esperar pelo SSE `deploy.completed` e mostrar a URL
4. **Cleanup de deploys antigos** — Os pods `api`, `web`, `worker` com `ErrImagePull` são lixo de deploys anteriores
5. **validate-manifests** — kubeconform no workflow precisa de uma imagem com o binário pré-instalado

---

## 5. Métricas da sessão

| Métrica | Valor |
|---------|-------|
| Arquivos criados | 15+ |
| Arquivos modificados | 30+ |
| Arquivos deletados | 6 (todos fakes) |
| Fakes removidos | 9 (5 API + 2 operator + 2 API stubs) |
| Implementações reais criadas | 8 |
| Testes de integração contra infra real | 20+ |
| Problemas de infra resolvidos | 10 |
| Iterações de E2E deploy | 12 |
| CLI tests | 298 passando |
| API tests | Todos passando |
| Workflow steps | 15/16 passando |
| Pod rodando | hello-loopzero 1/1 Running |

---

## 6. Arquivos-chave criados/modificados

### Implementações reais (novos)
- `api/internal/health/prober.go` — HTTP health prober com retries
- `api/internal/source/gitea_store.go` — Git push para Gitea via git CLI
- `api/internal/argo/workflow_submitter.go` — Submit Workflow CR via k8s dynamic client
- `api/internal/argocd/sync_client.go` — ArgoCD REST API sync + poll
- `api/internal/k8s/tenant_cr_manager.go` — Tenant CR via k8s dynamic client
- `api/internal/db/secret_store.go` — PostgreSQL secret metadata query
- `operators/internal/argocd/real_provisioner.go` — ArgoCD Application CRs reais
- `operators/internal/gitea/real_provisioner.go` — Gitea repos via REST API

### Infra
- `api/Dockerfile` — Adicionado `git` ao runtime image
- `infra/tilt/api-deployment.yaml` — Env vars para MinIO, Gitea, ArgoCD, build cluster
- `infra/tilt/operator-deployment.yaml` — Env vars para Gitea e ArgoCD
- `infra/scripts/provision-api-tokens.sh` — Provisioning de tokens e kubeconfig
- `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml` — Múltiplas correções

### Deletados
- `api/internal/source/fake_store.go`
- `api/internal/source/minio_store.go` (implementação errada)
- `api/internal/gitea/gitops_pusher.go` (responsabilidade errada — workflow faz isso)
- Fakes de `api/internal/adapters/adapters.go` (54 linhas removidas)
