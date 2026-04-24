# Plano: Sistematizacao Self-Hosted — Zero Gaps

> **Status:** Implementado (code complete, pending E2E validation)
> **Data:** 2026-04-24
> **Classificacao:** T2 (Contratos + Operacao)
> **Contexto:** Droplet E2E revelou 16 gaps entre "charts instalados" e "plataforma funcional". Nenhum cliente conseguiria instalar sem intervencao manual.

## Objetivo

Um cliente roda `loopzero platform install -f values.yaml` e em 10 minutos tem a plataforma 100% funcional — zero kubectl, zero patches manuais, zero debug.

---

## Diagnostico: 16 Gaps Encontrados no E2E

### Categoria A: Helm Charts (recursos faltando ou errados)

| # | Gap | Impacto | Fix |
|---|---|---|---|
| A1 | NetworkPolicies bloqueiam egress cross-namespace | API nao alcanca Gitea/ArgoCD/Ory | Charts: egress rules devem incluir namespaces de terceiros |
| A2 | `JWKS_URL` env var nao setado (chart usa `JWKS_ENDPOINT`) | JWT validation falha | Chart loopzero-api: corrigir nome do env var no configmap |
| A3 | `JWT_ISSUER` env var nao setado | JWT issuer validation falha | Chart loopzero-api: adicionar campo no values |
| A4 | `GITEA_TOKEN` vazio no deploy | API nao consegue criar repos no Gitea | Chart loopzero-api: secret ref para Gitea token |
| A5 | loopzero-build-cwts nao cria ServiceAccount | Argo Workflow falha: SA not found | Chart loopzero-build-cwts: adicionar SA + RBAC |
| A6 | loopzero-build-cwts nao cria ResourceQuota com SA | Build pods nao tem permissao | Chart loopzero-build-cwts: Role + RoleBinding |
| A7 | imagePullSecrets template bugado (double-nesting) | Pods nao pullam imagens | CORRIGIDO v0.1.5 |

### Categoria B: Installer (steps faltando)

| # | Gap | Impacto | Fix |
|---|---|---|---|
| B1 | Migrations nao executadas | API crasha: relation does not exist | Installer: step de migration apos PG |
| B2 | Gitea org + token nao criados | API nao consegue push para Gitea | Installer: step de bootstrap Gitea |
| B3 | ArgoCD token nao provisionado | API nao consegue sync | Installer: step de bootstrap ArgoCD |
| B4 | Hydra `SECRETS_SYSTEM` nao configurado | JWT signing falha | Installer: gerar secret e passar para Helm |
| B5 | Hydra `strategies.access_token=jwt` nao setado | Tokens opacos em vez de JWT | Installer: setar no values do Hydra |
| B6 | Kubeconfigs nao montados nos pods | Controller/API nao alcanam build/workload clusters | Installer: criar secret com kubeconfig |
| B7 | imagePullSecret nao criado | Pods falham pull do DOCR | CORRIGIDO no installer |
| B8 | User/tenant/project seed nao feito | `loopzero deploy` falha: no tenant | Installer: PlatformBootstrap ou seed step |

### Categoria C: Configuracao (valores faltando no customer values)

| # | Gap | Impacto | Fix |
|---|---|---|---|
| C1 | Nao ha campo para Hydra system secret | Installer nao sabe o secret | Values: `auth.systemSecret` (auto-gerado se vazio) |
| C2 | Nao ha campo para kubeconfig paths | Multi-cluster nao funciona | Values: `clusters.build.kubeconfig`, `clusters.workload.kubeconfig` |

---

## Arquitetura do Installer Corrigido

### Fluxo completo (8 steps)

```
Step 1: Validate cluster (kubectl, helm, nodes, storage)
Step 2: Install prerequisites (cert-manager, traefik, PG, Gitea, ArgoCD, Ory, MinIO)
         - Hydra com SECRETS_SYSTEM e strategies.access_token=jwt
         - Ory Kratos com DSN e identity schema
Step 3: Create registry credentials (imagePullSecret)
Step 4: Run database migrations
Step 5: Bootstrap services
         - Gitea: criar org "loopzero", gerar admin token
         - ArgoCD: extrair admin password, gerar API token
         - Hydra: criar OAuth2 client "loopzero-cli"
Step 6: Install CRDs
Step 7: Install LoopZero platform
         - Todos os env vars corretos (JWKS_URL, JWT_ISSUER, GITEA_TOKEN, etc.)
         - Kubeconfig secrets montados
         - NetworkPolicies com egress correto
Step 8: Validate (health checks, helm test, loopzero whoami)
```

### Valores que o installer gera automaticamente

| Valor | Fonte | Onde usado |
|---|---|---|
| `GITEA_TOKEN` | Step 5: Gitea API | loopzero-api, loopzero-controller, loopzero-tenant-controller |
| `ARGOCD_TOKEN` | Step 5: ArgoCD admin secret | loopzero-api, loopzero-controller |
| `HYDRA_SYSTEM_SECRET` | Step 2: auto-gerado (32 chars) | Ory Hydra values |
| `HYDRA_CLIENT_ID` | Step 5: Hydra admin API | loopzero-cloud |
| `DATABASE_URL` | Customer values ou Step 2 (PG install) | loopzero-api, loopzero-controller |
| `JWKS_URL` | Derivado: `http://ory-hydra-public.ory.svc:4444/.well-known/jwks.json` | loopzero-api |
| `JWT_ISSUER` | Derivado: `http://ory-hydra-public.ory.svc:4444/` | loopzero-api |
| `MINIO_ACCESS_KEY/SECRET` | Step 2 (MinIO install) ou customer values | loopzero-api |
| Kubeconfig | Customer fornece ou auto-detect (single-cluster) | loopzero-api, loopzero-controller, loopzero-tenant-controller |

---

## Correcoes nos Helm Charts

### A1: NetworkPolicies — egress cross-namespace

**Problema:** Os charts criam NetworkPolicies que bloqueiam egress exceto DNS. Em ambiente com namespaces separados (padrao CNCF que implementamos), a API nao alcanca Gitea/ArgoCD/Ory.

**Fix:** O values.yaml do chart loopzero-api ja tem `networkPolicy.extraEgress` — mas o default no profile dev desabilita NetworkPolicies inteiramente. Para producao, o installer deve gerar o extraEgress correto baseado nos namespaces dos prerequisites.

**Decisao:** Para o installer, desabilitar NetworkPolicies no install e habilitar apos bootstrap (quando os endpoints sao conhecidos). Ou gerar as rules automaticamente.

### A2 + A3: Env vars JWT

**Problema:** O chart loopzero-api usa `JWKS_ENDPOINT` no configmap mas o codigo le `JWKS_URL`.

**Fix:** Corrigir o template do configmap para usar `JWKS_URL` e `JWT_ISSUER`.

```yaml
# charts/loopzero-api/templates/configmap.yaml
JWKS_URL: {{ .Values.config.jwksUrl | quote }}
JWT_ISSUER: {{ .Values.config.jwtIssuer | quote }}
```

### A4: Gitea token

**Problema:** O chart nao tem secret para GITEA_TOKEN.

**Fix:** O installer cria um Kubernetes Secret com o token e o chart referencia via `existingSecret`.

### A5 + A6: loopzero-build-cwts SA + RBAC

**Problema:** O chart CWTs nao cria o ServiceAccount que os workflows usam.

**Fix:** Adicionar ao chart:
- `templates/serviceaccount.yaml` — SA `loopzero-build-sa`
- `templates/role.yaml` — permissoes para pods, PVCs, secrets
- `templates/rolebinding.yaml`

---

## Correcoes no Installer

### B1: Migrations

**Fix:** Apos instalar PG (ou receber external PG URL), rodar migrations:

```typescript
// cli/src/installer/migrations.ts
function runMigrations(databaseUrl: string, log: (msg: string) => void): void {
  // Copy migrations to temp, then exec via psql or migrate binary
  execSync(`migrate -database "${databaseUrl}" -path /migrations up`);
}
```

O installer pode usar o migrate binary dentro do API pod (que ja o tem) ou copiar os .sql para o host e rodar via psql.

### B2: Bootstrap Gitea

```typescript
// cli/src/installer/bootstrap-gitea.ts
function bootstrapGitea(giteaUrl: string, adminUser: string, adminPass: string): string {
  // 1. Create org "loopzero"
  fetch(`${giteaUrl}/api/v1/orgs`, { method: 'POST', body: { username: 'loopzero' } });
  // 2. Generate admin token
  const { sha1 } = fetch(`${giteaUrl}/api/v1/users/${adminUser}/tokens`, { body: { name: 'loopzero-api' } });
  // 3. Create K8s secret with token
  return sha1;
}
```

### B3: Bootstrap ArgoCD

```typescript
function bootstrapArgoCD(namespace: string): string {
  // 1. Read admin password from secret
  const pass = execSync(`kubectl get secret -n ${namespace} argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d`);
  // 2. Return password (or generate API token via ArgoCD API)
  return pass;
}
```

### B4 + B5: Hydra config

O installer gera o system secret e passa para o Helm values do Hydra:

```typescript
function installHydra(config: PlatformConfig): void {
  const systemSecret = config.auth.systemSecret || generateSecret(32);
  
  installOrUpgrade({
    name: 'ory-hydra',
    chart: 'ory/hydra',
    namespace: 'ory',
    values: {
      'hydra.config.dsn': config.database.url.replace('/loopzero', '/hydra'),
      'hydra.config.urls.self.issuer': `http://ory-hydra-public.ory.svc.cluster.local:4444/`,
      'hydra.config.strategies.access_token': 'jwt',
      'hydra.config.secrets.system[0]': systemSecret,
      'hydra.dev': 'true',
      'hydra.automigration.enabled': 'true',
    },
  });
}
```

### B6: Kubeconfigs

Para single-cluster (self-hosted simples), o installer detecta automaticamente:

```typescript
function createKubeconfigs(log: (msg: string) => void): void {
  // Single-cluster mode: use in-cluster config (ServiceAccount token)
  // Multi-cluster mode: customer provides kubeconfig files
  
  // For single-cluster, create a secret with the current kubeconfig
  execSync(`kubectl create secret generic loopzero-kubeconfigs -n loopzero-system \
    --from-file=build-kubeconfig=<(kubectl config view --raw --minify) \
    --from-file=workload-kubeconfig=<(kubectl config view --raw --minify) \
    --dry-run=client -o yaml | kubectl apply -f -`);
}
```

### B8: Seed data

O installer cria o primeiro admin user e tenant:

```typescript
function seedPlatform(databaseUrl: string, adminEmail: string): void {
  // Run seed SQL
  const seed = `
    INSERT INTO users (id, email, display_name, username) 
    VALUES (gen_random_uuid(), '${adminEmail}', 'Admin', 'admin') 
    ON CONFLICT DO NOTHING;
    -- Tenant created by PlatformBootstrap CR
  `;
  execSync(`psql "${databaseUrl}" -c "${seed}"`);
}
```

---

## Customer Values Schema (atualizado)

```yaml
# customer-values.yaml — COMPLETO
domain: "paas.customer.com"
clusterIssuer: "letsencrypt-prod"

# Admin account (created during install)
admin:
  email: "admin@customer.com"
  password: "initial-password"    # Kratos identity

# Database
database:
  external: true
  url: "postgres://user:pass@pg.customer.com:5432/loopzero?sslmode=require"
  # Hydra/Kratos get their own DBs (auto-created if PG user has createdb)
  autoCreateDbs: true

# Registry — for tenant app images
registry:
  type: "harbor"
  endpoint: "harbor.customer.com"
  username: "loopzero-robot"
  password: "..."

# Auth
auth:
  type: "bundled"
  systemSecret: ""               # Auto-generated if empty (32 chars)

# Storage
storage:
  endpoint: "s3.amazonaws.com"
  bucket: "loopzero-artifacts"
  accessKey: "..."
  secretKey: "..."

# Clusters (single-cluster = leave empty)
clusters:
  mode: "single"                 # single | multi
  # Multi-cluster mode:
  # build:
  #   kubeconfigPath: "/path/to/build-kubeconfig"
  # workload:
  #   kubeconfigPath: "/path/to/workload-kubeconfig"

# Sizing
profile: "production"

# Network policies (disable for simpler setup, enable for hardened)
networkPolicies:
  enabled: false                 # Recommended: enable after initial setup
```

---

## Ordem de implementacao

| # | Task | Tipo | Esforco | Impacto | Status |
|---|---|---|---|---|---|
| 1 | Fix chart loopzero-api: `JWKS_URL` + `JWT_ISSUER` no configmap | Chart | Baixo | A2, A3 | ✅ Done (prev session) |
| 2 | Fix chart loopzero-api: secret ref para GITEA_TOKEN | Chart | Baixo | A4 | ✅ Done (secret.yaml) |
| 3 | Fix chart loopzero-build-cwts: SA + RBAC | Chart | Baixo | A5, A6 | ✅ Done (prev session) |
| 4 | Installer: step migrations (via API pod) | Installer | Medio | B1 | ✅ Done (bootstrap.ts) |
| 5 | Installer: step bootstrap Gitea (org + token) | Installer | Medio | B2 | ✅ Done (Gitea 1.25 scopes) |
| 6 | Installer: step bootstrap ArgoCD (token) | Installer | Medio | B3 | ✅ Done (bootstrap.ts) |
| 7 | Installer: Hydra config (system secret + jwt strategy) | Installer | Medio | B4, B5 | ✅ Done (prerequisites.ts) |
| 8 | Installer: kubeconfig secrets (auto-detect single/multi) | Installer | Medio | B6 | ✅ Done (bootstrap.ts) |
| 9 | Installer: seed admin user | Installer | Baixo | B8 | ✅ Done (bootstrap.ts) |
| 10 | Installer: Kratos identity + Hydra OAuth2 client | Installer | Medio | Auth flow | ✅ Done (bootstrap.ts) |
| 11 | Chart loopzero-api: NetworkPolicy com egress parametrizavel | Chart | Medio | A1 | ✅ Done (disabled by default, enabled for production profile) |
| 12 | Publicar charts v0.2.0 (com todos os fixes) | CI | Baixo | Release | ⏳ Pending: push + CI |
| 13 | E2E completo: droplet clean install → loopzero deploy → app running | Validacao | Alto | Prova | ⏳ Pending |

---

## Criterio de aceite final

```bash
# 1. Customer cria values.yaml
# 2. Customer roda UM comando:
loopzero platform install -f customer-values.yaml --profile production

# 3. Em < 10 minutos:
#    - Todos os pods Running
#    - loopzero whoami funciona
#    - loopzero deploy funciona (build completa, app live)
#    - Sem NENHUM kubectl manual

# 4. Output final:
Step 8/8: Done!

  Platform URL:    https://paas.customer.com
  Dashboard:       https://cloud.paas.customer.com
  API:             https://api.paas.customer.com
  Admin:           admin@customer.com

  Run: loopzero login --api-url https://api.paas.customer.com
```
