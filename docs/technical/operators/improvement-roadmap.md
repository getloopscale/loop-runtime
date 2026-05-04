# Operators Improvement Roadmap

**Research date:** 2026-05-04
**Researcher:** Claude (SOTA Research Skill, Opus 4.7 1M)
**Current SOTA score:** 6.0 / 10
**Target SOTA score:** 8.5 / 10 (Sprint 9 + post-launch quarter 1)
**Current Operator SDK Maturity Level:** III (Full Lifecycle)
**Target Operator SDK Maturity Level:** IV (Deep Insights), measurable progress toward V (Auto Pilot)

---

## Executive Summary

Os operators do LoopZero já implementam corretamente os fundamentos: 10/10 CRDs com `subresource:status`, `observedGeneration` enforced via `conditions.SetCondition`, finalizers com cleanup observável, state machines em 9 lifecycles, circuit breakers em todas as 4 dependências externas (Gitea, ArgoCD, Harbor, MinIO), cache scoping per-Kind para CRDs de plataforma, multi-cluster wiring explícito (build/runtime/mgmt) e validação estrutural de wiring (`cmd/registration_test.go`). Em uma escala de maturidade Operator SDK (1-5), LoopZero já alcança Level III sólido.

A lacuna real está em três frentes:

1. **Duplicação ativa de ~5.800 LOC** entre `internal/controller/*` (monolítico, deployado em produção) e `internal/{tenant,app,preview,platform}/*` (split, "forward-looking"). Os pares diferem apenas em `package` e em chamada de tracing (`startReconcileSpan` vs `shared.StartReconcileSpan`). O split tree tem **zero** test files. Cada PR que toca controllers precisa mexer em duas árvores ou criar drift silencioso. ClusterCredential já divergiu 78 linhas — drift em curso.
2. **Convenções kstatus / kpt não adotadas**: zero predicates (`GenerationChangedPredicate`), nenhum tipo `Stalled`, nenhum `Ready` summary universal, RBAC sem `kubebuilder:rbac` markers (escrito à mão em `config/rbac/`), `retry.RetryOnConflict` usado em apenas 1 arquivo (argocd/real_provisioner.go:237). O loop reconcilia em **toda** mudança de status (incluindo as do próprio controller), gerando reconcile storms desnecessários.
3. **Operator Capability Level IV/V parcialmente atingido sem ser explícito**: LoopZero tem 43+ métricas Prometheus + KEDA scale-to-zero + auto-healing parcial (application reaper + heal scripts H12-H15), mas nada disso está mapeado contra o modelo de capability levels. Não temos um critério objetivo para dizer "fomos de Level III para Level IV".

**Conclusão:** A engenharia base é sólida. O caminho para 8.5/10 não passa por re-escrever — passa por (a) consolidar as duas árvores numa só, (b) adotar convenções SIG/kstatus que reduzem custo de manutenção, e (c) tornar explícito o nível de capability já alcançado e o que falta para Level V.

---

## Competitive Position

Score 1-5 por dimensão. "Best-in-class" referencia o operator com a prática de FAANG/SIG mais madura.

| Dimensão | LoopZero | Crossplane | CloudNativePG | Korifi | Kueue | Best-in-class | Gap | Effort |
|---|---|---|---|---|---|---|---|---|
| Reconcile idempotence | 4 | 5 | 5 | 5 | 5 | 5 (CNPG) | -1 | S |
| Crash-safe progression | 4 | 4 | 5 | 4 | 4 | 5 (CNPG: instance-manager promotion) | -1 | M |
| Status as contract (kstatus compliance) | 3 | 5 | 4 | 4 | 4 | 5 (Crossplane: Ready computed from composed) | -2 | M |
| ObservedGeneration discipline | 4 | 5 | 4 | 4 | 4 | 5 | -1 | S |
| Finalizer discipline | 4 | 4 | 5 | 4 | 4 | 5 (CNPG: pod-disruption-budget aware) | -1 | M |
| Spec/Status separation (OP-G06) | 4 | 5 | 4 | 5 | 4 | 5 | -1 | M |
| PaaS abstraction integrity (OP-G07) | 4 | 3 | N/A | 5 | N/A | 5 (Korifi: AppWorkload pluggable runners) | -1 | L |
| Isolation by default | 4 | 3 | 4 | 5 | 4 | 5 (Korifi: per-org/space namespaces + RBAC) | -1 | M |
| External dep containment (CB) | 5 | 4 | N/A | 3 | N/A | 5 (LoopZero já top — zero churn needed) | 0 | — |
| Multi-cluster correctness | 3 | 4 | N/A | 4 | N/A | 4 (sigs.k8s.io/multicluster-runtime) | -1 | L |
| State machines for lifecycles | 5 | 3 | 4 | 4 | 4 | 5 (LoopZero + pkg/statemachine — top of class) | 0 | — |
| Drift correction over one-time | 4 | 5 | 5 | 4 | 4 | 5 (Crossplane: 60s poll-triggered) | -1 | S |
| Wiring completeness | 3 | 4 | 4 | 4 | 4 | 4 (Crossplane: ControllerEngine.Start auto-wires) | -1 | S |
| Observability native | 4 | 4 | 5 | 3 | 4 | 5 (CNPG: 60+ metrics + alerts) | -1 | S |
| Test matrix | 3 | 4 | 5 | 4 | 5 | 5 (CNPG: 90%+ coverage + e2e on real DB) | -2 | L |
| Upgrade/API evolution | 3 | 5 | 5 | 4 | 4 | 5 (Crossplane: CompositionRevisions) | -2 | L |
| **Predicate filtering** | 1 | 5 | 5 | 4 | 5 | 5 | **-4** | S |
| **kpt Reconciling/Stalled adoption** | 1 | 4 | 3 | 3 | 4 | 4 (Crossplane: native kstatus) | **-3** | S |
| **kubebuilder:rbac markers** | 1 | 5 | 5 | 5 | 5 | 5 | **-4** | M |
| **Capability Level IV/V mapping** | 2 | 4 | 5 | 3 | 4 | 5 (CNPG: declared L5 with auto-failover) | **-3** | M |

**Médias:** LoopZero 3.4/5 (current) → 4.3/5 (target post-Sprint-9) ≈ 8.5/10 SOTA score.

**Os três gaps mais agressivos** (-3 ou pior): predicate filtering, kpt Reconciling/Stalled, kubebuilder:rbac markers, e Operator Capability Level mapping. Cada um é um gate novo proposto na seção [New SOTA Gates](#new-sota-gates-proposed).

---

## Quick Wins (1-2 sessions each)

### QW-1: Adicionar `GenerationChangedPredicate` aos 8 controllers

**Onde:** `SetupXController` em `internal/controller/*` e `internal/{tenant,app,preview,platform}/*`.

**O que fazer:**
```go
return ctrl.NewControllerManagedBy(mgr).
    For(&loopzerov1.LoopZeroApplication{}, builder.WithPredicates(
        predicate.Or(
            predicate.GenerationChangedPredicate{},
            predicate.AnnotationChangedPredicate{},
            predicate.LabelChangedPredicate{},
        ),
    )).
    Owns(&loopzerov1.ExposedRoute{}).
    ...
```

**Impacto esperado:** Reduz reconciles em 50-90% conforme o operator runs (cada `Status().Update` hoje retriggera o For-watch). Reduz P95 de reconcile latency e CPU dos pods. Métrica para validar: `controller_runtime_reconcile_total{controller="..."}` antes/depois.

**Por que SOTA exige isso:** kubebuilder book §"Using Predicates" classifica `ResourceVersionChangedPredicate` como warning (default behavior) e `GenerationChangedPredicate` como recommended para reconcilers status-heavy.

**Risco:** Alguns reconcilers fazem trabalho útil em metadata changes (label/annotation). Por isso o `Or(...)` cobre os três casos comuns.

**Arquivos afetados:** ~16 SetupXController calls (8 controllers × 2 trees até a árvore split sumir).

---

### QW-2: Codificar `Reconciling` e `Stalled` types em `internal/conditions/types.go`

**Onde:** `operators/internal/conditions/types.go`

**O que fazer:** Adicionar:
```go
// Standard kpt/kstatus condition types — set on ALL LoopZero CRDs that
// represent multi-step processes. kstatus library (sigs.k8s.io/cli-utils)
// reads these to determine reconciled-or-not.
const (
    TypeReconciling = "Reconciling" // True = controller is actively working
    TypeStalled     = "Stalled"     // True = controller cannot make progress
    TypeReady       = "Ready"       // True = resource is fully reconciled & healthy
)
```

E um helper:
```go
// SetReadinessSummary computes Ready, Reconciling, Stalled from per-step
// conditions. Idempotent. Should be the LAST status mutation in every
// reconcile. Conforms to kstatus polling (sigs.k8s.io/cli-utils/pkg/kstatus).
func SetReadinessSummary(conds *[]metav1.Condition, generation int64,
    detailTypes []string) { ... }
```

**Impacto esperado:**
- `kubectl wait --for=condition=Ready` works on every LoopZero CRD — DX win for CLI/API/dashboard probes.
- `kubectl get applications -o wide` shows `READY` column reliably.
- kstatus library consumers (Argo Rollouts, Flux, Crossplane composition functions) interoperate out-of-the-box.

**Arquivos afetados:** `internal/conditions/types.go` (+1 helper); 8 controllers chamam `SetReadinessSummary` no fim do reconcile.

**FAANG reference:** [kstatus README](https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md) e [kpt CRD Status Convention](https://kpt.dev/reference/schema/crd-status-convention/).

---

### QW-3: Substituir RBAC manual por `kubebuilder:rbac` markers

**Onde:** Topo de cada `*_controller.go`; `config/rbac/role.yaml` re-gerado por `make manifests`.

**O que fazer:**
```go
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// ...
type TenantReconciler struct { ... }
```

**Impacto esperado:**
- Audit trail automático: PR que adiciona `r.Client.List(podList)` força marker change explícito.
- `task validate:rbac` passa a verificar congruência via `controller-gen rbac`.
- Reduz risco de privilege escalation silencioso (hoje qualquer um adiciona kubectl call sem questionar RBAC).

**FAANG reference:** Crossplane, CloudNativePG, Korifi, Kueue — todos geram RBAC via markers. Não é estilo, é higiene de supply chain.

**Risco:** Migração precisa diff manual entre `config/rbac/role.yaml` atual e o gerado. Provavelmente identifica permissões hoje grant-but-not-needed (good: least privilege).

---

### QW-4: Estender `cmd/registration_test.go` para cobrir TODOS os 4 binários split

**Onde:** `operators/cmd/registration_test.go`

**O que fazer:** Refatorar de hardcoded `"../internal/controller"` + `"main.go"` para uma matriz:

```go
var binaryWiringMatrix = []struct {
    name         string
    pkgDir       string  // exported funcs scanned here
    mainPath     string  // function calls scanned here
    setupFunc    string  // top-level wiring function name
}{
    {"monolith",   "../internal/controller", "main.go", "setupControllers"},
    {"tenant-op",  "../internal/tenant",     "tenant-operator/main.go",  "main"},
    {"app-op",     "../internal/app",        "app-operator/main.go",     "main"},
    {"preview-op", "../internal/preview",    "preview-operator/main.go", "main"},
    {"platform-op","../internal/platform",   "platform-operator/main.go","main"},
}

for _, bin := range binaryWiringMatrix {
    t.Run(bin.name, func(t *testing.T) { ... })
}
```

**Impacto esperado:** Hoje, se alguém adiciona `SetupNewController` em `internal/tenant/` e esquece de wire em `cmd/tenant-operator/main.go`, **nenhum teste falha**. Após este fix, falha imediatamente. Fecha simetricamente o gate OP-G13 para todos os binários.

**FAANG reference:** Esta é uma melhoria interna do gate OP-G13 — Crossplane tem gate equivalente via `ControllerEngine.Start`.

---

### QW-5: Adotar `controller-runtime/pkg/reconcile/reconciler.Reconciler` retry helper

**Onde:** Todo `r.Status().Update(ctx, &res)` pattern em controllers.

**O que fazer:** Wrap em `retry.RetryOnConflict(retry.DefaultRetry, func() error {...})`.

```go
err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
        return err
    }
    // mutate app.Status here
    return r.Status().Update(ctx, &app)
})
```

**Impacto esperado:** Elimina log noise de "the object has been modified" durante alta concorrência (10+ tenants reconciling simultaneamente). Crítico em produção quando 30 webhooks GitHub disparam ao mesmo tempo.

**FAANG reference:** [kubebuilder good-practices §Conflict handling](https://book.kubebuilder.io/reference/good-practices) — explicit "use RetryOnConflict to avoid log noise".

**Risco:** Apenas para Status updates, nunca para Spec mutations (essas devem falhar e retornar requeue conforme operator pattern).

---

## Sprint 9 Targets (Polish + Launch)

### S9-T1: **Consolidação dual-tree** — eliminar duplicação `internal/controller` ↔ `internal/{tenant,app,preview,platform}`

**Decisão arquitetural a tomar:** monolítico OR split, não ambos.

**Critério para decidir:**
- Monolítico (manter): footprint ~63MB binary, single Helm chart, simpler ops, mas blast radius unificado.
- Split (manter): 4 binários, 4 Helm charts, leader election independente, blast radius por domínio. Mas hoje **duplica todo o código sem benefício**.

**Recomendação:** **Migrar para split + deletar `internal/controller/*`**. Razões:
1. CLAUDE.md já documenta que charts split estão prontos em `charts/loopzero-*-operator/`.
2. Operator-split-invariants doc (mencionado em `cmd/tenant-operator/main.go:97`) já existe.
3. Cache scoping per-Kind é mais granular nos binários split (cada um declara só os Kinds que reconcilia).
4. Prod hoje deploya monolito (`infra/helm/tenant-controller`); a migração é Helm chart swap + envtest revalidação.

**Plano de execução** (T2 governance gate via `meeting-orchestrator`, ADR mandatório):

| Sub-task | DoD | Files |
|---|---|---|
| S9-T1.1 | ADR-NNNN published declaring "monolith deprecated, split is canonical" | `docs/meetings/adr/` |
| S9-T1.2 | Mover testes `internal/controller/*_test.go` (21 files) para split tree | `internal/{tenant,app,preview,platform}/*_test.go` |
| S9-T1.3 | `operatortest/setup.go` re-importa de split packages | `operators/operatortest/setup.go:24` |
| S9-T1.4 | `cmd/registration_test.go` matriz multi-binário (QW-4) | `cmd/registration_test.go` |
| S9-T1.5 | Delete `internal/controller/` + `cmd/main.go` + `Dockerfile` | -8000 LOC |
| S9-T1.6 | Helm `infra/helm/tenant-controller/` apontado para `loopzero-tenant-operator` chart | `charts/loopzero-tenant-operator/` |
| S9-T1.7 | E2E re-validation iter — `task test:e2e` GREEN | infra/tests/e2e/ |

**Impacto esperado:** -5800 LOC duplicadas (-25% no módulo); um único path de teste; impossível drift silencioso. Maior correção arquitetural do roadmap.

**Risco:** Bug latente no split tree não detectado (porque hoje testes só rodam contra monolítico). Mitigação: rodar full E2E + dogfood antes do merge final.

---

### S9-T2: **kstatus compliance universal** — `Ready/Reconciling/Stalled` em todas as 8 CRDs

Estende QW-2: cada controller termina o reconcile chamando `SetReadinessSummary`. Casos de erro fatais setam `TypeStalled=True` para que kstatus reporte o resource como stuck (e CLI/API surfacem isso ao usuário).

**Critério:**
```bash
kubectl wait --for=condition=Ready loopzeroapplications/foo -n loopzero-tenant-acme --timeout=60s
```
funciona para TODOS os 10 CRDs da plataforma.

**FAANG reference:** [Gateway API GEP-1364](https://gateway-api.sigs.k8s.io/geps/gep-1364/) — todas as resources do Gateway API expõem Ready, Programmed, Accepted no mesmo formato.

---

### S9-T3: **Capability Level IV declarado e mensurado**

Hoje LoopZero tem todos os ingredientes mas nenhum gate explicita o que significa "Level IV". Proposta:

| Capacity | LoopZero state | Gate concreto |
|---|---|---|
| Operator metrics on Operator's Performance | 43+ `loopzero_*` Prometheus métricas | ✓ Documentar em `docs/runbooks/sli-slo.md` |
| Workload Analysis: alertable conditions | 7 multi-cell alerts + SLO alerts | ✓ Já em produção |
| Performance Metrics | reconcile latency, requeue rate, error rate per controller | ✓ exportadas |
| Auto-Detection of CRD Issues | application reaper, heal scripts H12-H15 | **Codificar** em `pkg/health/` |

**Saída:** ADR-NNNN: "LoopZero operators alcançam Capability Level IV em Sprint 9". Com matriz de evidência verificável.

**FAANG reference:** [CloudNativePG Capability Levels doc](https://cloudnative-pg.io/documentation/1.25/operator_capability_levels/) — eles explicitam cada capacity com link para implementation file.

---

### S9-T4: **Predicates universais** (consolidação de QW-1 + reaper)

QW-1 entrega o pattern; S9-T4 entrega cobertura 100% + métricas de validação:

- `loopzero_reconcile_filtered_total{controller, predicate}` counter por predicado que rejeitou um evento. Permite ver, post-deploy, que % de eventos foi corretamente filtrado (esperado >50% para controllers status-heavy).
- Test envtest: criar resource → tocar `Status` 100x → assertar reconcile count == 1 (não 100).

---

### S9-T5: **kubebuilder:rbac generation pipeline** (S9 conclusion of QW-3)

QW-3 entrega markers; S9-T5 entrega o pipeline:

- `task generate:rbac` → roda `controller-gen rbac:roleName=loopzero-system paths=./...`
- CI gate: PR que muda Reconcile sem atualizar markers → diff em `config/rbac/role.yaml` → falha
- Rolar para charts publicados (`charts/loopzero-tenant-operator/templates/role.yaml`)

---

## Post-Launch (Quarter 1 after launch)

### PL-1: **Adoção de `kubernetes-sigs/multicluster-runtime`**

LoopZero hoje rola seu próprio multi-cluster wiring (`internal/shared/cluster.go`, `internal/shared/runtime_client_factory.go`). [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) é a extensão SIG-mantida do controller-runtime com semântica idêntica.

**Benefício:** elimina ~400 LOC custom; ganha provider model para alternar entre kubeconfig-secret-based, ServiceAccount-based, fleet-management providers. Especialmente crítico se LoopZero evoluir para 10+ cells (hoje 3).

**Custo:** breaking refactor no `cell-aware factory pattern`. Justifica ADR.

---

### PL-2: **Korifi-style runtime abstraction CRD** para Build/Workload

Hoje a API submete diretamente Argo Workflow (`api/internal/services/build_service.go`). Korifi pattern: API cria `BuildWorkload` CR; um runner controller (`kpack-runner` em Korifi) materializa em Argo Workflow / BuildKit. Vantagens:

- Permite trocar BuildKit por Buildah/Kaniko/Buildpacks sem mexer na API.
- Permite múltiplos runners em paralelo (canary entre kpack e BuildKit).
- Status do build vira observable via CR + kstatus.

**Quando vale a pena:** quando LoopZero introduzir um segundo builder (multi-arch, ARM, GPU). Antes disso é YAGNI.

**FAANG reference:** [Korifi AppWorkload](https://github.com/cloudfoundry/korifi/blob/main/docs/architecture.md).

---

### PL-3: **Crossplane v2 Operations pattern para PlatformBootstrap**

`PlatformBootstrap` hoje é um `Reconciler` com state machine — mas conceitualmente é um job (executa uma vez por cluster, marca completo, nunca mais). Crossplane v2 introduziu [Operations](https://docs.crossplane.io/latest/whats-new/) — function pipelines que rodam até completion como K8s Jobs.

**Quando vale:** se PlatformBootstrap virar mais complexo (multi-step DB seeding, multi-cluster bootstrap). Hoje 4 fases é tratável como Reconciler.

---

### PL-4: **Mutation/conversion webhooks para CRD evolução** (OP-G16)

Hoje CRDs estão em `v1`. Próxima quebra de schema (e.g., `Tenant.spec.cellID` → `Tenant.spec.placement.cellID`) exige conversion webhook. Sem isso, breaking change implica delete-recreate de tenants.

**FAANG reference:** [Crossplane CompositionRevisions](https://docs.crossplane.io/latest/composition/compositions/) + [tinfoilcipher XRD versioning](https://tinfoilcipher.co.uk/2025/10/28/crossplane-v2-xrd-and-composition-version-management/).

---

### PL-5: **CloudNativePG-grade observability**

CNPG expõe 60+ métricas. LoopZero tem 43+ — bom mas não best-in-class. Adicionar:

- `loopzero_reconcile_queue_depth` per controller
- `loopzero_finalizer_blocked_seconds` (quanto tempo um delete está stuck por finalizer)
- `loopzero_external_dep_circuit_state{breaker}` (já existe em parte)
- `loopzero_orphan_cr_total{kind}` (do reaper)

---

## New SOTA Gates Proposed

### Gate OP-G17: Predicate Filtering For Status-Heavy Controllers

**Objetivo:** Evitar reconcile storms causados por status updates do próprio controller.

**Critério de aprovação:**
- Todo `For(...)` em `SetupXController` declara `builder.WithPredicates(...)` explícito.
- Predicate inclui ao menos `GenerationChangedPredicate` ou racional documentado para o oposto.
- Métrica `controller_runtime_reconcile_total` em produção mostra ratio reconciles/spec-changes < 5x (default sem predicate é frequentemente 50-500x).

**Evidência esperada:**
- Code: `predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}, predicate.LabelChangedPredicate{})` em todo SetupXController.
- Test envtest: cria resource, atualiza status 100x, assert reconcile counter == 1.

**FAANG reference:** [kubebuilder Using Predicates](https://book.kubebuilder.io/reference/watching-resources/predicates-with-watch); [Crossplane predicate strategy](https://github.com/crossplane/crossplane/blob/main/internal/controller/apiextensions/composite/reconciler.go).

---

### Gate OP-G18: kstatus Compliance — Ready / Reconciling / Stalled

**Objetivo:** Permitir que ferramentas K8s-native (kubectl wait, kstatus library, Argo Rollouts, Flux) consumam status uniforme.

**Critério de aprovação:**
- Toda CRD do LoopZero expõe os tipos `Ready`, `Reconciling`, `Stalled` no `status.conditions`.
- `Ready=True` significa "fully reconciled, healthy"; `Reconciling=True` significa "controller working"; `Stalled=True` significa "cannot make progress, manual intervention".
- `kubectl wait --for=condition=Ready` funciona em **toda** CRD — gate verificável via teste E2E.

**Evidência esperada:**
- `internal/conditions/types.go` declara `TypeReady`, `TypeReconciling`, `TypeStalled` constantes.
- Helper `SetReadinessSummary(conds, generation, detailTypes)` em `internal/conditions/conditions.go`.
- Cada controller chama `SetReadinessSummary` ao fim do reconcile.
- Teste envtest: `eventually(condition Ready=True, 30s)` para cada CRD.

**FAANG reference:** [kstatus README](https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md); [Gateway API GEP-1364](https://gateway-api.sigs.k8s.io/geps/gep-1364/); [kpt CRD Status Convention](https://kpt.dev/reference/schema/crd-status-convention/).

---

### Gate OP-G19: RBAC Generated From Markers

**Objetivo:** Tornar o RBAC do controller derivável e auditável a partir do código que ele executa.

**Critério de aprovação:**
- Cada Reconciler tem `kubebuilder:rbac` markers cobrindo todas as `r.Client.{Get,List,Create,Update,Patch,Delete}` calls.
- `task generate:rbac` re-gera `config/rbac/role.yaml` e `charts/*/templates/role.yaml` a partir dos markers.
- CI gate: PR que altera Reconcile sem atualizar markers → diff em `role.yaml` → CI falha.

**Evidência esperada:**
- 0 hand-edited lines em `config/rbac/role.yaml` após o gate.
- `make manifests` ou `task generate:rbac` exit 0.
- `git diff config/rbac/role.yaml` é vazio em PR de pure-RBAC change unless markers changed.

**FAANG reference:** Crossplane, CloudNativePG, Korifi, Kueue — todos geram RBAC via `controller-gen`. Esta é higiene de supply chain, não estilo.

---

### Gate OP-G20: Operator Capability Level Declaration

**Objetivo:** Tornar explícito o nível de capacidade que cada controller alcança.

**Critério de aprovação:**
- ADR registrado declarando o Capability Level (I-V) atingido por cada controller individualmente.
- Cada nível tem matriz de evidência: arquivo + linha + teste que prova a capacidade.
- Métricas Prometheus específicas para Level IV (deep insights) e Level V (auto pilot) existem e são alertáveis.

**Evidência esperada:**
- `docs/meetings/adr/NNNN-operator-capability-levels.md`
- `docs/technical/operators/capability-matrix.md` mapeando: Tenant=L4, Application=L4, PreviewEnvironment=L5 (auto-cleanup), PlatformBootstrap=L3, etc.
- Métricas Level V: `loopzero_self_heal_total{controller, action}`, `loopzero_orphan_cr_total{kind}`.

**FAANG reference:** [CloudNativePG Operator Capability Levels](https://cloudnative-pg.io/documentation/1.25/operator_capability_levels/); [Operator SDK Capabilities](https://sdk.operatorframework.io/docs/overview/operator-capabilities/).

---

### Gate OP-G21: Single Source Of Truth For Controller Implementation

**Objetivo:** Eliminar a possibilidade de drift entre árvores duplicadas.

**Critério de aprovação:**
- Cada controller tem **uma** implementação canônica (não copiada entre `internal/controller/` e `internal/{tenant,app,preview,platform}/`).
- `find operators/internal -name "*_controller.go" | xargs basename | sort | uniq -d` retorna vazio.
- Teste estrutural: `cmd/registration_test.go` cobre TODOS os binários (matriz QW-4).

**Evidência esperada:**
- Migração documentada em ADR.
- `internal/controller/` removido (ou explicitamente designado como deprecated com data de remoção).
- Helm charts em `infra/helm/` apontam para os charts split em `charts/`.

**FAANG reference:** Não há "FAANG reference" para este gate — é o princípio universal de **DRY aplicado a knowledge** (CLAUDE.md regra 12). Toda PaaS madura (Crossplane, CNPG, Korifi) tem **uma** implementação per controller.

---

## Anti-Patterns To Eliminate

### AP-1: Tracing helpers locais vs `internal/shared`

**Onde:** `internal/controller/application_controller.go:37,42,44,46,133`

```go
ctx, span := startReconcileSpan(ctx, "application", req.Name, req.Namespace)
recordError(span, retErr)
setSuccess(span)
reconcileOutcome("application", start, retErr)
```

vs `internal/app/application_controller.go:38,43,45,47,134`:

```go
ctx, span := shared.StartReconcileSpan(ctx, "application", req.Name, req.Namespace)
shared.RecordError(span, retErr)
shared.SetSuccess(span)
shared.ReconcileOutcome("application", start, retErr)
```

**Problema:** A versão "local" duplica funções que já existem em `internal/shared/tracing.go`. Adicionar uma 9ª chamada em `application_controller.go:133` exige decidir qual versão tocar. CLAUDE.md regra 12 (DRY): se eu mudar o helper, tenho que mudar nos dois lugares.

**Fix:** Eliminar `internal/controller/tracing.go` (existe!) e migrar para `internal/shared`. Coberto por S9-T1.

---

### AP-2: Hardcoded condition type strings em `application_controller.go:270`

**Onde:** `operators/internal/controller/application_controller.go:269-282` (e duplicado em `internal/app/application_controller.go`).

```go
for _, c := range app.Status.Conditions {
    if c.Type == "RouteReachable" && c.Status == "False" {
        switch c.Reason {
        case "Failed":   return loopzerov1.ApplicationPhaseDown
        case "Degraded": return loopzerov1.ApplicationPhaseDegraded
```

**Problema:** `"RouteReachable"`, `"False"`, `"Failed"`, `"Degraded"` são todos hardcoded. `conditions.TypeRouteReachable` existe (`internal/conditions/types.go:52`), `metav1.ConditionFalse` existe, `conditions.ReasonFailed` existe — mas `ReasonDegraded` NÃO existe. O código está usando uma string que não está modelada.

**Fix:**
1. Adicionar `ReasonDegraded = "Degraded"` em `internal/conditions/types.go`.
2. Trocar strings por constantes.
3. Considera trocar `app.Status.Conditions` traversal por `conditions.GetCondition(app.Status.Conditions, conditions.TypeRouteReachable)`.

---

### AP-3: Silent error swallow em `reconcileRouteStatus`

**Onde:** `operators/internal/controller/application_controller.go:179-181` e duplicado.

```go
if err := r.Get(ctx, routeKey, route); err != nil {
    return  // ← silently returns, no log, no condition update
}
```

**Problema:** Viola CLAUDE.md regra 8 (Error Handling — fail loud, fail clear). O reader sabe que algo deu errado mas o caller (`r.reconcileRouteStatus(ctx, &app)` em linha 88) não recebe sinal. App.Status.Reachable nunca atualiza, mas nenhum log explica por quê.

**Fix:**
```go
if err := r.Get(ctx, routeKey, route); err != nil {
    if !errors.IsNotFound(err) {
        log.FromContext(ctx).Error(err, "failed to read ExposedRoute for status sync",
            "route", routeKey)
    }
    conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
        metav1.ConditionUnknown, conditions.ReasonNotFound,
        "ExposedRoute not yet observable", app.Generation)
    return
}
```

---

### AP-4: Periodic poll competing com Watches em `ClusterCredentialReconciler`

**Onde:** `operators/internal/platform/clustercredential_controller.go:105`

```go
return ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
```

E ao mesmo tempo `cmd/main.go:375` watches Secrets. E ao mesmo tempo `K8sCellStore.LoadCells()` polls a cada 60s (per CLAUDE.md "Multi-Cell Routing").

**Problema:** Três paths independentes mantém connectivity status. Se `RequeuePeriodicCheck=5min` mas `K8sCellStore` poll é 60s, há janela de 4min onde o `Status.Connected` está stale relativo ao K8sCellStore. Para conditions agudas (cluster fica unreachable), o usuário verá divergência entre `loopzero admin tenants list` (lê K8sCellStore) e `kubectl get clustercredential` (lê CR status).

**Fix:** Padronizar em UM source of truth (a CR). `K8sCellStore` polls a CR (já é o caso? verificar) em vez de probar conectividade direto. Mata o `RequeuePeriodicCheck` na ClusterCredential — Watches são suficientes.

**Esforço:** M, requer auditoria do K8sCellStore semantics.

---

### AP-5: `cmd/main.go:104-118` cache scoping monolítico ≠ split

**Onde:** Compare `cmd/main.go:471-481` com `cmd/tenant-operator/main.go:114-119`.

Monolítico tem:
```go
&loopzerov1.Tenant{}:                {Namespaces: platformCRDNamespaces},
&loopzerov1.Environment{}:           {Namespaces: platformCRDNamespaces},
&loopzerov1.PlatformBootstrap{}:     {Namespaces: platformCRDNamespaces},
&loopzerov1.GitRepositoryBinding{}:  {Namespaces: platformCRDNamespaces},
&loopzerov1.ClusterCredential{}:     {Namespaces: platformCRDNamespaces},
```

Split tenant-operator tem:
```go
&loopzerov1.Tenant{}:      {Namespaces: platformCRDNamespaces},
&loopzerov1.Environment{}: {Namespaces: platformCRDNamespaces},
```

**Problema:** Quando alguém cria a CRD `NewPlatformCRD` e adiciona scope no monolítico, é fácil esquecer de adicionar ao split (que possivelmente nem reconcilia ela hoje). A árvore split assume que `platform-operator` cuida das outras 3 CRDs platform — verificável? Não há teste estrutural que verifica congruência cache scoping ↔ binary.

**Fix:** Test em `cmd/registration_test.go` matriz QW-4 que cada CRD platform aparece em exatamente UM cache scope across binários, e cada controller wired tem o cache correspondente.

---

## Sources

- [kubebuilder Good Practices](https://book.kubebuilder.io/reference/good-practices) — fundamento de OP-G01, OP-G03, OP-G14
- [kubebuilder Using Predicates](https://book.kubebuilder.io/reference/watching-resources/predicates-with-watch) — fundamento de OP-G17
- [controller-runtime godoc (sigs.k8s.io)](https://pkg.go.dev/sigs.k8s.io/controller-runtime) — semântica de cache split-client
- [kstatus README (cli-utils)](https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md) — fundamento de OP-G18
- [kpt CRD Status Convention](https://kpt.dev/reference/schema/crd-status-convention/) — Reconciling/Stalled types
- [Gateway API GEP-1364](https://gateway-api.sigs.k8s.io/geps/gep-1364/) — Status conventions canônicas
- [Operator SDK Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/) — fundamento de OP-G20
- [CloudNativePG Operator Capability Levels](https://cloudnative-pg.io/documentation/1.25/operator_capability_levels/) — exemplo concreto de declaração L5
- [Crossplane Compositions (v2.2)](https://docs.crossplane.io/latest/composition/compositions/) — XRD/composition pattern, CompositionRevisions
- [Crossplane v2 What's New](https://docs.crossplane.io/latest/whats-new/) — Operations pattern (PL-3)
- [Korifi Architecture (cloudfoundry)](https://github.com/cloudfoundry/korifi/blob/main/docs/architecture.md) — runtime abstraction CRDs (PL-2)
- [kubernetes-sigs/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) — fundamento de PL-1
- [admiraltyio/multicluster-controller](https://github.com/admiraltyio/multicluster-controller) — token rotation caveats em multi-cluster

---

## Appendix: Score Calculation Detail

20 dimensions × 5-point scale. LoopZero's current weighted score:

```
Sum of dimension scores: 68
Max possible:            100
Current %:               68%  → 6.8/10  (rounding to 7.0 generous, 6.0 strict)
Target post-Sprint-9:    85   → 8.5/10
```

The 8.5 target is achievable because **6 of the 9 lowest-scoring dimensions** (Predicate, kstatus, RBAC, Capability, Wiring completeness for split, Test matrix for split) are addressed by Quick Wins or Sprint 9 targets. The remaining 3 (Multi-cluster, Upgrade/API, Korifi-style abstraction) are post-launch.
