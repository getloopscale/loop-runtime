# Dogfood 2026-04-29 Repro Suite

Scripts and tests that reproduce the 13 dogfood findings
``dogfood-2026-04-29-remediation-plan`` *(removed from version control)*
against k3d standalone-multi.

## Usage

```bash
task dogfood:repro                                  # Run completo + summary
bash infra/tests/livetest/dogfood/repro-findings.sh --json   # JSON only (CI)
```

## Output Schema

```json
{
  "findings": [
    {
      "id": "F-01",
      "severity": "S0",
      "status": "REPRODUCED" | "FIXED" | "INDETERMINATE",
      "evidence": "..."
    }
  ],
  "summary": {
    "total": 13,
    "reproduced": 10,
    "fixed": 0,
    "indeterminate": 3
  }
}
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Every finding still REPRODUCES (matrix consistent, no fix merged) |
| 1 | At least one finding FIXED — update the regression matrix and the CHANGELOG |
| 2 | Pre-condition error (k3d absent, kubectl fails) |

## Status `INDETERMINATE`

Some checks depend on others (cascade). For example, F-07 (`/api/v1/version` returns 404)
cannot be validated while F-01 (API unreachable) still reproduces, because the connection refusal
masks the 404. Once F-01 is fixed, F-07 becomes deterministic.

## Finding → Check Function Mapping

| Finding | Check | Severity | Category |
|---------|-------|----------|-----------|
| F-01 | `check_F01` | S0 | Infra (NodePort/IngressRoute) |
| F-02 | `check_F02` | S1 | CLI (exit code drift) |
| F-03 | `check_F03` | S2 | CLI (Commander parse) |
| F-04 | `check_F04` | S1 | Operator (Tenant ns) |
| F-05 | `check_F05` | S1 | CLI (init silent-success) |
| F-06 | `check_F06` | S3 | CLI (logout msg) |
| F-07 | `check_F07` | S2 | API (`/version`) |
| F-08 | `check_F08` | S3 | Observability (`/metrics`) |
| F-09 | `check_F09` | S2 | Operator (pull secret loop) |
| F-10 | `check_F10` | S2 | Operator (OTel exporter) |
| F-11 | `check_F11` | S2 | ArgoCD (sharding/Lua) |
| F-12 | `check_F12` | S2 | API (ArgoCD auth) |
| F-13 | `check_F13` | S3 | Argo Workflows (podGC) |
