# Load Testing — LoopZero Platform

## Canonical Tools

| Tool | Purpose | Entry Point |
|------|---------|-------------|
| **Go harness** (`real_deploy_harness.go`) | Pipeline E2E tests (provision → build → deploy → verify) | `task loadtest:run LEVEL=l1` |
| **k6** (`../k6/scripts/`) | HTTP-level SLO testing (latency, throughput, error rate) | `task test:load:l1` |

## Test Levels

| Level | Tenants | Builds | Purpose |
|:---:|:---:|:---:|---------|
| L1 | 10 | 10 | Smoke test — every component works |
| L2 | 50 | 50 | Moderate load — autoscaler responds |
| L3 | 100 | 100 | Scale validation — 95+/100 pass required |
| L4 | 500 | 500 | Capacity test — find bottlenecks |
| L5 | 1000+ | 1000+ | Stress test — find breaking point |

## Quick Start

```bash
# Run L1 smoke test (recommended first run after infra recreate)
task loadtest:run LEVEL=l1

# Run k6 HTTP SLO test
task test:load:l1

# Run specific level
task loadtest:run LEVEL=l3
```

## File Inventory

| File | Status | Purpose |
|------|:---:|---------|
| `real_deploy_harness.go` | **Canonical** | Go E2E test harness (638 LOC) |
| `real_deploy_harness_test.go` | **Canonical** | Test helpers |
| `run-tests.sh` | **Canonical** | Unified runner for Go harness |
| `CORRECTIONS.txt` | **Reference** | War journal from stress test (19 manual fixes) |
| `stress-20k.sh` | **Deprecated** | Emergency seeding script — use Go harness instead |
| `run-l1.sh` | **Deprecated** | Pre-dates Go harness — superseded |

## Rules (MEET-055)

1. **Load test = Go harness + k6.** Bash scripts are deprecated.
2. **Always run L1 after infra changes** before proceeding to higher levels.
3. **k6 for HTTP SLOs**, Go harness for pipeline E2E. Different tools for different purposes.
4. **Results go to Grafana** — check `load-test.json` dashboard.
