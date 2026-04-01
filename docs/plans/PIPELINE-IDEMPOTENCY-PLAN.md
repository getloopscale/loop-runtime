# Plano: Pipeline Idempotente

**Data:** 2026-04-01
**Objetivo:** `pulumi destroy --stack dev && pulumi up --stack dev` funciona sem intervenção manual
**Estimativa:** 2-3h

---

## Critério de sucesso

O seguinte script deve completar sem erros e sem intervenção humana:

```bash
cd infra/pulumi
pulumi destroy --stack dev --yes
pulumi up --stack dev --yes
# Todos os pods Running
# API health OK
# Gitea acessível
# loopzero deploy funciona end-to-end
```

---

## Task 1: DB Firewall no Pulumi (P0)

### Problema

A firewall rule do managed PostgreSQL foi adicionada manualmente via `doctl`.
Se a infra for destruída e recriada, o DB fica sem trusted sources e nenhum
pod consegue conectar (connection timeout na porta 25060).

### Arquivo

`infra/pulumi/src/providers/digitalocean/database.ts`

### Implementação

Após criar o `DatabaseCluster`, adicionar um `DatabaseFirewall` que permite
acesso dos 3 DOKS clusters:

```typescript
new digitalocean.DatabaseFirewall(`${spec.name}-firewall`, {
  clusterId: cluster.id,
  rules: spec.trustedK8sClusters.map(id => ({
    type: "k8s",
    value: id,
  })),
});
```

### Mudanças necessárias

1. **`contracts/database.ts`:** Adicionar `trustedK8sClusters: pulumi.Input<string>[]` ao `DatabaseClusterSpec`
2. **`providers/digitalocean/database.ts`:** Criar `DatabaseFirewall` após o cluster
3. **`components/infra.ts`:** Passar os cluster IDs ao spec do database

### DoD

- [ ] Teste: `pulumi preview` mostra `DatabaseFirewall` como recurso novo
- [ ] Teste: após `pulumi up`, `doctl databases firewalls list <id>` mostra 1+ regras
- [ ] Teste: novo pod API consegue conectar ao DB (init container completa)
- [ ] Teste: `pulumi destroy && pulumi up` → DB firewall recriada automaticamente
- [ ] Zero intervenção manual com `doctl`

---

## Task 2: Migration Auto-Recovery (P0)

### Problema

Se uma migration falha no meio (timeout, crash, connection lost), o golang-migrate
marca o DB como `dirty` na tabela `schema_migrations`. Todas as execuções
subsequentes falham com `Dirty database version N. Fix and force version.`

Resultado: TODOS os pods API ficam em `Init:CrashLoopBackOff` para sempre
até alguém rodar `UPDATE schema_migrations SET dirty = false` manualmente.

### Arquivo

`infra/pulumi/src/components/mgmt-bootstrap.ts` (init container do API deployment)

### Implementação

Substituir o init container simples por um wrapper script que:
1. Verifica se existe dirty flag
2. Se sim, faz `force` na versão atual (limpa dirty)
3. Então executa `up` normalmente

```yaml
initContainers:
  - name: db-migrate
    image: migrate/migrate:v4.18.1
    command: ["/bin/sh", "-c"]
    args:
      - |
        # Auto-recover from dirty state
        VERSION=$(migrate -path=/migrations -database "$DATABASE_URL" version 2>&1 || true)
        if echo "$VERSION" | grep -q "dirty"; then
          DIRTY_VER=$(echo "$VERSION" | grep -oP '\d+')
          echo "Dirty database detected at version $DIRTY_VER — forcing clean state"
          migrate -path=/migrations -database "$DATABASE_URL" force "$DIRTY_VER"
        fi
        # Run migrations normally
        migrate -path=/migrations -database "$DATABASE_URL" up
```

### DoD

- [ ] Teste local: marcar DB como dirty (`UPDATE schema_migrations SET dirty=true`) → restart API pod → migration auto-recovers
- [ ] Teste: init container logs mostram "Dirty database detected... forcing clean state" quando dirty
- [ ] Teste: init container logs mostram "no change" quando limpo (não-regressão)
- [ ] Teste: `pulumi destroy && pulumi up` → migration roda sem dirty error
- [ ] Zero intervenção manual com `psql`

---

## Task 3: Validação End-to-End (Gate)

### Problema

Não existe um teste automatizado que valide que a infra está funcional após deploy.
Os deploys passam no Pulumi mas pods crasham por config issues que só são
descobertos manualmente.

### Implementação

Adicionar um step no workflow `infra-deploy.yml` após o Pulumi `up` que verifica:

```yaml
- name: Validate deployment health
  run: |
    # Wait for pods to stabilize
    sleep 60
    
    # Check all deployments are ready
    for deploy in loopzero-api tenant-controller cloudflared grafana; do
      kubectl rollout status deployment/$deploy -n platform-system --timeout=300s
    done
    
    # Health checks
    kubectl exec deploy/loopzero-api -n platform-system -c api -- \
      wget -qO- --timeout=10 http://localhost:8080/api/v1/health
    
    kubectl exec deploy/tenant-controller -n platform-system -- \
      wget -qO- --timeout=10 http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/repos/search?limit=1
    
    echo "All health checks passed"
```

### DoD

- [ ] Step adicionado ao workflow `infra-deploy.yml` após Step 3
- [ ] Se qualquer health check falha, o workflow falha (exit 1)
- [ ] O step roda em menos de 5 minutos
- [ ] O step verifica: API health, Gitea connectivity, tenant-controller running, cloudflared running

---

## Task 4: Destroy + Recreate Test (Validação Final)

### Problema

Nunca foi testado um ciclo completo de destroy+recreate via pipeline.

### Implementação

Após Tasks 1-3, executar manualmente:

```bash
# 1. Destroy dev stack
cd infra/pulumi
pulumi destroy --stack dev --yes

# 2. Recreate from zero
pulumi up --stack dev --yes

# 3. Build images (registry é novo, sem images)
gh workflow run "Build: Container Images" --ref develop -f component=all

# 4. Re-deploy (agora com images)
pulumi up --stack dev --yes

# 5. Validate
kubectl get pods -n platform-system  # all Running
curl -sf https://api.dev.loopzero.example/api/v1/health  # 200
```

### DoD

- [ ] `pulumi destroy --stack dev --yes` completa sem erros
- [ ] `pulumi up --stack dev --yes` completa sem erros (primeira vez — sem images)
- [ ] Build images completa com sucesso
- [ ] Segundo `pulumi up` completa sem erros (pods pull images com sucesso)
- [ ] Todos os pods Running (0 CrashLoopBackOff, 0 ImagePullBackOff)
- [ ] API health: `{"status":"ok"}`
- [ ] Gitea: acessível pelo tenant-controller
- [ ] Cloudflared: tunnel connected
- [ ] Zero intervenção manual em todo o processo

---

## Sequência de Execução

```
Task 1: DB Firewall (30min)
    ↓ Gate: pulumi preview mostra DatabaseFirewall
Task 2: Migration Auto-Recovery (30min)
    ↓ Gate: dirty flag recovery funciona local
Task 3: Health Validation Step (30min)
    ↓ Gate: step adicionado ao workflow
Task 4: Destroy + Recreate (60min)
    ↓ Gate: ciclo completo sem intervenção manual
DONE: Pipeline idempotente ✅
```

**Tempo total: 2-3h**

---

## Itens já resolvidos nesta sessão

| Item | Status | Commit |
|------|--------|--------|
| NetworkPolicy allow-gitea | ✅ | `8bdfe2d` |
| NetworkPolicy allow-minio | ✅ | `8bdfe2d` |
| DB port 5432 → 25060 | ✅ | `4b62ab6` |
| CA cert double base64 | ✅ | `0d274cf` |
| Cloudflared metrics port | ✅ | `1c80820` |
| Registry retainOnDelete | ✅ | `469a8b4` |
| Traefik release name compat | ✅ | `a80eab1` |
| Kube context names | ✅ | `65938b4` |
| Grafana timeout | ✅ | `404c859` |
