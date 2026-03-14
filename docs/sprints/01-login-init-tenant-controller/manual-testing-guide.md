# Sprint 1 — Guia de Testes Manuais

> Passo a passo para validar manualmente as entregas do Sprint 1.
> Pré-requisitos: Go 1.22+, Node.js 20+, Docker, k3d, Task (taskfile.dev)

---

## 1. Testes Unitarios (sem infraestrutura)

Estes testes rodam sem clusters, sem banco, sem dependencias externas.

### 1.1 API — Testes unitarios Go

```bash
cd api && go test ./tests/unit/... -v
```

**O que verificar:**
- `TestAuthMiddleware` — 8 sub-testes cobrindo:
  - Requisicao sem header → 401 UNAUTHORIZED
  - Token invalido → 401
  - Token valido + 1 tenant → auto-resolve com UserContext
  - Token valido + N tenants + X-LoopZero-Tenant → resolve correto
  - Token valido + N tenants sem X-LoopZero-Tenant → 409 TENANT_REQUIRED
  - Token valido + 0 tenants → 403 NO_TENANT
  - Token + tenant nao-membro → 403
  - Cache hit: segunda chamada com mesmo token nao chama validator
  - Cache expirado: validator chamado novamente apos TTL
- `TestServerRouting` — 7 sub-testes:
  - GET /api/v1/health e publico (200, sem auth)
  - GET /api/v1/health/deep e publico (nao retorna 401)
  - GET /api/v1/auth/whoami retorna 401 sem token
  - GET /api/v1/auth/whoami retorna 200 com token valido
  - POST /api/v1/projects retorna 401 sem token
  - Todas respostas tem header Correlation-ID
  - Correlation-ID do cliente e preservado
- `TestServerNilLogger` — NewRouter nao entra em panic quando Logger e nil
- `TestProjectsRoute` — 6 sub-testes:
  - POST → 201 com project ID
  - POST mesmo nome → 409
  - POST nome invalido (nao RFC 1123) → 400
  - POST sem auth → 401
  - POST chama TenantCRManager.EnsureTenant
  - POST concorrente mesmo nome → exatamente 1x 201 e 9x 409

### 1.2 CLI — Testes unitarios Jest

```bash
cd cli && npm test
```

**O que verificar:**
- `InitCommand` — 11 testes:
  - Gera loopzero.yaml no diretorio do projeto
  - loopzero.yaml contem nome do projeto e apps
  - loopzero.yaml nao contem campo "tenant"
  - Sai com exit 1 se loopzero.yaml ja existe (sem --force)
  - Sobrescreve com --force
  - Sai com exit 4 se nao autenticado
  - Adiciona .env.local ao .gitignore
  - Nao duplica .env.local no .gitignore
  - Chama createProject com payload correto
  - CI mode: exit 1 sem --yes, exit 0 com --yes
  - YAML quoting: caracteres especiais sao quoted, nomes simples nao

### 1.3 Operators — Testes unitarios Go

```bash
# Requer envtest binaries (setup-envtest)
# Instale com: go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
export KUBEBUILDER_ASSETS=$(setup-envtest use -p path)

cd operators && go test ./... -v -short
```

**O que verificar:**
- TenantController reconcile loop (47 specs via Ginkgo)
- State machine transitions
- Crash recovery: reconcile e idempotente

### 1.4 Todos os testes unitarios via Task

```bash
task test:unit
```

Roda todos os testes unitarios de todos os componentes de uma vez.

---

## 2. Subindo a Infraestrutura Local

### 2.1 Setup completo (primeira vez)

```bash
task setup
```

Isto executa sequencialmente:
1. `registry:create` — cria registry local (k3d-loopzero-registry:5000)
2. `clusters:create` — cria 3 clusters k3d (loopzero-mgmt, loopzero-build, loopzero-workload)
3. `infra:install` — instala PostgreSQL, Gitea, MinIO, ArgoCD, cert-manager, Traefik, Prometheus, OTel
4. `crds:install` — aplica CRDs do LoopZero no cluster mgmt
5. `db:migrate` — roda migrations no PostgreSQL

**Tempo estimado:** 10-15 minutos na primeira execucao.

### 2.2 Verificar clusters

```bash
task clusters:verify
```

Deve mostrar `cluster-info` dos 3 clusters sem erros.

### 2.3 Verificar pods do mgmt cluster

```bash
kubectl --context k3d-loopzero-mgmt get pods -n loopzero-system
```

**Esperado:** pods de PostgreSQL, Gitea, MinIO em estado `Running`.

```bash
kubectl --context k3d-loopzero-mgmt get pods -n argocd
```

**Esperado:** pods do ArgoCD em estado `Running`.

### 2.4 Verificar banco de dados

```bash
# Port-forward para acessar o PostgreSQL
kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/postgresql 5432:5432 &

# Conectar e verificar tabelas
PGPASSWORD=loopzero-dev-password psql -h localhost -p 5432 -U loopzero -d loopzero -c '\dt'
```

**Esperado:** tabelas `users`, `tenants`, `tenant_members`, `projects`, `schema_migrations`.

### 2.5 Verificar CRDs

```bash
kubectl --context k3d-loopzero-mgmt get crd | grep loopzero
```

**Esperado:** `tenants.loopzero.io` listado.

---

## 3. Testando a API Manualmente

### 3.1 Iniciar o servidor API

```bash
cd api && go run ./cmd/main.go
```

O servidor inicia na porta 3001 por padrao. Note que nesta fase, `TokenValidator`, `MemberStore`, `ProjectStore` e `TenantCRMgr` sao `nil` — apenas os endpoints publicos funcionam.

### 3.2 Health check

```bash
# Health simples
curl -s http://localhost:3001/api/v1/health | jq .
```

**Esperado:** `{"status": "ok"}` com HTTP 200.

```bash
# Deep health (checa DB — vai retornar 503 se DB nao estiver conectado)
curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/v1/health/deep
```

**Esperado:** `503` (DB nao esta wired no main.go ainda).

### 3.3 Correlation-ID

```bash
# Verificar que Correlation-ID e gerado automaticamente
curl -s -D - http://localhost:3001/api/v1/health 2>&1 | grep -i correlation
```

**Esperado:** header `Correlation-Id` presente na resposta.

```bash
# Verificar que Correlation-ID do cliente e preservado
curl -s -D - -H "Correlation-ID: meu-id-123" http://localhost:3001/api/v1/health 2>&1 | grep -i correlation
```

**Esperado:** `Correlation-Id: meu-id-123` na resposta.

### 3.4 Rotas protegidas (retornam 404 sem validators wired)

Com `TokenValidator` nil, as rotas protegidas nao sao registradas:

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/v1/auth/whoami
```

**Esperado:** `404` (rota nao registrada porque TokenValidator e nil).

> **Nota:** Para testar rotas protegidas de verdade, e necessario conectar o banco
> de dados e implementar o `GitHubTokenValidator` real. Isso sera feito em sprints futuros.
> Os testes unitarios cobrem toda a logica de auth com fakes/doubles.

---

## 4. Testando o CLI Manualmente

### 4.1 Instalar dependencias

```bash
cd cli && npm install
```

### 4.2 Build

```bash
cd cli && npm run build
```

### 4.3 loopzero init (modo non-interativo)

```bash
# Criar um diretorio temporario
mkdir /tmp/test-loopzero-init && cd /tmp/test-loopzero-init

# Inicializar um projeto Node.js para simular monorepo
npm init -y

# Rodar loopzero init (vai falhar na chamada API pois nao ha servidor — mas gera loopzero.yaml)
node /caminho/para/cli/dist/index.js init --yes --name my-test-project
```

**O que verificar:**
- `loopzero.yaml` foi criado no diretorio
- Conteudo contem `project: my-test-project`
- `.gitignore` foi criado/atualizado com `.env.local`
- Nenhum termo interno na saida (ArgoCD, Gitea, CRD, etc)

### 4.4 loopzero init com --force

```bash
# Rodar novamente sem --force → deve falhar (exit 1)
node /caminho/para/cli/dist/index.js init --yes --name my-test-project
echo "Exit code: $?"

# Rodar com --force → deve sobrescrever
node /caminho/para/cli/dist/index.js init --yes --force --name my-test-project
echo "Exit code: $?"
```

### 4.5 Limpar

```bash
rm -rf /tmp/test-loopzero-init
```

---

## 5. Testando o TenantController no Cluster

### 5.1 Aplicar um Tenant CR manualmente

```bash
cat <<'EOF' | kubectl --context k3d-loopzero-mgmt apply -f -
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: acme
  namespace: loopzero-system
spec:
  plan: starter
  ownerRef: github-lucas
EOF
```

### 5.2 Verificar status do Tenant

```bash
kubectl --context k3d-loopzero-mgmt get tenant acme -n loopzero-system -o yaml
```

**O que verificar:**
- `.status.phase` deve transicionar para `Provisioning` e depois `Active`
- `.status.conditions` mostra progresso das etapas (GiteaReady, NamespaceReady, etc)
- `.status.observedGeneration` deve igualar `.metadata.generation`

> **Nota:** O controller precisa estar rodando no cluster para que a reconciliacao aconteca.
> Se nao estiver deployado ainda, os testes unitarios com envtest cobrem todo esse comportamento.

### 5.3 Verificar recursos provisionados

```bash
# Namespace do tenant no workload cluster
kubectl --context k3d-loopzero-workload get ns | grep acme

# ResourceQuota e LimitRange
kubectl --context k3d-loopzero-workload get resourcequota -n tenant-acme
kubectl --context k3d-loopzero-workload get limitrange -n tenant-acme

# NetworkPolicy
kubectl --context k3d-loopzero-workload get networkpolicy -n tenant-acme

# Repositorio Gitea
curl -s http://localhost:3000/api/v1/repos/search?q=acme | jq .
```

### 5.4 Testar spec-change (mudanca de plano)

```bash
# Mudar o plano do tenant
kubectl --context k3d-loopzero-mgmt patch tenant acme -n loopzero-system --type merge -p '{"spec":{"plan":"growth"}}'

# Verificar que phase voltou para Provisioning
kubectl --context k3d-loopzero-mgmt get tenant acme -n loopzero-system -o jsonpath='{.status.phase}'
```

**Esperado:** phase volta para `Provisioning` e eventualmente retorna para `Active`.

### 5.5 Testar deletion

```bash
kubectl --context k3d-loopzero-mgmt delete tenant acme -n loopzero-system

# Verificar que recursos foram limpos
kubectl --context k3d-loopzero-workload get ns | grep acme  # Nao deve existir
```

---

## 6. Testes de Integracao

```bash
task test:integration
```

Roda os testes de integracao que dependem de clusters/envtest rodando.

---

## 7. Checklist Final

| # | Verificacao | Status |
|---|------------|--------|
| 1 | `task test:unit` passa sem falhas | [ ] |
| 2 | Health endpoint retorna 200 | [ ] |
| 3 | Deep health retorna 503 (DB nao conectado) ou 200 (com DB) | [ ] |
| 4 | Correlation-ID gerado e preservado | [ ] |
| 5 | CLI loopzero init gera loopzero.yaml | [ ] |
| 6 | CLI loopzero init --force sobrescreve | [ ] |
| 7 | CLI loopzero init sem auth retorna exit 4 | [ ] |
| 8 | .gitignore atualizado com .env.local | [ ] |
| 9 | loopzero.yaml nao contem termos internos | [ ] |
| 10 | Tenant CR aplicado reconcilia corretamente | [ ] |
| 11 | Spec-change transiciona para Provisioning | [ ] |
| 12 | Tenant deletion limpa recursos | [ ] |
| 13 | Nenhum termo proibido em saidas do CLI | [ ] |
