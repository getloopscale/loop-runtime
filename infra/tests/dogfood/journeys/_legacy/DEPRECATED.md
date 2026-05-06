# DEPRECATED — Bash Journey Scripts

These bash journey scripts have been replaced by Ginkgo E2E specs in `infra/tests/e2e/`.

## Replacement Commands

```bash
# Run all journeys (replaces running individual scripts):
LOOPZERO_E2E=1 ginkgo --label-filter="journey" ./infra/tests/e2e/

# Run smoke subset (<5 min):
LOOPZERO_E2E=1 ginkgo --label-filter="smoke" ./infra/tests/e2e/

# Run specific journey:
LOOPZERO_E2E=1 ginkgo --focus="Journey 01" ./infra/tests/e2e/

# Run edge cases:
LOOPZERO_E2E=1 ginkgo --label-filter="edge-case" ./infra/tests/e2e/
```

## Mapping

| Bash Script | Ginkgo Spec |
|---|---|
| journey-01-solo-deploy.sh | journey_solo_deploy_test.go |
| journey-02-multi-server.sh | journey_multi_server_test.go |
| journey-03-secrets.sh | journey_secrets_test.go |
| journey-04-scale.sh | journey_scale_test.go |
| journey-05-promote.sh | journey_promote_test.go |
| journey-06-rollback.sh | journey_rollback_test.go |
| journey-07-custom-domain.sh | journey_custom_domain_test.go |
| journey-08-github-autodeploy.sh | journey_autodeploy_test.go |
| journey-09-preview.sh | journey_preview_test.go |
| journey-10-failures.sh | journey_failures_test.go + failure_scenarios_test.go |
| journey-11-admin.sh | journey_admin_test.go |

## Why

- Ginkgo `Eventually()` replaces fragile `while sleep` loops
- `DeferCleanup` guarantees teardown even on panic
- Labels enable selective execution (`smoke`, `journey`, `edge-case`)
- JUnit XML reporting for CI
- Type safety + IDE support + parallel execution
