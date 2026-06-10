# Edge Case Review — M07 Tenant Build Pipeline to 3.85 Cap

**Data:** 2026-05-27
**Plano:** `docs/plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md`
**Tasks analisadas:** 13 phases (M1: 5 phases, M2: 8 phases + Final Phase)
**Edge cases encontrados:** 14 (MUST FIX: 7, SHOULD TEST: 4, DOCUMENT: 3)

**Veredicto:** **PLANO PRECISA DE AJUSTE** — 7 MUST FIX, todos verificáveis LIVE nesta sessão. M2 Phase 7 vai falhar imediatamente sem fix EC-1.

---

## MUST FIX

### EC-1: cert-manager NÃO instalado em k3d-loopzero-build → Sigstore stack falha em Pulumi up

- **Task afetada:** M2 Phase 7 (T7.2 `Pulumi up Sigstore stack`)
- **Família:** Infraestrutura
- **Cenário:** Confirmado LIVE nesta sessão (`kubectl --context k3d-loopzero-build get pods -n cert-manager` retorna `No resources found`). Cluster loopzero-build NUNCA recebeu cert-manager — apenas mgmt (Hydra/Kratos) + workload + runtime-2 têm. Sigstore stack precisa cert-manager para emitir TLS certs internos (Fulcio TLS, Rekor TLS, CTLog TLS).
- **Impacto:** `pulumi up` vai apply manifests com `Certificate` CRs que ficam Pending forever → 4 components nunca chegam Ready → Phase 7 trava → tudo de Phase 8-13 cancelado. Mesmo padrão do D-FF-03 desta sessão (cluster-issuer ausente).
- **Fix sugerido:** **Adicionar Phase 6.5 — Bootstrap cert-manager + cluster-issuer em loopzero-build** ANTES de Phase 7. 2 lines em Taskfile.yml `k3d:bootstrap-workload` pattern: `helmfile sync --selector name=cert-manager` + `name=cluster-issuer` no cluster loopzero-build. Plan documenta task novo `task k3d:bootstrap-build-cert-manager`.

### EC-2: Cross-cluster Fulcio URL — loopzero-api (mgmt) precisa alcançar Fulcio em loopzero-build cluster

- **Task afetada:** M2 Phase 8 (T8.1 — set `LOOPZERO_FULCIO_URL=http://fulcio.sigstore-system.svc.cluster.local:8080`)
- **Família:** Multi-Cell / Cross-Cluster
- **Cenário:** Plan especifica URL `*.svc.cluster.local` — esse FQDN só resolve DENTRO do cluster onde sigstore stack roda (loopzero-build). loopzero-api roda em loopzero-mgmt cluster. Cross-cluster service DNS NÃO funciona out-of-box em k3d.
- **Impacto:** loopzero-api inicia + processa builds, mas Argo Workflow em loopzero-build cluster TENTA assinar com Fulcio URL inválida → cosign-sign step falha → build FAILED. Cosign-sign roda DENTRO do cluster loopzero-build (Argo Workflow worker pod), então URL `*.svc.cluster.local` resolve OK desse contexto. **loopzero-api NÃO chama Fulcio diretamente — só passa URL como argv para Workflow.** Verify: cosign args incluem `--fulcio-url` flag.
- **Real risk:** Se algum código em loopzero-api faz lookup Fulcio (ex: pre-flight check), vai falhar. **Verifiquei: `grep "FulcioURL\|FULCIO_URL" api/` retorna apenas referencia em workflow_builder.go — não há cliente Fulcio em loopzero-api.**
- **Fix sugerido:** **Reclassificar como DOCUMENT** (não MUST FIX) — risco menor que parecia. Adicionar nota no plan ADR D4: "Fulcio URL usado apenas como argv para Argo Workflow worker pods (resolve intra-cluster), não pela loopzero-api."

### EC-3: InfraParams.CosignKeyless existe mas env var NÃO wired

- **Task afetada:** M2 Phase 8 (T8.1 — "Set `LOOPZERO_COSIGN_KEYLESS=true` em loopzero-api values")
- **Família:** Padrão sistêmico "Implemented but not wired"
- **Cenário:** Confirmado LIVE: `workflow_builder.go:48` define `CosignKeyless bool` + `:66` `CosignAttestEnabled bool`. Mas `grep "LOOPZERO_COSIGN_KEYLESS" api/cmd/ api/internal/config/` retorna **zero matches**. Não existe código que lê env var e injeta em InfraParams. Helm values bonitos viram pod env, mas pod startup ignora env → InfraParams.CosignKeyless permanece `false` (default).
- **Impacto:** Plan Phase 8 "executa" sem erro (helm sync OK + pods restart OK), mas **NENHUMA mudança real** — flag fica off. Phase 9 (LIVE keyless build proof) falha porque cosign-sign step gera keyed signature, não keyless. **Score 3.85 vira cosmético.**
- **Fix sugerido:** **NOVA sub-task T8.0 — Wire env var em loopzero-api startup**. Edit `api/cmd/main.go` ou config loader: ler `os.Getenv("LOOPZERO_COSIGN_KEYLESS")` + `LOOPZERO_COSIGN_ATTEST_ENABLED` + URLs, passar em `InfraParams` construction. ~10 linhas + 1 unit test. **Sem isso, M2 inteiro é teatro.**

### EC-4: MEMORY.md já em 28KB > 24.4KB limit — Phase 5 T5.2 vai truncar

- **Task afetada:** M1 Phase 5 (T5.2 — Memory note)
- **Família:** State / Storage
- **Cenário:** Confirmado LIVE: `wc -c MEMORY.md` retorna 28085 bytes. CLAUDE.md "auto memory" sistema warns "MEMORY.md is 27.1KB (limit: 24.4KB) — index entries are too long. Only part of it was loaded." Adicionar nova entrada vai PIORAR truncamento — entradas antigas (de 2026-04-30) podem cair fora da janela carregada em sessions futuras.
- **Impacto:** Memory entry de M07 re-audit pode ser invisível na próxima sessão. Plan diz "memory note for futuras consultas" — mas próxima consulta não vê a entrada.
- **Fix sugerido:** **NOVA sub-task T5.0 — Compactar MEMORY.md ANTES de adicionar entry**. Apagar 5-8 entradas mais antigas com data <2026-05-15 (já cobertas por commits/audit docs), mover seu conteúdo detalhado para topic files separados se necessário. Target: voltar a <24KB. ~30min.

### EC-5: Phase 10 enforce flip mata workloads existentes

- **Task afetada:** M2 Phase 10 (T10.1 — Flip ClusterImagePolicy warn → enforce)
- **Família:** State / Concurrency
- **Cenário:** Workload cluster (loopzero-workload) tem 2 tenant pods running esta sessão: `e2e-tenant-production/dogfood-cli-app` + `tn-e2etest-production` pods. Suas image refs no Rollout/Deployment USAM digest pin (verifiquei PV-4 nesta sessão — `naming.ImageDigestRef()` já enforced desde 2026-04-29 — CHG-2026-04-29-DOGFOOD iter 8). MAS platform pods em loopzero-system + argocd + traefik + cert-manager NÃO usam digest pin — usam tags (`:v0.1.0`, etc.). § 7 do M07 audit confirma "Platform charts publishing pipeline ainda emite `image.tag` (não `image.digest`) em `charts/loopzero-*/values.yaml`".
- **Impacto:** Enforce flip em loopzero-workload é OK (tenant pods enforced). MAS se policy também cobre loopzero-system (platform pods), próxima reconcile ArgoCD vai REJEITAR todos os pods de plataforma → cluster degrada.
- **Fix sugerido:** **Modificar T10.1 — verificar scope de ClusterImagePolicy ANTES do flip**: `kubectl get clusterimagepolicy loopzero-images-keyless -o jsonpath='{.spec.images[*].glob}'`. Se cobre `*.svc.cluster.local/*` ou wildcard, REJEITAR Phase 10 e adicionar exclusion para platform namespaces (loopzero-system, kube-system, argocd, cert-manager). Plan v1.1 já tem warn-mode policy que sabidamente cobre só user images — confirmar scope antes de flip.

### EC-6: Cleanroom destrói Sigstore stack — Final Phase /dogfood full incompatível com Sigstore ativo

- **Task afetada:** Final Phase (/dogfood full após M2)
- **Família:** State recovery
- **Cenário:** Skill `/dogfood` SKILL.md Etapa -1 mandates "task teardown → task setup" antes do dogfood (cleanroom requirement). `task setup` NÃO inclui Sigstore stack (não está em helmfile main, é Pulumi component separado). Logo: cleanroom destrói Sigstore + helmfile recria 4 clusters do zero + Sigstore NÃO restaurada → /dogfood roda contra infra SEM Sigstore → keyless flag flips for true but stack ausente → builds falham → false "dogfood failed" attribution.
- **Impacto:** Final Phase verdict invariavelmente FAIL ou tem que rodar /dogfood sem cleanroom (viola skill regra suprema).
- **Fix sugerido:** **Modificar Final Phase para usar `/dogfood` skill explícita em modo non-cleanroom** OU **adicionar Phase 13.5 — Re-aplicar Sigstore stack pós-cleanroom**. Preferred: criar `task setup:sigstore-local` que roda APÓS `task setup` para re-provisionar Sigstore. Documentar em plan que /dogfood Final Phase usa esse fluxo. Sem isso, gate é unattainable.

### EC-7: Phase 11 chaos drill cosign-fulcio-kill — manifest tem prerequisite mas chaos-test runner pode não respeitar

- **Task afetada:** M2 Phase 11 (T11.1 — chaos drill LIVE)
- **Família:** Integração
- **Cenário:** Plan v1.1 T1.3 EC-NEW-3 introduziu `loopzero.io/prerequisite: sigstore-active` annotation em manifest `cosign-fulcio-kill.yaml`. MAS o runner `infra/scripts/test/run-scenario.sh` NÃO implementa pre-flight check de prerequisite annotation. Plan v1.1 T1.3 task #3 dizia "Update chaos-test SKILL.md scenarios list with pre-flight check logic" mas só atualizou SKILL.md (docs), NÃO o script bash.
- **Impacto:** Script tenta apply chaos manifest. Fulcio existe (Phase 7 garantiu), então labelSelector encontra pods. OK funciona. **MAS se Phase 7 falhou (EC-1) OU se rodam Phase 11 sem M2 anterior**, manifest selecta 0 pods = false positive PASS sem signal.
- **Fix sugerido:** **NOVA sub-task em Phase 11 — `T11.0 — Pre-flight check Fulcio Running` ANTES de chaos**. 3 lines bash: `kubectl --context k3d-loopzero-build get pods -n sigstore-system -l app=fulcio --field-selector=status.phase=Running | wc -l` — se 0, abort. Plan já tem isso documentado conceitualmente; só falta no script.

### EC-8: Cross-validation pode retornar APROVADO COM RESSALVAS — plan não define limite de ressalvas aceitáveis

- **Task afetada:** M1 Phase 1 (T1.1 — `/cross-validation`)
- **Família:** Process / State
- **Cenário:** Plan diz "Se APROVADO COM RESSALVAS: documentar ressalvas + proceed". Mas o que define "aceitável vs blocker"? Cross-validation skill classifica CV-C/G/M/L. Plan não estipula: "0 CRITICO + 0 GRAVE + ≤N MODERADO".
- **Impacto:** Pode passar para Phase 2 com 5 GRAVES "documentados" — score 3.7 registrado em código inválido.
- **Fix sugerido:** **Modificar T1.1 Acceptance Criteria**: explicit "Veredito = APROVADO **OU** APROVADO COM RESSALVAS **com** zero CRITICO **e** zero GRAVE". Se algum GRAVE, criar sub-tasks corretivas antes de Phase 2.

---

## SHOULD TEST

### EC-9: Math da SUMMARY média deve ser conferido

- **Task afetada:** M1 T4.1 + M2 T13.1
- **Teste sugerido:** `test_summary_average_recompute_matches` — calcular `(43.44 - 3.4 + 3.7) / 12 = 3.645` (M1) e `(43.74 - 3.7 + 3.85) / 12 = 3.6575` (M2). Plan diz "3.65" e "3.66" — arredondamento OK para half-up, mas documentar convenção (half-up vs banker's).

### EC-10: JWT TTL=1h vs Ginkgo edge-case suite 30min

- **Task afetada:** M1 T2.1 (Ginkgo edge-case)
- **Teste sugerido:** `test_jwt_mint_within_15min_of_test_start` — mint JWT fresh imediatamente antes de iniciar suite, NÃO reuse JWT desta sessão (já tem 4+ horas). Se JWT expira mid-suite, falhas pós-min-30 são falsos negatives. Adicionar comando explícito em T2.1 Tasks: `HYDRA_NAMESPACE=loopzero-system bash infra/helmfile/scripts/seed-local-user.sh && export LOOPZERO_TOKEN=$(cat ~/.loopzero/local-dogfood-token.txt)`.

### EC-11: Trivy + TruffleHog gates podem precisar conectividade externa

- **Task afetada:** M1 T2.2 (Quality gates)
- **Teste sugerido:** `test_quality_gates_skip_on_missing_external_tools` — verificar que se Trivy não consegue alcançar DOCR (rede offline durante quality gates), gate skipa graciosamente ao invés de fail. Já documentado em audit § 7 ("skip-on-missing") mas validar comportamento real.

### EC-12: Workflow_builder.go env var read — verificar precedence

- **Task afetada:** M2 T8.0 (NOVA — wire env var)
- **Teste sugerido:** `test_env_var_LOOPZERO_COSIGN_KEYLESS_overrides_default_false` — assertar que setting env=true em deployment yaml resulta em `InfraParams.CosignKeyless=true` read at API startup. Usar testify mock para os.Getenv.

---

## DOCUMENT

### EC-13: Pulumi backend authentication pode estar setup para prod stack

- **Risco aceito:** M2 Phase 7 T7.1 — `pulumi stack init sigstore-local` requer Pulumi backend authenticated. Operator pode estar logged-in em prod backend (state bucket prod), criando local stack ali. Operator deve confirmar `pulumi whoami` mostra local/personal account antes de `pulumi up`. Documentar como pré-flight check ao invés de auto-fix.

### EC-14: Plan assume tilt up rodando continuamente — sessão multi-day pode quebrar

- **Risco aceito:** M2 inteiro depende de tilt up Running em background. Se operator interrompe sessão (kill terminal), tilt morre, loopzero-api pods restart sem env vars novas. Plan deveria assumir restart-friendly setup MAS isso é environment-level. Documentar: "M2 phases devem rodar em sessão contínua OU operator deve verificar `tilt up` Running antes de cada Phase".

### EC-15: TUF ceremony LOCAL via softHSM — operator pode não ter softHSM instalado

- **Risco aceito:** Plan T6.1 diz "Install softHSM (`apt install softhsm2`) — sponsor verifica". Mas plan não tem fallback se softHSM unavailable. Aceitar como operator-action: documentar dependency upfront ao invés de adicionar fallback código.

---

## Padroes Sistemicos Detectados

| Padrao | Encontrado? | Onde |
|--------|-------------|------|
| **Implemented but not wired** | **SIM** | M2 Phase 8 — InfraParams flags existem mas env var read não wired (EC-3) |
| Correct code in wrong place | Não | — |
| Project name vs ID | Não | — |
| ArgoCD notifiers not services | Não | — |
| Single ArgoCD App per tenant | Não | — |
| Implemented but not documented | Não | — |
| Custom domains conflicting apex | Não | — |
| Live vs snapshot credentials | Não | — |
| Bootstrap circular dependency | Não | — |
| Validation procedure proves nothing | **PARCIAL** | EC-6 Final Phase /dogfood pós-cleanroom prova nada porque Sigstore não restaurada |
| GitHub tarball ≠ git bundle | Não | — |
| Auto-deploy source = commit SHA | Não | — |

**Total: 2 padrões sistêmicos do CLAUDE.md detectados.** EC-3 é o mais grave — todo M2 desmaiagado sem fix.

---

## Resumo

| Phase | Edges | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------|----------|-------------|----------|
| M1 Phase 0 | 0 | 0 | 0 | 0 |
| M1 Phase 1 | 1 | 1 | 0 | 0 |
| M1 Phase 2 | 2 | 0 | 2 | 0 |
| M1 Phase 3 | 0 | 0 | 0 | 0 |
| M1 Phase 4 | 1 | 0 | 1 | 0 |
| M1 Phase 5 | 1 | 1 | 0 | 0 |
| M2 Phase 6 | 1 | 0 | 0 | 1 |
| M2 Phase 7 | 2 | 1 | 0 | 1 |
| M2 Phase 8 | 2 | 1 | 1 | 0 |
| M2 Phase 9 | 0 | 0 | 0 | 0 |
| M2 Phase 10 | 1 | 1 | 0 | 0 |
| M2 Phase 11 | 1 | 1 | 0 | 0 |
| M2 Phase 12 | 0 | 0 | 0 | 0 |
| M2 Phase 13 | 0 | 0 | 0 | 0 |
| Final Phase | 1 | 1 | 0 | 0 |
| **Sessão-wide** | 1 | 1 | 0 | 0 |
| **TOTAL** | **14** | **7** | **4** | **3** |

---

## Recomendação de Aplicação

Veredito requer **v1.1 do plan absorvendo os 7 MUST FIX**:

1. **EC-1** — Adicionar Phase 6.5 cert-manager bootstrap em loopzero-build (~30min eng)
2. **EC-3** — Adicionar T8.0 wire env var (~1h eng + 1 test)
3. **EC-4** — Adicionar T5.0 compactar MEMORY.md antes de entrada nova (~30min)
4. **EC-5** — Modificar T10.1 verificar scope ClusterImagePolicy (~15min)
5. **EC-6** — Reescrever Final Phase para usar `task setup:sigstore-local` (~1h)
6. **EC-7** — Adicionar T11.0 pre-flight Fulcio Running (~10min)
7. **EC-8** — Modificar T1.1 acceptance criteria (zero CRITICO/GRAVE) (~5min)

**Esforço total absorção: ~3.5h adicional** vs ~30h plan original. **Vital antes de iniciar M2** — sem EC-1+EC-3 fix, M2 inteiro é teatro silencioso (passa nos comandos, score sobe, mas nada keyless ativo).

**EC-2 reclassificado para DOCUMENT** (após verify — sem cliente Fulcio em loopzero-api).

M1 phases (3.4→3.7) podem iniciar IMEDIATAMENTE absorvendo apenas EC-4 + EC-8 (totalmente locais a M1, ~35min). M2 fixes (EC-1/3/5/6/7) podem ser aplicados ANTES de Phase 6 do M2.
