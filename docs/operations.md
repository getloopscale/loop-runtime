# Operations

Getting the stack up, keeping it honest, and knowing which credentials an environment
needs. Procedures that recover or rotate something live in [`runbooks.md`](runbooks.md).

## Dev loop

```bash
task doctor    # environment health check — run this FIRST
task setup     # ONE-SHOT cold start: 4 k3d clusters + Helmfile sync (~15-45min)
tilt up        # DEV LOOP: hot reload of API/operators (canonical day-to-day)
task status    # clusters, pods, DB, API at a glance
```

**`task setup`** is one-shot: clusters, Helm releases, CRDs, tokens.
**`tilt up`** is the loop: watches Go source in `api/` and `operators/*`, rebuilds and
reloads on change, port-forwards the API to `localhost:13001` and Postgres to
`localhost:15432`, tails `loopzero-system` logs.
**`task doctor`** checks 4 clusters Ready, critical pods Running, API `/health` 200, JWT
seeded, `schema_migrations` populated, cross-cluster connectivity, registry reachable from
all clusters, ArgoCD repo creds, cell metrics emitted.

### When NOT to re-run setup

| You changed | Do |
|---|---|
| Go code | nothing — `tilt up` picks it up on save |
| a Helm chart (`infra/helm/X/`) | targeted `task infra:install:core`, not a full rebuild |
| a CRD schema (`operators/api/v1/`) | `task crds:install` — operators auto-restart via Tilt |
| a DB migration | `task db:migrate` — loopzero-api auto-restarts |
| the CLI | it lives in `../loopzero-cli` since M2. Build there, point at this stack with `LOOPZERO_API_URL=http://localhost:13001` |

Fresh start: `task clean:all` (destroys clusters + registry) then `task setup`, then
`tilt up`.

## Tests

```bash
task test              # unit + lint + manifest validation
task test:unit
task test:e2e          # Ginkgo E2E (requires running clusters + tilt up)
task test:integration  # requires running clusters
task test:coverage
```

Per module — **the root does not cover them**, there is no root `go.mod`:

```bash
cd api && go test ./...          # also pkg, operators, cmd/loopzero-ops
cd api && go test ./internal/services/... -run TestScalingResolver -v
```

A single Ginkgo spec. `go test -timeout` alone does **not** extend the Ginkgo suite timeout
(its own default is 60 min), so pass both:

```bash
cd infra/tests && go test ./e2e/... -v -timeout 120m \
  -args -ginkgo.timeout=2h -ginkgo.focus="deploy happy path"
```

**Two traps that make a green run meaningless.** A suite reporting
`SUCCESS! - Suite skipped in BeforeSuite` ran zero specs: `apiURL()` defaults to
`localhost:3001`, and in dev-public the API is on `localhost:13001` — export
`LOOPZERO_API_URL`. And the e2e seed defaults to a Postgres URL from an in-process PG that no
longer exists; port-forward `svc/postgresql 15432:5432` and set
`LOOPZERO_E2E_DATABASE_URL=postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable`.

## Quality gates — the CI substitute

GitHub Actions was removed as the pipeline on 2026-05-23. The gates moved to a pre-push hook
that runs **on the pusher's machine and only there** — so a repository with no CI gate has
none for anyone else. The gate inventory lives in `cmd/loopzero-ops`.

```bash
task quality:build                 # build the gate binary to /tmp/loopzero-ops — do this FIRST
/tmp/loopzero-ops quality --list       # the inventory: NAME / WIRED / SUMMARY — the oracle
/tmp/loopzero-ops quality <gate>       # run one gate

task quality:gates                 # full-repo scan
task quality:diff                  # PR-diff scoped (BASE_REF=<ref>, default origin/main)
task quality:all                   # hermetic tests + full scan + contract gates
task quality:contract:cross-repo   # decomposition seam contracts across sibling repos

task lint                          # golangci-lint v2.11+ + ESLint
task validate:manifests            # kubeconform --strict
task db:migrate                    # forward-only migrations (golang-migrate)
task db:reset                      # drop + recreate + migrate + seed
task db:generate                   # regenerate api/internal/db/generated/ via sqlc
task db:verify-sync                # assert committed generated/ matches a fresh regen
task db:render-schema              # ephemeral PG 16 + migrate up -> api/internal/db/schema.gen.sql
```

A stray `./loopzero-ops` at the repo root is a **gitignored local build artifact**, not the gate
binary. It can be arbitrarily stale and is absent in a fresh clone. Always build first;
never read it as the oracle.

**Adding a new `.sh` file is FORBIDDEN by default** (`no-new-bash` gate). New logic belongs
in `cmd/loopzero-ops/internal/quality/` (Go), `infra/helmfile/` (declarative), or
`infra/tests/e2e/` (Ginkgo).

## Prerequisites

Go 1.26+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, **helm-diff plugin**,
**Helmfile v1.0+**, kubectl, kubeconform, golangci-lint v2.11+, cosign (optional).

## Credentials

`FAIL fast` means the service refuses to start rather than defaulting.

### Local k3d

| Variable | Purpose | If unset |
|---|---|---|
| `LOOPZERO_DB_PASSWORD` | Postgres password, all services | FAIL fast |
| `LOOPZERO_GITEA_ADMIN_PASSWORD` | Gitea admin (org `loopzero`) | FAIL fast |
| `LOOPZERO_GITEA_TOKEN` | Gitea API token (CLI + operators) | generated by `provision-api-tokens.sh` |
| `LOOPZERO_HARBOR_ADMIN_PASSWORD` | Harbor admin | auto-generated |
| `LOOPZERO_HARBOR_REDIS_PASSWORD` | Harbor Redis | auto-generated |
| `MINIO_ROOT_PASSWORD` | MinIO admin (standalone only) | FAIL fast |

### Cloud

| Variable | Purpose |
|---|---|
| `DIGITALOCEAN_TOKEN` | DO API + DOKS provisioning |
| `PULUMI_ACCESS_TOKEN` | Pulumi state backend |
| `PULUMI_CONFIG_PASSPHRASE` | decrypts `secure:` entries in the Pulumi stack config |
| `CLOUDFLARE_API_TOKEN` | CF Zone + Worker management |

### Per environment

| Credential | Local k3d | Drill DOKS | Prod DOKS |
|---|---|---|---|
| `DIGITALOCEAN_TOKEN` | — | required | required |
| `PULUMI_ACCESS_TOKEN` / `PULUMI_CONFIG_PASSPHRASE` | — | required | required |
| `CLOUDFLARE_API_TOKEN` | — | required (CF Zone) | required |
| `SPACES_ACCESS_KEY_ID` + `SPACES_SECRET_ACCESS_KEY` | optional | required | required |
| `LOOPZERO_DB_PASSWORD` | required | injected | injected |
| `LOOPZERO_GITEA_*` | required | injected | injected |
| `LOOPZERO_*_OAUTH_*` (GitHub/Google) | optional (auth disabled in dev) | required | required |
| `LOOPZERO_STRIPE_API_KEY` + webhook secret | mock OK | required | required |
| `LOOPZERO_RESEND_API_KEY` | optional | required | required |
| `LOOPZERO_SESSION_TOKEN_SECRET` | generated locally | injected | injected |
| `COSIGN_KEY` + `COSIGN_PASSWORD` | local pair from `generate-build-secrets.sh` | injected | injected |

### Pulumi config secrets

Set with `pulumi config set --secret <key>` per stack. Required across dev / staging / prod / drill:
`digitaloceanToken`, `spacesAccessKeyId`, `spacesSecretAccessKey`, `giteaAdminPassword`,
`minioRootPassword`, `loopzeroApiDbPassword`, `sessionTokenSecret`, `serviceAuthSecret`.

Required when the matching feature is on: `cfApiToken`, `cfAccountId`, `cfZoneId`, `tunnelSecret`
(Cloudflare); `stripeApiKey`, `stripeWebhookSecret`, `stripePriceProId` (billing); `resendApiKey`
(mail); `githubOAuthClientId`, `googleOAuthClientId` (social login).

### K8s Secrets created at bootstrap

Reconciled by `task bootstrap:standalone` locally, by Pulumi + Helmfile on drill/prod.

| Secret | Namespace | Source | Consumers | Rotation |
|---|---|---|---|---|
| `loopzero-db-credentials` | loopzero-system | `generate-secrets.sh:146` | loopzero-api, loopzero-cloud, Hydra/Kratos DSN | manual — see playbook |
| `gitea-admin` | loopzero-system | `generate-secrets.sh:157-158` | Gitea pod, tenant + app operators | Gitea admin UI, then re-run `provision-api-tokens.sh` |
| `loopzero-api-tokens` | loopzero-system | `provision-api-tokens.sh:100` | loopzero-api `/health/deep` | re-run the script |
| `argocd-creds-gitea` | argocd | `provision-api-tokens.sh:119` | ArgoCD repo-creds (Gitea HTTPS) | with `gitea-admin` |
| `cluster-cell-0` | argocd | `provision-api-tokens.sh:155` | ArgoCD destination resolution | n/a — kubeconfig embedded |
| `minio-credentials` | argo | `generate-build-secrets.sh:108` | Argo Workflows + BuildKit cache | with the MinIO root |
| `cosign-signing-key` | argo | `generate-build-secrets.sh:87` | the cosign-signer step | **quarterly** |
| `buildkit-registry-config` | argo | `generate-build-secrets.sh:100` | BuildKit `--registry-config` | with the registry creds |
| `cloudflared-tunnel-token` | loopzero-system | `generate-runtime-secrets.sh:65,74` | cloudflared Deployment | CF Zero Trust |
| `loopzero-eso-source` | external-secrets | `generate-runtime-secrets.sh:90` | ESO ClusterSecretStore | with the ESO SA token |
| `static-assets-s3` | loopzero-system | `generate-runtime-secrets.sh:126` | nginx-s3-gateway | with the Spaces creds |
| `harbor-admin` | loopzero-system | `generate-secrets.sh:294` | Harbor login | Harbor admin |
| `harbor-redis-credentials` | loopzero-system | `generate-secrets.sh:300` | Harbor ↔ Redis | with the Harbor admin |
| `loopzero-registry-pull` | loopzero-system (workload) | `bootstrap-cluster.sh:76` | every `loopzero-*` imagePullSecret | `doctl registry kubernetes-manifest` |

**ESO-managed:** the ClusterCredential kubeconfig Secret in `loopzero-system` (core) is synced into the
workload cluster as `cell-0.kubeconfig` for loopzero-api.

### Chart-hardcoded credentials

Dev fallbacks compiled into chart values. Each is a known default that can reach production through
`helmfile -e standalone sync`, which is why they are enumerated rather than left implicit.

| File:Line | Literal |
|---|---|
| `infra/helm/postgresql/values.yaml:19` | `loopzero-dev-password` |
| `infra/helm/minio/values.yaml:7` | `loopzero-dev-password` |
| `infra/helm/loki/values.yaml:25` | `loopzero-dev-password` |
| `infra/helm/monitoring/values.yaml:57` | `loopzero-dev-password` (Grafana admin) |
| `infra/helm/gitea/values.yaml:7` | `loopzero-gitea-dev-2026` |
| `infra/helm/gitea/values.yaml:19` | `loopzero-dev-password` (PASSWD env) |
| `infra/helm/ory-hydra/values.yaml.gotmpl:18` | `loopzero-dev-password` (DSN inline) |
| `infra/helm/ory-kratos/values.yaml.gotmpl:14` | `loopzero-dev-password` (DSN inline) |
| `infra/helmfile/scripts/generate-secrets.sh:140` | `loopzero-dev-password` fallback |
| `infra/helmfile/scripts/generate-runtime-secrets.sh:119` | `loopzero-dev-password` fallback |
| `infra/scripts/production/provision-api-tokens.sh:23` | `loopzero-gitea-dev-2026` fallback |

### Rotation playbook

**`loopzero-db-credentials`** — write the new value into the Secret, `ALTER USER loopzero WITH PASSWORD`
against Postgres, then restart the consumers (they cache the DSN at boot: `loopzero-api`, plus `hydra`
and `kratos` in `loopzero-auth`), and confirm with `task doctor`.

**`gitea-admin`** — use the idempotent re-run path, which refreshes both the Secret and the Gitea
API token: `generate-secrets.sh standalone mgmt` then `provision-api-tokens.sh`. Restart
`argocd-repo-server` afterwards to invalidate its cache.

**`cosign-signing-key`** — quarterly, with a dual-sign window. Procedure in
[`runbooks.md`](runbooks.md) § Cosign signing key rotation.

**Pulumi config secrets** — `pulumi config set --secret <key>` on the target stack, then re-apply.

**Everything else** — re-run the generator script that owns the Secret; each row above names it.

`LOOPZERO_ADMIN_API=true` is a separate defense-in-depth flag: without it, admin endpoints
return 404 even with a valid `X-Service-Auth`. Production sets it via the infra layer;
a standalone Tilt stack must set it explicitly.

## Blocked commands

`kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`,
`git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

## Pre-completion checklist

```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] task quality:gates passes (or quality:diff for a scoped change)
[ ] No internal terms in user-facing output
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes go through Transition()
[ ] CHANGELOG entry under [Unreleased]
```

## Related

- [`runbooks.md`](runbooks.md) — rotation, recovery, upgrade, drain, alert response
- [`architecture.md`](architecture.md) — what the clusters contain and why
