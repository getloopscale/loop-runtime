# Phase-5 Classification Ledger — un-homed operational logic

Proposed disposition per script (operator ratifies). Evidence is mechanical fact.

| Domain | Script | LOC | Python | ShellsLive | Proposed disposition |
|---|---|---:|:---:|:---:|---|
| bootstrap | infra/helmfile/scripts/bootstrap-cell-cc.sh | 149 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/bootstrap-eso-cluster-secret-store.sh | 196 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/bootstrap-oauth-clients.sh | 172 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/bootstrap-patch-admission-policies.sh | 29 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/bootstrap-patch-sigstore-policy-controller.sh | 43 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/bootstrap-patch-argocd-gitea-creds.sh | 45 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/bootstrap-patch-argocd-repo-server-restart.sh | 29 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/create-wiring-configmap.sh | 85 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/generate-build-secrets.sh | 120 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/generate-runtime-secrets.sh | 140 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/generate-secrets.sh | 391 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/health-check-postsync.sh | 108 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/install-local-path-provisioner.sh | 24 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/lib/argocd-register.sh | 99 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/lib/clustercredential-create.sh | 159 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/lib/hydra-mint.sh | 171 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/lib/pulumi-output.sh | 127 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/lib/scoped-kubeconfig.sh | 183 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/migrate-database.sh | 44 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/regression-checks.sh | 200 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/seed-local-user.sh | 187 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/test_helpers/mock-kubectl.sh | 54 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/test_helpers/run-all.sh | 37 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/test_helpers/test-create-wiring-configmap.sh | 68 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/test_helpers/test-generate-runtime-secrets.sh | 51 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/test_helpers/test-generate-secrets.sh | 83 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/test_helpers/test-idempotency.sh | 202 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/helmfile/scripts/wire-clusters.sh | 84 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/scripts/bootstrap-platform.sh | 276 | — | yes | DELEGATE-declarative (T2) |
| bootstrap | infra/scripts/bootstrap-seed-tenant-cr.sh | 58 | — | yes | DELEGATE-declarative (T2) |
| chaos | infra/scripts/chaos/baseline.sh | 228 | — | yes | MIGRATE-ginkgo (LIVE) |
| chaos | infra/scripts/chaos/cleanup.sh | 113 | — | yes | MIGRATE-ginkgo (LIVE) |
| chaos | infra/scripts/chaos/report.sh | 197 | — | yes | MIGRATE-ginkgo (LIVE) |
| chaos | infra/scripts/chaos/run-full.sh | 181 | — | yes | MIGRATE-ginkgo (LIVE) |
| chaos | infra/scripts/chaos/run-scenario.sh | 289 | — | yes | MIGRATE-ginkgo (LIVE) |
| drill | infra/scripts/check-drill-credentials-isolation.sh | 125 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| other | infra/scripts/dogfood-cleanroom-loop.sh | 424 | yes | yes | MIGRATE-go (python-wrapper — focused TDD) |
| drill | infra/scripts/dr-drill.sh | 297 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/cancel-refresh.sh | 142 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/chaos-orchestrator.sh | 275 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/cleanroom-orchestrator.sh | 257 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/cost-report.sh | 159 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/dogfood-orchestrator.sh | 290 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/evidence-capture.sh | 298 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/jwt-diagnose.sh | 372 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/preflight.sh | 381 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/state-reset.sh | 340 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/teardown-validator.sh | 283 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/dr/watchdog.sh | 149 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/drill-helmfile-bootstrap.sh | 1126 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/drill-mint-jwt.sh | 300 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| bootstrap | infra/scripts/lib/bootstrap-cell-cc-common.sh | 161 | — | yes | DELEGATE-declarative (T2) |
| drill | infra/scripts/lib/bootstrap-cell-cc-drill-common.sh | 166 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/lib/drill-dogfood-journeys.sh | 242 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| drill | infra/scripts/lib/drill-skill-common.sh | 464 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| other | infra/scripts/migrate-tenant-namespace.sh | 229 | yes | yes | MIGRATE-go (python-wrapper — focused TDD) |
| quality | infra/scripts/quality/flaky-track.sh | 77 | yes | — | MIGRATE-go (python-wrapper — focused TDD) |
| drill | infra/scripts/velero-dr-drill.sh | 413 | — | yes | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| bootstrap | infra/tests/bootstrap-smoke.sh | 304 | — | yes | DELEGATE-declarative (T2) |
| test | infra/tests/capacity/run-capacity.sh | 85 | — | yes | MIGRATE-ginkgo (LIVE) |
| chaos | infra/tests/chaos/values_gate_test.sh | 109 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/charts/values-schema-coverage_test.sh | 82 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/check-corrections-txt-closed_test.sh | 146 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/check-regen-scoped-kubeconfigs_test.sh | 213 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/docs/credentials-sot_test.sh | 105 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/docs/no-hardcoded-credentials_test.sh | 74 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/docs/pulumi-required-secrets_test.sh | 119 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/dogfood/cli-coverage.sh | 244 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/dogfood/full-loop.sh | 246 | yes | — | MIGRATE-go (python-wrapper — focused TDD) |
| test | infra/tests/dogfood/preflight.sh | 484 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/dogfood/repro-findings.sh | 302 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/dogfood/scripts/cleanup-stuck-finalizers.sh | 104 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh | 111 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/dogfood/setup-monorepo-turbo.sh | 66 | — | — | MIGRATE-ginkgo (LIVE) |
| drill | infra/tests/drill-dogfood-journeys_test.sh | 293 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/drill-jwt-diagnose_test.sh | 199 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/drill-jwt-gate_test.sh | 179 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/e2e/local-install-deploy.sh | 398 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/e2e/supply-chain-e2e.sh | 200 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/load/run-l1.sh | 154 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/load/run-tests.sh | 106 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/load/stress-20k.sh | 139 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/audit-argocd-drift.sh | 102 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/audit-golden-test.sh | 120 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/audit-regression-matrix.sh | 160 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/cell2-activation-gates.sh | 140 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/cell2-canary-stage1.sh | 308 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/cell2-canary-stage2.sh | 248 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/cell2-rollback-slo.sh | 265 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/doks-install.sh | 129 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/droplet-cloud-init.sh | 85 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/droplet-install.sh | 149 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/k3d-virgin-install.sh | 148 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c1.sh | 62 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c10.sh | 40 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c11.sh | 36 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c12.sh | 71 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c2.sh | 19 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c3.sh | 22 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c4.sh | 51 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c5.sh | 12 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c6.sh | 28 | — | — | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c7.sh | 26 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c8.sh | 27 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-c9.sh | 25 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/lib/cell2-gate-postflip.sh | 54 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/preflight.sh | 212 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/parity/validate-matrix.sh | 304 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/runner/run-chaos.sh | 133 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/runner/run-k6.sh | 83 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/runner/safety.sh | 56 | — | yes | MIGRATE-ginkgo (LIVE) |
| drill | infra/tests/scripts/bootstrap-drill-cc_test.sh | 264 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/check-advisory-lock-scope_test.sh | 47 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/check-cli-exit-codes_test.sh | 73 | — | — | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/check-drill-credentials-isolation_test.sh | 163 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/check-dry-run-support_test.sh | 141 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/check-environment-lock_test.sh | 118 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/check-pulumi-secrets_test.sh | 85 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/check-shell-hygiene_test.sh | 56 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/ci-path-filter-coverage_test.sh | 132 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/coverage-go_test.sh | 231 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/dockerfile-no-docker-hub_test.sh | 57 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/dogfood-cleanroom-loop_test.sh | 161 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-cancel-refresh_test.sh | 217 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-chaos-orchestrator_test.sh | 260 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-cleanroom-orchestrator_test.sh | 291 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-dogfood-orchestrator_test.sh | 305 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-evidence-capture_test.sh | 260 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-helmfile-bootstrap_test.sh | 836 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-mint-jwt_test.sh | 294 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-preflight_test.sh | 356 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-skill-common_test.sh | 456 | — | — | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-state-reset_test.sh | 366 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/scripts/drill-teardown-validator_test.sh | 300 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/dry-run-lib_test.sh | 122 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/helm-charts-schema-coverage_test.sh | 154 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/helm-patterns-audit_test.sh | 205 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/helm-schemas-negative_test.sh | 1159 | yes | yes | MIGRATE-go (python-wrapper — focused TDD) |
| test | infra/tests/scripts/helm-values-schemas_test.sh | 196 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/k3d-docker-hub-mirror_test.sh | 73 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/plan-2026-05-20-criteria_test.sh | 134 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/postgresql-pdb-config_test.sh | 49 | yes | yes | MIGRATE-go (python-wrapper — focused TDD) |
| test | infra/tests/scripts/run-migrations_bug14_test.sh | 164 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/run-migrations_sslmode_test.sh | 116 | — | yes | COUPLED (dies when its target homes) |
| test | infra/tests/scripts/test-inventory_test.sh | 215 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/skills/skill-paths_test.sh | 152 | — | yes | COUPLED (dies when its target homes) |
| drill | infra/tests/snapshots/render-drill.sh | 28 | — | — | MIGRATE-loopzero-ops-drill (LIVE-gate before delete) |
| test | infra/tests/snapshots/sanitize.sh | 18 | — | yes | MIGRATE-ginkgo (LIVE) |
| test | infra/tests/workflows/action-pinning-governance_test.sh | 81 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/workflows/advisory-mode-annotated_test.sh | 85 | — | — | COUPLED (dies when its target homes) |
| test | infra/tests/workflows/job-timeout-coverage_test.sh | 105 | yes | — | MIGRATE-go (python-wrapper — focused TDD) |
