# Sprint 5 — PIPELINE

**Objetivo:** Builds previsíveis. Build failure resolvido. Cache validado. Queue visível.
**Confiança:** ALTA (investigação + validação)
**Esforço:** ~4h
**Pré-requisito:** GATE-SPRINT-4 passou (métricas necessárias para medir impacto)
**Rollback:** Resource limits são mudança de config, reversível.

---

## Tasks

### Task 5.1 — Investigar build failure (SIGTERM)

**Estado atual:** Workflow `loopzero-build-build-83375c3021faaeac-trtth` falhou. Step `install-deps` recebeu SIGTERM (exit 143). Outros steps completaram.

**Investigação (NÃO mudar antes de entender):**

```bash
# 1. Events do pod (OOMKilled? Evicted? DeadlineExceeded?)
kubectl --context <build> get pods -n build-tn-loopzero -l workflows.argoproj.io/workflow --show-all
kubectl --context <build> describe pod <pod-name> -n build-tn-loopzero

# 2. Resource allocation dos nodes
kubectl --context <build> describe nodes | grep -A 10 "Allocated resources"

# 3. Resource limits no CWT
cat infra/helm/argo-workflows/values.yaml | grep -A 20 resources

# 4. Workflow activeDeadlineSeconds
kubectl --context <build> get workflow <name> -n build-tn-loopzero -o yaml | grep -i deadline
```

**Diagnóstico esperado:**

| Causa | Evidência | Fix |
|---|---|---|
| OOM | Events: "OOMKilled" | Aumentar `resources.limits.memory` com margem 50% |
| Step timeout | Events: "DeadlineExceeded" | Aumentar `activeDeadlineSeconds` do step |
| Node pressure | Node conditions: MemoryPressure/DiskPressure | Upgrade node size ou adicionar node |

**Registro:**
- Causa raiz: _______________
- Fix aplicado: _______________

**Gate 5.1:**
- [ ] Causa raiz documentada
- [ ] Build subsequente SUCCEEDED
- [ ] 3 builds consecutivos sem SIGTERM

---

### Task 5.2 — Validar cache pipeline

**Ação:**
1. Build 1: projeto limpo → registrar duração total e de `install-deps`
2. Build 2: mesmo projeto, sem mudança de lockfile → registrar durações
3. Comparar

**Registro:**

| Métrica | Build 1 | Build 2 | Delta |
|---|---|---|---|
| Duração total | ___s | ___s | ___% |
| install-deps | ___s | ___s | ___% |
| cache-restore log | miss/hit | miss/hit | — |

**Gate 5.2:**
- [ ] Build 2 usa cache (log de cache-restore confirma hit)
- [ ] install-deps de build 2 reduzido >50% vs build 1

---

### Task 5.3 — Queue depth e fairness

**Ação:**
1. Verificar métrica `loopzero_build_queue_depth` (adicionada no Sprint 4) está emitindo
2. Verificar ResourceQuota no build namespace:
   ```bash
   kubectl --context <build> get resourcequota -n build-tn-loopzero
   ```
3. Verificar `maxBuildConcurrency` no Tenant CR:
   ```bash
   kubectl -n platform-system get tenant tn-loopzero -o jsonpath='{.spec.limits.maxBuildConcurrency}'
   ```
4. Teste de concorrência: 3 builds simultâneos → 1× 202, 2× 409

**Gate 5.3:**
- [ ] Queue depth visível no dashboard Grafana
- [ ] ResourceQuota ativo no build namespace
- [ ] maxBuildConcurrency enforced pela API
- [ ] 3 builds simultâneos → 1× 202 + 2× 409

---

## GATE-SPRINT-5

- [ ] Build failure investigado e resolvido
- [ ] Cache funcional (build 2 mais rápido)
- [ ] Queue depth visível no dashboard
- [ ] Concorrência enforced por tenant
- [ ] 3 deploys consecutivos estáveis
- [ ] Zero SIGTERM em builds

**SE QUALQUER ITEM FALHAR → NÃO AVANÇAR PARA SPRINT 6.**
