# Plano de Produção — LoopZero Platform

**Data:** 2026-03-31
**Status:** Pré-produção (dogfood local + DigitalOcean validado)
**Objetivo:** Fechar todos os gaps restantes para deploy de produção

---

## Estado Atual

O sistema já foi validado em DigitalOcean real (2026-03-29):
- Deploy E2E: 24s build, pod Running, health OK ✅
- Delete com ArgoCD prune ✅
- Rollback: 3s, version verified ✅
- Logs via Loki ✅
- Secrets via ESO: `loopzero secret set KEY=val` → sync <30s ✅
- TLS via Let's Encrypt DNS-01 ✅
- Alertmanager: 4 SLO rules ✅
- Cloudflare Tunnel + Worker + KV ✅
- `loopzero status` funcional ✅
- 504 CLI tests + 60+ API test files ✅
- Hardening Sprints 1-6 completos ✅

---

## Gaps Remanescentes

Apenas **1 sprint de hardening** pendente: **Sprint 7 — Workload Security**.

---

## Fase 1 — Commit Fixes Pendentes (30min)

**O que:** 6 arquivos modificados na sessão de dogfood (111 linhas).

| Fix | Arquivo | Descrição |
|-----|---------|-----------|
| F-02 | `cli/src/commands/init.ts` | Warning em JSON quando nome truncado por comprimento |
| F-05 | `cli/src/index.ts` | Commander errors formatados como JSON com `--json` |
| Workflow Delete | `api/internal/argo/workflow_submitter.go` | Método `Delete()` para cleanup de workflows |
| Test cleanup | `api/internal/argo/workflow_submitter_test.go` | Tests deletam workflows após cancel (libera quota) |
| Tests | `cli/tests/unit/commands/init.test.ts` | 2 testes para F-02 |
| Tests | `cli/tests/unit/index.test.ts` | 1 teste para F-05 |

**DoD:**
- [ ] `git diff` mostra exatamente 6 arquivos, 111 insertions
- [ ] `cd cli && npm test` → 504 tests, 0 failures
- [ ] `go test ./api/internal/argo/...` → PASS (workflows deletados após teste)
- [ ] Commit criado no branch `dogfood`
- [ ] Nenhuma regressão em `go test ./api/... ./pkg/... ./operators/...`

---

## Fase 2 — Sprint 7: Workload Security Hardening (~6-8h)

**Referência:** `docs/hardening-sprints/07-workload-security/ROADMAP.md`
**Regra:** Uma task por vez, serializada. 3 deploys estáveis entre cada.

### Task 7.1 — SecurityContext nos pods de workload (CRÍTICO)

**Problema:** `api/internal/manifests/renderer.go` → `buildDeployment()` gera Deployments sem SecurityContext. Containers rodam como root.

**Ação:** Adicionar `securityContext` ao template de pod e container:
- Pod: `runAsNonRoot: true`, `runAsUser: 1000`, `fsGroup: 1000`, `seccompProfile: RuntimeDefault`
- Container: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities: drop: [ALL]`

**DoD:**
- [ ] Teste escrito: manifesto gerado contém `runAsNonRoot: true`
- [ ] Teste falha (RED)
- [ ] `renderer.go` atualizado com SecurityContext
- [ ] Teste passa (GREEN)
- [ ] Deploy real: `kubectl get pod -o yaml` confirma SecurityContext presente
- [ ] Pod inicia com sucesso (app Node.js roda como non-root)
- [ ] 3 deploys estáveis

---

### Task 7.2 — PodSecurity Admission labels (CRÍTICO)

**Problema:** Namespaces de tenant no workload cluster não têm PSA labels. Qualquer pod é aceito.

**Ação:** `operators/internal/controller/environment_controller.go` → `reconcileNamespace()` deve adicionar labels:
- `pod-security.kubernetes.io/enforce: restricted`
- `pod-security.kubernetes.io/warn: restricted`
- `pod-security.kubernetes.io/audit: restricted`

**DoD:**
- [ ] Teste envtest: namespace criado contém PSA labels
- [ ] Teste falha (RED)
- [ ] Controller atualizado
- [ ] Teste passa (GREEN)
- [ ] `kubectl get ns loopzero-acme-production -o yaml` confirma labels
- [ ] Pod de app é aceito pelo admission controller (SecurityContext da 7.1 é compatível)
- [ ] 3 deploys estáveis

---

### Task 7.3 — NetworkPolicy incondicional (ALTO)

**Problema:** `environment_controller.go` linha 103: `if env.Spec.NetworkPolicy.Isolate` — NetworkPolicy só é criada se flag está ativa. Default é sem isolamento.

**Ação:** Remover a condição. NetworkPolicy deve ser SEMPRE aplicada. Se `Isolate` for false, aplicar policy permissiva (egress allow-all + ingress do Traefik).

**DoD:**
- [ ] Teste envtest: NetworkPolicy criada mesmo com `Isolate: false`
- [ ] Teste falha (RED)
- [ ] Controller atualizado: NetworkPolicy sempre criada
- [ ] Teste passa (GREEN)
- [ ] `kubectl get networkpolicy -n loopzero-acme-production` retorna ao menos 1 policy
- [ ] 3 deploys estáveis

---

### Task 7.4 — ServiceAccount dedicado (ALTO)

**Problema:** Pods usam ServiceAccount `default` com `automountServiceAccountToken: true` implícito.

**Ação:**
1. `renderer.go`: adicionar `serviceAccountName: loopzero-app` e `automountServiceAccountToken: false`
2. `environment_controller.go`: criar ServiceAccount `loopzero-app` no namespace

**DoD:**
- [ ] Teste: manifesto contém `serviceAccountName: loopzero-app`
- [ ] Teste: envtest cria ServiceAccount no namespace
- [ ] Testes falham (RED)
- [ ] Implementação
- [ ] Testes passam (GREEN)
- [ ] `kubectl get sa -n loopzero-acme-production` mostra `loopzero-app`
- [ ] Pod usa `loopzero-app` SA
- [ ] 3 deploys estáveis

---

### Task 7.5 — PostgreSQL egress restrito (MÉDIO)

**Problema:** NetworkPolicy do PostgreSQL permite egress `- {}` (todo tráfego de saída).

**Ação:** `infra/helm/network-policies/loopzero-system.yaml` — restringir egress do PostgreSQL a:
- DNS (53/UDP+TCP)
- Replicação para outros pods PG (5432/TCP, label match)

**DoD:**
- [ ] `kubeconform --strict` passa
- [ ] PG pod consegue resolver DNS
- [ ] PG replication funciona (se HA ativo)
- [ ] PG não consegue fazer egress arbitrário (teste negativo)
- [ ] 3 deploys estáveis

---

### Task 7.6 — MinIO ingress com source selector (MÉDIO)

**Problema:** MinIO ingress aceita de qualquer fonte na porta 9000.

**Ação:** `infra/helm/network-policies/loopzero-system.yaml` — restringir ingress do MinIO a:
- `loopzero-api` (label: `app=loopzero-api`)
- `tenant-controller` (label: `app=tenant-controller`)

**DoD:**
- [ ] `kubeconform --strict` passa
- [ ] `loopzero deploy` funciona (API acessa MinIO)
- [ ] Tenant provisioning funciona (operator acessa MinIO)
- [ ] Pod arbitrário não consegue acessar MinIO (teste negativo)
- [ ] 3 deploys estáveis

---

### Task 7.7 — Image refs por digest (BAIXO)

**Problema:** Manifests usam tags (`image: registry/app:build-id`) em vez de digest (`image: registry/app@sha256:...`).

**Ação:** Após build, o Argo Workflow callback deve incluir o digest. `renderer.go` usa digest quando disponível.

**DoD:**
- [ ] Teste: manifesto com digest contém `@sha256:`
- [ ] Workflow callback inclui `image_digest` no payload
- [ ] `renderer.go` usa digest quando presente
- [ ] `kubectl get pod -o yaml` mostra image com digest
- [ ] 3 deploys estáveis

---

## Fase 3 — Validação Final em DigitalOcean (2-3h)

**Ação:** Re-executar o mesmo plano de validação de 2026-03-29, mas agora com security hardening:

**DoD:**
- [ ] `pulumi up --stack prod` sem erros
- [ ] Deploy E2E: exit 0, pod Running, health OK, SecurityContext presente
- [ ] Pod roda como non-root (`runAsUser: 1000`)
- [ ] PSA labels nos namespaces (`restricted`)
- [ ] NetworkPolicy ativa em todos os namespaces de tenant
- [ ] ServiceAccount `loopzero-app` em uso
- [ ] `loopzero rollback` funciona (<30s)
- [ ] `loopzero secret set KEY=val` → ESO sync
- [ ] `loopzero status --json` retorna dados corretos
- [ ] `loopzero logs --build <id>` retorna logs
- [ ] Alertmanager SLO rules disparam em cenário simulado
- [ ] Dogfood full: 0 achados S0/S1/S2

---

## Sequência de Execução

```
Fase 1: Commit fixes (30min)
  ↓
Fase 2: Sprint 7 (6-8h, serializado)
  7.1 SecurityContext  → 3 deploys → GATE
  7.2 PSA labels       → 3 deploys → GATE
  7.3 NetworkPolicy    → 3 deploys → GATE
  7.4 ServiceAccount   → 3 deploys → GATE
  7.5 PG egress        → 3 deploys → GATE
  7.6 MinIO ingress    → 3 deploys → GATE
  7.7 Image digest     → 3 deploys → GATE
  ↓
Fase 3: Validação DO (2-3h)
  ↓
PRODUÇÃO ✅
```

**Tempo total estimado: 9-12h de trabalho.**
