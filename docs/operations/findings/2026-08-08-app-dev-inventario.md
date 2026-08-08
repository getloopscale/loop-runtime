# Inventário do ambiente app-dev — 2026-08-08

Registro datado do estado do droplet `loopzero-e2e-runner` (`165.227.121.20`) medido em
2026-08-08 entre 13:25 e 13:50 UTC, logo após o merge do PR #270. É **audit trail**:
descreve o que era verdade naquele momento e não deve ser reescrito para refletir o
presente (`docs/README.md`).

Seis achados. Um é defeito de produto e virou a issue
[#271](https://github.com/getloopscale/loopzero/issues/271) + o plano
`.claude/knowledge-base/plans/argocd-application-status-preservation-plan.md`.
Os outros cinco são estado de ambiente: não há código a mudar, há operação a fazer.
Estão aqui, e não no plano, porque um plano de implementação carrega **um** objetivo com
**uma** métrica (`skills/to-plan/templates/plan-template.md § Goal`) — misturar
`git pull` com correção de operator produziria um plano cujo objetivo ninguém consegue
declarar em uma frase.

## Resumo

| # | Achado | Natureza | Encaminhamento |
|---|---|---|---|
| 1 | `.status` dos `Application` do ArgoCD oscila para vazio | **defeito de produto** | issue #271 + plano |
| 2 | `loopzero-api` servindo imagem de 6 dias | ambiente | `rollout restart` |
| 3 | 6 pods `Terminating` há 17–26 dias no `runtime-2` | ambiente | nó morto; recriar ou remover |
| 4 | `drill-watchdog` em `ImagePullBackOff` no build | ambiente | alcançabilidade do quay |
| 5 | `tn-f50e039ca488a1bc-preview` fixo em `Unknown` | **não medido até o fim** | precisa de leitura do repo gitops |
| 6 | load average 17–20 | sintoma | remedir depois de #271 |

## 1 — `.status` dos `Application` oscila para vazio (defeito de produto)

Medido:

```
13:34:13 sync=Synced
13:34:22 sync=          ← apagado
13:34:30 sync=Synced    ← ArgoCD recalculou
```

O `tenant-operator` escreve o `Application` com `Update` de objeto inteiro montado do
zero (`operators/internal/argocd/real_provisioner.go:283-321`); o CRD do ArgoCD não
habilita o subresource `status` (`kubectl get crd applications.argoproj.io -o
jsonpath='{.spec.versions[*].subresources}'` → `{}`), então o campo é substituído.

O motor da reescrita é uma colisão de nome medida depois: `appName` e `path`
(`operators/internal/tenant/tenant_gitops.go:226,238`) não incluem `env.Project`,
enquanto `destNS` inclui (`:229`) — um tenant com o mesmo `env.Name` em vários projetos
escreve o **mesmo** `Application` várias vezes por reconcile com destinos conflitantes.
Medida: 1519 linhas de `argocd application reconciled` em 5 minutos (~5 escritas/s).

Ver a issue para o registro completo, inclusive a correção de diagnóstico.

## 2 — `loopzero-api` servindo imagem de 6 dias

```
pod loopzero-api-89c9dcb95-r2n8d   age=6d18h
rodando   ghcr.io/getloopscale/loopzero-api@sha256:e8478f61d760b40646b3f0e4b688f09e7519c9352345b11320f8ed3dabd7c4c2
:develop  ghcr.io/getloopscale/loopzero-api@sha256:f8131d7a750b30ce7038bed334cc5b396d80aa962ea254940ef5bf3397d7b89d
```

O digest em execução **não** está entre os manifests da tag `:develop` atual. Nada do
#268 nem do #270 está rodando ali. `imagePullPolicy: Always` não resolve isso — ele só
puxa na criação do pod.

`loopzero-cloud` estava em dia (pod de 4 minutos).

**Remédio.** `kubectl -n loopzero-system rollout restart deploy/loopzero-api` seguido de
`kubectl -n loopzero-system rollout status deploy/loopzero-api`. O restart é não-bloqueante
(`CLAUDE.md` B39): sem o `rollout status`, qualquer comando seguinte corre com o pod
antigo ainda reconciliando.

## 3 — 6 pods `Terminating` há 17–26 dias no `runtime-2`

```
argo-rollouts-…      Terminating  17d
cert-manager-…       Terminating  17d
cert-manager-cain…   Terminating  17d
external-secrets-…   Terminating  26d
coredns-…            Terminating  26d
traefik-…            Terminating  26d
```

Não é indisponibilidade: cada um tem substituto `Running` (coredns 1/1, traefik 4/1,
cert-manager 3/2, external-secrets 3/1, argo-rollouts 1/1).

Causa medida:

```
$ kubectl --context k3d-loopzero-runtime-2 get nodes
k3d-loopzero-runtime-2-agent-0    NotReady   <none>                 26d
k3d-loopzero-runtime-2-server-0   Ready      control-plane,master   26d

$ kubectl -n kube-system get pod coredns-9fc4f469d-r26sw -o jsonpath='...'
finalizers=  deletionTimestamp=2026-07-23T14:43:06Z  node=k3d-loopzero-runtime-2-agent-0
```

O nó agente está `NotReady` há 26 dias. Os pods têm `deletionTimestamp` e **nenhum
finalizer** — ficam em `Terminating` porque o kubelet sumiu e ninguém confirma a
remoção. Não é finalizer preso, é nó morto.

**Remédio.** Recuperar o agente (`docker start k3d-loopzero-runtime-2-agent-0`) ou removê-lo
do cluster; os pods somem sozinhos quando o nó for deletado. `--force --grace-period=0`
resolve o sintoma sem tocar na causa, e o nó continua fora do cluster.

**O que este achado revela e não está coberto:** um nó de célula caiu e ficou 26 dias
sem ninguém notar. Não há alerta para isso. Fora do escopo do que foi pedido, mas é a
observação mais cara desta lista.

## 4 — `drill-watchdog` em `ImagePullBackOff` no build

```
Warning  Failed  4m18s (x370 over 89m)  kubelet  Error: ImagePullBackOff
imagem: quay.io/argoproj/argoexec:v4.0.3
```

Origem: `CronWorkflow drill-watchdog` no namespace `argo-ci` do cluster de build, 26 dias
de idade. As últimas execuções falharam:

```
drill-watchdog-1786125600  Failed  19h    Step exceeded its deadline
drill-watchdog-1786147200  Failed  13h    Step exceeded its deadline
drill-watchdog-1786168800  Failed  7h44m  Step exceeded its deadline
```

**A tag existe.** `GET quay.io/api/v1/repository/argoproj/argoexec/tag/?specificTag=v4.0.3`
retorna o manifest (`sha256:92add537…`). Não é pin quebrado no repositório — é o cluster
de build não conseguindo puxar do quay (rede, rate limit, ou mirror que não cobre o
quay).

**Remédio.** Diagnosticar a alcançabilidade a partir de um pod do cluster de build antes
de mexer em qualquer coisa. Mesmo comentário do achado 3: uma tarefa agendada falha a
cada ~6h há mais de 19h e não há sinal disso em lugar nenhum.

## 5 — `tn-f50e039ca488a1bc-preview` fixo em `Unknown` (não medido até o fim)

```
Failed to load target state: failed to generate manifest for source 1 of 1:
rpc error: code = Unknown desc = Manifest generation error (cached):
preview: app path does not exist
```

A `Application` aponta para `path=preview` no repo
`tn-f50e039ca488a1bc-gitops.git`, revisão `main` — mesma forma da
`tn-d10e0b6430d51588-preview`, que funciona.

**Não consegui fechar o diagnóstico.** A API do Gitea exigiu autenticação que eu não
tinha, então não sei dizer se o diretório `preview/` não existe no repo ou se é o cache
do repo-server descrito no `CLAUDE.md` (B32, resolvido por `?refresh=hard`).

Há uma pista que aponta para a primeira hipótese e que conecta com o achado 1: o
comentário em `tenant_gitops.go:235-237` afirma que a hierarquia de manifests é
`{environment}/{projectID}/manifests.yaml`, mas a App aponta para `{environment}`
inteiro. Se nenhum projeto daquele ambiente publicou, `preview/` não existe. Isso é a
mesma raiz da colisão de nome do achado 1 e está registrado como Q2/Q3 no plano.

**Próximo passo.** Ler o conteúdo do repo com token do Gitea antes de planejar qualquer
coisa. Planejar sem isso seria especulação.

## 6 — load average 17–20

```
13:25:54 up 15 days, 23:24, 13 users, load average: 17.61, 18.94, 20.68
```

Cinco clusters k3d num único droplet, mais o loop de reescrita do achado 1 (~5
escritas/s no apiserver do mgmt, com o application-controller do ArgoCD recalculando
status a cada ~2,5 s).

Não separei a contribuição de cada parte. A medida útil é remedir **depois** que o
achado 1 estiver corrigido — antes disso, qualquer atribuição é chute.

## Comandos que produziram este registro

```bash
ssh paulo@165.227.121.20

# achado 1
kubectl --context k3d-loopzero-mgmt get crd applications.argoproj.io \
  -o jsonpath='{.spec.versions[*].subresources}'
kubectl --context k3d-loopzero-mgmt -n argocd get application tn-d10e0b6430d51588-production \
  -o jsonpath='{range .metadata.managedFields[*]}{.manager}{" op="}{.operation}{"\n"}{end}'
kubectl --context k3d-loopzero-mgmt -n loopzero-system logs deploy/loopzero-tenant-operator \
  --since=5m --tail=5000 | grep -c "argocd application reconciled"

# achado 2
kubectl --context k3d-loopzero-mgmt get pods -A -o jsonpath='{range .items[*]}{.status.containerStatuses[*].imageID}{"\n"}{end}' | grep loopzero-api
docker manifest inspect -v ghcr.io/getloopscale/loopzero-api:develop | grep -o 'sha256:[a-f0-9]\{64\}' | sort -u

# achado 3
kubectl --context k3d-loopzero-runtime-2 get nodes
kubectl --context k3d-loopzero-runtime-2 -n kube-system get pod coredns-9fc4f469d-r26sw \
  -o jsonpath='finalizers={.metadata.finalizers} node={.spec.nodeName}'

# achado 4
kubectl --context k3d-loopzero-build -n argo-ci get workflows | grep drill
curl -s "https://quay.io/api/v1/repository/argoproj/argoexec/tag/?specificTag=v4.0.3"

# achado 5
kubectl --context k3d-loopzero-mgmt -n argocd get application tn-f50e039ca488a1bc-preview \
  -o jsonpath='{.status.conditions[*].message}'
```

Nenhum segredo neste documento.
