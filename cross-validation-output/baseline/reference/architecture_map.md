# Reference Architecture Map — sealos (labring/sealos)

**Identity:** AI-native Cloud Operating System on Kubernetes — unifies dev (cloud IDE) → production deploy → management.
**Scale:** 3,895 source files · ~495K LOC. Dominant: TypeScript (2,113 — web desktop/app store), Go (836 — controllers + services), YAML (625), Rust (9 — service-rs).
**License:** Apache-2.0.

## Top-level layout (the organizing architecture)

| Layer | Path | Role |
|---|---|---|
| Controllers (operators) | `controllers/` | kubebuilder operators — one Go module per domain (go.work) |
| Services (HTTP backends) | `service/` | Gin/HTTP backends — one Go module per domain (go.work) |
| Frontend | `frontend/` | Next.js monorepo (pnpm workspace): `desktop` shell + `providers/*` apps + `packages/*` libs |
| Webhooks | `webhooks/` | admission webhooks |
| Lifecycle | `lifecycle/` | `sealos` cluster install/bootstrap CLI tooling |
| Rust services | `service-rs/` | performance-sensitive services in Rust |

## Component inventory (registered)

- **Controllers** (`controllers/`): `user` (tenant/namespace provisioning), `account` (billing/balance/payment reconcile), `app`, `terminal` (cloud terminal), `objectstorage` (MinIO tenant), `devbox` (cloud IDE env), `resources` (usage metering), `license`, `node`, `job`, `pkg` (shared). Each is its own go module (`go.work`).
- **Services** (`service/`): `launchpad` (app deploy backend), `account`, `database` (managed DB), `pay` (payment gateway), `aiproxy` (AI model gateway), `devbox`, `minio`, `hubble` (network observability), `vlogs` (logs), `sshgate`, `exceptionmonitor`, `pkg`.
- **Frontend** (`frontend/`): `desktop` (cloud OS shell), `providers/*` (per-app frontends: applaunchpad, dbprovider, costcenter, terminal, template, etc.), `packages/*` (shared UI/client libs).

## Architectural signatures
- **Per-domain Go modules** via `go.work` — each controller and service is an independently buildable module (strong module boundary).
- **App-store model** — frontend `providers/*` are pluggable apps over a desktop shell; metering + account are platform-wide.
- **Metering-first billing** — `controllers/resources` + `controllers/account` + `service/pay` form an end-to-end usage→billing pipeline.
- **Cloud-IDE as first-class** — `devbox` + `terminal` controllers give in-cluster development environments (no loopzero equivalent).

## Entry points
- `controllers/*/main.go` → operator binaries; `service/*/cmd` or `main.go` → HTTP services; `frontend/desktop` → Next.js; `lifecycle/cmd/sealos` → install CLI.
