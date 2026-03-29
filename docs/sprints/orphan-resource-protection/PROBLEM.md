# Orphan Resource Protection — Problem Statement

> Recursos K8s orphanados bloqueiam o ArgoCD e impedem novos deploys.
> Prioridade: P0 — afeta todos os fluxos quando acumulam.

---

## O Problema

Quando um projeto é deletado, os manifests são removidos do Gitea e um ArgoCD sync com prune é trigado. Mas se o sync falha (por qualquer motivo), os recursos K8s permanecem no cluster como **orphans**. Esses orphans causam uma cascata de falhas:

```
Projeto deletado → manifests removidos do Gitea → sync triggered
  ↓ (sync falha: operation in progress, token expired, timeout)
  ↓
Recursos K8s permanecem (Deployments, Services, Certificates, IngressRoutes)
  ↓
Certificates orphanados tentam renovar → falham → status Progressing/Degraded
  ↓
ArgoCD Application health = Degraded
  ↓
Novo deploy trigga sync → "another operation is already in progress" (stuck sync)
  ↓
TODOS os deploys futuros falham
```

## Evidências (sessão 2026-03-29)

1. **5 projetos orphanados** acumularam durante testes:
   - `api` + `worker` (monorepo `static-mono`)
   - `df-express` (express teste com CMD errado)
   - `express-test` (segundo express teste)
   - `web` (frontend static do monorepo)

2. **ArgoCD stuck 50+ minutos** em sync "Running" esperando `tls-web` Certificate

3. **Novos deploys bloqueados** com `"another operation is already in progress"`

4. **Mesmo com fix de .gitkeep** (MEET-015), os orphans ainda acumulam quando:
   - O sync trigger falha silenciosamente
   - O ArgoCD tem uma operação stuck
   - O delete retorna exit 0 mas o prune nunca executa

## Cenários que Causam Orphans

| Cenário | Causa | Frequência |
|---------|-------|-----------|
| Delete + sync falha | ArgoCD token expirado, operation stuck | Alta em dev |
| Delete + ArgoCD Degraded | Certificate orphanado bloqueia sync | Alta |
| Build falha + pod CrashLoop | Pod nunca é saudável, ArgoCD Degraded | Média |
| Projeto deletado do DB mas não do GitOps | Race condition no delete flow | Baixa (corrigido) |
| Multiple deploys rápidos | Operations queued no ArgoCD | Média |

## Proteções Necessárias

### 1. Orphan Reaper (automático)
Um loop que periodicamente verifica o gitops repo vs os recursos no ArgoCD Application e remove resources que não existem nos manifests.

```
A cada 5 minutos:
  1. Listar resources no ArgoCD Application
  2. Listar diretórios no gitops repo
  3. Para cada resource sem manifest correspondente: marcar como orphan
  4. Se orphan há > 10 minutos: forçar delete via kubectl
```

### 2. Sync Operation Timeout
O ArgoCD sync não deveria ficar stuck indefinidamente. Configurar timeout:
- `spec.syncPolicy.syncOptions`: adicionar `Timeout=300` (5 min)
- Se o sync não completa em 5 min, ArgoCD cancela a operação automaticamente

### 3. Delete Verification Loop
Após `loopzero project delete`, a API deve verificar que os pods foram realmente removidos:
```
1. Delete → gitops.Remove → TriggerSync
2. Wait 60s
3. Verificar pods: kubectl get pods -l app={name}
4. Se pods existem: retry sync
5. Se retry falha: log ERROR + notificar
```

### 4. ArgoCD Operation Cleanup no Deploy
Antes de triggerar um novo sync, verificar se há uma operação stuck:
```
1. GET /api/v1/applications/{name}
2. Se operationState.phase == "Running" && startedAt > 10min ago:
   - POST /api/v1/applications/{name}/sync/terminate
   - Wait 5s
   - Retry sync
```

### 5. Certificate Cleanup Automático
Certificates de apps deletadas devem ser removidos junto com os manifests:
- Já acontece se o prune funciona
- O problema é quando o prune não executa (operação stuck)
- Fix: incluir Certificate delete explícito no delete flow (não depender apenas do ArgoCD prune)

## Priorização

| Fix | Impacto | Complexidade | Prioridade |
|-----|---------|-------------|-----------|
| ArgoCD sync timeout config | Alto — previne stuck operations | Baixa (Helm config) | P0 |
| Operation cleanup antes de novo sync | Alto — desbloqueia deploys | Média (API code) | P0 |
| Orphan Reaper loop | Alto — cleanup automático | Alta (novo serviço) | P1 |
| Delete Verification Loop | Médio — confirma delete | Média (API code) | P1 |
| Certificate cleanup explícito | Baixo — borda | Baixa (API code) | P2 |

## Decisão Requerida

O fix mínimo (P0) para desbloquear produção:
1. Configurar sync timeout no ArgoCD Helm values
2. Antes de TriggerSync, terminar operações stuck

O fix completo (P1) para prevenir acúmulo:
3. Orphan Reaper como background loop na API
4. Delete verification com retry

Convocar reunião para decidir scope e implementação.
