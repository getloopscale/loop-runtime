# GA Blockers Execution Plan

> Plano executável para resolver os 11 blockers identificados em MEET-2026-04-22-057.
> 4 feature blockers + 3 operational + 4 security = 11 total.
> Estimativa: 12 semanas. Organizadas em 4 waves por dependência e risco.

---

## Wave 1 — Quick Wins (Semana 1-2)

> Resolver blockers operacionais e de segurança que são mudanças de config/docs, não código novo.

### Task W1.1: OB-3 — Externalizar ArgoCD callback secret

**Blocker:** Secret hardcoded `cb-loopzero-dev-2026` em `infra/helm/argocd/values.yaml:174`
**Tier:** T1

**O que fazer:**
1. Criar K8s Secret `argocd-callback-secret` no namespace argocd via Pulumi
2. Alterar Helm values para referenciar o Secret via `valueFrom.secretKeyRef`
3. Atualizar `api/internal/config/config.go` para ler `ARGO_CALLBACK_SECRET` do env
4. Atualizar o handler `argocd_callback.go` para validar contra a config

**Critérios de aceite:**
- [ ] `grep -r "cb-loopzero-dev-2026" infra/` retorna 0 resultados
- [ ] Secret criado via Pulumi com valor aleatório (RandomPassword)
- [ ] Handler valida header contra o secret do env var
- [ ] `subtle.ConstantTimeCompare` usado na comparação

**DoD:**
- [ ] `go build ./api/...` compila
- [ ] `cd infra/pulumi && npx tsc --noEmit` compila
- [ ] Teste: `TestArgoCDCallback_InvalidSecret_Returns401`
- [ ] Teste: `TestArgoCDCallback_ValidSecret_Processes`

---

### Task W1.2: OB-1 — Testar DR (PG restore)

**Blocker:** PG restore nunca verificado. RTO desconhecido.
**Tier:** T0 (documentação + validação manual)

**O que fazer:**
1. Documentar procedimento de restore em `docs/runbooks/pg-disaster-recovery.md`
2. Executar restore de teste contra DO managed PG backup
3. Documentar RTO medido
4. Criar CronJob `pg-dump` para Spaces como belt-and-suspenders

**Critérios de aceite:**
- [ ] Runbook `docs/runbooks/pg-disaster-recovery.md` existe com comandos copy-paste
- [ ] Restore testado e RTO documentado (ex: "22 minutos para 47 tenants")
- [ ] CronJob de pg_dump para DO Spaces configurado (daily, 14-day retention)
- [ ] Teste de restore do pg_dump verificado

**DoD:**
- [ ] Runbook escrito com: pré-requisitos, steps, verificação, rollback
- [ ] Screenshot/log do restore bem-sucedido
- [ ] CronJob no Pulumi

---

### Task W1.3: OB-2 — Escrever 13 runbooks para alertas críticos

**Blocker:** 13 alertas críticos sem runbooks.
**Tier:** T0

**Criar em `docs/runbooks/alerts/`:**

| # | Alert | Runbook | Conteúdo mínimo |
|---|-------|---------|-----------------|
| 1 | LoopZeroPostgresDown | postgres-down.md | Connection string, verify pods, check DO dashboard, failover steps |
| 2 | LoopZeroPostgresBackupStale | postgres-backup-stale.md | Check CronJob, verify Spaces upload, manual pg_dump |
| 3 | LoopZeroGiteaDown | gitea-down.md | Check pods, Redis status, restore from backup |
| 4 | LoopZeroGiteaBackupStale | gitea-backup-stale.md | Check CronJob, verify S3 upload, manual dump |
| 5 | LoopZeroBuildClusterUnreachable | build-cluster-unreachable.md | Check kubeconfig, DOKS status, network |
| 6 | LoopZeroArgoCDCircuitOpen | argocd-circuit-open.md | Check ArgoCD pods, circuit breaker state, manual reset |
| 7 | LoopZeroTraefikDown | traefik-down.md | Check pods, LB status, DNS |
| 8 | LoopZeroDriftDetected | drift-detected.md | Identify drift, manual sync, investigate cause |
| 9 | LoopZeroGitMirrorExhausted | git-mirror-exhausted.md | Check Gitea connectivity, retry push, manual gitops |
| 10 | LoopZeroFallbackStuck | fallback-stuck.md | Identify stuck release, manual activate/rollback |
| 11 | LoopZeroBuildEgressStale | build-egress-stale.md | Check DNS, network policies, registry connectivity |
| 12 | LoopZeroBreakGlassAccess | break-glass-access.md | Identify who, why, audit trail, revoke |
| 13 | LoopZeroCellDiscoveryEmpty | cell-discovery-empty.md | (exists — verify completeness) |

**Critérios de aceite:**
- [ ] 13 runbooks existem em `docs/runbooks/alerts/`
- [ ] Cada runbook tem: Detecção, Triagem (copy-paste commands), Resolução, Escalação, Verificação
- [ ] Cada runbook referencia o alert name e a query Prometheus

**DoD:**
- [ ] `ls docs/runbooks/alerts/*.md | wc -l` ≥ 21 (8 existentes + 13 novos)

---

### Task W1.4: SB-4 — Playbook de incident response

**Blocker:** Sem procedimento documentado para incidentes de segurança.
**Tier:** T0

**Criar:** `docs/runbooks/security-incident-response.md`

**Seções obrigatórias:**
1. **Credential leak** — steps para revogar, rotacionar, notificar
2. **Data breach** — steps para containment, investigation, notification (GDPR)
3. **Tenant isolation breach** — steps para isolar, investigar, remediar
4. **Supply chain compromise** — steps para identificar, block, rebuild
5. **Unauthorized access** — steps para audit trail, lock account, investigate
6. **Severity classification** — P0/P1/P2/P3 com SLAs de resposta

**Critérios de aceite:**
- [ ] Playbook existe com 6 cenários
- [ ] Cada cenário tem: detection, containment, eradication, recovery, post-mortem
- [ ] Contato de emergência definido
- [ ] Procedimento testável (tabletop exercise)

**DoD:**
- [ ] `docs/runbooks/security-incident-response.md` existe

---

### Task W1.5: SB-1 — Habilitar MFA no Kratos

**Blocker:** Password-only auth para plataforma de produção.
**Tier:** T2

**O que fazer:**
1. Configurar TOTP no Kratos via Pulumi (cp-ory-kratos.ts)
2. Adicionar `totp` authenticator na identity schema
3. Configurar enforced MFA para Pro+ tiers (optional para Starter)

**Implementação Kratos TOTP:**
```yaml
selfservice:
  methods:
    totp:
      enabled: true
      config:
        issuer: "LoopZero Platform"
```

**Critérios de aceite:**
- [ ] TOTP habilitado no Kratos config
- [ ] Identity schema inclui TOTP credential
- [ ] Recovery codes habilitados
- [ ] Dashboard UI mostra setup MFA flow
- [ ] Login flow aceita TOTP como segundo fator

**DoD:**
- [ ] `cd infra/pulumi-control-plane && npx tsc --noEmit` compila
- [ ] MFA testável via dashboard (`cloud.{domain}/settings`)

---

### Task W1.6: OB-4+OB-5+OB-6+OB-7 — Quick operational fixes

**Blockers agrupados:** Todos são 1-2 horas cada.

| # | Fix | O que fazer |
|---|-----|-------------|
| OB-4 | External monitoring | Configurar UptimeRobot/Better Uptime para `api.{domain}/api/v1/health` |
| OB-5 | Grafana password | Gerar via Pulumi RandomPassword, não hardcoded |
| OB-6 | Loki S3 creds | Mover para Pulumi Secret, referenciar em Helm via secretKeyRef |
| OB-7 | CF Tunnel alert | Adicionar PrometheusRule para `cloudflared_tunnel_connections == 0` |

**Critérios de aceite:**
- [ ] External monitor ativo e alertando em canal independente
- [ ] `grep -r "loopzero-dev-password" infra/helm/monitoring/` retorna 0
- [ ] `grep -r "loopzero-dev-password" infra/helm/loki/` retorna 0
- [ ] Alert `LoopZeroCFTunnelDisconnected` existe em PrometheusRule

**DoD:**
- [ ] Todos os 4 itens verificáveis

---

## Wave 2 — Core Features (Semana 3-6)

> Implementar as 2 features menores (scaling + promote) que têm state machines/infra prontos.

### Task W2.1: FB-3 — Scaling (replicas + HPA em loopzero.yaml)

**Blocker:** Hardcoded `Replicas: 1` em renderer_deployment.go.
**Tier:** T2

**O que fazer — 5 sub-tasks:**

#### W2.1a: Adicionar campos scaling no loopzero.yaml schema
**Arquivo:** `cli/src/adapters/loopzero-yaml.ts`
```typescript
interface LoopZeroYamlApp {
  // ... existing fields
  replicas?: number;          // default: 1
  autoscaling?: {
    min: number;              // default: 1
    max: number;              // default: 3
    targetCPU?: number;       // default: 80 (percent)
  };
}
```

**Critérios de aceite:**
- [ ] `replicas` field parsed do loopzero.yaml
- [ ] `autoscaling.min/max/targetCPU` fields parsed
- [ ] Validation: replicas ≥ 1, max ≥ min, targetCPU 1-100
- [ ] `npm test` passa (adicionar testes para novos fields)

#### W2.1b: Propagar scaling para API
**Arquivos:** API routes (builds.go), services (build_orchestrator), db (build_store)

- Adicionar `Replicas` e `Autoscaling` nos AppSpec transmitidos com o build
- Persistir no apps_spec JSON do build

#### W2.1c: Gerar manifests com replicas + HPA
**Arquivo:** `api/internal/manifests/renderer_deployment.go`

- Usar `Replicas` do input (default 1)
- Se `Autoscaling` definido, gerar `HorizontalPodAutoscaler` manifest
- HPA target: `spec.metrics[].resource.target.averageUtilization: targetCPU`

**Critérios de aceite:**
- [ ] `Replicas` vem do loopzero.yaml, não hardcoded
- [ ] HPA manifest gerado quando `autoscaling` configurado
- [ ] PDB ajustado: `minAvailable: max(1, replicas-1)`

#### W2.1d: CLI `loopzero scale`
**Arquivo:** Criar `cli/src/commands/scale.ts`

```
loopzero scale --app api --replicas 3
loopzero scale --app api --autoscaling --min 2 --max 10 --cpu 80
```

**Critérios de aceite:**
- [ ] Comando existe com `--json` output
- [ ] Exit code 0 success, 1 usage error
- [ ] Testes unitários: happy path + validation errors

#### W2.1e: Testes
- [ ] Teste: `TestRenderDeployment_CustomReplicas` — 3 replicas no input → 3 no manifest
- [ ] Teste: `TestRenderDeployment_WithHPA` — autoscaling → HPA manifest gerado
- [ ] Teste: `TestRenderDeployment_DefaultReplicas` — sem campo → 1 replica
- [ ] Teste CLI: `test_scale_command` — happy path + validation

**DoD global W2.1:**
- [ ] `go build ./api/...` compila
- [ ] `go test ./api/internal/manifests/...` passa
- [ ] `cd cli && npm test` passa
- [ ] E2E: deploy com `replicas: 2` no loopzero.yaml → Deployment tem 2 pods

---

### Task W2.2: FB-4 — `loopzero promote`

**Blocker:** State machine existe (`pkg/statemachine/promotion.go`), zero implementação.
**Tier:** T2

**O que fazer — 5 sub-tasks:**

#### W2.2a: API endpoint `POST /api/v1/projects/{name}/promote`
**Criar:** `api/internal/routes/promote.go`

Request: `{ "from": "staging", "to": "production" }`
Response: `{ "promotion_id": "promo-xxx", "status": "validating" }`

Flow:
1. Validar que environment source tem release ACTIVE
2. Copiar manifests do release source para environment target via gitops
3. Trigger ArgoCD sync no target
4. Transicionar via state machine: Validating → Committing → Syncing → Completed

#### W2.2b: Serviço `PromotionOrchestrator`
**Criar:** `api/internal/services/promotion_orchestrator.go`

- Usa `withEnvironmentLock` no target environment
- Copia manifests via `gitops.Push` para o target path
- Cria release no target environment apontando para os mesmos manifests
- Emite SSE events: `promotion.started`, `promotion.completed`, `promotion.failed`

#### W2.2c: CLI command `loopzero promote`
**Criar:** `cli/src/commands/promote.ts`

```
loopzero promote staging production
loopzero promote --from staging --to production --project myapp
```

- Output: streaming via SSE (reutiliza deploy-renderer pattern)
- Exit codes: 0 success, 3 promote failure

#### W2.2d: Wire no router + testes
- Registrar rota em `router.go`
- Handler test: `TestPromote_HappyPath`, `TestPromote_NoSourceRelease`, `TestPromote_SameEnvironment`
- Service test: `TestPromotionOrchestrator_CopiesManifests`, `TestPromotionOrchestrator_FailsOnLock`
- CLI test: `test_promote_command`

**DoD global W2.2:**
- [ ] `loopzero promote staging production` funciona end-to-end
- [ ] State machine transitions: Validating → Committing → Syncing → Completed
- [ ] SSE events emitidos durante promote
- [ ] `--json` output estável
- [ ] 8+ testes passando

---

## Wave 3 — Log Streaming (Semana 5-7)

### Task W3.1: FB-1 — Log streaming (`loopzero logs --follow`)

**Blocker:** Logs são batch-only. Não há streaming em tempo real.
**Tier:** T2

**O que fazer — 4 sub-tasks:**

#### W3.1a: API endpoint para tail logs via SSE
**Criar:** `api/internal/routes/logs_stream.go`

`GET /api/v1/builds/{id}/logs/stream` — SSE endpoint
- Usa Loki tail API (`/loki/api/v1/tail`) via WebSocket
- Converte Loki WebSocket → SSE events para o cliente
- Suporta `Last-Event-ID` para reconexão
- Timeout: 30 minutos (configurable)

**Alternativa mais simples:** Polling-based SSE
- A cada 2s, query Loki com `start` = last timestamp
- Emite novos log entries como SSE events
- Menos complexo que WebSocket relay

#### W3.1b: Runtime logs (não apenas build)
**Estender:** Adicionar endpoint `GET /api/v1/projects/{name}/logs/stream`
- Query pods do workload cluster via RuntimeClient
- Agregar logs de todos os pods do app
- Filtrar por environment, app name

**Abordagem:** Loki já coleta logs do runtime cluster via Promtail. Query Loki com labels `{namespace="loopzero-{tenant}-{env}", container=~".+"}`.

#### W3.1c: CLI `--follow` flag
**Estender:** `cli/src/commands/logs.ts`
- Adicionar flag `--follow` / `-f`
- Conectar via SSE ao endpoint de streaming
- Exibir logs em tempo real com timestamp formatting
- Ctrl+C para parar (graceful disconnect)

#### W3.1d: Testes
- [ ] Teste API: `TestLogsStream_EmitsSSEEvents`
- [ ] Teste API: `TestLogsStream_ReconnectsWithLastEventID`
- [ ] Teste CLI: `test_logs_follow_flag`
- [ ] Teste CLI: `test_logs_runtime_project`

**DoD global W3.1:**
- [ ] `loopzero logs --follow --build <id>` mostra logs em tempo real
- [ ] `loopzero logs --follow --project myapp` mostra runtime logs
- [ ] SSE reconexão funciona (Last-Event-ID)
- [ ] `--json` output: cada line como JSON event
- [ ] 6+ testes passando

---

## Wave 4 — Preview Environments (Semana 7-10)

### Task W4.1: FB-2 — Preview Environments

**Blocker:** CRD + state machine existem. Zero implementação.
**Tier:** T3 (multi-componente: operator + API + CLI + webhook)

**O que fazer — 7 sub-tasks:**

#### W4.1a: PreviewEnvironment controller
**Criar:** `operators/internal/controller/preview_controller.go`

Reconcile flow:
1. Creating → criar namespace temporário no runtime (naming: `preview-{tenant}-pr{number}`)
2. Deploy a versão do PR branch
3. Criar ExposedRoute com hostname: `pr{number}-{project}.preview.{domain}`
4. Active → monitorar health, enforce TTL
5. Expiring → cleanup após merge/close/TTL
6. Deleting → deletar namespace, route, release
7. Deleted (terminal)

**Dependências:** RuntimeClient, GitProvisioner, manifests.Render

#### W4.1b: GitHub webhook PR handler
**Estender:** `api/internal/github/webhook/handler.go`

- PR opened/synchronize → criar/atualizar PreviewEnvironment CR
- PR closed/merged → marcar PreviewEnvironment para deletion
- Extrair branch name, PR number, commit SHA

#### W4.1c: API endpoints
**Criar:** `api/internal/routes/preview.go`

- `POST /api/v1/previews` — criar preview manualmente
- `GET /api/v1/previews` — listar previews do projeto
- `GET /api/v1/previews/{id}` — status do preview
- `DELETE /api/v1/previews/{id}` — deletar preview

#### W4.1d: CLI commands
**Criar:** `cli/src/commands/preview.ts`

```
loopzero preview list
loopzero preview inspect <id>
loopzero preview delete <id>
```

(Preview creation é automático via PR — não precisa de CLI create.)

#### W4.1e: TTL + cleanup
- Default TTL: 72h (configurable via Tenant.Spec.Preview.TTLHours)
- Max concurrent: configurable via Tenant.Spec.Preview.MaxEnvironments
- Cleanup: controller marca Expiring após TTL, then Deleting

#### W4.1f: GitHub commit status
- Set commit status `loopzero/preview` com URL do preview
- Status: pending → success (com URL) ou failure

#### W4.1g: Testes
- [ ] Controller envtest: 10+ specs (create, active, expire, delete, max limit)
- [ ] API handler tests: 6+ (create, list, get, delete, PR webhook)
- [ ] CLI tests: 4+ (list, inspect, delete, JSON output)
- [ ] E2E: PR aberto → preview URL criada → PR fechado → preview limpo

**DoD global W4.1:**
- [ ] PR aberto no GitHub → preview URL gerada automaticamente
- [ ] Preview URL acessível com app rodando
- [ ] PR fechado → preview cleanup automático (namespace deletado)
- [ ] `loopzero preview list` mostra previews ativos
- [ ] TTL enforcement funciona (72h default)
- [ ] Max concurrent previews respeitado
- [ ] Commit status no GitHub com URL do preview
- [ ] 20+ testes passando

---

## Wave S — Security (paralelo com Wave 2-4)

### Task WS.1: SB-2 — TTL + rotation para service auth secret

**Blocker:** God credential sem TTL.
**Tier:** T2

**O que fazer:**
1. Adicionar `LOOPZERO_SERVICE_AUTH_SECRET_TTL` config (default 90 dias)
2. Rotação via Pulumi: gerar novo secret a cada deploy, mover atual para PREVIOUS
3. Adicionar métrica `loopzero_service_auth_secret_age_seconds` (gauge)
4. Adicionar alert `LoopZeroServiceAuthSecretExpiring` quando age > 80 dias
5. Documentar procedimento de rotação em `docs/runbooks/service-auth-rotation.md`

**Critérios de aceite:**
- [ ] Métrica de idade do secret existe
- [ ] Alert fires 10 dias antes da expiração
- [ ] Runbook documenta rotação zero-downtime via dual-key
- [ ] Pulumi gera novo secret e mantém previous

**DoD:**
- [ ] `go build ./api/...` compila
- [ ] Runbook existe
- [ ] Alert no PrometheusRule

---

### Task WS.2: SB-3 — Deploy cosign policy-controller

**Blocker:** Cosign assina mas runtime não verifica.
**Tier:** T2

**O que fazer:**
1. Adicionar Helm chart `sigstore/policy-controller` ao infra/pulumi
2. Configurar ClusterImagePolicy para verificar assinaturas Fulcio (keyless)
3. Aplicar apenas a namespaces de workload (label selector: `loopzero.io/managed-by: environment-controller`)
4. Modo: Enforce (não audit)
5. Alterar VAP `vap-require-image-signature.yaml` para remover o audit-only

**Critérios de aceite:**
- [ ] policy-controller deployment rodando no cluster
- [ ] ClusterImagePolicy configurada para keyless Fulcio verification
- [ ] Pod com imagem não-assinada → rejected
- [ ] Pod com imagem assinada → admitted
- [ ] Plataforma (argocd, gitea, etc) não afetada (namespaces excluídos)

**DoD:**
- [ ] `cd infra/pulumi && npx tsc --noEmit` compila
- [ ] Teste manual: deploy imagem unsigned → blocked
- [ ] Teste manual: deploy imagem signed → success

---

## Ordem de Execução

```
WEEK 1-2: Wave 1 (operational + security quick wins)
  W1.1 (ArgoCD secret) ──┐
  W1.2 (DR test)     ────┤
  W1.3 (13 runbooks) ────┤── Tudo paralelo
  W1.4 (Incident playbook)┤
  W1.5 (MFA Kratos)  ────┤
  W1.6 (Quick fixes) ────┘

WEEK 3-6: Wave 2 (scaling + promote) + Wave S (security)
  W2.1a-e (Scaling) ──────┐
  W2.2a-d (Promote) ──────┤── Features em paralelo
  WS.1 (Service auth TTL) ┤── Security em paralelo
  WS.2 (Cosign verify)  ──┘

WEEK 5-7: Wave 3 (log streaming)
  W3.1a-d (Logs --follow) ── Pode começar na semana 5

WEEK 7-10: Wave 4 (preview environments)
  W4.1a-g (Previews) ──── O mais complexo, por último

WEEK 11-12: Integração + E2E + polish
  E2E tests para todos os features
  Load test L3 (100 tenants)
  Re-run /code-audit full (target: score B/80+)
```

**Dependências:**
- W2.1 (scaling) não depende de nada
- W2.2 (promote) não depende de nada
- W3.1 (logs) não depende de nada
- W4.1 (preview) pode reutilizar patterns de W2.1 (manifest rendering) e W3.1 (SSE)
- WS.1 e WS.2 são independentes de tudo

---

## Gate de Conclusão (GA Readiness)

```bash
# Feature blockers
loopzero logs --follow --build <id>    # FB-1: streaming funciona
loopzero scale --app api --replicas 3  # FB-3: scaling funciona  
loopzero promote staging production    # FB-4: promote funciona
# FB-2: abrir PR → preview URL → fechar PR → cleanup

# Operational blockers
cat docs/runbooks/pg-disaster-recovery.md  # OB-1: DR documentado
ls docs/runbooks/alerts/*.md | wc -l       # OB-2: ≥21 runbooks
grep "cb-loopzero-dev-2026" infra/             # OB-3: 0 resultados

# Security blockers
# SB-1: MFA habilitado no Kratos
# SB-2: Service auth TTL + alert
# SB-3: Cosign policy-controller deployed
cat docs/runbooks/security-incident-response.md  # SB-4: playbook existe

# Quality gate
/code-audit full  # Score ≥ B (80/100)
```

| Métrica | Antes | Target GA |
|---------|-------|-----------|
| Feature blockers | 4 | 0 |
| Operational blockers | 7 | 0 |
| Security blockers | 4 | 0 |
| Code audit score | C (73) | B (80+) |
| E2E failure scenarios | 7 pending | 0 pending |
| Runbooks | 8 | 21+ |
