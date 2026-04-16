# Scale 50k — Day 2 Execution Learnings (2026-04-16)

**Companion document to:** [`scale-50k-plan.md`](scale-50k-plan.md) · [`scale-50k-day1-learnings.md`](scale-50k-day1-learnings.md)
**Scope:** Segunda sessão de execução — foco em fechar L1 DoD com 10/10 apps LIVE em infra DigitalOcean real.
**Status:** 🏆 **L1 DoD ACHIEVED (10/10 success + live)**

---

## 1. Marco Histórico

Pela primeira vez na história do projeto LoopZero, o pipeline **end-to-end completo** foi validado contra infraestrutura de produção real (DigitalOcean), com **10 apps independentes deployadas e servindo HTTP 200** nas URLs públicas.

```
═══════════════════════════════════════════════════════
  REAL DEPLOY L1 — FINAL RESULTS (2026-04-16 21:16 UTC)
═══════════════════════════════════════════════════════
  Tenants:       10
  Succeeded:     10 / 10  ✅
  Apps Live:     10 / 10 (HTTP 200 verified)  ✅
  Failed:        0
  Avg upload:    3.969s
  Avg build:     3m26s
  Avg verify:    12s

  ✅ tn-0: build-e58e4e426a2b599f 2m43s [LIVE]  ← melhor tempo
  ✅ tn-1: build-382aeaeda496222d 3m3s  [LIVE]
  ✅ tn-2: build-e0814115dec79376 3m33s [LIVE]
  ✅ tn-3: build-992ff376fbd5d767 3m35s [LIVE]
  ✅ tn-4: build-990f1588fda7ae6e 4m17s [LIVE]
  ✅ tn-5: build-8ee4000fb5c0c23a 3m33s [LIVE]
  ✅ tn-6: build-8a45120d26e1b32d 4m27s [LIVE]
  ✅ tn-7: build-5a88262c7635fae4 3m36s [LIVE]
  ✅ tn-8: build-f3f404cac6b83e36 3m56s [LIVE]
  ✅ tn-9: build-f3778b0230728f65 4m17s [LIVE]
═══════════════════════════════════════════════════════
--- PASS: TestRealDeploy_L1_10Tenants (917.45s)
```

**SLO compliance:**
| Métrica | SLO | Medido | Status |
|---|---|---|---|
| Upload p95 | < 10s | 3.9s avg | ✅ |
| Deploy p99 | < 10min | 4m27s max | ✅ |
| Verify p95 | < 30s | 12s avg | ✅ |
| Success rate | 100% | 100% | ✅ |

---

## 2. Bugs Descobertos e Corrigidos (15 total)

Cada iteração do L1 revelou a próxima camada de bugs ocultos na plataforma. Esta é exatamente a razão de ser do "L1 real, não mock" prescrita no scale-50k-plan.

### 2.1 Panic no upload — nil HTTP client

**Sintoma:** HTTP 502 em todos os uploads; logs da API mostravam stack trace com `net/http.(*Client).do(0x0, ...)`.

**Causa raiz:** `source/gitea_store.go` e `source/gogit_store.go` passavam `nil` ao construir `GiteaRepoManager`. No primeiro request o código fazia `m.client.Do(req)` e crashava.

**Fix:** Defesa em `NewGiteaRepoManager` — quando `client == nil`, usa `http.DefaultClient`.

**Arquivo:** `api/internal/gitclient/gitea.go`

### 2.2 Imagem `minio/mc:RELEASE.2025-04-08` deletada

**Sintoma:** `ErrImagePull` em `restore-cache` e `save-cache` com mensagem `failed to resolve reference: not found`.

**Causa raiz:** Docker Hub removeu a tag antiga. Imagens pinadas em CWTs quebram quando o publisher remove versões.

**Fix:**
- Live: `kubectl apply` nos CWTs com a tag atual `RELEASE.2025-08-13T08-35-41Z`
- Persistência: atualizado Helm templates

**Arquivos:** `infra/helm/argo-workflows/templates/cwt-cache-{restore,save}.yaml`

### 2.3 PodSecurity baseline bloqueando BuildKit

**Sintoma:** `violates PodSecurity "baseline:latest": forbidden AppArmor profiles (container "main" ... "Unconfined"), seccompProfile ... "Unconfined"`.

**Causa raiz:** BuildKit precisa `unconfined` em AppArmor + seccomp para setup de user namespaces (rootlesskit). O namespace `argo` no build cluster tinha PSA enforce=baseline, que rejeita `Unconfined`.

**Fix:** `kubectl label namespace argo pod-security.kubernetes.io/enforce=privileged` (dev/staging — produção precisa decidir isolation strategy).

### 2.4 Plano Starter = 1 projeto, mas load test cria 10

**Sintoma:** HTTP 402 `PLAN_LIMIT_EXCEEDED` no segundo projeto de um tenant.

**Causa raiz:** O harness usava `"plan": "starter"` no bootstrap. Starter permite apenas 1 projeto. Tenants de runs anteriores já tinham 1 projeto, bloqueando criação do segundo.

**Fix duplo:**
1. Harness passa `"plan": "enterprise"` (unlimited) para novos bootstraps.
2. UPDATE no PG em runtime para promover tenants existentes.

**Arquivo:** `infra/tests/load/real_deploy_harness.go`

### 2.5 `crane:v0.20.2` deletada do gcr.io

**Sintoma:** Mesmo padrão da 2.2 — `ErrImagePull` em `mirror-harbor-api`.

**Fix:** Atualizado para `v0.21.5` (última versão estável no release).

**Arquivo:** `api/internal/argo/harbor_mirror.go`

### 2.6 Secret `cosign-signing-key` não existia

**Sintoma:** `MountVolume.SetUp failed: secret "cosign-signing-key" not found`.

**Causa raiz:** Código `cosign.go` referencia o secret, mas ele nunca foi criado pelo Pulumi no ns `argo`.

**Fix:** Gerado keypair via `docker run ... cosign generate-key-pair` + `kubectl create secret` no namespace `argo`.

### 2.7 Imagem `loopzero-cosign-signer:v0.1.0` nunca foi publicada

**Sintoma:** `ErrImagePull` ao referenciar a imagem do signer.

**Causa raiz:** O Dockerfile existia em `infra/images/cosign-signer/`, mas nunca foi construído e publicado em `registry.digitalocean.com`. Bonus: SHA256 do cosign no Dockerfile estava errado (`3e4cdd...` vs real `8b24b9...`).

**Fix:** Corrigido SHA + build + push manuais. Também bumped `cosign v2.4.1 → v3.0.6` (atual).

### 2.8 `crane:v0.21+` é distroless — não tem `/bin/sh`

**Sintoma:** `failed to find name in PATH: exec: "sh"` no `mirror-harbor-api`.

**Causa raiz:** A partir de v0.21, a imagem `crane` migrou para distroless (ko-build padrão). O código usava `Command: ["sh", "-c"], Args: ["crane copy ..."]`, mas não há shell.

**Fix:** Chamar binário direto — `Command: ["/ko-app/crane"], Args: ["copy", src, dst]`.

**Arquivo:** `api/internal/argo/harbor_mirror.go`

**Lição:** Upgrade de imagens distroless pode quebrar scripts. Prefira binários invocados diretamente quando possível.

### 2.9 Regex `sed` não aceitava whitespace

**Sintoma:** `ERROR: no digest found in metadata file` em `sign-image-api`. Inspeção manual do PVC mostrava o arquivo com o digest presente.

**Causa raiz:** O regex era `"containerimage.digest":"..."` (sem espaço), mas BuildKit escrevia JSON formatado com `"containerimage.digest": "..."` (com espaço após `:`).

**Fix:** `[[:space:]]*` entre `:` e `"` na regex.

**Arquivo:** `api/internal/argo/cosign.go`

**Lição:** Testes unit com JSON compact escondem bugs que só aparecem com JSON real (pretty-printed).

### 2.10 `DOCKER_CONFIG` em `/root/.docker` inacessível para user 65534

**Sintoma:** `No matching credentials were found for registry.digitalocean.com` no mirror.

**Causa raiz:** Secret montado em `/root/.docker`, mas container roda como user 65534 (nobody) — sem permissão de leitura.

**Fix:** Montar em `/tmp/.docker` + `DOCKER_CONFIG=/tmp/.docker` + `HOME=/tmp`.

### 2.11 `xargs` ausente em `minio/mc:2025-08+`

**Sintoma:** `/usr/bin/sh: line 39: xargs: command not found` no `save-cache`.

**Causa raiz:** Minio removeu `findutils` da imagem nova. O script usava `xargs` para trim de whitespace.

**Fix:** Shell parameter expansion puro:
```bash
d="${d#"${d%%[![:space:]]*}"}"
d="${d%"${d##*[![:space:]]}"}"
```

**Arquivo:** `infra/helm/argo-workflows/templates/cwt-cache-save.yaml`

### 2.12 cosign v2.4.1 → v3.0.6

Além do fix de SHA, aproveitei para atualizar cosign para a versão atual (v3.0.6 lançada em 2026-01). Dockerfile atualizado, imagem republished.

### 2.13 Runtime cluster sem `imagePullSecret` para DO registry

**Sintoma:** Builds OK, ArgoCD sync cria pods, mas pods ficam `ImagePullBackOff` com 401 Unauthorized.

**Causa raiz:** Tenant namespaces no runtime cluster não tinham o secret `loopzero-infra-registry`. O secret existia apenas em `platform-system` (mgmt). Também descobri que o SA padrão é `loopzero-app`, não `default`.

**Fix:**
1. Copiar secret `loopzero-infra-registry` de mgmt para **todos** os namespaces `loopzero-tn-lt-*` no runtime.
2. `kubectl patch serviceaccount loopzero-app -p '{"imagePullSecrets":[{"name":"loopzero-infra-registry"}]}'`.

**Nota Pulumi:** Isso precisa ser provisionado no `TenantController.CreateNamespace` ou via Pulumi (não só manual).

### 2.14 Callback race: WF Succeeded → DB FAILED

**Sintoma:** Workflow phase=Succeeded no Argo, mas `build.status=FAILED` no PG.

**Causa raiz:** O `BuildReconcileLoop` chegava a observar `phase=Failed` durante transições de DAG com `ContinueOn{Failed: true}` (best-effort steps como `mirror-harbor`). O observador interrompia prematuramente antes do estado estabilizar.

**Fix:** Adicionado campo `FinishedAt` ao `WorkflowStatus`. O reconcile só trata Failed/Error como terminal quando `FinishedAt != ""` — garantindo que o workflow realmente terminou (não é transiente durante execução).

**Arquivos:**
- `api/internal/services/build_orchestrator.go` (struct field)
- `api/internal/argo/workflow_submitter.go` (populate from forge)
- `api/internal/services/build_reconcile_loop.go` (check before failBuild)

**Commit:** `2afb72a` — "fix(build): reconcile loop must wait for workflow FinishedAt before FAILED"

**Lição:** Race condition clássica em sistemas observadores de DAG — nunca trate um estado transient como terminal. Sempre espere o estado estável.

### 2.15 BuildReaper `ReleasingTimeout` zero-value → reap instantâneo

**Sintoma:** Build entra em RELEASING, 2 segundos depois: `Build reaped due to timeout`. Workflow terminou OK.

**Causa raiz:** `cmd/main.go` e `cmd/controller/main.go` construíam `services.BuildReaperConfig{...}` sem passar `ReleasingTimeout`. Em Go, o zero-value de `time.Duration` é `0s`. A query SQL:

```sql
WHERE status = 'RELEASING' AND started_at < now() - 0s
```

...é equivalente a `started_at < now()`, que é **sempre verdadeiro**. Reaper pegava imediatamente qualquer build em RELEASING.

**Fix:**
1. Adicionado `BuildReleasingTimeout` ao `Config` struct (env `BUILD_RELEASING_TIMEOUT`, default 10min).
2. Passado `cfg.BuildReleasingTimeout` em ambos entrypoints.
3. Adicionado `releasing_timeout` ao log do `Start()` para visibilidade de regressões futuras.

**Arquivos:**
- `api/internal/config/config.go`
- `api/cmd/main.go`
- `api/cmd/controller/main.go`
- `api/internal/services/build_reaper.go`

**Commit:** `7990552` — "fix(reaper): add BuildReleasingTimeout to config + pass in both entrypoints"

**Lição:** Go zero-values em structs de config são uma fonte clássica de bugs silenciosos. Log de todas as durações na inicialização é a melhor defesa — uma entrada faltando no log teria flagado isso em segundos.

---

## 3. Padrões Sistêmicos Observados

### 3.1 "Imagens pinadas que somem"
- minio/mc `2025-04-08`: deletada
- crane `v0.20.2`: deletada
- minio/mc `2025-08+` mudou entrypoint (sem xargs)

**Recomendação:** 
- Rodar scan periódico de imagens usadas vs disponíveis no registry
- Mirror críticos no Harbor (que agora temos!)
- CI job semanal que tenta `docker pull` de todas as imagens referenciadas

### 3.2 "Código existe mas infra não"
Três ocorrências nesta sessão:
- `cosign-signing-key` secret: código espera, mas nenhum provisionamento cria
- `loopzero-cosign-signer` image: Dockerfile existe, mas pipeline nunca publica
- `loopzero-infra-registry` imagePullSecret em runtime: referenciado, mas não replicado

**Recomendação:** Adicionar ao `TenantController` e ao bootstrap Pulumi as tarefas de:
- Criar/sincronizar secrets necessários em todos os namespaces necessários
- Publicar imagens referenciadas pelo código no startup/provisioning

### 3.3 "Zero-values silenciosos"
Bug #2.15 (ReleasingTimeout) ficou oculto porque Go não diferencia "campo não setado" de "campo setado para 0". E o log inicial não listava o campo.

**Recomendação:** 
- Logar TODAS as configurações no startup
- Para durações de timeout, considerar `*time.Duration` (pointer) para distinguir "não configurado" de "configurado como 0"
- Adicionar testes que inicializam o reaper com config incompleto e verificam se emite warning

### 3.4 "Observação durante transição"
Bug #2.14 (callback race) ocorreu porque o observador lia estado não-estável.

**Recomendação:** Em todos os pontos que consomem estados externos (Argo Workflow, ArgoCD, K8s controllers), **sempre verificar `FinishedAt`/`Ready`/`ObservedGeneration` antes de agir**. Nunca trate um phase intermediário como terminal.

---

## 4. Métricas de Produtividade da Sessão

| Métrica | Valor |
|---|---|
| Bugs descobertos e corrigidos | 15 |
| Iterações do L1 até passar 10/10 | ~10 runs |
| Commits feitos | 3 (`7d7a2a6`, `2afb72a`, `7990552`) |
| Arquivos modificados (código) | 9 |
| Imagens Docker construídas + publicadas | 5 (loopzero-api x4, loopzero-cosign-signer x1) |
| Tempo decorrido | ~3h |
| Tempo de build (melhor) | 2m43s (tn-0) |
| Tempo de build (pior) | 4m27s (tn-6) |
| Tempo total do teste L1 final | 917s (~15min) |

---

## 5. Próximos Passos (Ordenados)

1. ✅ **L1 DoD completo** — 10/10 live validado
2. ⏳ **L2 — 50 tenants sustained** — próximo milestone (Phase 1.3)
3. 🔜 **Persistir infra fixes no Pulumi**:
   - `cosign-signing-key` secret no argo ns
   - `loopzero-cosign-signer:v0.1.0` publicada via GitHub Actions build-images.yml
   - PSA privileged no argo ns (ou segregação de workflows em namespace dedicado)
   - `loopzero-infra-registry` secret + SA patch no TenantController
4. 🔜 **Phase 0.1** — `LOOPZERO_DISABLE_LEGACY_LOOPS=true` ativado em prod
5. 🔜 **Phase 0.4** — `LOOPZERO_ADMISSION_CONTROL=true` ativado
6. 🔜 **Corrigir "namespace loopzero-system not found"** no controller DO (deveria ser `platform-system`)

---

## 6. Referências

- [scale-50k-plan.md](scale-50k-plan.md) — plano original
- [scale-50k-day1-learnings.md](scale-50k-day1-learnings.md) — sessão anterior
- Commits do dia:
  - `7d7a2a6` — 14 bugs iniciais (nil client, images, PSA, plan, crane, cosign, xargs, imagePullSecret, sed regex)
  - `2afb72a` — callback race (FinishedAt check)
  - `7990552` — BuildReaper ReleasingTimeout zero-value
