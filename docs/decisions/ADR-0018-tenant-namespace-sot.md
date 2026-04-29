# ADR-0018 — Tenant CR Single Source of Truth: namespace `loopzero-system`

> **Status:** APPROVED v2 (post-T2 review, MEET-ADR-0018 2026-04-29)
> **Authors:** dogfood-2026-04-29 remediation iter 3 (v1) + iter 5 amendment per MEET-ADR-0018 (v2)
> **Triggered by:** F-04 finding (Tenant duplicate em `default` E `loopzero-system`)
> **Scope:** T3.1 (operator/API code) + T3.2 (ValidatingAdmissionPolicy)
> **Related:** `docs/plans/dogfood-2026-04-29-remediation-plan.md` (ADR D3)
> **Companion:** `infra/scripts/migrate-tenant-namespace.sh` (T1 data cleanup, already shipped)
> **T2 Decision Record:** `docs/decisions/MEET-ADR-0018-decision.md` — APPROVED_WITH_CONDITIONS C1-C16
> **Rollout runbook:** `docs/runbooks/adr-0018-rollout.md` (mandatory deployment order)
> **Follow-up:** ADR-0019 (RBAC tightening, deferred to Sprint 9)

## Mandatory Conditions (from MEET-ADR-0018)

C1-C16 são **vinculantes** — implementação só pode prosseguir após cada uma estar coberta:

| ID | Condition | Source |
|----|-----------|--------|
| C1 | Remove `TENANT_NAMESPACE` env var; substituir por Go `const`. CHANGELOG `[Removed]`. | platform-architect |
| C2 | Tenant-controller startup detecta misplaced Tenants cluster-wide e emite warning event + métrica. Fail-loud. | platform-architect (= C5 em chave operacional) |
| C3 | ADR §Constraints: future multi-namespace tenant topology requer novo ADR superseding. | platform-architect |
| C4 | Mandatory deployment ORDER: T3.3 → T3.1 → T3.2. Encoded em runbook + Helmfile dependency. | production-design-reviewer |
| C5 | Métrica Prometheus `loopzero_misplaced_tenant_total{namespace}` + alert ≥1 por >5min. | production-design-reviewer |
| C6 | VAP `matchConstraints` exclui status subresource explicitamente; envtest spec verifica. | production-design-reviewer |
| C7 | Runbook `docs/runbooks/adr-0018-rollout.md` com order, abort, rollback, DR-restore. | production-design-reviewer |
| C8 | Customer disruption: 24h advance notice + scheduled migration window. | production-design-reviewer |
| C9 | T3.1 inclui `cache.Options{ByObject: ...}` per-Kind: Tenant/Environment/PlatformBootstrap/GitRepositoryBinding/ClusterCredential → `loopzero-system`; LoopZeroApplication/PreviewEnvironment/ExposedRoute → cluster-wide. | k8s-controller-engineer |
| C10 | Owner-reference topology verificada antes do merge. Documentada em §Implementation Plan. | k8s-controller-engineer |
| C11 | Envtest: `tenant_namespace_isolation_test.go` — operator NÃO reconcilia Tenant fora de `loopzero-system`. | k8s-controller-engineer |
| C12 | RBAC tightening DEFERRED → ADR-0019. Remove de §Positive consequences. | k8s-controller-engineer |
| C13 | Test matrix T-01..T-11 GREEN antes do merge; T-13 antes de prod rollout. | change-validation-planner |
| C14 | Regression test do iter 3 incident (skip-reconcile guard). | change-validation-planner |
| C15 | CLI mapeia VAP rejection → product language (Rule 3); não vaza "loopzero-system". | orchestrator self-challenge |
| C16 | DR/restore runbook update — restore-from-backup pode reintroduzir misplaced Tenants. | orchestrator self-challenge |

## Context

O dogfood 2026-04-29 revelou que `kubectl get tenants -A` retorna duas
instâncias **com plans divergentes** para o mesmo tenant lógico:

| Namespace | Plan | createdAt | Origem |
|-----------|------|-----------|--------|
| `default` | enterprise | 01:10 UTC | Manual `kubectl apply` (iter 7-8 do plan-pai) |
| `loopzero-system` | starter | 04:52 UTC | loopzero-api `POST /api/v1/projects` |

Resultado: **plan enforcement (billing, quota, RBAC, cell placement) é
indeterminístico**. O tenant-controller reconcile loop processa AMBAS
as CRs e ambas terminam em `Active`, com `.status.cellID` vazio (vs
`.spec.cellID=cell-default` populado).

Mais grave: o **incidente de iter 3** demonstrou que removendo o finalizer
de uma das CRs (a stale) cascateia cleanup destrutivo nos resources
**compartilhados** (ArgoCD App, workload namespace, IngressRoutes) que a
CR canônica também gerencia. HTTP 200 virou HTTP 404 imediatamente.

## Decision

**Tenant + Environment CRs DEVEM viver exclusivamente em `loopzero-system`**,
enforced em três camadas:

### Layer 1 — Code (T3.1)

`api/internal/k8s/tenant_cr_manager.go` e equivalentes do operator
hard-codam `const tenantNamespace = "loopzero-system"` em vez de aceitar via
config. Calls a `Resource(tenantGVR).Namespace(...)` SEMPRE usam essa
constante. Tenant-controller `mgr.GetClient().List(..., InNamespace
("loopzero-system"))` em watches (já o faz para o canônico, mas atualmente
também processa `default` por watch all-namespaces — investigar).

### Layer 2 — Admission (T3.2)

`infra/helm/admission-policies/templates/vap-tenant-namespace.yaml`
(NEW) declara `ValidatingAdmissionPolicy` nativo K8s 1.30+ que rejeita
criação de CRD `loopzero.io/{Tenant,Environment}` em qualquer namespace ≠
`loopzero-system`:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: loopzero-tenant-namespace-policy
spec:
  matchConstraints:
    resourceRules:
      - apiGroups: ["loopzero.io"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["tenants", "environments"]
  validations:
    - expression: 'object.metadata.namespace == "loopzero-system"'
      message: 'Tenant/Environment CRs must be created in loopzero-system namespace (ADR-0018)'
      reason: Forbidden
```

VAP é nativo K8s 1.30+ (mgmt cluster pin é 1.32). Sem dependência de
cert-manager para webhook (vs ValidatingWebhookConfiguration).

### Layer 3 — Data (T3.3, already shipped)

`infra/scripts/migrate-tenant-namespace.sh` reconcilia CRs duplicados
existentes. Ver `Taskfile.yml` task `migrate:tenants`. T3.3 sozinho é T1
(data cleanup, sem mudança de contrato) e foi shipped em iter 3.

## Rationale

### Por que `loopzero-system` e não `default`?

- `default` é namespace **user-friendly** (arbitrary user resources). LoopZero
  CRDs são **platform-scoped** — não convivem com user manifests.
- `loopzero-system` já é namespace canonical para loopzero-api, tenant-controller,
  Gitea, MinIO, postgresql, ArgoCD client connections. Single source of
  truth simplifica RBAC scope.
- `api/internal/k8s/secret_k8s_manager.go:64` já hard-coda
  `mgmtNamespace = "loopzero-system"`. T3.1 estende essa decisão para
  Tenant/Environment.

### Por que VAP nativo e não webhook custom?

- VAP é nativo K8s 1.30+ — não exige cert-manager para inject TLS no
  webhook server.
- CEL expressions são auditáveis sem code-review-em-dois-arquivos
  (validation runs no API server, não em nosso pod).
- Same chart pattern dos VAPs existentes (`infra/helm/admission-policies/
  templates/vap-{argocd,disallow-latest-tag,...}.yaml`).

### Por que T2 governance é obrigatório?

CLAUDE.md `pre-release-governance.md` lista "Tenant isolation changes" como
**Automatic T2 trigger**. ADR-0018 muda:
- **API contract**: callers de `tenant_cr_manager` que passam namespace
  param vão quebrar.
- **Operator behavior**: watches restringem-se a `loopzero-system`.
- **Customer expectation**: tenants self-applied em qualquer namespace
  (raro mas possível) deixam de funcionar.

Por isso T3.1 + T3.2 exigem meeting com:
- `platform-architect` (boundaries / source of truth)
- `production-design-reviewer` (blast radius vs operational simplicity)
- `change-validation-planner` (test strategy: what regressions exist?)
- `k8s-controller-engineer` (controller-runtime watch implications)

## Consequences

### Positive
- **Plan enforcement determinístico**: 1 CR canônica → 1 plan ativo → 1
  cell placement → 1 quota.
- **RBAC simplificado**: tenant-controller ClusterRole pode reduzir scope
  para namespace-scoped (Role + RoleBinding em loopzero-system).
- **Fail-closed**: VAP rejeita misplaced CRs em CREATE — usuários veem
  erro imediato em vez de drift silencioso.

### Negative
- **Backward break**: customers self-hosted que tenham Tenant em outras
  namespaces precisam migrar via T3.3 script ANTES de upgrade do operator.
- **Migration risk**: T3.3 script já demonstrou (incidente iter 3) que
  delete cascateia cleanup. Hardened com `skip-reconcile` annotation, mas
  precisa testes adicionais antes de produção.

### Constraints
- Operator deve continuar a tolerar Tenant CR em outras namespaces durante
  janela de migração (T3.3 deve rodar BEFORE T3.1 hard-coding entra em
  vigor).
- VAP em standalone-multi: como outros VAPs, opt-in via `.Values.alerts.
  enabled` ou similar — k3d local pode operar sem ele para iteração rápida.

## Implementation Plan (post-approval)

1. **T3.3 (DONE):** `migrate-tenant-namespace.sh` shipped em iter 3.
2. **T3.1:** `api/internal/k8s/tenant_cr_manager.go` + companion files —
   const `tenantNamespace = "loopzero-system"`. Update callers. Operator
   watch restrict.
3. **T3.2:** `infra/helm/admission-policies/templates/vap-tenant-namespace
   .yaml` + `values.yaml` toggle.
4. **Test plan (per change-validation-planner):**
   - Unit: tenant_cr_manager_test.go — assert namespace=loopzero-system in all
     creates/gets.
   - Integration: envtest spec — try create Tenant em `default`, expect
     forbidden via VAP.
   - E2E: `loopzero init` em fresh cluster com VAP ativo, validar que Tenant
     vai p/ loopzero-system. Migration script test em cluster com duplicates
     pré-existentes.
5. **Rollback plan:** VAP toggle off via Helm overlay; T3.1 revertido
   restaura namespace param. T3.3 deletions são irreversíveis (backup em
   `~/.loopzero/backups/tenant-migration-*` permite restore manual).

## Status checklist

- [ ] Convocação meeting via `meeting-orchestrator` agent
- [ ] platform-architect review
- [ ] production-design-reviewer review
- [ ] change-validation-planner sign-off + test plan ratificado
- [ ] k8s-controller-engineer review (watch + RBAC implications)
- [ ] decision-record-moderator promotes este draft → APPROVED
- [ ] Implementação T3.1 + T3.2 sob este ADR

## References

- F-04 finding: dogfood report 2026-04-29 (`docs/plans/dogfood-2026-04-29-
  remediation-plan.md`)
- Companion T3.3: `infra/scripts/migrate-tenant-namespace.sh`
- CLAUDE.md `pre-release-governance.md`: Tenant isolation = T2 trigger
- Similar pattern: ADR-0015 (adopt-or-create policy)
