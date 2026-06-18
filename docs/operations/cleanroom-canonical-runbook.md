# Cleanroom Canonical Runbook (T6.1 Phase 6)

Per plan `2026-06-11-cleanroom-canonical-5-defense-layers` v1.2 SHIPPABLE
98.0 (sponsor verbatim approved 2026-06-11).

This runbook is the operator-facing executable form of Phase 6 (Integration
Validation Final Phase). Following this runbook end-to-end emits the
`<promise>IMPLEMENTATION_COMPLETE</promise>` marker that closes the plan.

## Pre-conditions

Before invoking this runbook:

1. Branch on `worktree-glowing-growing-babbage` (NOT main per Unbreakable Rule 4)
2. Plan v1.2 + all Phase 1-5 commits shipped
3. Sponsor availability confirmed for J-07 + J-08 walkthrough
4. `~/.loopzero/local-dogfood-token.txt` JWT seed file present (mode 0600)
5. `~/.loopzero/dev-credentials.json` sponsor-provided creds file (mode 0600)
   for T2.2 `secrets-seed-dev.sh`

## Canonical sequence (~60-90min total)

### Step 1-3: Cleanroom TOTAL + dev loop (~30-45min)

```bash
task clean:all      # destroy 4 k3d clusters (~5min)
task setup          # recreate infra Helmfile sync (~25min)
tilt up &           # background dev loop (~5-10min)
```

Per `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL —
**this step CANNOT be skipped for valid LIVE validation**. Sponsor verbatim
override required if time-pressure demands warm-cluster path.

### Step 4: task doctor health gate (~30s)

```bash
task doctor
# Required: PASS=38+ FAIL=0
```

If FAIL > 0, abort + investigate. Common scenarios in `cleanroom-up-live-test-mandate.md`.

### Step 4.5: task check sprint (T4.1) (~30-60s)

```bash
bash infra/scripts/local/check-sprint.sh --fail-fast
# Required: FAIL=0
```

8 categorias x ~44 probes. Per ADR D3 COMPLEMENTAR a doctor (LoopZero canonical
state vs generic K8s health). Per ADR D4 + D9, Cleanroom uses --fail-fast.

### Step 5: Vault bootstrap + secrets seed (T2.1 + T2.2) (~5-10min)

```bash
bash infra/scripts/local/vault-bootstrap-dev.sh
# Aggregates 10 canonical OpenBao Transit manual steps (init + Shamir
# unseal + approle + role_id + secret_id + Secret + SPKI pin extraction)

bash infra/scripts/local/secrets-seed-dev.sh
# Reads ~/.loopzero/dev-credentials.json + writes 5 kv-v2 paths
```

### Step 6: LIVE E2E 9-journeys auto subset (~15min)

```bash
bash infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh
# 7 auto-journeys: J-22 + J-09 + J-23 + J-10 + J-17 + J-05 + J-04
```

Per ADR D10: auto journeys scripted bash without human interaction. PASS
counter increments enforce determinism. Per ADR D6: bash NOT Ginkgo for
LIVE orchestration.

**Engineering-side closure**: after 7 auto-journeys GREEN, emit
intermediate marker `IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE` per ADR D7
(sponsor walkthrough scheduling honored async).

### Step 7: Sponsor walkthrough JUNTOS (J-07 + J-08) (~30min)

**OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL.**

Sponsor must be present + verbatim approval captured.

#### J-07 Projects List walkthrough

1. Operator + sponsor open https://app-dev.loopzero.example/projects
2. Verify:
   - Project list renders (NO empty state if connect-canonical-fixture seed ran)
   - Each row shows Apps count + Framework + Last deploy timestamp (T2.1 enriched)
   - "Load more" button if N > page size
   - DataTable interactions (sort, filter)
3. Capture sponsor verbatim approval

#### J-08 Project Detail Overview walkthrough

1. Operator + sponsor click a project from J-07 list
2. Verify:
   - Overview page renders /projects/:name
   - Summary card shows env count + apps count + last deploy status
   - Deploys list shows builds[] from canonical source
   - Project Health Card NOT empty (T1.1 R1 closure — uses canonical fields)
3. Capture sponsor verbatim approval

### Step 8: Record sponsor verbatim approval + emit IMPLEMENTATION_COMPLETE

Once sponsor verbatim approval recorded (e.g., "Ficou ótimo, parabens!" OR
"Confirmado"), commit baseline doc OR update `CLAUDE.md § Dashboard Journeys
delivered` section per `feedback_claude_md_canonical_roadmap_not_roadmap_md`.

Then emit canonical promise:

```
<promise>IMPLEMENTATION_COMPLETE</promise>
```

## Single-command chain runner

For convenience, `cleanroom-canonical-chain-runner.sh` automates steps 1-6
with verdict tracking:

```bash
# Full chain (Cleanroom TOTAL + LIVE + pause for sponsor)
bash archive/canonical-tests-v1/scripts/local/cleanroom-canonical-chain-runner.sh

# Skip Cleanroom (debug iteration on existing cluster)
bash archive/canonical-tests-v1/scripts/local/cleanroom-canonical-chain-runner.sh --skip-cleanroom

# Engineering-side only (stop at step 6, skip sponsor pause)
bash archive/canonical-tests-v1/scripts/local/cleanroom-canonical-chain-runner.sh --skip-sponsor
```

## Abort criteria

| Step fails | Action |
|---|---|
| 1 (clean:all) | Investigate k3d cluster state. Manual `docker rm -f k3d-loopzero-*`. |
| 2 (setup) | Investigate helmfile sync error. Check `/tmp/cleanroom-setup.log`. |
| 4 (doctor) | Per `cleanroom-up-live-test-mandate.md` § Common scenarios. |
| 4.5 (check) | Run `check-sprint.sh --category=<failed>` for isolated debug. |
| 5 (vault) | Vault may have stale state. Inspect `~/.loopzero/cleanroom-unseal.json`. |
| 6 (9-journeys) | Run `--journey=J-NN` for isolated debug. Check per-journey log. |
| 7 (sponsor) | Capture sponsor specific feedback for fix-forward iteration. |

## Related

- Plan: `knowledge-base/plans/2026-06-11-cleanroom-canonical-5-defense-layers-plan.md` v1.2
- Mandate (INQUEBRÁVEL): `.claude/rules/cleanroom-up-live-test-mandate.md`
- Cleanroom skill: `.claude/skills/cleanroom/SKILL.md`
- Phase 3 hermetic gates (CI regression-defense): `infra/tests/helm/*.sh` + `cloud/tests/unit/*.go`
- Phase 4 check sprint: `infra/scripts/local/check-sprint.sh` + `infra/scripts/local/check-sprint/c[1-8]-*.sh`
- Phase 5 LIVE E2E: `infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh`
