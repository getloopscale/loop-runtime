# Runbook — migração da identidade do Application do ArgoCD (#271)

> **PERIGO.** As `Application` do ArgoCD carregam `resources-finalizer.argocd.argoproj.io`
> (`operators/internal/argocd/real_provisioner.go:205`). Um `kubectl delete application`
> **sem remover o finalizer antes** apaga os recursos que ela gerencia — o workload do
> tenant. Todo bloco deste documento traz o `patch` de orfanamento junto do `delete`.
> Nunca copie um `delete` isolado daqui.

## O que muda

A identidade da `Application` passa de `{tenant}-{env}` para `{tenant}-{project}-{env}`, e o
`spec.source.path` de `{env}` para `{env}/{project}` — casando com onde os manifests são de
fato escritos (`api/internal/gitops/gitea_pusher.go:136`).

## Por que a limpeza é PRÉ-deploy

`pruneOrphanPreviewApps` deleta toda `Application` do tenant que esteja fora do conjunto
esperado, **com o cascade ativo**. Depois do deploy, o conjunto esperado é canônico, então
todo App de nome legado passa a ser candidato a poda — e a poda leva o workload junto,
antes de o App canônico ter sincronizado.

Fazer a limpeza **antes** do deploy inverte isso: os recursos ficam órfãos e vivos, o
operator novo cria o App canônico, e o ArgoCD **adota** os recursos existentes. Sem
downtime e sem recriação.

O caso de teste `"App de nome legado é podado pelo prune canônico"`
(`operators/internal/tenant/tenant_gitops_test.go`) existe para que essa ordem não vire
folclore.

## Procedimento

### 1. Registrar o estado de partida

```bash
CTX=k3d-loopzero-mgmt   # ajuste ao ambiente
kubectl --context $CTX -n argocd get applications \
  -o custom-columns=NAME:.metadata.name,PATH:.spec.source.path,DEST:.spec.destination.namespace

# contagem de pods por namespace de tenant — a referência para provar que nada caiu
kubectl --context $CTX get pods -A --no-headers | grep '^loopzero-tn-' | awk '{print $1}' | sort | uniq -c
```

Guarde as duas saídas. A segunda é o que prova, ao fim, que a migração não derrubou nada.

### 2. Listar os Apps de nome legado

Legado = o nome **não** contém o segmento de projeto. Compare com os environments
declarados no Tenant CR:

```bash
kubectl --context $CTX -n loopzero-system get tenants \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.environments}{"\n"}{end}'
```

Para cada tenant, o conjunto canônico é `{tenant}-{project}-{env}` para cada entrada de
`spec.environments`. Todo App do tenant fora desse conjunto é legado.

### 3. Parar quem repõe os finalizers — o passo que faltava

> **Corrigido em 2026-08-09, depois de executar este runbook em `app-dev`.** A versão
> anterior mandava orfanar e deletar direto. **Não funciona**: o patch aplica, e o
> controller repõe o finalizer no reconcile seguinte, em segundos. Medido — os oito
> objetos voltaram com `resources-finalizer.argocd.argoproj.io` e
> `loopzero.io/environment-cleanup` antes do delete.
>
> O `EnsureFinalizer` do `EnvironmentReconciler` e o `buildApplicationObject` do
> provisioner recolocam. Enquanto eles rodam, orfanar é um no-op com aparência de
> sucesso — o pior tipo de passo de runbook.

```bash
kubectl --context $CTX -n loopzero-system scale deploy/loopzero-tenant-operator --replicas=0
kubectl --context $CTX -n loopzero-system rollout status deploy/loopzero-tenant-operator --timeout=90s

# confirme que zerou de verdade antes de prosseguir
kubectl --context $CTX -n loopzero-system get pods -l app.kubernetes.io/name=loopzero-tenant-operator
```

### 4. Orfanar — agora persiste

```bash
for A in $(kubectl --context $CTX -n argocd get applications -o name | cut -d/ -f2); do
  kubectl --context $CTX -n argocd patch application "$A" \
    --type=merge -p '{"metadata":{"finalizers":[]}}'
done
for E in $(kubectl --context $CTX -n loopzero-system get environments -o name | cut -d/ -f2); do
  kubectl --context $CTX -n loopzero-system patch environment "$E" \
    --type=merge -p '{"metadata":{"finalizers":[]}}'
done
```

**Verifique que nenhum voltou** antes de seguir. Se algum finalizer reaparecer, o operator
ainda está de pé — volte ao passo 3.

### 5. Deletar os legados

Sem finalizer, o delete não cascateia na `Application` e não dispara o `reconcileDelete`
do `Environment` (que apagaria o namespace):

```bash
for A in $(kubectl --context $CTX -n argocd get applications -o name | cut -d/ -f2); do
  kubectl --context $CTX -n argocd delete application "$A" --wait=false
done
for E in $(kubectl --context $CTX -n loopzero-system get environments -o name | cut -d/ -f2); do
  kubectl --context $CTX -n loopzero-system delete environment "$E" --wait=false
done
```

Confirme a contagem de pods do passo 1 **antes** de subir o operator.

### 6. Subir o operator novo e confirmar a adoção

```bash
kubectl --context $CTX -n loopzero-system scale deploy/loopzero-tenant-operator --replicas=2
kubectl --context $CTX -n loopzero-system rollout status  deploy/loopzero-tenant-operator

# os Apps canônicos aparecem e adotam os recursos órfãos
kubectl --context $CTX -n argocd get applications \
  -o custom-columns=NAME:.metadata.name,PATH:.spec.source.path,SYNC:.status.sync.status,HEALTH:.status.health.status

# a contagem de pods NÃO pode ter caído em relação ao passo 1
kubectl --context $CTX get pods -A --no-headers | grep '^loopzero-tn-' | awk '{print $1}' | sort | uniq -c
```

## Execução de referência — `app-dev`, 2026-08-09

Este runbook foi executado uma vez, e o registro fica aqui porque a primeira versão dele
estava errada:

| | Antes | Depois |
|---|---|---|
| Applications | 5, todas de nome legado | 10 canônicas, `path={env}/{project}` |
| Environment CRs | 4 | 9 |
| Pods / namespaces | 10 / 10 | **10 / 10** |

Zero downtime. Os 9 `Environment` CRs contra 4 são a medida do #273: os projetos que não
tinham namespace, RBAC nem NetworkPolicy passaram a ter.

## Rollback

Não há rollback limpo depois do passo 3: o App legado foi deletado. O que existe é
recuperação — os recursos continuam vivos (foi por isso que foram orfanados), e um
operator de qualquer versão os readota ao criar o App correspondente. Se for preciso
voltar à versão antiga do operator, ela recriará o App de nome legado apontando para
`{env}`, e o ArgoCD readotará os mesmos recursos.

## Cross-references

- Issue: [#271](https://github.com/getloopscale/loopzero/issues/271)
- Plano: `.claude/knowledge-base/plans/argocd-app-identity-canonical-plan.md`
- Inventário que originou o achado: `docs/operations/findings/2026-08-08-app-dev-inventario.md`
- O finalizer: `operators/internal/argocd/real_provisioner.go:205`
- O prune: `operators/internal/tenant/tenant_gitops.go` (`pruneOrphanPreviewApps`)
