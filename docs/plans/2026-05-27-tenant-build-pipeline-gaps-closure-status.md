---
type: plan
title: Plan 2026-05-27-tenant-build-pipeline-gaps-closure v1.1
---

# Status — Plan 2026-05-27-tenant-build-pipeline-gaps-closure v1.1

**Data:** 2026-05-27
**Source plan:** [`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`](./2026-05-27-tenant-build-pipeline-gaps-closure-plan.md) v1.1
**Status:** **ENGINEERING 100% SHIPPED** (14/14 deliverables)

---

## Sumário

Plan v1.1 propos 4 phases engineering + Phase 5 cross-validation + Phase 6 dogfood operator. Engineering (Phases 1-4) completou em 2 commits sequenciais nesta sessão (2026-05-27):

- **Commit `f1ca9bdf`** — Phase 1 (cosign keyless engineering deliverables): 7 arquivos
- **Commit `01a561f2`** — Phase 2 + 3 + 4 + 5 (SBOM attestation + Harbor obs + OTel docs): 7 arquivos
- **+ 3 commits paralelos de bugs LIVE encontrados nesta sessão**: `686e78e1`, `d7e79882`, `e32b6b8c`

Phase 5 (cross-validation) + Phase 6 (operator dogfood) NÃO executados pelo plan original — **abordados pelo plan derivado** [`2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md`](./2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md) v1.1.

---

## Inventário deliverables por phase

### Phase 1 — Cosign keyless engineering (commit `f1ca9bdf`)

| Task | Arquivo | LOC | Status |
|------|---------|-----|--------|
| T1.1 | [`docs/operations/sigstore-storage-model.md`](../operations/sigstore-storage-model.md) | 162 | ✓ shipped |
| T1.1 | `infra/helm/admission-policies/templates/sigstore-storage-alerts.yaml` | 57 | ✓ shipped |
| T1.1 | `infra/scripts/security/rekor-archive-old-entries.sh` | 82 | ✓ shipped (allowlisted) |
| T1.2 | `infra/helm/admission-policies/templates/sigstore-alerts.yaml` | 123 | ✓ shipped (5 alerts, `_seconds` suffix v1.1 EC-NEW-1 fix) |
| T1.2.b | `infra/helm/admission-policies/templates/tuf-expiry-cronjob.yaml` | 106 | ✓ shipped (Pushgateway 48h TTL) |
| T1.3 | `infra/tests/chaos/manifests/cosign-fulcio-kill.yaml` | 58 | ✓ shipped (`loopzero.io/prerequisite: sigstore-active` annotation v1.1 EC-NEW-3) |
| T1.4 | `charts/loopzero-admission-policies/templates/cluster-image-policy.yaml` + 4 files | modified | ✓ shipped (Helm `fail()` + multi-cluster OIDC URL v1.1 EC-NEW-4) |
| T1.5 | [`docs/operations/tuf-root-ceremony.md`](../operations/tuf-root-ceremony.md) | 223 | ✓ shipped (7-section procedure) |

### Phase 2 — SBOM-to-image attestation (commit `01a561f2`)

| Task | Arquivo | LOC | Status |
|------|---------|-----|--------|
| T2.1 | `api/internal/argo/cosign_attest.go` | 168 | ✓ shipped (`cosignAttestTemplate()` + `attestStepActiveDeadlineSeconds=180` v1.1 EC-NEW-6) |
| T2.1 | `api/internal/argo/cosign_attest_test.go` | 183 | ✓ shipped (**9 tests PASS** — HasActiveDeadlineSeconds180, UsesCycloneDXPredicateType, ReadsImageDigestFromMetadata, ReferencesSBOMFromSyftStep, FailsLoudIfSBOMAbsent, IdempotentForSameDigest, NoContinueOnFailure, ExportsRekorURL) |
| T2.1 | `api/internal/argo/workflow_builder.go` | modified | ✓ DAG dep `[signTaskName, sbomTaskName]` v1.1 EC-NEW-5 (BOTH cosign-sign AND syft) |
| T2.2 | `api/internal/db/migrations/000019_image_attestation_rekor_url.up.sql` | 22 | ✓ shipped (forward-only column NULLABLE) |
| T2.2 | `api/internal/db/release_store.go` | modified | ✓ `Release.ImageAttestationRekorURL *string` |
| T2.2 | `api/internal/services/build_orchestrator.go` | modified | ✓ `AppRelease.ImageAttestationRekorURL` field |
| T2.2 | `api/internal/services/container_delivery.go` | modified | ✓ sets `release.ImageAttestationRekorURL = &url` |

### Phase 3 — Harbor mirror observability (commit `01a561f2`)

| Task | Arquivo | LOC | Status |
|------|---------|-----|--------|
| T3.1 | `api/internal/argo/harbor_mirror_observe.go` | 145 | ✓ shipped (exit handler + `--max-time 10 --connect-timeout 5` v1.1 EC-NEW-8 + `Push-Lifetime: 86400` v1.1 EC-NEW-7 ADR D5) |
| T3.1 | `api/internal/argo/harbor_mirror_observe_test.go` | 109 | ✓ shipped (**8 tests PASS**) |
| T3.1 | `infra/helm/admission-policies/templates/harbor-mirror-alerts.yaml` | 56 | ✓ shipped (LoopZeroHarborMirrorFailureRateHigh + LoopZeroHarborMirrorAnyFailure) |

### Phase 4 — OTel sampling docs (commit `01a561f2`)

| Task | Arquivo | LOC | Status |
|------|---------|-----|--------|
| T4.1 | [`docs/operations/build-pipeline-otel.md`](../operations/build-pipeline-otel.md) | 156 | ✓ shipped (head-based 10% + `LOOPZERO_BUILD_FORCE_TRACE` escape v1.1 EC-NEW-9 ADR D3) |

**Total Phase 1-4: 14 arquivos NEW / 6 arquivos MODIFIED.**

---

## Commits paralelos LIVE-discovered (não em plan original)

Estes 3 commits são bugs S0/S1 descobertos LIVE durante validação nesta sessão (dogfood + cleanroom), fixados FAANG-grade SEM workarounds per CTO mandate:

| Commit | Severidade | Descrição |
|--------|-----------|-----------|
| `686e78e1` | S1 | Cleanroom FAANG refactors: argo-ci namespace single-writer + argo-events split (CRD-before-CR) + CronWorkflow v4 schedules |
| `d7e79882` | **S0** | EventBus PVC `storageClassName: ""` literal disables provisioning (CI pipeline 100% broken até fix) |
| `e32b6b8c` | **S0** | `task k3d:bootstrap-{workload,runtime-2}` faltava `cluster-issuer` selector (5/8 Ginkgo journey failures root-causa única) |

---

## Tests passing

| Suite | Count | Status |
|-------|-------|--------|
| `cosign_attest_test.go` (Phase 2 TDD) | 9 tests / 18 RUN+PASS markers | **✓ ALL PASS** |
| `harbor_mirror_observe_test.go` (Phase 3 TDD) | 8 tests / 16 RUN+PASS markers | **✓ ALL PASS** |
| Plan v1.1 hermetic render tests | (kubeconform + helm lint) | **✓ ALL PASS** (per cleanroom setup pre-flight) |

---

## Phase 5 (cross-validation) — pending per derived plan

Plan original Phase 5 mandate `/cross-validation`. **NÃO executado por plan original.** Abordado por plano derivado:
- [`docs/plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md`](./2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md) v1.1
- **M1 Phase 1** desse plano = invocar `/cross-validation 2026-05-27-tenant-build-pipeline-gaps-closure`
- Acceptance criteria v1.1 EC-8: zero CRITICO + zero GRAVE

## Phase 6 (operator dogfood) — pending per derived plan

Plan original Phase 6 mandate `task clean:all && task setup && tilt up && task doctor && LOOPZERO_E2E=1 task e2e:journey` com ≥73/76 PASS.

**Status real (esta sessão):**
- Cleanroom executado (commits paralelos + recovery LIVE de 3 bugs S0/S1)
- doctor PASS=41 FAIL=0 (paridade baseline 2026-05-25)
- Ginkgo journey: 105 PASS / 1 flake (re-run isolado 7/7 PASS) / 26 SKIP (vs baseline 106/0)
- chaos-test --full: 9 PASS + C01 isolado PASS = 10/10 (vs baseline 10/10)
- 1 deploy real end-to-end com Prova de Vida + Prova de Morte

**Phase 6 cumprida materialmente** mas NÃO documentada como "Phase 6 closure" no plan original — abordado por plano derivado M1 Phase 2 (LIVE proof gather).

---

## Coverage Matrix do plan original

| # | Gap M07 audit | Task | Status |
|---|---|------|--------|
| 1.a | Storage growth model + PV alerts | T1.1 | ✓ shipped (`f1ca9bdf`) |
| 1.b | 4 Prometheus alerts Sigstore | T1.2 | ✓ shipped (`_seconds` suffix v1.1 EC-NEW-1) |
| 1.b' | TUF expiry metric emitter | T1.2.b | ✓ shipped (CronJob + Pushgateway 48h TTL) |
| 1.c | Chaos drill cosign-fulcio-kill | T1.3 | ✓ shipped (prerequisite annotation v1.1 EC-NEW-3) |
| 1.d | OIDC issuer trust policy | T1.4 | ✓ shipped (cluster-external OIDC URL v1.1 EC-NEW-4) |
| 1.e | TUF ceremony runbook | T1.5 | ✓ shipped (7-section procedure) |
| 2 | SBOM-to-image attestation | T2.1+T2.2+T2.3 | ✓ shipped (DAG dep v1.1 EC-NEW-5 + activeDeadlineSeconds v1.1 EC-NEW-6) |
| 3 | Harbor mirror failures silent | T3.1 | ✓ shipped (Pushgateway-TTL v1.1 EC-NEW-7 + curl timeouts v1.1 EC-NEW-8) |
| 4 | OTel sampling undocumented | T4.1 | ✓ shipped (head-based 10% + FORCE_TRACE v1.1 EC-NEW-9) |

**Coverage engineering: 9/9 gaps shipped (100%).**

---

## Conclusão

Plan original v1.1 está **engineering-side COMPLETE**. Phase 5 (cross-validation) e Phase 6 (operator dogfood) endereçados pelo plan derivado [`2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md`](./2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md) v1.1 (M1 Phase 1 + M1 Phase 2 respectivamente).

**Próximo passo lógico:** Iniciar M1 Phase 1 do plan derivado (`/cross-validation`).
