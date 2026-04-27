# Plan: MinIO Standalone → Distributed Migration

> **Referenciado por:** ADR-0010 Phase 2a deliverable #5
> **Status:** Plan-only (no execution). Requires explicit user authorization for execution steps.
> **Author:** Sprint 8 (T2.3)
> **Last updated:** 2026-04-27

## Objective

Migrar o MinIO do mgmt cluster de `mode: standalone` (single PVC, SPOF) para `mode: distributed` (4 drives com erasure coding `EC:2`), eliminando ponto único de falha para artefatos de build (caches, bundles, logs).

## Why now

- Sprint 8 / Phase 2a Gate exige que cada shared service sobreviva à perda de uma instância com recovery <2min (chaos test gate).
- Hoje, perda do pod MinIO standalone = perda imediata de acesso a `loopzero-artifacts`, `loopzero-builds`, `loopzero-logs`. Builds em execução abortam, pull de cache falha, novos deploys travam.
- 100+ tenants amplificam o blast radius: 1 standalone falha = 100+ tenants impactados.

## Current state

`infra/helm/minio/values.yaml`:

```yaml
mode: standalone
persistence:
  size: 50Gi
buckets:
  - name: loopzero-artifacts
  - name: loopzero-builds
  - name: loopzero-logs
```

- **1 pod**, 1 PVC de 50Gi, sem replicação.
- Recovery após pod kill: depende do tempo de scheduling + remount do PVC (~30-90s no caso médio, mais em caso de node drain).
- Sem tolerância a corrupção de disco.

## Target state

`mode: distributed` com **4 drives** (mínimo suportado pelo MinIO distributed). Layout:

| Parameter | Value | Rationale |
|---|---|---|
| `mode` | `distributed` | Habilita erasure coding. |
| `replicas` (StatefulSet pods) | `4` | 1 drive por pod (drivesPerNode=1). |
| `drivesPerNode` | `1` | Mais simples operacionalmente; falha de pod = falha de 1 drive. |
| `persistence.size` | `50Gi` por drive (200Gi total bruto, ~100Gi útil com EC:2) | Mantém capacidade equivalente ao standalone atual (50Gi). |
| Erasure code set | `EC:2` (2 data + 2 parity) | Padrão MinIO para 4 drives. Sobrevive a perda de 2 drives simultâneos. |
| Affinity | `podAntiAffinity` por hostname | Distribui 4 pods em nodes diferentes (requer ≥4 nodes mgmt — depende de Sprint 8 T0.2 elevar `mgmtMaxNodes` para 5). |

## Pré-condições

- [ ] **mgmt cluster com ≥4 nodes disponíveis para anti-affinity** (depende de Sprint 8 T0.2: `mgmtNodeClass: medium` + `mgmtMaxNodes: 5`).
- [ ] **DO Volumes quota** suficiente para 4 PVCs de 50Gi adicionais (200Gi extra). Custo estimado: ~$20/mo (DO `do-block-storage` $0.10/GB/mo).
- [ ] **Backup recente** dos 3 buckets atuais (`loopzero-artifacts`, `loopzero-builds`, `loopzero-logs`) antes da migração. Janela: <24h.
- [ ] **Janela de manutenção** de até 30min (downtime aceito em dev; produção exigiria estratégia blue/green completa).
- [ ] **`mc` (MinIO client) instalado** localmente para `mc mirror`.

## Migration strategy: blue/green namespace

Estratégia escolhida: **deploy paralelo do MinIO distributed em namespace separado** (`minio-distributed`), copiar dados, trocar endpoint, deletar standalone. Evita downtime longo e mantém rollback trivial.

**Alternativa rejeitada:** in-place mode change. Helm chart oficial do MinIO **não suporta** transição standalone→distributed sem destruir o StatefulSet (PVCs deletados, dados perdidos).

### Phase A — Deploy lado a lado (zero downtime no antigo)

1. Criar `infra/helm/minio/values-distributed.yaml` com `mode: distributed`, `replicas: 4`, anti-affinity, e os 3 buckets idênticos.
2. `helm upgrade --install minio-distributed minio/minio -n minio-distributed --create-namespace -f values-distributed.yaml`
3. Aguardar todos os 4 pods `Ready` (`kubectl get pods -n minio-distributed -w`). Esperado: 2-3min.
4. Verificar quórum: `kubectl exec -n minio-distributed minio-distributed-0 -- mc admin info local`. Deve mostrar `4 Online, 0 Offline`.

### Phase B — Espelhar dados (read traffic continua no antigo)

5. Configurar aliases:
   ```bash
   mc alias set old http://<minio-old-svc>.minio.svc.cluster.local:9000 <user> <pass>
   mc alias set new http://<minio-distributed-svc>.minio-distributed.svc.cluster.local:9000 <user> <pass>
   ```
6. Para cada bucket:
   ```bash
   mc mb new/loopzero-artifacts new/loopzero-builds new/loopzero-logs
   mc mirror --preserve old/loopzero-artifacts new/loopzero-artifacts
   mc mirror --preserve old/loopzero-builds new/loopzero-builds
   mc mirror --preserve old/loopzero-logs new/loopzero-logs
   ```
7. **Validação intermediária:** `mc ls --recursive old/loopzero-builds | wc -l` == `mc ls --recursive new/loopzero-builds | wc -l`. Repetir para os outros buckets.

### Phase C — Cutover (curto downtime: ~5min)

8. **Pause** novos deploys: `kubectl scale deploy/api -n loopzero-api --replicas=0` (ou feature flag `LOOPZERO_DEPLOY_DISABLED=true`).
9. **Aguardar drain** dos workflows ativos. `kubectl get workflows -A --field-selector status.phase=Running` deve estar vazio.
10. **Mirror final** (incremental, captura escritas dos últimos minutos):
    ```bash
    mc mirror --preserve --overwrite old/loopzero-artifacts new/loopzero-artifacts
    mc mirror --preserve --overwrite old/loopzero-builds new/loopzero-builds
    mc mirror --preserve --overwrite old/loopzero-logs new/loopzero-logs
    ```
11. **Trocar endpoint** no `loopzero-api` (ConfigMap/Secret `S3_ENDPOINT`) e nos build CWTs (`infra/helm/loopzero-build-cwts/`):
    - `S3_ENDPOINT: minio-distributed.minio-distributed.svc.cluster.local:9000`
    - Reiniciar API: `kubectl rollout restart deploy/api -n loopzero-api`
12. **Resumir** deploys.

### Phase D — Validação pós-cutover

13. Executar deploy E2E happy path (1 tenant, 1 app) e validar:
    - Build conclui (lê cache de `loopzero-builds`).
    - Cache write funciona (escreve em `loopzero-builds`).
    - Logs persistem (`loopzero-logs`).
14. **Failover test:** `kubectl delete pod minio-distributed-0 -n minio-distributed`. Esperado: novo deploy ainda funciona (3/4 drives online > quórum mínimo de 3).
15. **Rollback test (somente em janela controlada):** repetir delete sem aguardar pod #0 voltar. Não deve passar para 2/4 (não kill 2 pods simultaneamente).

### Phase E — Decommission (após observação)

- Aguardar **48h de observação** com métricas estáveis (sem erros 5xx no API, sem build failures atribuídos a S3).
- Backup final do MinIO antigo (snapshot DO Volume).
- `helm uninstall minio -n minio` (ou rename namespace antigo para `minio-legacy`).
- Deletar PVC antigo após mais 7 dias.

## Rollback plan

Em qualquer ponto até **Phase C step 11** (troca de endpoint):

- Reverter Helm value `S3_ENDPOINT` para o serviço antigo.
- `kubectl rollout restart deploy/api`.
- Manter `minio-distributed` rodando para investigação; deletar com `helm uninstall minio-distributed -n minio-distributed` somente após postmortem.

Após Phase C: rollback exige novo `mc mirror` reverso (new → old), que captura apenas escritas pós-cutover. Janela de risco aceitável até Phase E.

## Risk matrix

| Risco | Probabilidade | Impacto | Mitigação |
|---|---|---|---|
| `mc mirror` falha no meio | Média | Médio | `--continue` flag; pode retomar do último objeto. |
| Quorum loss durante cutover (2 pods caem juntos) | Baixa | Alto (read+write travam) | Anti-affinity + nodes em zonas diferentes. Não kill manual. |
| Endpoint cache stale em pods antigos | Alta | Médio | Rollout restart explícito de API + workflows controllers. |
| DO Volumes quota estourada | Baixa | Alto (deploy trava) | Verificar quota antes (`doctl compute volume list`). |
| Performance degradação (EC overhead) | Média | Baixo | EC:2 tem ~10% overhead de CPU vs standalone. Aceitável. |
| Build cache miss durante cutover (custo de re-build) | Alta | Baixo (apenas latência) | Mirror final imediatamente antes do switch reduz a janela. |

## Acceptance criteria (referenciado por ADR-0010 e Sprint 8 T2.3)

- [ ] 4 pods `minio-distributed` `Ready` em nodes distintos.
- [ ] `mc admin info` reporta `4 Online, 0 Offline` e erasure code set ativo.
- [ ] Buckets `loopzero-artifacts`, `loopzero-builds`, `loopzero-logs` existem com mesmo conteúdo do MinIO antigo (object count diff = 0).
- [ ] Build pipeline E2E funcional com novo endpoint.
- [ ] Failover test: kill 1/4 pods → operações continuam <2min recovery (Gate 2a).
- [ ] MinIO antigo decomissionado após 48h estável.
- [ ] Zero data loss observado.

## Time + cost estimate

- **Wall time:** ~2-3h (Phase A: 5min; Phase B: 30-60min para mirror; Phase C: 10min; Phase D: 30min; Phase E observação: 48h passive).
- **Operator time:** ~3h ativo + monitoramento passivo.
- **Custo extra DO:** ~$20/mo durante Phase A-D (volumes paralelos), volta a baseline+50Gi após Phase E.

## Authorization required

Este plano descreve apenas a **estratégia**. Execução exige aprovação explícita do operador para:

1. `helm upgrade --install minio-distributed` (cria recursos novos no mgmt cluster).
2. `mc mirror` (transferência de dados).
3. Troca de `S3_ENDPOINT` (T2 — afeta contrato API/workers).
4. `helm uninstall minio` (T2 — destrutivo, irreversível para o cluster antigo).

Sem aprovação, este documento permanece como **referência arquitetural**, satisfazendo o item "plano criado" do ADR-0010 mas **não** o item "implementado".
