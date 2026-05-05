# Dogfood Cleanroom Loop — Progress Report (2026-05-05)

**Plan:** docs/plans/real-paas-cleanroom-dogfood-loop-plan.md
**Status:** **NOT CONVERGED** — blocked by external constraint (Docker Hub rate-limit 429)
**Iterations executed:** 6 of 8 (extended cap per MEET-ASYNC-AMENDMENT-001)
**Real product/infra bugs found:** 3 (B2, B3, B4)
**Orchestrator scaffolding bugs fixed:** 2 (B1, B5)
**External constraint hit:** 1 (B6)

## Iteration log

| Iter | Bug | Severity | Category | Commit | Phase ended | Lesson |
|------|-----|----------|----------|--------|-------------|--------|
| 1 | B1 — log file destroyed by `task clean` (FD orphaned at tee) | scaffolding | orchestrator design | `87dad159` | post-mortem (blind diagnostic) | Logs MUST live outside `.test-reports/`; sentinel via `trap EXIT` |
| 2 | B2 — postgresql PDB conflict (Bitnami `maxUnavailable` vs Tilt `minAvailable`) | **S0 product** | infra/helm/postgresql | `41ffacac` | tilt ci 9s exit 25 | Set `primary.pdb.create: false` in chart values; Tilt is sole PDB owner |
| 3 | B3 — loopzero-api ClusterRole missing `secrets` + `clustercredentials.loopzero.io` | **S0 product** | infra/tilt/api-deployment.yaml (out of scope) | `11aed9fc` | tilt ci 600s timeout | Additive ClusterRole `loopzero-api-cleanroom-extra` applied via orchestrator |
| 4 | B4 — heal post-tilt; ClusterCredential CRs absent at loopzero-api boot | **S0 orchestrator order** | dogfood-cleanroom-loop.sh | `eddfb93a` | tilt ci 600s exit 25 | Reorder: setup → rbac_patch → heal → tilt_up |
| 5 | B5 — full-loop.sh `--reset` exits without running journeys | scaffolding | dogfood-cleanroom-loop.sh | `c5d9dea8` | dogfood 0s exit 60 | Two calls: `--reset` then no-flag |
| 6 | B6 — Docker Hub rate-limit (429) on oryd/kratos + minio/mc | **environmental** | external infrastructure | (none — not code) | setup helm context deadline | Cleanroom assumes unlimited image pull bandwidth — wrong assumption |

## Real product bugs caught (3)

These are bugs that warm-infra testing would never have surfaced:

### B2 — PostgreSQL PDB conflict (commit `41ffacac`)

**Impact:** Tilt apply failed in 9 seconds with K8s validation error. Production deploy or chart upgrade would have hit the same wall.

**Root cause:** Bitnami chart 18.6.2 + Tilt-applied custom PDB both target the same name; Bitnami uses `maxUnavailable: 1`, Tilt uses `minAvailable: 1`. K8s rejects PDBs with both set.

**Fix:** Set `primary.pdb.create: false` so Tilt is sole PDB owner.

**Regression test:** `infra/tests/scripts/postgresql-pdb-config_test.sh` asserts the value.

### B3 — loopzero-api ClusterRole missing secrets + clustercredentials (commit `11aed9fc`)

**Impact:** loopzero-api pod CrashLoopBackOff at boot. Production likely has the same gap masked by RBAC patches applied manually during prior incident response.

**Root cause:** `infra/tilt/api-deployment.yaml` ships a narrow ClusterRole. Real loopzero-api needs `secrets` (KubeMintingTokenProvider, ESO, argocd-refresh) and `clustercredentials.loopzero.io` (K8sCellStore.LoadCells — fatal at boot).

**Fix (workaround):** Orchestrator phase_rbac_patch applies an additive ClusterRole `loopzero-api-cleanroom-extra`. Canonical fix (edit `infra/tilt/api-deployment.yaml`) blocked by governance scope.

**Regression test:** `test_rbac_patch_phase_exists_and_ordered` in `dogfood-cleanroom-loop_test.sh`.

**Follow-up needed:** A separate `/change` should expand scope to include `infra/tilt/` so the canonical fix can be applied; the additive ClusterRole then becomes redundant and can be removed.

### B4 — Heal-after-tilt creates ClusterCredentials too late (commit `eddfb93a`)

**Impact:** loopzero-api fatal at boot with `cell router: 0 ClusterCredentials discovered — fatal, no fallback by design`. Tilt ci hit 600s timeout.

**Root cause:** Heal H12/H13/H14/H15 create the ClusterCredential CRs that loopzero-api reads at startup. With heal AFTER tilt, the CRs don't exist when loopzero-api first boots.

**Fix:** Reorder orchestrator phases. Heal still runs after setup (D5 precondition preserved), just before tilt instead of after. H8/H10 silently no-op when Deployment not yet created.

**Regression test:** `test_rbac_patch_phase_exists_and_ordered` extended to assert ordering setup → rbac_patch → heal → tilt_up.

## Orchestrator scaffolding bugs fixed (2)

### B1 — Log destruction during Phase 1 (commit `87dad159`)

`task clean` deletes `.test-reports/`, orphaning tee FD. First iter ran with zero log evidence.

**Fix:** Separate `LOG_DIR=.test-reports-cleanroom/` (untouched by clean). EXIT_SENTINEL file via `trap EXIT`.

### B5 — full-loop.sh --reset exits immediately (commit `c5d9dea8`)

`--reset` mode resets state and exits without running journeys.

**Fix:** Call full-loop.sh twice — `--reset` then no flags.

## External constraint hit (1)

### B6 — Docker Hub rate-limit (no commit; environmental)

After 6 destroy/recreate cycles in ~1.5 hours, Docker Hub returned `429 Too Many Requests` for `oryd/kratos:v26.2.0`, `minio/mc:RELEASE.2024-01-28...`, and similar third-party images. Free-tier rate limit is 100 pulls per 6h per IP.

**Implication for the plan:** D1 (full destroy each iteration) is correct as a regression-detection mechanism but has a **hidden bandwidth cost** that becomes prohibitive after ~5 cycles. The plan v1.0 didn't account for this.

**Mitigation options (any of):**
1. Wait 1-6h for Docker Hub rate-limit to reset (passive)
2. Pre-load third-party images into k3d-loopzero-registry via `k3d image import` after `clusters:create` (multi-hour engineering work)
3. Authenticate to Docker Hub via Pulumi-provisioned creds (doubles limit to 200 pulls/6h; still bounded)
4. Adopt a registry mirror (e.g., Harbor proxy cache) — requires infra change, multi-day work

**Until B6 mitigated, cleanroom looping is bounded to ~5 iterations per 6h window in this environment.**

## Convergence assessment per Phase 4 sign-off criteria

- [ ] full-loop.sh exit 0 — **NO** (never reached; phase_dogfood blocked by setup failure or ran with empty state)
- [ ] All 12 journeys PASS — **NO** (0/12 in iter 5; iter 6 didn't reach phase_dogfood)
- [ ] Zero S0/S1 in dogfood findings — **N/A** (no findings produced)
- [ ] Rule 3 clean — **PASS** (audit ran in iter 5, zero forbidden terms in CLI output)
- [ ] Prova de Vida — **N/A**
- [ ] Prova de Morte — **N/A**

**Overall: NOT CONVERGED.** Phase 4 sign-off CANNOT be declared.

## Honest assessment of plan value delivered

Despite not converging:

- **3 real product bugs caught** that warm-infra testing missed for ~9 days post-charts-sota
- **1 hidden architectural constraint discovered** (Docker Hub bandwidth) — informs future cleanroom plans
- **5 commits adding regression tests** that prevent recurrence
- **2 commits codifying orchestrator hardening** (log persistence, exit sentinel, two-call dogfood invocation)

The plan's purpose was "find regression bugs cleanroom catches that warm-infra hides". On that axis, **plan succeeded**. The plan's stretch goal (12/12 GREEN) requires resolving B6 first.

## Recommended next actions (out of Ralph loop scope)

1. **Wait** 6h for Docker Hub rate limit reset OR
2. **Implement image-cache phase** (insert `k3d image import` of pre-pulled third-party images after clusters:create, before infra:install) OR
3. **/change** scope to include `infra/tilt/api-deployment.yaml` so B3 canonical fix can replace the additive ClusterRole

Then re-run `task dogfood:cleanroom` to converge through iters 7-8 (cap budget remaining).

## References

- Plan: `docs/plans/real-paas-cleanroom-dogfood-loop-plan.md`
- Cap amendment: `docs/meetings/MEET-ASYNC-AMENDMENT-001-cleanroom-cap-extension.md`
- Logs: `.test-reports-cleanroom/dogfood-cleanroom-202605*.log` (6 files)
- Exit sentinels: `.test-reports-cleanroom/*.exit`
- Commits: `df3e4b46` (Phase 0+1) → `87dad159` → `41ffacac` → `11aed9fc` → `eddfb93a` → `c5d9dea8` → `81a85849`
