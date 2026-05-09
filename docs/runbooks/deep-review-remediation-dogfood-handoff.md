# Deep Review Remediation — Dogfood Gate Handoff

**Status:** Engineering side closed (commit `34034cf6`). LIVE validation pending operator action.

**Plan:** `docs/plans/deep-review-remediation-plan.md`
**Engineering status:** `docs/plans/deep-review-remediation-final-status.md`

---

## Why this exists

The Deep Review Remediation plan's DoD requires `/dogfood full` PASS on a
clean cleanroom before plan can be marked complete. Engineering work for
all 70 findings is shipped (53 remediated + 13 wont_fix-with-ADR + 4
false-positive). What remains is operator-driven validation:

1. Apply migrations 000011, 000012, 000013 LIVE.
2. Verify all governance lints pass on a fresh checkout.
3. Run cleanroom + dogfood journey suite.
4. Verify Phase 0 launch-blocker fixes hold under journey load.

This document gives the operator a step-by-step playbook with exact
commands. Each step is small + reversible.

## Pre-flight check (5 min)

```bash
# 1. Branch state — must be on develop with all 25 plan commits.
git log --oneline 34034cf6 -1
git log --oneline develop --grep "deep-review-remediation" | wc -l
# Expected: 25 commits

# 2. Build status — every Go module must compile.
cd /home/paulo/loopzero
(cd api && go build ./...) && (cd operators && go build ./...) && \
  (cd pkg && go build ./...) && (cd cloud && go build ./...) && \
  echo "BUILD OK"

# 3. New unit tests pass.
(cd api && go test ./internal/correlation/ ./internal/middleware/ \
   ./internal/routes/ ./internal/services/ ./internal/db/ \
   ./internal/workqueue/ ./tests/unit/ -short -count=1) && \
  echo "UNIT OK"

# 4. Governance lints pass at baseline.
for s in check_actions_sha_pinned check_orphan_alerts check_alert_runbooks \
         migration_test_coverage check_agpl_isolation check_base_image_digest_pin; do
  bash infra/scripts/${s}.sh > /dev/null && echo "  ✓ ${s}" || echo "  ✗ ${s} FAILED"
done
```

If any step fails, STOP and investigate. Do not proceed to cleanroom.

## Phase 0 dogfood gate (~30-45 min)

```bash
# 1. Destroy infra + recreate from scratch (operator-driven; the dogfood
#    skill explicitly says auditor cannot destroy infra).
task teardown
task setup    # 4 k3d clusters + Helm + CRDs (idempotent)
tilt up       # background — wait for image build + deploy

# 2. Verify health.
task doctor   # gate: PASS=38 FAIL=0

# 3. Apply migrations 000011 + 000012 + 000013.
task db:migrate

# 4. Run Phase 0 dogfood — focused on the 4 CRITICAL findings.
JWT=$(cat ~/.loopzero/local-dogfood-token.txt)
LOOPZERO_E2E=1 \
  LOOPZERO_API_URL=http://localhost:13001 \
  LOOPZERO_TOKEN="$JWT" \
  LOOPZERO_E2E_DATABASE_URL='postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable' \
  go run github.com/onsi/ginkgo/v2/ginkgo -v \
    --label-filter="journey" \
    --timeout=60m \
    ./infra/tests/e2e/
```

### Phase 0 pass criteria (must verify)

- [ ] Zero S0/S1 findings.
- [ ] Rollback failure path: SQLSTATE 23514 does NOT fire (T0.1 fix).
- [ ] Tenant cross-account DELETE attempt: 401 (T0.2 fix).
- [ ] Bad-health canary deploy: rollout.aborted SSE within 8min, exit 3 (T0.3 fix).
- [ ] state machine vs CHECK invariant test: GREEN on releases + builds (T0.5).

### If FAIL

Capture the journey output, identify which fix regressed, fix it, re-run
cleanroom + dogfood. Repeat until 0 S0/S1.

## Cumulative dogfood gate (Phase 1-9)

Same flow as Phase 0, but full journey suite:

```bash
task teardown && task setup && tilt up
task doctor
JWT=$(cat ~/.loopzero/local-dogfood-token.txt)
LOOPZERO_E2E=1 ... go run github.com/onsi/ginkgo/v2/ginkgo -v \
  --label-filter="journey" \
  --timeout=60m \
  ./infra/tests/e2e/
```

Pass criteria identical to Phase 0 plus:
- [ ] All journey suites GREEN (not just Phase 0).
- [ ] Observability check: no PrometheusRule alert spurious firing.
- [ ] Rule 3 clean: zero internal terms in any CLI output.

## Cross-validation

Before declaring plan complete:

```bash
/cross-validation deep-review-remediation
```

Pass: zero CRITICO, zero GRAVE.

## Plan completion

Once both dogfood gates pass + cross-validation clean:

1. Update `CHANGELOG.md` under `[Unreleased]`:
   - `Added`: 5 ADRs (0045-0049), 7 governance lint scripts, BillingHandler routes, correlation context propagation.
   - `Fixed`: 4 CRITICAL launch-blockers (find_data_001, find_data_002, find_sec_001, find_flow_001) + 17 HIGH findings.
   - `Security`: X-Admin-Key bypass (CWE-306), constant-time admin key compare (CWE-208), Dockerfile FROM allow-list tighten.

2. Move plan to `docs/plans/closed/deep-review-remediation-plan.md`.

3. Tag commit (operator decision):
   ```bash
   git tag deep-review-remediation-COMPLETE-20260509
   git push --tags
   ```

4. Emit `<promise>TODAS AS TASKS, CRIETERIOS DE ACEITES E DODs CONCLUIDODOS VALIDADOS</promise>` per Ralph loop completion contract.

## Why operator-driven, not Ralph-driven

CLAUDE.md + skill conventions:
- Cleanroom skill destroys k3d clusters (DESTRUCTIVE). Per CLAUDE.md global
  rules, destructive operations require user confirmation.
- Dogfood skill explicitly says auditor "NEVER altera o sistema" — running
  dogfood from inside Ralph means Ralph cannot use Bash on `task teardown`
  or `kubectl delete`.
- Migration apply against running PG is blast-radius operation that needs
  human eyes on the pre-flight audit query.

The right boundary is: Ralph commits engineering work; operator runs
operational validation. Both signs of completion (engineering green +
dogfood green) gate the final promise.

## Estimated time

- Pre-flight: 5 min
- Phase 0 cleanroom + dogfood: 35-45 min
- Cumulative dogfood: 35-45 min
- Cross-validation: 10-15 min
- Plan archive + CHANGELOG: 10 min
- **Total: ~90-120 min** (active operator time; Ralph already complete on engineering)
