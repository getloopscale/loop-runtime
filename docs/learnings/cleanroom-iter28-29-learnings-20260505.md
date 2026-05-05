# Cleanroom Dogfood Loop — Aprendizados de Sessão (2026-05-05, iters 26-29)

> **Sessão:** `a0586b00-98b8-4379-a03b-2316544e81ff`
> **Plano executado:** `docs/plans/real-paas-cleanroom-dogfood-loop-plan.md`
> **Resultado:** 12/12 GREEN. 8 bugs novos canonicalmente fixados (B32-B39).
> **Tag de fechamento:** `dogfood-cleanroom-converged-20260505`

Este documento consolida TODOS os aprendizados extraídos da sessão. Foi
escrito para ser referência quando bugs análogos voltarem a aparecer
em sessões futuras (humano ou Claude). A organização é por categoria,
não cronológica.

---

## 1. Bugs sistêmicos resolvidos (B32-B39)

### B32 — ArgoCD repo-server cacheia "manifest error" por ~5min

**Sintoma:** Application em `OutOfSync` / `Missing` com mensagem
`InvalidSpecError: Application referencing project X which does not exist`
mesmo quando o `AppProject` existe e o `Application.spec.project` casa
exatamente.

**Diagnóstico:** ArgoCD repo-server tem dois caches separados:
1. Cache de manifests gerados — chave `(repo, path, revision)`
2. Cache de **erros** de manifest generation — mesma chave

Quando `tenant-controller` cria gitops repo com commit inicial vazio
(sem path `production/`), a primeira tentativa de gerar manifests
retorna `production: app path does not exist`. Esse erro é **cacheado
por ~5min**. Depois, quando `loopzero deploy` faz push de commit novo com
manifests reais, o repo-server reusa cache de erro (`manifest error
cache hit`) e o controller cospe erro genérico misleading sobre
AppProject.

**Fix canônico:** `SyncClient.TriggerSync` agora chama
`GET /api/v1/applications/{name}?refresh=hard` antes de `POST /sync`.
Hard refresh força repo-server a re-clonar e re-renderizar,
invalidando ambos os caches. Best-effort (15s timeout, não bloqueia).

**Commit:** `7fed6600`
**Arquivos:** `api/internal/argocd/sync_client.go`, `api/internal/argocd/sync_client_test.go`

**Padrão aprendido:** quando uma operação ArgoCD reporta erro
estranho que cita um recurso que VOCÊ JÁ VERIFICOU EXISTIR, suspeite
de cache stale do repo-server. Mensagens de erro do controller são
COLATERAIS — a causa raiz é frequentemente no caminho de manifest
generation (repo-server) ou cluster registration.

---

### B33 — VAP deadlocks namespace deletion (terminating eternal)

**Sintoma:** Namespaces ficam em `Terminating` indefinidamente. Status
mostra `ContentDeletionFailed: networkpolicies "Unknown" is forbidden:
ValidatingAdmissionPolicy 'loopzero-protect-platform-networkpolicies'`.

**Diagnóstico:** VAP `loopzero-protect-platform-networkpolicies` rejeita
`DELETE` em NetworkPolicies labeled `loopzero.io/managed-by=environment-controller`.
Quando um namespace entra em `Terminating`, o K8s namespace garbage
collector (`system:serviceaccount:kube-system:namespace-controller`)
tenta deletar TODOS os recursos. VAP rejeita as NPs platform → namespace
deadlock.

**Fix canônico:** `matchConditions` exempta:
- `system:serviceaccount:kube-system:namespace-controller` (namespace GC)
- `system:kube-controller-manager` (controllers internos)
- `system:masters` group (bootstrap / break-glass)

Tenants ainda não conseguem deletar NetworkPolicies via suas próprias
credenciais — só o GC machinery do cluster é exemptizado.

**Commit:** `1981c83a`
**Arquivos:** `infra/helm/admission-policies/templates/vap-protect-platform-networkpolicies.yaml`

**Padrão aprendido:** **toda VAP com `failurePolicy: Fail` precisa de
`matchConditions` excluindo namespace-controller**. Sem isso, a regra
colide com o GC interno do K8s e namespace deletion deadlock por
padrão. Isso aplica a QUALQUER VAP que reject DELETE — não apenas
NetworkPolicies.

**Heurística geral:** quando escrever uma regra de proteção
"contra tenants", sempre considere o conjunto de subjects da plataforma
que precisam operar livremente:
- `system:serviceaccount:kube-system:namespace-controller`
- `system:kube-controller-manager`
- `kubelet` (em alguns casos)
- `system:masters` group

---

### B34 — SecretK8sManager não acessa workload cluster

**Sintoma:** API loga `secret K8s manager not available — ESO sync
disabled" error="...stat /home/loopzero/.kube/config: no such file or
directory"`. `loopzero secret set` retorna 200 e audit loga
`secret.set`, mas Pods depois falham com `Error: secret
"user-secrets-X" not found` → `CreateContainerConfigError` → HTTP 503.

**Diagnóstico:** `cmd/setup_k8s.go::NewSecretK8sManager` usa
`rest.InClusterConfig()` para mgmt cluster (funciona — API roda nele),
mas para runtime cluster requer kubeconfig path explícito via
`RUNTIME_KUBECONFIG`. O api-deployment.yaml standalone-multi nunca
montou esse arquivo. `loopzero secret set` silently no-ops na criação do
ExternalSecret CR no workload — o handler retorna sucesso ao usuário
mas o effect lateral falha.

**Fix canônico:**
- Mount `loopzero-cluster-kubeconfigs` Secret (key=`runtime.kubeconfig`)
  como volume separado em `/etc/loopzero/runtime/`
- Export `RUNTIME_KUBECONFIG=/etc/loopzero/runtime/kubeconfig`
- Export `RUNTIME_KUBE_CONTEXT=in-cluster` (nome do context dentro do
  Secret, escrito pelo heal H2 — NÃO `runtime`)

**Commit:** `230970bc`
**Arquivos:** `infra/tilt/api-deployment.yaml`

**Padrão aprendido:** todos os componentes de cross-cluster operations
precisam de kubeconfig mount no Tilt (production usa Pulumi para
prover via Secret/Volume). Bugs assim só aparecem quando o flow E2E
é exercitado — `loopzero secret set` sucede no API surface mas fail
silently na materialização downstream. **Audit todo handler que
retorna 200 imediato + tem efeito downstream cross-cluster**.

---

### B35 — `seed-local-user.sh` regen mid-journey falha silently

**Sintoma:** Journey-03 falha ao regenerar token expirado mid-poll
com mensagem `seed-local-user.sh failed during regen` mesmo quando
plataforma está saudável. Default Hydra TTL ~30min triggers em
qualquer journey rodando >30min após preflight.

**Diagnóstico:** `seed-local-user.sh` faz `kubectl port-forward` para
Hydra, default `HYDRA_NAMESPACE=loopzero-auth` (production layout). Em
standalone-multi (k3d), Hydra roda em `loopzero-system`. Sem override,
port-forward apontava para serviço inexistente, `curl -sf` retornava
empty `TOKEN_RESP`, script saía 1 silently (porque journey wrapper
redirecionava `2>/dev/null`).

**Fix canônico:**
- Introduzir `SEED_HYDRA_NS=loopzero-system` em `_common.sh` wrapping
  toda chamada a `$SEED_SCRIPT`
- Capturar seed output em `/tmp/seed-regen.log` para causa real do
  fail aparecer no journey FAIL message

**Commit:** `230970bc`
**Arquivos:** `infra/tests/dogfood/journeys/_common.sh`

**Padrão aprendido:** **NUNCA `>/dev/null 2>&1` em script paths que
podem falhar** — isso esconde causa raiz e converte bug em "ghost".
Sempre capture output em arquivo temporário e mostre em failure
message.

**Heurística geral:** Defaults de produção (HYDRA_NAMESPACE=loopzero-auth,
kubeconfig externo, KUBE_CONTEXT=runtime) NÃO casam com layout k3d.
Standalone-multi precisa overrides exportados consistentemente em
TODOS os entry points: `task setup`, `task k3d:heal`,
`bash infra/tests/dogfood/full-loop.sh`, `bash infra/tests/dogfood/
journeys/_common.sh`.

---

### B36 — Test contract drift: scale handler não é mais stub

**Sintoma:** Journey-04 falha com
`expected status=acknowledged (current stub contract), got: applied`.

**Diagnóstico:** Plan T2.2 wired `PostgresScalingStore` via
`api/cmd/setup_orchestrator.go:144` — `scale` handler agora persiste
no DB e retorna `applied`/`no_change` quando `h.apply != nil`.
Legacy `acknowledged` só fires no fallback path (transitional). CLAUDE.md
ainda descrevia o stub anterior — gap doc.

**Fix canônico:** journey-04 aceita
`{applied, no_change, acknowledged}`. Adicionei comment explícito
que K8s `Deployment.spec.replicas` updates apenas no próximo `loopzero
deploy` porque o renderer lê do DB no deploy time.

**Commit:** `a24eb995`
**Arquivos:** `infra/tests/dogfood/journeys/journey-04-scale.sh`

**Padrão aprendido:** documentação (CLAUDE.md, SKILL.md, plan docs)
**deriva mais rápido que o código**. Antes de citar contract de uma
SKILL.md como evidência, **grep o código real**. Generaliza a memória
prévia `feedback_skill_md_outdated_vs_code`.

---

### B37 — DB CHECK constraint missing rollback states

**Sintoma:** `loopzero rollback` 500s com
`new row for relation "releases" violates check constraint
"releases_status_check" (SQLSTATE 23514)`.

**Diagnóstico:** `pkg/statemachine/release.go:27-28` declara
`ROLLING_BACK` e `ROLLED_BACK` como estados canônicos.
`services/rollback_orchestrator` transiciona para `ROLLING_BACK` em
todo rollback. Migration `000001` (e refeita por `000008` para
ARCHIVED) NUNCA incluiu esses dois estados. Toda release rollback
falha com PG check constraint violation.

**Por que não foi pego antes:** unit suite usa sqlite mock sem CHECK
constraints. Envtest não exercita rollback path. Gap invisible até
LIVE PostgreSQL exercitar a transição completa.

**Fix canônico:** migration `000010_release_rollback_states.up.sql`
drops o CHECK existente e re-adiciona com 12 estados canônicos
(antigos 10 + ROLLING_BACK + ROLLED_BACK). Forward-only.

**Commit:** `a24eb995`
**Arquivos:** `api/internal/db/migrations/000010_release_rollback_states.up.sql`

**Padrão aprendido:** **state machine declara estado mas migration
esquece** — drift comum entre `pkg/statemachine/*.go` e DB CHECK
constraints. Toda vez que adicionar novo estado em Go, **grep
migrations** por `releases_status_check` (e equivalents para outras
state machines: `builds_status_check`, etc.) para verificar que está
incluído.

**Heurística:** `pg_get_constraintdef(oid) FROM pg_constraint WHERE
conname='X_status_check'` é a forma de inspecionar o CHECK live.
Comparar contra `pkg/statemachine/X.go` constants é o audit.

---

### B38 — `LOOPZERO_ADMIN_API` flag missing em standalone Tilt

**Sintoma:** Journey-11 admin probe HTTP 404
`{"code":"NOT_FOUND","message":"Endpoint not available."}` mesmo com
`X-Service-Auth` correto.

**Diagnóstico:** Ambos endpoints admin (`admin_evict.go:73`,
`admin_pin_cell.go:78`) gate em `LOOPZERO_ADMIN_API=true` env var antes
de qualquer outra check. Production seta via Pulumi `mgmt-api.ts`.
Tilt YAML standalone nunca opt-in. Mensagem 404 é deliberada (defense-
in-depth — não revela whether flag é on).

**Fix canônico:** Adicionar `LOOPZERO_ADMIN_API=true` em
`infra/tilt/api-deployment.yaml`. `LOOPZERO_SERVICE_AUTH_SECRET` continua
gating cada call individualmente.

**Commit:** `441f5531`
**Arquivos:** `infra/tilt/api-deployment.yaml`

**Padrão aprendido:** **feature flags de defense-in-depth produzem
404 em vez de 403 deliberadamente**. Mensagem `Endpoint not available`
é o sinal — significa flag missing, não auth issue. Audit todo endpoint
admin/internal por gates de feature flag e garantir que standalone
Tilt seta todos.

---

### B39 — Heal H10 race: rollout assíncrono + annotate sobrescreve

**Sintoma:** Application destination fica `https://k3d-loopzero-workload-server-0:6443`
(hostname) mesmo depois do heal H10 explicitly patchar para IP.
ArgoCD sync falha com `unable to find destination server` porque
cluster Secret tem IP, não hostname.

**Diagnóstico (deep — vale ler):** Sequência atômica do heal H10:
1. `kubectl set env WORKLOAD_CLUSTER_URL=$IP` — cria nova ReplicaSet
2. `kubectl rollout restart` — adiciona annotation, **non-blocking**
3. `kubectl annotate tenant ... heal-trigger=...` — triggers reconcile
4. `kubectl patch application ... destination/server=IP` — direct override

**Race:** entre 2 e 3, o Pod-A (env=stale hostname) ainda está rodando
em graceful shutdown (terminationGracePeriod). Pod-A recebe o
annotate event do step 3 e reconcile **antes de morrer**.
`EnsureApplication` lê `r.WorkloadClusterURL` (capturado no constructor
= valor antigo, hostname). Update sobrescreve `destination.server` =
hostname.

Pod-B (env=fresh IP) inicia depois, mas seu reconcile não acontece
porque:
- Controller-runtime list-watch JÁ PROCESSOU o heal-trigger annotation
  (consumido por Pod-A)
- Default reconcile period é 10h
- Nenhuma mudança subsequente no Tenant CR ocorre

Resultado: Application fica em destination stale **for forever**.
H10 patch step 4 também é overwritten porque managedFields da
last-write é Pod-A.

**Como descobri:** `kubectl get app ... --show-managed-fields -o json`
mostrou `loopzero-tenant-operator` (Update) at 20:10:14Z como last writer
de `f:destination`. Cross-referenced com pod start time + heal log
timestamps + ReplicaSet creation timestamps.

**Fix canônico:** Inserir `kubectl rollout status --timeout=60s`
ENTRE `rollout restart` e `annotate`. Garante que Pod-A morreu
totalmente e Pod-B está Ready antes de annotate firar reconcile.
Apenas Pod-B (env fresco) processa o evento.

**Commit:** `6ab84cb1`
**Arquivos:** `infra/scripts/k3d-cross-cluster-heal.sh`

**Padrão aprendido:** **`kubectl rollout restart` é não-bloqueante**.
Sempre pair com `kubectl rollout status` se houver downstream
operations que dependem do new pod. Especialmente crítico quando o
controller-pattern usa env vars capturadas no constructor (não re-read
em runtime).

**Heurística geral para race conditions em K8s:**
1. `managedFields` mostra QUEM e QUANDO foi o último writer de cada field
2. `pod.status.startTime` + `replicaset.metadata.creationTimestamp` +
   timestamps de logs do heal/script revelam a sequência exata
3. Se "current spec ≠ what code writes", suspeite que código teve
   env-capture-at-startup com env mismatch entre old/new pod

---

## 2. Aprendizados meta-processuais (operacionais)

### 2.1 Stop hook do ralph-loop tem requisitos LITERAIS

O ralph-loop stop hook (`hooks/stop-hook.sh`) faz:

```bash
LAST_LINES=$(grep '"role":"assistant"' "$TRANSCRIPT_PATH" | tail -n 100)
LAST_OUTPUT=$(echo "$LAST_LINES" | jq -rs '
  map(.message.content[]? | select(.type == "text") | .text) | last // ""
')
PROMISE_TEXT=$(echo "$LAST_OUTPUT" | perl -0777 -pe \
  's/.*?<promise>(.*?)<\/promise>.*/$1/s; s/^\s+|\s+$//g; s/\s+/ /g')
[[ "$PROMISE_TEXT" = "$COMPLETION_PROMISE" ]] && exit 0
```

**Implicações que aprendi do hard way:**

1. **`last` em `jq` pega o ÚLTIMO text block** — se você emite text +
   tool calls, o ÚLTIMO block é o text antes dos tool calls. Se o tool
   call foi seu último ato, o text block antes dele é o que conta.

2. **Tool calls após o `<promise>` empurram o text antes** — se você
   fizer um tool call depois de emitir a promise, o text antes do tool
   call (que pode não conter `<promise>`) torna-se o `LAST_OUTPUT`.

3. **`<promise>` em backticks ou code blocks ainda é capturado pelo
   regex perl** — `s/.*?<promise>(.*?)<\/promise>.*/$1/s` é greedy-suffix
   non-greedy-prefix-and-capture. Se você mencionar `<promise>` em texto
   explanatório ANTES da emissão real, o regex captura o conteúdo
   ENTRE essa primeira ocorrência e o primeiro `</promise>` — que vai
   ser texto não-promise.

4. **Solução**: para fechar o ralph-loop, emit APENAS:
   ```
   <promise>EXACT_TEXT_OF_completion_promise</promise>
   ```
   Sem prefácio. Sem tool calls posteriores. Sem backticks ao redor.
   Sem mencionar a tag em texto antes da emissão.

5. Se o hook re-injeta mesmo com `<promise>` correto, debug com:
   ```bash
   HOOK_INPUT=$(printf '{"session_id":"%s","transcript_path":"%s"}' "$SID" "$TP")
   echo "$HOOK_INPUT" | bash "$HOOK"
   ```
   E inspecionar o `LAST_OUTPUT` extraído manualmente.

### 2.2 Live testing é não-negociável (nunca dependa só de unit/envtest)

**Bugs DESTA SESSÃO que nunca foram detectados por unit/envtest:**

| Bug | Por que unit/envtest não pegou |
|---|---|
| B32 | Repo-server cache não existe em mocks Argo |
| B33 | Envtest cria namespaces mas não exercita Terminating com VAP ativa |
| B34 | Mock `SecretK8sSync` não detecta missing kubeconfig |
| B35 | Journey scripts são "testes" mas mocks nunca rodam contra Hydra real |
| B36 | Unit tests passam com qualquer status retornado |
| B37 | sqlite mock não tem CHECK constraints |
| B38 | Unit tests setam env explicitly; deployment YAML é separate |
| B39 | Race condition timing nunca é deterministically reproducible em unit |

**Princípio:** unit tests provam que o código FUNCIONA isoladamente.
Eles NÃO PROVAM que o sistema funciona. Para isso é dogfood.

### 2.3 Codify EVERY manual workaround

Cada `kubectl`/`docker`/`helm` manual fix DEVE virar:
- Script idempotente commitado (e.g., `k3d-cross-cluster-heal.sh`)
- Entry no regression matrix (`docs/runbooks/bootstrap-regression-matrix.md`)
- Memory entry (memória persistente em `~/.claude/projects/.../memory/`)

**Trigger pessoal:** parar ANTES da TERCEIRA repetição do mesmo logical
fix manual. Generaliza memória prévia
`feedback_no_tribal_knowledge`.

### 2.4 Audit por log evidence > teorização

Múltiplas vezes nesta sessão eu teorizei sobre o estado do sistema e
errei. A correção sempre veio quando capturei evidência real:

- B32: teorizei "AppProject não existe". Real: `kubectl get appproject`
  mostrou que existe. Mensagem ArgoCD era misleading.
- B39: teorizei "Pod-B reconcile escreveu IP". Real: `managedFields`
  + timestamps revelaram que Pod-A reconcile (com env stale) foi o
  último writer.

**Heurística:** sempre que houver mismatch entre "o que o código deveria
fazer" e "o que está acontecendo", colete:
1. `kubectl get X -o yaml --show-managed-fields` (managedFields revela
   última escrita)
2. Pod start times + ReplicaSet creation timestamps
3. Logs de heal + tilt + tenant-operator + api com timestamps
   alinhados em UTC
4. `pg_get_constraintdef` (pra DB constraints)
5. ArgoCD `operationState.message` (pode ser misleading mas é o que
   o controller registrou)

---

## 3. Aprendizados técnicos transversais

### 3.1 ArgoCD repo-server caching

ArgoCD tem 3 níveis de cache em sequência:
1. **Manifest cache** — chave `(repo, path, revision)`. Sucesso é cached.
2. **Manifest error cache** — mesma chave. Falhas são cached separadamente.
3. **Comparison cache** — controller cacheia comparison result por App.

Mostly invalidação automática via `git fetch` periodic refresh, MAS o
**error cache tem TTL próprio (~5min default)** que NÃO é invalidado
por simples push de novo commit. Solução documentada: hard refresh
(`?refresh=hard` na API ou annotation `argocd.argoproj.io/refresh: hard`).

### 3.2 K8s pod env é capturado no startup

`containers[].env` é injected uma única vez quando o container starts.
`kubectl set env` atualiza Deployment template e force ReplicaSet
update, mas o pod EXISTENTE não vê mudança até die.

**Implicações em controllers:**
- Constructor-time pattern (`r.WorkloadClusterURL = cfg.WorkloadClusterURL`)
  congela env value. Reconciles subsequentes usam valor antigo.
- `kubectl rollout restart` é a forma de force pod recreation.
- Pair com `kubectl rollout status` para wait deterministic.

### 3.3 ESO sync chain (External Secrets Operator)

Para um secret aparecer no namespace workload, TUDO abaixo precisa
existir:

1. **Source secret** no mgmt cluster (`loopzero-system/secret-X`)
2. **ClusterSecretStore** (`loopzero-mgmt-store`) com status `Valid` + `Ready`
3. **ExternalSecret CR** no namespace destino do workload cluster
   referenciando ClusterSecretStore
4. **ServiceAccount RBAC** que permite ExternalSecret read source secret
5. **ESO controller pod** rodando no workload cluster

Falha em qualquer um → secret não materialize → pod fail
`CreateContainerConfigError`. Debug:
```bash
kubectl --context k3d-loopzero-workload -n loopzero-tn-X-Y get externalsecrets
kubectl --context k3d-loopzero-mgmt -n loopzero-system get secrets | grep -i secret-X
kubectl --context k3d-loopzero-workload get clustersecretstore
```

### 3.4 ManagedFields é a fonte para race conditions

```bash
kubectl get APP_NAME --show-managed-fields -o json | jq '.metadata.managedFields'
```

Cada entry tem:
- `manager` — quem escreveu (controller name)
- `time` — UTC timestamp da última escrita
- `fieldsV1` — quais fields foram tocados

Para descobrir QUEM escreveu QUE field por último:
```bash
kubectl get X -o json --show-managed-fields | python3 -c "
import json, sys
doc = json.load(sys.stdin)
for f in doc['metadata']['managedFields']:
    spec = f.get('fieldsV1', {}).get('f:spec', {})
    if 'f:destination' in spec:
        print(f'{f[\"manager\"]} @ {f[\"time\"]}')
"
```

### 3.5 Forward-only migrations: state machine vs DB drift

Toda vez que adicionar estado em `pkg/statemachine/X.go`:
1. `grep migrations` por `X_status_check`
2. Adicionar nova migration `NNN_X_new_states.up.sql` que drops + re-adds
   o CHECK com lista completa
3. Verificar live com `pg_get_constraintdef`

Migration 000010 desta sessão é exemplo canônico.

### 3.6 ArgoCD Application destination patterns

Dois patterns suportados:
1. **`destination.server: URL`** — ArgoCD lookup direto pelo URL string
   na cluster Secret.
2. **`destination.name: cellID`** — ArgoCD lookup por label `name=X` em
   cluster Secret. Resilient a IP rotation.

Para multi-cell + standalone, o operator escolhe baseado em `cellID`:
- `cellID == "cell-default"` → uses `destination.server` (precisa
  match exato com cluster Secret URL)
- `cellID != "cell-default"` → uses `destination.name`

**Implicação prática:** em standalone, IP do workload cluster muda em
`k3d cluster restart`. URL string em Application precisa ser updated.
Se `destination.name` fosse usado, ArgoCD resolvia automaticamente
via cluster Secret label. Mas há trade-off de produção (alguns installs
têm cluster Secret label diferente).

---

## 4. Heurísticas de debugging por sintoma

| Sintoma | Hipótese ranqueada |
|---|---|
| `InvalidSpecError: project X does not exist` (mas project EXISTE) | Repo-server cache de erro stale → hard refresh |
| `unable to find destination server` | App `destination.server` ≠ cluster Secret `data.server` (formato — IP vs hostname) |
| `Endpoint not available` (HTTP 404 de admin) | Feature flag missing (`LOOPZERO_ADMIN_API`) |
| `secret X not found` (em pod runtime) | ESO sync chain quebrada (source/ExternalSecret/ClusterSecretStore) |
| Namespace Terminating eternal | VAP `failurePolicy: Fail` sem matchConditions para namespace-controller |
| `max sync retriggers reached` na convergence | TriggerSync chega à API mas operation falha por outro motivo (cache, AppProject, RBAC). Verifique `operationState.message`. |
| `releases_status_check` constraint violation | State machine declara estado não na lista do DB CHECK |
| Pod `CreateContainerConfigError: secret not found` | Mesma causa raiz que ESO chain — verifique mgmt source + workload ExternalSecret |
| `seed-local-user.sh failed during regen` | HYDRA_NAMESPACE mismatch (loopzero-auth prod vs loopzero-system standalone) |
| Application destination revertido após heal patch | Race: old pod (env stale) reconcile depois do annotate, sobrescreve patch — adicionar `kubectl rollout status` |

---

## 5. Recomendações para sessões futuras

### 5.1 Antes de iniciar plano de cleanroom

- [ ] `git tag dogfood-cleanroom-baseline-${DATE}` antes do primeiro destroy
- [ ] Verifique `LOOPZERO_TOKEN` válido em `~/.loopzero/local-dogfood-token.txt`
  + `LOOPZERO_SERVICE_AUTH` em `~/.loopzero/local-service-auth.txt`
- [ ] Garanta que `task setup` chama `bootstrap:standalone` (B9-B30 codified)
- [ ] Garanta que `task k3d:heal` é idempotent e cobre H1-H15 (B22, B25, B27, B31, B33, B39)

### 5.2 Durante o loop

- Após CADA fix: rebuild da imagem afetada + `kubectl set image` ou
  rollout restart, ANTES de re-run o journey. Tempo total ~3min.
- Para journey-XX: capture log em `/tmp/jXX.log`, evite buffering
  bash via `> /tmp/jXX.log 2>&1`
- Se journey FAIL, primeiros recursos a checar:
  - `kubectl --context k3d-loopzero-mgmt -n argocd get app -A`
  - `kubectl --context k3d-loopzero-workload -n loopzero-tn-X-Y get pods,events`
  - `kubectl --context k3d-loopzero-mgmt -n loopzero-system logs deploy/loopzero-api --since=5m`
  - DB releases status: `SELECT id, build_id, status FROM releases WHERE tenant_id='X' ORDER BY created_at DESC LIMIT 3`

### 5.3 Ao fechar plano

- [ ] Append "Convergence Log" no plano (T4.2)
- [ ] Commit relatório em `docs/reports/dogfood-cleanroom-${DATE}.md`
- [ ] Salvar memória `reference_cleanroom_iter${N}_${DATE}.md` +
  index `MEMORY.md`
- [ ] Tag `dogfood-cleanroom-converged-${DATE}` annotated apontando
  para HEAD
- [ ] Rule 3 audit: `grep -E "ArgoCD|Gitea|MinIO|tenant_id|advisory lock|
  Kaniko|kubebuilder|envtest|GitOps|gitops-push|CRD"
  /tmp/journey-*.log | grep -v "^\[k3d-heal\]\|^\[_common\.sh\]"`
  deve retornar 0 matches em CLI output user-facing
- [ ] Emitir `<promise>...</promise>` como ÚLTIMO text block sem tool
  calls posteriores

---

## 6. Mapa de dependências entre os 8 bugs (B32-B39)

```
B32 (ArgoCD repo-server cache)
   └─ blocks: journey-01..11 (qualquer Application sync)

B33 (VAP namespace deadlock)
   └─ blocks: prefix-T0.10 cleanup → preflight → all journeys

B34 (RUNTIME_KUBECONFIG mount)
   └─ blocks: journey-03 (secrets), journey-09 (preview if uses ESO)

B35 (HYDRA_NAMESPACE regen)
   └─ blocks: any journey > 30min wall-clock

B36 (scale contract)
   └─ blocks: journey-04 only

B37 (rollback DB constraint)
   └─ blocks: journey-06 (rollback)

B38 (LOOPZERO_ADMIN_API flag)
   └─ blocks: journey-11 (admin)

B39 (heal H10 race)
   └─ blocks: journey-01..11 in cleanroom (Application destination wrong)
```

B32 e B39 são "fundacionais" — afetam todos os journeys. Os outros
são journey-specific.

---

## 7. Estatísticas finais da sessão

- **Iterações ralph-loop:** 14 (state file ended at iteration 15 antes
  do hook detectar promise)
- **Commits no develop:** 41 desde `dogfood-cleanroom-baseline-20260505`
- **Tools usados:** Bash 932 / Edit 98 / Read 82 / Write 15 /
  TaskCreate 6 / TaskUpdate 11
- **Tempo total estimado:** ~6 horas wall-clock
- **Files novos criados nesta sessão:**
  - `api/internal/db/migrations/000010_release_rollback_states.up.sql`
  - `docs/reports/dogfood-cleanroom-20260505-progress.md` (CONVERGED section)
  - `docs/learnings/cleanroom-iter28-29-learnings-20260505.md` (este doc)
  - `~/.claude/projects/-home-paulo-loopzero/memory/reference_cleanroom_iter29_2026_05_05.md`
- **Files modificados (canonical fixes):**
  - `api/internal/argocd/sync_client.go` (B32)
  - `api/internal/argocd/sync_client_test.go` (B32 tests)
  - `infra/helm/admission-policies/templates/vap-protect-platform-networkpolicies.yaml` (B33)
  - `infra/tilt/api-deployment.yaml` (B34, B38)
  - `infra/tests/dogfood/journeys/_common.sh` (B35)
  - `infra/tests/dogfood/journeys/journey-04-scale.sh` (B36)
  - `infra/scripts/k3d-cross-cluster-heal.sh` (B39)
  - `docs/plans/real-paas-cleanroom-dogfood-loop-plan.md` (Convergence Log)

---

## 8. Próximos passos sugeridos (não-bloqueantes)

1. **CLAUDE.md atualizar** — descrever `loopzero scale` como persistência
   ativa (não mais stub). Documentar que `Deployment.spec.replicas`
   atualiza no próximo `loopzero deploy` (renderer lê do DB).

2. **Production audit B33** — verificar se VAPs em production tem
   `matchConditions` similares. Se não, namespace deletion pode
   deadlock em production também (latente até alguém deletar tenant).

3. **Production audit B37** — `pg_get_constraintdef` em production PG
   para `releases_status_check` deve incluir ROLLING_BACK + ROLLED_BACK.
   Migration 000010 precisa rodar em produção também.

4. **Operator improvement** — `tenant-controller` poderia usar
   `destination.name=workload` em vez de `destination.server=URL` para
   eliminar B39 dependency em IP rotation. Trade-off: produção pode ter
   cluster Secret labelado diferente.

5. **Self-test do heal script** — adicionar smoke test que valida
   sequência H10: post-heal, Application.spec.destination deve ser IP
   atual + tenant-operator pod env deve match. Pinned via go test ou
   bash test em `infra/tests/`.

---

**Final note:** este documento captura o aprendizado técnico. As
memórias persistentes em `~/.claude/projects/-home-paulo-loopzero/memory/`
capturam o aprendizado como referência para sessões futuras de Claude.
Os dois são complementares — este doc é detail-heavy para humanos
auditarem; as memórias são compactas para Claude consumir como contexto.
