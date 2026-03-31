# Plano: Cell Architecture — Phase 4 (ApplicationSets + Optimization)

> **Objetivo:** Otimizar operacoes ArgoCD por Cell usando ApplicationSets. Reduzir
> complexidade do controller. Preparar o sistema para 50k+ apps.
>
> **Escopo:** 5 tasks. ApplicationSets, auto-sync evaluation, ArgoCD tuning, Cell template versioning, load test.
>
> **Classificacao:** T3 (ArgoCD model, controller logic, performance critica)
>
> **Trigger:** `loopzero_cell_tenant_count > 1000` ou ArgoCD reconcile latency p95 > 30s
>
> **Duracao:** 1 sprint
>
> **Principio:** "Nao gerenciar 50k Applications imperativamente. Deixar o ArgoCD declarar o que precisa existir."

---

## Contexto

Com Phase 3 completa, Cells sao provisionadas e decommissionadas automaticamente.
Porem, dentro de cada Cell, o TenantController ainda cria ArgoCD Applications
imperativamente via `EnsureApplication`. Com 5k+ apps por Cell, essa abordagem
gera carga desnecessaria no controller e no ArgoCD API server.

ApplicationSets permitem declarar "para cada diretorio no Gitea, crie um Application"
— o ArgoCD gerencia o ciclo de vida automaticamente.

### O Que Muda

```
ANTES: TenantController.EnsureApplication() → cria/atualiza ArgoCD Application via API
DEPOIS: 1 ApplicationSet por Cell → ArgoCD gera Applications automaticamente do Gitea
```

### O Que NAO Muda

- Deploy flow do desenvolvedor
- CLI identica
- API contracts identicos
- Gitea como source of truth para manifests
- AppProject isolation per-tenant (RBAC)
- State machines

---

## Tasks

### P4-1: ApplicationSet Per Cell

**Classificacao:** T3 (ArgoCD model change, controller logic)

**Arquivos:**
- `operators/internal/argocd/applicationset.go` (novo)
- `operators/internal/controller/tenant_controller.go` (remover `EnsureApplication`)
- `infra/helm/argocd/values.yaml` (habilitar ApplicationSet controller)

**Desafio de design:**
Cada tenant tem seu PROPRIO Gitea repo (`{tenantID}-gitops.git`). O ApplicationSet
precisa saber quais repos pertencem a cada Cell. Opcoes avaliadas:

| Opcao | Mecanismo | Pros | Contras |
|-------|-----------|------|---------|
| List Generator | ConfigMap com lista de tenant repos | Simples, previsivel | Requer atualizar ConfigMap ao adicionar tenant |
| Git Generator | Um meta-repo que lista todos os tenants | Nativo do ArgoCD | Mais um repo para manter |
| SCM Generator (Gitea) | Descobre repos automaticamente | Zero config | Requer SCM provider no ArgoCD, menos controle |
| Matrix Generator | List × Git Directory | Combina tenant list com env dirs | Mais complexo, mas mais poderoso |

**Decisao: List Generator via ConfigMap** (mais simples, mais previsivel, KISS):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: cell-sfo3-01-apps
  namespace: argocd
spec:
  generators:
    - matrix:
        generators:
          # Generator 1: lista de tenants nesta Cell (ConfigMap)
          - list:
              elementsYaml: "{{ .configMap.cell-sfo3-01-tenants.data.tenants }}"
          # Generator 2: diretorios no repo gitops do tenant
          - git:
              repoURL: "https://gitea/loopzero/{{ .tenantID }}-gitops.git"
              directories:
                - path: "*"  # cada diretorio = um environment
  template:
    metadata:
      name: "{{ .tenantID }}-{{ .path.basename }}"
    spec:
      project: "{{ .tenantID }}"  # AppProject isolation mantido
      source:
        repoURL: "https://gitea/loopzero/{{ .tenantID }}-gitops.git"
        path: "{{ .path }}"
      destination:
        server: "https://kubernetes.default.svc"
        namespace: "loopzero-{{ .tenantID }}-{{ .path.basename }}"
```

**ConfigMap gerenciado pelo TenantController:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cell-sfo3-01-tenants
  namespace: argocd
data:
  tenants: |
    - tenantID: tenant-abc
    - tenantID: tenant-def
```

**Mudancas no TenantController:**
- Remover: `EnsureApplication()` (substituido pelo ApplicationSet)
- Manter: `EnsureAppProject()` (RBAC isolation per-tenant)
- Adicionar: `UpdateCellTenantConfigMap()` — adiciona/remove tenant do ConfigMap da Cell

**Rollout gradual:**
- Implementar em 1 Cell primeiro (canary)
- Validar que Applications sao geradas corretamente
- Validar que deploys continuam funcionando
- Expandir para demais Cells

**DoD:**
- [ ] ApplicationSet template definido
- [ ] ConfigMap de tenants por Cell gerenciado pelo TenantController
- [ ] `EnsureApplication` removido do TenantController
- [ ] `EnsureAppProject` mantido (RBAC)
- [ ] Applications auto-geradas a partir do Gitea
- [ ] Teste: adicionar tenant ao ConfigMap → Application criada automaticamente
- [ ] Teste: remover tenant do ConfigMap → Application removida
- [ ] Teste: novo diretorio no Gitea → novo Application criado
- [ ] Teste: deploy E2E funciona com ApplicationSet
- [ ] Rollout em 1 Cell primeiro, expandir apos validacao
- [ ] `go test ./operators/...` passa
- [ ] `task validate:manifests` passa
- [ ] Validado em DigitalOcean

---

### P4-2: Auto-Sync Within Cells

**Classificacao:** T3 (ArgoCD sync model, impacta deploy flow)

**Arquivos:**
- `docs/adr/NNNN-argocd-auto-sync-evaluation.md` (novo — ADR obrigatorio)
- `api/internal/argocd/client.go` (potencial simplificacao)
- `api/internal/services/build_orchestrator.go` (potencial simplificacao)

**Avaliacao (trade-offs):**

| Aspecto | Manual Sync (atual) | Auto-Sync |
|---------|-------------------|-----------|
| Controle | API decide quando sincronizar | ArgoCD sincroniza ao detectar git change |
| Complexidade | TriggerSync + wait + callback | Elimina TriggerSync, simplifica API |
| Race condition | API controla timing | Git push + auto-sync pode conflitar com status check |
| Rollback | API reverte git + trigger sync | API reverte git, ArgoCD detecta automaticamente |
| Observability | Correlacao explicita (build → sync → active) | ArgoCD sync events (menos controle) |

**Proposta: auto-sync com `selfHealEnabled: false`:**
```yaml
syncPolicy:
  automated:
    prune: true           # remover recursos orfaos
    selfHeal: false        # NAO corrigir drift automaticamente
  retry:
    limit: 3
    backoff:
      duration: 5s
      factor: 2
      maxDuration: 1m
```

- `automated.prune: true` — ArgoCD remove recursos que nao existem no Git
- `selfHeal: false` — ArgoCD NAO corrige drift (evita conflito com operacoes manuais)
- Sync dispara quando Gitea push acontece (ArgoCD webhook ou polling)

**Se auto-sync aprovado:**
1. Remover `TriggerSync` calls do `ContainerDeliveryActivator`
2. `ReleaseConvergenceLoop` apenas monitora health, nao trigger sync
3. Simplificar `SyncCallbackActivator` — ArgoCD notifica via webhook quando sync completa
4. Ajustar testes de integracao

**Se auto-sync rejeitado:**
1. Manter manual sync
2. Documentar razoes no ADR
3. Reavaliar na Phase 5

**DoD:**
- [ ] ADR escrito com avaliacao completa (pros, contras, riscos)
- [ ] Decisao documentada: auto-sync aprovado ou rejeitado
- [ ] Se aprovado: auto-sync habilitado no ApplicationSet
- [ ] Se aprovado: `TriggerSync` removido do API
- [ ] Se aprovado: testes atualizados
- [ ] Se rejeitado: ADR documenta razoes e condicoes de reavaliacao
- [ ] Deploy E2E funciona com a decisao implementada
- [ ] `go test ./api/... ./operators/...` passa
- [ ] Validado em DigitalOcean

---

### P4-3: ArgoCD Resource Optimization

**Classificacao:** T2 (Helm values, performance tuning)

**Arquivos:**
- `infra/helm/argocd/values.yaml` (parametrizar por Cell size)
- `infra/pulumi/src/components/cell-template.ts` (adicionar tier config)

**Tuning por Cell size tier:**

| Parametro | Small (< 1k apps) | Medium (1k-3k apps) | Large (3k-5k apps) |
|-----------|-------------------|---------------------|---------------------|
| `--status-processors` | 20 | 50 | 100 |
| `--operation-processors` | 10 | 25 | 50 |
| Repo server replicas | 1 | 3 | 5 |
| Repo server parallelism | 5 | 10 | 20 |
| Redis maxmemory | 128MB | 256MB | 512MB |
| Redis compression | enabled | enabled | enabled |
| Application controller CPU request | 500m | 1 | 2 |
| Application controller CPU limit | 1 | 2 | 4 |
| Application controller memory request | 1GB | 2GB | 4GB |
| Application controller memory limit | 2GB | 4GB | 8GB |

**Mecanismo:**
- Cell template (Phase 3 P3-1) recebe `tier` parameter
- Pulumi resolve Helm values baseado no tier
- Tier pode ser ajustado sem reprovisionar Cell (helm upgrade)

**Auto-scaling consideration:**
- NAO implementar auto-scaling de ArgoCD nesta phase (YAGNI)
- Se necessario no futuro, HPA baseado em `argocd_app_reconcile_pending` queue length

**DoD:**
- [ ] Helm values parametrizados por tier (small/medium/large)
- [ ] Cell template aceita `tier` parameter
- [ ] Documentacao: qual tier usar para qual carga
- [ ] Teste: Cell com tier medium tem resources corretos
- [ ] Teste: mudar tier de Cell existente via helm upgrade
- [ ] `task validate:manifests` passa

---

### P4-4: Cell Template Versioning

**Classificacao:** T2 (infra, versionamento)

**Arquivos:**
- `infra/pulumi/src/components/cell-template.ts` (adicionar versioning)
- `infra/cell-templates/` (novo diretorio — templates versionados)

**Modelo:**
- Cell template = conjunto versionado de:
  - K8s version (DOKS)
  - ArgoCD Helm chart version + values
  - Traefik Helm chart version + values
  - cert-manager Helm chart version + values
  - Network policies
  - Resource quotas default

**Versionamento:**
```
infra/cell-templates/
├── v1.0.0/
│   ├── template.yaml      # versoes de todos os componentes
│   ├── argocd-values.yaml  # ArgoCD Helm values
│   ├── traefik-values.yaml
│   └── cert-manager-values.yaml
├── v1.1.0/
│   └── ...
```

**Template manifest:**
```yaml
apiVersion: loopzero.dev/v1
kind: CellTemplate
metadata:
  version: "1.1.0"
spec:
  kubernetes:
    version: "1.32"
    nodeCount: 2
    nodeSize: "s-2vcpu-4gb"
  argocd:
    chartVersion: "7.7.16"
    tier: "medium"  # maps to P4-3 resource tuning
  traefik:
    chartVersion: "28.3.0"
    replicas: 2
  certManager:
    chartVersion: "1.14.5"
```

**Cells em versoes diferentes (rolling upgrade):**
- Cells podem estar em versoes diferentes do template
- Upgrade process:
  1. Criar nova Cell com template v1.1.0
  2. Migrar tenants da Cell antiga (Phase 3 P3-2)
  3. Decommissionar Cell antiga (Phase 3 P3-4)
- Alternativa (in-place upgrade): helm upgrade de componentes na Cell existente
  - Mais rapido mas com risco de downtime
  - Usar apenas para minor version bumps

**Metrica:** `loopzero_cell_template_version{cell_id, version}` (gauge, 1=current)

**DoD:**
- [ ] Estrutura de templates versionados criada
- [ ] Pulumi le template version ao provisionar Cell
- [ ] Cells podem rodar em versoes diferentes
- [ ] Upgrade process documentado (rolling vs in-place)
- [ ] Metrica `loopzero_cell_template_version` emitida
- [ ] Teste: provisionar Cell com template v1.0.0
- [ ] Teste: provisionar Cell com template v1.1.0
- [ ] Teste: verificar que Cells em versoes diferentes coexistem
- [ ] `task validate:manifests` passa

---

### P4-5: 50k App Load Test

**Classificacao:** T2 (teste de carga, validacao de escala)

**Arquivos:**
- `infra/tests/load/cell_scale_test.go` (novo)
- `infra/tests/load/fixtures/synthetic_tenant.go` (novo)

**Setup:**
- 10 Cells simuladas (pode ser Cells reais em DO ou mock Cells para custo)
- 5k apps sinteticas por Cell (50k total)
- Tenants sinteticos com deployments sinteticos
- Gitea repos sinteticos com manifests minimos (1 Deployment + 1 Service)

**Metricas a medir:**

| Metrica | Target | Failure threshold |
|---------|--------|-------------------|
| Deploy latency p50 | < 30s | > 60s |
| Deploy latency p95 | < 60s | > 120s |
| Deploy latency p99 | < 120s | > 300s |
| ArgoCD sync time p95 | < 10s | > 30s |
| API response time p95 | < 200ms | > 1s |
| Cross-cell query p95 | < 2s | > 5s |

**Stress scenarios:**
1. 100 concurrent deploys across 10 Cells
2. Burst: 50 deploys em 1 Cell + 50 nas outras 9
3. Cell-5 under heavy load → validar que Cell-1 nao e afetada (isolation)
4. 1 Cell unhealthy → validar que outras Cells operam normalmente

**Tooling:**
- k6 ou custom Go load generator
- Go load generator preferido (ja temos infra de teste em Go)

**Report format:**
```markdown
# Cell Scale Test — 50k Apps

## Environment
- 10 Cells, 5k apps each
- Region: sfo3
- Template: v1.0.0

## Results
| Metric | p50 | p95 | p99 | Status |
|--------|-----|-----|-----|--------|
| Deploy latency | 25s | 52s | 98s | PASS |
| ...

## Isolation Validation
- Cell-5 under stress: OK, Cell-1 unaffected
- Cell-3 unhealthy: OK, other Cells normal

## Bottlenecks Identified
- ...

## Recommendations
- ...
```

**DoD:**
- [ ] Load test framework implementado em Go
- [ ] 10 Cells com 5k apps cada (50k total) simuladas
- [ ] Deploy latency p50 < 30s, p95 < 60s, p99 < 120s
- [ ] ArgoCD sync time p95 < 10s
- [ ] 100 concurrent deploys validados
- [ ] Isolation validada: Cell sob stress nao afeta outras
- [ ] Report gerado com metricas e recomendacoes
- [ ] Bottlenecks identificados e documentados
- [ ] Resultados revisados por equipe

---

## Checklist Global — Phase 4

### Gate: Phase 4 Complete

- [ ] P4-1: ApplicationSets substituem `EnsureApplication` em pelo menos 1 Cell
- [ ] P4-2: Auto-sync avaliado, decisao documentada em ADR, implementada se aprovada
- [ ] P4-3: ArgoCD tuning documentado por Cell size (small/medium/large)
- [ ] P4-4: Cell templates versionados, Cells coexistem em versoes diferentes
- [ ] P4-5: Load test de 50k apps passou com metricas dentro dos targets
- [ ] Todos: `go build ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `go test ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `task validate:manifests` passa
- [ ] Todos: testes existentes continuam passando
- [ ] Changelog atualizado
- [ ] Documentacao completa
- [ ] Validado em DigitalOcean

### O Que NAO Fazer na Phase 4

- [ ] NAO implementar auto-scaling de ArgoCD (HPA — YAGNI)
- [ ] NAO implementar multi-region failover (escopo diferente)
- [ ] NAO modificar CLI publica
- [ ] NAO modificar API contracts publicos
- [ ] NAO implementar Cell federation (management plane distribuido)
- [ ] NAO otimizar prematuramente — medir primeiro, otimizar depois

---

## Apos Phase 4

Com Phase 4 completa, o LoopZero suporta:
- 50k+ apps distribuidas em N Cells
- Cells provisionadas e decommissionadas automaticamente (Phase 3)
- ApplicationSets gerenciam Applications declarativamente (Phase 4)
- ArgoCD otimizado por Cell size
- Cell templates versionados com rolling upgrade

**Proximos passos potenciais (nao planejados — YAGNI):**
- Multi-region failover (Cell em SFO3 falha → Cell em NYC3 assume)
- Cell federation (management plane distribuido)
- Auto-scaling de ArgoCD por Cell (HPA)
- Tenant self-service Cell selection (premium tier)

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `docs/adr/0008-cell-based-architecture.md` | ADR que aprova esta arquitetura |
| `docs/plans/cell-architecture-phase0.md` | Phase 0 — foundation |
| `docs/plans/cell-architecture-phase3.md` | Phase 3 — automation (pre-requisito) |
| `docs/adr/0007-argocd-application-model.md` | ArgoCD Application model atual (substituido por ApplicationSets) |
| `infra/helm/argocd/values.yaml` | ArgoCD Helm values atuais |
| `operators/internal/argocd/` | ArgoCD provisioner atual (EnsureApplication) |
| `api/internal/placement/` | PlacementEngine + CellRouter |
