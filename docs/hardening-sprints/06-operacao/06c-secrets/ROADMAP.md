# Sub-Sprint 6C — `loopzero secret`

**Ação:** Instalar ESO e expor CRUD de secrets via API e CLI com sync cross-cluster.
**DoD:** `loopzero secret set KEY=val` sincroniza para workload em <30s via ESO, valor nunca visível em output.
**Confiança:** MÉDIA
**Esforço:** ~16h (5 tasks sequenciais)
**Pré-requisito:** GATE-6B passou (`loopzero rollback` funcional).
**Regra:** Uma camada por vez. Segurança primeiro. Valores NUNCA em logs ou output. Zero termos internos.

---

## Contexto Técnico

**O que existe:**
- **Secrets table** (migration 000005) — `id, tenant_id, project_id, environment, app, name, encrypted_value (BYTEA), created_at, updated_at`. UNIQUE constraint em `(tenant_id, project_id, environment, app, name)`. **ATENÇÃO:** Coluna `encrypted_value` será renomeada para `value` via nova migration — v1 armazena plaintext (encryption at rest delegada ao PG gerenciado da DigitalOcean). Nome "encrypted" sem encryption é mentira no schema.
- **Secret store (read-only)** (`api/internal/db/secret_store.go`) — `ListNames()` retorna apenas nomes. Wired no router como `GET /api/v1/secrets`.
- **CLI loopzero.yaml** — `LoopZeroYamlApp.secrets?: string[]` campo existe (declaração de quais secrets uma app precisa).
- **Multi-cluster client** — TenantController já usa `BuildClient` e `RuntimeClient`. Padrão estabelecido.
- **ADR 0004** — Encryption at rest: produção usa ESO, dev usa base64.
- **MinIO credentials pattern** — `minio-credentials-secret` no build cluster é referência de como secrets cross-cluster funcionam hoje.

**O que NÃO existe:**
- External Secrets Operator (nenhum chart instalado)
- `POST /api/v1/secrets` (mutação)
- `DELETE /api/v1/secrets/{name}`
- CLI commands `loopzero secret set/list/delete`
- Cross-cluster Secret sync (mgmt → workload)

**O que será REMOVIDO:**
- `operators/api/v1/appsecret_types.go` — CRD custom substituído por ExternalSecret CR nativo do ESO
- `operators/api/v1/appsecret_types_test.go` — testes do CRD removido
- Qualquer referência a AppSecretController no roadmap — ESO assume essa responsabilidade
- `pkg/crypto/` — não será criado. Encryption delegada ao backend do ESO (v1: K8s Secrets, futuro: Vault/KMS).

**Decisões de design (pré-definidas):**

| Decisão | Escolha | Rejeitado | Justificativa |
|---------|---------|-----------|---------------|
| Secret sync engine | External Secrets Operator (ESO) | AppSecretController custom | ESO: CNCF, Apache 2.0, 4.2k+ stars, 20+ backends, releases mensais. Controller custom: ~800 linhas, 1 pessoa entende, só suporta K8s Secrets. Princípio: não reinventar a roda. |
| Encryption | Delega ao backend do ESO. PG armazena plaintext (managed PG encrypts at rest). Coluna renomeada de `encrypted_value` para `value`. | AES-256-GCM custom (pkg/crypto), pgcrypto | Nunca implementar crypto custom. v1 usa K8s Secrets (base64 + etcd encryption at rest). PG gerenciado (DO) já faz encryption at rest no disco. Futuro: trocar backend para Vault sem mudar código. |
| CRD | ExternalSecret (nativo ESO) | AppSecret (custom) | KISS. CRD custom que wrapa outro CRD é complexidade sem valor. ExternalSecret é padrão da indústria. |
| API cria o quê? | K8s Secret no mgmt + ExternalSecret CR no workload | K8s Secret + AppSecret CR | Mesmo fluxo, CRD diferente. API não muda de perspectiva. |
| Namespace target | `{tenant}-{env}` no workload cluster | Custom | Segue padrão do EnvironmentController |
| Secret name no workload | `{app}-secrets` (K8s Secret tipo Opaque) | Um Secret por secret name | Um Secret por app por environment — menos objetos K8s |
| Audit trail | K8s Events nativos + API audit middleware | Tabela custom `audit_log` | Events são nativos, auditáveis, e já têm tooling (kubectl get events). API middleware já loga mutações. |

---

## Task 6C.1 — Instalar ESO via Helm

**Confiança:** ALTA
**Esforço:** ~3h

### Arquitetura cross-cluster (CRÍTICO — ler antes de implementar)

```
┌─────────────────────────┐         ┌─────────────────────────┐
│  MGMT CLUSTER           │         │  WORKLOAD CLUSTER       │
│                         │         │                         │
│  K8s Secret             │  lido   │  ESO (roda AQUI)        │
│  secret-{tenant}-{app}  │ ◄────── │  ClusterSecretStore     │
│  namespace: loopzero-system │  via    │  (usa kubeconfig mgmt)  │
│                         │ kubeconf│                         │
│  API cria secrets aqui  │         │  ExternalSecret CR      │
│                         │         │  → gera K8s Secret      │
│                         │         │  no namespace do tenant │
└─────────────────────────┘         └─────────────────────────┘
```

**ESO roda no WORKLOAD cluster** — ele precisa criar K8s Secrets nos namespaces dos tenants (que estão no workload). O ClusterSecretStore usa um `kubeconfig` Secret para ler secrets do mgmt cluster remotamente.

**NÃO usar `kubernetes.default.svc`** como provider URL — isso apontaria para o próprio workload, não para o mgmt. Reutilizar o pattern do `ClusterCredential` controller que já distribui kubeconfigs para o workload cluster.

### O que fazer

1. **Criar** `infra/helm/external-secrets/values.yaml`
2. **Instalar** ESO **no workload cluster** (não no mgmt)
3. **Criar** kubeconfig Secret no workload cluster com credenciais de leitura do mgmt cluster
4. **Configurar** ClusterSecretStore no workload cluster para ler secrets do mgmt via kubeconfig
5. **Adicionar** tasks no `Taskfile.yml`

### ESO config

```yaml
# infra/helm/external-secrets/values.yaml
installCRDs: true

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 200m
    memory: 128Mi

# Webhook para validação de ExternalSecret CRs
webhook:
  create: true

# Cert controller para TLS do webhook
certController:
  create: true
```

### Kubeconfig Secret (no workload cluster, aponta para mgmt)

O ClusterCredential controller já distribui kubeconfigs. Reutilizar esse pattern para criar um kubeconfig read-only do mgmt no workload cluster.

```yaml
# infra/helm/external-secrets/mgmt-kubeconfig-secret.yaml
# Aplicado no WORKLOAD cluster
# Valores preenchidos pelo Taskfile/Pulumi com credenciais do mgmt cluster
apiVersion: v1
kind: Secret
metadata:
  name: mgmt-cluster-kubeconfig
  namespace: external-secrets
type: Opaque
stringData:
  kubeconfig: |
    apiVersion: v1
    kind: Config
    clusters:
      - cluster:
          server: ${MGMT_API_SERVER}
          certificate-authority-data: ${MGMT_CA_DATA}
        name: mgmt
    users:
      - name: eso-reader
        user:
          token: ${MGMT_ESO_READER_TOKEN}
    contexts:
      - context:
          cluster: mgmt
          user: eso-reader
          namespace: loopzero-system
        name: mgmt
    current-context: mgmt
```

### ClusterSecretStore (no workload cluster)

```yaml
# infra/helm/external-secrets/cluster-secret-store.yaml
# Aplicado no WORKLOAD cluster
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: loopzero-mgmt-store
spec:
  provider:
    kubernetes:
      remoteNamespace: loopzero-system
      server:
        # NÃO usar kubernetes.default.svc — isso apontaria para o workload, não mgmt
        caProvider:
          type: Secret
          name: mgmt-cluster-kubeconfig
          namespace: external-secrets
          key: kubeconfig
      auth:
        # Usa kubeconfig Secret com token do ServiceAccount do mgmt
        token:
          bearerToken:
            name: mgmt-cluster-kubeconfig
            namespace: external-secrets
            key: kubeconfig
```

**Nota:** A configuração exata do provider `kubernetes` com kubeconfig remoto depende da versão do ESO. Verificar docs do ESO para a sintaxe correta de `server.url` + `auth` com kubeconfig externo. O pattern acima é ilustrativo — ajustar conforme a [documentação oficial do Kubernetes provider](https://external-secrets.io/latest/provider/kubernetes/).

### ServiceAccount + RBAC (no MGMT cluster)

```yaml
# infra/helm/external-secrets/rbac.yaml
# Aplicado no MGMT cluster — dá acesso read-only ao ESO do workload
apiVersion: v1
kind: ServiceAccount
metadata:
  name: eso-reader
  namespace: loopzero-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: eso-secret-reader
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch"]
    # Restringir a namespaces específicos se possível (via RoleBinding em vez de ClusterRoleBinding)
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: eso-secret-reader-binding
subjects:
  - kind: ServiceAccount
    name: eso-reader
    namespace: loopzero-system
roleRef:
  kind: ClusterRole
  name: eso-secret-reader
  apiGroup: rbac.authorization.k8s.io
```

**Segurança:** O ServiceAccount `eso-reader` no mgmt tem apenas `get/list/watch` em secrets. Não pode criar, deletar ou modificar. O token desse SA é o que vai no kubeconfig Secret do workload cluster.

### Taskfile

```yaml
eso:install:
  desc: Install External Secrets Operator on workload cluster + RBAC on mgmt cluster
  cmds:
    # 1. Criar ServiceAccount + RBAC read-only no MGMT cluster
    - kubectl apply --context {{.MGMT_CONTEXT}} -f infra/helm/external-secrets/rbac.yaml
    # 2. Gerar token do ServiceAccount e criar kubeconfig Secret no WORKLOAD cluster
    - task: eso:create-mgmt-kubeconfig
    # 3. Instalar ESO no WORKLOAD cluster
    - helm repo add external-secrets https://charts.external-secrets.io
    - helm upgrade --install external-secrets external-secrets/external-secrets
      --kube-context {{.WORKLOAD_CONTEXT}}
      --namespace external-secrets --create-namespace
      -f infra/helm/external-secrets/values.yaml
    # 4. Criar ClusterSecretStore no WORKLOAD cluster (após ESO CRDs estarem disponíveis)
    - kubectl apply --context {{.WORKLOAD_CONTEXT}} -f infra/helm/external-secrets/cluster-secret-store.yaml

eso:create-mgmt-kubeconfig:
  desc: Generate kubeconfig Secret in workload cluster for ESO to read mgmt secrets
  internal: true
  cmds:
    - |
      MGMT_SERVER=$(kubectl config view --context {{.MGMT_CONTEXT}} -o jsonpath='{.clusters[0].cluster.server}')
      MGMT_CA=$(kubectl config view --context {{.MGMT_CONTEXT}} --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
      SA_TOKEN=$(kubectl create token eso-reader --context {{.MGMT_CONTEXT}} -n loopzero-system --duration=8760h)
      kubectl create secret generic mgmt-cluster-kubeconfig \
        --context {{.WORKLOAD_CONTEXT}} \
        --namespace external-secrets \
        --from-literal=kubeconfig="$(cat <<KUBEEOF
      apiVersion: v1
      kind: Config
      clusters:
        - cluster:
            server: ${MGMT_SERVER}
            certificate-authority-data: ${MGMT_CA}
          name: mgmt
      users:
        - name: eso-reader
          user:
            token: ${SA_TOKEN}
      contexts:
        - context:
            cluster: mgmt
            user: eso-reader
            namespace: loopzero-system
          name: mgmt
      current-context: mgmt
      KUBEEOF
      )" --dry-run=client -o yaml | kubectl apply --context {{.WORKLOAD_CONTEXT}} -f -
```

### TDD

1. Teste: ESO pods rodando no namespace `external-secrets` do **workload** cluster
2. Teste: ClusterSecretStore status = Ready no **workload** cluster
3. Teste: criar K8s Secret no **mgmt** cluster → criar ExternalSecret no **workload** → Secret criado no target namespace do workload
4. Teste: kubeconfig Secret existe no workload cluster e aponta para mgmt
5. Teste: ServiceAccount `eso-reader` no mgmt tem apenas get/list/watch (sem create/delete)

### Gate 6C.1
- [ ] ESO rodando no **workload** cluster (não mgmt)
- [ ] kubeconfig Secret funcional (workload lê secrets do mgmt)
- [ ] ClusterSecretStore status Ready
- [ ] ExternalSecret de teste cria Secret no namespace target do workload
- [ ] RBAC mínimo no mgmt (apenas leitura de secrets)

---

## Task 6C.2 — API mutations (set/delete)

**Confiança:** MÉDIA
**Esforço:** ~5h

### O que fazer

0. **Migration** — Criar `000009_rename_encrypted_value.up.sql`: `ALTER TABLE secrets RENAME COLUMN encrypted_value TO value;` (forward-only, coluna não estava em uso pela API — apenas pelo migration DDL)
1. **Adicionar** `POST /api/v1/secrets` — criar/atualizar secret
2. **Adicionar** `DELETE /api/v1/secrets/{name}` — remover secret
3. **Adicionar** métodos ao SecretStore: `Set()`, `Delete()`
4. **Ao criar via POST:**
   - Persistir no PostgreSQL (valor como está — encryption delegada ao backend)
   - Criar K8s Secret no namespace do tenant no mgmt cluster
   - Criar ExternalSecret CR no namespace do workload cluster
5. **Ao deletar:**
   - Remover do PostgreSQL
   - Remover K8s Secret do mgmt
   - Remover ExternalSecret CR (ESO remove o Secret target automaticamente via `creationPolicy: Owner`)
6. **Wiring** no router em `server.go`

### POST /api/v1/secrets

**Request:**
```json
{
  "name": "DATABASE_URL",
  "value": "postgres://...",
  "environment": "production",
  "app": "api"
}
```

**Validação:**
- `name`: regex `^[A-Z][A-Z0-9_]*$` (env var naming convention)
- `value`: não vazio, max 64KB
- `environment`: obrigatório
- `app`: obrigatório (deve existir no projeto)

**Mensagem de erro para nome inválido:**
```
Secret name must be uppercase with underscores (e.g., DATABASE_URL). Got: 'database_url'.
```

**Response (200):**
```json
{
  "name": "DATABASE_URL",
  "environment": "production",
  "app": "api",
  "created": true
}
```

`created: true` se novo, `created: false` se update (upsert via `ON CONFLICT ... DO UPDATE`).

**Valor NUNCA retornado na response.**

### DELETE /api/v1/secrets/{name}

**Query params:** `?environment=production&app=api`

**Response (200):**
```json
{
  "name": "DATABASE_URL",
  "deleted": true
}
```

### ExternalSecret CR criado pela API

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: es-{app}-{name}
  namespace: {tenant}-{env}                   # workload cluster namespace
  labels:
    loopzero.io/tenant: {tenant}
    loopzero.io/project: {project}
    loopzero.io/app: {app}
spec:
  refreshInterval: 30s                        # sync <30s ✅
  secretStoreRef:
    name: loopzero-mgmt-store
    kind: ClusterSecretStore
  target:
    name: {app}-secrets                       # agrupado por app
    creationPolicy: Merge                     # múltiplos ExternalSecrets → 1 K8s Secret
    deletionPolicy: Merge
  data:
    - secretKey: {NAME}                       # ex: DATABASE_URL
      remoteRef:
        key: secret-{tenant}-{app}-{name}     # K8s Secret no mgmt
        property: value
```

### Middleware

- Auth (obrigatório)
- Environment lock (mutação em secret é mutação de estado)
- Idempotency-Key (para POST)
- Correlation-ID

### TDD

1. Teste: POST novo secret → 200 com `created: true`
2. Teste: POST update (mesmo name) → 200 com `created: false`
3. Teste: POST sem auth → 401
4. Teste: POST com name inválido ("lowercase") → 400 com mensagem sugestiva
5. Teste: POST com value vazio → 400
6. Teste: DELETE existente → 200
7. Teste: DELETE inexistente → 404
8. Teste: Response NUNCA contém campo `value`
9. Teste: Tenant isolation (secret de outro tenant → 404)
10. Teste: POST cria K8s Secret no mgmt namespace
11. Teste: POST cria ExternalSecret CR no workload namespace
12. Teste: DELETE remove ExternalSecret CR
13. Teste: Endpoint wired no router (teste de integração)

### Gate 6C.2
- [ ] POST cria/atualiza secret no PG + K8s Secret + ExternalSecret CR
- [ ] DELETE remove de PG + mgmt + ExternalSecret CR
- [ ] Valor NUNCA na response
- [ ] Lock + idempotency aplicados
- [ ] **Wired no router** (verificado por teste de integração)
- [ ] 13 testes passam

---

## Task 6C.3 — CLI commands

**Confiança:** ALTA
**Esforço:** ~4h

### O que fazer

1. **Criar** `cli/src/commands/secret.ts` — classe `SecretCommand` com subcomandos
2. **Wiring** em `cli/src/index.ts` como `loopzero secret <subcommand>`

### Subcomandos

#### `loopzero secret set <name>=<value> [--env] [--app]`

```
Secret DATABASE_URL set for production/api.
```

- Validação: `name=value` format obrigatório
- Valor NUNCA exibido no output (nem em `--json`)
- `--json`: `{"name": "DATABASE_URL", "environment": "production", "app": "api", "created": true}`

#### `loopzero secret list [--env] [--app]`

```
Environment: production

  DATABASE_URL    api
  REDIS_URL       api
  API_KEY         worker
```

- `--json`: `{"secrets": [{"name": "...", "environment": "...", "app": "..."}]}`

#### `loopzero secret delete <name> [--env] [--app] [--yes]`

```
Are you sure you want to delete DATABASE_URL from production/api? (y/N)
Secret DATABASE_URL deleted from production/api.
```

- `--yes` skipa confirmação (CI mode)
- `--json`: `{"name": "DATABASE_URL", "deleted": true}`

### Exit codes

| Code | set | list | delete |
|------|-----|------|--------|
| 0 | Criado/atualizado | Listado | Deletado |
| 1 | Nome inválido, formato errado | — | — |
| 3 | API failure | API failure | API failure |
| 4 | Auth error | Auth error | Auth error |

### TDD

1. Teste: `secret set DB_URL=postgres://...` → exit 0, valor NÃO no output
2. Teste: `secret set` sem `=` → exit 1, mensagem de uso
3. Teste: `secret set lowercase=x` → exit 1, mensagem de validação sugestiva
4. Teste: `secret list` → exit 0, lista nomes
5. Teste: `secret list --json` → JSON com schema correto
6. Teste: `secret delete DB_URL` → exit 0, confirmação exibida
7. Teste: `secret delete DB_URL --yes` → exit 0, sem confirmação
8. Teste: sem auth (qualquer subcomando) → exit 4
9. Teste: sem loopzero.yaml → exit 1
10. Teste: zero termos internos em qualquer cenário
11. Teste: valor nunca aparece em writeTo ou writeError calls

### Gate 6C.3
- [ ] 3 subcomandos funcionais
- [ ] Valor NUNCA em output (critical security check)
- [ ] Zero termos internos
- [ ] 11 testes passam

---

## Task 6C.4 — Remover AppSecret CRD

**Confiança:** ALTA
**Esforço:** ~1h

### O que fazer

1. **Remover** `operators/api/v1/appsecret_types.go`
2. **Remover** `operators/api/v1/appsecret_types_test.go`
3. **Remover** `operators/config/crd/bases/` — CRD YAML do AppSecret (se existir)
4. **Remover** referências ao AppSecret em `operators/api/v1/groupversion_info.go` (SchemeBuilder)
5. **Remover** condition constants de `TypeSynced` / `TypeRotationCurrent` se exclusivas do AppSecret
6. **Verificar** que nenhum controller, route, ou test referencia AppSecret

### TDD

1. Teste: `go build ./...` no módulo operators compila sem erro
2. Teste: `go test ./...` no módulo operators passa
3. Teste: grep confirma zero referências a "AppSecret" no codebase (exceto docs/git history)

### Gate 6C.4
- [ ] CRD removido sem breaking changes
- [ ] Operators compila e testes passam
- [ ] Zero referências residuais a AppSecret

---

## Task 6C.5 — Validação E2E

**Confiança:** MÉDIA
**Esforço:** ~4h

### O que fazer

1. **Teste E2E:**
   - `loopzero secret set DATABASE_URL=postgres://localhost/mydb --env production --app api`
   - Verificar: K8s Secret existe no mgmt namespace
   - Verificar: ExternalSecret CR existe no workload namespace
   - Aguardar <30s
   - Verificar: K8s Secret existe no workload namespace com valor correto
   - `loopzero secret list --env production` → mostra DATABASE_URL
   - `loopzero secret set DATABASE_URL=postgres://NEW --env production --app api` (update)
   - Aguardar <30s → workload Secret atualizado
   - `loopzero secret delete DATABASE_URL --env production --app api --yes`
   - Verificar: ExternalSecret CR removido → Secret removido do workload
2. **Verificar**: valor NUNCA aparece em logs da API
3. **Verificar**: ESO status conditions mostram sync saudável

### Gate 6C.5
- [ ] set → sync para workload em <30s
- [ ] update → re-sync em <30s
- [ ] delete → cleanup no workload
- [ ] Valor nunca em logs
- [ ] ESO ExternalSecret status = SecretSynced
- [ ] 3 deploys estáveis pós-secrets

---

## Evolução futura (sem mudança de código)

| Fase | Backend ESO | Esforço | Mudança na API/CLI |
|------|-------------|---------|-------------------|
| v1 (agora) | K8s Secrets no mgmt cluster | 0 | Nenhuma |
| v2 | HashiCorp Vault | Trocar `provider` no ClusterSecretStore | Nenhuma |
| v3 | AWS Secrets Manager / GCP SM | Trocar `provider` no ClusterSecretStore | Nenhuma |

**Zero mudança na API. Zero mudança no CLI. Apenas configuração do ESO.**

---

## Limitações Aceitas (v1)

| Limitação | Justificativa | Futuro |
|-----------|---------------|--------|
| K8s Secrets como backend (base64, não Vault) | Simples. ESO permite trocar backend depois. | Integrar com HashiCorp Vault |
| Sem secret versioning | YAGNI. Não há demanda. | Versionamento via Vault se necessário |
| Sync eventual (~30s) | ESO refreshInterval = 30s. Aceitável. | Webhook trigger para sync imediato |
| Um K8s Secret por app (todas env vars juntas) | Simples. Evita explosão de Secrets. | Separar por sensibilidade se necessário |
| Sem key rotation automática | ESO com K8s backend não tem rotation nativa | Migrar para Vault com rotation |

---

## GATE-6C

- [ ] ESO instalado e ClusterSecretStore Ready
- [ ] `loopzero secret set` cria K8s Secret no mgmt + ExternalSecret CR
- [ ] ESO sincroniza para workload em <30s
- [ ] `loopzero secret list` mostra nomes (nunca valores)
- [ ] `loopzero secret delete` remove de mgmt + workload
- [ ] Valor NUNCA em logs, output, ou response
- [ ] AppSecret CRD removido (sem dead code)
- [ ] Zero termos internos
- [ ] Testes passam (unit API + unit CLI + E2E)
- [ ] 3 deploys estáveis pós-secrets
