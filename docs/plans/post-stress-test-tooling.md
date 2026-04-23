# Plan: Post-Stress-Test Tooling & Infra Recreate

**Data:** 2026-04-21
**Status:** APROVADO COM CONDIÇÕES (MEET-2026-04-21-055)
**Owner:** Infra Engineer
**Revisão:** Board completo (SRE, Architect, GitOps, DX, Security)

---

## Context

2 dias de stress test no DigitalOcean revelaram 17 problemas sistêmicos. Root cause: 62 operações manuais (kubectl/doctl) bypassing IaC → 576 drift changes → destroy total.

O plano original propunha 10 items em 3 tiers. O board reduziu para **4 items aprovados + 4 prerequisites mandatórios**, rejeitando 6 items.

**Estado atual DO:** Infra destruída. Apenas `loopzero-code-vm` (142.93.254.245). Pulumi state zerado. DOCR registry existe com imagens `:homolog`.

---

## Prerequisites Mandatórios (ANTES de qualquer tooling)

Identificados pelo board como mais críticos que qualquer ferramenta nova.

### M1: Fix Alertmanager /dev/null (TIER 0 BLOCKER)
**Agentes:** SRE (VETO), Security (MANDATORY)
**O que:** Alertmanager rota para `/dev/null`. Todas as ferramentas que dependem de alertas são teatro de segurança até isto ser corrigido.
**Ação:** Configurar receiver Slack real no Helm values. Validar com synthetic alert test.
**Arquivo:** `infra/helm/monitoring/values.yaml` (Alertmanager receivers)
**DoD:** Alerta sintético enviado → recebido no Slack
**Esforço:** 0.5 dia

### M2: Restringir RBAC Humano (ROOT CAUSE FIX)
**Agentes:** Security (CRITICAL), SRE (MISSING)
**O que:** 62 operações manuais foram possíveis porque o operador tinha cluster-admin. Operadores humanos devem ter kubeconfig read-only por default.
**Ação:** 
- Criar `loopzero-operator` ClusterRole: read-only + restart deployments + view logs + port-forward
- Remover cluster-admin do kubeconfig distribuído
- cluster-admin somente via break-glass (M3)
**Arquivos:**
- `infra/pulumi/src/components/mgmt-bootstrap.ts` (RBAC para human SA)
- Documentação em `docs/runbooks/`
**DoD:** `kubectl set image` com kubeconfig padrão → 403 Forbidden
**Esforço:** 1 dia

### M3: Implementar Break-Glass (PREREQUISITE para enforcement)
**Agentes:** Security (MANDATORY)
**O que:** Antes de qualquer política que bloqueie operações, deve existir um mecanismo de bypass seguro.
**Ação:**
- Script que cria ServiceAccount temporário com cluster-admin (TTL 4h)
- Loga issuance no PG audit table
- Emite métrica `loopzero_break_glass_sessions_active`
- K8s Job auto-deleta o SA após TTL
**Arquivos:**
- Criar: `infra/scripts/break-glass.sh`
- Editar: `api/internal/db/migrations/` (audit table se necessário)
- Editar: `infra/helm/monitoring/manifests/prometheusrule-critical.yaml` (LoopZeroBreakGlassAccess já existe, validar métrica)
**DoD:** Break-glass emitido → ações logadas → SA expira automaticamente
**Esforço:** 1 dia

### M4: Completar Foundation (S1-S4)
**Agentes:** Architect (FOUNDATION)
**O que:** O plano `next-sprint-execution.md` tem 4 items que são prerequisites para multi-cell funcionar:
- S1: Fix Alertmanager (= M1)
- S2: Wire ClusterCredential via Pulumi (= infra recreate)
- S3: Flip DynamicRouter para default
- S4: Rodar capacity baselines + L3 (100 tenants, 95+/100)
**DoD:** L3 100/100 tenants com DynamicRouter ativo e 2 runtime clusters
**Esforço:** Incluso na infra recreate

---

## Items Aprovados (4 de 10)

### B1: Pulumi Drift Detection
**Decisão:** APROVADO
**Agentes:** SRE (P0), GitOps (APPROVE), Security (APPROVE)
**O que:** GitHub Actions cron (cada 6h) rodando `pulumi preview --expect-no-changes`. Alert via Slack + GitHub Issue.
**Regra:** NUNCA auto-remediar. Drift intencional (hotfix) deve ser reconciliado via break-glass + Pulumi within 24h.
**Arquivos:**
- Criar: `.github/workflows/infra-drift.yml`
- Editar: `Taskfile.yml` (add `task infra:drift`)
**DoD:** Drift manual detectado → GitHub Issue criado + Slack notificado
**Esforço:** 0.5 dia

### B2: PG Scale-Up + PgBouncer
**Decisão:** APROVADO (escopo expandido pelo SRE)
**O que:** `dbNodeClass: medium` (max_connections ~100) + PgBouncer em transaction mode entre API pods e PG.
**Justificativa SRE:** Sem PgBouncer, 2+ API pods (mandatório para HA) competem pelo mesmo max_connections.
**Arquivos:**
- Editar: `infra/pulumi/Pulumi.homolog.yaml` → `dbNodeClass: medium`
- Editar: `infra/pulumi/src/components/mgmt-bootstrap.ts` (PgBouncer Helm release)
**DoD:** `SHOW max_connections;` ≥ 100. API conecta via PgBouncer. `task test:load:l2` sem connection errors.
**Esforço:** 0.5 dia

### A1: k6 + Go Harness Activation
**Decisão:** APROVADO (escopo refinado pelo DX)
**O que:** Dois propósitos distintos:
- **k6** para HTTP-level SLO testing (já existe em `infra/tests/k6/`)
- **Go harness** (`real_deploy_harness.go`) para pipeline E2E testing
- Integrar ambos no Taskfile
- Deprecar bash scripts ad-hoc (`run-l1.sh` superseded)
**Arquivos:**
- Editar: `Taskfile.yml` (add `task loadtest:run LEVEL=l1`)
- Criar: `infra/tests/load/README.md`
- Deprecar: `infra/tests/load/run-l1.sh` (mantido apenas como referência)
**DoD:** `task loadtest:run LEVEL=l1` funciona end-to-end. README documenta ferramentas canônicas.
**Esforço:** 1 dia

### Operation Rules (machine-enforced)
**Decisão:** APROVADO com enforcement mandatório (DX conditional block)
**O que:** 8 regras (6 originais + 2 novas) com enforcement técnico, não apenas documental.

| # | Regra | Enforcement |
|---|-------|------------|
| 1 | NUNCA `kubectl set image/env` | RBAC: remover `update` em deployments do SA humano (M2) |
| 2 | NUNCA criar ArgoCD Application manual | VAP: rejeitar Application sem label `loopzero.io/managed-by` |
| 3 | NUNCA `pulumi state delete --force` | Wrapper `task infra:state-delete` com audit log |
| 4 | Load test = Go harness + k6 | Taskfile entry points (`task loadtest:run`) |
| 5 | Drift check cada 6h | GitHub Actions cron (B1) |
| 6 | tenant-controller ≠ loopzero-api images | CI validation no `build-images.yml` |
| 7 | NUNCA `helm install/upgrade` fora do Pulumi | RBAC: remover Helm RBAC do SA humano |
| 8 | NUNCA `doctl` para modificações | doctl = read-only. Infra changes via Pulumi only. |

**Ação adicional (DX):** Criar `task api:hotfix` como fast-path sancionado para emergências:
```bash
# task api:hotfix → build, push, rollout restart com audit logging
# Substitui kubectl set image manual com trilha de auditoria
```

**Arquivos:**
- Criar: `infra/helm/admission-policies/manifests/vap-argocd-managed-by.yaml`
- Editar: `Taskfile.yml` (add `task api:hotfix`, `task infra:drift`, `task infra:state-delete`)
**DoD:** Cada regra tem enforcement técnico verificável. `task api:hotfix` funciona.
**Esforço:** 2 dias

---

## Items Rejeitados (6 de 10)

| Item | Veredicto | Razão (board) | Votos |
|------|-----------|---------------|:---:|
| **A2: ApplicationSet** | DEFER Phase 4 | Dual ownership com TenantController. ConfigMap race condition (`applicationset.go` sem resourceVersion). Code not production-ready. | 5/5 |
| **B3: Kyverno** | **VETADO** (SRE + Security) | VAPs existentes + RBAC suficientes. Kyverno é SPOF em admission chain. Break-glass não implementado. Se Kyverno crashar → ALL deployments bloqueados. | 4/5 |
| **B4: Build Priority Queue** | DEFER | YAGNI a <10 tenants. Advisory lock per-tenant já previne cross-tenant queuing. Instrumentar admission gate rejections primeiro. | 5/5 |
| **B5: ko Builds** | DEFER | API image é multi-binary (migrate + 2 Go bins + ca-certs). ko não produz limpa. DX fix é `task api:hotfix`, não mudar toolchain. | 3/5 |
| **C2: Crossplane** | REJEITADO | Decisão fechada (MEET-054). Pulumi ISP contracts cobrem multi-cloud. | 5/5 |
| **C3: Dagger** | **VETADO** (Architect + GitOps) | CWTs são 200 linhas YAML funcional. Dagger quebra build/release separation. BuildReconcileLoop depende de Workflow CRDs. | 4/5 |

---

## Infra Recreate (Multi-Cell)

### Estado Atual
- Pulumi state: 0 resources
- DO: apenas `loopzero-code-vm` (142.93.254.245) + DOCR registry + VPC orphan + 1 Spaces bucket orphan
- Imagens: todas 7 com tag `:homolog` no DOCR
- Código: todos fixes commitados (CellLister, CellDiscovery, runtime-2 kubeconfigs, lockfile warning, /tmp emptyDir, MaxTenantsPerCell)

### Fixes de Código já Commitados
- ✅ K8s version → `1.33.9-do.3`
- ✅ Lockfile → warning, não error (modelo Vercel)
- ✅ Java /tmp → emptyDir volume
- ✅ MaxTenantsPerCell → configurável (default 5000)
- ✅ ArgoCD skip-reconcile annotation
- ✅ ArgoCD workqueue tuning (maxdelay 300s)
- ✅ RELEASE_PENDING_TIMEOUT → 6m
- ✅ CellLister wired no BuildReconcileLoop
- ✅ CellDiscoveryChecker wired no health handler
- ✅ runtime-2 kubeconfigs (API + tenant-controller)
- ✅ 2 ClusterCredentials (runtime-nyc3-01, runtime-nyc3-02)
- ✅ 45/45 findings do review/final_report.md corrigidos

### Orphan Cleanup (antes do pulumi up)
```bash
# 1. Deletar bucket orphan container-images (versioned objects)
# 2. VPC: adopt-or-create detecta automaticamente
# 3. DOCR: Pulumi import após primeiro erro
```

### Pulumi Up
```bash
eval "$(grep -E '^(DIGITALOCEAN_TOKEN|SPACES_ACCESS_KEY_ID|SPACES_SECRET_ACCESS_KEY|PULUMI_CONFIG_PASSPHRASE|CLOUDFLARE_API_TOKEN)=' .env | sed 's/^/export /')"
export AWS_ACCESS_KEY_ID=$SPACES_ACCESS_KEY_ID
export AWS_SECRET_ACCESS_KEY=$SPACES_SECRET_ACCESS_KEY
pulumi up --stack homolog --yes
```

**Cria:**
- 4 DOKS clusters (mgmt, build, runtime-1, runtime-2)
- 1 PG managed (5 DBs)
- Helm charts (ArgoCD, Gitea, Traefik ×3, cert-manager ×3, Harbor, monitoring)
- 2 ClusterCredentials (multi-cell)

### Validação Multi-Cell
```bash
kubectl get clustercredential -n platform-system              # 2 CRs, Connected=true
kubectl logs deploy/loopzero-api -n platform-system | grep "cell" # count: 2
curl -s https://api.homolog.loopzero.example/api/v1/health/deep    # "cells": "2 discovered"
```

---

## Sequenciamento (2 semanas)

### Semana 1 — Foundation + Infra
| Dia | Item | Esforço |
|:---:|------|:---:|
| 1 | M1: Fix Alertmanager → Slack | 0.5 dia |
| 1 | B2: PG scale-up (`dbNodeClass: medium`) | 0.5 dia |
| 2 | Pulumi up (infra recreate com 4 clusters) | ~20 min + validação |
| 2 | M2: RBAC humano (read-only kubeconfig) | 1 dia |
| 3 | M3: Break-glass procedure | 1 dia |
| 4 | Validação multi-cell (L1 10/10) | 0.5 dia |
| 5 | Buffer / hotfixes | — |

### Semana 2 — Operational Tooling
| Dia | Item | Esforço |
|:---:|------|:---:|
| 1 | B1: Drift detection cron | 0.5 dia |
| 1 | VAP ArgoCD Application restriction | 0.5 dia |
| 2 | A1: k6 + Go harness + Taskfile | 1 dia |
| 3 | Operation rules enforcement (`task api:hotfix`, wrappers) | 1 dia |
| 4 | L3 100/100 stress test validation | 1 dia |
| 5 | L4 500/500 (se L3 passar) | 1 dia |

**Total: ~10 dias de engenharia**

---

## Regras para NÃO Repetir Erros

1. **NUNCA `kubectl set image/env`** → enforced via RBAC (M2). Use `task api:hotfix`.
2. **NUNCA criar ArgoCD Application manual** → enforced via VAP.
3. **NUNCA `pulumi state delete --force`** → use `task infra:state-delete` com audit.
4. **Load test = Go harness + k6** → `task loadtest:run LEVEL=l1`.
5. **Drift check cada 6h** → GitHub Actions cron (B1).
6. **tenant-controller ≠ loopzero-api** → CI validation.
7. **NUNCA `helm install/upgrade` fora do Pulumi** → RBAC.
8. **NUNCA `doctl` para modificações** → read-only. Pulumi only.

---

## Referências

- MEET-2026-04-21-055: Ata da reunião (decisões formais)
- MEET-2026-04-18-054: Multi-cloud strategy
- ADR-0008: Cell-based architecture
- `docs/plans/next-sprint-execution.md`: S1-S4
- `docs/plans/road-to-50k-evidence-based.md`: Scaling roadmap
- `docs/plans/cell-architecture-phase4.md`: ApplicationSet sharding (Phase 4)
- `review/final_report.md`: 45 findings (todos corrigidos)
