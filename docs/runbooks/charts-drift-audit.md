# Charts Drift Audit — Runbook

## Purpose

`infra/scripts/audit-helm-drift.sh` detecta drift entre `charts/` (published) e `infra/helm/` (bootstrap). Implementa CH-G16 (NEW gate, charts-sota-implementation-plan T1.3) per ADR-0041.

## When This Triggers

- CI run on every push/PR touching `charts/**` or `infra/helm/**`.
- Manual: `bash infra/scripts/audit-helm-drift.sh` (human format) or `bash infra/scripts/audit-helm-drift.sh json | jq .`.

## What "Drift" Means

A drift is detected when:
- Same name in both `charts/<X>` and `infra/helm/<X>` but different template lists; OR
- Conceptual mapping in `CONCEPTUAL_PAIRS` (e.g., `charts/loopzero-admission-policies` ↔ `infra/helm/admission-policies`) has template count or set diff.

Drift severity:
- `info` — pair has approving ADR (intentional). Reported but doesn't fail CI.
- `medium`/`high` — drift unapproved. Currently advisory mode (CI warns); graduates to blocking pre-GA.

## Workflow When Drift Is Detected

### Step 1: Determine if intentional or regression

Read the drift report:
- "Only in published" templates that exist in `charts/` but not `infra/helm/` — typically MORE production-grade resources (RBAC, PDB, NetworkPolicy, ConfigMap, Secret) than dev bootstrap.
- "Only in bootstrap" templates that exist in `infra/helm/` but not `charts/` — typically dev-specific (IngressRoute for k3d local) OR a regression where production posture wasn't propagated to published chart.

**Rule of thumb:**
- Bootstrap minimal vs published full = INTENTIONAL (e.g., loopzero-api, loopzero-cloud — write ADR).
- Bootstrap stricter posture missing in published = REGRESSION (e.g., admission-policies pre-T1.2 — must fix).

### Step 2a: If INTENTIONAL — write an ADR

1. Create `docs/meetings/adr/00NN-drift-<published>-<bootstrap>.md` documenting:
   - **Decision**: drift X is intentional because...
   - **Rationale**: production needs Y; dev needs Z.
   - **Consequences**: published chart cannot be used as-is for local dev (or vice versa).
2. Add to `APPROVED_DRIFTS` map in `infra/scripts/audit-helm-drift.sh`:
   ```bash
   ["<published>:<bootstrap>"]="ADR-NNNN"
   ```
3. Re-run audit: drift becomes `intentional: true`, exit 0.

### Step 2b: If REGRESSION — fix the chart

1. Identify which side is correct (typically infra/helm has stricter prod posture).
2. Port missing templates from one side to the other.
3. Extend values + schema accordingly.
4. Bump chart version (minor for additive, major for breaking).
5. Run helm-unittest tests.
6. Re-run audit: drift cleared.

## Output Formats

### Human-readable (default)
```
$ bash infra/scripts/audit-helm-drift.sh
Drift audit report
===================

ℹ loopzero-api:loopzero-api
  published: 10 templates / bootstrap: 4 templates
  Only in published: clusterrolebinding.yaml ...
  STATUS: intentional (approved by ADR-0043)

✓ All drifts have approving ADRs (intentional).
```

### JSON (machine-readable)
```
$ bash infra/scripts/audit-helm-drift.sh json | jq .
{"drifts":[{"pair":"loopzero-api:loopzero-api","intentional":true,"adr":"ADR-0043",...}]}
```

### GitHub annotations (CI)
```
$ bash infra/scripts/audit-helm-drift.sh github-annotations
::notice::charts/ ↔ infra/helm/ drift audit: no unapproved drifts.
```

Or if drift unapproved:
```
::warning::Drift detected for loopzero-X:loopzero-X: templates differ; 12 vs 3.
```

## Graduation Plan (Advisory → Blocking)

Per ADR-0041 + CONDITION-3 (platform-security-engineer):

- **D+0 (now)**: Advisory. CI emits annotations; doesn't fail.
- **D+30 OR pre-GA (whichever first)**: Each unapproved drift classified (ADR or fix).
- **Post-classification**: Remove `continue-on-error: true` from CI step. New drifts require ADR before merge.

## Related

- Plan: `docs/plans/charts-sota-implementation-plan.md` T1.3
- ADR-0041: drift detector advisory→blocking strategy
- ADR-0042: ghost chart deletion T1
- CH-G16: NEW gate — Drift Detector Between Bootstrap And Published
- Script: `infra/scripts/audit-helm-drift.sh`
- Tests: `infra/scripts/audit-helm-drift.test.sh`
