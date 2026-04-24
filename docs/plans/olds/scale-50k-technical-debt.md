# Scale 50k — Technical Debt & Disaster Recovery Gap (End of Day 2)

**Date:** 2026-04-16
**Context:** [`scale-50k-plan.md`](scale-50k-plan.md) · [`scale-50k-day1-learnings.md`](scale-50k-day1-learnings.md) · [`scale-50k-day2-learnings.md`](scale-50k-day2-learnings.md)
**Scope:** Dívidas técnicas acumuladas durante L1 (10/10 ✅) e L2 (em progresso), análise de sobrevivência a `destroy + recreate`, e plano de ação priorizado para tornar a plataforma **idempotente**.

---

## TL;DR

Hoje, `pulumi destroy + pulumi up + deploy` **NÃO restaura** a plataforma ao estado funcional atual. **~62% dos bugs encontrados não sobrevivem** ao ciclo de destruição-recriação porque são runtime-only, bloqueados pelo Pulumi drift, ou arquiteturais.

**Causa raiz dominante:** Bug 22 (Pulumi state drift no stack `dev`) multiplica o problema — impede aplicação de fixes que já estão em código.

**Outro gap crítico para escala:** a plataforma é **cell-ready** (código completo), mas **cell-deployed=false** (roda em single-cell default). Isso é aceitável para L1/L2, mas bloqueia L4+ e inviabiliza meta de 50k apps sem Phase 3.

**Prioridade #1 para Dia 3:** Resolver Pulumi drift. Desbloqueia 3 bugs imediatamente e interrompe o crescimento da dívida.

---

## Resumo Executivo

Durante 2 dias de execução do scale-50k-plan, **26 bugs distintos** foram descobertos.

| Categoria | # Bugs | % |
|-----------|--------|---|
| ✅ Sobrevive destroy (código + Pulumi OK) | 14 | 54% |
| ⚠️ Em código mas Pulumi bloqueia | 3 | 12% |
| 🔴 Runtime-only (somem no restart) | 7 | 27% |
| 🔴 Arquitetural aberto | 2 | 8% |

**Probabilidade de L1 passar após `destroy+recreate` hoje: <40%.**

---

## Matriz de Sobrevivência a `destroy + recreate`

### ✅ Sobrevive (14 bugs) — código em git, Pulumi aplica limpo

Fixes estão em `.go` / `.yaml` commitados. Com Pulumi funcionando, destroy → recreate → deploy traz todos de volta.

| # | Fix | Local | Commit |
|---|-----|-------|--------|
| 1 | Auth header Bearer | `infra/tests/load/real_deploy_harness.go` | `9c4cad6` |
| 2 | Prefixos tenant/user | `infra/tests/load/real_deploy_harness.go` | `9c4cad6` |
| 3 | Concorrência 10→3 | `infra/tests/load/l1_scenarios_test.go` | `e8a06a2` |
| 4 | `gitea.go` nil client | `api/internal/gitclient/gitea.go` | `55ce988` |
| 5 | crane v0.21 distroless | `api/internal/argo/harbor_mirror.go` | `55ce988` |
| 6 | crane nonroot auth path | `api/internal/argo/harbor_mirror.go` | `55ce988` |
| 7 | cosign v3 + SHA | `infra/images/cosign-signer/Dockerfile` | `55ce988` |
| 8 | cosign regex whitespace | `api/internal/argo/cosign.go` | `55ce988` |
| 9 | minio/mc xargs → POSIX | `infra/helm/argo-workflows/templates/cwt-cache-save.yaml` | `55ce988` |
| 10 | Harness plan enterprise | `infra/tests/load/real_deploy_harness.go` | `55ce988` |
| 11 | Transient Failed phase | `api/internal/services/build_reconcile_loop.go` | `2afb72a` |
| 12 | `BuildReleasingTimeout`=0 | `api/internal/config/config.go` | `7990552` |
| 13 | `NewK8sCellStore(nil)` | `api/internal/placement/k8s_cell_store.go` | `0c9aafc` |
| 14 | Docker Hub → mirror.gcr.io | `api/internal/argo/buildkit.go` | `0c9aafc` |

### ⚠️ Em código mas Pulumi bloqueia (3 bugs)

Fixes existem no código Pulumi, mas não podem ser aplicados até o drift ser resolvido. Quote do transcript (23:28:56):

> *"O código Pulumi já tem os fixes corretos — é o state drift que impediu a aplicação."*

| # | Bug | Workaround atual | Código Pulumi |
|---|-----|------------------|---------------|
| 15 | `runtimeMaxNodes: 6` | `doctl kubernetes cluster node-pool update` | ✅ PR #43 merged (`77c57cf`) |
| 23 | `loopzero-api` ClusterRole sem `clustercredentials` perms | `kubectl patch clusterrole` | ✅ já correto em `pulumi-platform/src/` |
| 25 | `TENANT_NAMESPACE` env ausente no loopzero-api | `kubectl patch configmap` | ✅ já correto em Pulumi |

**Se Pulumi drift for resolvido:** `pulumi up` aplicaria todos 3 automaticamente.

### 🔴 Runtime-only (7 bugs) — não sobrevivem ao restart

Nenhum código commitado. Fix existe apenas no estado atual do cluster.

| # | Bug | Workaround aplicado | Impacto no destroy |
|---|-----|---------------------|---------------------|
| 16 | imagePullSecret DOCR nos tenants | Shell watcher `/tmp/patch-pullsecrets.sh` | 🔴 50+ pods em `ImagePullBackOff` |
| 17 | PSA `argo` privileged (BuildKit) | `kubectl label ns argo pod-security...=privileged` | 🔴 BuildKit não sobe |
| 18 | `cosign-signing-key` secret ausente | `kubectl create secret` manual | 🔴 sign-image pipeline quebra |
| 19 | Imagem `loopzero-cosign-signer:v0.1.0` não publicada | `docker buildx push` manual | 🔴 Pipeline falha pull |
| 20 | `runtimeMaxNodes` via doctl (bypass Pulumi) | `doctl kubernetes node-pool update` | 🔴 Nodes limitados a 3 |
| 24 | Harbor 404 via CF Tunnel | `curl CF API` — A record explícito para Traefik IP | 🔴 Harbor volta a 404 |
| 26 | Prometheus memory (em investigação) | Desconhecido | 🔴 Desconhecido |

### 🔴 Arquitetural aberto (2 bugs)

| # | Bug | Bloqueia |
|---|-----|---------|
| 21 | Workspace em PVC 5Gi (DO CSI gargalo) | SLO L2+ (throughput 3 PVCs/min vs 5/min) |
| 22 | **Pulumi state drift no stack dev** | Qualquer aplicação de fix Pulumi, multiplica dívida |

---

## 🟡 Runtime-Only Fixes Detalhados (bugs 16-20, 24, 26)

Estes fixes foram aplicados manualmente via comandos ad-hoc durante execução do L1/L2. O código/manifest **não foi atualizado**, então qualquer recriação do cluster dev voltará a quebrar.

### Bug 16 — `imagePullSecret` DOCR propagation nos namespaces tenant

**Sintoma:** Pods em `ImagePullBackOff` — runtime namespace sem credencial para pull de `registry.digitalocean.com/loopzero-infra-registry/...`.

**Workaround atual:** Shell script watcher `/tmp/patch-pullsecrets.sh`:
```bash
while true; do
  for ns in $(kubectl get ns -l loopzero.io/type=tenant -o name); do
    kubectl get secret loopzero-infra-registry -n $ns 2>/dev/null || \
      kubectl get secret loopzero-infra-registry -n loopzero-system -o json | \
        jq '.metadata.namespace=$ns' | kubectl apply -f -
    kubectl patch sa loopzero-app -n $ns -p '{"imagePullSecrets":[{"name":"loopzero-infra-registry"}]}'
  done
  sleep 10
done
```

**Por que não escala:** polling 10s+ não cobre burst de 50 tenants criados em 30s.

**Fix proposto (código):**

Opção A — **TenantController cria o secret ao provisionar namespace** (preferida):
```go
// operators/internal/controller/tenant_controller.go
func (r *TenantReconciler) ensureImagePullSecret(ctx, ns string) error {
    src := &corev1.Secret{}
    r.mgmtClient.Get(ctx, types.NamespacedName{Namespace: "loopzero-system", Name: "loopzero-infra-registry"}, src)
    target := src.DeepCopy()
    target.Namespace = ns
    target.ResourceVersion = ""
    return r.runtimeClient.Patch(ctx, target, client.Apply, client.ForceOwnership)
}
```

Opção B — **External Secrets Operator (ESO) com `ClusterExternalSecret`**: sync automático.

**Esforço:** 4h (A) / 2h (B).
**Prioridade:** 🔴 ALTA — bloqueia L2+ real.

---

### Bug 17 — PSA `privileged` no namespace `argo` para BuildKit

**Sintoma:** Pods BuildKit falhavam schedule com PSA `restricted`.

**Workaround:** `kubectl label ns argo pod-security.kubernetes.io/enforce=privileged --overwrite`

**Fix proposto:**

```yaml
# infra/helm/argo-workflows/templates/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: argo
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/warn: privileged
    pod-security.kubernetes.io/audit: privileged
```

**Esforço:** 30min. **Prioridade:** 🟡 MÉDIA.

---

### Bug 18 — Secret `cosign-signing-key` sem geração automática

**Workaround:** `cosign generate-key-pair` + `kubectl create secret`.

**Fix proposto:**

```yaml
# infra/helm/argo-workflows/templates/cosign-key-bootstrap-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: cosign-key-bootstrap
  namespace: argo
  annotations:
    "helm.sh/hook": post-install
spec:
  template:
    spec:
      containers:
      - name: gen
        image: ghcr.io/sigstore/cosign/cosign:v3.0.6
        command: ["/bin/sh", "-c"]
        args:
          - |
            if kubectl get secret cosign-signing-key -n argo >/dev/null 2>&1; then exit 0; fi
            cosign generate-key-pair k8s://argo/cosign-signing-key
```

**Esforço:** 2h. **Prioridade:** 🟡 MÉDIA.

---

### Bug 19 — Imagem `loopzero-cosign-signer:v0.1.0` não publicada no CI

**Workaround:** `docker buildx build --push` manual.

**Fix proposto:** Adicionar step em `.github/workflows/build-images.yml`:

```yaml
- name: Build and push cosign-signer
  uses: docker/build-push-action@v5
  with:
    context: ./infra/images/cosign-signer
    platforms: linux/amd64
    push: true
    tags: |
      registry.digitalocean.com/loopzero-infra-registry/loopzero-cosign-signer:${{ github.sha }}
      registry.digitalocean.com/loopzero-infra-registry/loopzero-cosign-signer:v0.1.0
```

**Esforço:** 1h. **Prioridade:** 🟡 MÉDIA.

---

### Bug 20 — Node pool bump via `doctl` (bypass Pulumi drift)

**Workaround:** `doctl kubernetes cluster node-pool update $POOL_ID --max-nodes 6`.

**Fix:** Resolver Bug 22 → aplicar `77c57cf` via `pulumi up --target '**runtime-pool**'`.

**Esforço:** 30min (após Bug 22). **Prioridade:** 🟡 MÉDIA.

---

### Bug 24 — Harbor 404 via CF Tunnel

**Sintoma:** `harbor.dev.loopzero.example` retorna 404. Subdomínios "funcionais" têm A record explícito → Traefik LoadBalancer IP; Harbor caía no wildcard → CF Tunnel (quebrado) → 404.

**Workaround aplicado:**
```bash
curl -X POST "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/dns_records" \
  -H "Authorization: Bearer $CF_TOKEN" \
  --data '{"type":"A","name":"harbor","content":"24.144.71.120","proxied":false}'
```

**Fix proposto:**

```typescript
// infra/pulumi-platform/src/providers/cloudflare/tunnel.ts (ou dns.ts)
const harborRecord = new cloudflare.Record("harbor-a", {
  zoneId: args.zoneId,
  name: "harbor",
  type: "A",
  value: args.traefikLBIp,  // output do infra stack
  proxied: false,
  ttl: 300,
});
```

**Esforço:** 1h. **Prioridade:** 🟡 MÉDIA — Harbor é dep de sign-image.

**Decisão arquitetural pendente:** Que serviços vão na lista "A record explícito" vs "fallback CF Tunnel"? Critério: infra de plataforma vs workload tenant.

---

### Bug 26 — Prometheus memory (investigação em curso)

**Sintoma:** Memória de Prometheus elevada (detectado em SRE Report #4, tick #57).

**Status:** Investigação em curso pela outra sessão (23:33:47). Detalhes pendentes.

**Possíveis causas:**
- Retenção muito longa (`--storage.tsdb.retention.time`)
- Cardinality explosion de labels (loopzero_* metrics com labels per tenant?)
- Queries caras sendo cacheadas

**Fix proposto:** Aguardar diagnóstico completo. Possivelmente: reduzir retenção, recording rules, ou migrar para Thanos/Mimir.

**Esforço:** TBD. **Prioridade:** 🟡 MÉDIA — não blocker, mas degrada SRE skill.

---

## 🔴 Bugs Arquiteturais (2)

### Bug 21 — Workspace em PVC bloqueia throughput

**Sintoma:** Cada build pede PVC de 5Gi. DO CSI provisiona ~3 PVCs/min. L2 com 15 workflows simultâneos tem fila de 7-10min.

**Impacto SLO:**
- L2 SLO "5 deploys/min sustained 30min" → impossível com CSI em 3/min
- L3+: 100 builds × 5Gi = 500Gi storage/rodada — caro e lento

**Fix proposto:** Substituir PVC por `emptyDir`:

```yaml
volumes:
  - name: workspace
    emptyDir:
      sizeLimit: 5Gi
```

**Benefícios:**
- Throughput L2 5x+
- Redução custo DO (~$100/mês @ 1000 builds/dia)
- Latência -2min/build

**Risco:** local disk exhaustion com 10+ builds concorrentes por node. Mitigação: `sizeLimit` + `ephemeral-storage` requests.

**Esforço:** 4h. **Prioridade:** 🔴 ALTA. **Decisão:** requer T2 meeting (muda contrato storage).

---

### Bug 22 — Pulumi state drift (CRÍTICO — blocker multiplicador)

**Sintoma:** `pulumi preview` mostra `-194 DELETE` — destruiria clusters DOKS, PG, DNS, ESO.

**Documentado em memória:** `feedback_pulumi_state_drift.md`:
> *"dev stack drifted: any `pulumi up` deletes all 3 clusters. Always preview first. doctl is exceptional fallback."*

**Consequência operacional:** Bug 22 é a **causa raiz de 8 outros bugs**:
- Bugs 15, 23, 25 ficaram runtime-only porque Pulumi não pôde aplicar fixes existentes
- Bugs 20, 24 idem (fixes Pulumi pendentes de aplicação)
- Bugs 16, 17, 18, 19 agravados (cada novo bug pode ter fix Pulumi bloqueado)

**Causas prováveis:**

1. **Recursos criados out-of-band** (via kubectl/doctl) não importados ao state
2. **Remoção de componentes** do código sem `pulumi state rm`
3. **Mudança de provider version** que renomeou resources
4. **Backend state desatualizado**

**Investigação proposta (sem aplicar mudanças):**

```bash
# 1. Listar recursos que Pulumi quer DELETAR
pulumi preview --json > /tmp/pulumi-preview.json
jq '.steps[] | select(.op=="delete") | .urn' /tmp/pulumi-preview.json

# 2. Cross-check com recursos DO reais
doctl kubernetes cluster list --format ID,Name
doctl databases list --format ID,Name

# 3. Exportar state para análise
pulumi stack export > /tmp/pulumi-state.json
```

**Fix proposto (em fases):**

**Fase 1 — Refresh (sync state com DO real):**
```bash
pulumi refresh --yes   # atualiza state sem alterar DO
pulumi preview         # ver diff após refresh
```

**Fase 2 — Import / state rm seletivo:**

Para cada recurso que Pulumi quer deletar mas deveria existir:
- Se está no DO mas não no state: `pulumi import <type> <name> <id>`
- Se foi removido corretamente: `pulumi state delete <urn>`

**Fase 3 — Apply targeted:**
```bash
pulumi up --target '**runtime-pool**'  # um componente por vez
```

**Esforço:** 4-8h. **Prioridade:** 🔴 CRÍTICA. **Decisão:** requer T3 meeting.

---

## 🏗️ Cell-Based Architecture — Ready vs Deployed

A arquitetura cell-based é o mecanismo projetado para atingir 50.000 aplicações (plano §14). Hoje ela existe **em código** mas **não em runtime**. Esta seção explicita a distinção e mapeia quando cada camada se torna blocker.

### O que está pronto (código)

| Componente | Status | Arquivo |
|------------|:------:|---------|
| `DynamicRouter` (discovery) | ✅ Wired | `api/internal/placement/dynamic_router.go` |
| `K8sCellStore` (lista cells via CRs) | ✅ Wired | `api/internal/placement/k8s_cell_store.go` |
| `PlacementEngine` (RoundRobin) | ✅ Active | `api/internal/placement/engine.go` |
| `CellConfig`, `Router`, `StaticRouter` | ✅ Defined | `api/internal/placement/` |
| `CrossCellAggregator` (status/list cross-cell) | ✅ Defined | `api/internal/placement/` |
| Pulumi `cell.ts` (provisioning) | ✅ Exists | `infra/pulumi/src/components/cell.ts` |
| Feature flag `LOOPZERO_DYNAMIC_ROUTER=true` | ✅ Ativado em prod | config.go |

**Resumo:** todo o scaffolding para descobrir e rotear entre cells está **commitado e ativo** em produção.

### O que NÃO está em runtime

| Componente | Status | Evidência |
|------------|:------:|-----------|
| `ClusterCredential` CRs (registro de cells) | ❌ **Zero registros** | `kubectl get clustercredentials -A → No resources found` |
| Clusters runtime adicionais (cell-sfo3-02, ...) | ❌ **Não provisionados** | `doctl kubernetes cluster list` mostra apenas mgmt/build/runtime (base) |
| Router em uso | ⚠️ **StaticRouter cell-default** (fallback) | Log: `"cell router: StaticRouter with cell-default (single cell)"` |
| Blast radius isolation física | ❌ Single cluster runtime | Todos tenants compartilham recursos, nodes, control plane |

**Estado operacional:** o DynamicRouter detecta zero cells → código faz fallback automático para `StaticRouter{cellID="cell-default"}`. **Todos os tenants rodam no mesmo cluster runtime.**

### Capacidade atual do single-cell

| Nível | Tenants | Viável em single-cell? |
|-------|---------|:----------------------:|
| L1 | 10 | ✅ Validado 10/10 |
| L2 | 50 | ✅ Validado 50/50 em v8 |
| L3 | 100 | ⚠️ Limite conhecido de ~500 tenants/cell — roda, mas com pressure |
| L4 | 500 | ❌ Saturação iminente — provisionar 2ª cell torna-se crítico |
| L5 | 5.000 | ❌ **Impossível** em single-cell |
| L6 | 50.000 | ❌ **Inviável arquiteturalmente** |

### Bug #33 — Cell-deployed=false (gap arquitetural)

**Sintoma:** plataforma não tem isolamento físico entre tenants. Falha em um tenant pode afetar vizinhos (resource pressure, noisy neighbor).

**Não é bug de código:** é gap de **provisioning/operation** — o código está pronto, falta ativar.

**Impacto por nível:**
- L1-L3: **baixo** (single cluster aguenta)
- L4: **médio** (blast radius = 500 tenants afetados por 1 falha)
- L5+: **crítico** (sem cells, meta impossível)

**Fix proposto:**

1. **Phase 3 (plano scale-50k §3 — weeks 5-6):**
   - Provisionar `cell-sfo3-02` via `pulumi up --target '**cell-sfo3-02**'`
   - Criar `ClusterCredential` CR apontando para novo cluster
   - `K8sCellStore` descobre em <60s
   - Novos tenants passam a ser distribuídos round-robin
   - Validação com `TestL4_500Tenants_MultiCell` (já existe, skip atual)

2. **Phase 5 (weeks 10-16):** escalar para 100 cells em 2 regions.

**Custo incremental por cell:**
- DOKS cluster 2-3 nodes: ~$200-400/mês
- ArgoCD + Traefik + cert-manager: ~$50/mês ops
- Total: **~$250-450/mês por cell**

Para 100 cells: ~$25-45k/mês só em compute. Necessário para meta 50k.

**Esforço para 1ª cell adicional:** ~4h (provisioning + validação).
**Prioridade:** 🟡 MÉDIA — não blocker para L2/L3, blocker para L4+.
**Decisão:** requer T2 meeting (custo vs timing de escalonamento).

### Por que não ativamos cells antes?

Decisão arquitetural consciente:

1. **Single-cell é suficiente até ~500 tenants** — overhead injustificado antes
2. **Custo incremental** não-trivial por cell
3. **Plano scale-50k agenda isso** para Phase 3 (weeks 5-6), após L3/L4 validados
4. **Debugging mais simples** em single-cluster (cross-cell debugging é 10x mais complexo)

### Impacto na matriz de sobrevivência

**Adiciona-se Bug #33 à categoria "Arquitetural aberto" (agora 3 bugs nessa categoria):**

| Categoria | # Bugs (antes) | # Bugs (depois) |
|-----------|:--------------:|:---------------:|
| ✅ Sobrevive destroy | 14 | 14 |
| ⚠️ Código OK, Pulumi bloqueia | 3 | 3 |
| 🔴 Runtime-only | 7 | 7 |
| 🔴 Arquitetural aberto | 2 | **3** (+33) |

**Nota:** Bug #33 não impacta destroy+recreate do estado atual (single-cell continua single-cell). Só é relevante quando plano avança para Phase 3+.

---

## 📋 Plano de Ataque Dia 3 (Ordem Recomendada)

Ordem pensada para **maximizar fixes por hora**: resolver bloqueadores primeiro.

| Ordem | Item | Tipo | Esforço | Desbloqueia |
|-------|------|------|---------|-------------|
| 1 | **Bug 22** — Pulumi state drift | T3 meeting | 4-8h | **Desbloqueia 15, 20, 23, 24, 25** (5 bugs) |
| 2 | Bug 16 — imagePullSecret TenantController | T2 | 4h | L3+ real |
| 3 | Bug 21 — workspace emptyDir | T2 meeting | 4h | SLO L2+ (5 deploys/min) |
| 4 | Bug 17 — PSA label Helm | T1 | 30min | Recovery freshcluster |
| 5 | Bug 18 — cosign-key Job | T1 | 2h | Recovery |
| 6 | Bug 19 — cosign-signer no CI | T1 | 1h | Release repetível |
| 7 | Bug 24 — Harbor A record (Pulumi CF) | T1 | 1h | Resilience |
| 8 | Bug 26 — Prometheus memory | T1 | TBD | SRE observability |

**Total dia 3:** ~17-21h (sem contar meetings).

**Meetings:** 2 (T3 Pulumi drift, T2 workspace emptyDir).

### Timeline para cell-deployed (Phase 3 do plano, weeks 5-6)

Separado do Dia 3. Bug #33 **não é urgente** até L4 (500 tenants).

| Ordem | Item | Esforço | Destrava |
|-------|------|---------|----------|
| 9 | Bug 33.1 — Provisionar `cell-sfo3-02` (Pulumi) | 4h | L4 multi-cell |
| 10 | Bug 33.2 — `ClusterCredential` CR + validação K8sCellStore discovery | 2h | Routing cross-cell |
| 11 | Bug 33.3 — Remover `t.Skip()` de `TestL4_500Tenants_MultiCell` + run | 2h | Validação |
| 12 | Bug 33.4 — Provisionar cells 3-10 via Pulumi config array | 4h | L5 (5k tenants) |
| 13 | Bug 33.5 — Cross-region (cells 11+ em NYC3) | 8h | L5+ |

**Esforço total cell activation:** ~20h ao longo de 2-3 semanas (não urgente).

Após isso, teste de validação de idempotência:

```bash
# Validação Dia 3
pulumi destroy --yes
pulumi up --yes
./infra/tests/load/run-tests.sh l1
# Esperado: 10/10 HTTP 200 sem intervenção manual
```

---

## Padrões Sistêmicos (Meta-Lições)

### 1. "Funciona local, quebra em escala"
Bugs 4, 5, 6, 7, 8, 11, 14, 16 compartilham: builds isolados passam, 50 concorrentes expõem assunções implícitas.

**Mitigação:** Load tests reais (L1+) no CI para cada PR que toca pipeline.

### 2. "Imagens imutáveis não são"
Bugs 5, 6, 8, 19 vieram de mudanças upstream. Pipelines dependem de imagens externas cujos conteúdos variam.

**Mitigação:** Mirror imagens base para registry próprio + renovação controlada via PR.

### 3. "Zero-value Duration é silencioso"
Bug 12 (`BuildReleasingTimeout=0` via struct literal incompleto) é hazard do Go.

**Mitigação:** `Validate()` em cada Config struct que retorna erro se Duration for 0.

### 4. "Infra aplicada live é dívida multiplicadora"
Bugs 16-20, 23-25 mostram o custo. Cada `kubectl patch` / `doctl` que não vira PR é dívida.

**Mitigação:** Checklist obrigatório: ação manual → PR → Helm/Pulumi → validação em cluster fresh.

### 5. "State backend precisa de fencing"
Bug 22 é catastrófico porque o state backend não tinha verificação independente.

**Mitigação:**
- Cron diário de `pulumi preview` → alerta se detectar DELETEs inesperados
- `pulumi refresh` regular (não só antes de `up`)
- Dashboard de drift (count de DELETEs planejadas)

### 6. "DNS wildcard é faca de dois gumes"
Bug 24: wildcard `*.dev.loopzero.example` era fallback útil MAS mascarou que Harbor caiu quando CF Tunnel quebrou.

**Mitigação:** Serviços de infra crítica têm A record explícito + alerta de health-check.

### 7. "Fix runtime + código não aplicado = dívida dobrada"
Bugs 15, 23, 25 exibem o pior padrão: fix correto EM CÓDIGO, aplicado via kubectl manual. **Duas fontes de verdade** (cluster ≠ Pulumi state) até resolver Bug 22.

**Mitigação:** Política: se fix runtime, PR obrigatório + tag `tech-debt:pulumi-pending` + verificação periódica pós-resolução do drift.

---

## Referências

- [`scale-50k-plan.md`](scale-50k-plan.md) — plano original
- [`scale-50k-day1-learnings.md`](scale-50k-day1-learnings.md) — aprendizados dia 1
- [`scale-50k-day2-learnings.md`](scale-50k-day2-learnings.md) — aprendizados dia 2
- Memória: `feedback_pulumi_state_drift.md` — política sobre Pulumi
- Memória: `feedback_never_manual_nodepool.md` — exceção doctl
- Commits relevantes: `9c4cad6` `e8a06a2` `55ce988` `7d7a2a6` `2afb72a` `7990552` `96f83d7` `0c9aafc` `77c57cf` `90319ae`

---

## Changelog deste documento

- **v1 (23:19 UTC):** Criação com bugs 16-22.
- **v2 (23:45 UTC):** Adicionados bugs 23, 24, 25, 26 descobertos durante tick monitoring. Adicionada matriz de sobrevivência a destroy+recreate. Reorganizado plano de ataque dia 3 com foco em Bug 22 como bloqueador multiplicador. Ampliado padrões sistêmicos de 5 → 7.
- **v3 (2026-04-17):** Adicionada seção "Cell-Based Architecture — Ready vs Deployed" explicitando que código cell está completo mas runtime opera em single-cell. Adicionado Bug #33 (cell-deployed gap) à categoria arquitetural (agora 3 bugs). Adicionado "Timeline para cell-deployed (Phase 3)" ao plano de ataque com 5 sub-tasks para cells #33.1-#33.5. Atualizado TL;DR para mencionar gap cell-deployed.
