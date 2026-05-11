# Cross-Validation Report — deep-review-2026-05-10-remediation

**Date:** 2026-05-11
**Tasks scanned:** 22

## Summary

| Severity | Count |
|----------|------:|
| CRITICO | 2 |
| GRAVE | 0 |
| MEDIO | 2 |
| OK | 18 |

## CRITICO — 2 tasks

### T3.2 — Add Index on build_apps.build_id ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `api/internal/db/migrations/000016_build_apps_index.up.sql`

### T3.4 — Remove ArgoCD Dual-Owner for Admission Policies ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `infra/argocd-bootstrap/apps/runtime-admission-policies.yaml`

## MEDIO — 2 tasks

### T1.1 — SecretHandler: Add Environment Lock ✅ DONE 2026-05-10

- **Files total:** 3
- **Existing:** 2
- **Missing:** 1

  Missing files:
  - `api/internal/server/setup_orchestrator.go`

### T2.2 — Bundle SHA256 Digest (per ADR D4) ✅ DONE 2026-05-10

- **Files total:** 4
- **Existing:** 3
- **Missing:** 1

  Missing files:
  - `api/internal/db/migrations/000015_bundle_digest.up.sql`

## OK — 18 tasks

### T0.1 — Preview Controller: Use Cell-Aware Client ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T0.2 — pgDedup.Release: Use Background Context in Defer ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T0.3 — Port Conflict: Align LOOPZERO_API_PORT with service.port ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T0.4 — BuildStore.GetByID: Add Tenant Filter ✅ DONE 2026-05-10

- **Files total:** 5
- **Existing:** 5
- **Missing:** 0

### T0.5 — Policy-Controller: Flip to Deny Mode ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T1.2 — PreviewService: Add Environment Lock ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T1.3 — Supersession Error: Return to Roll Back Transaction ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T1.4 — Preview SM: Use Actual Phase + TryTransition ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T1.5 — Reaper MarkAsCleaned: Re-fetch Before Finalizer ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T1.6 — Placement Serializer: Add lock_timeout ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T1.7 — Callback Activator: Return Error for HTTP 500 ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T2.1 — Admin Secrets: Consolidate (per ADR D3) ✅ DONE 2026-05-10

- **Files total:** 5
- **Existing:** 5
- **Missing:** 0

### T2.3 — Rate Limiter: Per-User Key (per ADR D5) ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T3.1 — Remove 17 Dead Prometheus Alerts ✅ DONE 2026-05-10

- **Files total:** 4
- **Existing:** 4
- **Missing:** 0

### T3.3 — Bootstrap Charts: Add Startup Probes + Seccomp ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T4.1 — CLI: Add deploy.delivery_failed Handler ✅ DONE 2026-05-10

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T4.2 — Error Shape: Migrate 4 Route Files to LoopZeroError ✅ DONE 2026-05-10

- **Files total:** 4
- **Existing:** 4
- **Missing:** 0

### T4.3 — Metric: Move SetToCurrentTime After Query ✅ DONE 2026-05-10

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

