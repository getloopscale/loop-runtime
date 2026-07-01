# Deep Analysis — Scored Comparisons (loopzero target vs sealos reference)

Scale 0-5 = how loopzero (target) scores; 5 = on par with or better than reference.

| # | Dimension | Score | loopzero evidence | sealos evidence |
|---|---|:---:|---|---|
| 5 | Error Handling | **4.5** | `pkg/errors/errors.go:40` LoopZeroError typed + `pkg/errors/http/mapping.go:127` code→status | `account_controller.go:335` plain `fmt.Errorf %w`, no typed errors |
| 6 | Testing Strategy | **4.5** | 792 `_test.go` + 62 Ginkgo/envtest + property tests | 161 `_test.go` (+36 TS) |
| 10 | API Design & Contracts | **4.5** | `middleware/idempotency.go` + `--json` contract + exit codes + scrub | `service/account/api/card.go:88` Gin binding validation |
| 2 | Folder Organization | 4.0 | 33 named pkgs in api/internal | controllers/ + service/ domain split |
| 3 | Multi-Tenancy & Isolation | 4.0 | NetworkPolicy triad + multi-cell (`preview_controller.go`) | ns+RBAC+quota (`user_controller.go:299`) |
| 4 | Operator/Controller Design | 4.0 | state machines (`statemachine/application.go`) + finalizers + envtest | finalizers + conditions (`license_controller.go:89`) |
| 8 | Observability | 4.0 | 119 `loopzero_*` Prometheus + tracing (`metrics/deploy_timeline.go:22`) | zap logging + dedicated obs services (hubble/vlogs) |
| 9 | Build & Dev Experience | 4.0 | Taskfile + Tilt + k3d parity | per-module matrix (`detect-build-modules.sh:20`) |
| 11 | Security & Secrets | 4.0 | Vault Transit + cosign/SBOM + Ory (`pkg/vault/transit_client.go`) | AES-GCM + JWT HS256 (`crypto.go:38`) |
| 12 | Configuration Mgmt | 4.0 | dual chart + helmfile overlays | per-controller chart (`user-controller/values.yaml`) |
| 1 | Module Boundaries | 3.5 | single coarse `go.work` | 12 per-domain modules (`controllers/go.work`) |
| 7 | Metering & Billing | **3.0** | request-time caps + Stripe (`pkg/enforcement`) | continuous metering reconcile (`monitor_controller.go:438`) |

**Unweighted mean: 4.0/5.** Reference is stronger on: continuous metering pipeline, module isolation, dedicated observability services. Target is stronger on: typed errors, test breadth, external API contract discipline.
