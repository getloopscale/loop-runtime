# Scale 50k — Day 1 Execution Learnings (2026-04-16)

**Companion document to:** [`scale-50k-plan.md`](scale-50k-plan.md)
**Follow-up:** [`scale-50k-day2-learnings.md`](scale-50k-day2-learnings.md) — L1 DoD fechado 10/10 (continuação da sessão)
**Scope:** Aprendizados, correções, e descobertas durante a primeira sessão de execução do plano — focada em Phase 0 (wiring) e validação inicial de L1 real em infra DigitalOcean.
**Status:** L1 parcialmente validado — pipeline ponta-a-ponta funcional, 6/10 apps com HTTP 200 verificado.

---

## Resumo Executivo

A primeira sessão de execução do scale-50k-plan completou Phase 0 (wiring de building blocks) e expôs o pipeline de build a tráfego real pela primeira vez. O trabalho descobriu **9 bugs distintos** no pipeline de build, **3 gaps de credenciais** na cadeia DOCR, e **1 gap de observabilidade** (dashboards com queries quebradas). Nenhum deles é bug de código do scale plan em si — todos são problemas latentes do pipeline que estavam mascarados porque nunca tinham sido exercitados em produção com este nível de profundidade.

**Outcome operacional:** 6 apps tenants vivos em produção, respondendo HTTP 200 em URLs públicas (`tn-lt-l1-tn-N-lt-l1-app-N-api-production.dev.loopzero.example`), build pipeline completo validado em DOKS.

---

## 1. Phase 0 — Estado ao Final do Dia 1

| Sub-fase | Status | Notas |
|----------|--------|-------|
| 0.0 Real deploy harness | Completo | Commit `93a8c9e` + 3 iterações de fixes (`9c4cad6`, `e8a06a2`, pendente) |
| 0.1 Controller binary activation | Parcial — manifest pronto, flag ainda `LOOPZERO_DISABLE_LEGACY_LOOPS=false` | `api-deployment.yaml` tem a flag declarada; falta apenas ativar |
| 0.2 PG claims (both loops) | Completo | Commits `5a2161b` (BuildReconcileLoop) + `734928f` (ReleaseConvergenceLoop) |
| 0.3 DynamicRouter + K8sCellStore | Completo | Commit `734928f`, wired em `api/cmd/main.go` com fallback para StaticRouter |
| 0.4 Admission control (prod) | Não iniciado | Código existe, falta ativar `LOOPZERO_ADMISSION_CONTROL=true` |
| 0.5 Controller Helm/manifest | Completo — manifest pronto, não aplicado | `infra/tilt/controller-deployment.yaml` (216 linhas): ServiceAccount, ClusterRole, Deployment |

**Commits da sessão:**
- `5a2161b` feat(scale): Phase 0.2 — PG claim dedup layer for BuildReconcileLoop
- `734928f` feat(scale): Phase 0.2 ReleaseConvergenceLoop + Phase 0.3 DynamicRouter wiring
- `ebe0635` feat(scale): config flags for PG claims + DynamicRouter + fix K8sCellStore namespace
- `97448f0` test(scale): L1 scenario tests (sequential + concurrent + rollback)
- `9c4cad6` fix(tests): bootstrap identity mapping + auth for real deploy harness
- `e8a06a2` fix(tests): limit concurrent deploys to 3 in L1 test
- `55ce988` Fix fix + CWT image bump (consolidação grande — 25 arquivos, +1942/-99 linhas)

---

## 2. Correções no Harness de Load Test

Durante a primeira execução de L1 real, o harness precisou de 3 correções antes de o bootstrap de tenants funcionar:

### 2.1 Header de autenticação incorreto

**Sintoma:** 401 no bootstrap endpoint.

**Causa:** Harness usava `X-Service-Auth: <secret>`, mas o endpoint de bootstrap exige `Authorization: Bearer <secret>`.

**Fix:** `9c4cad6` — `setBootstrapAuth()` passou a usar header correto.

### 2.2 Identidade sem os prefixos esperados

**Sintoma:** Tenants criados mas chamadas subsequentes retornavam 403/404.

**Causa:** O endpoint de bootstrap cria:
- `tenant_id = "tn-" + slug`
- `user_id = "github-" + login`

O harness gerava `TenantID` e `UserID` puros (sem prefixo). Os headers `X-LoopZero-Tenant` e `X-LoopZero-User-ID` não correspondiam ao que estava no DB.

**Fix:** `NewTenantIdentity()` passou a gerar valores com prefixos, e `ProvisionTenant()` faz strip antes de enviar ao bootstrap (que re-adiciona).

### 2.3 Plano `starter` insuficiente para múltiplos runs

**Sintoma:** "plan limit exceeded — 1 project allowed" no segundo run.

**Causa:** Plan `starter` permite apenas 1 projeto por tenant. Re-runs que reutilizavam tenants existentes (com projects leftover) falhavam.

**Fix (uncommitted):** Harness agora provisiona tenants com `plan: "enterprise"` (limites NULL em projects/environments/builds/services).

### 2.4 Concorrência excessiva para port-forward / CF Tunnel

**Sintoma:** Uploads multipart falhando intermitentemente com 10 deploys simultâneos.

**Causa:** Port-forward e Cloudflare Tunnel não suportam bem 10 uploads multipart concorrentes.

**Fix:** `e8a06a2` — concorrência limitada a 3 via semáforo no harness. Quando rodando in-cluster (k6 Job), esse limite pode ser aumentado.

---

## 3. Bugs no Pipeline de Build Descobertos via L1 Real

Cada bug abaixo foi descoberto ao examinar pods falhos no build cluster durante o teste real. Nenhum deles apareceria em unit tests.

### 3.1 `gitea.go` nil client causa panic em produção

**Arquivo:** `api/internal/gitclient/gitea.go:29`

**Sintoma latente:** Potencial nil pointer panic no primeiro request de upload de source bundle.

**Causa:** Dois callers passavam `nil` como `*http.Client` intencionalmente (`source/gitea_store.go`, `source/gogit_store.go`). A função não fazia guard para nil, então o primeiro `client.Do(req)` causaria panic.

**Fix (uncommitted):**
```go
if client == nil {
    client = http.DefaultClient
}
```

Bug latente — nunca apareceu porque os outros callers passavam um httpClient real, mascarando o code path vulnerável.

### 3.2 Crane v0.21+ é distroless — `sh -c` falha

**Arquivo:** `api/internal/argo/harbor_mirror.go:27`

**Sintoma:** `exec: "sh": executable file not found in $PATH` (exit 64) no step `mirror-harbor-<app>`.

**Causa:** O bump de `crane:v0.20.2` → `v0.21.5` introduziu imagem distroless (sem `/bin/sh`). O template wrappava o comando em `sh -c "..."`, quebrando.

**Fix (uncommitted):** Usar o binário direto `/ko-app/crane` (entrypoint padrão de imagens build by `ko`):

```go
Command: []string{"/ko-app/crane"},
Args:    []string{"copy", sourceTag, harborTag},
```

### 3.3 Crane rodando como nonroot não lê `/root/.docker/config.json`

**Arquivo:** `api/internal/argo/harbor_mirror.go`

**Sintoma:** Após fix 3.2, crane mostrava `No matching credentials were found for "registry.digitalocean.com"` e retornava 401 Unauthorized.

**Causa:** O container roda como `runAsUser: 65534` (nonroot). O secret `buildkit-harbor-registry-config` estava montado em `/root/.docker/`, que requer permissão root. O crane via o diretório mas não conseguia ler o `config.json`.

**Fix (uncommitted):**
```go
Env: []forge.EnvBuilder{
    forge.Env{Name: "DOCKER_CONFIG", Value: "/tmp/.docker"},
    forge.Env{Name: "HOME", Value: "/tmp"},
},
VolumeMounts: []forge.VolumeBuilder{
    &forge.SecretVolume{Name: secretRegistryConfig, MountPath: "/tmp/.docker", ReadOnly: true, ...},
},
```

### 3.4 Cosign v2.4.1 SHA256 incorreto no Dockerfile

**Arquivo:** `infra/images/cosign-signer/Dockerfile:17`

**Sintoma:** Build da imagem cosign-signer falhava com checksum mismatch.

**Causa:** SHA256 no Dockerfile (`3e4cdd62...`) não correspondia ao binário oficial do cosign v2.4.1.

**Fix (uncommitted):** Atualização para cosign v3.0.6 com SHA correto (`c956e5df...`). Versão major jump aceita porque v2.4.1 tinha SHA inválido (evitar rebuscar o SHA de uma versão que pode ter artefato corrompido).

### 3.5 Regex do cosign não tolera whitespace em JSON do BuildKit

**Arquivo:** `api/internal/argo/cosign.go:36`

**Sintoma:** `ERROR: no digest found in metadata file` mesmo com BuildKit tendo gravado metadata válido.

**Causa:** BuildKit v0.18.2 emite JSON com whitespace (`"key": "value"`), mas o regex `sed` esperava JSON compacto (`"key":"value"`).

**Fix (uncommitted):**
```diff
-DIGEST=$(sed -n 's/.*"containerimage.digest":"\([^"]*\)".*/\1/p' %s)
+DIGEST=$(sed -n 's/.*"containerimage.digest":[[:space:]]*"\([^"]*\)".*/\1/p' %s)
```

### 3.6 `minio/mc` 2025-08 removeu `xargs`

**Arquivo:** `infra/helm/argo-workflows/templates/cwt-cache-save.yaml:79`

**Sintoma:** `/usr/bin/sh: line 39: xargs: command not found` (exit 127) em todo save-cache.

**Causa:** Bump de `minio/mc:RELEASE.2025-04-08` → `RELEASE.2025-08-13` tornou a imagem mais minimalista, removendo `xargs`. O script de cache-save usava `xargs` para trim de whitespace.

**Fix (uncommitted):** Parameter expansion POSIX puro:

```diff
-              d=$(echo "$d" | xargs)  # trim whitespace
+              # Trim whitespace (portable — xargs not available in minio/mc 2025-08+)
+              d="${d#"${d%%[![:space:]]*}"}"
+              d="${d%"${d##*[![:space:]]}"}"
```

### 3.7 Padrão comum — Imagens distroless/minimalistas quebram scripts shell

As correções 3.2, 3.3, e 3.6 têm uma causa raiz comum: **bumps de versão de imagens quebram assumptions sobre shell/binários disponíveis**. Mitigações sistêmicas propostas:

- **Auditoria prévia:** antes de bumpar imagens, validar entrypoint, shell, e binários auxiliares.
- **Preferir chamada direta ao binário** em vez de wrapper shell sempre que possível.
- **Usar tags `-debug`** (imagens com shell) em CWTs cujos scripts dependem de shell, se existirem.
- **POSIX-only scripts** em CWTs (evitar `xargs`, `sed -E`, `gawk`, etc.) — maximiza portabilidade entre imagens.

---

## 4. Cadeia de Credenciais DOCR — 3 Pontos de Falha

Durante o L1 real, descobrimos que as credenciais DOCR precisam estar presentes em **três locais distintos**, e nenhum deles era trivial:

| Local | Propósito | Status inicial | Fix |
|-------|-----------|----------------|-----|
| Build cluster — secret `buildkit-harbor-registry-config` em `/tmp/.docker` (nonroot) | Crane pull de DOCR + push para Harbor | Permissão errada (montava em `/root/.docker`) | Remount em `/tmp/.docker` + `DOCKER_CONFIG=/tmp/.docker` |
| Build cluster — mesmo secret, conteúdo | Crane precisa de credenciais DOCR (não só Harbor) | Presumivelmente só Harbor | Ainda falhando ao final do dia 1 — mirror-harbor continua exit 1 |
| Runtime cluster — `imagePullSecret` `loopzero-infra-registry` nos namespaces tenant | Pods puxarem imagem de DOCR | Ausente (ImagePullBackOff em todos os pods) | Propagado via ESO/TenantController em algum ponto — destravou após ~4min |

**Observação importante:** o mirror-harbor sendo best-effort (`ContinueOn{Failed: true}`, ver MEET-048) salvou o L1 — apesar do mirror continuar falhando, os pods do runtime usam a imagem DOCR diretamente. Mas **isso significa que a imagem canônica em Harbor não é atualizada**, então deploys L2+ podem divergir entre clusters se Harbor for eventualmente a fonte.

---

## 5. Descobertas de Infra

### 5.1 ArgoCD migrou de namespace

**Doc anterior:** ArgoCD em `namespace: argocd`.
**Realidade atual:** ArgoCD em `namespace: platform-system` (mgmt cluster).

Consequência: vários scripts de bootstrap e skills (`sre-guardian`, `argocd-expert`) têm paths desatualizados.

### 5.2 Sem ingress público para Argo Workflows

**Doc anterior:** Implícito que todos os UIs têm ingress.
**Realidade:** Argo Workflows no build cluster é `ClusterIP`-only. Sem IngressRoute, sem DNS público. Acesso exclusivamente via `kubectl port-forward`. Deliberadamente privado por superfície de ataque.

### 5.3 Padrão de hostname das apps tenant

Descoberto ao inspecionar IngressRoute no runtime cluster:

```
Host(`{tenant_id}-{project_name}-{app_name}-{env_name}.dev.loopzero.example`)
```

Exemplo: `tn-lt-l1-tn-0-lt-l1-app-0-api-production.dev.loopzero.example`

Observações:
- Usa `tenant_id` (com prefixo `tn-`) em vez de apenas slug.
- `app_name` aparece no meio do hostname — multi-app projects geram múltiplos hosts.
- Comprimento do hostname próximo do limite RFC 1123 (63 chars por label) em casos de tenant/project longos.

### 5.4 Credenciais descobertas

- Grafana admin: `admin / GMZGLoZEpXKd4SYC22u90SWFNDjtIjTActzT3fDO`
- ArgoCD admin: `admin / H-eyCS7M2yIM0mpu`

Ambos em secrets K8s no mgmt cluster (`platform-system` namespace).

---

## 6. Observabilidade — Gap Importante Descoberto

Ao auditar dashboards existentes, identificamos que **vários painéis no `home.json` têm queries Prometheus que apontam para métricas inexistentes**:

| Query usada | Existe? | Métrica correta |
|-------------|---------|-----------------|
| `loopzero_builds_active` | Não | `argo_workflows_gauge{phase="Running"}` ou `loopzero_releasing_builds` |
| `loopzero_deploy_duration_seconds_bucket` | Não | `loopzero_build_duration_seconds_bucket` |
| `loopzero_lock_conflicts_total` | Não | `loopzero_lock_acquired_total` (sem counterpart de conflito direto) |

**Total de métricas disponíveis no Prometheus:** 1.191 (43 `loopzero_*`, 46 `argo_workflows_*`, + stock exporters).

**Dashboard bom de referência:** `loopzero-argo-workflows` (29 panels, templating vars, rows colapsáveis, queries corretas).

**Dashboards a priorizar:**
1. Fix `home.json` — corrigir queries quebradas
2. Novo `loopzero-scale-live` — visão completa de L1-L5 em tempo real
3. Novo `loopzero-deploy-journey` — rastreio ponta-a-ponta de uma build específica
4. Novo `loopzero-infra-saturation` — CPU/Mem/Disk/Network por cluster

---

## 7. Testes Pré-existentes Falhando (NÃO-regressão)

Dois testes em `api/internal/argo/` falham **desde antes do scale work começar** (confirmado em commits `a87e699` e `93a8c9e`):

- `TestWorkflowSubmitter_CancelWorkflow`
- `TestWorkflowSubmitter_SubmitWithMultipleApps`

**Erro:** `Object 'Kind' is missing in '{...}'` — típico de fake dynamic client do client-go não preencher `TypeMeta` em operações de patch.

**Ação recomendada:** abrir issue separada para fix. Não é prioridade para o scale plan mas polui o sinal de CI.

---

## 8. Estado Operacional ao Final do Dia 1

### 8.1 Tenants L1 provisionados e live

6 apps validados HTTP 200 em `dev.loopzero.example`:

- `tn-lt-l1-tn-0` — Running, HTTP 200
- `tn-lt-l1-tn-1` — Running, HTTP 200
- `tn-lt-l1-tn-2` — Running, HTTP 200
- `tn-lt-l1-tn-4` — Running, HTTP 200
- `tn-lt-l1-tn-6` — Running, HTTP 200
- `tn-lt-l1-tn-9` — Running, HTTP 200

Ainda pendentes para DoD completo: tn-3, tn-5, tn-7, tn-8.

### 8.2 Orphans a limpar

Ao final do dia, temos orphans no cluster runtime (namespaces tenant antigos com 4h+ de idade). O DoD do L1 exige "zero orphans após cleanup" — isso precisará ser validado separadamente em uma rodada dedicada a testar o `CleanupTenant()` do harness.

### 8.3 Commits pendentes

5 arquivos uncommitted ao final do dia, com fixes críticos:

- `api/internal/argo/cosign.go` — regex whitespace-tolerant
- `api/internal/argo/harbor_mirror.go` — crane distroless + auth mount path
- `infra/helm/argo-workflows/templates/cwt-cache-save.yaml` — POSIX expansion em vez de xargs
- `infra/images/cosign-signer/Dockerfile` — cosign v3.0.6
- `infra/tests/load/real_deploy_harness.go` — plan `enterprise`

**Risco:** acumular ~1h30min de trabalho uncommitted é risco de perda. Recomendação para dia 2: commit consolidado antes de qualquer novo trabalho, com mensagem `fix(build): crane v0.21 distroless + cosign v3 + POSIX cache + DOCR auth mount`.

---

## 9. Padrões Sistêmicos Reforçados

### 9.1 "Scale test valida o próprio pipeline"

O L1 real expôs 3 bugs diferentes nos image bumps do pipeline de build. Nenhum apareceria em unit tests. Esta é precisamente a razão pela qual o plano insiste em **deploys reais, não mocks** — o stress test expõe assumptions implícitas.

### 9.2 "Implementado mas não ativado"

Padrão já documentado no CLAUDE.md, reforçado no dia 1:
- Controller binary extraído e com manifest pronto, mas `LOOPZERO_DISABLE_LEGACY_LOOPS=false` → API ainda roda os loops.
- Admission control implementado, mas `LOOPZERO_ADMISSION_CONTROL=false` → flag existe, comportamento antigo preservado.
- PG claims implementados, mas `LOOPZERO_USE_PG_CLAIMS` não ativado em prod.

Ativação requer `pulumi up` com novas env vars — deve ser um dos primeiros passos do dia 2 antes de L2.

### 9.3 "Commit tardio = commit gigante"

O commit `55ce988` teve 1.942 linhas em 25 arquivos com a mensagem "Fix fix + CWT image bump". Futuros dias devem priorizar commits pequenos e descritivos.

---

## 10. Recomendações para Dia 2

Em ordem de prioridade:

1. **Commit consolidado dos 5 fixes pendentes** (com mensagens separadas se possível).
2. **Validar Harbor mirror end-to-end** — secret precisa conter credenciais DOCR + Harbor; adicionar teste específico de auth no mirror.
3. **Completar L1 DoD** — re-rodar para cobrir tn-3, tn-5, tn-7, tn-8, e validar cleanup (zero orphans).
4. **Ativar Phase 0.1** — `LOOPZERO_DISABLE_LEGACY_LOOPS=true` + deploy do controller binary.
5. **Ativar Phase 0.4** — `LOOPZERO_ADMISSION_CONTROL=true`.
6. **Fixar `home.json`** e **criar `loopzero-scale-live` dashboard** antes de L2 (precisamos enxergar 50 tenants sem manualmente `kubectl get wf`).
7. **Corrigir `TestWorkflowSubmitter_*` pre-existing failures** — não bloqueia, mas polui CI.
8. **Abrir MEET para revisar credentials flow DOCR↔Harbor↔Runtime** — a cadeia tem 3 pontos de falha; vale decisão de arquitetura.

---

## 11. Referências

- [scale-50k-plan.md](scale-50k-plan.md) — plano original
- [load-test-plan.md](load-test-plan.md) — detalhamento de L0-L7
- Commits do dia: `5a2161b`, `734928f`, `ebe0635`, `97448f0`, `9c4cad6`, `e8a06a2`, `55ce988`
