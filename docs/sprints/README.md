# LoopZero — Sprint Plan

> Extracted from `docs/technical.md` §23. The system design is the authoritative source. These files are a working reference for sprint execution.

## Overview

| Sprint | Weeks | Focus | Product Exit Gate | CLI Commands |
|--------|-------|-------|-------------------|--------------|
| [0](./00-foundation/sprint.md) | 1-2 | Foundation | `task setup` boots, Grafana running | — |
| [1](./01-login-init-tenant-controller/sprint.md) | 3-5 | Login + Init + TenantController | `loopzero login` → `loopzero init` → `loopzero.yaml` | `login`, `logout`, `whoami`, `init` |
| [2](./02-deploy-happy-path/sprint.md) | 6-9 | Deploy happy path | `loopzero deploy` → live URL | `deploy` |
| [3](./03-logs-status-secrets/sprint.md) | 10-12 | Logs + Status + Secrets | All daily workflows functional | `logs`, `status`, `inspect`, `secret` |
| [4](./04-rollback-promote-cancel/sprint.md) | 13-15 | Rollback + Promote + Cancel | Recovery in < 30s | `rollback`, `promote`, `build`, `env`, `team`, `project` |
| [5](./05-preview-environments/sprint.md) | 16-18 | Preview environments | PR → preview URL in comment | `preview` |
| [6](./06-security-supply-chain/sprint.md) | 19-21 | Security + Supply chain | Signed images, encrypted secrets, RBAC | — |
| [7](./07-observability/sprint.md) | 22-23 | Observability complete | Incident resolved via dashboards | — |
| [8](./08-hardening-chaos/sprint.md) | 24-25 | Hardening + Chaos | 50 concurrent deploys, all chaos passes | — |
| [9](./09-polish-launch/sprint.md) | 26-27 | Polish + Launch | Non-team-member onboards in < 10 min | — |

## Note on Dashboard

The web dashboard is **not in scope** for Sprints 0-9. Prerequisites before dashboard work begins:

- All commands in §25 implemented and tested
- At least 10 active users using the product via CLI
- Real usage feedback informs which dashboard flows have highest value
