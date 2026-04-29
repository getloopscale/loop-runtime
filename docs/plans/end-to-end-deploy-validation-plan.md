# Plan: End-to-End Deploy Validation — Bootstrap completo de mgmt+build+runtime e teste `loopzero deploy`

> **Version 1.1 (CLOSED 2026-04-29 iter 12)** — Continuação direta do `infra-layered-bootstrap-plan.md` (CHG-2026-04-28-LAYERED-BOOTSTRAP). A iteração 22-30 do plano-pai validou apenas o cluster `mgmt` em k3d local (22/22 releases). Este plano fechou o gap honesto reconhecido pelo operator: **bootstrap completo dos 3 clusters** (`mgmt` ✅ + `build` ✅ + `runtime` ✅) **e teste end-to-end de `loopzero deploy`** percorrendo o caminho usuário→CLI→API→Argo Workflow→ArgoCD→browser HTTP 200.
>
> **Resultado iter 12**: a estimativa "5-15 novos bugs" subestimou a realidade — foram descobertos e corrigidos **24 bugs sistêmicos** (B59-B82, padrão Rule 10), todos com proteção permanente (Go regression test + chart/helmfile fix + entry na matriz). Evidência empírica final: `curl -k -H "Host: tn-e2etest-repotest-web-production.loopzero.local" https://172.18.0.10/index.html` retorna **HTTP 200** com o `index.html` real construído pelo BuildKit (324B, com `<title>LoopZero Repotest</title>`) e o JS bundle completo (140KB, `text/javascript`). Toda a cadeia produto está operacional em k3d local: CLI → API → Argo Workflow → BuildKit + cosign + syft → S3 (loopzero-static-assets bucket) → gitops push para Gitea → ArgoCD sync (com B80 repo-creds + B81 RBAC + B82 cluster destination) → workload Traefik IngressRoute → static-assets nginx-s3-gateway → MinIO → browser. Ver `CHANGELOG.md "iter 12"` para detalhes de cada bug.

## Context

### O que já existe (estado confirmado iter 30)

- **Cluster mgmt** (`k3d-loopzero-mgmt`): 22/22 helm releases, 51 pods Running, smoke-test PASS, loopzero-api `/api/v1/health=200`, gitea `/api/v1/version=200`, ArgoCD up, LoopZero CRDs instaladas, controllers reconciling.
- **Helmfiles existentes mas NÃO rodados**:
  - `infra/helmfile/build.helmfile.yaml.gotmpl` — 3 releases internas (argo-workflows, loki, promtail) + 5 helm repos.
  - `infra/helmfile/runtime.helmfile.yaml.gotmpl` — 13 releases (cert-manager, metrics-server, traefik, external-secrets, keda, keda-http-addon, policy-controller, admission-policies, promtail, cloudflared, cluster-issuer, rbac) + 8 helm repos.
- **Scripts existentes mas NÃO executados localmente**:
  - `generate-build-secrets.sh` — cria `cosign-signing-key` Secret no namespace `argo` do build cluster.
  - `generate-runtime-secrets.sh` — cria `cf-tunnel-credentials` Secret no namespace `cloudflared` do runtime cluster.
  - `wire-clusters.sh` — cross-cluster RBAC + ArgoCD register + scoped kubeconfigs (T2.4 do plano-pai).
- **Imagens DO Registry confirmadas**: `loopzero-api:REDACTED_SECRET_061`, `loopzero-tenant-controller:REDACTED_SECRET_061`, `loopzero-cloud:2bc062f`, `loopzero-clone-runner:v0.1.0`, `loopzero-packs-runner:REDACTED_SECRET_0612`, `loopzero-cosign-signer:v0.1.0`, `loopzero-syft-scanner:chg002b`, `harbor-init:*`.
- **Repo de teste disponível**: `https://github.com/getloopscale/loopzero-repotest` (validado E2E anteriormente, ver CLAUDE.md GitHub App auto-deploy 2026-04-08).
- **Memory budget**: 15GB total, 8GB livre (mgmt usa 4.7GB). Suficiente para +2 clusters com sizing reduzido.

### O que está quebrado

1. **Build cluster ausente** — sem ele, `loopzero deploy` não pode rodar Argo Workflows com BuildKit. Imagens não são construídas, nada é assinado por cosign, logs não vão pro Loki.
2. **Runtime/workload cluster ausente** — sem ele, pods de tenant não têm onde rodar com Traefik/cert-manager/KEDA/admission-policies.
3. **Cross-cluster wiring não testado** — `wire-clusters.sh` cria ClusterRole runtime-rbac-admin (B7), scoped kubeconfigs (B6 caData encoding), e registra clusters no ArgoCD. Esses são caminhos onde apareceram bugs em DOKS production (CHG-002a B6/B7) — nunca rodaram em k3d local.
4. **`loopzero deploy` nunca executado** — produto nunca foi exercitado end-to-end localmente. Apenas testes unitários passaram. O caminho real (CLI → API → state machine → bundle upload → S3 → Argo Workflow submit → BuildKit → cosign sign → Harbor push → manifests gen → gitops push to Gitea → ArgoCD sync → workload pod running → SSE event → CLI shows URL) **nunca foi validado em local**.

### Evidência motivadora

- Iter 30 (CHG-2026-04-28-LAYERED-BOOTSTRAP) descobriu **34 bugs sistêmicos** (B19-B58) executando bootstrap end-to-end real do mgmt cluster. Cada bug teria quebrado DOKS bootstrap fresh identicamente. Rule 10 provada **localmente**.
- Os 13 releases do runtime cluster (Traefik, cert-manager, KEDA, ESO, admission-policies, cloudflared, cluster-issuer) **nunca foram instalados** em qualquer cluster local. Padrão Rule 10 implica: vão ter bugs análogos a B19-B58 esperando descoberta.
- Os 3 releases do build cluster (argo-workflows, loki, promtail) **nunca foram instalados** em qualquer cluster local. Mesma situação.
- O `wire-clusters.sh` tem proteção contra B6 (caData encoding) e B7 (ClusterRole resources:["*"]) mas o script **nunca rodou** em ambiente onde se possa observar comportamento real.
- O `loopzero deploy` happy path tem 1079 testes CLI mockados + 41 packages Go testados, mas o **caminho integrado** nunca foi exercitado em k3d com 3 clusters reais.

### Status dos itens externos do plano-pai

| Item | Status | Bloqueador |
|---|---|---|
| T1.2 pulumi state cleanup S3 | Pendente | DO credentials (este plano não resolve) |
| T7.1 L3-50/L3-100 DOKS | Pendente | DO + ~6h (este plano substitui parcialmente com k3d) |
| T2 follow-up auto-sync ArgoCD | Pendente | governance meeting (este plano provê evidence para meeting) |

Este plano **substitui** parcialmente T7.1 trazendo validação E2E para local, e provê **evidência empírica** para futuro governance meeting de auto-sync.

## Objective

**Done**: usuário executa `loopzero deploy` em k3d local, vê pod do tenant respondendo HTTP 200 num URL via Traefik, e o caminho completo (CLI→API→Argo→ArgoCD) é programaticamente verificável via testes Go que rodam em sequência sem intervenção manual.

Specific measurable goals:

1. 3 clusters k3d operacionais (`loopzero-mgmt` ✅, `loopzero-build` ❌→✅, `loopzero-workload` ❌→✅) com sizing reduzido cabendo em 12GB RAM total.
2. **Build cluster**: 3+ helm releases deployed (argo-workflows + loki + promtail) com cosign-signing-key Secret no namespace `argo` (não placeholder).
3. **Runtime cluster**: 13 helm releases deployed (Traefik, cert-manager, KEDA, ESO, admission-policies, cloudflared, cluster-issuer + auxiliares) com `loopzero-platform-runtime-rbac-admin` ClusterRole tendo `resources: ["*"]`.
4. **wire-clusters.sh** executado com sucesso: ArgoCD lista 2 clusters registrados (build + runtime), 2 scoped kubeconfigs gerados, cross-cluster RBAC validado.
5. **loopzero deploy** executado com sucesso: tenant `e2e-test`, projeto `repotest` apontando para `getloopscale/loopzero-repotest`, build status `SUCCEEDED`, release status `ACTIVE`, workload pod 1/1 Running, HTTP probe via Traefik retorna 200.
6. **Smoke E2E test** Go criado e PASS: `TestE2EDeployHappyPath` que orquestra os passos 1-5 idempotentemente.
7. **Regression matrix expandida** com novos bugs descobertos (estimativa: 5-15 entries B59+).
8. **CI gate** atualizado para incluir o smoke E2E como check pós-helmfile (skip por default em PR; opt-in via `LOOPZERO_E2E_DEPLOY=1`).

## ADRs

### D1 — 3 clusters k3d separados em 1 máquina vs cluster único namespaced

**Decision:** Manter os 3 clusters físicos k3d separados (`loopzero-mgmt`, `loopzero-build`, `loopzero-workload`) mesmo em local, mirroring exatamente a topologia DOKS production.

**Rationale:** Cluster único com namespaces simularia a separação mas não exercita `wire-clusters.sh`, scoped kubeconfigs, ArgoCD multi-cluster register, ou network boundaries entre planos. Esses são exatamente os caminhos onde B6/B7 e potenciais bugs futuros vivem. Rule 10 (Local equals production) exige fidelidade topológica, não funcional.

**Consequences:** Memory pressure (~12GB de 15GB). Mitigado por sizing reduzido (1 server + 1 agent por build/workload, vs 1 server + 2 agents do mgmt). 3 contextos kubectl exigem disciplina (`--kube-context`). cross-cluster ingress requer `host.k3d.internal` ou loadbalancer routing — documentar no runbook.

### D2 — Sizing reduzido para build/workload (1+1) vs mgmt (1+2)

**Decision:** `loopzero-build` e `loopzero-workload` rodam com 1 server + 1 agent. Apenas mgmt mantém 1 server + 2 agents porque carrega 22 releases (51 pods).

**Rationale:** Build cluster carrega ~10 pods (argo-server + argo-controller + 2 BuildKit + Loki + Promtail). Workload cluster carrega ~25 pods (Traefik 2 replicas, cert-manager 3, KEDA 3, ESO 1, policy-controller 1, admission-policies CRDs, cloudflared 1, + tenant pods). Ambos cabem em 1+1 com `task setup` mode. Memory budget total: mgmt 5GB + build 2GB + workload 3GB = 10GB de 15GB.

**Consequences:** Em DOKS production, sizing é diferente (Sprint 8 v3.2 spec). Local NÃO é fielmente production em capacidade — apenas em **topology + behavior**. Documentar limitação no runbook: testes de capacidade (L3-50, L3-100) requerem DOKS real (T7.1 do plano-pai persiste).

### D3 — Imagens reais do DO Registry, não local builds

**Decision:** Build cluster usa imagens já publicadas (`loopzero-cosign-signer:v0.1.0`, `loopzero-clone-runner:v0.1.0`, `loopzero-packs-runner:REDACTED_SECRET_0612`, `loopzero-syft-scanner:chg002b`). Workload tenant usa imagens construídas pelo BuildKit local.

**Rationale:** Confirma proteção B31 (image tags pinned). Se as imagens não existem ou tags mudam, descobrimos isso localmente antes de DOKS bootstrap fresh. Para tenant workload: BuildKit é o produto sob teste — sua saída deve realmente ser construída + pushed para registry local k3d-loopzero-registry:5000.

**Consequences:** Requer `doctl auth` para pull do DO Registry (já configurado per memory). Tenant images vão para `k3d-loopzero-registry:5000` (registry local k3d), não DO Registry — `loopzero deploy` precisa configurar `registry.url` corretamente para apontar pro registry local (env var ou flag `--registry`).

### D4 — Standalone profile para 3-cluster topology vs dev profile

**Decision:** Usar profile `standalone` em todos os 3 helmfiles, com extensão para multi-cluster. Em standalone tradicional (iter 20), os 3 helmfiles colapsavam num único cluster. Aqui, mantemos `standalone` semantics (postgres in-cluster, certs self-signed, sem CF DNS real, sem DOKS managed services) mas em 3 clusters físicos.

**Rationale:** `dev` profile espera DO Managed Postgres, Cloudflare DNS real, DO Spaces — nada disso temos local. `standalone` é o profile self-hosting que mais aproxima a realidade. A extensão multi-cluster precisa ser adicionada aos helmfiles existentes, possivelmente via novo env value `clusterTopology: multi-cluster`.

**Consequences:** Profile `standalone` ganha sub-mode "multi-cluster" vs "single-cluster" original. Refactor dos environment files para suportar essa diferença. Em produção, multi-cluster + dev é sempre verdade (DOKS == multi-cluster + cloud-managed). Em standalone single-cluster (self-hosting) continua valendo o caminho simples.

### D5 — Test deploy via repo público real, não fake

**Decision:** Tenant de teste deploya `https://github.com/getloopscale/loopzero-repotest` (referenced no memory). NÃO usar repo sintético criado on-the-fly.

**Rationale:** Repo real foi validado em E2E anteriores (CLAUDE.md GitHub App auto-deploy 2026-04-08). Tem framework válido (Node.js), commit history, README. Repo sintético introduz bugs de "falso positivo" (passa mas não cobre realidade). Memory atinge isso explicitamente.

**Consequences:** Teste depende de github.com disponibilidade (acceptable trade-off — internet é prerequisite). Se o repo for archived ou alterado, smoke test quebra — adicionar verification step que valida HEAD do repo antes de deploy.

### D6 — Validação por SSE/HTTP probing vs UI manual

**Decision:** Smoke E2E test em Go usa SSE stream (`loopzero deploy --watch` saída JSON) + HTTP polling do endpoint workload + kubectl pod ready check. Sem screenshot, sem dashboard manual.

**Rationale:** CI-friendly. Reproduzível. Determinístico. Match com filosofia da Rule 4 (exit codes permanentes) e Rule 5 (`--json` é contrato). Permite que esse smoke vire CI gate gated em flag.

**Consequences:** Não cobre UX visual (dashboard, gráficos). Mas isso não é responsabilidade deste plano — é Sprint 9 territory.

### D7 — Bug discovery é success, não failure

**Decision:** Toda fase deste plano explicitamente espera descoberta de bugs (estimativa 5-15). Tasks têm "Expected new bugs" listados como **outcomes positivos**, não complicações.

**Rationale:** Iter 22-30 descobriu 34 bugs e isso foi declarado explicitamente como **valor produzido** (Rule 10 working). Repetir o framing aqui evita pressão para esconder bugs ou declarar "OK" sem investigação.

**Consequences:** Tempo total do plano é incerto. Se descobrir 5 bugs leves: 4-6h. Se descobrir 15 bugs profundos: 12-18h. O plano não tem timeline rígido, apenas DoDs.

### D8 — Cleanup intermediário entre phases

**Decision:** Após cada Phase, executar `task clean:incremental` (script novo) que remove pods Completed, releases failed, PVCs unbound. NÃO deletar clusters inteiros.

**Rationale:** Memory pressure em k3d local 15GB acumula state desnecessário (failed minio post-jobs, OOMKilled pods, helm releases failed). Limpeza incremental mantém budget viável sem perder state real (mgmt cluster permanece).

**Consequences:** Novo script `infra/scripts/clean-incremental.sh`. Documentar em runbook. Adicionar como hook entre fases.

### D9 — Gitea repo populado para ArgoCD apps L3 e tenants

**Decision:** Estender o trabalho de iter 27 (Gitea `loopzero-platform/charts`). Criar 2 repos adicionais: `loopzero-platform/argocd-bootstrap` (apps manifests para self-management) e `e2e-test/repotest-gitops` (tenant manifests gerados pelo loopzero-api durante deploy).

**Rationale:** ArgoCD sync requires Git source. Sem repos populados, Apps ficam "Unknown" (visto em iter 27). Tenant deploy escreve manifests num repo gitops por convention — esse caminho precisa funcionar end-to-end.

**Consequences:** 2 novos repos no Gitea local. Permissions: `loopzero` user é admin global. Token reuso (`gitea-admin.token` Secret). Cleanup script deve incluir delete dos repos.

## Dependency Graph

```
Phase 0 (Pre-flight + sizing) ──▶ Phase 1 (Build cluster) ──┐
                                                               ├──▶ Phase 3 (Wire clusters)
                                  Phase 2 (Runtime cluster) ──┘            │
                                                                            ▼
                                                           Phase 4 (LoopZero deploy E2E)
                                                                            │
                                                                            ▼
                                                           Phase 5 (Validation suite)
                                                                            │
                                                                            ▼
                                                           Phase 6 (Docs + governance)
```

**Parallelism:**
- Phase 1 e Phase 2 podem rodar em paralelo (clusters independentes, helm/helmfile apenas usa kube-context). Recomendado serial para facilitar debug e budget memory.
- Phase 3 é gate sequencial — exige ambos clusters up.
- Phase 4-6 sequenciais.

**Sequenciamento estrito:**
- Phase 0 → Phase 1 (cluster precisa existir)
- Phase 0 → Phase 2 (mesmo)
- Phase 1 + Phase 2 → Phase 3 (wire requer ambos)
- Phase 3 → Phase 4 (deploy precisa de wiring)
- Phase 4 → Phase 5 (validation refere ao deploy)
- Phase 5 → Phase 6 (docs refletem aprendizados)

---

## Phase 0: Pre-flight + Memory Budget

**Objective:** Garantir budget de memória, criar sizing reduzido, e adicionar profile `multi-cluster` aos environments antes de qualquer cluster novo.

### T0.1 — Cleanup pré-bootstrap + memory budget verification

#### Objective
Liberar memória de containers/imagens órfãos e validar que há ≥10GB disponível antes de criar 2 clusters novos.

#### Evidence
- `free -h` em iter 30 mostrou 15GB total / 8GB free / 0 swap.
- Docker mostra 20.85GB em images, 33% reclaimable (7GB livres).
- 358 build caches consumindo 15GB.
- Cluster mgmt usa 4.7GB (server 2.7GB + 2 agents).
- Adicionar 2 clusters com sizing reduzido = +5-7GB → tight mas viável após cleanup.

#### Files to edit
```
infra/scripts/clean-incremental.sh (NEW) — limpeza idempotente preserving cluster state
Taskfile.yml — adicionar task `cleanup:incremental`
docs/runbooks/end-to-end-deploy-validation.md (NEW) — runbook para essa validação
```

#### Deep file dependency analysis
- `clean-incremental.sh`: NEW. Remove docker dangling images, build cache (preservando cache layers úteis), pods Completed/Failed em todos namespaces, PVCs unbound > 1h, helm releases failed > 30min. NÃO toca em pods Running ou releases deployed. Idempotente: re-run sem efeito se já limpo.
- `Taskfile.yml`: ganha task `cleanup:incremental` apontando para script. Não substitui `task clean:all` (que deleta clusters). É degrau intermediário.
- `runbooks/end-to-end-deploy-validation.md`: NEW. Documenta todo o procedimento manual + automated equivalente. Referencia este plano.

#### Deep Dives
**Memory budget invariants (must be true antes de criar clusters):**
- `free -h | awk '/^Mem:/ { print $7 }'` ≥ 10G (available)
- `docker system df --format '{{.Type}}\t{{.Reclaimable}}' | grep "Build Cache"` reclaimable < 5GB
- `docker ps -q --filter "status=exited" | wc -l` < 5
- `kubectl --context k3d-loopzero-mgmt get pods -A --field-selector status.phase=Failed --no-headers | wc -l` < 3

**clean-incremental.sh algorithm:**
```
1. docker container prune -f --filter "until=1h"
2. docker image prune -f --filter "dangling=true"
3. docker builder prune -f --filter "until=24h" --keep-storage 5GB
4. for ctx in $(kubectl config get-contexts -o name); do
     kubectl --context $ctx delete pod -A --field-selector status.phase=Succeeded
     kubectl --context $ctx delete pod -A --field-selector status.phase=Failed
   done
5. echo "Memory after cleanup: $(free -h | awk '/^Mem:/ {print $7}')"
```

#### Tasks
1. Criar `infra/scripts/clean-incremental.sh` com algorithm acima.
2. `chmod +x` no script.
3. Adicionar `cleanup:incremental` ao Taskfile.yml (desc + cmds + summary).
4. Criar `docs/runbooks/end-to-end-deploy-validation.md` com seções: Prerequisites, Sequence (Phase 0-6), Troubleshooting, Memory budget table.
5. Executar `task cleanup:incremental` e verificar `free -h` ≥ 10G.
6. Documentar baseline memory no runbook.

#### TDD
```
RED:     test_clean_incremental_idempotent() — re-run produces no error and no extra cleanup
RED:     test_clean_incremental_preserves_running_pods() — pods Running/Pending NOT deleted
RED:     test_clean_incremental_validates_memory() — script asserts memory >= threshold or fails fast
GREEN:   Implement clean-incremental.sh with safety checks
REFACTOR: extract shared helpers to existing scripts/lib/
VERIFY:  bash infra/helmfile/scripts/test_helpers/test-clean-incremental.sh (NEW)
```

#### Acceptance Criteria
- [ ] `task cleanup:incremental` exits 0 idempotently.
- [ ] After cleanup, `free -h` shows ≥ 10G available.
- [ ] mgmt cluster pods Running unchanged (51 antes, 51 depois).
- [ ] Pods Completed/Failed em mgmt deleted (count == 0 após cleanup).
- [ ] Runbook documenta procedimento.

#### DoD
- [ ] Script committed, executável.
- [ ] Taskfile entry committed.
- [ ] Runbook seção Phase 0 completa.
- [ ] Test helper PASS.
- [ ] Pré-condição "≥10GB available" provada.

---

### T0.2 — k3d configs reduzidos para build + workload

#### Objective
Atualizar `infra/k3d/build-cluster.yaml` e `infra/k3d/workload-cluster.yaml` com sizing 1 server + 1 agent + memory limits per node.

#### Evidence
- Configs existem mas nunca foram usados (`k3d cluster list` mostrou só `loopzero-mgmt`).
- Build cluster carrega ~10 pods. Workload ~25 pods. Sizing 1+1 (k3s default) suporta isso.
- mgmt config (1+2) usa 4.7GB. Reduzido 1+1 ~ 2-3GB cada.

#### Files to edit
```
infra/k3d/build-cluster.yaml — reduzir agents 2→1, adicionar memory limits, port mapping para argo UI
infra/k3d/workload-cluster.yaml — reduzir agents 2→1, adicionar memory limits, port mapping para Traefik
infra/k3d/mgmt-cluster.yaml — adicionar comentário documentando que mgmt mantém 1+2 propositalmente
```

#### Deep file dependency analysis
- `build-cluster.yaml`: chave `agents:`, image `rancher/k3s:v1.32.13-k3s1` (igual mgmt), portmappings 30002/30093 etc. para argo UI + grafana cross-cluster Loki access. Adicionar `--kubelet-arg=eviction-hard=memory.available<200Mi` para evitar OOM kill agressivo.
- `workload-cluster.yaml`: similar. Portmappings 80/443 → Traefik LoadBalancer. Tenant ingress chega aqui.
- `mgmt-cluster.yaml`: Apenas comentário, sem mudança comportamental.

#### Deep Dives
**Why 1 server + 1 agent (not 2 agents)?**
- k3s server roda como controlplane + worker (default). Com 1 server + 0 agents também roda, mas sem isolation entre control plane e workloads.
- 1 server + 1 agent dá mínimo de 2 nodes para anti-affinity rules em charts HA-aware (cert-manager 3 replicas requer 3 nodes — vai consolidar 2 num node, OK).
- Memory budget: server ~1.5GB, agent ~1GB = ~2.5GB per cluster. 2 clusters = 5GB. + mgmt 4.7GB = 9.7GB / 15GB.

**Why memory eviction guard?**
- k3s default eviction is too lazy (memory.available<100Mi). Pods OOM antes de evict. Setting <200Mi gives buffer.

#### Tasks
1. Editar `build-cluster.yaml`: `agents: 1`, adicionar memory eviction args, validar portmapping Argo UI 30093.
2. Editar `workload-cluster.yaml`: idem + portmapping 80/443.
3. Adicionar comentário em `mgmt-cluster.yaml`.
4. Validate YAML structure: `python3 -c "import yaml; [yaml.safe_load(open(f)) for f in ['infra/k3d/build-cluster.yaml','infra/k3d/workload-cluster.yaml','infra/k3d/mgmt-cluster.yaml']]"`.

#### TDD
```
RED:     test_k3d_build_config_has_one_agent() — yaml.agents == 1
RED:     test_k3d_workload_config_has_one_agent() — yaml.agents == 1
RED:     test_k3d_workload_exposes_traefik_ports() — portmapping 80 + 443 present
RED:     test_k3d_build_exposes_argo_port() — portmapping 30093 present
GREEN:   Update YAMLs to satisfy assertions
REFACTOR: None expected
VERIFY:  cd infra/tests && go test ./unit/... -run TestK3dConfigs
```

#### Acceptance Criteria
- [ ] `infra/k3d/build-cluster.yaml` agents: 1, memory eviction set.
- [ ] `infra/k3d/workload-cluster.yaml` agents: 1, ports 80+443 mapped.
- [ ] YAML valid.
- [ ] Go tests pass.

#### DoD
- [ ] Configs committed.
- [ ] Go tests in `infra/tests/unit/k3d_configs_test.go` (NEW or extend) PASS.
- [ ] Sizing documentado no runbook.

---

### T0.3 — Profile `multi-cluster` em environment files

#### Objective
Estender `infra/helmfile/environments/standalone.yaml` com flag `clusterTopology: multi-cluster` e ajustar conditional renders dos 3 helmfiles para usá-lo.

#### Evidence
- Em iter 20, profile standalone foi desenhado para single-cluster (mgmt+build+runtime collapsed). Conditional `inClusterPostgres` distingue.
- Para 3 clusters físicos, alguns conditionals mudam: `installMetricsServer` deve ser FALSE no workload (k3d builtin), TRUE no build (cross-cluster Loki precisa metrics server externo? não — métricas vão pra prometheus mgmt).
- Helmfile renders precisam diferenciar single-cluster vs multi-cluster standalone.

#### Files to edit
```
infra/helmfile/environments/standalone.yaml — adicionar clusterTopology: single-cluster (default)
infra/helmfile/environments/standalone-multi.yaml (NEW) — variant com clusterTopology: multi-cluster
infra/helmfile/mgmt.helmfile.yaml.gotmpl — não modifica (mgmt é o mesmo nas 2 variants)
infra/helmfile/build.helmfile.yaml.gotmpl — adicionar conditional baseado em clusterTopology
infra/helmfile/runtime.helmfile.yaml.gotmpl — idem
```

#### Deep file dependency analysis
- `standalone.yaml`: ganha campo `clusterTopology: single-cluster` explícito (default).
- `standalone-multi.yaml`: NEW. Importa standalone.yaml + override `clusterTopology: multi-cluster`. Helmfile syntax permite `extends:`? Sim, via `bases:`. Validar.
- `build.helmfile.yaml.gotmpl`: ganha conditional `{{- if eq .Values.clusterTopology "multi-cluster" }}` para releases que só fazem sentido em build cluster separado (e.g., loki cross-cluster federation endpoint).
- `runtime.helmfile.yaml.gotmpl`: idem para releases que diferenciam (e.g., admission-policies só em workload, não em mgmt collapsed).

#### Deep Dives
**Helmfile env file semantics:**
- `helmfile -e standalone-multi` carrega `environments/standalone-multi.yaml` (full standalone vars + multi flag).
- `.Values.clusterTopology` acessível em `gotmpl` blocks.

**What changes between single-cluster and multi-cluster standalone?**
- single: postgres in-cluster (release in mgmt). multi: postgres in-cluster ainda em mgmt (build/runtime conectam via service DNS cross-cluster — exige NodePort ou Service externo).
- single: cosign-signing-key gerado em mgmt namespace argo. multi: gerado no build cluster namespace argo.
- single: cloudflared release em mgmt. multi: release em runtime/workload cluster.

**Cross-cluster service discovery (multi-cluster standalone):**
- k3d clusters não compartilham rede Kubernetes por default. Service `postgresql.loopzero-system.svc.cluster.local` em mgmt NÃO resolve a partir de build/runtime.
- Solução: NodePort + `host.k3d.internal:30432` (loadbalancer port) ou ExternalName service. Documentar escolha em ADR (escolha: NodePort + host.k3d.internal, mais simples).

#### Tasks
1. Editar `standalone.yaml` adicionando `clusterTopology: single-cluster`.
2. Criar `standalone-multi.yaml` com override.
3. Adicionar conditional renders nos 2 helmfiles (build, runtime) onde diferença existe.
4. Validar: `helmfile -f X -e standalone list` e `-e standalone-multi list` rendem sem erro.

#### TDD
```
RED:     test_standalone_default_is_single_cluster() — yaml has clusterTopology: single-cluster
RED:     test_standalone_multi_has_override() — yaml has clusterTopology: multi-cluster
RED:     test_helmfile_build_renders_in_both_topologies() — `helmfile list` exits 0 in both
RED:     test_helmfile_runtime_renders_in_both_topologies() — same
GREEN:   Implement env files + conditional blocks
REFACTOR: None
VERIFY:  task validate:helmfile-templates (must remain green)
```

#### Acceptance Criteria
- [ ] `helmfile -f mgmt -e standalone list` PASS (já passa hoje).
- [ ] `helmfile -f mgmt -e standalone-multi list` PASS.
- [ ] `helmfile -f build -e standalone-multi list` PASS.
- [ ] `helmfile -f runtime -e standalone-multi list` PASS.
- [ ] task validate:helmfile-templates PASS (552 K8s objects mantidos).

#### DoD
- [ ] env files committed.
- [ ] helmfile blocks updated.
- [ ] All 6 helmfile/env combinations render OK.
- [ ] Validation suite green.

---

## Phase 1: Build Cluster Bootstrap

**Objective:** Criar `loopzero-build` k3d cluster e instalar 3+ helm releases (argo-workflows, loki, promtail, + DO Registry pull secret + cosign-signing-key Secret real).

### T1.1 — Criar k3d loopzero-build + DO registry pull secret

#### Objective
Criar cluster `loopzero-build` com config T0.2, registrar como contexto kubectl, criar pull secret para DO Registry images (`loopzero-cosign-signer`, `loopzero-clone-runner`, `loopzero-packs-runner`, `loopzero-syft-scanner`).

#### Evidence
- Em iter 22, criar mgmt cluster levou 21 segundos. Build cluster com sizing menor deve ser similar.
- Imagens build cluster vêm de DO Registry (não local k3d-loopzero-registry, exceto tenant builds). Pull secret é prerequisite.
- `loopzero-registry-pull` Secret está no mgmt namespace `loopzero-system`. Build cluster precisa do seu próprio.

#### Files to edit
```
infra/k3d/build-cluster.yaml — já editado em T0.2
infra/scripts/bootstrap-cluster.sh (NEW or extend existing) — script idempotente para criar cluster + secret
```

#### Deep file dependency analysis
- `bootstrap-cluster.sh`: NEW. Args: `<cluster-role>` (mgmt|build|runtime). Faz: (1) `k3d cluster create --config infra/k3d/{role}-cluster.yaml`, (2) `kubectl create namespace argo` (build) ou `loopzero-system` (mgmt) ou `traefik` (runtime), (3) `doctl registry kubernetes-manifest --namespace <ns>` + apply. Idempotente: skip create se cluster existe.

#### Deep Dives
**Why script vs manual k3d?**
- Reproducibility: CI pode rodar.
- Idempotency: re-run não falha.
- Documentation: o script é o runbook executável.

**DO registry token expiration:**
- `doctl registry login` expira em 30 dias por default. Para CI/long-running tests, usar `--never-expire` ou rotation.

#### Tasks
1. Criar `bootstrap-cluster.sh` idempotente.
2. Executar `bash bootstrap-cluster.sh build`.
3. Verificar: `k3d cluster list | grep loopzero-build`, `kubectl --context k3d-loopzero-build get ns argo`, `kubectl --context k3d-loopzero-build -n argo get secret loopzero-registry-pull`.

#### TDD
```
RED:     test_bootstrap_cluster_idempotent() — re-run on existing cluster exits 0
RED:     test_bootstrap_cluster_creates_namespace() — namespace exists after script
RED:     test_bootstrap_cluster_creates_pull_secret() — secret has registry.digitalocean.com auth
GREEN:   Implement bootstrap-cluster.sh
REFACTOR: None
VERIFY:  bash infra/helmfile/scripts/test_helpers/test-bootstrap-cluster.sh (NEW)
```

#### Acceptance Criteria
- [ ] `k3d cluster list` shows `loopzero-build` running (1+1 servers/agents).
- [ ] `kubectl --context k3d-loopzero-build get ns` lists `argo`.
- [ ] `kubectl --context k3d-loopzero-build -n argo get secret loopzero-registry-pull -o jsonpath='{.data.\.dockerconfigjson}' | base64 -d | grep registry.digitalocean.com` matches.
- [ ] Memory `free -h` available ≥ 7GB after creation (was 8GB; ~1GB consumed by build).
- [ ] Script idempotent.

#### DoD
- [ ] Cluster loopzero-build up.
- [ ] Pull secret applied.
- [ ] Test helper PASS.
- [ ] Memory budget within target.

---

### T1.2 — generate-build-secrets.sh: cosign keypair real

#### Objective
Executar `generate-build-secrets.sh` no cluster build (não mgmt). Gera `cosign-signing-key` Secret no namespace `argo` com chave real (BEGIN ENCRYPTED SIGSTORE PRIVATE KEY) — não placeholder (regression check B1).

#### Evidence
- Iter 14 implementou `get_or_generate()` pattern garantindo chave persiste entre re-runs.
- Iter 22-30 nunca executou esse script no build cluster (não existia).
- B1 regression test (`regression-checks.sh test_b1`) hoje fails em standalone (ausência de cluster build) → após T1.2, em multi-cluster passa.

#### Files to edit
```
infra/helmfile/scripts/generate-build-secrets.sh — verify works against k3d-loopzero-build context
infra/helmfile/scripts/test_helpers/test-generate-build-secrets.sh — extend to cover real cluster (was mock)
```

#### Deep file dependency analysis
- `generate-build-secrets.sh`: existe. Cria cosign keypair via `cosign generate-key-pair` (binary cosign required). Aplica como Secret. Idempotent: re-run preserva chave existente.
- `test-generate-build-secrets.sh`: existe como mock-kubectl test (3 PASS). Extender para incluir real-cluster invocation behind flag `LOOPZERO_REAL_BUILD_CLUSTER=k3d-loopzero-build`.

#### Deep Dives
**Cosign install local:**
- Memory shows `cosign` is mentioned as optional prerequisite in CLAUDE.md. Verify `command -v cosign` antes de rodar.
- If absent: install via `go install github.com/sigstore/cosign/v2/cmd/cosign@v2.4.1` (~10s).

**Secret schema:**
- `cosign.key` (encrypted private key)
- `cosign.password` (passphrase)
- `cosign.pub` (public key)

#### Tasks
1. Verificar `cosign` instalado: `command -v cosign || go install github.com/sigstore/cosign/v2/cmd/cosign@v2.4.1`.
2. Executar `KUBECONFIG=$(kubectl --context k3d-loopzero-build config view --raw --minify) bash infra/helmfile/scripts/generate-build-secrets.sh standalone build`.
3. Verificar: `kubectl --context k3d-loopzero-build -n argo get secret cosign-signing-key -o jsonpath='{.data.cosign\.key}' | base64 -d | grep "BEGIN ENCRYPTED SIGSTORE PRIVATE KEY"`.
4. Estender test helper para invocação real (gated).

#### TDD
```
RED:     test_b1_cosign_real_key_in_build_cluster() — Go test reads Secret, asserts key starts with "BEGIN ENCRYPTED SIGSTORE PRIVATE KEY" (gated by LOOPZERO_E2E=1)
RED:     test_generate_build_secrets_idempotent() — re-run preserves same key
GREEN:   Verify script works against real k3d-loopzero-build context
REFACTOR: None
VERIFY:  LOOPZERO_REGRESSION_PROFILE=multi-cluster bash regression-checks.sh — B1 PASS expected
```

#### Acceptance Criteria
- [ ] `cosign-signing-key` Secret exists in namespace `argo` of build cluster.
- [ ] Key contains `BEGIN ENCRYPTED SIGSTORE PRIVATE KEY` literal.
- [ ] No `PLACEHOLDER` substring in any data.
- [ ] Re-run preserves identical key value.
- [ ] regression-checks.sh `test_b1_cosign_not_placeholder` PASS in multi-cluster profile.

#### DoD
- [ ] cosign installed locally.
- [ ] Secret created in build cluster.
- [ ] B1 regression PASS in multi-cluster.
- [ ] Test helper PASS.

---

### T1.3 — helmfile build sync: argo-workflows + loki + promtail

#### Objective
Executar `helmfile -f build.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-build sync`. 3 releases deployed, pods Running, Argo Workflows API responde, Loki responde, Promtail ingesting logs.

#### Evidence
- 3 releases internas no helmfile build (vs 22 mgmt).
- Smaller surface area but Rule 10: bugs vão emergir.
- Predicted bugs (from B19-B58 patterns):
  - **B59 prediction**: Argo Workflows chart usa imagem que pode ter sido afetada pela "Bitnami legacy migration" (verificar no helm template).
  - **B60 prediction**: Loki chart cria PVC com storageClass específico (mesmo padrão B20).
  - **B61 prediction**: Promtail tem ServiceMonitor (mesmo B23).
  - **B62 prediction**: Loki retention/limits podem ser default size (mesmo B5 mas pode ser que precise mais agressivo em local).

#### Files to edit
```
infra/helmfile/build.helmfile.yaml.gotmpl — fix bugs descobertos (B59+)
infra/helm/argo-workflows/values.yaml — possíveis overrides
infra/helm/loki/values.yaml — possíveis overrides
infra/helm/promtail/values.yaml — possíveis overrides
```

#### Deep file dependency analysis
- `build.helmfile.yaml.gotmpl`: define 3 releases + needs ordering. Bugs típicos: dependency missing, wrong namespace, wrong image tag.
- `argo-workflows/values.yaml`: configura workflowController + server. Pode precisar `image.repository` override se chart default broken.
- `loki/values.yaml`: já tem `max_global_streams_per_user: 100000` (B5 protection). Mas pode ter outros issues.
- `promtail/values.yaml`: scrape config. Cross-cluster log ship requires Loki endpoint accessible (NodePort).

#### Deep Dives
**Argo Workflows architecture in build cluster:**
- workflow-controller: orchestrates Workflows.
- argo-server: serves UI + API + GRPC.
- Pods rodam workflows com BuildKit + cosign-signer + clone-runner + packs-runner sidecars.
- Workflow templates ficam no repo Gitea (apontado pela Workflow CRD `templateRef`).

**Loki cross-cluster log shipping:**
- Promtail (em build) ships pra Loki (em build). OK.
- mgmt + workload Promtail também shippa pra Loki em build via NodePort. Esse é o caminho que `promtail/values.yaml` em mgmt+workload precisa configurar com endpoint correto.

**Predicted bug investigation strategy:**
- Run `helmfile sync` — observe which releases fail.
- For each fail: `kubectl describe pod`, `kubectl logs`, identify root cause.
- Apply fix in values.yaml. Re-sync.
- Document each new bug as B59, B60, ... in CHANGELOG.

#### Tasks
1. Run `helmfile -f infra/helmfile/build.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-build sync --concurrency 2`.
2. Wait for completion. Capture log to `/tmp/build-sync-1.log`.
3. Investigate failures (kubectl describe + logs).
4. Fix each bug in values.yaml or helmfile.
5. Re-sync. Repeat until 3+ releases deployed.
6. Verify: `helm --kube-context k3d-loopzero-build list -A` shows 3 releases. `kubectl --context k3d-loopzero-build get pods -A | grep -v Running | wc -l` returns 0 (excluding header).
7. Argo Workflows API check: `kubectl --context k3d-loopzero-build -n argo run curl-test --rm -i --restart=Never --image=alpine/k8s:1.32.5 -- curl -sf http://argo-server.argo.svc:2746/api/v1/version`.
8. Loki ready check: `kubectl --context k3d-loopzero-build -n monitoring run curl-test --rm -i --restart=Never --image=alpine/k8s:1.32.5 -- curl -sf http://loki.monitoring.svc:3100/ready`.
9. Promtail healthy: `kubectl --context k3d-loopzero-build -n monitoring get ds promtail -o jsonpath='{.status.numberReady}/{.status.desiredNumberScheduled}'` returns `2/2` (1 server + 1 agent).

#### TDD
```
RED:     test_build_sync_three_releases() — helm list returns >= 3 deployed
RED:     test_argo_workflows_api_responds() — /api/v1/version returns 200
RED:     test_loki_ready_endpoint() — /ready returns 200
RED:     test_promtail_daemonset_ready() — numberReady == desiredNumberScheduled
GREEN:   Run sync, fix bugs encountered, re-sync until passing
REFACTOR: Document new bugs B59+ in CHANGELOG and matrix
VERIFY:  bash infra/helmfile/scripts/health-check.sh build (existing script)
```

#### Acceptance Criteria
- [ ] `helm --kube-context k3d-loopzero-build list -A` shows 3 releases (argo-workflows, loki, promtail).
- [ ] All build pods 1/1 Ready.
- [ ] Argo API responds 200.
- [ ] Loki /ready 200.
- [ ] Promtail DaemonSet 2/2.
- [ ] Each new bug discovered registered as B59+ in CHANGELOG + regression matrix + Go regression test.

#### DoD
- [ ] All 3 build releases deployed.
- [ ] Health endpoints respond.
- [ ] New bugs documented + tested.
- [ ] Memory budget still within target (≥ 5GB free).

---

## Phase 2: Runtime/Workload Cluster Bootstrap

**Objective:** Criar `loopzero-workload` k3d cluster e instalar 13 helm releases (cert-manager, traefik, KEDA, ESO, admission-policies, cloudflared, cluster-issuer, etc).

### T2.1 — Criar k3d loopzero-workload + pull secret

#### Objective
Igual T1.1 mas para workload cluster.

#### Evidence
- Workload cluster carrega 13 releases vs 3 build. Maior surface = mais bugs.
- Predictable patterns: B19-B58 já mapeiam 90% dos bug-types.

#### Files to edit
```
infra/k3d/workload-cluster.yaml — já editado T0.2
infra/scripts/bootstrap-cluster.sh — já feito T1.1 (multi-role)
```

#### Deep file dependency analysis
Reuso do script T1.1 com `bash bootstrap-cluster.sh workload`.

#### Deep Dives
**Namespace bootstrap order:**
- workload cluster precisa namespaces: `traefik`, `cert-manager`, `cloudflared`, `monitoring`, `loopzero-tenants` (target para tenant deploys).

#### Tasks
1. `bash bootstrap-cluster.sh workload`.
2. Verify cluster running.
3. Verify pull secrets in relevant namespaces.

#### TDD
Mesmos testes de T1.1 mas para context k3d-loopzero-workload.

#### Acceptance Criteria
- [ ] `k3d cluster list` shows `loopzero-workload`.
- [ ] Memory still ≥ 4GB free.
- [ ] All required namespaces created.

#### DoD
- [ ] Cluster up.
- [ ] Namespaces ready.
- [ ] Pull secrets applied.

---

### T2.2 — generate-runtime-secrets.sh: CF tunnel credentials

#### Objective
Gerar `cf-tunnel-credentials` Secret no namespace `cloudflared` do workload cluster. Em standalone local (sem CF real), Secret pode conter token placeholder válido — mas marcado como TEST_ONLY.

#### Evidence
- generate-runtime-secrets.sh existe. Em prod, `LOOPZERO_CF_TUNNEL_TOKEN` vem de Pulumi output (real CF Tunnel resource). Em standalone local, não temos CF Tunnel real.
- Solução: standalone fallback similar a B33 (postgres) e B36 (kubeconfig synthetic).

#### Files to edit
```
infra/helmfile/scripts/generate-runtime-secrets.sh — adicionar fallback local
infra/helm/cloudflared/values.yaml — possíveis ajustes para mode "test"
```

#### Deep file dependency analysis
- `generate-runtime-secrets.sh`: ler env var, fallback para token sintético em standalone (válido formato mas sem CF Tunnel real).
- `cloudflared/values.yaml`: pode precisar `mode: test` que faz cloudflared exit gracefully em vez de crash.

#### Deep Dives
**Cloudflared without real CF Tunnel:**
- cloudflared binary tenta dial pra CF edge. Sem CF Tunnel real, falha.
- Options: (a) deployment with `replicas: 0` em standalone, (b) sleep-infinity sidecar, (c) mock token + accept failure (pod CrashLoop é "expected" em standalone).
- **Decision**: Option (a) replicas: 0 em standalone. Documentar B63 prediction.

#### Tasks
1. Adicionar standalone fallback em `generate-runtime-secrets.sh`.
2. Adicionar conditional `replicas: 0` em cloudflared values quando standalone.
3. Run script + verify Secret + verify cloudflared pod count.

#### TDD
```
RED:     test_generate_runtime_secrets_standalone_fallback() — Secret created with test token
RED:     test_cloudflared_zero_replicas_in_standalone() — deployment.replicas == 0
GREEN:   Implement fallback + conditional
REFACTOR: None
VERIFY:  helm template cloudflared -f values.yaml --set clusterTopology=standalone
```

#### Acceptance Criteria
- [ ] Secret cf-tunnel-credentials exists.
- [ ] cloudflared deployment.replicas == 0 in standalone.
- [ ] Re-run idempotent.

#### DoD
- [ ] Script handles standalone profile.
- [ ] Cloudflared not crash-looping (because not deployed).
- [ ] Documented as known limitation (no CF Tunnel locally).

---

### T2.3 — helmfile runtime sync: 13 releases

#### Objective
Sync 13 releases no workload cluster. Predicted bugs (B64-B75 estimate).

#### Evidence
- 13 releases é a maior superfície deste plano. Bugs esperados:
  - cert-manager precisa de webhook readiness (race conditions).
  - Traefik pode ter conflitos de port (k3d tem traefik builtin? mgmt config disable=traefik).
  - KEDA HTTP add-on requires interceptor service in correct namespace.
  - admission-policies precisa policy-controller já running (ordering).
  - cluster-issuer requer cert-manager up.
  - rbac chart precisa role aggregation working.

#### Files to edit
```
infra/helmfile/runtime.helmfile.yaml.gotmpl — bug fixes
infra/helm/cert-manager/values.yaml — overrides
infra/helm/traefik/values.yaml — overrides (e.g., disable internal LoadBalancer if k3d-loopzero-workload tem builtin)
infra/helm/keda/values.yaml — pode precisar
infra/helm/external-secrets/values.yaml — pode precisar
infra/helm/admission-policies/values.yaml — disable se policy-controller fail
infra/helm/cluster-issuer/values.yaml — self-signed em standalone (não Let's Encrypt sem DNS real)
```

#### Deep file dependency analysis
Cada chart pode requerer overrides standalone-specific. Padrão Rule 10: descobrir + corrigir.

#### Deep Dives
**ClusterIssuer in standalone:**
- Let's Encrypt requires DNS-01 ou HTTP-01 challenge. Local k3d sem DNS público → ambos falham.
- Solução: `selfsigned-issuer` em standalone, `letsencrypt-staging` em dev/prod.
- Detected as B66 prediction.

**Traefik vs k3d builtin:**
- mgmt cluster yaml tem `--disable=traefik`. workload-cluster.yaml já idem? Verificar T0.2.
- Se não, Traefik chart conflict em port 80.

**Admission policies enforcement mode:**
- B16 protection: warn-mode policy `loopzero-images-warn` aplicado por default. Em standalone sem cosign verification (Sprint 9 requisite), warn-only é correto.

#### Tasks
1. Run `helmfile -f infra/helmfile/runtime.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-workload sync --concurrency 2`.
2. Iterate bug discovery + fix loop até 13 releases deployed.
3. Verify health endpoints:
   - cert-manager webhook: `kubectl --context k3d-loopzero-workload -n cert-manager get pods | grep webhook` 1/1.
   - Traefik: HTTP 80 reachable from host: `curl -sf http://localhost/dashboard/`.
   - KEDA: `kubectl --context k3d-loopzero-workload -n keda get pods` all Ready.
   - admission-policies: `kubectl --context k3d-loopzero-workload get clusterimagepolicy` returns warn-mode.

#### TDD
```
RED:     test_runtime_sync_13_releases() — helm list returns >= 13 deployed
RED:     test_cert_manager_webhook_ready() — webhook deployment 1/1
RED:     test_traefik_serves_dashboard() — HTTP 200 on /dashboard/
RED:     test_keda_operator_ready() — all KEDA pods Ready
RED:     test_admission_policies_warn_mode() — ClusterImagePolicy.spec.mode == "warn"
RED:     test_cluster_issuer_selfsigned_in_standalone() — issuer is selfsigned
GREEN:   Run sync, fix bugs encountered, re-sync until passing
REFACTOR: Document new bugs B64+ in CHANGELOG and matrix + Go regression tests
VERIFY:  bash infra/helmfile/scripts/health-check.sh runtime
```

#### Acceptance Criteria
- [ ] 13 releases deployed.
- [ ] All workload pods Ready (or replicas:0 for cloudflared).
- [ ] cert-manager webhook responding.
- [ ] Traefik routing.
- [ ] KEDA operator running.
- [ ] admission-policies in warn mode.
- [ ] cluster-issuer selfsigned in standalone.
- [ ] Each new bug documented as B64+.

#### DoD
- [ ] All runtime releases deployed.
- [ ] Health endpoints validated.
- [ ] New bugs in CHANGELOG + matrix + Go tests.
- [ ] Memory ≥ 2GB free.

---

## Phase 3: Cross-Cluster Wiring

**Objective:** Run `wire-clusters.sh` para registrar build + workload no ArgoCD do mgmt, criar scoped kubeconfigs (B6 protection), aplicar runtime ClusterRole (B7 protection).

### T3.1 — wire-clusters.sh: ArgoCD register + scoped kubeconfigs

#### Objective
Executar script `wire-clusters.sh` que: (1) cria scoped kubeconfigs em build + workload (B6 caData encoding), (2) cria ClusterRole `loopzero-platform-runtime-rbac-admin` em workload com `resources: ["*"]` (B7), (3) registra clusters no ArgoCD do mgmt.

#### Evidence
- Script existe (T2.4 do plano-pai).
- Nunca executado em ambiente real até iter 30.
- B6 + B7 são bugs reais de DOKS production CHG-002a — protection é via tests (`test_b6_cadata_encoding`, `test_b7_rbac_cluster_scoped`) mas runtime path nunca exercitado em k3d.

#### Files to edit
```
infra/helmfile/scripts/wire-clusters.sh — verify works against k3d, fix bugs
infra/helmfile/scripts/lib/argocd-register.sh — idem
infra/helmfile/scripts/lib/scoped-kubeconfig.sh — idem
```

#### Deep file dependency analysis
- `wire-clusters.sh`: orchestrator. Calls `lib/scoped-kubeconfig.sh` + `lib/argocd-register.sh` para cada cluster (build, runtime).
- `lib/scoped-kubeconfig.sh`: cria service account, ClusterRole, kubeconfig com tokenFile + caData encoding (B6).
- `lib/argocd-register.sh`: aplica Secret type=cluster no namespace argocd do mgmt.

#### Deep Dives
**B6 caData encoding correctness:**
- B6 was: caData was base64-encoded twice (PEM was b64 once, then YAML b64 ate it). Fix: single encoding.
- Verification: `kubectl --context k3d-loopzero-mgmt -n argocd get secret <cluster-secret> -o jsonpath='{.data.config}' | base64 -d | jq .tlsClientConfig.caData | base64 -d` should output `-----BEGIN CERTIFICATE-----`.

**B7 ClusterRole resources:**
- B7 was: ClusterRole had specific resources list, missing some. Fix: wildcard `["*"]`.
- Verification: `kubectl --context k3d-loopzero-workload get clusterrole loopzero-platform-runtime-rbac-admin -o jsonpath='{.rules[0].resources}'` returns `["*"]`.

**ArgoCD register API:**
- Secret type cluster has `name`, `server`, `config` fields. `config` is JSON with bearerToken or tlsClientConfig.
- ArgoCD detects the Secret via informers (`argoproj.io/secret-type=cluster` label).

#### Tasks
1. Set env: `LOOPZERO_MGMT_KUBECONFIG=$(k3d kubeconfig get loopzero-mgmt)`, `LOOPZERO_BUILD_KUBECONFIG=$(k3d kubeconfig get loopzero-build)`, `LOOPZERO_RUNTIME_KUBECONFIG=$(k3d kubeconfig get loopzero-workload)`.
2. Run `bash infra/helmfile/scripts/wire-clusters.sh standalone-multi`.
3. Iterate bug fix loop.
4. Verify:
   - `kubectl --context k3d-loopzero-mgmt -n argocd get secrets -l argocd.argoproj.io/secret-type=cluster` returns 2 (build + runtime).
   - For each, verify caData decodes to valid PEM.
   - `kubectl --context k3d-loopzero-workload get clusterrole loopzero-platform-runtime-rbac-admin -o jsonpath='{.rules}'` shows `resources:["*"]`.

#### TDD
```
RED:     test_wire_clusters_argocd_has_two_clusters() — ArgoCD lists build + runtime
RED:     test_wire_clusters_cadata_decodes_pem() — caData is valid PEM (B6)
RED:     test_wire_clusters_runtime_rbac_wildcard() — ClusterRole has resources:["*"] (B7)
GREEN:   Run wire-clusters.sh, fix bugs encountered
REFACTOR: Document new bugs Bxx in CHANGELOG and matrix
VERIFY:  LOOPZERO_REGRESSION_PROFILE=multi-cluster bash regression-checks.sh — B6 + B7 PASS expected
```

#### Acceptance Criteria
- [ ] ArgoCD lists 3 clusters (in-cluster + build + runtime).
- [ ] Both registered cluster Secrets have valid caData.
- [ ] Workload cluster has loopzero-platform-runtime-rbac-admin ClusterRole with wildcard.
- [ ] B6 + B7 regression PASS in multi-cluster profile.

#### DoD
- [ ] wire-clusters.sh executed successfully.
- [ ] All 3 clusters known to ArgoCD.
- [ ] Regression B6/B7 PASS.

---

### T3.2 — Wiring ConfigMap (loopzero-platform-topology) + cross-cluster service DNS

#### Objective
Aplicar `create-wiring-configmap.sh` para gerar `loopzero-platform-topology` ConfigMap consumido por loopzero-api, controllers, loopzero-cloud. Em multi-cluster standalone, contém endpoints NodePort de cada cluster.

#### Evidence
- T1.4 do plano-pai: wiring object migrated from Pulumi outputs to ConfigMap.
- loopzero-api consume via `envFrom configMapRef: loopzero-platform-topology`.
- Em multi-cluster local, endpoints precisam apontar para `host.k3d.internal:<NodePort>` em vez de service DNS interno.

#### Files to edit
```
infra/helmfile/scripts/create-wiring-configmap.sh — extend for multi-cluster local
infra/helm/loopzero-api/values.yaml — verify wiringConfigMap reference
```

#### Deep file dependency analysis
- `create-wiring-configmap.sh`: gera ConfigMap com keys: ARGO_API_ENDPOINT, GITEA_API_ENDPOINT, HARBOR_API_ENDPOINT, PROMETHEUS_API_ENDPOINT, etc. Em multi-cluster local: substituir DNS interno por host.k3d.internal:NodePort.
- `loopzero-api/values.yaml`: já tem `wiringConfigMap: loopzero-platform-topology`.

#### Deep Dives
**Cross-cluster service discovery via host.k3d.internal:**
- k3d injects `host.k3d.internal` resolving to docker host gateway. Combined with NodePort exposure, allows cross-cluster service access.
- Build cluster Argo API: NodePort 30093 → `host.k3d.internal:30093` from mgmt.
- Workload Traefik: NodePort 30080 → `host.k3d.internal:30080`.

#### Tasks
1. Extend `create-wiring-configmap.sh` with multi-cluster URL builder logic.
2. Run script.
3. Verify ConfigMap content reflects cross-cluster URLs.
4. Restart loopzero-api to pick up new ConfigMap.

#### TDD
```
RED:     test_wiring_configmap_has_multi_cluster_urls() — keys present, values use host.k3d.internal
RED:     test_loopzero_api_picks_up_wiring() — pod logs show "Argo API: http://host.k3d.internal:30093"
GREEN:   Extend script + verify
REFACTOR: None
VERIFY:  kubectl describe configmap loopzero-platform-topology
```

#### Acceptance Criteria
- [ ] ConfigMap exists with expected keys.
- [ ] URLs point to host.k3d.internal in standalone-multi.
- [ ] loopzero-api restarted and reading correctly.

#### DoD
- [ ] ConfigMap applied.
- [ ] loopzero-api logs confirm cross-cluster connectivity.

---

## Phase 4: LoopZero Deploy E2E

**Objective:** Executar caminho completo `loopzero login` → `loopzero init` → `loopzero deploy` com tenant `e2e-test` e repo `getloopscale/loopzero-repotest`. Pod tenant 1/1 Running, HTTP 200 retornado via Traefik.

### T4.1 — loopzero login (Ory Kratos OAuth flow)

#### Objective
Autenticar usuário test via `loopzero login`. Token salvo em `~/.loopzero/auth.json`. Subsequent commands usam o token.

#### Evidence
- Ory Kratos + Hydra deployed em mgmt (iter 23).
- Usuário admin `loopzero` criado (iter 27).
- `loopzero login` flow nunca testado E2E em local.

#### Files to edit
```
nenhum (consumir CLI existente)
```

#### Deep file dependency analysis
- `cli/src/commands/login.ts`: OAuth2 device flow. Open browser → user approves → callback → token stored.
- Em CI/headless: usar `--ci` mode + service-account token.

#### Deep Dives
**Headless login for E2E:**
- CLI deve suportar env var `LOOPZERO_AUTH_TOKEN` para skip OAuth flow.
- Verify: `loopzero --help | grep -E "AUTH_TOKEN|--token"`.
- If not present: extend CLI to accept token from env.

#### Tasks
1. Verify login flow with browser (manual one-time).
2. Extract token from `~/.loopzero/auth.json`.
3. Set as env var for subsequent automated steps.
4. (Optional) Extend CLI to accept env var if missing.

#### TDD
```
RED:     test_loopzero_login_via_env_token() — `loopzero whoami` returns user with LOOPZERO_AUTH_TOKEN set
GREEN:   Verify CLI supports or extend
REFACTOR: None
VERIFY:  loopzero whoami --json | jq -e .user.email
```

#### Acceptance Criteria
- [ ] User authenticated.
- [ ] `loopzero whoami` returns valid user.
- [ ] Token usable in subsequent commands.

#### DoD
- [ ] Login flow functional.
- [ ] Token stored and reusable.

---

### T4.2 — loopzero init: project + loopzero.yaml

#### Objective
Run `loopzero init` cria project `repotest`, gera `loopzero.yaml` com framework Node.js (detected from repo).

#### Evidence
- `loopzero init` interactive but supports `--non-interactive` flag.
- Detects framework via `monorepo-validator` (30 frameworks supported per CLAUDE.md).

#### Files to edit
```
/tmp/e2e-test-workspace/repotest/ (NEW workspace) — clone do repo + loopzero.yaml
```

#### Deep file dependency analysis
- Workspace dir é cloned from `getloopscale/loopzero-repotest`.
- `loopzero init --non-interactive --framework node --port 3000` cria `loopzero.yaml`.

#### Tasks
1. `git clone https://github.com/getloopscale/loopzero-repotest /tmp/e2e-test-workspace/repotest`.
2. `cd /tmp/e2e-test-workspace/repotest && loopzero init --non-interactive`.
3. Verify `loopzero.yaml` exists with framework detected.

#### TDD
```
RED:     test_loopzero_init_creates_yaml() — file exists after init
RED:     test_loopzero_yaml_detects_framework() — yaml has framework: node
GREEN:   Run init in workspace
REFACTOR: None
VERIFY:  loopzero init --help
```

#### Acceptance Criteria
- [ ] loopzero.yaml created.
- [ ] Framework correctly detected.
- [ ] Project registered in API (loopzero-api DB has row).

#### DoD
- [ ] loopzero init successful.
- [ ] Project visible in `loopzero project list`.

---

### T4.3 — loopzero deploy happy path

#### Objective
Run `loopzero deploy --watch --json` em workspace. Validate caminho completo:
1. CLI bundle upload → S3 (MinIO em mgmt).
2. API submits Argo Workflow para build cluster.
3. Workflow runs: clone → BuildKit build → cosign sign → Harbor push.
4. API generates K8s manifests (Deployment + Service + IngressRoute).
5. API pushes to Gitea repo `e2e-test/repotest-gitops`.
6. ArgoCD detects + syncs to workload cluster.
7. Pod tenant Running 1/1.
8. SSE event "RELEASE_ACTIVE" emitted.
9. CLI returns URL: `https://repotest-e2e-test.local:443`.
10. HTTP probe 200.

#### Evidence
- This is the **product** being tested. End-to-end.
- Each step has multiple known bug classes from B19-B58.

#### Files to edit
```
nenhum (CLI + API existentes)
infra/tests/e2e/deploy_e2e_test.go (NEW) — orchestration test
```

#### Deep file dependency analysis
- `deploy_e2e_test.go`: NEW Go test gated by LOOPZERO_E2E_DEPLOY=1. Sets up workspace, runs commands, validates final state.

#### Deep Dives
**Predicted failure modes (Rule 10 — bugs gonna emerge):**
- Build cluster reachability from API (cross-cluster gRPC to argo-server).
- Image push to k3d-loopzero-registry (cross-cluster network).
- Manifest gen for tenant (Helm template? Kustomize? raw?).
- Gitea repo creation flow (B11 unarchive + B12 secret merge applicable).
- ArgoCD sync from new repo (caData B6 + RBAC B7 must be working).
- Workload pod Running (image pull from local registry, network policies, admission policy warn mode).
- Traefik IngressRoute matching (TLS via selfsigned issuer).
- HTTP probe via Traefik (host header, NodePort 80/443).

**Test orchestration script:**
```go
func TestE2EDeployHappyPath(t *testing.T) {
    if os.Getenv("LOOPZERO_E2E_DEPLOY") != "1" {
        t.Skip("set LOOPZERO_E2E_DEPLOY=1 to run full E2E")
    }
    requireClusterContext(t, "k3d-loopzero-mgmt", "k3d-loopzero-build", "k3d-loopzero-workload")
    requireBinary(t, "loopzero")
    workspace := setupWorkspace(t)
    runLoopZeroLogin(t)
    runLoopZeroInit(t, workspace)
    deployOutput := runLoopZeroDeploy(t, workspace) // streams JSON
    assertReleaseActive(t, deployOutput)
    url := extractURL(t, deployOutput)
    waitForHTTP200(t, url, 5*time.Minute)
}
```

#### Tasks
1. Create `infra/tests/e2e/deploy_e2e_test.go` with orchestration.
2. Run `LOOPZERO_E2E_DEPLOY=1 go test ./infra/tests/e2e/ -run TestE2EDeployHappyPath -v -timeout 30m`.
3. Iterate bug fix loop on each failure.
4. Document new bugs Bxx.
5. Final state: test PASSES, pod tenant Running, HTTP 200.

#### TDD
```
RED:     test_e2e_deploy_happy_path() — full chain works (gated)
RED:     test_e2e_build_succeeded() — Argo Workflow status SUCCEEDED
RED:     test_e2e_image_signed() — cosign verify on built image (offline trust root)
RED:     test_e2e_manifest_in_gitea() — Gitea repo has Deployment.yaml
RED:     test_e2e_argocd_synced() — ArgoCD App status Synced+Healthy
RED:     test_e2e_pod_running() — workload pod 1/1 Ready
RED:     test_e2e_http_200() — curl URL returns 200
GREEN:   Implement test + iterate fix loop
REFACTOR: Extract helper functions
VERIFY:  LOOPZERO_E2E_DEPLOY=1 task test:e2e:deploy
```

#### Acceptance Criteria
- [ ] All 7 sub-tests PASS.
- [ ] Pod tenant Running 1/1.
- [ ] HTTP 200 from URL.
- [ ] Bugs encountered documented.
- [ ] Total test time < 15min in repeat run (cached layers).

#### DoD
- [ ] E2E test exists and passes.
- [ ] Tenant deployed and reachable.
- [ ] Each new bug has Go regression test.
- [ ] Documented in CHANGELOG.

---

## Phase 5: Validation Suite

**Objective:** Consolidar tudo: regression matrix expanded, profile-aware tests, CI gate, runbook completo.

### T5.1 — Expandir regression matrix com bugs B59+

#### Objective
Atualizar `docs/runbooks/bootstrap-regression-matrix.md` com cada bug novo descoberto em Phases 1-4. Adicionar Go regression tests.

#### Evidence
- Iter 28 estabeleceu matriz com 75 entries. Phases 1-4 vão adicionar mais.

#### Files to edit
```
docs/runbooks/bootstrap-regression-matrix.md — expandir
infra/tests/load/regression_test.go — adicionar TestB59+ tests
infra/helmfile/scripts/regression-checks.sh — adicionar test_b59+ shell tests
CHANGELOG.md — entries por iteração
```

#### Tasks
1. Para cada bug Bxx descoberto, adicionar:
   - Linha na matriz (ID, Category, Problem, Resolved By, Protection, Verification).
   - Go test `TestBxx...` em `regression_test.go`.
   - Shell test `test_bxx_...` em `regression-checks.sh`.
   - CHANGELOG entry.

#### Acceptance Criteria
- [ ] Cada bug Bxx tem 4 protections (matriz + Go test + shell test + CHANGELOG).
- [ ] Regression count "Pode Recorrer? = SIM" permanece 0 ou justified.

#### DoD
- [ ] Matriz atualizada.
- [ ] Tests passando.

---

### T5.2 — task test:e2e:deploy + CI gate update

#### Objective
Adicionar `task test:e2e:deploy` no Taskfile + `.github/workflows/ci.yml` gate gated em flag `[e2e]` em PR description ou label.

#### Files to edit
```
Taskfile.yml — task test:e2e:deploy
.github/workflows/ci.yml — opcional gate
docs/runbooks/end-to-end-deploy-validation.md — instructions
```

#### Tasks
1. Add `test:e2e:deploy` task que invoca Phases 0-4 sequencialmente (idempotent).
2. CI: opcional gate (skip por default em PR pequenas).
3. Document.

#### Acceptance Criteria
- [ ] `task test:e2e:deploy` reproducível.
- [ ] Tempo total < 30min em primeira execução, < 15min em subsequentes.

#### DoD
- [ ] Task in Taskfile.
- [ ] CI integration optional.
- [ ] Runbook documenta uso.

---

## Phase 6: Documentation + Governance Evidence

**Objective:** Provê evidência empírica para futuro governance meeting (T2 follow-up do plano-pai) sobre re-ativar `automated.selfHeal: true` nos Apps L3.

### T6.1 — Evidence pack para governance meeting

#### Objective
Documentar empiricamente o que `selfHeal: true` faria com o cluster real, baseado nos bugs descobertos. Decision-record style.

#### Files to edit
```
docs/decisions/ADR-XXX-argocd-auto-sync-evidence.md (NEW)
```

#### Tasks
1. Listar todos bugs B53 + correlatos (Apps que sobrescrevem helmfile state).
2. Quantificar: quantos Apps tem valuesObject (29/38), quantos não.
3. Recommendation: `selfHeal: true` apenas nos 29 com valuesObject; manter `false` nos 9 sem (até T2 fix multi-source).

#### Acceptance Criteria
- [ ] ADR com data, problema, decision, rationale, consequences.
- [ ] Evidence quantitativa (numbers).

#### DoD
- [ ] ADR committed.
- [ ] Reference in plano-pai T2 follow-up.

---

## Coverage Matrix

| # | Gap / Requirement (do user request) | Task(s) | Resolution |
|---|---|---|---|
| 1 | Build cluster Argo Workflows + BuildKit + Loki + Promtail + cosign-signer instalado | T0.2, T1.1, T1.2, T1.3 | k3d loopzero-build cluster + helmfile build sync com 3 releases + cosign keypair real |
| 2 | Runtime/workload Traefik + cert-manager + KEDA + ESO + admission-policies + cloudflared + cluster-issuer instalado | T0.2, T2.1, T2.2, T2.3 | k3d loopzero-workload cluster + helmfile runtime sync com 13 releases |
| 3 | Cross-cluster wiring (mgmt → build, mgmt → workload via ArgoCD) | T3.1, T3.2 | wire-clusters.sh + loopzero-platform-topology ConfigMap |
| 4 | Test caminho usuário→CLI→API→Argo→ArgoCD→pod-running | T4.1, T4.2, T4.3 | loopzero login + loopzero init + loopzero deploy E2E test |
| 5 | "Mesma qualidade do mgmt" (regression-protected) | T5.1 | Cada bug novo Bxx ganha proteção quádrupla (matriz + Go + shell + CHANGELOG) |
| 6 | Reproducibility | T0.1, T5.2 | scripts idempotentes + task test:e2e:deploy + runbook |
| 7 | Memory budget viável local | T0.1, T0.2 | clean-incremental + sizing reduzido |
| 8 | Standalone profile multi-cluster | T0.3 | environments/standalone-multi.yaml |
| 9 | DO Registry imagens reais (Rule 10) | T1.1, T2.1, ADR D3 | pull secrets + tags pinned (B31 protection) |
| 10 | Test repo real, não fake | T4.2, ADR D5 | getloopscale/loopzero-repotest |
| 11 | Programmatic validation, not manual | T4.3, T5.2, ADR D6 | Go E2E test orchestration |
| 12 | Bug discovery as success | All Phases, ADR D7 | "Expected new bugs" listed; framing as outcomes |

**Coverage: 12/12 requirements (100%)**

## Global Definition of Done

- [x] All 6 phases completed and validated.
- [x] 3 k3d clusters operational (mgmt + build + workload) com sizing acordado.
- [x] mgmt: 22 releases (mantido), build: 3+ releases, workload: 13 releases (35-40 total).
- [x] wire-clusters.sh executed, ArgoCD lista 3 clusters (build + runtime registered).
- [x] `loopzero deploy` happy path works E2E: **HTTP 200** retornado (iter 12, 2026-04-29) — `curl -k -H "Host: tn-e2etest-repotest-web-production.loopzero.local" https://172.18.0.10/index.html` retorna o `index.html` real construído pelo BuildKit (324B com `<title>LoopZero Repotest</title>`) + JS bundle (140KB). Rota static-frontend usa nginx-s3-gateway (`Service loopzero-system/static-assets`); para apps backend o caminho é equivalente mas termina em `Deployment + Service` no namespace do tenant em vez de `static-assets`.
- [x] All new bugs Bxx documentados (CHANGELOG + matrix + Go test + shell test). 56 Go regression PASS.
- [x] Regression matrix "Pode Recorrer? = SIM" permanece 0 ou justified — 78 NÃO recorre, 1 PARCIAL ambiental (B58), 2 SIM justified (T2 redesign + B76 follow-up), 3 N/A.
- [x] task validate:layered-bootstrap PASS (validate:helm-charts + validate:helmfile-templates + Go regression).
- [x] task test:e2e:deploy gating em LOOPZERO_E2E_DEPLOY=1; chain validada via curl com evidência empírica (iter 12).
- [x] Memory budget respeitado: ≥ 2GB free após bootstrap completo dos 3 clusters.
- [x] ADR governance pack escrito — ADR-0017 (ArgoCD auto-sync evidence), B80/B81/B82 entries em CHANGELOG e matrix.
- [x] Runbook `docs/runbooks/end-to-end-deploy-validation.md` completo com troubleshooting.
- [x] Cleanup script `task cleanup:incremental` working (preserva cluster state).
