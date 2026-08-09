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

### 3. Orfanar e deletar — nesta ordem, no mesmo bloco

```bash
APP=<nome-legado>

# (a) orfanar: remove o finalizer de cascade. Sem este passo, o delete abaixo
#     apaga o workload do tenant.
kubectl --context $CTX -n argocd patch application "$APP" \
  --type=merge -p '{"metadata":{"finalizers":[]}}'

# (b) confirmar que o finalizer saiu ANTES de deletar
kubectl --context $CTX -n argocd get application "$APP" \
  -o jsonpath='{.metadata.finalizers}'   # deve imprimir vazio ou []

# (c) só então deletar
kubectl --context $CTX -n argocd delete application "$APP"
```

Se o passo (b) ainda mostrar o finalizer, **pare**. Repita (a); não prossiga para (c).

### 4. Subir o operator novo e confirmar a adoção

```bash
kubectl --context $CTX -n loopzero-system rollout restart deploy/loopzero-tenant-operator
kubectl --context $CTX -n loopzero-system rollout status  deploy/loopzero-tenant-operator

# os Apps canônicos aparecem e adotam os recursos órfãos
kubectl --context $CTX -n argocd get applications \
  -o custom-columns=NAME:.metadata.name,PATH:.spec.source.path,SYNC:.status.sync.status,HEALTH:.status.health.status

# a contagem de pods NÃO pode ter caído em relação ao passo 1
kubectl --context $CTX get pods -A --no-headers | grep '^loopzero-tn-' | awk '{print $1}' | sort | uniq -c
```

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
