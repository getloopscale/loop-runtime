# Stale Claims Report — 2026-05-11

**Total claims:** 348
**Stale claims:** 7 (2.0% of catalog)

## By Severity

| Severity | Count |
|----------|------:|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 7 |

## By Dimension

| Dimension | Stale Count |
|-----------|------------:|
| product-completeness | 5 |
| infra-ops | 1 |
| core-deploy | 1 |

## MEDIUM (<=30d overdue) — 7 claims

### MEET-2026-03-29-019-1-alerts-deferred-to-pre-harbor-phase-loopzeroregistryunreachabl

- **Source:** `docs/meetings/records/MEET-2026-03-29-019.md:120`
- **Dimension:** product-completeness
- **Last validated:** 2026-03-29
- **Expired:** 2026-04-28 (13d overdue)
- **Text:** 1. **Alerts deferred to PRE-HARBOR phase** — `LoopZeroRegistryUnreachable` and `LoopZeroRegistryCredentialStale` alerts are trac

### MEET-2026-03-30-022-resolution-consensus-with-nuance-phase-42-scope-a-install-ky

- **Source:** `docs/meetings/records/MEET-2026-03-30-022.md:247`
- **Dimension:** infra-ops
- **Last validated:** 2026-03-30
- **Expired:** 2026-04-29 (12d overdue)
- **Text:** **Resolution:** Consensus with nuance. Phase 4.2 scope: (a) Install Kyverno on workload cluster (required for verifyImag

### MEET-2026-04-04-030-decision-required-what-is-the-minimum-viable-set-of-changes-

- **Source:** `docs/meetings/records/MEET-2026-04-04-030.md:16`
- **Dimension:** product-completeness
- **Last validated:** 2026-04-04
- **Expired:** 2026-05-04 (7d overdue)
- **Text:** **Decision Required:** What is the minimum viable set of changes, in what order, with what Definition of Done, before we

### MEET-2026-04-04-030---sre-conditional-veto-on-declaring-production-ready-without

- **Source:** `docs/meetings/records/MEET-2026-04-04-030.md:82`
- **Dimension:** product-completeness
- **Last validated:** 2026-04-04
- **Expired:** 2026-05-04 (7d overdue)
- **Text:** - **SRE CONDITIONAL VETO on declaring production-ready without Sprint 7 tasks 7.1 + 7.2.** RunAsRoot and no PSA enforcem

### MEET-2026-04-04-030-total-to-production-ready-10-14h

- **Source:** `docs/meetings/records/MEET-2026-04-04-030.md:437`
- **Dimension:** product-completeness
- **Last validated:** 2026-04-04
- **Expired:** 2026-05-04 (7d overdue)
- **Text:** | **Total to production-ready** | **10-14h** | — |

### MEET-2026-04-06-038-recommendation-6-phases-db-first-free-tier-db-only-no-stripe

- **Source:** `docs/meetings/records/MEET-2026-04-06-038.md:61`
- **Dimension:** product-completeness
- **Last validated:** 2026-04-08
- **Expired:** 2026-05-08 (3d overdue)
- **Text:** **Recommendation:** 6 phases, DB-first. Free tier = DB-only, no Stripe subscription. Stripe customer creation deferred t

### MEET-2026-04-09-044---github-app-auto-deploy-e2e-validated-happy-path-only

- **Source:** `docs/meetings/records/MEET-2026-04-09-044.md:21`
- **Dimension:** core-deploy
- **Last validated:** 2026-04-09
- **Expired:** 2026-05-09 (2d overdue)
- **Text:** - GitHub App auto-deploy E2E validated (happy path only)

## Phase 2 Priority Queue

Cross-validation order:
1. CRITICAL severity (oldest first) — high regression risk
2. BROKEN refs from evidence-gaps-report.md
3. HIGH severity by dimension (security/scale first)
4. MEDIUM as bandwidth allows
