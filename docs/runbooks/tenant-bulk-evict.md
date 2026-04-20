# Runbook: Tenant Bulk Eviction

**Context:** T-02 em `docs/plans/50k-immediate-focus.md`. Operação
administrativa para remover tenants em bulk — cleanup pós-loadtest,
GC de tenants abandonados, ou correção de estado inconsistente.

Substitui o anti-pattern bash `kubectl delete --type=json replace finalizers=null`
por um pipeline auditado, transacional, e idempotente.

---

## Caminhos disponíveis (em ordem de preferência)

### 1. CLI (recomendado — produção)

```bash
export LOOPZERO_API_URL=https://api.dev.loopzero.example
export LOOPZERO_SERVICE_AUTH=$(kubectl --context mgmt -n platform-system \
  get secret loopzero-api-secrets -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d)

# Dry-run: lista quais tenants seriam evicted
loopzero admin tenants evict --pattern 'tn-lt-%' --dry-run

# Live: com confirmação interativa
loopzero admin tenants evict --pattern 'tn-lt-l3-%'

# Live: sem prompt (para scripts)
loopzero admin tenants evict --pattern 'tn-lt-l3-%' --yes

# Por IDs explícitos (sem pattern)
loopzero admin tenants evict --ids tn-abc,tn-def --yes
```

**Pré-requisitos:**
- API deployada com `LOOPZERO_ADMIN_API=true` no env
- API com code ≥ commit `ea04ba9` (K8s teardown phase 2)
- `LOOPZERO_SERVICE_AUTH_SECRET` no API matching `LOOPZERO_SERVICE_AUTH` do CLI

**Fluxo interno:**
1. CLI → `POST /api/v1/admin/tenants/evict` (dry-run) → lista matches
2. Prompt ou `--yes` → `POST /api/v1/admin/tenants/evict` (live)
3. API `TenantEvictor`:
   a. **K8s teardown primeiro** (mgmt + build + runtime clusters)
      - Delete Argo workflows (build cluster, grace=0)
      - Delete mgmt CRs: applications.argoproj.io, exposedroutes,
        loopzeroapplications, environments, tenants
      - Force-null finalizers nos stuck CRs (3 passes × 3s)
      - Force-finalize runtime namespaces stuck Terminating
   b. **DB cleanup depois** (FK-ordered, atomic):
      environment_targets → releases → build_apps → builds →
      projects → subscriptions → tenant_members → tenants
4. Invariant crítico: **se K8s falhar, DB NÃO é wiped** (test:
   `TestEnqueueEviction_K8sError_SkipsDBCleanup`)

**Exit codes:**
- `0` — sucesso ou dry-run listou matches
- `1` — erro de uso (flags faltando, pattern perigoso) ou prompt aborted
- `4` — auth error (`LOOPZERO_SERVICE_AUTH` ausente)
- `5` — partial failure (API retornou `accepted: false`)

### 2. Bash fallback (emergencial)

```bash
tools/admin-evict-tenants.sh --pattern 'tn-lt-%' --yes
tools/admin-evict-tenants.sh --pattern 'tn-lt-l3-%'
```

**Quando usar:**
- API está down (bash não depende do loopzero-api running)
- Debug — ver cada etapa acontecendo no stderr
- CI/GitOps scripts que precisam funcionar mesmo pre-API-deploy

**O que faz de diferente:**
- Mesma sequência (Argo wf → mgmt CRs → finalizer force-null → ns finalize → DB)
- Usa `kubectl` direto contra os 3 contexts (mgmt/build/runtime)
- Spawn de 1 pod `psql-evict-$(date +%s)` no mgmt para a transação DB

---

## Patterns perigosos (rejeitados pela API e pelo bash)

O endpoint valida e retorna `400 PATTERN_TOO_BROAD` para:
- `%` — match todos tenants incluindo produção
- `tn-%` — match todos tenants com prefixo padrão (idem)

Se precisar evict produção, enumere IDs explicitamente:

```bash
loopzero admin tenants evict --ids tn-abc-123,tn-def-456 --yes
```

Isso força reflection manual — não há atalho de "--force-all" no design.

---

## Monitoring durante eviction

```bash
# Watch mgmt CRs diminuindo
watch -n 2 "kubectl --context mgmt -n platform-system \
  get tenants,environments,loopzeroapplications,exposedroutes,applications.argoproj.io \
  --no-headers 2>/dev/null | grep 'tn-lt-' | wc -l"

# Watch runtime namespaces diminuindo
watch -n 2 "kubectl --context runtime get ns --no-headers 2>/dev/null \
  | grep 'loopzero-tn-lt-' | wc -l"

# Watch DB rows diminuindo
kubectl --context mgmt -n platform-system run psql-check --rm -i --restart=Never \
  --image=postgres:16-alpine -- \
  psql "$DB_URL" -c "SELECT COUNT(*) FROM tenants WHERE id LIKE 'tn-lt-%';"
```

---

## Pós-eviction — verificação

```bash
# Todos devem ser 0:
kubectl --context mgmt -n platform-system get tenants --no-headers | grep -c tn-lt
kubectl --context runtime get ns --no-headers | grep -c loopzero-tn-lt
kubectl --context mgmt get applications.argoproj.io -A --no-headers | grep -c tn-lt

# Se algum > 0: re-rodar eviction (idempotente — seguro repetir)
# Se re-run não limpa: verificar que finalizers não estão sendo re-added
# por algum controller zumbi.
```

---

## Troubleshooting

### `403 FORBIDDEN — Admin endpoint requires X-Service-Auth`

```bash
# Verifica que LOOPZERO_ADMIN_API está ligado no deployment
kubectl --context mgmt -n platform-system get deploy loopzero-api \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="LOOPZERO_ADMIN_API")].value}'

# Deve retornar "true". Se vazio:
kubectl --context mgmt -n platform-system set env deploy/loopzero-api LOOPZERO_ADMIN_API=true
kubectl --context mgmt -n platform-system rollout status deploy/loopzero-api

# Verifica que o secret matches:
# API lê LOOPZERO_SERVICE_AUTH_SECRET, CLI envia em X-Service-Auth header.
kubectl --context mgmt -n platform-system get secret loopzero-api-secrets \
  -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d
# Comparar com $LOOPZERO_SERVICE_AUTH do shell
```

### `404 NOT_FOUND — Endpoint not available`

API está running mas `LOOPZERO_ADMIN_API != "true"`. Feature flag fail-safe
(retorna 404 em vez de 403 para não vazar que o endpoint existe).

### CRs ficam stuck depois de múltiplas evictions

Algum controller está re-adicionando finalizers. Procurar:

```bash
kubectl --context mgmt -n platform-system get tenants -o json \
  | jq '.items[] | select(.metadata.finalizers != null) | .metadata.name'

# Logs do controller suspeito (tenant-controller):
kubectl --context mgmt -n platform-system logs -l app=tenant-controller --tail=100
```

### DB cleanup retorna erro mas CRs foram deletados

Invariant garante: se CR K8s falha, DB não roda. Mas o inverso (CR OK,
DB falha) deixa o sistema em estado inconsistente (CRs gone, rows
órfãs). A transação atômica de DB minimiza isso, mas se acontecer:

```bash
# Manualmente limpar rows órfãs (mesma SQL do DeleteTenantsFKOrdered):
# BEGIN; DELETE FROM environment_targets WHERE tenant_id LIKE ...;
#   ... (7 tabelas FK-ordered) ...
# COMMIT;
```

---

## Segurança e auditoria

- Todo eviction via API loga: (a) o pattern/ids recebidos, (b) quantos
  matches, (c) quem disparou (`X-Service-Auth` é service-level, não
  user — auditoria em log do API).
- Bash tool não loga centralmente — preferir CLI para cada produção.
- Feature flag `LOOPZERO_ADMIN_API` deve ficar OFF em clusters produtivos
  onde operadores usam bastion com bash. ON em staging para testar.

---

## Referências

- Plano: `docs/plans/50k-immediate-focus.md` (T-02)
- API endpoint: `api/internal/routes/admin_evict.go`
- Service (evictor): `api/internal/services/tenant_evictor.go`
- DB teardown: `api/internal/db/tenant_store.go` (`DeleteTenantsFKOrdered`)
- K8s teardown: `api/internal/k8s/tenant_evictor.go` (`EvictTenantsFromClusters`)
- CLI: `cli/src/commands/admin-evict.ts` + registered em `cli/src/index.ts`
- Bash fallback: `tools/admin-evict-tenants.sh`
- Commits-chave: `ea04ba9` (K8s phase 2), `e79b50e` (DB teardown), `4a9a077` (CLI)
