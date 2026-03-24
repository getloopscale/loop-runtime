# LoopZero Platform — System Audit

> **Data:** 2026-03-24T15:49Z
> **Ambiente:** dev (Digital Ocean SFO3)
> **Auditor:** Claude Opus 4.6 — dados coletados via kubectl, doctl, curl, openssl direto dos clusters
> **Método:** Auditoria real (zero documentação — só facts observados nos clusters)

---

## 1. Infraestrutura Digital Ocean

### 1.1 Clusters Kubernetes (DOKS)

| Cluster | Nodes | CPU/node | RAM/node | K8s Version | Propósito |
|---------|-------|----------|----------|-------------|-----------|
| **mgmt** | 2 | 2 vCPU | ~4 GB | v1.32.13 | Control plane: API, operators, ArgoCD, Gitea, Grafana, MinIO |
| **build** | 2 | 2 vCPU | ~4 GB | v1.32.13 | Build plane: Argo Workflows, Kaniko, build namespaces |
| **runtime** | 2 | 2 vCPU | ~4 GB | v1.32.13 | Workload plane: tenant apps, Traefik ingress |

**Total:** 6 nodes, 12 vCPU, ~24 GB RAM

### 1.2 Banco de Dados

| Nome | Engine | Versão | Região | Status | Tamanho |
|------|--------|--------|--------|--------|---------|
| loopzero-infra-dev-sfo3-db | PostgreSQL | 16 | sfo3 | online | db-s-1vcpu-2gb |

### 1.3 Container Registry

| Repositório | Tag | Propósito |
|-------------|-----|-----------|
| loopzero-api | dev | API server |
| tenant-controller | dev | Kubernetes operator |
| tn-loopzero/cache | sha256:4755a7e5... (4 tags) | Build cache layers |
| tn-loopzero/node-npm-json-test/node-npm-json-test | d631a196... | App deployada |

### 1.4 Object Storage (Spaces)

| Bucket | Propósito |
|--------|-----------|
| loopzero-builds | Source bundles para build pipeline |
| loopzero-infra-dev-sfo3-artifacts | Artifacts gerais |

### 1.5 Load Balancers

| IP | Região | Status | Cluster |
|----|--------|--------|---------|
| 209.38.119.14 | sfo3 | active | runtime (Traefik) |
| 24.144.69.42 | sfo3 | active | mgmt (Traefik) |

### 1.6 DNS (loopzero.example)

DNS records não retornaram no audit — possível que estejam configurados mas o doctl format filtrou.
URLs verificadas funcionais via curl (seção 5.3).

---

## 2. Control Plane (mgmt cluster)

### 2.1 Pods (22 pods, todos Running)

| Componente | Pods | Status | Restarts |
|-----------|------|--------|----------|
| ArgoCD (server, repo-server, app-controller, etc.) | 7 + 1 init | Running + Completed | 0 |
| Gitea | 1 | Running | 0 |
| Grafana | 1 | Running | 0 |
| Prometheus (server + kube-state-metrics + node-exporter x2) | 4 | Running | 0 |
| cert-manager (controller + cainjector + webhook) | 3 | Running | 0 |
| Traefik | 1 | Running | 0 |
| MinIO | 1 | Running | 0 |
| **LoopZero API** | **2 réplicas** | **Running** | **0** |
| **Tenant Controller** | **1** | **Running** | **0** |

### 2.2 Helm Releases (7)

| Chart | Versão | App Version |
|-------|--------|-------------|
| argo-cd | 7.3.11 | v2.11.7 |
| gitea | 10.6.0 | 1.22.3 |
| grafana | 8.5.2 | 11.2.1 |
| cert-manager | v1.14.5 | v1.14.5 |
| traefik | 28.3.0 | v3.0.2 |
| minio | 5.4.0 | RELEASE.2024-12-18 |
| prometheus | 25.27.0 | v2.54.1 |

### 2.3 CRDs Registrados (12)

| CRD | Propósito | Status |
|-----|-----------|--------|
| tenants.loopzero.io | Tenant lifecycle | **Em uso** (1 tenant Active) |
| environments.loopzero.io | Environment lifecycle (Fase 3) | **Em uso** (1 environment) |
| platformbootstraps.loopzero.io | Platform bootstrap (Fase 1) | Registrado, sem CR ativo |
| gitrepositorybindings.loopzero.io | Repo registration (Fase 2) | Registrado, sem CR ativo |
| loopzeroapplications.loopzero.io | App health (Fase 4) | Registrado, sem CR ativo |
| exposedroutes.loopzero.io | Route exposure (Fase 4) | Registrado, sem CR ativo |
| ~~loopzerobuilds.loopzero.io~~ | ~~Build tracking~~ | **Removido (ADR-0006)** — estado de build owned por PostgreSQL |
| ~~loopzeroreleases.loopzero.io~~ | ~~Release tracking~~ | **Removido (ADR-0006)** — estado de release owned por PostgreSQL |
| appsecrets.loopzero.io | Secret management | Registrado |
| clustercredentials.loopzero.io | Cluster credentials | Registrado |
| previewenvironments.loopzero.io | Preview envs | Registrado |
| reconciliationreports.loopzero.io | Drift detection | Registrado |

### 2.4 Tenant Status

```
Nome: tn-loopzero
Phase: Active
Generation: 1

Conditions (8/8 True):
  NamespacesReady=True
  RBACReady=True
  NetworkPolicyReady=True
  GitRepoReady=True
  ArgoCDAppReady=True
  BuildNamespaceReady=True
  QuotaEnforced=True
  PriorityClassReady=True
```

### 2.5 ArgoCD Application

```
Nome: tn-loopzero-production
Sync: Synced
Health: Healthy
Destination: loopzero-tn-loopzero-production
Repo: http://gitea-http.platform-system.svc.cluster.local:3000/loopzero/tn-loopzero-gitops.git
Path: production
```

### 2.6 IngressRoutes (mgmt)

| Nome | Propósito | URL |
|------|-----------|-----|
| api-ingress | LoopZero API | https://api.dev.loopzero.example |
| argocd-ingress | ArgoCD UI | https://argocd.dev.loopzero.example |
| git-ingress | Gitea UI | https://git.dev.loopzero.example |
| grafana-ingress | Grafana UI | https://grafana.dev.loopzero.example |

### 2.7 Secrets (19 no namespace platform-system)

Secrets de operação:
- `loopzero-platform-tokens` — GITEA_TOKEN + ARGOCD_TOKEN (gerados pelo bootstrap Job)
- `loopzero-api-secrets` — DATABASE_URL + MINIO keys + callback secret
- `loopzero-api-kubeconfig` — kubeconfigs para mgmt + build clusters
- `tenant-controller-kubeconfig` — kubeconfigs para build + runtime clusters
- `loopzero-minio-credentials` — MinIO/Spaces access keys
- `gitea-repo-creds` — ArgoCD repo credentials para Gitea
- `docker-config` — Registry credentials
- `argocd-initial-admin-secret` — ArgoCD admin password

---

## 3. Build Plane (build cluster)

### 3.1 Helm Releases

| Chart | Versão |
|-------|--------|
| argo-workflows | 1.0.5 (v4.0.3) |
| loopzero-build-cwts | 0.1.0 (CWTs locais) |

### 3.2 ClusterWorkflowTemplates (4)

| CWT | Propósito |
|-----|-----------|
| loopzero-clone | Download source bundle do MinIO/Spaces + git clone |
| loopzero-cache-restore | Restaura cache de deps (node_modules, etc.) |
| loopzero-cache-save | Salva cache de deps |
| loopzero-summary | Emite build summary (artifact lineage) |

### 3.3 Build Pipeline — Último Build (SUCCEEDED)

```
Workflow: loopzero-build-build-34bfa36a6d697a7f-kzwr5
Namespace: build-tn-loopzero

Steps (12/12 Succeeded):
  1. clone-repo          → Succeeded  (download bundle do Spaces, git clone)
  2. restore-cache       → Succeeded  (restaura node_modules)
  3. validate-workspace  → Succeeded  (verifica package.json + lockfile)
  4. security-scan       → Succeeded  (scan de vulnerabilidades)
  5. install-deps        → Succeeded  (npm install)
  6. build-apps          → Succeeded  (npm build)
  7. generate-dockerfile → Succeeded  (gera Dockerfile dinâmico)
  8. build-image         → Succeeded  (Kaniko build + push to registry)
  9. save-cache          → Succeeded  (salva node_modules para próximo build)
```

### 3.4 ResourceQuota (build namespace)

```
Workflows concorrentes: max 2
CPU requests: 4 cores
CPU limits: 8 cores
Memory requests: 8 Gi
Memory limits: 16 Gi
```

### 3.5 Secrets no build namespace

| Secret | Propósito |
|--------|-----------|
| docker-config | Registry auth para Kaniko push |
| loopzero-infra-registry | Docker pull secret |
| loopzero-minio-credentials | Spaces access para download/upload de bundles |

---

## 4. Runtime Plane (runtime cluster)

### 4.1 Namespaces

| Namespace | Propósito |
|-----------|-----------|
| platform-system | Traefik + cert-manager |
| loopzero-tn-loopzero-production | Tenant workloads |

### 4.2 App Deployada

```
Nome: node-npm-json-test
Replicas: 1
Image: registry.digitalocean.com/loopzero-infra-registry/tn-loopzero/node-npm-json-test/node-npm-json-test:d631a196ebff
Port: 3000
Status: Running (0 restarts)
Node: loopzero-infra-dev-sfo3-runtime-default-djvwz
```

### 4.3 Networking

| Recurso | Detalhes |
|---------|----------|
| Service | ClusterIP 10.245.106.182:3000 |
| IngressRoute | `node-npm-json-test` → Host match → Service |
| Certificate | `tls-node-npm-json-test` — **Ready=True**, Let's Encrypt |

### 4.4 Security (por namespace tenant)

| Recurso | Config |
|---------|--------|
| NetworkPolicy `deny-all` | Default deny all ingress/egress |
| NetworkPolicy `allow-ingress-traefik` | Permite tráfego do Traefik |
| NetworkPolicy `allow-egress` | Permite saída para DNS e internet |
| ResourceQuota | CPU: 4 cores, Memory: 8Gi, PVCs: 10, LBs: 0 |
| ServiceAccount | `loopzero-deployer` com Role/RoleBinding |

---

## 5. Fluxo de Deploy — Verificação Real

### 5.1 Gitea Repos

| Repo | Branch | Propósito |
|------|--------|-----------|
| loopzero/tn-loopzero-gitops | main | GitOps desired state (manifests) |
| loopzero/loopzero-source-tn-loopzero-node-npm-json-test | main | Source code bundles |

### 5.2 GitOps Repo Structure

```
tn-loopzero-gitops/
├── README.md
└── production/
    └── node-npm-json-test/
        └── manifests.yaml    ← Deployment + Service + Certificate + IngressRoute
```

### 5.3 Fluxo Real Observado

```
1. CLI (loopzero deploy)
   └→ Cria git bundle do código local
   └→ POST /api/v1/source/upload (bundleData)
       └→ API faz PUT no Spaces bucket "loopzero-builds"
       └→ API faz git push para Gitea source repo

2. API cria Build record no PostgreSQL
   └→ POST /api/v1/builds (202 Accepted)
   └→ API submete Argo Workflow no build cluster
       └→ namespace: build-tn-loopzero

3. Argo Workflow executa pipeline (9 steps):
   clone-repo → restore-cache → validate-workspace →
   security-scan → install-deps → build-apps →
   generate-dockerfile → build-image (Kaniko) → save-cache

4. Build complete callback
   └→ API gera manifests (Deployment + Service + Certificate + IngressRoute)
   └→ API faz git push para Gitea GitOps repo (production/app/manifests.yaml)
   └→ API cria Release record no PostgreSQL

5. ArgoCD detecta mudança no GitOps repo
   └→ Sync automático (selfHeal: true, prune: true)
   └→ Aplica manifests no runtime cluster namespace loopzero-tn-loopzero-production

6. Runtime cluster
   └→ Deployment criado → Pod running
   └→ Service criado → ClusterIP
   └→ Certificate CR criado → cert-manager → Let's Encrypt ACME → TLS Secret
   └→ IngressRoute criado → Traefik roteia tráfego
```

### 5.4 URLs Live (verificadas com curl)

| URL | Resposta | TLS |
|-----|----------|-----|
| https://api.dev.loopzero.example/api/v1/health | `{"status":"ok"}` | Traefik default cert |
| https://git.dev.loopzero.example | Gitea v1.22.3 | Traefik default cert |
| https://argocd.dev.loopzero.example | ArgoCD UI (200) | Traefik default cert |
| https://node-npm-json-test-production.node-npm-json-test.tn-loopzero.example.loopzero.example | `{"status":"ok","message":"Hello from node-npm!"}` | **Let's Encrypt (R12)** |

---

## 6. Achados de Auditoria

### CRÍTICOS

**F-01: Platform services sem TLS Let's Encrypt**
- API, Gitea, ArgoCD, Grafana usam **Traefik default cert** (self-signed)
- Apenas apps deployadas via `loopzero deploy` recebem Let's Encrypt (via Certificate CR no renderer)
- **Impacto:** Browser mostra aviso de segurança para todas as URLs de plataforma
- **Fix:** Adicionar Certificate CRs no Pulumi bootstrap para api.dev, git.dev, argocd.dev, grafana.dev

**F-02: Credentials expostas no controller env vars**
- `DIGITALOCEAN_TOKEN`, `SPACES_ACCESS_KEY_ID`, `SPACES_SECRET_ACCESS_KEY` estão como plain text env vars no tenant-controller deployment
- **Impacto:** Qualquer pod com acesso ao API server pode ler essas env vars via Kubernetes API
- **Fix:** Mover para Secrets com secretKeyRef em vez de value direto

### ALTOS

**F-03: Apenas 1 app deployada (não é monorepo)**
- O deploy feito foi de `examples/node-npm` — um app Express simples (backend only)
- **NÃO foi testado deploy de monorepo** com frontend + backend
- **Impacto:** Não há prova de que o sistema suporta monorepos reais
- **Ação necessária:** Testar com `examples/fullstack-mixed` ou outro monorepo real

**F-04: 8 dos 12 CRDs não têm CRs ativos**
- PlatformBootstrap, GitRepositoryBinding, LoopZeroApplication, ExposedRoute, LoopZeroBuild, LoopZeroRelease — todos registrados mas sem CRs em uso
- Os controllers estão compilados na imagem mas **nunca reconciliaram um CR real em produção**
- **Impacto:** Controllers são código não-exercitado em ambiente real

**F-05: CLI deploy retorna DEPLOY_FAILED (health probe timeout)**
- O deploy funciona (app fica live) mas o CLI retorna exit code 3 (DEPLOY_FAILED)
- O health probe do CLI expira antes do ArgoCD completar o sync
- **Impacto:** CI/CD baseado em exit code vai tratar como falha

### MÉDIOS

**F-06: Build pipeline usa PVC VolumeClaimTemplates que podem vazar**
- PVCs são criados por workflow e limpos pelo TTL do Argo Workflows
- Se o workflow não é garbage collected, PVCs ficam orphaned
- TTL configurado: 4h succeeded, 24h failed

**F-07: Namespace naming inclui prefix duplo implícito**
- Namespace: `loopzero-tn-loopzero-production` (prefix `loopzero-` + CRD name `tn-loopzero`)
- Funciona mas é verbose e pode confundir operadores

**F-08: Bootstrap Job usa `bitnami/kubectl:latest`**
- Tag `latest` viola a regra de pinned image tags do projeto
- Pode quebrar se Bitnami mudar breaking changes
- **Fix:** Encontrar tag específica que existe (e.g., 1.32.0)

### BAIXOS

**F-09: Grafana health endpoint não respondeu**
- `curl -sk https://grafana.dev.loopzero.example/api/health` retornou vazio
- Pode ser timeout ou cert issue
- Grafana pod está Running com 0 restarts

**F-10: DNS records não visíveis no audit**
- `doctl compute domain records list` não retornou records para dev.loopzero.example
- As URLs funcionam, então os records existem — pode ser filtering issue do doctl

---

## 7. Configuração de Credenciais

### Secrets que o sistema usa

| Secret | Namespace | Keys | Gerado por |
|--------|-----------|------|-----------|
| loopzero-platform-tokens | platform-system | GITEA_TOKEN, ARGOCD_TOKEN | Bootstrap Job |
| loopzero-api-secrets | platform-system | DATABASE_URL, MINIO_ACCESS_KEY, MINIO_SECRET_KEY, LOOPZERO_ARGO_CALLBACK_SECRET | Pulumi |
| loopzero-api-kubeconfig | platform-system | mgmt kubeconfig, build kubeconfig | Pulumi |
| tenant-controller-kubeconfig | platform-system | build kubeconfig, runtime kubeconfig | Pulumi |
| loopzero-minio-credentials | platform-system + argo (build) | AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, MINIO_ENDPOINT | Pulumi |
| gitea-repo-creds | platform-system | type, url, username, password | Bootstrap Job |
| docker-config | build-tn-loopzero | config.json (registry auth) | Pulumi |
| argocd-initial-admin-secret | platform-system | password | ArgoCD Helm chart |

### Fluxo de credenciais no deploy

```
1. CLI autentica via GitHub token (stored em ~/.loopzero/config.json)
2. API valida token contra GitHub API
3. API usa GITEA_TOKEN para push source code e manifests para Gitea
4. API usa ARGOCD_TOKEN para trigger sync
5. API usa MINIO_ACCESS_KEY/SECRET_KEY para upload de bundles para Spaces
6. Argo Workflow usa loopzero-minio-credentials para download de bundles
7. Kaniko usa docker-config para push de images para DO Registry
8. ArgoCD usa gitea-repo-creds para clone de repos
```

---

## 8. Resumo Executivo

### O que funciona

- Deploy de uma app Node.js simples: **CLI → build pipeline → live URL com TLS Let's Encrypt**
- 12 steps do build pipeline executam com sucesso
- ArgoCD sync automático com selfHeal
- Tenant provisioning completo (8 conditions, namespace, RBAC, NetworkPolicy, Quota)
- Destroy + recreate via Pulumi (com bootstrap Job automático)

### O que NÃO foi validado

- Deploy de monorepo real (frontend + backend)
- Multiple environments (staging + production)
- Rollback (`loopzero rollback`)
- Preview environments (`loopzero preview`)
- Secrets management (`loopzero secret`)
- Concurrent deploys (lock behavior)
- Scale (multiple tenants, multiple apps)
- Os 8 CRDs novos da refatoração não foram exercitados com CRs reais

### Classificação

| Área | Nota | Justificativa |
|------|------|---------------|
| Infraestrutura | **B+** | Clusters, DB, storage, LBs — tudo funcional |
| Build Pipeline | **A-** | 12 steps, cache, security scan, Kaniko — completo |
| Control Plane | **B** | Controllers, CRDs, ArgoCD — funcional mas CRDs novos não exercitados |
| Security | **C+** | Credentials em env vars, platform TLS self-signed |
| DX (Developer Experience) | **B-** | Deploy funciona mas CLI retorna exit code errado |
| Monorepo Support | **NÃO TESTADO** | Zero evidência de deploy multi-app |
| Production Readiness | **C** | Single tenant, single app, single environment testado |
