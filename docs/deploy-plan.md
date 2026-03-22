# Plano de Deploy — LoopZero Platform (DigitalOcean Dev)

> Plano executável para levar o LoopZero de "infra provisionada" para "deploy funcional end-to-end".
>
> **Data:** 2026-03-22
> **Stack alvo:** `dev` (dev.loopzero.example)
> **Estado atual:** Infraestrutura pronta (clusters, DNS, TLS, observabilidade). Código de aplicação existe mas não está deployado.

---

## Visão Geral das Fases

```
Fase 1          Fase 2          Fase 3          Fase 4          Fase 5
Container       Database +      Serviços        Build           End-to-End
Registry +      Deploy API      de Suporte      Plane           Validação
Images          + Operators

[██░░░░░░░░]   [░░░░░░░░░░]   [░░░░░░░░░░]   [░░░░░░░░░░]   [░░░░░░░░░░]
```

| Fase | Entregável | Critério de Saída |
|------|-----------|-------------------|
| **1** | Images no registry, pullable pelo cluster | `docker pull registry/loopzero-api:dev` funciona |
| **2** | API respondendo, operators reconciliando | `curl https://api.dev.loopzero.example/api/v1/health` → 200 |
| **3** | MinIO + Argo Workflows operacionais | Workflow de teste completa com sucesso |
| **4** | Pipeline de build funcionando | Source → image → manifests → Gitea push |
| **5** | `loopzero deploy` produz URL acessível | CLI → API → Build → ArgoCD → live app |

---

## Fase 1 — Container Registry + Images

### Objetivo
Criar um container registry e publicar as images do LoopZero API e Operator.

### 1.1 Criar DigitalOcean Container Registry

DigitalOcean oferece registry integrado com DOKS (pull sem credenciais extras).

```bash
# Criar registry (starter tier = grátis, 500MB)
doctl registry create loopzero-registry --region nyc3 --subscription-tier starter

# Integrar com os clusters DOKS (permite pull sem imagePullSecrets)
doctl kubernetes cluster registry add loopzero-infra-dev-nyc3-mgmt
doctl kubernetes cluster registry add loopzero-infra-dev-nyc3-runtime
```

**Alternativa Pulumi** (preferível para consistência IaC):

Adicionar em `infra/pulumi/src/components/infra.ts`:
```typescript
const registry = new digitalocean.ContainerRegistry("loopzero-registry", {
  name: "loopzero-registry",
  subscriptionTierSlug: "starter", // 500MB grátis, suficiente para dev
  region: config.region,
});
```

**Output esperado:** `registry.digitalocean.com/loopzero-registry`

### 1.2 Build e Push das Images

#### LoopZero API
```bash
# Do diretório raiz do projeto
docker build -f api/Dockerfile -t registry.digitalocean.com/loopzero-registry/loopzero-api:dev .
docker push registry.digitalocean.com/loopzero-registry/loopzero-api:dev
```

#### Tenant Controller (Operator)
```bash
docker build -f operators/Dockerfile -t registry.digitalocean.com/loopzero-registry/tenant-controller:dev .
docker push registry.digitalocean.com/loopzero-registry/tenant-controller:dev
```

### 1.3 GitHub Actions — CI/CD de Images

Criar `.github/workflows/build-images.yml`:

```yaml
name: "Build: Container Images"

on:
  push:
    branches: [develop]
    paths:
      - "api/**"
      - "operators/**"
      - "pkg/**"
      - ".github/workflows/build-images.yml"

env:
  REGISTRY: registry.digitalocean.com/loopzero-registry

jobs:
  build-api:
    name: Build loopzero-api
    runs-on: ubuntu-latest
    environment: infra-dev
    steps:
      - uses: actions/checkout@v4
      - name: Login to DO Registry
        run: doctl registry login --expiry-seconds 600
        env:
          DIGITALOCEAN_ACCESS_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
      - name: Build and push
        run: |
          docker build -f api/Dockerfile -t $REGISTRY/loopzero-api:${{ github.sha }} -t $REGISTRY/loopzero-api:dev .
          docker push $REGISTRY/loopzero-api --all-tags

  build-operator:
    name: Build tenant-controller
    runs-on: ubuntu-latest
    environment: infra-dev
    steps:
      - uses: actions/checkout@v4
      - name: Login to DO Registry
        run: doctl registry login --expiry-seconds 600
        env:
          DIGITALOCEAN_ACCESS_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
      - name: Build and push
        run: |
          docker build -f operators/Dockerfile -t $REGISTRY/tenant-controller:${{ github.sha }} -t $REGISTRY/tenant-controller:dev .
          docker push $REGISTRY/tenant-controller --all-tags
```

### Critério de saída Fase 1
- [ ] `doctl registry repository list-v2` mostra `loopzero-api` e `tenant-controller`
- [ ] Clusters DOKS conseguem fazer pull das images (integração registry ativa)

---

## Fase 2 — Deploy API + Operators no Management Cluster

### 2.1 Criar Database da API

O PostgreSQL managed já existe com database `gitea`. A API precisa do seu próprio database.

**Opção A — Via Pulumi** (preferível):

Em `infra/pulumi/src/providers/digitalocean/database.ts`, adicionar:
```typescript
new digitalocean.DatabaseDb(`${spec.name}-loopzero`, {
  clusterId: cluster.id,
  name: "loopzero",
});
```

**Opção B — Via doctl** (imediato):
```bash
doctl databases db create <cluster-id> --name loopzero
```

### 2.2 Aplicar Migrations

```bash
# Obter connection string
export DATABASE_URL=$(pulumi stack output dbUri --show-secrets -s dev)

# Substituir database name de defaultdb para loopzero
DATABASE_URL="${DATABASE_URL/defaultdb/loopzero}"

# Aplicar migrations
go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest \
  -path api/internal/db/migrations \
  -database "$DATABASE_URL" \
  up
```

**Verificar:**
```bash
# Deve mostrar 8 migrations aplicadas
migrate -database "$DATABASE_URL" -path api/internal/db/migrations version
```

### 2.3 Criar K8s Secrets para a API

```bash
KUBECONFIG=/tmp/loopzero-mgmt.kubeconfig

# Database
kubectl -n platform-system create secret generic loopzero-api-db \
  --from-literal=DATABASE_URL="postgres://doadmin:<password>@private-loopzero-infra-dev-nyc3-db-do-user-778930-0.j.db.ondigitalocean.com:25060/loopzero?sslmode=require"

# Gitea
kubectl -n platform-system create secret generic loopzero-api-gitea \
  --from-literal=GITEA_TOKEN="<gitea-admin-token>" \
  --from-literal=GITEA_URL="http://gitea-http.platform-system.svc.cluster.local:3000"

# ArgoCD
kubectl -n platform-system create secret generic loopzero-api-argocd \
  --from-literal=ARGOCD_TOKEN="<argocd-admin-password>" \
  --from-literal=ARGOCD_URL="http://argocd-server.platform-system.svc.cluster.local:80"

# MinIO (criado na Fase 3)
kubectl -n platform-system create secret generic loopzero-api-minio \
  --from-literal=MINIO_ACCESS_KEY="<access-key>" \
  --from-literal=MINIO_SECRET_KEY="<secret-key>" \
  --from-literal=MINIO_ENDPOINT="minio.platform-system.svc.cluster.local:9000"

# Argo callback
kubectl -n platform-system create secret generic loopzero-api-argo-callback \
  --from-literal=LOOPZERO_ARGO_CALLBACK_SECRET="$(openssl rand -hex 32)"
```

### 2.4 Deploy LoopZero API

Adicionar em `infra/pulumi/src/components/bootstrap.ts`:

```typescript
function installLoopZeroAPI(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.apps.v1.Deployment {
  // ConfigMap com variáveis não-sensíveis
  const configMap = new k8s.core.v1.ConfigMap("loopzero-api-config", {
    metadata: { name: "loopzero-api-config", namespace: PLATFORM_NAMESPACE },
    data: {
      PORT: "8080",
      METRICS_PORT: "9090",
      LOOPZERO_ENV: "production",
      BASE_DOMAIN: config.domain,
      WORKLOAD_INGRESS_URL: `https://${config.domain}`,
      GITEA_URL: "http://gitea-http.platform-system.svc.cluster.local:3000",
      ARGOCD_URL: "http://argocd-server.platform-system.svc.cluster.local:80",
      ARGOCD_INSECURE: "true",
      MINIO_ENDPOINT: "minio.platform-system.svc.cluster.local:9000",
    },
  }, { provider, dependsOn: [ns] });

  // Deployment
  return new k8s.apps.v1.Deployment("loopzero-api", {
    metadata: { name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
    spec: {
      replicas: 2,
      selector: { matchLabels: { app: "loopzero-api" } },
      template: {
        metadata: {
          labels: { app: "loopzero-api" },
          annotations: { "prometheus.io/scrape": "true", "prometheus.io/port": "9090" },
        },
        spec: {
          containers: [{
            name: "api",
            image: `registry.digitalocean.com/loopzero-registry/loopzero-api:dev`,
            ports: [
              { name: "http", containerPort: 8080 },
              { name: "metrics", containerPort: 9090 },
            ],
            envFrom: [
              { configMapRef: { name: "loopzero-api-config" } },
              { secretRef: { name: "loopzero-api-db" } },
              { secretRef: { name: "loopzero-api-gitea" } },
              { secretRef: { name: "loopzero-api-argocd" } },
              { secretRef: { name: "loopzero-api-minio" } },
              { secretRef: { name: "loopzero-api-argo-callback" } },
            ],
            resources: {
              requests: { cpu: "200m", memory: "256Mi" },
              limits: { cpu: "1", memory: "512Mi" },
            },
            livenessProbe: {
              httpGet: { path: "/api/v1/health", port: "http" },
              initialDelaySeconds: 10,
            },
            readinessProbe: {
              httpGet: { path: "/api/v1/health/deep", port: "http" },
              initialDelaySeconds: 5,
            },
          }],
        },
      },
    },
  }, { provider, dependsOn: [ns, configMap] });
}
```

**Service** (para o IngressRoute que já existe):
```typescript
new k8s.core.v1.Service("loopzero-api-svc", {
  metadata: { name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
  spec: {
    selector: { app: "loopzero-api" },
    ports: [{ port: 8080, targetPort: "http" }],
  },
}, { provider });
```

### 2.5 Deploy CRDs + TenantController

```bash
KUBECONFIG=/tmp/loopzero-mgmt.kubeconfig

# Aplicar CRDs no management cluster
kubectl apply -f operators/config/crd/bases/

# Verificar
kubectl get crd | grep loopzero.io
```

Deployment do controller via Pulumi ou manifests diretos:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tenant-controller
  namespace: platform-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tenant-controller
  template:
    metadata:
      labels:
        app: tenant-controller
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: tenant-controller
      containers:
        - name: controller
          image: registry.digitalocean.com/loopzero-registry/tenant-controller:dev
          args: ["--leader-elect=true"]
          ports:
            - name: metrics
              containerPort: 8080
            - name: health
              containerPort: 8081
          livenessProbe:
            httpGet:
              path: /healthz
              port: health
          readinessProbe:
            httpGet:
              path: /readyz
              port: health
          resources:
            requests: { cpu: "100m", memory: "128Mi" }
            limits: { cpu: "500m", memory: "256Mi" }
          env:
            - name: GITEA_URL
              value: "http://gitea-http.platform-system.svc.cluster.local:3000"
            - name: GITEA_TOKEN
              valueFrom:
                secretKeyRef:
                  name: loopzero-api-gitea
                  key: GITEA_TOKEN
```

**RBAC necessário:**
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tenant-controller
rules:
  - apiGroups: ["loopzero.io"]
    resources: ["tenants", "tenants/status"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: [""]
    resources: ["namespaces", "resourcequotas", "limitranges"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tenant-controller
subjects:
  - kind: ServiceAccount
    name: tenant-controller
    namespace: platform-system
roleRef:
  kind: ClusterRole
  name: tenant-controller
  apiGroup: rbac.authorization.k8s.io
```

### 2.6 Configurar Gitea (Bootstrap Inicial)

O Gitea está rodando mas precisa de configuração inicial:

```bash
GITEA_URL="https://git.dev.loopzero.example"

# 1. Criar admin user (primeira vez — ou via Gitea init container)
#    Acessar https://git.dev.loopzero.example e completar setup wizard

# 2. Criar org para o LoopZero
curl -X POST "$GITEA_URL/api/v1/orgs" \
  -H "Authorization: token $GITEA_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username": "loopzero-platform", "visibility": "private"}'

# 3. Criar repo de GitOps (onde ArgoCD lê os manifests)
curl -X POST "$GITEA_URL/api/v1/orgs/loopzero-platform/repos" \
  -H "Authorization: token $GITEA_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "gitops", "auto_init": true, "private": true}'
```

### 2.7 Configurar ArgoCD → Runtime Cluster

```bash
KUBECONFIG=/tmp/loopzero-mgmt.kubeconfig

# Obter ArgoCD admin password
ARGOCD_PASS=$(kubectl -n platform-system get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)

# Login no ArgoCD
argocd login argocd.dev.loopzero.example --username admin --password "$ARGOCD_PASS" --grpc-web

# Adicionar runtime cluster como destino de deploy
argocd cluster add loopzero-infra-dev-nyc3-runtime --kubeconfig /tmp/loopzero-runtime.kubeconfig --name runtime
```

### Critério de saída Fase 2
- [ ] `curl https://api.dev.loopzero.example/api/v1/health` → `{"status":"ok"}`
- [ ] `curl https://api.dev.loopzero.example/api/v1/health/deep` → `{"status":"ok","database":"ok"}`
- [ ] `kubectl get tenants` → funciona (CRDs aplicados)
- [ ] TenantController pod Running com leader election
- [ ] Gitea tem org `loopzero-platform` + repo `gitops`
- [ ] ArgoCD conhece o runtime cluster

---

## Fase 3 — Serviços de Suporte (MinIO + Argo Workflows)

### 3.1 MinIO no Management Cluster

MinIO armazena: container images (cache), frontend bundles, build cache, source bundles.

Adicionar em `bootstrap.ts` ou via Helm direto:

```bash
KUBECONFIG=/tmp/loopzero-mgmt.kubeconfig

helm repo add minio https://charts.min.io/
helm install minio minio/minio \
  --namespace platform-system \
  --set fullnameOverride=minio \
  --set mode=standalone \
  --set rootUser=loopzero-admin \
  --set rootPassword="$(openssl rand -hex 16)" \
  --set persistence.size=10Gi \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=128Mi \
  --set resources.limits.cpu=500m \
  --set resources.limits.memory=512Mi
```

**Criar buckets necessários:**
```bash
# Port-forward para acessar MinIO
kubectl -n platform-system port-forward svc/minio 9000:9000 &

# Configurar mc client
mc alias set loopzero http://localhost:9000 loopzero-admin <password>

# Criar buckets
mc mb loopzero/source-bundles    # Source code uploads
mc mb loopzero/build-cache       # Dependency cache
mc mb loopzero/artifacts         # Build artifacts (images, frontend bundles)
mc mb loopzero/manifests         # Generated K8s manifests
```

### 3.2 Argo Workflows no Build Plane

**Decisão arquitetural:** O spec prevê um build cluster separado. Na topologia DO atual, builds rodam no management cluster (simplificação para dev) ou no runner VM.

**Opção recomendada para dev:** Instalar Argo Workflows no management cluster em namespace `argo`.

```bash
KUBECONFIG=/tmp/loopzero-mgmt.kubeconfig

# Instalar Argo Workflows
helm repo add argo https://argoproj.github.io/argo-helm
helm install argo-workflows argo/argo-workflows \
  --namespace argo --create-namespace \
  --set fullnameOverride=argo-workflows \
  --set controller.workflowWorkers=32 \
  --set controller.podWorkers=32 \
  --set controller.resources.requests.cpu=200m \
  --set controller.resources.requests.memory=256Mi \
  --set controller.resources.limits.cpu=500m \
  --set controller.resources.limits.memory=512Mi \
  --set server.enabled=true \
  --set server.resources.requests.cpu=100m \
  --set server.resources.requests.memory=128Mi
```

### 3.3 WorkflowTemplate para Build

O `loopzero-forge` gera workflows programaticamente, mas o template base precisa existir.

```bash
# Aplicar WorkflowTemplate base
kubectl -n argo apply -f - <<'EOF'
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: loopzero-build-v1
spec:
  entrypoint: build-pipeline
  serviceAccountName: argo-workflows
  arguments:
    parameters:
      - name: tenant
      - name: project
      - name: environment
      - name: commit-sha
      - name: callback-url
      - name: callback-secret
  templates:
    - name: build-pipeline
      dag:
        tasks:
          - name: clone
            template: clone-source
          - name: build
            template: build-app
            dependencies: [clone]
          - name: push-manifests
            template: gitops-push
            dependencies: [build]

    - name: clone-source
      container:
        image: alpine/git:2.43.0
        command: [sh, -c]
        args:
          - |
            echo "Cloning source for {{workflow.parameters.tenant}}/{{workflow.parameters.project}}"
            # Clone from Gitea
            git clone http://gitea-http.platform-system.svc.cluster.local:3000/{{workflow.parameters.tenant}}/{{workflow.parameters.project}}.git /workspace
        volumeMounts:
          - name: workspace
            mountPath: /workspace

    - name: build-app
      container:
        image: registry.digitalocean.com/loopzero-registry/loopzero-packs:dev
        command: [sh, -c]
        args:
          - |
            echo "Building {{workflow.parameters.project}}"
            # loopzero-packs auto-detect + build
        volumeMounts:
          - name: workspace
            mountPath: /workspace

    - name: gitops-push
      container:
        image: alpine/git:2.43.0
        command: [sh, -c]
        args:
          - |
            echo "Pushing manifests to GitOps repo"
            # Generate + push K8s manifests to Gitea gitops repo
        volumeMounts:
          - name: workspace
            mountPath: /workspace
EOF
```

**Nota:** Este é um template simplificado. O template real será gerado pelo `loopzero-forge` com steps completos (security scan, cache, image signing, etc.).

### Critério de saída Fase 3
- [ ] MinIO rodando, buckets criados
- [ ] `kubectl -n argo get pods` → argo-workflows-controller Running
- [ ] `argo submit --from workflowtemplate/loopzero-build-v1 -p tenant=test ...` → Workflow completa

---

## Fase 4 — Wire do Deploy Flow

### 4.1 Configurar API → Argo Workflows

A API precisa submeter workflows quando `POST /api/v1/builds` é chamado.

**Env vars adicionais para a API:**
```yaml
ARGO_NAMESPACE: "argo"
BUILD_KUBECONFIG: ""  # vazio = usa in-cluster (API e Argo no mesmo cluster)
```

### 4.2 Configurar API → ArgoCD Sync

Quando o build termina e o gitops-push é feito, a API notifica o ArgoCD:

```bash
# Criar ArgoCD Application apontando para o Gitea gitops repo
kubectl --kubeconfig=/tmp/loopzero-mgmt.kubeconfig apply -f - <<'EOF'
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: workload-apps
  namespace: platform-system
spec:
  project: default
  source:
    repoURL: http://gitea-http.platform-system.svc.cluster.local:3000/loopzero-platform/gitops.git
    targetRevision: HEAD
    path: .
  destination:
    name: runtime
    namespace: default
  syncPolicy:
    automated:
      selfHeal: true
      prune: true
EOF
```

### 4.3 Configurar Argo Workflow Callback → API

Quando um step do workflow completa/falha, o Argo chama de volta a API:

**No WorkflowTemplate, adicionar exit handler:**
```yaml
onExit: callback
templates:
  - name: callback
    container:
      image: curlimages/curl:8.5.0
      command: [curl, -X, POST]
      args:
        - "http://loopzero-api.platform-system.svc.cluster.local:8080/api/v1/builds/{{workflow.parameters.build-id}}/callback"
        - -H
        - "X-Argo-Callback-Secret: {{workflow.parameters.callback-secret}}"
        - -d
        - '{"status": "{{workflow.status}}"}'
```

### 4.4 Configurar CLI → API

```bash
# A CLI precisa saber o endpoint da API
# Isso é configurado durante `loopzero login`:
loopzero login --api-url https://api.dev.loopzero.example
```

### Critério de saída Fase 4
- [ ] `POST /api/v1/builds` cria Build record + submete Argo Workflow
- [ ] Argo Workflow executa: clone → build → push manifests
- [ ] Callback atualiza Build status na API
- [ ] ArgoCD detecta mudança no gitops repo e sincroniza para runtime cluster
- [ ] SSE stream reflete todos os estados em tempo real

---

## Fase 5 — Validação End-to-End

### 5.1 Teste Manual Completo

```bash
# 1. Login
loopzero login --api-url https://api.dev.loopzero.example

# 2. Criar projeto de teste
mkdir /tmp/loopzero-test-app && cd /tmp/loopzero-test-app
echo '{"name": "test-app", "version": "1.0.0"}' > package.json
cat > index.js << 'JS'
const http = require("http");
http.createServer((req, res) => {
  res.writeHead(200);
  res.end("Hello from LoopZero!");
}).listen(3000);
JS

# 3. Inicializar
loopzero init

# 4. Deploy
loopzero deploy

# 5. Verificar
curl https://test-app.dev.loopzero.example
# Deve retornar: "Hello from LoopZero!"
```

### 5.2 Checklist de Validação

```
Deploy Flow:
[ ] loopzero login     → autentica com GitHub OAuth
[ ] loopzero init      → gera loopzero.yaml, cria projeto na API
[ ] loopzero deploy    → retorna build ID, streama eventos SSE
[ ] Build          → Argo Workflow executa todos os steps
[ ] ArgoCD sync    → manifests aplicados no runtime cluster
[ ] App live       → URL acessível com TLS automático

Serialização:
[ ] Deploy concurrent → segundo deploy retorna 409
[ ] Idempotency key   → retry com mesma key retorna cached response
[ ] Lock timeout      → 5s max, liberado em crash

Observabilidade:
[ ] Grafana mostra métricas do build
[ ] Prometheus coleta loopzero_build_duration_seconds
[ ] Logs estruturados (JSON) em todos os componentes

Resiliência:
[ ] Build falha → status FAILED, CLI mostra erro com next action
[ ] ArgoCD down → circuit breaker abre, 503 com retry
[ ] Gitea push conflict → retry automático (até 5x)
```

### 5.3 Teste Automatizado (E2E)

```bash
# Localizado em infra/tests/e2e/
# Ginkgo test que executa o flow completo
cd infra/tests/e2e
ginkgo run --timeout 10m
```

---

## Dependências entre Fases

```
Fase 1 ─────────────────────────┐
  Container Registry            │
  + Images                      │
                                ▼
Fase 2 ─────────────────────────┤
  Database + Migrations         │
  LoopZero API (Deployment)         │
  TenantController (Deployment) │
  Gitea bootstrap               │
  ArgoCD → runtime cluster      │
                                ▼
Fase 3 ─────────────────────────┤
  MinIO (artifacts)             │
  Argo Workflows (build engine) │
  WorkflowTemplate base         │
                                ▼
Fase 4 ─────────────────────────┤
  API → Argo wiring             │
  API → ArgoCD wiring           │
  Argo → API callback           │
  CLI → API configuration       │
                                ▼
Fase 5 ─────────────────────────┘
  End-to-End validation
  Stress test
  Dogfood
```

---

## Variáveis de Ambiente — Referência Completa

### LoopZero API (Deployment)

| Variável | Valor (dev) | Origem |
|----------|-------------|--------|
| `PORT` | `8080` | ConfigMap |
| `METRICS_PORT` | `9090` | ConfigMap |
| `LOOPZERO_ENV` | `production` | ConfigMap |
| `DATABASE_URL` | `postgres://...loopzero?sslmode=require` | Secret `loopzero-api-db` |
| `GITEA_URL` | `http://gitea-http.platform-system.svc.cluster.local:3000` | ConfigMap |
| `GITEA_TOKEN` | `<admin-token>` | Secret `loopzero-api-gitea` |
| `ARGOCD_URL` | `http://argocd-server.platform-system.svc.cluster.local:80` | ConfigMap |
| `ARGOCD_TOKEN` | `<admin-password>` | Secret `loopzero-api-argocd` |
| `ARGOCD_INSECURE` | `true` | ConfigMap |
| `MINIO_ENDPOINT` | `minio.platform-system.svc.cluster.local:9000` | ConfigMap |
| `MINIO_ACCESS_KEY` | `<access-key>` | Secret `loopzero-api-minio` |
| `MINIO_SECRET_KEY` | `<secret-key>` | Secret `loopzero-api-minio` |
| `LOOPZERO_ARGO_CALLBACK_SECRET` | `<random-hex>` | Secret `loopzero-api-argo-callback` |
| `BASE_DOMAIN` | `dev.loopzero.example` | ConfigMap |
| `WORKLOAD_INGRESS_URL` | `https://dev.loopzero.example` | ConfigMap |
| `ARGO_NAMESPACE` | `argo` | ConfigMap |

### TenantController (Deployment)

| Variável | Valor (dev) | Origem |
|----------|-------------|--------|
| `GITEA_URL` | `http://gitea-http.platform-system.svc.cluster.local:3000` | ConfigMap |
| `GITEA_TOKEN` | `<admin-token>` | Secret |

---

## Recursos Cloud Adicionais Necessários

| Recurso | Custo Mensal (dev) | Motivo |
|---------|-------------------|--------|
| DO Container Registry (starter) | $0 | 500MB, suficiente para dev |
| Database `loopzero` | $0 | Mesmo cluster PostgreSQL managed |
| MinIO PVC 10Gi | $0 | Incluso no cluster DOKS |
| Argo Workflows | $0 | Roda no mgmt cluster existente |

**Custo incremental: $0** — tudo roda na infra já provisionada.

---

## Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|--------------|---------|-----------|
| Registry 500MB insuficiente | Média | Build falha | Upgrade para basic ($5/mês) ou cleanup policy |
| Mgmt cluster sem recursos para Argo Workflows | Média | Pods evicted | Monitorar via Grafana; scale nodes se necessário |
| Gitea in-cluster crash perde dados Git | Baixa | Perda de state | PVC com backup; Gitea é mirror, não source of truth |
| ArgoCD não consegue acessar runtime cluster | Baixa | Deploy falha | Testar conectividade na Fase 2; kubeconfig como Secret |
| Runner VM ociosa gerando custo | Certa | $$ desperdiçado | Remover via Pulumi após confirmar que builds rodam no cluster |

---

## Decisões em Aberto (Requer Input)

1. **Build plane: cluster separado vs mgmt cluster?**
   - Spec prevê cluster separado para isolamento de recursos
   - Para dev, rodar no mgmt é mais simples e barato
   - **Recomendação:** mgmt para dev, cluster separado para staging/prod

2. **Gitea VM: manter ou remover?**
   - Gitea agora roda in-cluster via Helm
   - VM está ociosa gerando custo (~$6/mês)
   - **Recomendação:** remover via Pulumi

3. **Container registry: DO ou GitHub Packages?**
   - DO registry integra nativamente com DOKS (zero config para pull)
   - GitHub Packages requer imagePullSecrets
   - **Recomendação:** DO Container Registry (starter, grátis)

4. **Migrations: automáticas no deploy ou manual?**
   - Init container na API que roda migrate antes de start?
   - Ou job separado no pipeline?
   - **Recomendação:** init container (simples, determinístico)
