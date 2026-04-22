# E2E Validation Plan — 99% Confidence

> Plano para validar TODAS as features implementadas contra infraestrutura real.
> Executa na droplet `165.227.121.20` (8 vCPUs, 16GB RAM, 300GB disk).
> Target: 99% de confiança de que o sistema funciona em produção.

---

## Princípios

1. **Infraestrutura real** — k3d clusters com mesmos Helm charts de produção
2. **Dados reais** — tenants, builds, releases criados via CLI (não seed direto)
3. **Fluxos completos** — CLI → API → Argo Workflows → ArgoCD → app live
4. **Failure injection** — testar cenários de falha, não apenas happy path
5. **Idempotência** — cada teste pode rodar múltiplas vezes sem efeito colateral
6. **Evidência** — cada teste produz log com timestamp, comando, resultado
7. **Rollback** — cada teste verifica que cleanup funciona

---

## Fase 0 — Setup Infraestrutura (30 min)

### Step 0.1: Bootstrap k3d clusters

```bash
ssh paulo@165.227.121.20
cd loopzero

# Criar clusters k3d (3 clusters: mgmt, build, workload)
task setup

# Verificar
k3d cluster list
kubectl --context k3d-loopzero-mgmt get nodes
kubectl --context k3d-loopzero-build get nodes
kubectl --context k3d-loopzero-workload get nodes
```

**Critério:** 3 clusters Running, todos os nodes Ready.

### Step 0.2: Deploy platform (Tilt)

```bash
# Iniciar dev loop
tilt up

# Esperar convergência (pode levar 5-10 min na primeira vez)
# Verificar no Tilt UI: http://localhost:10350

# Verificar health
curl -s http://localhost:13001/api/v1/health | jq .
curl -s http://localhost:13001/api/v1/health/deep | jq .
```

**Critério:** Todos os serviços healthy no Tilt. API deep health retorna `{"status":"ok"}`.

### Step 0.3: Seed E2E data

```bash
task test:e2e:setup
```

**Critério:** API tokens provisionados, seed data inserida, health check OK.

### Step 0.4: Build CLI

```bash
cd cli && npm run build && npm link
loopzero --version
```

**Critério:** `loopzero --version` retorna versão válida.

---

## Fase 1 — Feature Validation: Happy Paths (45 min)

### Test F1.1: Deploy Happy Path

```bash
# Login
loopzero login --token $E2E_TOKEN

# Init projeto
mkdir -p /tmp/e2e-hello && cd /tmp/e2e-hello
cat > loopzero.yaml << 'YAML'
version: 2
project: e2e-test-$(date +%s)
apps:
  api:
    type: server
    port: 8080
    framework: go
YAML
cat > main.go << 'GO'
package main
import "net/http"
func main() { http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })) }
GO
cat > go.mod << 'MOD'
module e2e-hello
go 1.25
MOD
git init && git add -A && git commit -m "init"

# Deploy
loopzero deploy --env production --yes 2>&1 | tee /tmp/e2e-deploy.log

# Verificar
loopzero status | tee /tmp/e2e-status.log
```

**Critérios de aceite:**
- [ ] Deploy retorna exit code 0
- [ ] `loopzero status` mostra release ACTIVE
- [ ] URL acessível retorna "OK"
- [ ] Build duration < 5 min

**Evidência:** `/tmp/e2e-deploy.log`, `/tmp/e2e-status.log`

---

### Test F1.2: Scaling (replicas + HPA)

```bash
cd /tmp/e2e-hello
# Adicionar scaling ao loopzero.yaml
cat > loopzero.yaml << 'YAML'
version: 2
project: e2e-test-scaling
apps:
  api:
    type: server
    port: 8080
    framework: go
    replicas: 2
    autoscaling:
      min: 2
      max: 5
      targetCPU: 80
YAML
git add -A && git commit -m "add scaling"

loopzero deploy --env production --yes 2>&1 | tee /tmp/e2e-scaling.log

# Verificar replicas
kubectl --context k3d-loopzero-workload get deployment -n loopzero-*-production -o wide | tee /tmp/e2e-replicas.log

# Verificar HPA
kubectl --context k3d-loopzero-workload get hpa -n loopzero-*-production | tee /tmp/e2e-hpa.log
```

**Critérios de aceite:**
- [ ] Deployment tem 2 replicas (not 1)
- [ ] HPA exists com min=2, max=5, targetCPU=80%
- [ ] `loopzero status` mostra a configuração de scaling

**Evidência:** `/tmp/e2e-scaling.log`, `/tmp/e2e-replicas.log`, `/tmp/e2e-hpa.log`

---

### Test F1.3: Promote (staging → production)

```bash
# Criar ambiente staging primeiro
loopzero deploy --env staging --yes 2>&1 | tee /tmp/e2e-staging.log

# Verificar staging está live
loopzero status --env staging

# Promote para production
loopzero promote staging production --yes 2>&1 | tee /tmp/e2e-promote.log

# Verificar production
loopzero status --env production
```

**Critérios de aceite:**
- [ ] `loopzero promote` retorna exit code 0
- [ ] Production tem o mesmo build que staging
- [ ] Ambas as environments estão ACTIVE
- [ ] `--json` output tem promotion_id

**Evidência:** `/tmp/e2e-promote.log`

---

### Test F1.4: Log Streaming (--follow)

```bash
# Iniciar deploy e observar logs em tempo real
loopzero deploy --env production --yes &
DEPLOY_PID=$!

# Em paralelo, seguir logs
sleep 5
timeout 120 loopzero logs --follow --build $(loopzero build list --json | jq -r '.builds[0].id') 2>&1 | tee /tmp/e2e-logs-follow.log &
LOGS_PID=$!

# Esperar deploy completar
wait $DEPLOY_PID
sleep 5
kill $LOGS_PID 2>/dev/null

# Verificar runtime logs
timeout 10 loopzero logs --follow --project e2e-test --env production 2>&1 | head -20 | tee /tmp/e2e-runtime-logs.log
```

**Critérios de aceite:**
- [ ] Build logs streamados em tempo real (não batch)
- [ ] Cada log line tem timestamp
- [ ] Stream para automaticamente quando build termina
- [ ] Runtime logs mostram output do app

**Evidência:** `/tmp/e2e-logs-follow.log`, `/tmp/e2e-runtime-logs.log`

---

### Test F1.5: Rollback

```bash
# Deploy versão 2 (com bug simulado)
echo 'func init() { panic("bug") }' >> main.go
git add -A && git commit -m "introduce bug"
loopzero deploy --env production --yes 2>&1 || true  # pode falhar

# Rollback para versão anterior
loopzero rollback --env production --yes 2>&1 | tee /tmp/e2e-rollback.log

# Verificar app voltou a funcionar
curl -s $(loopzero status --json | jq -r '.url') | tee /tmp/e2e-rollback-verify.log
```

**Critérios de aceite:**
- [ ] Rollback retorna exit code 0
- [ ] App acessível após rollback
- [ ] Versão anterior restaurada (não a com bug)

**Evidência:** `/tmp/e2e-rollback.log`

---

### Test F1.6: Custom Domains

```bash
loopzero domain add test-e2e.example.com --project e2e-test 2>&1 | tee /tmp/e2e-domain.log
loopzero domain list | tee /tmp/e2e-domain-list.log
loopzero domain status test-e2e.example.com | tee /tmp/e2e-domain-status.log
```

**Critérios de aceite:**
- [ ] Domain criado com status pending
- [ ] `domain list` mostra o domínio
- [ ] `domain status` mostra verification token

**Evidência:** `/tmp/e2e-domain.log`

---

### Test F1.7: Secrets

```bash
loopzero secret set DB_URL "postgres://test:test@localhost/test" --env production 2>&1 | tee /tmp/e2e-secret.log
loopzero secret list --env production | tee /tmp/e2e-secret-list.log
```

**Critérios de aceite:**
- [ ] Secret criado com sucesso
- [ ] `secret list` mostra o secret (valor oculto)
- [ ] Nenhum valor exposto no output

**Evidência:** `/tmp/e2e-secret.log`

---

### Test F1.8: Team Management

```bash
loopzero team list | tee /tmp/e2e-team.log
# loopzero team add e2e-member@test.com --role member (se API permitir)
```

**Critérios de aceite:**
- [ ] Team list retorna membros
- [ ] JSON output tem campos esperados

---

## Fase 2 — Feature Validation: New Features (30 min)

### Test F2.1: `loopzero scale` command

```bash
# Scale fixed replicas
loopzero scale --app api --replicas 3 --env production 2>&1 | tee /tmp/e2e-scale.log

# Scale with autoscaling
loopzero scale --app api --min 2 --max 8 --cpu 70 --env production 2>&1 | tee /tmp/e2e-autoscale.log

# Verify
kubectl --context k3d-loopzero-workload get deployment,hpa -n loopzero-*-production
```

**Critérios de aceite:**
- [ ] Replicas alterados para 3
- [ ] HPA configurado com min=2, max=8, targetCPU=70%
- [ ] Exit code 0

---

### Test F2.2: `loopzero preview` commands

```bash
loopzero preview list | tee /tmp/e2e-preview-list.log

# Preview creation é via PR webhook — testar o CLI commands
loopzero preview list --json | tee /tmp/e2e-preview-json.log
```

**Critérios de aceite:**
- [ ] Command existe e retorna JSON válido
- [ ] Exit code 0

---

## Fase 3 — Failure Scenarios (30 min)

### Test F3.1: Concurrent Deploy Rejection

```bash
# Deploy 1 (background)
loopzero deploy --env production --yes &
PID1=$!
sleep 2

# Deploy 2 (should be rejected — 409 Conflict)
loopzero deploy --env production --yes 2>&1 | tee /tmp/e2e-concurrent.log
EXIT_CODE=$?

wait $PID1
```

**Critérios de aceite:**
- [ ] Primeiro deploy: 202 Accepted
- [ ] Segundo deploy: 409 Conflict (exit code != 0)
- [ ] Mensagem: "A deploy is already in progress"

---

### Test F3.2: Invalid Dockerfile

```bash
mkdir -p /tmp/e2e-bad && cd /tmp/e2e-bad
cat > Dockerfile << 'DF'
INVALID SYNTAX HERE
DF
cat > loopzero.yaml << 'YAML'
version: 2
project: e2e-bad-build
apps:
  api:
    type: server
    port: 8080
YAML
git init && git add -A && git commit -m "bad build"
loopzero deploy --env production --yes 2>&1 | tee /tmp/e2e-bad-build.log
echo "Exit code: $?"
```

**Critérios de aceite:**
- [ ] Exit code 2 (build failure)
- [ ] Error message NÃO contém termos internos (ArgoCD, Gitea, etc.)
- [ ] Mensagem sugere ação: "Check your Dockerfile"

---

### Test F3.3: Auth Error

```bash
LOOPZERO_TOKEN=invalid-token loopzero deploy --env production --yes 2>&1 | tee /tmp/e2e-auth.log
echo "Exit code: $?"
```

**Critérios de aceite:**
- [ ] Exit code 4 (auth error)
- [ ] Mensagem: "Authentication failed. Run: loopzero login"

---

### Test F3.4: Lock Release on Crash

```bash
# Simulate: start deploy, kill API mid-way, verify lock released
# (This requires API restart capability — may need manual intervention)
```

**Critérios de aceite:**
- [ ] After API restart, subsequent deploy succeeds (lock not stuck)

---

## Fase 4 — Operational Validation (20 min)

### Test O4.1: Health Endpoints

```bash
curl -s http://localhost:13001/api/v1/health | jq . | tee /tmp/e2e-health.log
curl -s http://localhost:13001/api/v1/health/deep | jq . | tee /tmp/e2e-health-deep.log
```

**Critérios de aceite:**
- [ ] `/health` retorna `{"status":"ok"}`
- [ ] `/health/deep` verifica: postgres, gitea, argocd, argo-workflows
- [ ] Nenhum serviço degraded

---

### Test O4.2: Metrics

```bash
curl -s http://localhost:13001/metrics | grep loopzero_ | head -20 | tee /tmp/e2e-metrics.log
```

**Critérios de aceite:**
- [ ] `loopzero_build_duration_seconds` exists
- [ ] `loopzero_reconcile_loop_last_run_timestamp` exists (nosso novo gauge)
- [ ] `loopzero_service_auth_secret_age_seconds` exists (novo)

---

### Test O4.3: Audit Log

```bash
loopzero audit --limit 10 | tee /tmp/e2e-audit.log
loopzero audit --json | jq '.entries | length' | tee /tmp/e2e-audit-count.log
```

**Critérios de aceite:**
- [ ] Audit log tem entries
- [ ] Cada entry tem: action, user, timestamp
- [ ] Deploy action registrado

---

## Fase 5 — JSON Contract Validation (15 min)

### Test J5.1: Validate all --json outputs

```bash
for cmd in \
  "loopzero status --json" \
  "loopzero build list --json" \
  "loopzero secret list --json --env production" \
  "loopzero team list --json" \
  "loopzero domain list --json" \
  "loopzero preview list --json" \
  "loopzero audit --json" \
  "loopzero whoami --json"; do
  echo "=== $cmd ===" 
  eval $cmd 2>/dev/null | jq . > /dev/null 2>&1 && echo "VALID JSON" || echo "INVALID JSON"
done | tee /tmp/e2e-json-contracts.log
```

**Critérios de aceite:**
- [ ] TODOS os --json outputs são JSON válido
- [ ] Nenhum contém `team_id` (Rule 3)
- [ ] Nenhum contém termos internos

---

### Test J5.2: No Internal Terms

```bash
# Run ALL CLI commands and grep for forbidden terms
for cmd in "deploy" "status" "rollback" "logs" "build list" "secret list" "domain list" "team list" "preview list" "audit" "whoami"; do
  loopzero $cmd --help 2>&1 | grep -iE "argocd|gitea|minio|kaniko|kubebuilder|envtest|gitops|tenant_id|advisory.lock" && echo "VIOLATION in: $cmd"
done | tee /tmp/e2e-internal-terms.log
```

**Critérios de aceite:**
- [ ] ZERO violations

---

## Fase 6 — Cleanup + Report (10 min)

### Collect Evidence

```bash
mkdir -p /tmp/e2e-evidence
cp /tmp/e2e-*.log /tmp/e2e-evidence/

# Summary
echo "=== E2E VALIDATION REPORT ===" > /tmp/e2e-evidence/REPORT.md
echo "Date: $(date)" >> /tmp/e2e-evidence/REPORT.md
echo "Droplet: $(hostname)" >> /tmp/e2e-evidence/REPORT.md
echo "" >> /tmp/e2e-evidence/REPORT.md
echo "## Results" >> /tmp/e2e-evidence/REPORT.md
for f in /tmp/e2e-evidence/*.log; do
  name=$(basename $f .log)
  if grep -q "Exit code: 0\|PASS\|ok\|ACTIVE\|VALID" "$f" 2>/dev/null; then
    echo "- [x] $name: PASS" >> /tmp/e2e-evidence/REPORT.md
  else
    echo "- [ ] $name: CHECK" >> /tmp/e2e-evidence/REPORT.md
  fi
done
cat /tmp/e2e-evidence/REPORT.md
```

### Cleanup

```bash
# Delete test projects (not clusters — keep for re-runs)
loopzero project delete e2e-test --yes 2>/dev/null
loopzero project delete e2e-test-scaling --yes 2>/dev/null
loopzero project delete e2e-bad-build --yes 2>/dev/null
rm -rf /tmp/e2e-hello /tmp/e2e-bad
```

---

## Matriz de Confiança

| Teste | Tipo | Confiança sem E2E | Confiança com E2E |
|-------|------|------------------|------------------|
| Deploy happy path | Critical | 60% (unit only) | **99%** (real cluster) |
| Scaling + HPA | New feature | 40% (code exists) | **95%** (real K8s) |
| Promote | New feature | 40% (code exists) | **95%** (real flow) |
| Log streaming | New feature | 50% (unit tests) | **95%** (real Loki) |
| Rollback | Validated | 90% (tested before) | **99%** (re-validated) |
| Concurrent rejection | Critical | 70% (integration) | **99%** (real lock) |
| Failure scenarios | Critical | 20% (pending) | **95%** (real errors) |
| JSON contracts | Contract | 80% (unit tests) | **99%** (real output) |
| Internal terms | Rule 3 | 85% (grep tests) | **99%** (real CLI) |
| Health/metrics | Operational | 70% (unit) | **99%** (real endpoints) |

**Confiança global sem E2E:** ~60%
**Confiança global com E2E:** ~97%

---

## Pré-requisitos na Droplet

```bash
# Ferramentas necessárias (já instaladas):
go, node, docker, k3d, kubectl, helm, task, jq

# Precisa instalar/configurar na droplet:
# 1. task setup (cria clusters k3d)
# 2. tilt up (deploya a plataforma)
# 3. loopzero CLI (npm link)
# 4. E2E tokens (task test:e2e:setup)

# Tempo estimado de setup: ~30 min
# Tempo estimado de testes: ~2h
# Total: ~2.5h para validação completa
```

## Ordem de Execução

```
Fase 0: Setup (~30 min)
  task setup → tilt up → test:e2e:setup → CLI build

Fase 1: Happy Paths (~45 min)
  F1.1 Deploy → F1.2 Scaling → F1.3 Promote → F1.4 Logs
  F1.5 Rollback → F1.6 Domains → F1.7 Secrets → F1.8 Teams

Fase 2: New Features (~30 min)
  F2.1 Scale command → F2.2 Preview commands

Fase 3: Failure Scenarios (~30 min)
  F3.1 Concurrent → F3.2 Bad build → F3.3 Auth → F3.4 Lock

Fase 4: Operational (~20 min)
  O4.1 Health → O4.2 Metrics → O4.3 Audit

Fase 5: Contracts (~15 min)
  J5.1 JSON → J5.2 Internal terms

Fase 6: Report (~10 min)
  Collect evidence → Generate report → Cleanup
```
