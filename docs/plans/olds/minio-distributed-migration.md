# MinIO Standalone → Distributed Migration Plan

**Date:** 2026-04-17
**Context:** Bug #35 (cache-restore timeout) descoberto em L3 revelou que MinIO `standalone` satura com 100 tenants concorrentes. Este plano migra para `distributed mode` com erasure coding, em 3 fases incrementais.
**Companion docs:** [`scale-50k-technical-debt.md`](scale-50k-technical-debt.md) · [`scale-50k-l3-readiness.md`](scale-50k-l3-readiness.md)

---

## Estado atual (baseline)

```yaml
# infra/helm/minio/values.yaml
mode: standalone          # ← SPOF
resources.limits.memory: 512Mi   # ← pequeno
resources.limits.cpu: 500m       # ← pequeno
persistence.size: 10Gi

Single pod: platform-system/minio-695f68c677-w4nk9
PVC único: 10Gi do-block-storage
Buckets: loopzero-artifacts, loopzero-builds, loopzero-logs
```

**Limites observados:**
- 1 pod → nenhuma HA
- 512Mi memory → satura com ~50 concurrent clients
- 500m CPU → throughput limitado
- 10Gi storage → insuficiente para 100+ tenants com cache

---

## Descoberta crítica (fonte oficial)

**MinIO Operator NÃO suporta upgrade standalone → distributed** (ref: [github.com/minio/operator#1025](https://github.com/minio/operator/issues/1025)).

**Implicação:** migração **não é "flip do flag"** — é **deploy new + data migration + cutover**.

**Estratégia escolhida:** blue/green migration usando `mc mirror` para sync + cutover de endpoint no Kubernetes DNS.

---

## Fase 1 — Tuning do standalone (QUICK WIN, 30min)

**Objetivo:** dobrar capacidade sem mudar arquitetura. Destrava L3 imediatamente.

**Risco:** baixo (revert em 30s).

### 1.1 — Edit values.yaml

```yaml
# infra/helm/minio/values.yaml
mode: standalone                # unchanged

resources:
  requests:
    cpu: "500m"                 # 100m → 500m
    memory: "2Gi"               # 128Mi → 2Gi
  limits:
    cpu: "2000m"                # 500m → 2000m
    memory: "4Gi"               # 512Mi → 4Gi

persistence:
  size: 50Gi                    # 10Gi → 50Gi (room for growth)

# NEW: performance tuning env vars (not in default chart values)
extraEnvVars:
  - name: MINIO_API_REQUESTS_MAX
    value: "10000"              # default: 4 × CPU = 8
  - name: MINIO_API_REQUESTS_DEADLINE
    value: "30s"                # default: 10s (conservador)
  - name: MINIO_SCANNER_SPEED
    value: "slowest"            # scanner menos agressivo sob carga
```

### 1.2 — Apply

```bash
cd /home/paulo/loopzero
helm upgrade minio infra/helm/minio \
  --namespace platform-system \
  --reuse-values \
  -f infra/helm/minio/values.yaml \
  --wait --timeout 5m

# Validar
kubectl -n platform-system get pods -l app=minio
kubectl -n platform-system exec deploy/minio -- mc admin info local
```

### 1.3 — Smoke test

```bash
# Exec into mc client
kubectl -n platform-system run mc-test --rm -it --image=minio/mc --restart=Never -- sh

# Dentro do pod:
mc alias set local http://minio.platform-system.svc:9000 loopzero-admin <password>
mc admin info local
mc ls local/loopzero-artifacts/ | head -5
```

### 1.4 — Bump CWT timeout (backstop)

```yaml
# infra/helm/argo-workflows/templates/cwt-cache-restore.yaml
spec:
  activeDeadlineSeconds: 600   # was 120
```

**Esforço total Fase 1:** 30min. **Ganho esperado:** reduzir cache-restore timeouts em ~70%.

---

## Fase 2 — Backup completo (CRITICAL, 1h)

**Objetivo:** proteger dados em `loopzero-artifacts` (source bundles reais) antes da migração destrutiva.

**Risco se pular:** perda permanente de source bundles de todos tenants.

### 2.1 — Instalar mc client

```bash
kubectl -n platform-system run mc-backup --image=minio/mc --restart=Never --command -- sleep 3600
kubectl -n platform-system exec mc-backup -- mc alias set source \
  http://minio.platform-system.svc:9000 loopzero-admin <password>
```

### 2.2 — Dump buckets críticos

```bash
# loopzero-artifacts (source bundles): CRITICAL
kubectl -n platform-system exec mc-backup -- mc mirror --watch \
  source/loopzero-artifacts \
  /backup/loopzero-artifacts &

# loopzero-builds (cache): NÃO crítico (regenerável), pular backup
# loopzero-logs: NÃO crítico, pular

# Copy backup para fora do cluster
kubectl cp platform-system/mc-backup:/backup /tmp/minio-backup-$(date +%Y%m%d)

# Validar tamanho
du -sh /tmp/minio-backup-*/loopzero-artifacts
# Se < esperado (baseado em `mc admin info`), investigar antes de prosseguir.
```

### 2.3 — Snapshot do PVC (segurança adicional)

```bash
# Criar VolumeSnapshot do PVC atual (DO suporta via CSI)
cat > /tmp/minio-snapshot.yaml <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: minio-pre-migration-$(date +%Y%m%d)
  namespace: platform-system
spec:
  volumeSnapshotClassName: do-block-storage
  source:
    persistentVolumeClaimName: minio
EOF

kubectl apply -f /tmp/minio-snapshot.yaml
kubectl -n platform-system get volumesnapshot
```

**Esforço Fase 2:** 1h (depende do tamanho dos dados). **Não pular — é contrato com o passado.**

---

## Fase 3 — Deploy distributed side-by-side (4-6h)

**Objetivo:** provisionar cluster distributed novo, sync data, cutover atômico.

**Risco:** médio (rollback requer restaurar DNS + restaurar backup).

### 3.1 — Deploy novo cluster distributed (helm release separado)

```yaml
# infra/helm/minio/values-distributed.yaml (NEW file)
mode: distributed
statefulset:
  replicaCount: 4               # 4 pods (mínimo para EC)
  drivesPerNode: 1              # 1 drive por pod (4 drives total = EC 2+2)
  zones: 1                      # single zone por enquanto

rootUser: loopzero-admin
rootPassword: loopzero-dev-password  # mesmo de standalone (compat)

persistence:
  enabled: true
  size: 50Gi                    # 50Gi × 4 = 200Gi total (50% usable com EC 2+2)
  storageClass: do-block-storage

resources:
  requests:
    cpu: "1000m"
    memory: "2Gi"
  limits:
    cpu: "2000m"
    memory: "4Gi"

podSecurityContext:
  runAsNonRoot: true

service:
  type: ClusterIP
  name: minio-dist              # DIFFERENT name from existing "minio"

consoleService:
  type: ClusterIP

extraEnvVars:
  - name: MINIO_API_REQUESTS_MAX
    value: "10000"
  - name: MINIO_API_REQUESTS_DEADLINE
    value: "30s"

# Buckets serão criados pelo mc mirror step, não pelo chart
buckets: []
```

**Deploy como release SEPARADO:**

```bash
helm install minio-dist infra/helm/minio \
  --namespace platform-system \
  -f infra/helm/minio/values-distributed.yaml \
  --wait --timeout 10m

# Validar
kubectl -n platform-system get sts minio-dist
kubectl -n platform-system get pods -l app=minio-dist
# Esperado: 4/4 pods Ready

# Validar erasure coding
kubectl -n platform-system exec minio-dist-0 -- mc admin info local
# Deve mostrar: "EC:2" (2 parity blocks)
```

### 3.2 — Sync data standalone → distributed

```bash
kubectl -n platform-system exec mc-backup -- sh <<EOF
# Setup aliases
mc alias set old http://minio.platform-system.svc:9000 loopzero-admin <password>
mc alias set new http://minio-dist.platform-system.svc:9000 loopzero-admin <password>

# Create buckets no new cluster
mc mb new/loopzero-artifacts
mc mb new/loopzero-builds
mc mb new/loopzero-logs

# Mirror (sync data — latest version only)
mc mirror --overwrite old/loopzero-artifacts new/loopzero-artifacts
mc mirror --overwrite old/loopzero-builds new/loopzero-builds  # opcional; cache regenerável
mc mirror --overwrite old/loopzero-logs new/loopzero-logs

# Verify
mc ls --recursive new/loopzero-artifacts | wc -l   # deve bater com old
mc du new/loopzero-artifacts                       # tamanho similar
EOF
```

**Tempo estimado:** 15-30min (depende do volume; bundles pequenos, transferência rápida).

### 3.3 — Smoke test no novo cluster (antes do cutover)

```bash
# Upload teste
kubectl -n platform-system exec mc-backup -- sh -c '
  echo "migration-test-$(date)" > /tmp/test.txt
  mc cp /tmp/test.txt new/loopzero-artifacts/migration-test.txt
  mc ls new/loopzero-artifacts/migration-test.txt
  mc rm new/loopzero-artifacts/migration-test.txt
'

# Performance test
kubectl -n platform-system exec mc-backup -- \
  mc admin speedtest new/ --duration 30s
# Esperado: >100 MiB/s PUT, >200 MiB/s GET (distributed é 2-4× mais rápido que standalone)
```

### 3.4 — Cutover atômico (DNS swap)

**Estratégia:** renomear Service atual, apontar `minio.platform-system.svc` para novo cluster.

```bash
# 1. Save old service manifest
kubectl -n platform-system get svc minio -o yaml > /tmp/minio-old-svc.yaml

# 2. Delete old service (mantém old StatefulSet/pods rodando como backup)
kubectl -n platform-system delete svc minio

# 3. Rename new service
kubectl -n platform-system patch svc minio-dist -p '{"metadata":{"name":"minio"}}' \
  --type=strategic
# Isso não funciona — Services não suportam rename em K8s.

# Alternativa: criar novo service "minio" apontando para distributed pods
cat > /tmp/minio-cutover.yaml <<EOF
apiVersion: v1
kind: Service
metadata:
  name: minio
  namespace: platform-system
spec:
  selector:
    app.kubernetes.io/instance: minio-dist
    app.kubernetes.io/name: minio
  ports:
    - port: 9000
      targetPort: 9000
      name: api
EOF

kubectl apply -f /tmp/minio-cutover.yaml

# 4. Validar CWTs acessam novo cluster
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-build -n argo logs \
  -l workflows.argoproj.io/workflow --tail=20 | grep -i minio
```

### 3.5 — Validação end-to-end

```bash
# Rodar L1 real (10 tenants) — deve passar 10/10 em ~15min
cd /home/paulo/loopzero/infra/tests/load
LOOPZERO_API_URL=... ./run-tests.sh l1

# Validar buckets acessíveis
curl -I https://minio.platform-system.svc:9000/loopzero-artifacts/
# 200 OK = success
```

### 3.6 — Decommission standalone (após 24h de observação)

```bash
# Manter standalone pods rodando (mas sem tráfego) por 24h
# para permitir rollback rápido.

# Após 24h sem incidentes:
helm uninstall minio --namespace platform-system

# Delete old PVC (cuidado — irreversível!)
kubectl -n platform-system delete pvc minio
kubectl -n platform-system delete volumesnapshot minio-pre-migration-YYYYMMDD
```

**Esforço total Fase 3:** 4-6h.

---

## Rollback plan

### Rollback Fase 1 (env vars)
```bash
git revert <commit-fase-1>
helm upgrade minio infra/helm/minio -n platform-system -f values.yaml
# Tempo: ~2min
```

### Rollback Fase 3 (durante migração)

**Se `mc mirror` falhar:**
1. Não fez cutover ainda → só destruir `minio-dist`: `helm uninstall minio-dist -n platform-system`
2. Apps continuam no standalone. Sem impacto.

**Se cutover foi feito e apps falhando:**
1. Restaurar service antigo:
```bash
kubectl apply -f /tmp/minio-old-svc.yaml
# DNS volta a apontar para standalone em <30s
```
2. Investigate distributed issues offline.

**Se dados perdidos (catastrophic):**
1. Restore from backup:
```bash
kubectl -n platform-system exec mc-backup -- \
  mc mirror /backup/loopzero-artifacts old/loopzero-artifacts
```
2. Recreate standalone PVC from snapshot if needed:
```bash
# Using VolumeSnapshot class
cat > /tmp/minio-restore-pvc.yaml <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: minio-restored
  namespace: platform-system
spec:
  storageClassName: do-block-storage
  dataSource:
    name: minio-pre-migration-YYYYMMDD
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 10Gi
EOF
```

---

## Validação por fase

| Fase | Validação | Gate |
|------|-----------|------|
| 1 | L3 v2 passa 100/100 com timeout 600s | ✅ Gate pass |
| 2 | Backup size > 0, `mc du` bate com source | ✅ Integridade |
| 3.1 | 4 pods Ready, `EC:2` em `mc admin info` | ✅ HA ok |
| 3.2 | `mc ls --recursive new/` count == `old/` count | ✅ Sync ok |
| 3.3 | Speedtest >100 MiB/s PUT | ✅ Performance ok |
| 3.4 | `kubectl get svc minio` aponta para distributed | ✅ Cutover ok |
| 3.5 | L1 10/10 passa após cutover | ✅ E2E ok |

---

## Custo incremental

| Recurso | Hoje (standalone) | Após distributed | Δ |
|---------|:-----------------:|:-----------------:|:--:|
| Pods MinIO | 1 | 4 | +3 |
| PVCs | 1 × 10Gi = 10Gi | 4 × 50Gi = 200Gi | +190Gi |
| DO storage | $1/mês | $20/mês | +$19/mês |
| DO compute (1 pod extra por 3 réplicas) | - | ~$30/mês | +$30/mês |
| **Total** | ~$1/mês | **~$50/mês** | **+$49/mês** |

Para scale 50k: trivial vs custo de outage ou migração tardia sob pressão.

---

## Timeline recomendada

| Quando | Ação | Impacto |
|--------|------|---------|
| **Hoje (L3 concluir)** | Fase 1 — tuning standalone | Destrava L3 v2, sem risco |
| **Dia 4 (amanhã)** | Fase 2 — backup completo | Pré-requisito para Fase 3 |
| **Dia 5 (pré-L4)** | Fase 3 — deploy distributed + cutover | L4 com HA garantida |
| **Dia 6** | Decommission standalone (após 24h obs) | Cleanup |

**Total:** ~3 dias calendário, ~6-8h de trabalho efetivo.

---

## Critérios de sucesso (DoD)

1. ✅ L3 passa 100/100 (Bug #35 eliminado)
2. ✅ `minio.platform-system.svc` aponta para 4 pods distributed
3. ✅ `mc admin info` mostra `EC:2` (erasure coding ativo)
4. ✅ L1 + L2 + L3 regression tests passam após cutover
5. ✅ Chaos test: `kubectl delete pod minio-dist-0` → buckets continuam acessíveis
6. ✅ Backup runbook testado (restore dry-run em namespace dev)
7. ✅ Values.yaml commitado em git com fix

---

## Riscos conhecidos

| Risco | Probabilidade | Mitigação |
|-------|:-------------:|-----------|
| Data loss em `loopzero-artifacts` durante migração | 🟡 Médio | Backup Fase 2 + VolumeSnapshot |
| Cutover causa downtime >5min | 🟢 Baixo | Service swap é atômico (~30s) |
| Distributed MinIO performance pior que standalone | 🟢 Baixo | Speedtest na Fase 3.3 antes do cutover |
| DO CSI não provisiona 4 PVCs (Bug #18 reaparece) | 🟡 Médio | Provisioning serial — deploy manual se necessário |
| Helm chart Bitnami muda breaking | 🟢 Baixo | Pin version específica no Chart.yaml |

---

## Referências

**Oficiais:**
- [MinIO Distributed Mode Docs](https://min.io/docs/minio/linux/operations/install-deploy-manage/deploy-minio-multi-node-multi-drive.html)
- [Bitnami MinIO Helm Chart](https://github.com/bitnami/charts/tree/main/bitnami/minio)
- [MinIO Operator GitHub Issue #1025 — no upgrade path](https://github.com/minio/operator/issues/1025)
- [mc mirror reference](https://min.io/docs/minio/linux/reference/minio-mc/mc-mirror.html)

**Tutoriais:**
- [OneUptime — Distributed MinIO on K8s (2026-02-09)](https://oneuptime.com/blog/post/2026-02-09-minio-distributed-s3-kubernetes/view)
- [Medium — Setup Distributed MinIO Cluster on K8s](https://fazpeerbaksh.medium.com/how-to-setup-distributed-minio-cluster-on-kubernetes-5227202a0048)
- [MinIO Blog — CI/CD Distributed Cluster](https://blog.min.io/ci-cd-distributed-cluster-kubernetes/)

**Migração / backup:**
- [MahdadGhasemian — Backup/Restore Script](https://gist.github.com/MahdadGhasemian/416476b536f9a1334382065a7c44b33c)
- [MinIO Blog — Data Migration Tools](https://blog.min.io/data-migration-tools-into-minio/)

---

## Changelog deste documento

- **v1 (2026-04-17):** Plano inicial baseado em pesquisa de fontes oficiais + contexto específico do LoopZero. 3 fases incrementais com rollback para cada.
