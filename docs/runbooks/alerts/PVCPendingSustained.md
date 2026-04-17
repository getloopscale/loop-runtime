# Runbook: PVCPendingSustained

**Severity:** warning  
**Component:** storage  
**Task:** T-07

## O que dispara

```promql
kube_persistentvolumeclaim_status_phase{phase="Pending"} == 1
```

PVC em `Pending` por 2min+. Histórico: bugs #18 (DO CSI serial), #40 (DO API 429).

## Diagnóstico rápido

```bash
# 1. Qual PVC?
kubectl --context <ctx> -n <ns> describe pvc <name> | grep -A5 Events

# 2. StorageClass usado
kubectl --context <ctx> -n <ns> get pvc <name> -o jsonpath='{.spec.storageClassName}'
```

## Decisão por StorageClass

| StorageClass | Sinal | Ação |
|--------------|-------|------|
| `local-path` | Pending + `WaitForFirstConsumer` | **Normal** — binda quando pod mount. Verifica se pod está schedulable |
| `do-block-storage` | `ProvisioningFailed` 429 | DO API rate limit (bug #40). Espera 5min OR migra workload para local-path |
| `do-block-storage` | `ProvisioningFailed` outros erros | Checa DO console — region capacity? |
| Qualquer | `Pending` sem events | Provisioner pod down. Checa `local-path-storage` ou CSI controller |

## Correções conhecidas

- **Commit 32226f8:** workspace builds migrados para `local-path` (bug #21/#40 fix)
- Se vir `do-block-storage` em workflow workspace → provavelmente código antigo. Verificar imagem API.

## Recuperação

1. Se CSI rate limit:
   ```bash
   # Aguardar janela + verificar se retenta bindou
   kubectl --context <ctx> -n <ns> get pvc <name> -w
   ```
2. Se local-path binding mode WFC: checar pod que monta:
   ```bash
   kubectl --context <ctx> -n <ns> get pods -o jsonpath='{.items[?(@.spec.volumes[*].persistentVolumeClaim.claimName=="<name>")].metadata.name}'
   ```
3. Se stuck > 10min: force delete pod + PVC + recreate via operator reconcile.
