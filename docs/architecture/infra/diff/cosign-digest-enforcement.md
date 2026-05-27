---
name: cosign-digest-enforcement
description: PROPOSED — chart deployments emit image digest references (sha256) instead of mutable tags + ClusterImagePolicy enforces digest. T2.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T1.5 + T2.3 design
---

<!-- PROPOSED STATE — depends on operator T2.3 HARD STOP (Change Governor meeting) -->

# Cosign Digest Enforcement — Infrastructure Diff

> **Plan reference:** T2.3 + T1.5 of `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Depends on:** Change Governor meeting (T2 — Helm modifications + admission policy). EC-4 + EC-5 of edge-case-review absorbed inline.

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

## Operator HARD STOP

1. Sponsor approves T2.3 via Change Governor meeting (T2)
2. Operator updates 7 chart deployment templates
3. Operator updates values.schema.json with `oneOf` constraint (EC-4)
4. Operator updates `infra/helm/policy-controller/values.yaml` (mode: warn + namespaceSelector)
5. Operator updates `api/internal/argo/buildkit.go` to emit digest output
6. Render tests PASS (`task helm:test`)
7. 7-day Phase 1 warn-mode soak — operator audits `loopzero_policy_violation_total{mode="warn"}`
8. Phase 2 flip after zero warnings

## Related

- `docs/operations/cosign-signing-key-rotation-runbook.md` (T3.3 — pairs with this)
- `[[cosign-key-rotation]]` (concept page)
- `api/internal/argo/buildkit.go` (digest emission)
- `charts/loopzero-admission-policies/templates/cluster-image-policy.yaml` (existing warn policy — adds digest rule)
