---
name: cosign-digest-enforcement
description: PARTIALLY IMPLEMENTED (warn-mode) — chart deployments emit image digest references via loopzero-common.image helper + VAP loopzero-require-image-digest enforces digest in WARN mode. T2.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: T2.3 engineering implementation (warn-mode phase)
---

<!-- PROPOSED STATE (warn-mode shipped; Deny-mode HARD STOP awaiting sponsor + 7-day soak) -->

# Cosign Digest Enforcement — Infrastructure Diff

> **Plan reference:** T2.3 + T1.5 of `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Status:** Phase 1 (warn-mode) ENGINEERING-READY. Phase 2 (Deny-mode) HARD STOP awaiting sponsor + 7-day soak.
>
> **Depends on:** Change Governor meeting confirms warn → enforce flip. EC-5 progressive rollout (warn before enforce) absorbed via 2 separate values: `requireImageDigest: true` (warn-mode default ON) + `requireImageDigestEnforce: false` (sponsor flips post-soak).

## AS-IS

```
build (BuildKit) ──► registry push ──► image: registry/app:v1.2.3
                                                  │
                                                  ▼
Helm chart template:
  image: {{ .Values.image }}:{{ .Values.tag }}   ◄── mutable tag
                                                  │
                                                  ▼
policy-controller verifies signature
   (no digest enforcement; mutable tag = re-tag attack possible)
```

**Risk:** Tag-reuse attacks. Adversary with registry push access can re-tag `v1.2.3` to malicious image; pod restart fetches new image (signed by attacker's key) silently.

## PROPOSED (post-T2.3)

```
build (BuildKit) ──► registry push ──► image: registry/app@sha256:<64hex>
                                                  │
                                                  ▼
buildkit.go outputs digest as Argo Workflow output
                                                  │
                                                  ▼
Helm chart template:
  image: {{ .Values.image }}@{{ .Values.digest }}   ◄── content-addressed
                                                  │
                                                  ▼
values.schema.json constraint (EC-4):
  oneOf: [{required: [tag]}, {required: [digest]}]
                                                  │
                                                  ▼
ClusterImagePolicy (EC-5 progressive rollout):
  Phase 1: mode: warn + namespaceSelector tenants-only
  Phase 2: mode: enforce + namespaceSelector ALL (after 7-day soak)
```

## What changes

| Component | AS-IS | PROPOSED |
|-----------|-------|----------|
| Chart template image field | `image:tag` (mutable) | `image@sha256:digest` (immutable) |
| values.schema.json | tag required | `oneOf` tag XOR digest (EC-4) |
| Build pipeline output | tag only | tag + digest |
| ClusterImagePolicy | Any signed image accepted | Phase 1 warn + scope tenants; Phase 2 enforce |
| Re-tag attack window | Until cosign keyless cutover (T0+90d) | Eliminated |

## Charts affected (7 deployment templates)

- `charts/loopzero-api/templates/deployment.yaml`
- `charts/loopzero-cloud/templates/deployment.yaml`
- `charts/loopzero-app-operator/templates/deployment.yaml`
- `charts/loopzero-platform-operator/templates/deployment.yaml`
- `charts/loopzero-preview-operator/templates/deployment.yaml`
- `charts/loopzero-tenant-operator/templates/deployment.yaml`
- `charts/loopzero-dashboard/templates/deployment.yaml`

Plus: `charts/loopzero-admission-policies/templates/cluster-image-policy-digest.yaml` (NEW — digest enforcement rule).

## Progressive rollout (EC-5)

**Phase 1 (this task — warn mode):**
```yaml
# ClusterImagePolicy.spec
mode: warn  # Logs violations, does NOT reject
namespaceSelector:
  matchLabels:
    loopzero.io/tenant: "true"  # Tenant namespaces only — admission-policies,
                            # monitoring, operator pods exempt
```

**Phase 2 (separate task ≥ 7-day soak):**
```yaml
mode: enforce  # Rejects non-matching admissions
namespaceSelector: {}  # ALL namespaces including loopzero-system
```

Without progressive rollout, enforce-mode on Day 1 rejects pre-existing admission-policies + monitoring + operator pods using `:latest`/`:vN.Y.Z` mutable tags → cluster-wide CrashLoop. Phase 1 catches these violations as warnings; operator fixes one-by-one; Phase 2 flips after audit shows zero outstanding warnings.

## Backward compatibility

Pre-release — break tag references after migration. Per plan ADR D1 (pre-release bias), backwards compatibility NOT required.

## Engineering shipped (warn-mode phase)

1. ✅ `loopzero-common.image` helper updated to prefer `image.digest` over `image.tag` when set (`charts/loopzero-common/templates/_helpers.tpl`)
2. ✅ ValidatingAdmissionPolicy `loopzero-require-image-digest` created in warn-mode (`infra/helm/admission-policies/templates/vap-require-image-digest.yaml` + `charts/loopzero-admission-policies/templates/vap-require-image-digest.yaml`)
3. ✅ Quality gate `pkg/quality/prodimagedigests` (warn-mode default, `--enforce` flag for post-soak flip)
4. ✅ Wired into `task quality:gates` orchestrator (`taskfiles/quality.yml`)
5. ✅ Prometheus alert `LoopZeroImageDigestWarnRatePresent` (info severity, surfaces warn-mode violations for 7-day soak audit)
6. ✅ values.yaml flags `requireImageDigest: true` + `requireImageDigestEnforce: false` in both chart systems

## Operator HARD STOP (Deny-mode phase)

1. Sponsor invokes `/meeting` for T2.3 Change Governor approval
2. Operator deploys updated admission-policies + loopzero-common charts via ArgoCD
3. 7-day Phase 1 warn-mode soak — operator audits:
   - `apiserver_admission_policy_check_failures_total{policy="loopzero-require-image-digest"}` (Prometheus)
   - `task quality:gates` output (`prod-image-digests` warnings count)
   - Grafana `policy-controller` dashboard
4. Operator updates build pipeline (`api/internal/argo/buildkit.go`) to write `imageDigest` value into Helm release values when deploying (currently builds emit digest but charts default to tag)
5. Render tests PASS (`task helm:render-test`)
6. Phase 2 flip after zero warnings: set `requireImageDigestEnforce: true` in helmfile overrides for prod
7. `prod-image-digests` gate flips from warn to `--enforce` mode (CI block)

## Related

- `docs/operations/cosign-signing-key-rotation-runbook.md` (T3.3 — pairs with this)
- `[[cosign-key-rotation]]` (concept page)
- `api/internal/argo/buildkit.go` (digest emission)
- `charts/loopzero-admission-policies/templates/cluster-image-policy.yaml` (existing warn policy — adds digest rule)
