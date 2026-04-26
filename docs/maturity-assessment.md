# LoopZero PaaS — Maturity Assessment

**Date:** 2026-04-25
**Assessor:** Engineering team (self-assessment)
**Score: 3.2 / 5.0**

---

## Summary

LoopZero has a strong technical foundation -- 4-plane architecture, cell-based scaling, strict state machines, advisory locks, consistent DIP -- all implemented with high code quality. The core deploy flow works end-to-end on real infrastructure (DigitalOcean). However, the product is commercially unvalidated: zero external users, missing table-stakes features (preview environments), and scale validation is loopzeroretical.

**In one line:** Excellent engineering, incomplete product.

---

## Dimension Scores

| Dimension | Score | Justification |
|-----------|:-----:|---------------|
| **Core Deploy Flow** | 4.5/5 | `loopzero deploy` -> live URL works E2E on DO. Rollback ~3s. Promote cross-env. GitHub App auto-deploy (~4min). Real, validated, competitive. |
| **DX / CLI** | 4.0/5 | 36 commands, `--json` on all, standardized exit codes, 1079 tests. Missing: preview environments (the killer feature that differentiates from `kubectl apply`) and onboarding < 10 min (Sprint 9). |
| **Infrastructure / Operations** | 3.5/5 | 3 DO clusters, CF Tunnel, 43 Prometheus metrics, SLO alerts, Loki. But: cross-cluster logs have gaps, KEDA not activated on real cluster, cell-2 not activated. Infra exists, day-2 ops not validated under real pressure. |
| **Security** | 3.0/5 | cosign signing, PSA restricted, NetworkPolicy, non-root, RSA-only JWT, secret rotation. But: syft SBOM missing, admission policy doesn't reject unsigned images, `platform.ts` (495 LOC installer) has zero tests. |
| **Scale / Multi-tenancy** | 3.0/5 | Cell architecture code 96% complete (impressive). But: never ran with 2+ real cells. Zero chaos tests. Zero load tests above L3. Architecture is solid on paper -- no empirical validation above ~100 apps. |
| **Product Completeness** | 2.5/5 | Missing: preview environments (Sprint 5 -- differentiating feature), `loopzero scale` persistence, SBOM, admission policy, chaos suite, polish/onboarding. A user paying $29/mo expects preview envs and 5-min onboarding. |
| **Tests / Confidence** | 3.5/5 | 1079 CLI + 41 Go packages + 186 Ginkgo specs. Good happy-path coverage. Gaps: `platform.ts` zero tests, concurrent reconcile test partial, 10 failing tests in `api/internal/k8s`. |

---

## What Justifies 3.2 (Not Higher)

1. **Zero external users have ever used the product.** No real dogfooding outside the team. The onboarding flow has never been tested by someone who doesn't know the system.

2. **Preview environments are table stakes.** Railway, Render, Vercel, Northflank -- all have them. Without this, LoopZero is a deploy tool with rollback, not a PaaS that justifies pricing.

3. **Scale validation is loopzeroretical.** Cell architecture is in code, but never ran with 2 cells, never had a real chaos test, never processed 1000 concurrent deploys. Confidence is based on design, not evidence.

4. **Supply chain is incomplete.** cosign without syft + without admission policy = half the story. It's like having the lock but not locking the door.

## What Justifies 3.2 (Not Lower)

1. **The deploy flow actually works** on real infrastructure (DO, not simulation)
2. **1079 + 41 + 186 tests** is serious coverage
3. **Cell architecture code ready** is a real competitive differentiator -- no Tier 2-3 competitor has this
4. **Custom domains Vercel model** is production-grade (CF Zone Management, WAF, auto-TLS)
5. **GitHub App auto-deploy** works end-to-end -- this is the experience that sells

---

## Path to 4.0

| Action | Impact | Sprint |
|--------|:------:|--------|
| Preview Environments (PR -> temp URL -> cleanup) | +0.3 | Sprint 5 |
| Polish / Onboarding < 10 min first deploy | +0.2 | Sprint 9 |
| Cell-2 activation + real chaos test | +0.2 | Phase 2b |
| 1 external early adopter doing real deploys | +0.1 | -- |

## Path to 5.0

Everything above, plus:
- syft SBOM generation
- Admission policy (reject unsigned images)
- Full chaos engineering suite
- 50+ real tenants
- Published SLA
- Validated incident runbooks
- 99.9% uptime measured over 30 days

---

## Test Suite (Current)

| Component | Count | Status |
|-----------|------:|--------|
| CLI (Jest) | 1,079 | Passing (59 suites) |
| API + Shared libs (Go) | 41 packages | Passing |
| Operators (Ginkgo + envtest) | 186 specs | 185 passing, 1 flaky (pre-existing) |
| Capacity tests | 10 files | Passing |
| CI pipeline | 14 workflows | Green |

---

## Feature Completion Matrix

| Feature | Status | Notes |
|---------|:------:|-------|
| Deploy happy path | Done | E2E validated on DO |
| Rollback | Done | ~3s, E2E validated |
| Promote | Done | cross-env, HTTP 202 |
| Secrets (ESO) | Done | sync <30s, zero value leak |
| Custom Domains (CF Zone) | Done | WAF/DDoS, auto-TLS, E2E with aquim.ai |
| GitHub App auto-deploy | Done | push -> live in ~4min |
| Monorepo (30 frameworks) | Done | static auto-infer, path traversal protection |
| Image signing (cosign) | Done | every build signed |
| Scale-to-zero (KEDA) | Done (code) | manifests + Helm + Pulumi. Cluster KEDA activation pending |
| Cell architecture (Phase 0+1) | Done (code) | 25/26 items. Phase 2b activation = config only |
| Plan enforcement | Done | Starter/Pro/Team caps |
| Auth hardened | Done | RSA-only, audience validation, rotation |
| Observability | Done | 43 metrics, dashboards, SLO alerts |
| JSON contracts | Done | 8/8 commands, 0 internal term leaks |
| Preview environments | Not done | Sprint 5 -- CRD defined, no production logic |
| SBOM (syft) | Not done | Sprint 6 remainder |
| Admission policy | Not done | Sprint 6 remainder |
| `loopzero scale` persistence | Not done | Stub (202 acknowledged) |
| Chaos engineering | Not done | Sprint 8 |
| Onboarding polish | Not done | Sprint 9 |

---

*This assessment will be updated quarterly or after major milestones.*
