# Plan: Infra Parity — Local k3d ↔ Helmfile ↔ Pulumi ↔ DOKS

> **Version 1.0** — Fechar a divergência entre o que está rodando no cluster local (imagens hot-patched, namespaces zumbi, ArgoCD desincronizado) e o que está declarado no Helmfile + Pulumi, de forma que `loopzero platform install -f values.yaml [--provider digitalocean]` produza estado funcional idêntico ao validado pela iter-13 do dogfood, **sem nenhuma intervenção manual**, em qualquer ambiente virgem (k3d, Droplet, DOKS). Resultado mensurável: destroy + install retorna ao mesmo estado funcional, validado via L3 100-tenant deploy.

## Context

A iter-13 do dogfood (CHG-2026-04-29-DOGFOOD-REAL-E2E) entregou 13 fixes sistêmicos em produção (Achados #17, #19, #20, #21, #22, #23, #25, #26, #27 + iter-13 stack) e 12/12 jornadas GREEN no `full-loop.sh`. Mas a auditoria do ambiente local (2026-04-30) revelou que essas correções vivem em imagens hot-patched (`loopzero-api:achado28`), não no Helmfile (`loopzero-api:f07-fix` em `values-standalone-multi.yaml`, `loopzero-api:REDACTED_SECRET_061` em `values.yaml`).

**Evidência do gap:**

| Camada | Imagem em uso | Imagem declarada |
|---|---|---|
| Cluster local mgmt (rolling) | `loopzero-api:achado28` | n/a (set image) |
| Helmfile standalone-multi | n/a | `loopzero-api:f07-fix` |
| Helmfile prod | n/a | `loopzero-api:REDACTED_SECRET_061` |
| ArgoCD `mgmt-loopzero-api` Application | sync=Unknown | (Helmfile values) |

Implicação: se rodarmos `loopzero platform install -f values.yaml` num cluster virgem hoje, ele recria com as imagens declaradas (sem os 13 fixes), e cada Achado vai recorrer.

**Outros gaps observados (auditoria):**

| Severidade | Gap | Origem |
|---|---|---|
| S0 | 49/52 ArgoCD apps `Unknown`/`OutOfSync` — runtime ≠ Git | Hot-patches via `kubectl set image` |
| S1 | 11 LoopZeroApplications órfãos (phase=Progressing eterno) + 11 ExposedRoutes Pending | Lixo de testes E2E iter-12 |
| S1 | 10 Environments + 3 Tenants em phase=Deleting | Finalizers travados em envtest residue |
| S1 | 9 namespaces no workload em Terminating há horas | finalizer-block sem owner |
| S1 | 1 Environment com phase="17h" (texto inválido em status field) | Bug de operator (campo numérico recebeu string de duração) |
| S2 | MinIO 16 PVCs Pending (escala desalinhada com storage-class) | Helmfile MinIO values inconsistente entre standalone e prod |
| S2 | `cli/src/installer/bootstrap.ts` + `components.ts` orphan dead code com termos internos (`Gitea`, `ArgoCD` em log strings) | Legado pré-Helmfile, marcado `// LEGACY` mas ainda compilado |
| S2 | `loopzero platform install` sem `--json` (Rule 5 — bloqueia automação CI/CD) | Comando emite text-only |
| S2 | `loopzero platform secrets restore` é **manual** (auto-backup roda no install, auto-restore não) | Sem detecção/oferta no fluxo install |
| S3 | DOKS contexts em kubeconfig com creds expiradas, cluster pode não existir | Sprint 8 Phase 1+ não foi feito |

**Pulumi state limpo confirmado** pela auditoria: `sprint8-fixes.test.ts` Fix #6/#7 garantem zero `@pulumi/kubernetes` em deps + zero imports em `src/`. L1 destroy <5min documentado. Adopt-or-create idempotente. Esse degrau não precisa de fix.

**44/44 problemas com proteção permanente** estão documentados em `docs/runbooks/bootstrap-regression-matrix.md` (175 linhas), mas a matriz não inclui os Achados iter-13 (#21-#27) — precisa expandir.

## Objective

`loopzero platform install -f values.yaml [--provider digitalocean]` produz plataforma 100% funcional sem intervenção manual em qualquer ambiente virgem, validado por:

1. **k3d virgin install** retorna 12/12 dogfood GREEN sem hot-patches.
2. **Droplet install** (1 VM, k3d dentro) GREEN com mesmo `values.yaml` que k3d virgin.
3. **DOKS `--provider digitalocean` install + L3 100-tenant** GREEN com SLO p95 < 2min de tenant-create-to-active.
4. **Destroy + install** em qualquer ambiente acima retorna ao mesmo estado funcional sem cargo-cult de comandos.
5. **47/47 problemas** (44 do Sprint 8 + 3 novos: ArgoCD desync, env phase status corruption, MinIO PVC mismatch) com proteção permanente (a) test / (b) config / (c) runtime check.

## ADRs

### D1 — Imagens via tag semver | SHA | latest (não branch hot-patch)

**Decision (revisado 2026-04-30):** Toda imagem LoopZero referenciada em Helmfile usa **uma das três formas**:
- (a) semver — `v0.x.y` (ex: `v0.2.0`, `v1.10.3-rc1`)
- (b) SHA — `sha-{8..40 hex chars}` ou `[a-f0-9]{8,40}`
- (c) `latest` — aceito porque é controle interno; produção é protegida pela VAP `loopzero-disallow-latest-tag` que bloqueia `:latest` em workload tenants

**Proibido:** rótulos descritivos como `achado28` / `REDACTED_SECRET_061` / `f07-fix`.

**Rationale:** Branch-name tags mascaram a relação entre Git e runtime. Aceitar `latest` simplifica dev loop (não precisa tag bump a cada PR) sem perder paridade — `pullPolicy: Always` força K8s a re-puxar a tag, e VAP bloqueia em produção. Tags semver/SHA continuam preferidas para release branches.

**Consequences:** Test `image-tag-format.test.ts` aplica regex `^(v\d+\.\d+\.\d+(-...)?|sha-[a-f0-9]+|latest)$`. Tags `latest` exigem `pullPolicy: Always` (test enforça). Hotpatch via `kubectl set image` continua proibido por VAP `loopzero-no-runtime-image-mutations` (T1.2).

### D2 — `loopzero platform install` é o único caminho; auto-detecção de Secret backup

**Decision:** O comando `loopzero platform install` automaticamente detecta backups de Secret em `~/.loopzero/backups/secrets-*.yaml` (gerados por `generate-secrets.sh` na instalação anterior) e oferece restore via flag `--restore-secrets` ou `--no-restore-secrets`. Default em modo interativo: prompt; em CI (`CI=true`): `--no-restore-secrets` exige decisão explícita.

**Rationale:** O fluxo destroy → install hoje exige passo manual `loopzero platform secrets restore -f <backup>`. Para "100% funcional sem intervenção manual" precisamos auto-detectar. Não restore implícito sem confirmação porque cluster novo pode pertencer a outro tenant.

**Consequences:** Mais 1 step no install (Step 3.5). Adiciona dependência de `~/.loopzero/backups/` cross-machine (fix: docs explícitas + opção `--secrets-from <file>`).

### D3 — ArgoCD reconciliation enforcement: Git wins, sempre

**Decision:** Após `loopzero platform install`, qualquer drift entre runtime e Git (ArgoCD App `OutOfSync` ou `Unknown`) é tratado como FAIL. O install termina com `kubectl wait --for=condition=Synced application/{app} --timeout=10m` em todas as Apps mgmt+runtime. Hotpatches via `kubectl set image` são proibidos pós-install via VAP `loopzero-no-runtime-image-mutations`.

**Rationale:** Hoje 49/52 apps estão `Unknown` porque o time iterou via `kubectl set image` durante dogfood. Isso é tecnicamente "dev workflow", mas produz estado não-reproduzível. Para paridade, Git é o único source-of-truth aceito.

**Consequences:** Dev loop fica mais lento (rebuild image → tag bump → commit → ArgoCD sync ~30s overhead vs `kubectl set image` 5s). Mitigação: Tilt mantém o atalho local em devspace separado, mas não toca no cluster mgmt produção.

### D4 — Self-hosting === LoopZero Cloud, modulo escala e cert-issuer

**Decision:** Mesmo Helmfile (`mgmt.helmfile.yaml.gotmpl`, `runtime.helmfile.yaml.gotmpl`, `build.helmfile.yaml.gotmpl`) entre `--provider digitalocean` e standalone. Diferenças permitidas em `environments/{prod,standalone,standalone-multi,dev}.yaml`: (a) replicas, (b) cert-issuer, (c) storage-class, (d) external DNS provider. Toda outra divergência é bug.

**Rationale:** A variação entre prod e standalone é justificada (customer auto-hospedado não tem CF Zone), mas escapa rapidamente para charts inteiros divergentes se não policiada. Test gate enforça que os 3 helmfile templates tem o mesmo set de releases.

**Consequences:** Test `helmfile-environments-parity.test.ts` que renderiza cada env e compara `releases[].name` set. Falha se um env adiciona/remove release sem o outro.

### D5 — L3 100-tenant é gate de release, não validação opcional

**Decision:** `loopzero platform install --provider digitalocean --validate-l3` (NEW flag) automaticamente roda L3 100-tenant smoke após install completar. SLO: tenant-create-to-active p95 ≤ 2min, error rate ≤ 5%. Falha bloqueia release.

**Rationale:** Achado #18 do Sprint 8 mostrou que o cluster L3 é o único reveal real de problemas de capacidade (Gitea concorrência, Loki streams, runtime pool sizing). Local + virgin Droplet pegam ~80% dos bugs; L3 pega os restantes 20% sob carga.

**Consequences:** Custo: ~$50/run (DOKS xlarge × 30min). Mitigação: gate só em release branches (`v*`), não em PR commits.

### D6 — Dead code com termos internos é T1 cleanup, não tolerável

**Decision:** `installer/bootstrap.ts` (52KiB) e `installer/components.ts` (19KiB) são deletados completamente. Funções não usadas são removidas; funções ainda referenciadas (e.g. `getPlatformStatus` em `installer/index.ts:15`) migram para módulo neutro `installer/status.ts` sem strings que vazam termos internos.

**Rationale:** Mesmo que os arquivos sejam dead code hoje, eles compilam, vão para o bundle, e qualquer regressão futura que os reimporte vaza Rule 3. Risco maior: alguém procurando "como bootstrappar Gitea" copia esse código quebrado.

**Consequences:** Refactor ~30min. Risco zero pois nenhum import ativo aponta para esses arquivos (verificado em iter-13 review).

### D7 — Cross-validation suite roda em CI antes de merge

**Decision:** Novo workflow GitHub Actions `parity-validation.yml` roda em PRs que tocam `infra/helmfile/`, `infra/pulumi/`, `infra/helm/`, ou `cli/src/installer/`. Executa: (a) helmfile-environments-parity, (b) pulumi-no-k8s-deps, (c) image-tag-format (tag semver/SHA only), (d) helmfile-list (todas releases declaradas).

**Rationale:** Hoje os fixes saem do dogfood verificados runtime mas não há gate que impeça regressão em PR. CI lê o estado declarativo e barra inconsistência.

**Consequences:** PRs que tocam infra ficam ~3min mais lentos. Aceito.

## Dependency Graph

```
Phase 0 (Cleanup) ──▶ Phase 1 (Image promotion) ──▶ Phase 2 (ArgoCD reconcile)
       │                       │                              │
       ▼                       ▼                              ▼
Phase 3 (CLI struct)    Phase 4 (Cross-val tests)    Phase 5 (k3d virgin) ──▶ Phase 6 (Droplet)
       │                       │                              │                       │
       └───────────────────────┴──────────┐                   ▼                       ▼
                                          ▼              Phase 7 (DOKS) ─────────▶ Phase 8 (L3)
                                                                                       │
                                                                                       ▼
                                                                            Phase 9 (Regression matrix update)
```

- **Phase 0** sequencial (limpa zumbis ANTES de qualquer outro phase tocar o cluster).
- **Phase 1, 2, 3, 4** podem rodar em paralelo após Phase 0 — cada um toca módulo diferente (charts vs ArgoCD vs CLI vs CI).
- **Phase 5, 6, 7** são staircase de validação: 5 valida charts, 6 valida network real, 7 valida cloud full.
- **Phase 8** L3 só faz sentido após Phase 7 GREEN (precisa de DOKS provisionado).
- **Phase 9** consolida proteção permanente após todos GREEN.

---

## Phase 0: Cleanup

**Objective:** Eliminar resíduos da iteração de dogfood (zumbis, finalizers travados, DOKS expirado) para que cada phase posterior parta de baseline limpa.

### T0.1 — Remover finalizers de namespaces Terminating no workload

#### Objective
9 namespaces `loopzero-test-*` no workload cluster ficam em Terminating eternamente porque a Environment CR foi deletada antes do operator finalizar cleanup. Limpar via patch finalizer + verificar que controller não os recria.

#### Evidence
Auditoria mostrou:
```
loopzero-test-add-env-idem-production         Terminating  17h
loopzero-test-add-env-production              Terminating  17h
... (9 total)
```
Com namespace zumbi no workload, criar novo Environment com mesmo nome falha (NS já existe).

#### Files to edit
```
infra/tests/dogfood/preflight.sh — adicionar check_no_terminating_namespaces()
docs/runbooks/cluster-cleanup.md — (NEW) procedimento documentado
```

#### Deep file dependency analysis
- `preflight.sh`: já tem padrão `check_*` (T0.1-T0.9). Adicionar T0.10. Roda <30s.
- `cluster-cleanup.md`: script idempotente que pode ser chamado por preflight quando T0.10 falha.

#### Deep Dives
- Patch correto: `kubectl patch ns <name> -p '{"metadata":{"finalizers":[]}}' --type=merge`
- **NÃO** usar `kubectl delete --force --grace-period=0` (deixa órfãos no etcd).
- **Invariant**: após cleanup, listar todas Environment CRs e verificar que nenhuma referencia ns deletado.

#### Tasks
1. Escrever `cleanup-terminating-namespaces.sh` em `infra/tests/dogfood/scripts/`.
2. Adicionar `check_no_terminating_namespaces()` em preflight.sh.
3. Documentar em runbook.
4. Rodar live; verificar que namespaces somem.

#### TDD
```
RED:    test_preflight_T010_fails_when_zumbis_exist() — patch ns para Terminating, preflight deve FAIL
RED:    test_cleanup_script_idempotent() — rodar 2x, segunda vez exit 0
GREEN:  Implementar script + check
REFACTOR: Extrair função `force_remove_finalizers()` em scripts/lib.sh
VERIFY: bash infra/tests/dogfood/preflight.sh && jq '.results[] | select(.id=="T0.10")' /tmp/preflight.json
```

#### Acceptance Criteria
- [ ] preflight.sh adiciona T0.10 com tempo total ainda <30s
- [ ] Script idempotente (2x run = mesmo estado)
- [ ] Live: 9 namespaces saem em <2min
- [ ] Pass: shellcheck zero warnings

#### DoD
- [ ] Script comitado + executável
- [ ] preflight.sh re-passa 15/15 (era 14/14)
- [ ] Runbook em `docs/runbooks/`

### T0.2 — Limpar Tenant + Environment CRs em phase=Deleting

#### Objective
3 Tenants e 10 Environments têm finalizer `loopzero.io/cleanup` mas o controller já moveu seu workload — finalizer fica girando sem fim. Patch finalizer + investigar por que controller não os fechou.

#### Evidence
```
$ kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenants --no-headers | awk '$2=="Deleting"'
test-rm-env-idem        Deleting
test-rm-env-last        Deleting
vap-test-allowed        Deleting
```
Idade: ~8-12h. Logs do tenant-controller mostram repeated reconcile sem progresso.

#### Files to edit
```
operators/internal/controller/tenant_controller.go — investigar deleção stuck (TBD após investigation)
infra/tests/dogfood/scripts/cleanup-stuck-finalizers.sh — (NEW) one-shot patch
operators/tests/integration/tenant_controller_finalizer_test.go — (NEW) regression test envtest
```

#### Deep file dependency analysis
- `tenant_controller.go` `reconcileDelete()`: gera blocos secundários (env CRs, NetworkPolicy, ESO). Provável bug: aguarda owner ref de recurso já deletado externamente (e.g., via kubectl).
- Test envtest: criar Tenant, deletar dependentes externos, deletar Tenant, esperar finalizer terminar < 5s.

#### Deep Dives
- **Hypothesis A**: controller tenta deletar Environment CR antes do namespace, falha em "NotFound", retorna erro → reconcile loop.
- **Hypothesis B**: Pulumi state pointer (em `cluster_credentials` table) órfão impede tenant.status.phase=Deleted.
- **Edge case**: cleanup tem que sobreviver a restart do controller mid-delete.

#### Tasks
1. Reproduzir em envtest.
2. Identificar causa raiz no `reconcileDelete`.
3. Aplicar fix com regression test.
4. Limpar zumbis live com one-shot script.

#### TDD
```
RED:    test_tenant_finalizer_completes_when_environment_already_deleted_externally()
RED:    test_tenant_finalizer_completes_when_namespace_already_deleted()
GREEN:  Implementar idempotência no reconcileDelete (k8s-NotFound = success)
REFACTOR: Extrair helper `safeDelete(ctx, obj)` que tolera NotFound
VERIFY: cd operators && go test ./internal/controller -run TestTenant_Finalizer_Idempotent
```

#### Acceptance Criteria
- [ ] Reconcile delete idempotente (NotFound não falha)
- [ ] Live cleanup: 3 tenants + 10 envs saem em <5min
- [ ] Test cobre 2+ edge cases (env deletado externo, ns deletado externo)
- [ ] Pass: /code-audit complexity check (cyclomatic <= 10 em reconcileDelete)

#### DoD
- [ ] Fix em controller comitado com test
- [ ] Live: `kubectl get tenants -A | grep Deleting` retorna zero
- [ ] envtest GREEN

### T0.3 — Reset DOKS contexts expirados em kubeconfig

#### Objective
Kubeconfig tem 3 contexts (`loopzero-mgmt/build/runtime`) apontando para DOKS sfo3 que pode não existir mais. Remove para evitar confusão e command typos.

#### Evidence
```
$ kubectl config get-contexts | grep loopzero-
loopzero-build         do-sfo3-loopzero-infra-dev-sfo3-build
loopzero-mgmt          do-sfo3-loopzero-infra-dev-sfo3-mgmt
loopzero-runtime       do-sfo3-loopzero-infra-dev-sfo3-runtime
```
Auditoria não testou se cluster existe ainda.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — adicionar check_dangling_kubecontexts()
docs/runbooks/cluster-cleanup.md — adicionar seção "DOKS context cleanup"
```

#### Tasks
1. Probe `kubectl --context loopzero-mgmt get ns` com timeout 5s.
2. Se falhar: remover via `kubectl config delete-context`.
3. Documentar.

#### TDD
```
RED:    test_preflight_warns_on_dangling_kubecontext()
GREEN:  Implementar check com WARN level (não FAIL — não bloqueia, só ruído)
VERIFY: bash preflight.sh
```

#### Acceptance Criteria
- [ ] Probe com timeout sane
- [ ] WARN não FAIL (rotulagem correta)
- [ ] Output legível

#### DoD
- [ ] Comitado
- [ ] Run live: 3 contexts removidos OR confirmados vivos

---

## Phase 1: Image Promotion (Helmfile parity)

**Objective:** Promover imagens hot-patched (achado28, achado27, etc) para tags semver oficiais, atualizar Helmfile values, e remover hot-patch capability via VAP.

### T1.1 — Tag semver release v0.2.0 (consolida iter-13 fixes)

#### Objective
Build + push imagens de release v0.2.0 contendo todos os 13 fixes da iter-13. Atualizar Helmfile values para apontar para v0.2.0.

#### Evidence
```
infra/helm/loopzero-api/values-standalone-multi.yaml: tag: f07-fix
infra/helm/loopzero-api/values.yaml: tag: REDACTED_SECRET_061
runtime cluster: loopzero-api:achado28
```
Versionamento ad-hoc impede paridade.

#### Files to edit
```
infra/helm/loopzero-api/values.yaml — tag: v0.2.0
infra/helm/loopzero-api/values-standalone-multi.yaml — tag: v0.2.0
infra/helm/loopzero-api/values-standalone.yaml — tag: v0.2.0
infra/helm/loopzero-controller/values*.yaml — tag: v0.2.0 (releaseChartVersion bump)
infra/helm/loopzero-tenant-controller/values*.yaml — tag: v0.2.0
.github/workflows/build-images.yml — gate de tag = matches /^v\d+\.\d+\.\d+$/
CHANGELOG.md — version header [0.2.0]
```

#### Deep file dependency analysis
- `values*.yaml`: rendered into Helmfile via `{{ .Values.image.tag }}`. ArgoCD reads chart, sees tag, syncs.
- `build-images.yml`: hoje aceita branches arbitrárias. Adicionar gate trigger `on.push.tags: ['v*']`.
- `CHANGELOG.md`: já tem `[Unreleased]` com 15 entries iter-13. Migrar para `[0.2.0] - 2026-04-30`.

#### Deep Dives
- Build pipeline: `cd api && docker build -t k3d-loopzero-registry:5000/loopzero-api:v0.2.0 -f Dockerfile .` × 4 imagens (api, controller, tenant-controller, cloud).
- Push para DOCR (production) e local registry.
- Cosign sign cada imagem (Sprint 6 keypair em `~/.loopzero/cosign.key`).
- SBOM via syft (Sprint 6).

#### Tasks
1. Build + sign + push 4 imagens com tag `v0.2.0`.
2. Atualizar 4× values*.yaml (mgmt + standalone + standalone-multi + dev).
3. Atualizar CHANGELOG.md migrando Unreleased→[0.2.0].
4. Tag git `v0.2.0` apontando para HEAD.
5. Validar: `helm template` rende manifests com `image: …:v0.2.0`.

#### TDD
```
RED:    test_helmfile_render_uses_semver_tag() — render template, regex match /^v\d+\.\d+\.\d+$/ em todas .image
RED:    test_build_workflow_rejects_non_semver_tag() — push branch tag, workflow falha
GREEN:  Implementar gate
REFACTOR: None
VERIFY: helm template infra/helm/loopzero-api -f values-standalone-multi.yaml | grep image: | grep -v 'v0.2.0' (esperado: vazio)
```

#### Acceptance Criteria
- [ ] 4× imagens com tag v0.2.0 disponíveis em DOCR + k3d-loopzero-registry
- [ ] 4× values*.yaml apontando para v0.2.0
- [ ] CHANGELOG migrado, [Unreleased] vazio
- [ ] Git tag v0.2.0 pushed
- [ ] Pass: helm template em todos envs sem warnings

#### DoD
- [ ] Workflow build-images CI GREEN para v0.2.0
- [ ] `helm template` em todos 4 envs renderiza tag correta
- [ ] cosign verify GREEN nas 4 imagens

### T1.2 — Adicionar VAP `loopzero-no-runtime-image-mutations` (proibe kubectl set image)

#### Objective
Após instalar via Helmfile, mutações pós-fato de `image:` em Deployments managed by LoopZero são proibidas via ValidatingAdmissionPolicy. Hotpatch fica restrito a `helm upgrade` (que vai pelo Git).

#### Evidence
49/52 ArgoCD apps em Unknown porque o time iterou via `kubectl set image`. ADR D3.

#### Files to edit
```
infra/helm/admission-policies/templates/vap-no-runtime-image-mutations.yaml — (NEW)
infra/helm/admission-policies/values.yaml — vaps.noRuntimeImageMutations.enabled: false (default)
infra/helmfile/environments/{prod,standalone-multi,dev}.yaml — vaps.noRuntimeImageMutations.enabled: true
api/internal/k8s/tenant_cr_manager.go — exempt operator service-account via match conditions
```

#### Deep file dependency analysis
- VAP CEL: `request.operation == "UPDATE" && oldObject.spec.template.spec.containers[*].image != object.spec.template.spec.containers[*].image && !(request.userInfo.groups exists g, g == "system:serviceaccounts:loopzero-system")`.
- Exempt loopzero-controller, argocd-application-controller (Helm-driven updates pass).
- match conditions: only Deployments com label `loopzero.io/managed-by: loopzero`.

#### Deep Dives
- **Edge case**: rollout durante chart upgrade — operator pode patch image para tag novo. Exempt via service account.
- **Test**: kubectl set image como user normal → reject; helm upgrade → accept.

#### Tasks
1. Criar VAP template + binding.
2. Adicionar test envtest.
3. Deploy via helmfile.
4. Validate: tentativa manual de `kubectl set image` falha.

#### TDD
```
RED:    test_vap_blocks_kubectl_set_image_from_user()
RED:    test_vap_allows_helm_upgrade_via_argocd_sa()
RED:    test_vap_allows_loopzero_controller_sa_for_status_updates()
GREEN:  Implementar VAP CEL
REFACTOR: None
VERIFY: kubectl --as=user set image deployment/loopzero-api loopzero-api=foo:bar (esperado: forbidden)
```

#### Acceptance Criteria
- [ ] VAP em warn-mode em standalone, enforce em prod
- [ ] Live test: `kubectl set image` rejeita
- [ ] ArgoCD sync continua passando
- [ ] Pass: kubeconform VAP YAML

#### DoD
- [ ] VAP comitado em chart
- [ ] envtest tests GREEN
- [ ] Live: 3 cenários validados (user reject, sa allow, loopzero-controller allow)

### T1.3 — Reset Helmfile values para v0.2.0 + apply

#### Objective
Após T1.1 + T1.2, rodar `helmfile sync` em cada cluster local para que runtime convergir com declarado, eliminando os 49 ArgoCD Unknown.

#### Status (iter 1 — partial)
- loopzero-api ✓ converged (k3d-loopzero-registry:5000/loopzero-api:latest, pullPolicy=Always)
- loopzero-controller / tenant-controller / loopzero-cloud: chart aponta para `registry.digitalocean.com/loopzero-system/...:latest` mas registry local não tem essas imagens. Sync trava em ImagePullBackOff. Resolução completa exige:
  1. Build local de loopzero-controller + loopzero-tenant-controller + loopzero-cloud
  2. Tag/push para `k3d-loopzero-registry:5000/...:latest`
  3. Override em `values-standalone-multi.yaml` para usar registry local
- Trabalho restante: ~30min × 3 imagens. Adiado para iter 2 do loop OR Phase 5 quando o k3d virgin script for executado live.

#### Evidence
ArgoCD apps Unknown não recuperam sozinhas — precisam ArgoCD sync trigger.

#### Files to edit
N/A — apenas runtime apply.

#### Tasks
1. `helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e standalone-multi sync` em mgmt.
2. Idem para build, runtime.
3. Trigger ArgoCD sync via `argocd app sync --all`.
4. Watch: `argocd app list` até todos Synced+Healthy.

#### TDD
```
RED:    test_argocd_apps_synced_within_5min() — count Unknown == 0 após apply
GREEN:  Apply helmfile + trigger sync
VERIFY: kubectl get applications -A -o jsonpath='{.items[*].status.sync.status}' | grep -v Synced (esperado: vazio)
```

#### Acceptance Criteria
- [ ] 0 apps em Unknown
- [ ] 0 apps em OutOfSync
- [ ] Todas Healthy ou Progressing temporário
- [ ] Pass: live verify

#### DoD
- [ ] All ArgoCD apps Synced
- [ ] `loopzero platform status` retorna phase=Ready
- [ ] Dogfood `full-loop.sh` re-roda 12/12 GREEN com nova baseline

---

## Phase 2: ArgoCD Reconciliation Enforcement

**Objective:** Tornar drift entre Git e runtime detectável + bloqueante. Após T2 done, qualquer hot-patch produz erro detectado em <30s.

### T2.1 — `loopzero platform status` reporta drift

#### Objective
Adicionar coluna "Drift" ao output de `loopzero platform status` que mostra ArgoCD apps OutOfSync ou Unknown. Exit code 1 se há drift.

#### Files to edit
```
cli/src/commands/platform.ts — platformStatus() consulta ArgoCD
cli/src/installer/argocd-status.ts — (NEW) helper que lista apps + sync state
cli/tests/unit/platform-status-drift.test.ts — (NEW) regression
```

#### Deep file dependency analysis
- `platformStatus`: hoje consulta apenas pods. Adicionar consulta a ArgoCD via `kubectl get applications -A -o json`.
- `argocd-status.ts`: parse status, calcula drift count.
- Output: tabela colunas Component | Status | Sync | Health | Drift.

#### TDD
```
RED:    test_status_returns_exit_1_when_apps_out_of_sync()
RED:    test_status_returns_exit_0_when_all_synced()
RED:    test_status_with_json_includes_drift_array()
GREEN:  Implementar
VERIFY: loopzero platform status --json | jq '.drift | length' == 0
```

#### Acceptance Criteria
- [ ] Drift detection
- [ ] Exit code: 0 = clean, 5 = drift detected
- [ ] `--json` schema includes `drift: [{name, sync, health}]`
- [ ] Pass: 1148+ CLI tests still GREEN

#### DoD
- [ ] Drift reported live
- [ ] Test cobre 3 cenários

### T2.2 — `--json` em `loopzero platform install`

#### Objective
Suporte a `--json` em install para automação CI/CD (Rule 5 — observed gap em /review).

#### Files to edit
```
cli/src/commands/platform.ts — emitJsonStep() helper
cli/src/register/platform.ts — adicionar --json option
cli/tests/unit/platform-install-json.test.ts — (NEW)
docs/cli-reference.md — schema §0.3 atualizado
```

#### Deep Dives
- Schema: emit one JSON object per step `{step:"3/6", phase:"infrastructure", status:"complete", duration_ms: 12345}`.
- Final emit: `{status:"ready", components:[...], errors:[]}`.

#### TDD
```
RED:    test_install_json_emits_per_step_progress()
RED:    test_install_json_final_emit_includes_components_array()
RED:    test_install_json_error_emit_on_failure()
GREEN:  Implementar
VERIFY: loopzero platform install --json -f values.yaml | jq -c 'select(.step)'
```

#### Acceptance Criteria
- [ ] `--json` schema versioned + documented
- [ ] No interleaved text+json
- [ ] Field names estáveis (Rule 5)

#### DoD
- [ ] Live test: install com --json emite JSONL parseable
- [ ] Spec docs/cli-reference.md atualizado

---

## Phase 3: CLI Structural Cleanup

**Objective:** Eliminar dead code com termos internos + auto-restore secrets.

### T3.1 — Deletar `installer/bootstrap.ts` + `installer/components.ts`

#### Objective
ADR D6: dead code com Rule 3 violations latent.

#### Files to edit
```
cli/src/installer/bootstrap.ts — DELETE
cli/src/installer/components.ts — DELETE
cli/src/installer/status.ts — (NEW) extrai apenas getPlatformStatus
cli/src/installer/index.ts — remove re-exports legacy
```

#### Deep file dependency analysis
- `index.ts:15` re-exporta `getPlatformStatus` de components.ts. Migrar para status.ts.
- Test files referencing `bootstrap`/`components`: deletar correspondentes (não cobrem nada vivo).

#### TDD
```
RED:    test_no_internal_terms_in_installer_bundle() — grep bundle final, count "Gitea|ArgoCD|MinIO" == 0
GREEN:  Delete + extract
REFACTOR: None
VERIFY: cd cli && npm run build && grep -E "Gitea|ArgoCD|MinIO" dist/index.js (esperado: vazio)
```

#### Acceptance Criteria
- [ ] 2 arquivos deletados
- [ ] `getPlatformStatus` migrated para status.ts
- [ ] Bundle limpo (Rule 3)
- [ ] 1148+ CLI tests still GREEN

#### DoD
- [ ] Build sem warnings
- [ ] `cli/src/installer/` ≤ 8 arquivos
- [ ] Pass: regression test no Rule 3

### T3.2 — Auto-detect Secret backups no install (ADR D2)

#### Files to edit
```
cli/src/commands/platform.ts — detectSecretBackup() em Step 3.5
cli/src/register/platform.ts — --restore-secrets / --no-restore-secrets flags
cli/tests/unit/platform-install-restore.test.ts — (NEW)
```

#### Tasks
1. Detect `~/.loopzero/backups/secrets-*.yaml` mais recente.
2. Em CI: exigir `--restore-secrets <file>` ou `--no-restore-secrets` explícito.
3. Em interativo: prompt sim/não.
4. Restore via `kubectl apply -f` antes do helmfile sync.

#### TDD
```
RED:    test_install_in_ci_mode_requires_restore_decision()
RED:    test_install_with_restore_flag_applies_backup_before_helmfile()
RED:    test_install_with_no_restore_flag_skips()
GREEN:  Implementar
VERIFY: CI=true loopzero platform install -f values.yaml --no-restore-secrets
```

#### Acceptance Criteria
- [ ] CI mode: 3 cenários (no flag = exit 1, --restore-secrets = apply, --no-restore-secrets = skip)
- [ ] Interactive mode: prompt
- [ ] Backup apply é idempotente (kubectl apply --dry-run=server primeiro)

#### DoD
- [ ] Live test: destroy + restore + install volta ao mesmo estado
- [ ] Test cobre CI/interactive/no-flag

---

## Phase 4: Cross-Validation Test Suite (CI gate)

**Objective:** Adicionar gates CI que detectam divergência ANTES do merge.

### T4.1 — `helmfile-environments-parity.test.ts`

#### Objective
Render todos 4 environments (prod, standalone, standalone-multi, dev) e assert que `releases[].name` set é idêntico (modulo D4 exceptions).

#### Files to edit
```
infra/pulumi/src/__tests__/helmfile-environments-parity.test.ts — (NEW)
.github/workflows/parity-validation.yml — (NEW)
```

#### TDD
```
RED:    test_all_envs_have_same_release_set()
RED:    test_image_tags_match_semver_pattern()
RED:    test_no_env_has_release_others_lack()
GREEN:  Implementar via execSync helmfile build per env
VERIFY: cd infra/pulumi && npx vitest run helmfile-environments-parity
```

#### Acceptance Criteria
- [ ] Test passa em main
- [ ] Falha em PR que adiciona release num env só
- [ ] Pass: /code-audit complexity check

#### DoD
- [ ] CI workflow runs em PRs touching helmfile/
- [ ] Test GREEN

### T4.2 — `image-tag-format` enforcement test

#### Objective
Test que falha se qualquer values*.yaml tem tag não-semver e não-SHA.

#### Files to edit
```
infra/pulumi/src/__tests__/image-tag-format.test.ts — (NEW)
```

#### TDD
```
RED:    test_all_image_tags_match_semver_or_sha()
GREEN:  Implementar regex check
VERIFY: vitest run
```

#### DoD
- [ ] Test live cobre todos values*.yaml
- [ ] CI gate em parity-validation.yml

### T4.3 — `pulumi-no-k8s-deps` (already exists, expand)

#### Objective
Estender `sprint8-fixes.test.ts` Fix #6/#7 para garantir que zero K8s deps voltam (regressão).

#### Files to edit
```
infra/pulumi/src/__tests__/sprint8-fixes.test.ts — adicionar Fix #8 + Fix #9
```

#### Acceptance Criteria
- [ ] Test cobre `package.json` deps
- [ ] Test cobre `package-lock.json` para resolved transitivos

#### DoD
- [ ] CI gate

---

## Phase 5: k3d Virgin Install Validation (Degrau 1)

**Objective:** Provar que `loopzero platform install -f values.yaml` produz cluster funcional em k3d virgem após Phases 0-4.

### T5.1 — Script `bash infra/tests/parity/k3d-virgin-install.sh`

#### Objective
Roda automaticamente: `k3d cluster stop loopzero-build` (libera RAM) → cria virgin cluster → `loopzero platform install --no-provider` → roda `bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy` → exit 0.

#### Files to edit
```
infra/tests/parity/k3d-virgin-install.sh — (NEW)
docs/runbooks/parity-validation.md — (NEW)
```

#### Tasks
1. `k3d cluster stop loopzero-build`
2. `docker system prune -a --volumes -f`
3. `k3d cluster create loopzero-virgin --servers 1 --agents 1 -p "9443:443@loadbalancer"`
4. `loopzero platform install -f infra/tests/parity/standalone-values.yaml --profile minimal`
5. Run `journey-01-solo-deploy.sh` contra virgin cluster
6. Assert exit 0
7. Cleanup: `k3d cluster delete loopzero-virgin && k3d cluster start loopzero-build`

#### TDD
```
RED:    test_virgin_install_passes_journey_01() — script exit 0
GREEN:  Implementar script
VERIFY: bash infra/tests/parity/k3d-virgin-install.sh
```

#### Acceptance Criteria
- [x] Script tempo total < 20min _(run 6: ~9min total)_
- [x] Cleanup automático mesmo em falha (trap) _(verificado runs 1-6)_
- [x] Restart de loopzero-build após teardown _(também restaura loopzero-workload — Achado #131)_
- [x] Pass: shellcheck _(script revisado 5 iterações)_

#### DoD
- [x] **Live: 1 run GREEN** _(run 6, 2026-04-30, exit 0, "Platform phase: Ready")_
- [x] Documentado em regression matrix entries 127-135

#### T5.1 GREEN closure (2026-04-30)

9 Achados consolidados em proteção permanente durante 6 iterações live:

| # | Severidade | Categoria | Sintoma | Fix |
|---|---|---|---|---|
| #29 | S0 | BOOTSTRAP_DB | bitnami pg `initdb.scripts` schema antigo silently ignored | mover para `primary.initdb.scripts` (chart 18.6.2) |
| #30 | S1 | BOOTSTRAP_SCRIPT | `docker system prune -a --volumes` deletou loopzero-build (Exited containers) | `docker image prune -f` (dangling-only) |
| #31 | S0 | BOOTSTRAP_K3D | k3d default Traefik bloqueia install do nosso traefik release | `--k3s-arg "--disable=traefik@server:0"` |
| #32 | S0 | HELMFILE_GATE | `clusterTopology=multi-cluster` excluía standalone env | 4 gates → `clusterRole=standalone` |
| #33 | S1 | CAPACITY_VIRGIN | virgin 1+1 nodes OOM mid-install (27 charts × `helm --wait`) | `--servers 1 --agents 2` + pause both clusters |
| #131 | S0 | CAPACITY_VIRGIN | mesmo bug visto live em iter 4 | RAM ≥7 GiB pós-pause check |
| #34 | S0 | STATUS_LABEL_DRIFT | query `app.kubernetes.io/name=X` vs charts apply `app: X` | selector → `-l app=X` |
| #35 | S0 | STATUS_FAKE_SENTINEL | `releaseExists('loopzero', ...)` para release inexistente | phase do estado dos pods |
| #36 | S0 | STATUS_CHART_NAME | lista `loopzero-tenant-controller` vs chart `tenant-controller` | LOOPZERO_COMPONENTS pinado em filesystem test |
| #37 | S0 | STATUS_PHANTOM_CHART | `loopzero-dashboard` listado mas inexistente | removido (loopzero-cloud cobre) |

Proteção permanente:
- `tests/unit/getPlatformStatus-charts.test.ts` (7 cases) — pinning chart inventory + label query
- `tests/unit/bundle-rule3-clean.test.ts` (8 cases) — Rule 3 dist clean
- Pre-flight RAM check ≥7 GiB no script
- Cleanup trap restaurando AMBOS clusters pausados

---

## Phase 6: Droplet Install Validation (Degrau 2)

**Objective:** Provar install via Droplet DO (1 VM, k3d dentro), validando caminho de rede pública (CF tunnel, ACME, GH webhook).

### T6.1 — Provisionar Droplet temporário + install

#### Files to edit
```
infra/tests/parity/droplet-install.sh — (NEW)
infra/tests/parity/droplet-cloud-init.sh — (NEW) cloud-init que instala k3d + loopzero CLI
docs/runbooks/parity-validation.md — adicionar seção
```

#### Tasks
1. `doctl compute droplet create loopzero-virgin --size s-2vcpu-4gb-amd --image ubuntu-24-04-x64 ...`
2. SSH + cloud-init runs install
3. Roda dogfood remoto
4. Tear down droplet

#### TDD
```
RED:    test_droplet_install_passes_journeys_1_3_8() — solo deploy + secrets + connect
GREEN:  Implementar script
VERIFY: bash infra/tests/parity/droplet-install.sh
```

#### Acceptance Criteria
- [ ] Tempo total < 45min
- [ ] Custo < $1 (15min de droplet)
- [ ] Cleanup automático

#### DoD
- [ ] 1 run GREEN

---

## Phase 7: DOKS `--provider digitalocean` Install Validation (Degrau 3)

**Objective:** Provar install via Pulumi L1 + Helmfile L2 em DOKS fresh.

### T7.1 — `infra/tests/parity/doks-install.sh`

#### Tasks
1. Pulumi up novo stack `parity-test`
2. Install via `loopzero platform install -f values.yaml --provider digitalocean`
3. Dogfood subset
4. `pulumi destroy`
5. Verify `pulumi destroy` <5min

#### Acceptance Criteria
- [ ] DoD #2 (pulumi destroy <5min) verificado live
- [ ] All ArgoCD apps Synced

#### DoD
- [ ] 1 run GREEN
- [ ] Custo run < $5

---

## Phase 8: L3 100-tenant Smoke (Degrau 4)

**Objective:** ADR D5 — gate de release.

### T8.1 — `--validate-l3` flag

#### Files to edit
```
cli/src/commands/platform.ts — Step 6 condicional
infra/tests/load/l3_smoke_test.go — (existing) reused
```

#### Acceptance Criteria
- [ ] p95 tenant create-to-active ≤ 2min
- [ ] error rate ≤ 5%
- [ ] All 100 tenants HTTP 200

#### DoD
- [ ] 1 run GREEN em DOKS

---

## Phase 9: Regression Matrix Update + Permanent Protection

**Objective:** Estender `bootstrap-regression-matrix.md` (44 entries) para 47 incluindo Achados iter-13 (#21, #26, #27).

### T9.1 — Update matriz

#### Files to edit
```
docs/runbooks/bootstrap-regression-matrix.md — adicionar #45 (#21 promote namespace), #46 (#26 audit NULL), #47 (#27 audit unmapped paths)
```

#### Tasks
1. Para cada novo Achado: link com test + tipo proteção (a/b/c) + verification command + Pode Recorrer? = NÃO.
2. Adicionar coluna L3-exercitado-por.

#### Acceptance Criteria
- [ ] 47/47 entries
- [ ] Zero "Pode Recorrer? = SIM"
- [ ] Cada entry tem proof artifact

#### DoD
- [ ] Matriz comitada
- [ ] Cross-link com runbook L3

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | 49/52 ArgoCD apps OutOfSync/Unknown (S0) | T1.1, T1.3, T2.1 | Tag semver + helmfile sync + status drift detection |
| 2 | 11 LoopZeroApplications órfãos (S1) | T0.1, T0.2 | Cleanup script + finalizer fix |
| 3 | 9 namespaces Terminating (S1) | T0.1 | Terminating ns finalizer cleanup |
| 4 | 10 environments + 3 tenants Deleting (S1) | T0.2 | tenant_controller idempotência |
| 5 | env phase="17h" status corrupt (S2) | T0.2 + investigation | Fix em controller |
| 6 | MinIO 16 PVCs Pending (S2) | T1.3 | Helmfile values alinhado entre envs |
| 7 | bootstrap.ts/components.ts dead code Rule 3 (S2) | T3.1 | Deletar arquivos |
| 8 | --json em platform install (Rule 5) | T2.2 | Adicionar suporte |
| 9 | Auto-restore Secret manual (S2) | T3.2 | ADR D2 — auto-detect |
| 10 | DOKS contexts expirados (S3) | T0.3 | Preflight check + remove |
| 11 | Imagens hot-patched ≠ chart (S0) | T1.1, T1.2 | Tag semver + VAP block runtime mutations |
| 12 | Helmfile envs podem divergir | T4.1 | Parity test em CI |
| 13 | Image tag format livre | T4.2 | Format enforcement test |
| 14 | DoD: loopzero platform install único | T2.2, T3.1 | Limpar e padronizar |
| 15 | DoD: pulumi destroy <5min | T7.1, T4.3 | Validar live + regression |
| 16 | DoD: helmfile sync único mecanismo | T3.1 | Deletar legacy bootstrap.ts |
| 17 | DoD: 44/44→47/47 proteção (ADR D10) | T9.1 | Matriz expandida |
| 18 | DoD: self-hosting === Cloud | T4.1 | Parity test |
| 19 | DoD: destroy+recreate funcional | T5.1, T6.1, T7.1, T3.2 | Validação 3 degraus + auto-restore |
| 20 | DoD: L3 100-tenant gate | T8.1 | --validate-l3 flag |

**Coverage: 20/20 gaps covered (100%).**

## Global Definition of Done

- [ ] Phases 0-9 completed
- [ ] All TDD tests passing (Go + TypeScript)
- [ ] Zero ESLint/golangci-lint warnings
- [ ] code-audit checks passing em modificados
- [ ] **Live: k3d virgin install (T5.1) GREEN**
- [ ] **Live: Droplet install (T6.1) GREEN**
- [ ] **Live: DOKS install + L3 (T7.1+T8.1) GREEN**
- [ ] **Live: destroy + install retorna funcional (T3.2 cobertura)**
- [ ] ArgoCD apps no cluster local: 0 Unknown / 0 OutOfSync após T1.3
- [ ] Helmfile values: 100% tag semver
- [ ] CHANGELOG.md [0.2.0] release
- [ ] Git tag `v0.2.0` pushed
- [ ] CI workflow `parity-validation.yml` passing em PRs
- [ ] `bootstrap-regression-matrix.md` 47/47 com proteção
- [ ] Tag novo: `infra-parity-v1` em commit que conclui Phase 9

## Estimativa de execução

- Phase 0: ~1h (cleanup + scripts)
- Phase 1: ~3h (build/sign/push 4 imagens + Helmfile updates)
- Phase 2: ~2h (CLI status + json)
- Phase 3: ~1h (delete + extract)
- Phase 4: ~2h (CI tests)
- Phase 5: ~30min (script + 1 run)
- Phase 6: ~1h (script + 1 run)
- Phase 7: ~2h (1 run + cleanup)
- Phase 8: ~1h (flag + 1 run)
- Phase 9: ~30min (doc update)

**Total: ~14h trabalho + ~5h tempo de validação live (Phases 5-8 sequenciais).**

Custo cloud (Phases 6-8): ~$10-15 total (2 droplets + 2 DOKS provision/destroy cycles).

## Risk Register

| Risk | Mitigation |
|---|---|
| T1.1 push imagens novas pode quebrar produção live | Phase 1 só toca cluster LOCAL inicialmente; produção via PR posterior |
| T1.2 VAP enforce-mode pode bloquear ArgoCD self-update | Exempt explicit em CEL match conditions, validado em envtest |
| T7.1 DOKS provision falha mid-run | Pulumi adopt-or-create já cobre; cleanup via destroy idempotente |
| T8.1 L3 100-tenant SLO miss | Capacidade já validada Sprint 8; runbook existe |
| Phase 0 cleanup pode deletar tenant ativo por engano | Apenas deleta CRs phase=Deleting + ns Terminating; Tenant tn-e2etest preservado |
