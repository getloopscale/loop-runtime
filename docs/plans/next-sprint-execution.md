# Next Sprint — Execution Plan

**Criado:** 2026-04-19
**Objetivo:** Fix Alertmanager + ativar multi-cell + validar L3 100/100
**Duracao estimada:** 5-7 dias
**Prerequisitos:** DO quota desbloqueada :white_check_mark:, Phase A multi-cell done :white_check_mark:

---

## Sequencia

```
S1 (Alertmanager)  ━━━ SRE VETO GATE ━━━
       │
S2 (ClusterCredential CR via Pulumi)
       │
S3 (Flip DynamicRouter default)
       │
S4 (Capacity baselines) ──┐
                           ├──► S5 (L3 100/100)
S4b (2o runtime cluster) ─┘
```

---

## S1 — Fix Alertmanager /dev/null

**SRE VETO ativo.** 50+ alertas existem, zero chegam a humanos.

### S1-01: Criar Slack webhook para alertas

**Acao manual (Paulo):**
1. Criar canal `#loopzero-alerts` no Slack do time
2. Criar canal `#loopzero-critical` no Slack do time
3. Criar incoming webhook para cada canal
4. Guardar URLs dos webhooks

**Criterio de aceite:**
- [ ] 2 canais Slack existem
- [ ] 2 webhook URLs disponiveis

**DoD:** URLs copiadas, prontas para usar no Helm values

---

### S1-02: Configurar Alertmanager receivers

**Arquivo:** `infra/helm/monitoring/values.yaml` L152-160

**Antes:**
```yaml
receivers:
  - name: default
    webhook_configs:
      - url: 'http://localhost:9093/dev-null'
        send_resolved: true
  - name: critical
    webhook_configs:
      - url: 'http://localhost:9093/dev-null'
        send_resolved: true
```

**Depois:**
```yaml
receivers:
  - name: default
    slack_configs:
      - api_url_file: '/etc/alertmanager/secrets/slack-webhook-url'
        channel: '#loopzero-alerts'
        send_resolved: true
        title: '{{ .Status | toUpper }}: {{ .CommonLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
  - name: critical
    slack_configs:
      - api_url_file: '/etc/alertmanager/secrets/slack-critical-webhook-url'
        channel: '#loopzero-critical'
        send_resolved: true
        title: ':rotating_light: {{ .CommonLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

**Alternativa (se Slack nao for possivel):** Usar webhook generico com um endpoint que loga:
```yaml
receivers:
  - name: critical
    webhook_configs:
      - url: 'https://hooks.example.com/loopzero-alerts'
        send_resolved: true
```

**Criterios de aceite:**
- [ ] `localhost:9093/dev-null` nao aparece no values.yaml
- [ ] Receivers apontam para canal real (Slack/webhook)
- [ ] `send_resolved: true` em ambos receivers
- [ ] Secrets para webhook URLs via K8s Secret (nao hardcoded no values)

**DoD:** `grep -c "dev-null" infra/helm/monitoring/values.yaml` retorna 0

---

### S1-03: Criar Secret com webhook URLs no cluster

**Acao:**
```bash
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n monitoring \
  create secret generic alertmanager-slack-webhooks \
  --from-literal=slack-webhook-url="https://hooks.slack.com/..." \
  --from-literal=slack-critical-webhook-url="https://hooks.slack.com/..."
```

**Ou via Pulumi:** Adicionar Secret ao `mgmt-bootstrap.ts` com valores de `pulumi config set --secret`.

**Criterios de aceite:**
- [ ] Secret existe no namespace `monitoring`
- [ ] Alertmanager pod tem volume mount para o Secret

**DoD:** `kubectl get secret alertmanager-slack-webhooks -n monitoring` retorna sucesso

---

### S1-04: Deploy Alertmanager atualizado

**Acao:**
```bash
# Via Helm upgrade do monitoring stack
helm upgrade monitoring infra/helm/monitoring/ \
  --namespace monitoring \
  --values infra/helm/monitoring/values.yaml \
  --set alertmanager.secrets[0]=alertmanager-slack-webhooks
```

**Ou via Pulumi se monitoring e managed por Pulumi.**

**Criterios de aceite:**
- [ ] Alertmanager pod reinicia com nova config
- [ ] `kubectl logs -n monitoring alertmanager-0` sem erros de config

**DoD:** Pod running, config carregada sem erros

---

### S1-05: Validar alerta sintetico

**Acao:** Trigger alerta real:
```bash
# Opcao A: Deletar um ClusterCredential temporariamente (LoopZeroCellDiscoveryEmpty)
# Opcao B: Usar amtool para criar alerta sintetico
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n monitoring \
  exec -it alertmanager-0 -- amtool alert add \
  alertname="TestAlert" severity="critical" \
  --annotation.summary="Test alert - please ignore" \
  --end=$(date -u -d "+5 minutes" +%Y-%m-%dT%H:%M:%S.000Z)
```

**Criterios de aceite:**
- [ ] Alerta sintetico aparece no canal Slack `#loopzero-critical` em <2 min
- [ ] Alerta tem titulo, descricao, severity corretos
- [ ] Alerta resolve (send_resolved) apos expirar

**DoD:** Screenshot do alerta no Slack. SRE VETO RESOLVIDO.

---

### S1 Gate

- [ ] Zero `dev-null` no monitoring values
- [ ] 1 alerta sintetico recebido no Slack
- [ ] SRE VETO resolvido

---

## S2 — ClusterCredential CR via Pulumi

**Prerequisito:** S1 Gate passed (alertas funcionam — se algo quebrar, sabemos).

### S2-01: Verificar Secret de kubeconfig existente

**Acao:**
```bash
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  get secret tenant-controller-kubeconfig -o jsonpath='{.data}' | jq 'keys'
```

**Criterio de aceite:**
- [ ] Secret existe com key `runtime-kubeconfig`
- [ ] Kubeconfig aponta para o runtime cluster correto

**DoD:** Output confirma keys `build-kubeconfig` e `runtime-kubeconfig`

---

### S2-02: Adicionar ClusterCredential CR ao Pulumi

**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts` (apos L2792, depois do `controllerKubeconfig` Secret)

**Codigo a adicionar:**
```typescript
// MEET-053 B-01: Register runtime cluster as a Cell via ClusterCredential CRD.
// This enables DynamicRouter to discover the cell and PlacementEngine to route
// tenants. The controller will reconcile → status.connected=true → ArgoCD
// cluster secret created.
const runtimeClusterCredential = new k8s.apiextensions.CustomResource(
  "runtime-cluster-credential",
  {
    apiVersion: "platform.loopzero.io/v1",
    kind: "ClusterCredential",
    metadata: {
      name: `runtime-${config.region}-01`,
      namespace: PLATFORM_NAMESPACE,
      labels: {
        "loopzero.io/ingress-domain": config.baseDomain,
      },
    },
    spec: {
      clusterName: runtimeCluster.name,
      apiServer: runtimeCluster.endpoint,
      secretRef: {
        name: "tenant-controller-kubeconfig",
        namespace: PLATFORM_NAMESPACE,
      },
      scope: {
        argocd: true,
        operators: true,
      },
      tls: {
        insecure: false,
      },
      cellID: "cell-default",
    },
  },
  { provider, dependsOn: [controllerKubeconfig, crdInstall] },
);
```

**Criterios de aceite:**
- [ ] CR referencia Secret `tenant-controller-kubeconfig` (ja existe)
- [ ] `cellID: "cell-default"` (match com tenants existentes no PG)
- [ ] `dependsOn` inclui o Secret e o CRD install
- [ ] `apiServer` vem do output do cluster Pulumi (nao hardcoded)

**DoD:** `pulumi preview --stack dev` mostra o CR como `+create`

---

### S2-03: Pulumi up

**Acao:**
```bash
cd infra/pulumi
npx pulumi up --stack dev --yes
```

**Criterios de aceite:**
- [ ] CR criado sem erro
- [ ] Nenhum recurso existente destruido
- [ ] Preview mostra apenas o CR novo (nao drift em outros recursos)

**DoD:** `pulumi up` completa com 1 resource created, 0 destroyed

---

### S2-04: Validar controller reconcilia

**Acao:**
```bash
# Verificar CR status
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  get clustercredential -o wide

# Verificar conditions
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  describe clustercredential runtime-sfo3-01

# Verificar ArgoCD cluster secret criado
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n argocd \
  get secret -l argocd.argoproj.io/secret-type=cluster
```

**Criterios de aceite:**
- [ ] `Connected=True` no CR status
- [ ] Condition `SecretNotFound` ausente ou `False`
- [ ] ArgoCD cluster secret existe no namespace `argocd`
- [ ] `ObservedGeneration` == `Generation` (D-03 fix funciona)

**DoD:** CR Connected=True + ArgoCD secret existe

---

### S2-05: Validar DynamicRouter descobre cell

**Acao:**
```bash
# Habilitar DynamicRouter temporariamente
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  set env deployment/loopzero-api LOOPZERO_DYNAMIC_ROUTER=true

# Verificar logs
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  logs -l app=loopzero-api --tail=20 | grep "cell"

# Verificar metrica
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  port-forward svc/loopzero-api-metrics 19090:9090 &
curl -s http://localhost:19090/metrics | grep loopzero_cell_discovery_count
```

**Criterios de aceite:**
- [ ] Log: `"cell router: DynamicRouter active", "cells_discovered": 1`
- [ ] `loopzero_cell_discovery_count` = 1
- [ ] health/deep inclui `"cells": "1 discovered"`

**DoD:** DynamicRouter ativo com 1 cell. Reverter env var (Phase C fara o flip permanente).

```bash
# Reverter (sera feito permanente no S3)
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  set env deployment/loopzero-api LOOPZERO_DYNAMIC_ROUTER-
```

---

### S2 Gate

- [ ] ClusterCredential CR existe com `Connected=True`
- [ ] ArgoCD cluster secret criado automaticamente
- [ ] DynamicRouter descobre 1 cell (testado e revertido)
- [ ] Nenhum deploy existente quebrou (verificar `loopzero platform status` ou app URLs)

---

## S3 — Flip DynamicRouter Default

**Prerequisito:** S2 Gate passed.

### S3-01: Mudar default no config.go

**Arquivo:** `api/internal/config/config.go` L324

**Antes:**
```go
DynamicRouter: os.Getenv("LOOPZERO_DYNAMIC_ROUTER") == "true",
```

**Depois:**
```go
DynamicRouter: os.Getenv("LOOPZERO_DYNAMIC_ROUTER") != "false",
```

**Criterios de aceite:**
- [ ] Sem env var → `DynamicRouter = true` (novo default)
- [ ] `LOOPZERO_DYNAMIC_ROUTER=false` → desativa explicitamente
- [ ] `LOOPZERO_SINGLE_CELL=true` → forca StaticRouter (overrides, ja implementado em A-08)

**DoD:**
- Teste unitario: config sem env vars → `DynamicRouter == true`
- Teste unitario: `LOOPZERO_DYNAMIC_ROUTER=false` → `DynamicRouter == false`
- `go test ./api/internal/config/...` passa

---

### S3-02: Build + push nova imagem API

**Acao:**
```bash
# Build via CI ou manual
# Push para Harbor/DOCR
# Atualizar tag no deployment
```

**Criterios de aceite:**
- [ ] Imagem com o flip compilada e pushed
- [ ] Tag anotada no git

**DoD:** Imagem disponivel no registry

---

### S3-03: Deploy em producao

**Acao:**
```bash
# Atualizar deployment para usar nova imagem
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  set image deployment/loopzero-api loopzero-api=<registry>/<image>:<tag>

# Ou via Pulumi/Helm se o deployment e managed
```

**Criterios de aceite:**
- [ ] Pod reinicia sem crash
- [ ] Log: `"cell router: DynamicRouter active"`
- [ ] `/api/v1/health/deep` retorna 200 com `"cells": "1 discovered"`
- [ ] `loopzero_cell_discovery_count` = 1

**DoD:** API rodando com DynamicRouter como default

---

### S3-04: Validar deploy end-to-end

**Acao:**
```bash
# Deploy real via API (ou loopzero deploy se possivel)
# Verificar que build completa, release ativa, app live
```

**Criterios de aceite:**
- [ ] `loopzero deploy` (ou equivalente via API) completa com sucesso
- [ ] Build status: SUCCEEDED
- [ ] Release status: ACTIVE
- [ ] App URL retorna HTTP 200

**DoD:** 1 deploy cycle completo com DynamicRouter ativo

---

### S3-05: Validar rollback funciona

**Acao:** Testar `LOOPZERO_SINGLE_CELL=true` como escape hatch:
```bash
# Simular problema: setar single-cell mode
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  set env deployment/loopzero-api LOOPZERO_SINGLE_CELL=true

# Verificar: StaticRouter ativo
kubectl logs -l app=loopzero-api --tail=5 | grep "single-cell"

# Reverter
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  set env deployment/loopzero-api LOOPZERO_SINGLE_CELL-
```

**Criterios de aceite:**
- [ ] `LOOPZERO_SINGLE_CELL=true` → log warning, StaticRouter ativo
- [ ] Deploys continuam funcionando em single-cell mode
- [ ] Remover env var → DynamicRouter retoma

**DoD:** Rollback testado e revertido

---

### S3 Gate

- [ ] DynamicRouter e o default (sem env var)
- [ ] 1 deploy E2E com sucesso
- [ ] Rollback via `LOOPZERO_SINGLE_CELL=true` testado
- [ ] 24h sem incidents (monitorar alertas no Slack — agora funcionam!)

---

## S4 — Capacity Baselines + L3

**Prerequisito:** S3 Gate passed (multi-cell ativo, alertas funcionam).

### S4-01: Rodar capacity suite (baselines)

**Acao:**
```bash
# Port-forwards
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n loopzero-system \
  port-forward svc/loopzero-api-metrics 19090:9090 &
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n argocd \
  port-forward svc/argocd-metrics 18082:8082 &

# Rodar suite
LOOPZERO_CAPACITY=1 \
LOOPZERO_API_URL=https://api.dev.loopzero.example \
LOOPZERO_API_METRICS_URL=http://localhost:19090 \
LOOPZERO_ARGOCD_METRICS_URL=http://localhost:18082 \
LOOPZERO_SERVICE_AUTH=<token> \
LOOPZERO_APP_DOMAIN=dev.loopzero.example \
LOOPZERO_MGMT_CONTEXT=do-sfo3-loopzero-infra-dev-sfo3-mgmt \
LOOPZERO_RUNTIME_CONTEXT=do-sfo3-loopzero-infra-dev-sfo3-runtime \
LOOPZERO_GITEA_URL=http://localhost:13000 \
LOOPZERO_GITEA_USER=gitea_admin \
LOOPZERO_GITEA_PASS=<password> \
./infra/tests/capacity/run-capacity.sh all
```

**Criterios de aceite:**
- [ ] 5 testes executados sem erros fatais
- [ ] Resultados JSON em `/tmp/capacity-bench/`
- [ ] Valores reais vs projecoes documentados

**DoD:** 5 arquivos JSON com resultados reais. Atualizar `cell-budget.md` com valores medidos.

---

### S4-02: Analisar resultados e atualizar cell-budget

**Acao:** Para cada suposicao, comparar projecao vs medido:

| Suposicao | Projecao | Medido | Acao |
|-----------|----------|--------|------|
| Go memory peak | 200-300 Mi | ? Mi | Se >300 Mi → investigar |
| BuildKit /no | ~4 | ? | Se <3 → sizing inadequado |
| ArgoCD mem/app | ~500 kB | ? kB | Se >2 MB → budget errado |
| Gitea push p95 @100 repos | <2s | ?s | Se >2s → tuning needed |
| Pod memory avg | ~50 Mi | ? Mi | Se >100 Mi → budget update |

**Criterios de aceite:**
- [ ] 5 suposicoes com valores reais preenchidos
- [ ] `docs/architecture/cell-budget.md` atualizado com coluna "MEASURED"
- [ ] Decisao: re-run L3 com 1 cell ou provisionar 2o runtime cluster

**DoD:** Cell-budget atualizado com dados reais

---

### S4-03: Re-run L3 100/100

**Acao:**
```bash
LOOPZERO_REAL_DEPLOY=1 \
LOOPZERO_API_URL=https://api.dev.loopzero.example \
LOOPZERO_APP_DOMAIN=dev.loopzero.example \
LOOPZERO_SERVICE_AUTH=<token> \
LOOPZERO_LOAD_LEVEL=l3 \
./infra/tests/load/run-tests.sh l3
```

**Criterios de aceite:**
- [ ] >=95/100 tenants com app live (HTTP 200)
- [ ] Zero lock timeouts
- [ ] Zero circuit breaker trips
- [ ] Build queue nao excede 2x MaxConcurrent
- [ ] Alertas NO Slack se algo falhar (validacao do S1)

**DoD:** L3 >=95/100 PASSED. Se <95, documentar failures e criar fix tasks.

---

### S4 Gate

- [ ] Capacity baselines medidos e documentados
- [ ] L3 resultado >=95/100 OU gaps identificados com fix plan
- [ ] Cell-budget atualizado com MEASURED values

---

## Resumo Total

| Step | Tasks | Estimativa | Prerequisito |
|------|:-----:|:----------:|:------------:|
| S1 (Alertmanager) | 5 | 1 dia | — |
| S2 (ClusterCredential) | 5 | 1 dia | S1 |
| S3 (Flip DynamicRouter) | 5 | 1 dia + 24h validacao | S2 |
| S4 (Capacity + L3) | 3 | 2-3 dias | S3 |
| **Total** | **18** | **5-7 dias** | |
