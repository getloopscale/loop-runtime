# Layered Bootstrap — End-to-End Validation Runbook

> CHG-2026-04-28-LAYERED-BOOTSTRAP — T7.1
>
> Validates that the new `loopzero platform install [--provider digitalocean]` flow
> works end-to-end without manual intervention, including L3-50 and L3-100 deploy
> validation against DigitalOcean.

## Pre-conditions

- DO API token with droplet quota >= 12
- CF API token with zone access
- `helmfile`, `pulumi`, `helm`, `kubectl`, `cosign`, `doctl` installed
- ~$200/month DO budget (4 DOKS + PG + Spaces + Valkey for ~6h test window)

## Procedure (2 progressive gates)

### Gate 0 — Destroy current state (<5 min)

```bash
cd /home/paulo/loopzero

# 1. Backup state (safety)
cd infra/pulumi
pulumi stack export --stack dev > ~/.loopzero/backups/state-$(date +%Y%m%d-%H%M).json

# 2. Run destroy — measure timing
START=$(date +%s)
pulumi destroy --stack dev --yes
END=$(date +%s)
DURATION=$(( END - START ))
echo "Destroy duration: ${DURATION}s"

# Acceptance: DURATION < 300s (5 min)
# Pre-CHG-2026-04-28 baseline: ~3600s (60 min) due to zombie K8s state
```

### Gate 1 — L3-50 (50 tenants)

```bash
# 1. Provision via loopzero platform install
loopzero platform install -f platform-dev.yaml --provider digitalocean

# 2. Verify health
loopzero platform status   # all components Running
bash infra/helmfile/scripts/health-check.sh mgmt
bash infra/helmfile/scripts/health-check.sh build
bash infra/helmfile/scripts/health-check.sh runtime
bash infra/helmfile/scripts/health-check.sh wire

# 3. Run regression suite (T8.1)
bash infra/helmfile/scripts/regression-checks.sh

# 4. Run L3-50
LOOPZERO_REAL_DEPLOY=1 \
  LOOPZERO_LOAD_LEVEL=l3-realistic \
  LOOPZERO_TENANT_COUNT=50 \
  go test ./infra/tests/load/ -run L3Realistic -v -timeout 2h
```

**Acceptance:** ≥ 47/50 tenants (94%) responding HTTP 200.

### Gate 2 — L3-100 (100 tenants)

```bash
# 1. Bump runtime pool if needed (already 8 in Pulumi.dev.yaml)
# 2. Run L3-100
LOOPZERO_REAL_DEPLOY=1 \
  LOOPZERO_LOAD_LEVEL=l3-realistic \
  LOOPZERO_TENANT_COUNT=100 \
  go test ./infra/tests/load/ -run L3Realistic -v -timeout 4h
```

**Acceptance:** ≥ 95/100 tenants (95%) responding HTTP 200.

### Gate 3 — Loop back regression coverage

```bash
LOOPZERO_REAL_DEPLOY=1 \
  LOOPZERO_LOAD_LEVEL=l3-churn \
  go test ./infra/tests/load/ -run L3RealisticWithChurn -v -timeout 4h
```

**Acceptance:** All 100 tenants (including the 10 deleted+recreated) serve HTTP 200.

## Success Metrics

| Metric                 | Target          | Pre-CHG baseline |
|------------------------|-----------------|------------------|
| Destroy time           | < 5 min         | 60+ min          |
| Provision time         | < 30 min        | 90+ min          |
| Manual interventions   | 0               | 76 (from retro)  |
| L3-50 success rate     | >= 94%          | unknown          |
| L3-100 success rate    | >= 95%          | 0% (B16 blocked) |
| L3-Churn success rate  | >= 95%          | 0% (B11 blocked) |

## Recovery

If a gate fails:

1. **Provision time > 30 min:** dump pod events with `kubectl get events -A`. Common
   cause: helmfile chart download timeout — check repo connectivity.
2. **L3-50 < 94%:** likely capacity issue. Bump `runtimeMaxNodes` and retry without
   re-provisioning L1: `loopzero platform install -f values.yaml --provider digitalocean --skip-infra`.
3. **regression-checks.sh fails:** specific bug regressed — see
   `docs/runbooks/bootstrap-regression-matrix.md` for the protection.

## Status: PENDING EXECUTION

This runbook documents the validation procedure. Actual execution against
DigitalOcean is **out of scope** for the current implementation session because:
- Real DO credentials are required
- L3-100 takes ~4h, plus L3-50 + provisioning (~6h total)
- The user must approve provisioning costs (~$50 for the test window)

When ready to execute, run the procedure above and append:

```markdown
## Execution Log — <DATE>

- Operator: <name>
- Stack: dev
- Gate 0 destroy duration: ___s
- Gate 1 L3-50 result: __ / 50
- Gate 2 L3-100 result: __ / 100
- Gate 3 L3-Churn result: __ / 100
- Anomalies: <list>
```
