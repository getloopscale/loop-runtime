# E2E Tests — Ginkgo

End-to-end tests for the LoopZero platform. Runs against real k3d infrastructure.

## Quick Start

```bash
# 1. Setup infrastructure
task setup && tilt up && task doctor

# 2. Run smoke tests (<5 min)
task e2e:smoke

# 3. Run all tests
task e2e:all
```

## Labels

| Label | Description | Command |
|-------|------------|---------|
| `smoke` | Quick subset for development | `task e2e:smoke` |
| `journey` | Full end-to-end flows (11 journeys) | `task e2e:journey` |
| `edge-case` | Concurrency, failures, state machine | `task e2e:edge` |

## Running Specific Tests

```bash
# By label
LOOPZERO_E2E=1 ginkgo --label-filter="journey" ./infra/tests/e2e/

# By name
LOOPZERO_E2E=1 ginkgo --focus="Journey 01" ./infra/tests/e2e/

# With JUnit report
task e2e:report
```

## Test Structure

```
e2e/
  constants_test.go       — Timeouts, forbidden terms, exit codes
  helpers_test.go         — Shared journey helpers (waitBuildTerminal, proveDead, etc.)
  cli_helpers_test.go     — CLI execution helpers (runLoopZero, parseJSON, etc.)
  deploy_suite_test.go    — Suite entrypoint

  journey_*_test.go       — Full E2E flows (migrated from bash)
  edge_*_test.go          — Edge case tests
  failure_scenarios_test.go — Build/deploy failure paths
  cli_*_test.go           — Per-command CLI contract tests

  fixtures/               — Test applications
    solo-server/           Single app (api: server)
    multi-server/          Multi-app (api + worker)
    secret-app/            App with secret declaration
    versioned-app/         App with VERSION.txt
    broken-build/          Intentionally failing Dockerfile
    hello-server/          Simple Node.js server
    multi-app/             Multi-app fixture
    dogfood-monorepo/      Full monorepo
    static-frontend/       Static frontend app
```

## Troubleshooting

**Tests skip with "Set LOOPZERO_E2E=1":**
```bash
export LOOPZERO_E2E=1
```

**Tests skip with "clusters not running":**
```bash
task setup && tilt up
kubectl cluster-info  # Verify clusters are reachable
```

**Tests skip with "API not ready":**
```bash
task doctor           # Check infrastructure health
curl http://localhost:3001/api/v1/health/deep
```

**Deploy timeout:**
Default deploy timeout is 25 minutes. If builds are slow, check BuildKit and registry health.

**Auth errors:**
The seed script creates test tokens. If expired:
```bash
task db:seed
```

## Adding a New Test

1. Create `journey_<name>_test.go` or `edge_<name>_test.go`
2. Add `Label("journey")` or `Label("edge-case")` to the `Describe`
3. Use `skipIfNotE2E()` and `seedE2E()` in `BeforeAll`/`BeforeEach`
4. Use `deployAndWaitSuccess()` for deploy + wait
5. Use `proveDead()` for cleanup verification
6. Use `assertNoForbiddenTermsInAll()` on all output (Rule 3)
7. Use `expectAllowedExitCode()` on all exit codes (Rule 4)
