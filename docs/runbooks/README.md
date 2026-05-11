# Runbooks

Operational runbooks for the LoopZero platform. Each runbook is referenced by Prometheus alert `runbook_url` annotations, CI workflows, and operational scripts.

**Status:** Pending creation as part of Sprint 9 (Production Readiness). The directory structure exists because 80+ references in production code (Helm alert templates, Go source, CI workflows, shell scripts) point here. Runbooks will be authored during the production canary phase.

## Expected Runbooks

Referenced across the codebase but not yet authored:

| Runbook | Referenced By |
|---------|--------------|
| `alerts/` | PrometheusRule annotations (multi-cell, velero, policy-controller, capacity) |
| `multi-cell-alerts.md` | `prometheusrule-multi-cell.yaml`, parity tests |
| `multi-cell-canary.md` | `cell2-soaks.yml`, canary stage scripts |
| `multi-cell-canary-prod.md` | API background setup, ADR verification |
| `bootstrap-regression-matrix.md` | `parity-validation.yml`, load tests, chaos plan (T3.2 BLOCKED) |
| `disaster-recovery-restore.md` | `velero-backup-check.yml`, velero alerts |
| `charts-drift-audit.md` | `charts-drift-cron.yml`, audit-helm-drift.sh |
| `operator-cutover-runbook.md` | operator cutover alerts and scripts |
| `soak-test-failure.md` | k6 load tests, soak alerts |
| `observability.md` | dogfood repro script, loopzero-api ServiceMonitor |
| `service-auth-rotation.md` | PrometheusRule warning |
| `security-context-audit-2026.md` | promtail + nfs-server-provisioner ADRs |
| `pulumi-consolidation.md` | pulumi-state PrometheusRules |

## Convention

- Filename matches the Prometheus alert name (kebab-case) or operational domain
- Each runbook follows: **Symptom** / **Diagnosis** / **Resolution** / **Escalation** structure
- Alert-specific runbooks go in `alerts/` subdirectory
