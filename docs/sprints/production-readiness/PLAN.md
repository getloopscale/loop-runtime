# Production Readiness Plan

> Plano executável para levar a plataforma LoopZero de dev validado → produção.
> Pré-requisito: destroy+recreate validado via pipeline (MEET-015-IaC, 2026-03-29).

**Objetivo:** Resolver todos os bloqueadores para deploy em produção.
**Escopo excluso:** Backup de DB (DO managed PG tem backups automáticos, restore validation é operacional).

---

## Fase 1 — Passwords Seguros (CRÍTICO, ~2h)

### Problema
Gitea e MinIO usam `loopzero-dev-password` hardcoded nos Helm values (`infra/helm/gitea/values.yaml`, `infra/helm/minio/values.yaml`). Qualquer pessoa com acesso ao repo pode logar no Gitea UI e MinIO console em produção.

### Tarefas

#### 1.1 Mover Gitea admin password para Pulumi secret
- Adicionar `giteaAdminPassword` em `PlatformStackConfig` (optional, fallback para dev default)
- `pulumi config set --secret giteaAdminPassword <password>` no stack prod
- Pulumi `installGitea()` passa o password via `--set admin.password` no Helm values
- Valor default `loopzero-dev-password` apenas quando `config.environment !== "prod"`

**Arquivos:** `infra/pulumi/src/config/stack.ts`, `infra/pulumi/src/components/bootstrap.ts`

#### 1.2 Mover MinIO root password para Pulumi secret
- Adicionar `minioRootPassword` em `PlatformStackConfig`
- Mesmo padrão: Pulumi override no Helm values para prod
- Bootstrap Job e API config usam o password do Pulumi secret

**Arquivos:** `infra/pulumi/src/config/stack.ts`, `infra/pulumi/src/components/bootstrap.ts`

#### 1.3 Validar que bootstrap step usa passwords do Pulumi
- O `gitea-bootstrap` Job usa `GITEA_PASS` hardcoded — mudar para ler do K8s Secret
- Pipeline bootstrap step usa `gitea_REDACTED_SECRET_042` — mudar para ler do Helm secret

**Arquivos:** `infra/pulumi/src/components/bootstrap.ts`, `.github/workflows/infra-deploy.yml`

#### 1.4 Criar `Pulumi.prod.yaml` com secrets placeholder
- Documentar quais secrets precisam ser setados com `pulumi config set --secret`
- Incluir: `giteaAdminPassword`, `minioRootPassword`, `cfApiToken`, `digitaloceanToken`, `spacesAccessKeyId`, `spacesSecretAccessKey`

**Arquivos:** `infra/pulumi/Pulumi.prod.yaml`

### Critério de aceite
- [ ] `grep -r "loopzero-dev-password" infra/pulumi/` retorna 0 matches para stack prod
- [ ] Gitea login com password antigo falha em prod
- [ ] Pipeline bootstrap usa password do K8s Secret, não hardcoded

---

## Fase 2 — Pulumi.prod.yaml (ALTO, ~1h)

### Problema
Não existe configuração de stack para produção. Dev usa nodes `small` (2 vCPU, 2GB), produção precisa de sizing adequado.

### Tarefas

#### 2.1 Criar `Pulumi.prod.yaml`
Configurações diferenciadas para produção:

| Config | Dev | Prod |
|--------|-----|------|
| `environment` | dev | prod |
| `region` | sfo3 | nyc3 |
| `domain` | dev.loopzero.example | loopzero.example |
| `mgmtNodeClass` | small | medium |
| `mgmtMinNodes` | 2 | 3 |
| `runtimeNodeClass` | small | medium |
| `runtimeMinNodes` | 2 | 3 |
| `buildNodeClass` | small | medium |
| `buildMinNodes` | 2 | 2 |
| `dbNodeCount` | 2 | 3 (HA) |

**Arquivo:** `infra/pulumi/Pulumi.prod.yaml`

#### 2.2 Configurar GitHub Environment `infra-prod`
- Required reviewers: `paulohenriquevn`
- Deployment branch: `main` only
- Secrets: mesmos do `infra-dev` + passwords seguros

**Local:** GitHub Settings → Environments

#### 2.3 Validar `pulumi preview --stack prod`
- Rodar preview para garantir que o stack compila sem erros
- Não aplicar — apenas validar

### Critério de aceite
- [ ] `Pulumi.prod.yaml` existe com todas as configs
- [ ] `pulumi preview --stack prod` retorna sem erros
- [ ] GitHub Environment `infra-prod` tem required reviewers

---

## Fase 3 — Testar `loopzero rollback` (ALTO, ~1h)

### Problema
`loopzero rollback` nunca foi validado no ambiente DO pós-recreate. O fluxo depende de ArgoCD sync + git history, que pode ter comportamento diferente do local.

### Tarefas

#### 3.1 Deploy v1 de um app de teste
```bash
loopzero deploy  # v1 — "Hello v1"
```

#### 3.2 Deploy v2
```bash
# Modificar app
loopzero deploy  # v2 — "Hello v2"
```

#### 3.3 Executar rollback
```bash
loopzero rollback --json
```

#### 3.4 Prova de Vida pós-rollback
- Pod running com versão v1
- ArgoCD Synced + Healthy
- Health endpoint respondendo
- Git history no Gitea mostra revert commit

#### 3.5 Corrigir se necessário
Se o rollback falhar, investigar:
- ArgoCD app name (usa TenantID, não ProjectName — já corrigido em MEET-012)
- Git revert no Gitea (depende de repo credentials válidos)
- Release state machine transitions

### Critério de aceite
- [ ] `loopzero rollback` retorna exit 0
- [ ] Pod volta para versão anterior
- [ ] ArgoCD Synced + Healthy após rollback

---

## Fase 4 — Testar `loopzero secret` + ESO (MÉDIO, ~1h)

### Problema
External Secrets Operator está deployado no runtime cluster, mas a integração nunca foi testada no DO. O fluxo `loopzero secret set` → ESO sync → pod env var pode falhar se o ClusterSecretStore não está configurado.

### Tarefas

#### 4.1 Verificar ESO está healthy
```bash
kubectl get pods -n external-secrets
kubectl get clustersecretstores
```

#### 4.2 Criar secret via CLI
```bash
loopzero secret set MY_VAR=my_value --json
```

#### 4.3 Verificar sync
- Secret aparece no K8s namespace do app
- Pod tem env var `MY_VAR=my_value`
- Tempo de sync < 30s

#### 4.4 Corrigir se necessário
Se ESO não funcionar:
- Verificar ClusterSecretStore configuration
- Verificar RBAC para cross-cluster secret access
- Verificar se o operator tem as credentials do mgmt cluster

### Critério de aceite
- [ ] `loopzero secret set` retorna exit 0
- [ ] Secret propagado para o runtime cluster em < 30s
- [ ] Pod lê o secret como env var

---

## Fase 5 — SLOs e Alertas (MÉDIO, ~2h)

### Problema
Prometheus coleta métricas `loopzero_*` mas não há alerting rules configuradas. Em produção, precisamos ser notificados quando algo quebra.

### Tarefas

#### 5.1 Definir SLOs básicos

| SLO | Target | Métrica |
|-----|--------|---------|
| API availability | 99.5% | `loopzero_api_request_duration_seconds{status!~"5.."}` |
| Deploy success rate | 95% | `loopzero_builds_total{status="SUCCEEDED"} / loopzero_builds_total` |
| Deploy duration P95 | < 120s | `histogram_quantile(0.95, loopzero_api_request_duration_seconds{path="/api/v1/builds"})` |
| ArgoCD sync time | < 60s | ArgoCD metrics |

#### 5.2 Criar PrometheusRule para alertas críticos
- `LoopZeroAPIDown` — API não responde health check por > 1 min
- `LoopZeroDeployFailing` — 3+ deploys consecutivos falhando
- `LoopZeroBuildStuck` — Build em RUNNING por > 30 min
- `LoopZeroArgocdUnhealthy` — ArgoCD app não Healthy por > 5 min

**Arquivo:** `infra/helm/monitoring/templates/loopzero-alerts.yaml` (ou Pulumi inline)

#### 5.3 Configurar notification channel
- Webhook para Slack/Discord (ou email via Grafana)
- Alertmanager configuration no Prometheus Helm values

### Critério de aceite
- [ ] 4 alerting rules criadas
- [ ] `kubectl get prometheusrules` mostra as rules
- [ ] Teste: simular API down → alerta dispara

---

## Fase 6 — Wiring de `loopzero status` (BAIXO, ~30min)

### Problema
Handler de status existe, adapters criados, mas falta 1 linha no `server.go` para registrar a rota. Sprint H6 item pendente.

### Tarefas

#### 6.1 Wire status handler no server.go
```go
// Em server.go, adicionar:
r.Get("/api/v1/projects/{name}/status", statusHandler.HandleGetStatus)
```

#### 6.2 Teste E2E
```bash
curl https://api.dev.loopzero.example/api/v1/projects/my-app/status -H "Authorization: Bearer ..."
```

### Critério de aceite
- [ ] `GET /api/v1/projects/{name}/status` retorna 200
- [ ] Resposta inclui build status, release status, ArgoCD sync status

---

## Ordem de Execução

```
Fase 1 (Passwords)     ████████████████████ 2h   ← BLOCKER, fazer primeiro
Fase 2 (Pulumi.prod)   ██████████           1h   ← depende de Fase 1
Fase 3 (Rollback)      ██████████           1h   ← independente
Fase 4 (ESO/Secrets)   ██████████           1h   ← independente
Fase 5 (SLOs)          ████████████████████ 2h   ← independente
Fase 6 (Status)        █████                30m  ← independente
                                            ─────
                                            ~7.5h total
```

Fases 3, 4, 5 e 6 são independentes entre si e podem ser paralelizadas.

---

## Gate de Promoção para Produção

Antes de executar `gh workflow run "Infra: Deploy" -f stack=prod -f command=up`:

- [ ] **Fase 1:** Passwords seguros (nenhum `loopzero-dev-password` em prod)
- [ ] **Fase 2:** `Pulumi.prod.yaml` criado + GitHub Environment configurado
- [ ] **Fase 3:** `loopzero rollback` validado no dev
- [ ] **Fase 4:** `loopzero secret` + ESO validado no dev
- [ ] **Fase 5:** SLOs + alertas configurados
- [ ] **Fase 6:** `loopzero status` wired
- [ ] **Dogfood final:** Ciclo completo (login → init → deploy → status → rollback → secret → delete) no dev com zero achados S0/S1
- [ ] **PR develop → main** aprovado por reviewer
- [ ] **Pipeline preview** no stack prod sem erros

---

## Riscos e Mitigações

| Risco | Probabilidade | Mitigação |
|-------|--------------|-----------|
| Passwords vazam via git history | Média | Nunca commitou prod passwords — só via `pulumi config set --secret` |
| Rollback falha em prod | Baixa | Testado exaustivamente em dev antes |
| ESO não funciona cross-cluster | Média | Testar em dev; se falhar, usar K8s Secrets direto (sem ESO) |
| Node sizing insuficiente | Baixa | Começar com medium, escalar conforme métricas |
| DNS propagation delay | Baixa | Cloudflare TTL 1 (auto), propagação em segundos |
