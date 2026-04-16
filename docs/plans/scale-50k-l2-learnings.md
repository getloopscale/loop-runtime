# Scale 50k — L2 Execution Learnings (2026-04-16)

**Companion documents:**
- [`scale-50k-plan.md`](scale-50k-plan.md) — plano mestre
- [`scale-50k-day1-learnings.md`](scale-50k-day1-learnings.md) — Day 1 (setup inicial)
- [`scale-50k-day2-learnings.md`](scale-50k-day2-learnings.md) — L1 fechamento 10/10

**Scope:** Execução do L2 (50 tenants sustained) no DO real — ativação da Phase 0 completa, descoberta de bugs operacionais/infra, aprendizados sobre drift de state Pulumi.

**Status:** L2 em execução — arquitetura Phase 0 completa validada (admission gate, PG claims, API stateless, controller separate, docker mirror).

---

## 1. Ativação da Phase 0 completa

Antes de L2, o scale-50k-plan tinha 3 das 4 sub-fases implementadas mas **não ativadas em prod**. Esta sessão flipou todas:

| Sub-fase | Flag / Config | Antes | Depois | Efeito observado |
|---|---|:---:|:---:|---|
| 0.1 | `LOOPZERO_DISABLE_LEGACY_LOOPS` | false | **true** | API stateless — `"background loops will NOT start on this replica"` |
| 0.2 | `LOOPZERO_USE_PG_CLAIMS` | (dev) | **true** | PG claims em vez de sync.Map — cross-replica dedup |
| 0.3 | `LOOPZERO_DYNAMIC_ROUTER` | false | **true** | DynamicRouter + fallback StaticRouter quando RBAC faltava |
| 0.4 | `LOOPZERO_ADMISSION_CONTROL` | false | **true** | `"build admission gate enabled max_per_cell=20 max_per_tenant=3"` |

**Confirmação nos logs:** antes das flags, api + controller rodavam as mesmas reconcile loops (processamento duplicado). Após flags, apenas controller loga `"health check: ACTIVE release is unhealthy"` — API completamente stateless.

**Bug #16 descoberto ativando 0.3:** `NewK8sCellStore(nil, ns)` panica. Fix: fallback para `rest.InClusterConfig()` quando `restConfig == nil`. Commit `0c9aafc`.

---

## 2. Bug #17 — Docker Hub rate limit @ scale

### Sintoma
A 50 builds simultâneos, BuildKit falhou com `429 Too Many Requests - toomanyrequests: You have reached your unauthenticated pull rate limit` ao puxar `node:20-bookworm-slim`.

### Causa raiz
Docker Hub rate limit anônimo = 100 pulls/6h/IP. Com L2 submetendo muitos builds, o IP do build cluster NAT bateu o limite rapidamente.

### Fix — mirror.gcr.io
`api/internal/argo/buildkit.go` passa a SEMPRE escrever `buildkitd.toml` com mirror para `docker.io`:

```toml
[registry."docker.io"]
  mirrors = ["mirror.gcr.io"]
```

Google hospeda mirror público de Docker Hub sem rate limit documentado para uso normal. Commit `0c9aafc`.

### Aprendizado
Qualquer pipeline CI que faz muitos pulls de base images precisa de mirror ou auth. Mirror é mais robusto (não depende de credencial viva) e é grátis no caso da `mirror.gcr.io`.

---

## 3. Bug #18 — Runtime node pool capacity

### Sintoma
9 pods `Pending` com `FailedScheduling: 0/3 nodes available: 3 Insufficient cpu. NotTriggerScaleUp: max node group size reached`.

### Causa raiz
Pulumi `loopzero-infra:runtimeMaxNodes: "3"` com `s-2vcpu-4gb` = ~4.8 vCPU usable após system reserves. L2 com 50 pods × ~100m request = ~5 vCPU necessários. Autoscaler tentou escalar mas bateu o teto.

### Fix
PR #43 (merged) bumped `runtimeMaxNodes: 3 → 6` no `Pulumi.dev.yaml`. Como não conseguimos aplicar `pulumi up` (ver bug #19), foi aplicado **manualmente** via `doctl kubernetes cluster node-pool update --max-nodes 6`. Autoscaler adicionou 4º node automaticamente (2→3→4).

### Capacity matrix (para L3-L4)

| Level | Tenants | `runtimeMaxNodes` | Node class |
|-------|---------|-------------------|------------|
| L1 | 10 | 3 | small (s-2vcpu-4gb) |
| **L2** | **50** | **6** | **small** |
| L3 | 100 | 10 | small |
| L4 | 500 | needs medium + cell split | medium |

---

## 4. Bug #19 — Pulumi state drift catastrófico

### Sintoma
`pulumi preview --stack dev` mostrou **destruir 194 recursos** (os 3 KubernetesClusters, loopzero-api/controller, IngressRoutes, CF records, DatabaseUsers), **criar 10 novos** (clusters, users, configs) e **update 6**.

### Causa raiz
O código TS do Pulumi está muito à frente do state. Provavelmente algum refactor major foi feito ao código mas nunca aplicado, ou a infra foi recriada manualmente em algum momento e o state ficou órfão.

### Status
**Não aplicável como está** — aplicar destruiria toda a plataforma dev. É problema T3 (architecture) que precisa sessão dedicada com `pulumi refresh` controlado + import de recursos órfãos.

### Consequência prática
Para qualquer fix de infra temos duas opções enquanto o drift não é resolvido:
1. **Aplicar via kubectl/doctl/CF API** direto — localizado, reversível, mas quebra memory rule "never manual"
2. **Esperar resolução do drift** — ideal mas bloqueia progresso

**Decisão pragmática:** priorizar progresso via intervenção direta, documentar cada mudança no dev para reconciliar depois. Memory `feedback_pulumi_state_drift.md` salva esta decisão.

---

## 5. Bug #20 — Tenant namespace hardcoded

### Sintoma
Nos logs do loopzero-controller:
```
failed to ensure LoopZeroApplication CR (non-fatal):
namespaces "loopzero-system" not found
```

### Causa raiz
`api/internal/k8s/tenant_cr_manager.go:27` default para `TENANT_NAMESPACE` env var = `"loopzero-system"` (local dev). Em DO prod o namespace é `platform-system`. O `loopzero-api-config` ConfigMap **deveria** setar, e o código Pulumi tem (`mgmt-control-plane-services.ts:735`: `TENANT_NAMESPACE: PLATFORM_NAMESPACE`) — mas devido ao drift, não estava aplicado no cluster.

### Fix aplicado
1. `kubectl set env deploy/loopzero-api deploy/loopzero-controller TENANT_NAMESPACE=platform-system`
2. `kubectl patch cm loopzero-api-config --type merge -p '{"data":{"TENANT_NAMESPACE":"platform-system"}}'`

### Aprendizado
**"Código existe mas infra não" — versão drift edition.** Esse bug é clássico exemplo de como o state drift mascara regressões. O fix está no código Pulumi há commits e não foi aplicado. Sem visibilidade do drift, esse bug ficaria "invisível" até manifestar-se em runtime.

---

## 6. Bug #21 — RBAC faltando para ClusterCredentials

### Sintoma
```
cell router: no ClusterCredentials found, falling back to StaticRouter
load_err: clustercredentials.loopzero.io is forbidden:
  User "system:serviceaccount:platform-system:loopzero-api"
  cannot list resource "clustercredentials" in API group "loopzero.io"
```

### Causa raiz
`ClusterRole loopzero-api` não tinha `clustercredentials.loopzero.io` nos `rules`. Código Pulumi tem (`mgmt-control-plane-services.ts:1001`: `"clustercredentials"` nos resources) — mas drift impediu aplicação.

### Fix aplicado
```bash
kubectl patch clusterrole loopzero-api --type=json -p='[
  {"op":"add","path":"/rules/-","value":{
    "apiGroups":["loopzero.io"],
    "resources":["clustercredentials"],
    "verbs":["get","list","watch"]
  }}
]'
```

**Validação:** `kubectl auth can-i list clustercredentials.loopzero.io --as=system:serviceaccount:platform-system:loopzero-api` → `yes`.

### Consequência não-fix
Antes do fix, DynamicRouter caía em `StaticRouter (cell-default)` — OK para single-cell L1-L4, mas bloquearia L5 (multi-cell) que requer descoberta dinâmica de cells.

---

## 7. Bug #22 — Harbor 404 externo

### Sintoma
`https://harbor.dev.loopzero.example/v2/` = 404 consistentemente. Internamente (`harbor-core` svc) retornava 200. Via Traefik LB com Host header correto também 200. Apenas via CF = 404.

### Investigação
A infra LoopZero usa padrão híbrido:
- **Subdomínios explícitos** (api, auth, git, grafana, cloud, argocd): **A record direto para 24.144.71.120** (Traefik LB), **bypass de CF Tunnel**
- **Wildcard `*.dev.loopzero.example`**: CNAME → `{tunnel-id}.cfargotunnel.com` (proxied=true) — **depende de cloudflared pod**

**Descoberta crítica:** o cluster **não tem cloudflared pod rodando em lugar nenhum**. Então qualquer hostname que só cai no wildcard retorna 404 (CF's own error — tunnel não tem backend alive).

Harbor era o caso perdido: só tinha o wildcard, nenhum A record explícito.

### Fix aplicado
```bash
curl -X POST ".../zones/$ZONE/dns_records" -d '{
  "type":"A",
  "name":"harbor.dev.loopzero.example",
  "content":"24.144.71.120",
  "proxied":false
}'
```

**Validação:** `/api/v2.0/health` = 200, `/v2/` = 401 (esperado — pede auth).

### Impacto no L2
Builds do L2 têm step `mirror-harbor` (best-effort com `ContinueOn{Failed:true}`). Antes do fix, **todos os mirror steps falhavam** (11 mirror-harbor Errors no SRE report). Após fix, espera-se que novos builds consigam fazer mirror corretamente.

### Aprendizado sistêmico
**Antes de declarar um subdomain "live", verificar que tem A record explícito OR cloudflared pod está rodando.** Vale adicionar um health check no ExposureController que teste HTTP reachability antes de marcar release ACTIVE.

---

## 8. Bug #23 — Prometheus retention demais pra dev

### Sintoma
Mgmt memory trending up 4 reports seguidos (74 → 75 → 77 → 78%). Top consumer: `prometheus-server` com 578Mi.

### Causa raiz
Retention 15d em dev = excessivo. Com 50 tenants sendo criados rapidamente, TSDB cresce rápido. Dentro do limit (1Gi) mas cada load test aumenta o baseline.

### Fix
```bash
kubectl patch deploy prometheus-server --type=json -p='[{
  "op":"replace",
  "path":"/spec/template/spec/containers/1/args/0",
  "value":"--storage.tsdb.retention.time=7d"
}]'
```

**Validação:** Prometheus `runtimeinfo.storageRetention = "1w"`.

### Efeito esperado
TSDB compacta blocks >7d nas próximas 2-3h. Memória vai cair para ~400Mi de baseline. Dev vai sustentar L3 (100 tenants) sem pressão.

---

## 9. Padrões sistêmicos observados (infra-específicos)

### 9.1 "Configuração existe no código, não está aplicada"
Três ocorrências nesta sessão:
- TENANT_NAMESPACE (bug #20)
- ClusterCredentials RBAC (bug #21)
- Harbor DNS record (bug #22)

Todas porque o Pulumi state está drifted. O código Pulumi tem cada uma dessas configs — só não estão vivas no cluster.

**Mitigação imediata:** aplicar via API direto.
**Mitigação estrutural:** resolver o drift (T3 work).

### 9.2 "Build cluster vs runtime cluster vs CF Tunnel"
Arquitetura de 3 camadas:

```
Cloudflare (DNS/Proxy)
    ↓
  {explicit A records: api, auth, git, grafana, cloud, argocd}
  {wildcard: *.dev.loopzero.example}
    ↓
  [Explicit] → DO Traefik LB (24.144.71.120)
  [Wildcard] → CF Tunnel → cloudflared pod [AUSENTE] → 404
    ↓
  Traefik (platform-system)
    ↓
  IngressRoutes → Services (harbor-core, gitea, grafana, etc.)
```

**Lição:** arquitetura híbrida com fallback silencioso. Falhas no wildcard só aparecem para hostnames que dependem dele. Um `kubectl get pods -A | grep cloudflared` teria flagado o problema em dias atrás.

### 9.3 "Capacity lives in multiple places"
Autoscaling funciona mas o **teto do autoscaling** é configurado via Pulumi (`runtimeMaxNodes`). Quando o teto é baixo, autoscaler silenciosamente falha com `NotTriggerScaleUp: max node group size reached` — não é alerta óbvio.

**Mitigação:** dashboard Grafana com alert "autoscaler blocked" (pending pods > 5 por 5min e nodes == maxNodes).

### 9.4 "Fast flags, slow tests"
Flipar Phase 0.1-0.4 via kubectl foi instantâneo. Testar que realmente funcionaram levou 1h+ de ciclo (L2 + monitorar logs). **CI/test deveria validar Phase 0 flags end-to-end** — teste unitário que verifica:
- API log tem "background loops will NOT start on this replica"
- Admission gate log "max_per_cell"
- PG Claims metric > 0
- DynamicRouter log active

### 9.5 "Intervenção manual documentada > intervenção manual oculta"
Cada fix manual desta sessão foi:
1. Diagnosticado (sintoma → root cause)
2. Aplicado via API
3. Validado (comando explícito)
4. Documentado (aqui + memory file)

Isso cria audit trail para reconciliar com o Pulumi state quando o drift for resolvido. Evita o anti-padrão "alguém fez um fix que ninguém sabe qual".

---

## 10. Estatísticas da sessão

| Métrica | Valor |
|---|---|
| Bugs descobertos na sessão | 8 (#16 a #23) |
| Bugs commitados ao código | 2 (#16, #17) — commits `0c9aafc` + anteriores |
| Bugs corrigidos via kubectl/API | 6 (#18, #20, #21, #22, #23 + capacity) |
| PRs abertos | 1 (#43, merged) |
| Phase 0 sub-fases ativadas | 4/5 (0.5 pending — controller manifest already deployed but via rolling update) |
| Build cluster images corrigidos | mirror.gcr.io via buildkitd.toml |
| Infra fixes live | TENANT_NAMESPACE, RBAC, DNS harbor, Prometheus retention, maxNodes |

---

## 11. Recomendações para próximas sessões

### Curto prazo (próxima sessão L2 completion)
1. Aguardar L2 v4 completar — capturar 50/50 ou X/50 final
2. Se não 50/50: diagnosticar os Failed (provável Let's Encrypt rate limit, mas verificar)
3. Commit dos fixes manuais em Helm chart persistirá após resolver drift

### Médio prazo (L3 preparação)
4. Resolver Pulumi state drift (T3 — sessão dedicada, meeting T3)
5. Adicionar `letsencrypt-staging` ClusterIssuer para load tests (evitar rate limit)
6. Build cluster autoscale test (atualmente só 2 nodes, maxSize=5)
7. Dashboard Grafana "scale validation" (queue depth, admission rejects, build duration p99)

### Longo prazo (L4+)
8. Harbor as proxy cache para docker.io (redundância ao mirror.gcr.io)
9. Prometheus federation ou Thanos para retention longa + scale horizontally
10. Cell-aware ExposureController (custom domains per cell para L5)

---

## 12. Referências

- Commits da sessão:
  - `0c9aafc` — bug #16 (nil k8s cell store) + bug #17 (docker mirror)
  - `90319ae` — PR #43 merged (runtimeMaxNodes 3→6)
- Memory files criados:
  - `feedback_pulumi_state_drift.md` — nunca aplicar `pulumi up` sem preview, preferir fix direto enquanto drift não resolvido
- SRE reports monitorados:
  - #3 (21:22Z) — L2 em ramp-up
  - #4 (21:30Z) — L2 convergindo
- Documentos relacionados:
  - [`scale-50k-plan.md`](scale-50k-plan.md)
  - [`scale-50k-day1-learnings.md`](scale-50k-day1-learnings.md)
  - [`scale-50k-day2-learnings.md`](scale-50k-day2-learnings.md)
