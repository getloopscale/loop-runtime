---
name: tenant-build-pipeline-as-is
description: AS-IS state of tenant build pipeline 2026-05-27 — covers BuildKit + cosign keyed signing + syft SBOM + push DOCR. Baseline for gaps closure plan.
type: architecture
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: tenant-build-pipeline-gaps-closure plan pre-implementation snapshot
---

# Tenant Build Pipeline AS-IS — 2026-05-27

> **Pre-plan baseline** for `docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`. Captures the build pipeline state across all submodules before the 4 gaps closure.

## System Context

```
┌──────────────────────────────────────────────────────────────────────┐
│  CLIENT — `loopzero deploy`                                              │
└─────────────────────────────────┬────────────────────────────────────┘
                                  │ multipart upload bundle.tar.zst
                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│  MGMT CLUSTER — loopzero-api                                             │
│  api/internal/services/build_orchestrator.go                         │
│  api/internal/argo/workflow_submitter.go                             │
└─────────────────────────────────┬────────────────────────────────────┘
                                  │ Workflow CR submission
                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│  BUILD CLUSTER — Argo Workflows (namespace: argo)                    │
│                                                                      │
│  DAG: clone → cache-restore → buildkit → cosign-sign → syft-scan    │
│       → push-docr → [optional Harbor mirror]                         │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ Phase: BUILD                                                 │   │
│  │   - loopzero-clone (CWT)        — pulls bundle from MinIO        │   │
│  │   - cache-restore (CWT)     — restores BuildKit cache from S3│   │
│  │   - buildkit-runner         — rootless single-container build│   │
│  │   - cache-save (CWT)        — saves BuildKit cache to S3     │   │
│  └──────────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ Phase: SIGN + SBOM (parallel)                                │   │
│  │   - cosign-sign-{app}       — keyed signing (kms-key)        │   │
│  │   - syft-scan-{app}         — CycloneDX SBOM → S3 upload     │   │
│  └──────────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ Phase: PUSH                                                  │   │
│  │   - push-image-{app}        — DOCR push (registry.do.com)    │   │
│  │   - mirror-harbor-{app}     — Harbor (best-effort, fail-open)│   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────┬────────────────────────────────────┘
                                  │ build callback → API enriches AppRelease
                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│  MGMT — release activation                                           │
│  - enrichAppReleasesWithAttestation (cosign sig + syft S3 ref)       │
│  - manifest_builder.go: render image@sha256:<digest> (fails-loud)    │
│  - gitops push → ArgoCD sync → workload cluster                      │
└──────────────────────────────────────────────────────────────────────┘
```

## Components present today

| Component | Path | Maturity 2026-05-26 |
|-----------|------|----------------------|
| `workflow_builder.go` (299 LOC) | `api/internal/argo/` | ✅ |
| `workflow_submitter.go` (~250 LOC) | `api/internal/argo/` | ✅ |
| `cosign.go` (327 LOC) — keyed + keyless templates | `api/internal/argo/` | 🟡 keyless code shipped, infra not active |
| `syft.go` (178 LOC) — CycloneDX SBOM + S3 upload | `api/internal/argo/` | ✅ functional, ⚠️ NOT linked to image via attestation |
| `buildkit.go` (471 LOC) | `api/internal/argo/` | ✅ |
| `dockerfile_lint.go` (85 LOC) — 8 hard rules | `api/internal/argo/` | ✅ |
| `languages.go` (489 LOC) — 9 strategies | `api/internal/argo/` | ✅ |
| ClusterWorkflowTemplates: loopzero-clone, loopzero-cache-save, loopzero-cache-restore | `charts/loopzero-build-cwts/templates/` | ✅ |
| loopzero-packs v0.4.0 (lenient Dockerfile) | constants.go:39 pinned | ✅ |
| Harbor dual-push (best-effort, fail-open) | `workflow_builder.go:408,432` `continueOn.failed=true` | ⚠️ silent on failure |
| Trivy auto-delete vulnerable images post-push | `task quality:gates` re-wired 2026-05-27 | ✅ |
| TruffleHog secret scan | `task quality:gates` re-wired 2026-05-27 | ✅ |
| cosign keyed signing (current) | `cosign.go:cosignSignTemplate()` | ✅ functional, ⏳ deadline 2026-08-10 |
| `check-cosign-cutover-deadline.sh` enforcement | `infra/scripts/security/` + T-30d/T-15d/T-7d warnings | ✅ |
| ClusterImagePolicy loopzero-images-warn | `charts/loopzero-admission-policies/templates/cluster-image-policy.yaml` | ✅ |
| VAP loopzero-require-image-digest (warn-mode) | `infra/helm/admission-policies/templates/` | ✅ shipped 2026-05-27 |
| Manifest renderer fails-loud on missing digest | `manifest_builder.go:70-77` | ✅ since 2026-04-29 |
| Argo Rollouts canary deploys | `renderer_rollout.go` | ✅ since 2026-05-07 |

## Gaps identified (M07 audit 2026-05-26 + status update 2026-05-27)

### GAP-1: Cosign keyless infrastructure activation (HIGH, deadline 2026-08-10)
- **Status:** Pulumi `Sigstore` ComponentResource shipped 2026-05-27 commit `321e6471`
- **Remaining sub-deliverables (per ADR-2026-05-27-T2.2 § Engineering deliverables):**
  - GAP-1.a — Storage growth model + PV alerts (~2h eng)
  - GAP-1.b — 4 Prometheus alerts (sign_failure, cert_issuance, log_append, TUF expiry)
  - GAP-1.c — Chaos drill manifest cosign-fulcio-kill
  - GAP-1.d — OIDC issuer trust policy in ClusterImagePolicy keyless variant
  - GAP-1.e — Offline TUF ceremony runbook (`docs/operations/tuf-root-ceremony.md`)

### GAP-2: SBOM-to-image attestation linking (MEDIUM, SLSA L3)
- **Status:** SBOM emitted independently; signature emitted independently; NO `cosign attest --predicate` linking them cryptographically
- **Engineering required:** ~4h
- **Files affected:** `api/internal/argo/syft.go`, `api/internal/argo/cosign.go`, new `cosignAttestTemplate()`

### GAP-3: Harbor mirror failures silent (LOW)
- **Status:** `continueOn.failed=true` on `mirror-harbor-{app}` step (`workflow_builder.go:408,432`) — failure not logged to Loki
- **Engineering required:** ~2h
- **Files affected:** `api/internal/argo/workflow_builder.go` — add exit handler that emits Loki warning when Harbor mirror fails

### GAP-4: OTel sampling policy não documentado (LOW)
- **Status:** Argo workflow templates sem explicit OTel sampling config
- **Engineering required:** Docs only
- **Files affected:** New `docs/operations/build-pipeline-otel.md`

## Invariants preserved post-plan

These MUST continue to hold:
1. **Tenant build still works during transition** — keyed mode remains valid until cutover OR deadline
2. **Manifest renderer fails-loud on missing digest** (`manifest_builder.go:70-77`)
3. **cosign signatures verified at admission via policy-controller** (warn-mode now, deny-mode post-T2.3)
4. **Argo Workflows controller serves both tenant builds AND CI workflows** (post-T0.2)
5. **DOCR is canonical registry** (Harbor mirror best-effort)
6. **Build cluster isolation** (mgmt never runs Workflow CRs)

## Files defining this AS-IS state

- `api/internal/argo/*.go` — 22 files, ~4,200 LOC source + ~2,200 LOC test
- `charts/loopzero-build-cwts/templates/` — 3 CWTs
- `infra/helm/argo-workflows/values.yaml` — Argo Workflows release config
- `infra/scripts/security/check-cosign-cutover-deadline.sh` — deadline enforcer (T-30d/T-15d/T-7d warnings 2026-05-27)
- `infra/pulumi-platform/src/services/sigstore.ts` — Sigstore ComponentResource (offline TUF required)
- `cmd/loopzero-ops/internal/quality/prodimagedigests/check.go` — digest enforcement gate

## Related

- M07 audit: `.claude/knowledge-base/audits/2026-05-26/07-build-pipeline.md` (§7 Status Update 2026-05-27)
- Plan superseding: `docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`
- PROPOSED diff (will be created post-implementation): `docs/architecture/build-pipeline/diff/post-gaps-closure.md`
- Existing PROPOSED: `docs/architecture/build-pipeline/diff/keyless-active.md` (Cosign keyless cutover)
- ADRs: `2026-05-27-T2.2-offline-tuf-ceremony.md`
