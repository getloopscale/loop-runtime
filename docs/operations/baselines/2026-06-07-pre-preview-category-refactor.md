---
type: baseline
title: "Baseline: pre preview-as-category refactor (2026-06-07)"
---

# Baseline: pre preview-as-category refactor (2026-06-07)

**Context:** Capture state immediately before Phase 0 T0.2 cleanup + Phase 1 migration begin per plan `.claude/knowledge-base/plans/2026-06-07-preview-as-category-single-slot-plan.md`.

**Purpose:** Bisect anchor per `.claude/rules/steady-state-baseline.md`. If post-refactor regression surfaces, this baseline is the comparison point.

---

## Layer 1 — Code state

| Item | Value |
|---|---|
| Branch | `worktree-glowing-growing-babbage` |
| HEAD | `5d5943b5 plan(j-23/j-10): preview-as-category single-slot refactor` |
| Recent commits | `400135de docs(ops): review checklist`, `347fdbc0 fix(j17)`, `b21e9446 fix(j23) #71`, `e682e4d4 fix(j23) #5` |
| Working tree | clean (no uncommitted changes) |

## Layer 2 — Runtime state

### Workload namespaces (k3d-loopzero-workload)

```
loopzero-tn-08fa2ecedc3c7e77-production           Active   4h44m  (legacy pre-#69)
loopzero-tn-08fa2ecedc3c7e77-teste-preview-pr-1   Active   7h50m  (canonical, PR #1)
loopzero-tn-08fa2ecedc3c7e77-teste-production     Active   4h11m  (canonical post-#69)
loopzero-tn-e2etest-production                    Active   4h44m  (legacy artifact)
```

### ArgoCD Apps

```
tn-08fa2ecedc3c7e77-preview-pr-1   Synced   Healthy
tn-08fa2ecedc3c7e77-production     Synced   Healthy
```

### PreviewEnvironment CRs

```
prev-6e864001abda5c5f   Active   feature/test-preview   7h50m
```

(Note: `prev-5e2be45bb755a2ad` was deleted from K8s in earlier regression test, DB row still exists — Bug E manifestation.)

### Operator images deployed

| Operator | Image |
|---|---|
| tenant-operator | `k3d-loopzero-registry:5000/loopzero-tenant-operator:fix-71` |
| preview-operator | `k3d-loopzero-registry:5000/loopzero-preview-operator:tilt-dd652e9fefb6963c` |
| loopzero-api | `k3d-loopzero-registry:5000/loopzero-api:fix-bug5` |
| loopzero-cloud | (Tilt-managed, STRIPE_API_KEY injected LIVE via #76 fix) |

## Layer 3 — Config state

### schema_migrations (PG)

```
version=26, dirty=false
```

(Note: dual row `version=10, dirty=false` is golang-migrate's tracking quirk, not a real second migration.)

### Tenant CR spec.environments (tn-08fa2ecedc3c7e77)

```json
[{"cluster":"workload","name":"production","project":"teste"}]
```

### environment_targets table

```
tn-08fa2ecedc3c7e77 | teste | preview-pr-1
tn-08fa2ecedc3c7e77 | teste | preview-pr-2  ← orphan (CR deleted, row remains — Bug D)
tn-08fa2ecedc3c7e77 | teste | production
```

### preview_environments table

```
prev-6e864001abda5c5f | tn-08fa2ecedc3c7e77 | pr=1 | feature/test-preview          | status=creating  ← stale (CR is Active — Bug E)
prev-5e2be45bb755a2ad | tn-08fa2ecedc3c7e77 | pr=2 | regression-preview-2026-06-07 | status=active    ← CR deleted (Bug E orthogonal)
```

### subscription state

```
tn-08fa2ecedc3c7e77 | plan=pro | status=active  (LIVE-fixed during session, sponsor canonical user)
```

### plan_limits.environments cap

```
starter   | 1
pro       | 3
team      | 10
enterprise| NULL (unlimited)
```

## Layer 4 — LIVE functional probes

| Probe | Result |
|---|---|
| production HTTPS via canonical ns | `HTTP 200` ✅ |
| preview-pr-1 HTTPS via wildcard LE cert | `HTTP 200` ✅ |
| /v1/billing direct (no auth) | `HTTP 401` ✅ (handler registered post-#76 fix) |
| `kubectl auth can-i list previewenvironments --as=...:tenant-operator` | `yes` ✅ (#65) |
| `tenant_members.role` for admin@loopzero.example | `owner` ✅ (#75) |
| `users.active_tenant_id` for admin@loopzero.example | `tn-08fa2ecedc3c7e77` ✅ (#75) |
| `dogfood-preview.sh --skip-envtest` | 3/3 suites + 9/9 LIVE scenarios ✅ |

## Migration number drift

Plan documented migration `000022` but next available is **`000027`** (develop merged Memory section P0-P4 commits adding 000022-000026 between plan authorship and Phase 1 begin).

**Adjustment in implementation:** All ADRs referencing `000022` will be implemented as `000027`. Plan body update happens in commit that ships migration.

## What this baseline expects to change post-refactor

| Surface | Before | After (post-Phase 8) |
|---|---|---|
| `Tenant.Spec.Environments[]` | 1 entry (production) | 2 entries (production + preview category) |
| `environment_targets` schema | no `kind` column | `kind` column NOT NULL CHECK |
| `environment_targets` rows for tn-08fa | 3 rows (production + pr-1 + pr-2) | 1 row (production, kind=permanent) |
| `preview_environments` rows | 2 rows (stale) | 0 or 1 row (clean break removes PR rows) |
| `preview_slot` table | doesn't exist | new table, 1 row per tenant (active or empty) |
| Dashboard `/environments` | shows 1 env (production) | shows 2 entries (production + preview category) |
| Enforcement `current=` | 3 (broken) | 1 (correct, permanent only) |
| Preview URL formula | `<tenant>-<project>-<app>-preview-pr-N.<base>` | `<tenant>-<project>-preview.<base>` (no -pr-N) |

## Restore reference

If implementation needs to rollback to this exact state:

```bash
# Code
git checkout 5d5943b5

# K8s state — would need manual recreation; data is unique to LIVE LIVE
# DB — golang-migrate down 000022 (when implemented) reverses schema changes
# Reseed sponsor account if needed: see #75 task
```

---

**Tracked by plan:** `.claude/knowledge-base/plans/2026-06-07-preview-as-category-single-slot-plan.md` T0.1
