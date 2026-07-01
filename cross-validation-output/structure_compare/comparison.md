# Structure Comparison — loopzero (target) vs sealos (reference)

## Dimensions defined (12)

| # | Dimension | Category | Weight |
|---|---|---|---|
| 1 | Module Boundaries & Layering | architecture | 1.5 |
| 2 | Folder Organization & Naming | architecture | 1.0 |
| 3 | Multi-Tenancy & Isolation | security | 1.5 |
| 4 | Operator / Controller Design | architecture | 1.5 |
| 5 | Error Handling | error_handling | 1.3 |
| 6 | Testing Strategy | testing | 1.4 |
| 7 | Metering & Billing Pipeline | domain | 1.0 |
| 8 | Observability | observability | 1.1 |
| 9 | Build System & Dev Experience | tooling | 1.0 |
| 10 | API Design & Contracts | api_design | 1.1 |
| 11 | Security & Secrets Management | security | 1.3 |
| 12 | Configuration Management | configuration | 0.8 |

## Key structural differences

| Axis | loopzero | sealos | Note |
|---|---|---|---|
| Module split | single `go.work` (7 coarse modules) | `controllers/go.work` (12) + `service/go.work` (~12), per-domain, `replace`-pinned | sealos = stronger boundary; loopzero = simpler build (KISS) |
| Product shape | CLI-first, backend-heavy (Go 1375 files) | Web-first cloud OS (TS 2113 files) + Go controllers | different surfaces |
| RBAC/CRD wiring | centralized `charts/` + `infra/helm` (GitOps) | per-controller `config/` kustomize scaffolding | both valid |
| Metering | request-time caps (`pkg/enforcement`) + `cloud` Stripe | continuous reconcile (`controllers/resources/monitor_controller.go`) → `account` → `pay` | sealos = continuous loop |
| Shared utils | `pkg/{circuitbreaker,id,naming,...}` | `controllers/pkg/utils/{retry,rate,lock,jwt}` | sealos surfaces retry/rate/lock explicitly |
| Testing (Go) | 792 `_test.go` + 62 Ginkgo/envtest suites | 161 `_test.go` | **loopzero stronger** |

## Honesty notes
- Comparison is scoped to the **Go control-plane layer** where both compete. sealos's TS frontend (cloud desktop / app store) and Rust services have NO loopzero counterpart and are out of scope for scoring.
- loopzero's CLI-first model means its "frontend" is a CLI + a thin dashboard; sealos's is a full web OS. Not a defect on either side — different products.
