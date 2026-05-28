# Plan: M07 Tenant Build Pipeline — 3.4 → ~3.85 (local-validation cap)

> **Version 1.2** — Eleva M07 score registrado de 3.4 (baseline 2026-05-26) para 3.7 (M1) → 3.80 (path 1 sign-image threading fix). **M2 (3.85 local) DEFERRED to prod DOKS plan** após Ralph iter 8 (2026-05-28) confirmar Fulcio chart 2.9.1 + K3s structural blocker. Engineering Phase 0+1 shipped (script diagnose 10/10 tests + 4 layers LIVE fixes em K3s/RBAC) mas Fulcio HTTP 400 token verification persistente apesar de: anonymous-auth=true, ClusterRoleBinding system:unauthenticated, service-account-jwks-uri DNS, kube-root-ca mount em /etc/ssl/certs/. Detalhes completos: `docs/plans/2026-05-28-M07-fulcio-oidc-trust-to-3-85-plan.md` v1.6. 3.85 target será atingido em prod DOKS (anonymous-auth=true default + Fulcio chart upgrade path).
>
> **Version 1.1** — Eleva M07 score registrado de 3.4 (baseline 2026-05-26) para 3.7 (M1 — sem activation) e depois ~3.85 (M2 — Sigstore stack LIVE em k3d local-only). Teto efetivo deste plano é ~3.85 por **D1 — DNS Local-only**: prod activation (Fulcio/Rekor com TLS público) fica para plano separado. Engineering shipped esta sessão (commits `f1ca9bdf`, `01a561f2`, `686e78e1`, `d7e79882`, `e32b6b8c`) habilita M1; sponsor confirmou ceremony para M2.

## Changelog

### v1.1 (2026-05-27, post-edge-case-review)

Absorbed 7 MUST FIX from `docs/plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-edge-case-review.md`:

- **EC-1** Phase 6.5 NEW: cert-manager + cluster-issuer bootstrap em loopzero-build (sem isso, Phase 7 Pulumi up trava em Certificate Pending)
- **EC-3** T8.0 NEW: wire env var `LOOPZERO_COSIGN_KEYLESS` + `LOOPZERO_COSIGN_ATTEST_ENABLED` em api/cmd/main.go (sem isso, M2 inteiro é teatro silencioso — pipeline executa sem erro mas keyless NUNCA ativa)
- **EC-4** T5.0 NEW: compactar MEMORY.md (já 28KB > 24.4KB limit) antes de adicionar entrada nova
- **EC-5** T10.1 modificado: verificar scope ClusterImagePolicy ANTES do flip enforce (evitar matar platform pods sem digest pin)
- **EC-6** Final Phase reescrita: `task setup:sigstore-local` re-aplica Sigstore stack pós-cleanroom (sem isso, dogfood inválido)
- **EC-7** T11.0 NEW: pre-flight check Fulcio pods Running antes de chaos drill (evitar false positive PASS)
- **EC-8** T1.1 acceptance criteria explícito: zero CRITICO + zero GRAVE (sem isso, "APROVADO COM RESSALVAS" pode esconder bloqueadores)

EC-2 (cross-cluster Fulcio URL) reclassificado para DOCUMENT após verify (sem cliente Fulcio em loopzero-api; URL só usado como argv para Argo Workflow worker em loopzero-build, resolve intra-cluster).

Esforço revisado: ~30h → ~33.5h (+3.5h absorption MUST FIX).

## Context

**Estado atual (2026-05-27):**
- M07 score **registrado**: 3.4/4.0 (audit 2026-05-26 § 6, confidence 88%)
- M07 score **estimado** pela § 7 Status Update: 3.7 hoje (não-recalculado, sem evidência consolidada)
- Plan `2026-05-27-tenant-build-pipeline-gaps-closure-plan.md` v1.1 ENGINEERING 100% shipped:
  - Phase 1 (cosign keyless deliverables): `f1ca9bdf` — storage model + 5 alerts + chaos drill + OIDC policy + TUF runbook
  - Phase 2 (SBOM-attestation linking): `01a561f2` — `cosignAttestTemplate()` + DAG wiring + DB migration 000019 + 9/9 tests PASS
  - Phase 3 (Harbor mirror observability): `01a561f2` — exit handler + Pushgateway-TTL + alerts + 8/8 tests PASS
  - Phase 4 (OTel sampling docs): `01a561f2` — `docs/operations/build-pipeline-otel.md`
- Plan Phase 5 (cross-validation) + Phase 6 (operator dogfood) NÃO executados
- Sigstore Pulumi component (`infra/pulumi-platform/src/services/sigstore.ts`) shipped iter prévia, awaiting activation

**Evidência LIVE consolidada nesta sessão (2026-05-27):**
- `task doctor`: PASS=41 FAIL=0 (paridade baseline 2026-05-25)
- Ginkgo journey: 105 PASS / 1 flake (re-run isolado 7/7 PASS) / 26 SKIP (vs baseline 106/0)
- `chaos-test --full`: 9 PASS + C01 isolado PASS = 10/10 (paridade baseline 2026-05-25 10/10)
- 1 deploy real end-to-end via `$LOOPZERO`: build SUCCEEDED 469s + Prova de Vida (pod 1/1, runAsNonRoot, SA=loopzero-app, drop=ALL, health 200) + Prova de Morte (pods removidos, ArgoCD Synced/Healthy)
- 3 bugs S0/S1 encontrados E fixados FAANG-grade (cluster-issuer bootstrap, EventBus PVC storageClassName, cleanroom session FAANG refactors)

**Motivação para registro:**
- Re-audit formal é a **única** forma de score subir oficialmente (CLAUDE.md Rule 11 — 3 evidence sources)
- Engineering ≠ Score. Sem registro, ganho engineering esta sessão fica invisível em SUMMARY.md
- ADR-2026-05-12 cosign keyless deadline 2026-08-10 (74 dias) impõe pressão temporal

**O que está fora deste plano (DNS Local-only):**
- Cosign keyless **prod** activation com Fulcio/Rekor em sub-domains públicos (e.g., `fulcio.loopzero.example`)
- Score 3.9 cap (M11 #25 ADR D3 cap aplica a portfolio inteiro, M07 individual sobe a ~3.85 com k3d local)
- Cross-cluster verification via cluster-external OIDC URL (DOKS-specific, requer prod cluster)

## Objective

Dois milestones independentes:

**M1 — Score 3.4 → 3.7 (registrado)**
- Cross-validation do plan original APROVADO
- Phase 6 Dogfood operator-run completo
- M07 re-audit formal aplicando Evidence Bar §1
- `SUMMARY.md` atualizado com 3.7 (Δ +0.3 vs 3.4 baseline)
- `CLAUDE.md` "Status" line refletindo novo score
- Memory note com path para reprodução

**M2 — Score 3.7 → ~3.85 (registrado, local-validation cap)**
- TUF ceremony LOCAL multi-party 3-of-5 quorum executado (operator HARD STOP)
- Sigstore Pulumi component aplicado em k3d (`pulumi up` LIVE)
- `CosignAttestEnabled=true` + flow keyless ativo em k3d
- 1 deploy real produzindo Rekor entry + attestation cryptographically linked SBOM (verificado via `cosign verify-attestation`)
- VAP `loopzero-require-image-digest` flipped warn→enforce
- Chaos drill `cosign-fulcio-kill` LIVE PASS
- M07 re-audit segundo aplicando Evidence Bar §1 com LIVE proof
- `SUMMARY.md` atualizado com ~3.85 (Δ +0.15)

## ADRs

### D1 — Validation scope = LOCAL k3d only (DNS dispatch deferred)

**Decisão:** Sigstore activation testada em k3d com self-signed CAs + `loopzero.local` domain. Prod DNS allocation (Fulcio/Rekor sub-domains públicos com TLS válido) fica para plano separado.

**Rationale:** Sponsor confirmou ceremony, mas DNS provisioning não foi priorizado este sprint. LIVE proof local-only é suficiente para registrar Phase 1-5 engineering como working e habilitar score ~3.85. Prod cap (3.9) requer prod DNS — sem DNS, evidência cross-cluster external OIDC é inacessível.

**Consequences:**
- ✅ Plan executável em 1 sprint sem dependência externa
- ⚠️ Cap efetivo ~3.85 (não 3.9)
- ⚠️ Prod canary stage 4-5 (1 customer + 10/50/100%) requer NEW plan com DNS allocation + ClusterImagePolicy enforce em prod
- ⚠️ OIDC issuer URL local (`https://kubernetes.default.svc.cluster.local`) não é cross-cluster viable — funciona para validar fluxo intra-k3d-build-cluster

### D2 — Cross-validation MANDATÓRIA antes de re-audit

**Decisão:** `/cross-validation 2026-05-27-tenant-build-pipeline-gaps-closure` deve retornar **APROVADO** antes de qualquer re-audit M07.

**Rationale:** Score 3.7 baseia-se em pressuposto de que engineering shipped match exato do plan original. Cross-val verifica esse match line-by-line (DoD, acceptance criteria, TDD cycles, ADRs). Sem isso, re-audit estaria registrando score sobre código não-validado.

**Consequences:**
- Re-audit acontece sobre código verificado, não assumido
- Qualquer CRITICO/GRAVE no cross-val bloqueia M1 — primeiro corrige, depois re-audita

### D3 — Re-audit M07 segue Evidence Bar §1 estrito

**Decisão:** Re-audit aplica T1 (commit+test) + T2 (LIVE log) + T3 (CI run = N/A para LoopZero). Confidence cap 90-95% (Claude single-model residual).

**Rationale:** CLAUDE.md Rule 11 mandate. Re-audit que pula evidência viola governance + produz score inflacionado.

**Consequences:**
- Score 3.7 registrado com confidence 88-92%
- TTL: 60 dias (architecture), 30 dias se operacional
- Drift detectado em audit subsequente reduce confidence

### D4 — M2 keyless stack k3d usa self-signed CA + loopzero.local

**Decisão:** Sigstore stack em k3d usa o existing `loopzero-ca-issuer` ClusterIssuer (self-signed, criado pelo cluster-issuer chart). OIDC issuer = `https://kubernetes.default.svc.cluster.local` (intra-cluster). Domain = `*.loopzero.local`.

**Rationale:** Letsencrypt em k3d requer DNS público + cert-manager DNS01 — fora de escopo D1. Self-signed CA + intra-cluster URL = funcional para validar fluxo end-to-end localmente.

**Consequences:**
- ✅ Stack provisionável sem prod resources
- ⚠️ Workload cluster external verification não testável (cross-cluster OIDC URL = intra-cluster, não resolve fora)
- ⚠️ Operator runbook documenta passos prod-specific separadamente

**Nota v1.1 EC-2 (DOCUMENT):** Fulcio URL passada via env var `LOOPZERO_FULCIO_URL=http://fulcio.sigstore-system.svc.cluster.local:8080` em loopzero-api funciona porque é usada APENAS como argv para Argo Workflow worker pod (que roda em loopzero-build cluster — intra-cluster resolve). loopzero-api em loopzero-mgmt NÃO tem cliente Fulcio direto — verificado via `grep "FulcioURL" api/` retornando apenas referência em workflow_builder.go (template argument, não chamada de rede). Sem ação adicional necessária.

### D5 — TUF ceremony LOCAL = simulada com YubiKey pessoal OR softHSM

**Decisão:** Em M2 Phase 6 (TUF ceremony), aceita 3-of-5 quorum LOCAL com 5 chaves geradas em softHSM (não YubiKey FIPS hardware). Documenta procedure prod versão (com YubiKey + air-gapped machine) mas executa LOCAL com simulated quorum.

**Rationale:** Hardware YubiKey FIPS + air-gapped machine é prod-grade. Para validar LIVE local-only, softHSM com 5 keypairs gerados em laptop suficiente para validar fluxo. Prod ceremony fica em plano separado com runbook real.

**Consequences:**
- ✅ M2 executável esta semana (sem hardware procurement)
- ⚠️ TUF root.json gerado LOCAL ≠ prod root.json (operator regera em prod ceremony)
- ⚠️ Operator runbook `docs/operations/tuf-root-ceremony.md` JÁ shipped descreve prod-grade — LOCAL variant adicionado como appendix

## Dependency Graph

```
M1 — 3.4 → 3.7 (Claude eng, ~12.5h, ZERO HARD STOPs)
   │
   ├─ Phase 0 — Verification consolidation (~1h)
   │     └─ Audit engineering shipped vs plan v1.1 (14 deliverables)
   │
   ├─ Phase 1 — /cross-validation (~3h)
   │     └─ Plan original APROVADO check (v1.1: zero CRITICO + zero GRAVE)
   │
   ├─ Phase 2 — LIVE proof gather (~2h)
   │     ├─ Ginkgo edge-case suite (complement ao journey já rodado)
   │     ├─ Quality gates full pass
   │     └─ Capture evidence em /tmp/m07-evidence-2026-05-27/
   │
   ├─ Phase 3 — M07 Re-audit Evidence Bar §1 (~3h)
   │     └─ Write .claude/knowledge-base/audits/2026-05-27/07-build-pipeline.md
   │
   ├─ Phase 4 — Aggregate update (~2h)
   │     ├─ SUMMARY.md (3.62 → 3.65 — apenas M07 sobe)
   │     └─ CLAUDE.md "Status" line
   │
   └─ Phase 5 — Commit + memory record (~1.5h)
         ├─ T5.0 — Compactar MEMORY.md (v1.1 EC-4 NEW, ~30min)
         ├─ T5.1 — Commit + push
         └─ T5.2 — Memory note
         │
         ▼
M2 — 3.7 → ~3.85 (Claude eng + operator HARD STOPs, ~21h)
   │
   ├─ Phase 6 — TUF ceremony LOCAL multi-party (~3h, OPERATOR HARD STOP)
   │     ├─ softHSM 5 keypairs
   │     ├─ root.json signed 3-of-5
   │     └─ kubectl create secret sigstore-system/tuf-root-keys
   │
   ├─ Phase 6.5 — Bootstrap cert-manager + cluster-issuer em loopzero-build (v1.1 EC-1 NEW, ~30min)
   │     ├─ helmfile sync --selector name=cert-manager k3d-loopzero-build
   │     ├─ helmfile sync --selector name=cluster-issuer k3d-loopzero-build
   │     └─ Verify ClusterIssuer Ready=True
   │
   ├─ Phase 7 — Pulumi up Sigstore k3d (~4h, Claude pode driver)
   │     ├─ pulumi stack init sigstore-local
   │     ├─ pulumi config set tufPreMintedKeysSecret tuf-root-keys
   │     ├─ pulumi up — Fulcio + Rekor + CTLog + Trillian em k3d
   │     └─ Health check 4 components Running
   │
   ├─ Phase 8 — Flip keyless mode (~2h, Claude)
   │     ├─ T8.0 — Wire env var em api/cmd/main.go (v1.1 EC-3 NEW, ~1h)
   │     ├─ T8.1 — Set LOOPZERO_COSIGN_KEYLESS=true + URLs em helm values
   │     └─ Helmfile sync mgmt cluster (loopzero-api restart)
   │
   ├─ Phase 9 — LIVE keyless build proof (~3h, Claude + operator)
   │     ├─ 1 deploy real via $LOOPZERO (app de teste)
   │     ├─ Build pipeline executa cosign-sign (keyless) + cosign-attest
   │     ├─ Query Rekor entry by image digest
   │     ├─ cosign verify-attestation passes
   │     └─ Capture Rekor URL em release.image_attestation_rekor_url
   │
   ├─ Phase 10 — VAP enforce flip (~2h, Claude)
   │     ├─ Verify ClusterImagePolicy scope (v1.1 EC-5 — platform pods excluídos)
   │     ├─ requireImageDigestEnforce=true em helm values
   │     ├─ helmfile sync workload cluster
   │     ├─ Test: deploy sem digest → REJEITADO admission
   │     └─ Test: deploy com digest válido → ADMITIDO
   │
   ├─ Phase 11 — Chaos drill cosign-fulcio-kill LIVE (~2h, Claude)
   │     ├─ T11.0 — Pre-flight Fulcio Running (v1.1 EC-7 NEW, ~10min)
   │     ├─ Inject PodChaos no Fulcio (chaos-mesh existing manifest)
   │     ├─ Tentar build during chaos
   │     ├─ Verificar build FAILED (exit code 2, CHAIN_BROKEN)
   │     └─ Recovery: Fulcio restart → build próximo PASS
   │
   ├─ Phase 12 — M07 re-audit segundo (~2h, Claude)
   │     └─ Write .claude/knowledge-base/audits/2026-05-27-keyless/07-build-pipeline.md
   │
   └─ Phase 13 — SUMMARY + CLAUDE update final (~1h)
         │
         ▼
Final Phase — Dogfood QA (operator LIVE, ~3h)
   ├─ task setup:sigstore-local — re-aplicar Sigstore pós-cleanroom (v1.1 EC-6 NEW)
   └─ /dogfood full PASS com Sigstore stack ativo
```

**Parallel opportunities:** Phase 0-2 podem rodar em paralelo (independentes). Phase 7 + Phase 8 também (Pulumi up roda enquanto Claude prepara helm values).

**Sequencing:** M2 depende de M1 completo. Phase 12 depende de Phases 6-11. Final Phase depende de M2 completo.

---

## M1 — 3.4 → 3.7 Registrado

### Phase 0: Verification consolidation engineering shipped

**Objective:** Confirma que engineering do plan v1.1 (Phases 1-5) está 100% shipped antes de cross-validation.

#### T0.1 — Inventário deliverables shipped

##### Objective
Audit cada um dos 14 arquivos previstos no plan v1.1 contra repo real + git log.

##### Evidence
Verificação live nesta sessão confirmou 14/14 deliverables presentes (output do `ls -la`). Documentar formalmente.

##### Files to edit
```
docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-status.md (NEW) — status report
```

##### Deep file dependency analysis
- Novo doc lista cada deliverable + commit hash + LOC + test count
- Referenciado por Phase 1 (`/cross-validation`)

##### Tasks
1. Listar 14 deliverables por phase
2. Mapear cada → commit SHA + LOC + tests
3. Confirmar 0 deliverables faltando
4. Documentar 3 commits desta sessão que ENGINEERING fixos paralelos (cluster-issuer, EventBus PVC, cleanroom refactors)

##### TDD
N/A — task de auditoria docs-only.

##### Acceptance Criteria
- [ ] 14/14 deliverables listed
- [ ] 0 MISSING
- [ ] 3 commits paralelos documentados

##### DoD
- [ ] Status doc committed

---

### Phase 1: Cross-validation plan original

#### T1.1 — Executar `/cross-validation` skill

##### Objective
Validar formalmente que engineering match plan v1.1 line-by-line.

##### Evidence
Skill `/cross-validation` é gate FAANG-grade per `.claude/skills/cross-validation/SKILL.md`. Sem APROVADO, score subir é cosmético.

##### Files to edit
```
.claude/knowledge-base/reviews/cross-validation/2026-05-27-tenant-build-pipeline-gaps-closure-xval-2026-05-27.md (NEW — generated by skill)
```

##### Deep file dependency analysis
- Skill lê plan + cruza com git diff + roda tests
- Output APROVADO/REPROVADO classifica BLOCKER/CRITICAL/MAJOR/MINOR/INFO

##### Tasks
1. Invocar `/cross-validation 2026-05-27-tenant-build-pipeline-gaps-closure` (formato slug)
2. **v1.1 EC-8: contar CRITICOS + GRAVES no report.** Se ZERO de ambos: proceed Phase 2.
3. Se ≥1 CRITICO OU ≥1 GRAVE: ABORT — criar sub-tasks corretivas, re-rodar até zero CRITICO + zero GRAVE.
4. Se APROVADO COM RESSALVAS apenas em MODERADO/MENOR/INFO: documentar justificativa + proceed.

##### TDD
N/A — skill execution.

##### Acceptance Criteria
- [ ] Cross-validation report committed em `.claude/knowledge-base/reviews/cross-validation/`
- [ ] **Veredito = APROVADO OU APROVADO COM RESSALVAS** (v1.1 EC-8)
- [ ] **Zero CRITICOS (CV-C) no report** (v1.1 EC-8 — bloqueante absoluto)
- [ ] **Zero GRAVES (CV-G) no report** (v1.1 EC-8 — bloqueante absoluto)
- [ ] Se MODERADO/MENOR: documentadas justificativa

##### DoD
- [ ] Report exists + veredito documented
- [ ] Counts CV-C + CV-G ambos = 0 verificados explicitamente

---

### Phase 2: LIVE proof gather

**Objective:** Complementar evidência LIVE já capturada (journey + chaos) com edge-case suite + quality gates.

#### T2.1 — Ginkgo edge-case suite

##### Objective
Run `--label-filter=edge-case` suite (complement ao journey já rodado).

##### Evidence
Plan v1.1 referencia edge-case suite (`infra/tests/e2e/edge_*.go`). Não rodado nesta sessão — gap para evidência completa.

##### Files to edit
```
/tmp/dogfood-ginkgo-edgecase-2026-05-27.xml (NEW — JUnit output)
```

##### Tasks
1. Mintar JWT fresh (`HYDRA_NAMESPACE=loopzero-system bash infra/helmfile/scripts/seed-local-user.sh`)
2. Run `LOOPZERO_E2E=1 ginkgo -v --label-filter=edge-case --timeout=30m ./infra/tests/e2e/`
3. Capture JUnit XML
4. Document pass rate em status report

##### TDD
N/A — test execution.

##### Acceptance Criteria
- [ ] JUnit XML generated
- [ ] Pass rate ≥ 90%
- [ ] Failures (if any) documented com root-cause analysis

##### DoD
- [ ] XML committed em artifacts
- [ ] Pass rate em status doc

#### T2.2 — Quality gates full pass

##### Objective
`task quality:gates` zero failures (todos os gates `loopzero-ops quality *`).

##### Evidence
CLAUDE.md Rule 11 (3 evidence sources) requer quality gates passing.

##### Files to edit
```
N/A — execução
```

##### Tasks
1. `task quality:gates` 2>&1 | tee /tmp/quality-gates-output.log
2. Document any failures
3. If failures: fix root cause ou document why acceptable

##### TDD
N/A.

##### Acceptance Criteria
- [ ] Exit 0
- [ ] Zero CRITICAL gate failures

##### DoD
- [ ] Output captured

#### T2.3 — Evidence capture consolidation

##### Objective
Consolidar todos os logs LIVE em `/tmp/m07-evidence-2026-05-27/` para feed do re-audit.

##### Tasks
1. mkdir /tmp/m07-evidence-2026-05-27/
2. cp Ginkgo journey + edge-case XML + chaos summary + doctor output + quality gates log
3. tar zcf m07-evidence-2026-05-27.tar.gz

##### Acceptance Criteria
- [ ] Tarball criado
- [ ] All sources presentes

##### DoD
- [ ] Tarball pronto para re-audit consumption

---

### Phase 3: M07 Re-audit Evidence Bar §1

#### T3.1 — Re-audit M07 aplicando Evidence Bar

##### Objective
Novo audit doc em `.claude/knowledge-base/audits/2026-05-27/07-build-pipeline.md` com score 3.7 + 3 evidence sources.

##### Evidence
CLAUDE.md Rule 11: T1 commit+test + T2 LIVE log + T3 CI run/N/A. 90-95% confidence cap.

##### Files to edit
```
.claude/knowledge-base/audits/2026-05-27/07-build-pipeline.md (NEW)
.claude/knowledge-base/audits/2026-05-27/SUMMARY.md (NEW — only M07 entry initially)
```

##### Deep file dependency analysis
- Novo audit baseado em template do 2026-05-26 module
- Atualiza § 5 Gaps: marca 4 dos 5 gaps como RESOLVED (cosign keyless infra Pulumi pronto, deadline script done, Trivy/TruffleHog re-wired, SBOM attestation shipped, Harbor obs shipped). § 6 score = 3.7.
- SUMMARY.md inicial só com M07 entry — Phase 4 atualiza aggregate

##### Deep Dives

**Evidence Bar T1 (commit+test):**
- 5 commits esta sessão: `f1ca9bdf`, `01a561f2`, `686e78e1`, `d7e79882`, `e32b6b8c`
- Phase 1: 9 hermetic tests sigstore-alerts
- Phase 2: 9/9 cosign_attest_test.go PASS
- Phase 3: 8/8 harbor_mirror_observe_test.go PASS
- Phase 4: docs-only (no tests)
- Phase 5: cross-validation (Phase 1 deste plan)

**Evidence Bar T2 (LIVE log):**
- `task doctor 41/0` (2026-05-27 ~15:30Z)
- Ginkgo journey 105 PASS / 1 flake (isolated re-run 7/7 PASS) / 26 SKIP
- chaos-test --full 9 PASS + C01 isolated PASS = 10/10
- 1 deploy real end-to-end com Prova de Vida + Prova de Morte
- Edge-case suite (Phase 2 deste plan)
- Quality gates pass (Phase 2 deste plan)

**Evidence Bar T3 (CI run):**
- N/A — GitHub Actions removed. Gate equivalente é `task quality:gates` rodado em Phase 2.

##### Tasks
1. Read 2026-05-26 audit template
2. Write new audit applying changes:
   - § 5: 4 gaps RESOLVED, 1 LOW (Harbor mirror obs shipped, alert wired)
   - § 6: 3.7 / 4.0 confidence 88%
   - § 7: Status note (3.7 deriva engineering shipped + LIVE proof)
   - § 8 NEW: Path para 3.9 cap (gated por D1 — prod DNS allocation)
3. Cross-reference engineering deliverables to commits + test paths
4. Document confidence cap 90-95% per Rule 11

##### TDD
```
RED:     test_audit_doc_exists() — Bash file check
RED:     test_audit_has_3_evidence_sources() — grep T1+T2+T3
RED:     test_audit_score_is_3_7() — yq query frontmatter (if any)
GREEN:   Write audit doc
REFACTOR: None
VERIFY:  cat .claude/knowledge-base/audits/2026-05-27/07-build-pipeline.md | head -10
```

##### Acceptance Criteria
- [ ] Audit doc 200+ lines
- [ ] 3 evidence sources present
- [ ] Score 3.7 documented
- [ ] Δ +0.3 vs baseline 3.4
- [ ] Path para 3.9 cap documentado em § 8

##### DoD
- [ ] Doc committed em .claude/knowledge-base/audits/2026-05-27/
- [ ] Memory entry indica re-audit done

---

### Phase 4: Aggregate update

#### T4.1 — SUMMARY.md atualização

##### Objective
SUMMARY.md de `2026-05-27` inclui M07 entry + recalcula média global.

##### Files to edit
```
.claude/knowledge-base/audits/2026-05-27/SUMMARY.md (UPDATE) — added 12 modules
```

##### Tasks
1. Copy SUMMARY.md de 2026-05-26 como base
2. Update M07 entry: 3.4 → 3.7 (Δ +0.3)
3. Recalcular média: anterior 3.62, com M07 subindo +0.3 / 12 módulos = +0.025 → nova média **3.65**
4. Cross-reference M07 audit doc 2026-05-27 (link)
5. Documentar quais módulos ainda estão pendentes re-audit (outros 11)

##### TDD
N/A — doc update.

##### Acceptance Criteria
- [ ] Média recalculada
- [ ] M07 row atualizada
- [ ] Cross-refs validos

##### DoD
- [ ] SUMMARY.md commited

#### T4.2 — CLAUDE.md Status line atualização

##### Objective
CLAUDE.md "Status:" line reflete nova baseline e M07 boost.

##### Files to edit
```
CLAUDE.md (UPDATE) — Status section
```

##### Tasks
1. Read CLAUDE.md "Status:" line (line ~13)
2. Update reference: "Latest snapshot: `.claude/knowledge-base/audits/2026-05-26/SUMMARY.md` (3.62/4.0)" → adicionar "Δ M07 3.4→3.7 per audit 2026-05-27, nova média 3.65"
3. Update "Maturity:" section (line ~98) similar

##### TDD
N/A.

##### Acceptance Criteria
- [ ] CLAUDE.md references both 2026-05-26 baseline + 2026-05-27 M07 update
- [ ] Média 3.65 mencionada

##### DoD
- [ ] CLAUDE.md committed

---

### Phase 5: Commit + memory record

#### T5.0 — Compactar MEMORY.md (v1.1 EC-4 NEW)

##### Objective
MEMORY.md já está em 28KB (>24.4KB limit per auto-memory warning system). Adding entry sem compactar = truncamento de entradas antigas no carregamento da próxima sessão. Compactar ANTES de T5.2 evita perda silenciosa.

##### Evidence
Confirmed LIVE nesta sessão: `wc -c /home/paulo/.claude/projects/-home-paulo-loopzero/memory/MEMORY.md` = **28085 bytes**. CLAUDE.md auto-memory mensagem warning: "MEMORY.md is 27.1KB (limit: 24.4KB) — index entries are too long. Only part of it was loaded."

##### Files to edit
```
/home/paulo/.claude/projects/-home-paulo-loopzero/memory/MEMORY.md (PRUNE)
```

##### Tasks
1. Read MEMORY.md atual
2. Identificar 5-8 entradas com data <= 2026-05-15 que já estão consolidadas em commits/CHANGELOG/audit docs (evidência redundante)
3. Para cada entry: verificar que conteúdo detalhado em file topic separado (move detail to topic file SE necessário, senão delete)
4. Remove apenas as linhas do índice (one-line entries em MEMORY.md), preservar topic files se ainda úteis
5. Verify final size: `wc -c MEMORY.md` < 24000 bytes (margem para T5.2 entry)
6. Commit individual: "memory(prune): compact MEMORY.md to fit auto-load window"

##### TDD
N/A — manual content curation.

##### Acceptance Criteria
- [ ] MEMORY.md < 24KB pós-prune
- [ ] Entradas removidas listadas no commit message (audit trail)
- [ ] Topic files preservados se ainda referenciados por entradas mantidas

##### DoD
- [ ] Size verificado via `wc -c`
- [ ] Commit independente para audit

---

#### T5.1 — Commit + push

##### Tasks
1. git add SUMMARY.md + 07-build-pipeline.md + CLAUDE.md
2. Commit "audit(M07): 3.4 → 3.7 registrado pós-engineering ship"
3. NO push (operator decide quando push develop → main)

##### Acceptance Criteria
- [ ] Commit clean
- [ ] Message follow CLAUDE.md convention

#### T5.2 — Memory note

##### Objective
Memory entry indica M07 re-audited; futuras consultas usam novo score.

##### Files to edit
```
/home/paulo/.claude/projects/-home-paulo-loopzero/memory/reference_m07_reaudit_2026_05_27.md (NEW)
```

##### Tasks
1. Write memory entry: M07 3.4→3.7 registered, evidence sources, audit doc path
2. Update MEMORY.md index

##### Acceptance Criteria
- [ ] Memory file present
- [ ] MEMORY.md indexed

---

## M2 — 3.7 → ~3.85 (gated por TUF ceremony LOCAL)

### Phase 6: TUF ceremony LOCAL multi-party

**Objective:** Generate root.json signed 3-of-5 via softHSM (LOCAL variant per D5).

#### T6.1 — Generate 5 softHSM keypairs

##### Files to edit
```
/tmp/tuf-ceremony-local-2026-05-27/keys/{1..5}.pem (NEW — softHSM keys)
docs/operations/tuf-root-ceremony.md (UPDATE) — add Local Variant appendix
```

##### Tasks
1. Install softHSM (`apt install softhsm2`) — sponsor verifica
2. softhsm2-util --init-token --label="loopzero-tuf-local"
3. Generate 5 RSA-4096 keypairs (one per simulated party)
4. Document procedure em appendix do runbook

##### Acceptance Criteria
- [ ] 5 keypairs em softHSM
- [ ] Runbook appendix documented

#### T6.2 — Sign root.json 3-of-5

##### Tasks
1. Use sigstore/scaffolding ou similar tool para gerar root.json template
2. Sign with parties 1, 2, 3 (3-of-5 quorum)
3. Validate signature via TUF spec tools
4. Output: root.json + signatures

##### Acceptance Criteria
- [ ] root.json valid TUF spec
- [ ] 3 signatures present
- [ ] Tool verify_root pass

#### T6.3 — Create K8s Secret tuf-root-keys

##### Tasks
1. kubectl create namespace sigstore-system --context k3d-loopzero-build
2. kubectl create secret generic tuf-root-keys -n sigstore-system --from-file=root.json --from-file=keys/

##### Acceptance Criteria
- [ ] Secret exists
- [ ] All key files mounted

---

### Phase 6.5: Bootstrap cert-manager + cluster-issuer em loopzero-build (v1.1 EC-1 NEW)

**Objective:** loopzero-build cluster NÃO tem cert-manager. Sigstore stack (Phase 7) precisa cert-manager para emit TLS certs internos (Fulcio + Rekor + CTLog). Sem este pre-step, Phase 7 trava com Certificate Pending forever (mesmo padrão do D-FF-03 desta sessão).

#### T6.5.1 — Sync cert-manager + cluster-issuer em loopzero-build

##### Evidence
Confirmed LIVE nesta sessão: `kubectl --context k3d-loopzero-build get pods -n cert-manager` retorna `No resources found`. `kubectl get clusterissuer` retorna `error: the server doesn't have a resource type "clusterissuer"` (CRD ausente). Cluster loopzero-build NUNCA recebeu cert-manager via bootstrap (apenas mgmt + workload + runtime-2 tem). Sigstore Pulumi component (`infra/pulumi-platform/src/services/sigstore.ts`) cria Certificate CRs que ficam Pending sem cert-manager — Pulumi up falha em readiness check.

##### Files to edit
```
Taskfile.yml (UPDATE) — add task k3d:bootstrap-build-cert-manager
```

##### Deep file dependency analysis
- Mesmo pattern de `k3d:bootstrap-workload` (linha 263) e `k3d:bootstrap-runtime-2` (linha 296)
- Helmfile já tem `cert-manager` + `cluster-issuer` releases em `runtime.helmfile.yaml.gotmpl` — selectors precisam apenas apontar para k3d-loopzero-build
- Idempotente (helmfile sync no-ops se já deployed)

##### Tasks
1. Adicionar task `k3d:bootstrap-build-cert-manager` em Taskfile.yml:
   ```yaml
   k3d:bootstrap-build-cert-manager:
     desc: |
       Bootstrap cert-manager + cluster-issuer em loopzero-build cluster.
       Required ANTES de Pulumi up Sigstore stack (per plan 2026-05-27-M07).
       Sigstore Fulcio/Rekor/CTLog precisam TLS internos via loopzero-ca-issuer.
     cmds:
       - helmfile -f infra/helmfile/runtime.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-build --selector name=cert-manager sync
       - helmfile -f infra/helmfile/runtime.helmfile.yaml.gotmpl -e standalone-multi --kube-context k3d-loopzero-build --selector name=cluster-issuer sync
   ```
2. Run: `task k3d:bootstrap-build-cert-manager`
3. Verify cert-manager pods Running:
   ```bash
   KC=$(k3d kubeconfig write loopzero-build); kubectl --kubeconfig="$KC" get pods -n cert-manager
   # Expected: cert-manager + cert-manager-cainjector + cert-manager-webhook Running
   ```
4. Verify ClusterIssuer Ready=True:
   ```bash
   kubectl --kubeconfig="$KC" get clusterissuer loopzero-ca-issuer -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
   # Expected: "True"
   ```

##### TDD
```
RED:     test_taskfile_has_bootstrap_build_cert_manager_target — yq query Taskfile.yml
RED:     test_cert_manager_pods_running_post_bootstrap — kubectl assertion
RED:     test_cluster_issuer_ready_true — kubectl jsonpath assertion
GREEN:   Add task + run + verify
VERIFY:  task k3d:bootstrap-build-cert-manager + kubectl get pods + clusterissuer
```

##### Acceptance Criteria
- [ ] Task target adicionado em Taskfile.yml
- [ ] cert-manager 3 pods Running em loopzero-build
- [ ] ClusterIssuer loopzero-ca-issuer Ready=True
- [ ] Idempotente (re-run no-op)

##### DoD
- [ ] Taskfile.yml committed
- [ ] LIVE verification recorded

---

### Phase 7: Pulumi up Sigstore k3d

**Objective:** Apply `infra/pulumi-platform/src/services/sigstore.ts` em k3d local.

**Dependency:** Phase 6.5 MUST be complete (cert-manager + ClusterIssuer Ready em loopzero-build).

#### T7.1 — Pulumi stack init sigstore-local

##### Files to edit
```
infra/pulumi-platform/Pulumi.sigstore-local.yaml (NEW) — k3d stack config
```

##### Tasks
1. cd infra/pulumi-platform && npx pulumi stack init sigstore-local
2. pulumi config set tufPreMintedKeysSecret tuf-root-keys
3. pulumi config set namespace sigstore-system
4. pulumi config set domain loopzero.local
5. pulumi config set --secret tlsCAIssuerName loopzero-ca-issuer

##### Acceptance Criteria
- [ ] Stack exists
- [ ] All config keys present

#### T7.2 — Pulumi up Sigstore stack

##### Tasks
1. KUBECONFIG=$(k3d kubeconfig write loopzero-build) pulumi up --stack sigstore-local --yes
2. Wait for 4 components Running: Fulcio + Rekor + CTLog + Trillian
3. Health check: `kubectl get pods -n sigstore-system` all Running

##### Acceptance Criteria
- [ ] pulumi up exit 0
- [ ] 4 pods Running
- [ ] Health endpoints respond 200

#### T7.3 — Validate stack endpoints

##### Tasks
1. curl http://fulcio.loopzero.local/api/v2/configuration
2. curl http://rekor.loopzero.local/api/v1/log
3. Document any failures

##### Acceptance Criteria
- [ ] Both endpoints respond

---

### Phase 8: Flip keyless mode

#### T8.0 — Wire env var em loopzero-api startup (v1.1 EC-3 NEW)

##### Objective
`InfraParams.CosignKeyless` + `.CosignAttestEnabled` + URLs existem em `workflow_builder.go:48-66` MAS NÃO há código que lê env var e injeta esses valores em construction. Sem este wire, T8.1 helm values ficam ignorados — flags permanecem default false — keyless NUNCA ativa apesar de pipeline executar sem erro.

##### Evidence
Confirmed LIVE nesta sessão: `grep "LOOPZERO_COSIGN_KEYLESS\|LOOPZERO_COSIGN_ATTEST_ENABLED" api/cmd/ api/internal/config/` retorna **zero matches**. Padrão CLAUDE.md "Implemented but not wired" classic — código InfraParams existe mas zero call site lê env var.

##### Files to edit
```
api/cmd/main.go (UPDATE) — ler env vars em InfraParams construction
api/internal/argo/workflow_builder.go (NO CHANGE — InfraParams struct já tem campos)
api/internal/argo/workflow_builder_test.go (UPDATE) — test env var loading
```

##### Deep file dependency analysis
- `api/cmd/main.go` constrói `argo.BuildOrchestratorDeps` ou similar contendo InfraParams — find local exato via `grep -n "InfraParams" api/cmd/*.go api/internal/services/*.go`
- Env vars adicionadas:
  - `LOOPZERO_COSIGN_KEYLESS` (bool, default false)
  - `LOOPZERO_COSIGN_ATTEST_ENABLED` (bool, default false)
  - `LOOPZERO_FULCIO_URL` (string, default empty — uses cosign default)
  - `LOOPZERO_REKOR_URL` (string, default empty — uses cosign default)
- Existente env var read pattern em loopzero-api: ver `os.Getenv` calls em main.go para inspiração

##### Deep Dives
- **Bool parsing:** Usar `strconv.ParseBool` para aceitar `true`/`1`/`yes` → InfraParams.CosignKeyless.
- **Url validation:** Se `LOOPZERO_FULCIO_URL` set mas `LOOPZERO_COSIGN_KEYLESS=false`, log warning (URL irrelevante sem keyless ativo).
- **Default behavior:** Ambos defaults false preservam comportamento atual (keyed mode) — zero regression risk.
- **Helm values injection:** Phase T8.1 já especifica env vars no loopzero-api deployment. Quando T8.0 wire merged, helm sync → pod env → main.go read → InfraParams populated. End-to-end loop fecha.

##### Tasks
1. `grep -n "InfraParams{" api/cmd/*.go api/internal/services/*.go` — localize construction
2. Adicionar reads em main.go ANTES de construction:
   ```go
   cosignKeyless, _ := strconv.ParseBool(os.Getenv("LOOPZERO_COSIGN_KEYLESS"))
   cosignAttestEnabled, _ := strconv.ParseBool(os.Getenv("LOOPZERO_COSIGN_ATTEST_ENABLED"))
   fulcioURL := os.Getenv("LOOPZERO_FULCIO_URL")
   rekorURL := os.Getenv("LOOPZERO_REKOR_URL")
   ```
3. Inject em InfraParams:
   ```go
   infraParams := argo.InfraParams{
       // ...existing fields...
       CosignKeyless:       cosignKeyless,
       CosignAttestEnabled: cosignAttestEnabled,
       CosignFulcioURL:     fulcioURL,
       CosignRekorURL:      rekorURL,
   }
   ```
4. Log structured: `slog.Info("cosign config", "keyless", cosignKeyless, "attest_enabled", cosignAttestEnabled, "fulcio_url", fulcioURL, "rekor_url", rekorURL)` — observability ao boot

##### TDD
```
RED:     test_env_var_LOOPZERO_COSIGN_KEYLESS_true_sets_InfraParams_CosignKeyless()
RED:     test_env_var_LOOPZERO_COSIGN_ATTEST_ENABLED_true_sets_InfraParams_CosignAttestEnabled()
RED:     test_env_var_LOOPZERO_FULCIO_URL_propagates_to_InfraParams()
RED:     test_default_CosignKeyless_is_false_when_env_unset()
RED:     test_invalid_bool_value_logs_warning_defaults_false()
GREEN:   Add env reads em main.go
REFACTOR: Extract to function loadCosignConfigFromEnv() if main.go fica longo
VERIFY:  cd api && go test ./internal/argo/ -run TestEnvVar
```

##### Acceptance Criteria
- [ ] Env var reads adicionados em main.go (ou config loader)
- [ ] 5 testes PASS (true/false/url/default/invalid)
- [ ] Log structured emite config ao boot
- [ ] Zero regression em testes existentes
- [ ] Pass: `cd api && go test ./...`
- [ ] Pass: `cd api && go vet ./...`

##### DoD
- [ ] Todos RED tests escritos primeiro
- [ ] All GREEN tests pass
- [ ] Logging structured presente
- [ ] Commit: "fix(api): wire LOOPZERO_COSIGN_KEYLESS env var to InfraParams (closes EC-3)"

---

#### T8.1 — Update workflow_builder InfraParams

##### Files to edit
```
infra/helmfile/values/standalone-multi/loopzero-api.yaml.gotmpl (UPDATE) — set env vars
```

##### Tasks
1. Set `LOOPZERO_COSIGN_KEYLESS=true` em loopzero-api values
2. Set `LOOPZERO_COSIGN_ATTEST_ENABLED=true`
3. Set `LOOPZERO_FULCIO_URL=http://fulcio.sigstore-system.svc.cluster.local:8080`
4. Set `LOOPZERO_REKOR_URL=http://rekor.sigstore-system.svc.cluster.local:3000`
5. helmfile sync loopzero-api

##### TDD
```
RED:     test_workflow_builder_emits_keyless_args_when_enabled()
GREEN:   InfraParams flag wired (already exists in workflow_builder.go:48)
VERIFY:  helm template + grep cosign args
```

##### Acceptance Criteria
- [ ] loopzero-api pods restart com new env
- [ ] Pods Running 2/2

##### DoD
- [ ] Helm sync done

---

### Phase 9: LIVE keyless build proof

#### T9.1 — Deploy test app via $LOOPZERO

##### Tasks
1. cd /tmp && rm -rf m07-keyless-app && mkdir m07-keyless-app && cd m07-keyless-app
2. git init + create index.js + package.json (per dogfood pattern)
3. $LOOPZERO init --yes --name m07-keyless-app --json
4. $LOOPZERO deploy --no-wait --json → capture build_id
5. Wait build SUCCEEDED (~8min)
6. PV: pod Running + health 200

##### Acceptance Criteria
- [ ] Build SUCCEEDED
- [ ] Pod 1/1 Running
- [ ] Health 200

#### T9.2 — Verify cosign keyless signature

##### Tasks
1. cosign verify --keyless image@sha256:DIGEST → should pass
2. cosign tree image@sha256:DIGEST → should show 1 signature + 1 attestation
3. Capture Rekor entries: cosign tree --output json

##### Acceptance Criteria
- [ ] cosign verify exit 0
- [ ] Signature + attestation both present

#### T9.3 — Verify SBOM attestation linking

##### Tasks
1. cosign verify-attestation --type cyclonedx image@sha256:DIGEST → should pass
2. cosign download attestation image@sha256:DIGEST | jq → CycloneDX BOM
3. Validate SBOM components match buildkit output

##### Acceptance Criteria
- [ ] verify-attestation pass
- [ ] SBOM has > 0 components
- [ ] Image digest matches

#### T9.4 — Verify DB record

##### Tasks
1. psql query: SELECT image_attestation_rekor_url FROM releases WHERE id = '<release_id>'
2. URL should match Rekor entry from T9.2

##### Acceptance Criteria
- [ ] DB column populated
- [ ] URL matches Rekor

---

### Phase 10: VAP enforce flip

#### T10.1 — Flip ClusterImagePolicy warn → enforce (v1.1 EC-5 — verify scope first)

##### Evidence
M07 audit § 7 confirma: "Platform charts publishing pipeline ainda emite `image.tag` (não `image.digest`) em `charts/loopzero-*/values.yaml`". Se ClusterImagePolicy cobre namespaces loopzero-system/argocd/cert-manager via wildcard (`*.svc.cluster.local/*`), flip warn→enforce vai REJEITAR todos os pods de plataforma na próxima reconcile.

##### Files to edit
```
infra/helm/admission-policies/values.yaml (UPDATE) — requireImageDigestEnforce=true
infra/helm/admission-policies/templates/vap-require-image-digest.yaml (READ-ONLY check) — verify scope
```

##### Tasks
1. **v1.1 EC-5 pre-flight — verify scope ClusterImagePolicy ANTES do flip:**
   ```bash
   kubectl --context k3d-loopzero-workload get clusterimagepolicy loopzero-images-warn -o jsonpath='{.spec.images[*].glob}'
   # Expected: glob específico tenant images (e.g., "k3d-loopzero-registry:5000/*", "registry.digitalocean.com/loopzero-system/tenant-*")
   # NOT acceptable: "*" or "*.svc.cluster.local/*" (cobriria platform pods)
   ```
2. **Se scope cobre platform images:** ABORT flip + criar sub-task corretiva: adicionar exclusion regex para namespaces loopzero-system/argocd/cert-manager/kube-system. Re-rodar pre-flight.
3. **Se scope OK (tenant-only):** Set `requireImageDigestEnforce: true` em values.yaml
4. helmfile sync admission-policies em loopzero-workload
5. Test positivo: deploy app COM digest pin (`naming.ImageDigestRef()` já default tenant pipeline desde 2026-04-29) → ADMITIDO
6. Test negativo: tentar `kubectl run debug --image=busybox:1.36` (sem digest) → REJEITADO admission
7. Verify platform pods ainda Running (sanity check post-flip):
   ```bash
   for ns in loopzero-system argocd cert-manager kube-system; do
     kubectl --context k3d-loopzero-workload get pods -n $ns | grep -v Running | head -3
   done
   # Expected: only header lines (zero non-Running)
   ```

##### TDD
```
RED:     test_clusterimagepolicy_scope_excludes_platform_namespaces — kubectl jsonpath
RED:     test_admission_rejects_pod_without_digest — kubectl run failure expected
RED:     test_admission_accepts_pod_with_digest_pin — kubectl apply success expected
RED:     test_platform_pods_still_running_post_flip — sanity
GREEN:   Verify + flip + helmfile sync
VERIFY:  Sequence acima + kubectl get pods all namespaces
```

##### Acceptance Criteria
- [ ] Pre-flight scope check confirmed tenant-only (v1.1 EC-5)
- [ ] Without-digest test FAILS admission
- [ ] With-digest test PASSES admission
- [ ] Zero platform pods em estado degradado pós-flip

##### DoD
- [ ] Scope check executed + result documented em log
- [ ] If scope ampliado: sub-task corretiva committed BEFORE flip
- [ ] LIVE assertion zero platform degradation

---

### Phase 11: Chaos drill cosign-fulcio-kill LIVE

#### T11.0 — Pre-flight Fulcio Running (v1.1 EC-7 NEW)

##### Objective
Manifest `cosign-fulcio-kill.yaml` tem `loopzero.io/prerequisite: sigstore-active` annotation (per plan v1.1 T1.3 EC-NEW-3), MAS o runner `infra/scripts/test/run-scenario.sh` NÃO implementa pre-flight check do prerequisite annotation. Sem este pre-step, manifest aplica labelSelector com 0 pods alvo = chaos resource criada mas sem efeito = false positive PASS (chaos returns "completed" pq não havia nada para matar).

##### Evidence
Plan v1.1 T1.3 task #3 disse "Update chaos-test SKILL.md scenarios list with pre-flight check logic" mas só docs atualizou — script bash NÃO foi modificado. SKILL.md updates não são executable.

##### Files to edit
```
infra/scripts/test/run-scenario.sh (UPDATE) — adicionar pre-flight prerequisite annotation check
infra/scripts/test/run-scenario_test.sh (NEW) — hermetic test
```

##### Tasks
1. Adicionar bash function `check_prerequisite()` em run-scenario.sh ANTES de injection step:
   ```bash
   check_prerequisite() {
     local manifest="$1"
     local prereq=$(yq '.metadata.annotations["loopzero.io/prerequisite"]' "$manifest" 2>/dev/null)
     [ "$prereq" = "null" ] && return 0  # no prerequisite = OK
     case "$prereq" in
       sigstore-active)
         local fulcio_pods=$(kubectl --context k3d-loopzero-build -n sigstore-system get pods -l app=fulcio --field-selector=status.phase=Running -o name 2>/dev/null | wc -l)
         [ "$fulcio_pods" -gt 0 ] || { echo "[chaos] SKIP: prerequisite sigstore-active NOT satisfied (0 Fulcio pods Running)"; exit 0; }
         ;;
       *)
         echo "[chaos] WARN: unknown prerequisite '$prereq' — not checked"
         ;;
     esac
   }
   check_prerequisite "$MANIFEST"
   ```
2. Hermetic test simulando prerequisite ausente: criar mock kubectl que retorna 0 pods → script deve exitar 0 com SKIP message
3. Hermetic test prerequisite satisfeito: mock kubectl retorna 1 pod → script prossegue para injection

##### TDD
```
RED:     test_run_scenario_skips_when_prerequisite_not_satisfied — bash test + grep SKIP em output
RED:     test_run_scenario_proceeds_when_prerequisite_satisfied — bash test + grep "Etapa 3" em output
RED:     test_run_scenario_proceeds_when_no_prerequisite_annotation — backward compat
GREEN:   Add check_prerequisite function
VERIFY:  bash infra/tests/scripts/check-run-scenario-prerequisite_test.sh
```

##### Acceptance Criteria
- [ ] Pre-flight function adicionada em run-scenario.sh
- [ ] 3 hermetic tests PASS
- [ ] Backward compat: manifests sem annotation continuam funcionando (Phase 11 Final Phase passa)

##### DoD
- [ ] Script updated + tests committed
- [ ] Functional verification: run-scenario com manifest cosign-fulcio-kill ANTES de Phase 7 (sigstore-system vazio) → SKIP. APÓS Phase 7 → proceed.

---

#### T11.1 — Inject Fulcio PodChaos + verify build FAIL

##### Tasks
1. bash infra/scripts/test/run-scenario.sh --scenario cosign-fulcio-kill --target-context k3d-loopzero-build
2. Trigger build during chaos via $LOOPZERO deploy
3. Verify build STATUS=FAILED + error code CHAIN_BROKEN
4. Post-recovery: next build PASS

##### Acceptance Criteria
- [ ] During chaos: build FAILED
- [ ] Post recovery: build PASS

---

### Phase 12: M07 re-audit segundo

#### T12.1 — Re-audit M07 com LIVE keyless evidence

##### Files to edit
```
.claude/knowledge-base/audits/2026-05-27-keyless/07-build-pipeline.md (NEW)
.claude/knowledge-base/audits/2026-05-27-keyless/SUMMARY.md (NEW)
```

##### Deep Dives
- Same Evidence Bar §1 structure as M1 Phase 3
- T2 LIVE log INCLUI: Phases 7-11 evidence (Pulumi up + LIVE keyless build + Rekor verify + VAP enforce + chaos drill LIVE)
- Score = 3.85 / 4.0 confidence 90%
- § 8 Path para 3.9 cap: ONLY gated por D1 (prod DNS) — engineering 100% done

##### Acceptance Criteria
- [ ] Audit doc 250+ lines
- [ ] Score 3.85
- [ ] LIVE keyless evidence cited

---

### Phase 13: SUMMARY + CLAUDE update final

#### T13.1 — SUMMARY.md final update

##### Tasks
1. Recompute média: M07 3.85 (+0.45 vs 3.4) / 12 = ~3.66 (vs 3.62 baseline)
2. Note dois milestones: M1 fechou 3.7, M2 fechou 3.85
3. Cross-link both audit docs

##### Acceptance Criteria
- [ ] SUMMARY reflects both milestones

#### T13.2 — CLAUDE.md final update

##### Tasks
1. Update Status section
2. Document path para 3.9 cap (D1 prod DNS only)

##### Acceptance Criteria
- [ ] CLAUDE.md committed

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | M07 score not formally registered | M1 Phase 3 | Re-audit aplicando Evidence Bar |
| 2 | Engineering shipped invisible em SUMMARY | M1 Phase 4 | Aggregate update |
| 3 | Cross-validation plan original não rodado | M1 Phase 1 | Skill execution (v1.1 EC-8 zero CRITICO/GRAVE) |
| 4 | LIVE proof keyless ausente | M2 Phase 9 | Deploy real + cosign verify |
| 5 | TUF ceremony nunca executada | M2 Phase 6 | softHSM local-variant |
| 6 | Sigstore stack nunca aplicada | M2 Phase 7 (gated por 6.5) | Pulumi up k3d |
| 7 | VAP digest enforcement warn-mode | M2 Phase 10 | Flip enforce (v1.1 EC-5 scope check) |
| 8 | Chaos drill cosign-fulcio-kill nunca LIVE | M2 Phase 11 (gated por T11.0) | Real chaos run |
| 9 | Aggregate SUMMARY desatualizado | M1 Phase 4 + M2 Phase 13 | Both updates |
| 10 | CLAUDE.md Status drift | M1 Phase 4 + M2 Phase 13 | Both updates |
| **11 (v1.1 EC-1)** | cert-manager AUSENTE em loopzero-build | **M2 Phase 6.5 NEW** | `task k3d:bootstrap-build-cert-manager` |
| **12 (v1.1 EC-3)** | InfraParams.CosignKeyless código existe mas env var NÃO wired | **M2 T8.0 NEW** | Wire env var em api/cmd/main.go |
| **13 (v1.1 EC-4)** | MEMORY.md já 28KB > 24.4KB limit | **M1 T5.0 NEW** | Compactar antes de adicionar entry |
| **14 (v1.1 EC-6)** | Cleanroom destrói Sigstore stack | **Final Phase TFinal.0 NEW** | `task setup:sigstore-local` |
| **15 (v1.1 EC-7)** | run-scenario.sh ignora prerequisite annotation | **M2 T11.0 NEW** | check_prerequisite() function em script |

**Coverage: 15/15 (100%)** — v1.1 +5 (absorption MUST FIX edge-case review)

## Global Definition of Done

### M1 done quando:
- [ ] Cross-validation APROVADO em `.claude/knowledge-base/reviews/cross-validation/`
- [ ] LIVE proof tarball em /tmp/m07-evidence-2026-05-27/
- [ ] Audit doc 2026-05-27/07-build-pipeline.md committed com score 3.7
- [ ] SUMMARY.md aggregate atualizado
- [ ] CLAUDE.md Status atualizado
- [ ] Memory note + MEMORY.md indexed
- [ ] Commit clean (sem co-autoria per memory)

### M2 done quando:
- [ ] TUF ceremony LOCAL executada (3-of-5 softHSM)
- [ ] Sigstore stack k3d 4 components Running
- [ ] 1 LIVE deploy com keyless cosign verify PASS
- [ ] SBOM attestation linked + DB column populated
- [ ] VAP digest enforce flipped + tests admission pass/fail
- [ ] Chaos drill cosign-fulcio-kill LIVE PASS
- [ ] Audit doc 2026-05-27-keyless/07-build-pipeline.md committed com score 3.85
- [ ] Aggregate SUMMARY.md final atualizado
- [ ] CLAUDE.md Status final atualizado

### Global:
- [ ] Zero `task quality:gates` regressions
- [ ] Backward compatibility preserved
- [ ] Operator runbook documenta path para prod 3.9 cap (separate plan)

## Final Phase: Dogfood QA (MANDATORY após M2 — v1.1 EC-6 rewrite)

### Evidence
Skill `/dogfood` SKILL.md Etapa -1 mandates `task teardown → task setup` ANTES de dogfood (cleanroom requirement). `task setup` NÃO inclui Sigstore stack (Pulumi component separado, não está em helmfile main). Logo: cleanroom destrói Sigstore stack → helmfile recria 4 clusters do zero SEM Sigstore → /dogfood roda contra infra sem Sigstore → LOOPZERO_COSIGN_KEYLESS=true flag flips but stack ausente → builds falham → false "dogfood failed" attribution para mudanças do M2.

Sem fix EC-6, Final Phase é UNATTAINABLE (sempre fail ou viola skill regra suprema).

### Pre-requisites (operator HARD STOP)

Adicionar task `setup:sigstore-local` que re-aplica Sigstore stack pós-cleanroom. Esta task NÃO existe ainda — criar como parte de Final Phase setup:

#### TFinal.0 — Create `task setup:sigstore-local`

##### Files to edit
```
Taskfile.yml (UPDATE) — add setup:sigstore-local task
```

##### Tasks
1. Adicionar em Taskfile.yml:
   ```yaml
   setup:sigstore-local:
     desc: |
       Re-aplicar Sigstore stack em k3d-loopzero-build pós-cleanroom.
       Required ANTES de /dogfood full quando keyless mode ativo (M2 Phase 8+).
       Pre-condicao: TUF root keys Secret EXISTE em sigstore-system ns (M2 Phase 6).
     cmds:
       - task: k3d:bootstrap-build-cert-manager  # Phase 6.5 task
       - cd infra/pulumi-platform && KUBECONFIG=$(k3d kubeconfig write loopzero-build) npx pulumi up --stack sigstore-local --yes --skip-preview
       - bash -c 'KC=$(k3d kubeconfig write loopzero-build); for i in {1..60}; do kubectl --kubeconfig="$KC" get pods -n sigstore-system | grep -E "fulcio|rekor|ctlog|trillian" | grep -v Running | head -1; sleep 5; done; kubectl --kubeconfig="$KC" get pods -n sigstore-system'
   ```
2. Document operator runbook: pre-Final-Phase, operator runs `task setup:sigstore-local` explicitly após cleanroom

##### Acceptance Criteria
- [ ] Task target adicionado
- [ ] LIVE verify: pós-cleanroom, `task setup:sigstore-local` restaura Sigstore stack 4/4 pods Running

### Execution (revised)

```bash
# Operator runs após M2 commits:
task clean:all && task setup      # cleanroom restaura clusters + base infra
tilt up                            # API + operators
task setup:sigstore-local          # v1.1 EC-6 NEW: re-aplicar Sigstore
task doctor                        # 41/0 pre-flight gate

# Then dogfood:
/dogfood full
```

### Acceptance Criteria
- [ ] `task setup:sigstore-local` task created + functional (TFinal.0)
- [ ] Pós-cleanroom: Sigstore stack 4 components Running
- [ ] Health score ≥ 70/100
- [ ] Zero CRITICAL plan-caused
- [ ] Build pipeline ainda E2E funcional
- [ ] Keyless signatures verifiable em journey tests
- [ ] Final Phase NÃO requer skipar cleanroom (Regra Suprema do /dogfood preservada)

### If Dogfood Fails
1. Identify plan-caused vs pre-existing
2. Fix CRITICAL + HIGH from this plan
3. Re-run /dogfood full (com `task setup:sigstore-local` repeat)
4. Pre-existing issues logged but NOT block

---

## References

- M07 baseline audit: `.claude/knowledge-base/audits/2026-05-26/07-build-pipeline.md`
- Plan original engineering: `docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md` v1.1
- ADR-2026-05-12 cosign keyless cutover: `.claude/knowledge-base/decisions/`
- ADR-2026-05-27-T2.2-OFFLINE-TUF: `.claude/knowledge-base/decisions/2026-05-27-T2.2-offline-tuf-ceremony.md`
- Sigstore Pulumi component: `infra/pulumi-platform/src/services/sigstore.ts`
- TUF ceremony runbook: `docs/operations/tuf-root-ceremony.md`
- Sigstore storage model: `docs/operations/sigstore-storage-model.md`
- Roadmap macro 2026-05-27: `docs/strategy/roadmap-macro-2026-05-27.md` R1

---

## Estimated Effort (v1.1 revised)

| Milestone | Claude eng | Operator HARD STOPs | Total |
|-----------|-----------|---------------------|-------|
| M1 (3.4→3.7) | ~12.5h (+0.5h T5.0 prune) | 0h | ~12.5h |
| M2 (3.7→3.85) | ~14h (+1.5h T8.0 + Phase 6.5 + T11.0) | ~6h (ceremony) | ~20h |
| Final Phase | ~1.5h (TFinal.0 + dogfood) | ~1.5h (operator-run dogfood) | ~3h |
| **Total v1.1** | **~28h** | **~7.5h** | **~35.5h** |

**Delta vs v1.0:** +5.5h (de ~30h para ~35.5h) — absorption de 7 MUST FIX + Final Phase TFinal.0.

**Critical path:** M1 sequential (Phase 0→5). M2 gated em Phase 6 TUF ceremony — sem isso, Phase 7+ não rodam. Phase 6.5 (cert-manager) DEVE preceder Phase 7. T8.0 DEVE preceder T8.1.

**Parallelism:** Em M2, Phase 6.5 (cert-manager bootstrap) e Phase 6 (TUF ceremony) podem rodar paralelos (clusters distintos). Phase 7 (Pulumi up) e T8.0 (env var wire código) podem rodar paralelos enquanto chains chegam em T8.1 → Phase 9.

**Recommended order de execução:**
1. M1: Phase 0 → 1 → 2 → 3 → 4 → 5 (com T5.0 antes de T5.1)
2. M2: (Phase 6 // Phase 6.5) → Phase 7 → (T8.0 // T8.1 setup) → Phase 9 → Phase 10 → Phase 11 → Phase 12 → Phase 13
3. Final Phase: TFinal.0 → cleanroom → setup:sigstore-local → /dogfood full
