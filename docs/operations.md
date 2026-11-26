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
`localhost:15432`, tails `loopruntime-system` logs.
**`task doctor`** checks 4 clusters Ready, critical pods Running, API `/health` 200, JWT
seeded, `schema_migrations` populated, cross-cluster connectivity, registry reachable from
all clusters, ArgoCD repo creds, cell metrics emitted.

### When NOT to re-run setup

| You changed | Do |
|---|---|
| Go code | nothing — `tilt up` picks it up on save |
| a Helm chart (`infra/helm/X/`) | targeted `task infra:install:core`, not a full rebuild |
| a CRD schema (`operators/api/v1/`) | `task crds:install` — operators auto-restart via Tilt |
| a DB migration | `task db:migrate` — loopruntime-api auto-restarts |
| the CLI | it lives in `../loopruntime-cli` since M2. Build there, point at this stack with `LOOPRUNTIME_API_URL=http://localhost:13001` |

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
cd api && go test ./...          # also pkg, operators, cmd/loopruntime-ops
cd api && go test ./internal/scaling/... -run TestResolve -v
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
`LOOPRUNTIME_API_URL`. And the e2e seed defaults to a Postgres URL from an in-process PG that no
longer exists; port-forward `svc/postgresql 15432:5432` and set
`LOOPRUNTIME_E2E_DATABASE_URL=postgres://loopruntime:loopruntime-dev-password@127.0.0.1:15432/loopruntime?sslmode=disable`.

## Quality gates — the CI substitute

GitHub Actions was removed as the pipeline on 2026-05-23. The gates moved to a pre-push hook
that runs **on the pusher's machine and only there** — so a repository with no CI gate has
none for anyone else. The gate inventory lives in `cmd/loopruntime-ops`.

```bash
task quality:build                 # build the gate binary to /tmp/loopruntime-ops — do this FIRST
                                   # every gate exits 0 pass, 1 usage error or a violation,
                                   # 5 when it could not complete (an input it could not read)
/tmp/loopruntime-ops quality --list       # the inventory: NAME / WIRED / SUMMARY — the oracle
/tmp/loopruntime-ops quality <gate>       # run one gate

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

A stray `./loopruntime-ops` at the repo root is a **gitignored local build artifact**, not the gate
binary. It can be arbitrarily stale and is absent in a fresh clone. Always build first;
never read it as the oracle.

**Adding a new `.sh` file is FORBIDDEN by default** (`no-new-bash` gate). New logic belongs
in `cmd/loopruntime-ops/internal/quality/` (Go), `infra/helmfile/` (declarative), or
`infra/tests/e2e/` (Ginkgo).

## Prerequisites

Go 1.26+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, **helm-diff plugin**,
**Helmfile v1.0+**, kubectl, kubeconform, golangci-lint v2.11+, cosign (optional).

## Credentials

`FAIL fast` means the service refuses to start rather than defaulting.

### Local k3d

| Variable | Purpose | If unset |
|---|---|---|
| `LOOPRUNTIME_DB_PASSWORD` | Postgres password, all services | FAIL fast |
| `LOOPRUNTIME_GITEA_ADMIN_PASSWORD` | Gitea admin (org `loopruntime`) | FAIL fast |
| `LOOPRUNTIME_GITEA_TOKEN` | Gitea API token (CLI + operators) | generated by `provision-api-tokens.sh` |
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
| `LOOPRUNTIME_DB_PASSWORD` | required | injected | injected |
| `LOOPRUNTIME_GITEA_*` | required | injected | injected |
| `LOOPRUNTIME_*_OAUTH_*` (GitHub/Google) | optional (auth disabled in dev) | required | required |
| `LOOPRUNTIME_STRIPE_API_KEY` + webhook secret | mock OK | required | required |
| `LOOPRUNTIME_RESEND_API_KEY` | optional | required | required |
| `LOOPRUNTIME_SESSION_TOKEN_SECRET` | generated locally | injected | injected |
| `COSIGN_KEY` + `COSIGN_PASSWORD` | local pair from `generate-build-secrets.sh` | injected | injected |

### Pulumi config secrets

Set with `pulumi config set --secret <key>` per stack. Required across dev / staging / prod / drill:
`digitaloceanToken`, `spacesAccessKeyId`, `spacesSecretAccessKey`, `giteaAdminPassword`,
`minioRootPassword`, `loopruntimeApiDbPassword`, `sessionTokenSecret`, `serviceAuthSecret`.

Required when the matching feature is on: `cfApiToken`, `cfAccountId`, `cfZoneId`, `tunnelSecret`
(Cloudflare); `stripeApiKey`, `stripeWebhookSecret`, `stripePriceProId` (billing); `resendApiKey`
(mail); `githubOAuthClientId`, `googleOAuthClientId` (social login).

### K8s Secrets created at bootstrap

Reconciled by `task bootstrap:standalone` locally, by Pulumi + Helmfile on drill/prod.

| Secret | Namespace | Source | Consumers | Rotation |
|---|---|---|---|---|
| `loopruntime-db-credentials` | loopruntime-system | `generate-secrets.sh:146` | loopruntime-api, loopruntime-cloud, Hydra/Kratos DSN | manual — see playbook |
| `gitea-admin` | loopruntime-system | `generate-secrets.sh:157-158` | Gitea pod, tenant + app operators | Gitea admin UI, then re-run `provision-api-tokens.sh` |
| `loopruntime-api-tokens` | loopruntime-system | `loopruntime-ops seed gitea-token` / `argocd-token` (each writes only its key; ESO adds `cf-api-token`) | loopruntime-api `/health/deep` | re-run `provision-api-tokens.sh` |
| `argocd-creds-gitea` | argocd | `loopruntime-ops seed argocd-gitea-creds` | ArgoCD repo-creds (Gitea HTTPS) | with `gitea-admin` |
| `cluster-cell-0` | argocd | `provision-api-tokens.sh:81` | ArgoCD destination resolution | n/a — kubeconfig embedded |
| `minio-credentials` | argo | `generate-build-secrets.sh:108` | Argo Workflows + BuildKit cache | with the MinIO root |
| `cosign-signing-key` | argo | `generate-build-secrets.sh:87` | the cosign-signer step | **quarterly** |
| `buildkit-registry-config` | argo | `generate-build-secrets.sh:100` | BuildKit `--registry-config` | with the registry creds |
| `cloudflared-tunnel-token` | loopruntime-system | `generate-runtime-secrets.sh:65,74` | cloudflared Deployment | CF Zero Trust |
| `loopruntime-eso-source` | external-secrets | `generate-runtime-secrets.sh:90` | ESO ClusterSecretStore | with the ESO SA token |
| `static-assets-s3` | loopruntime-system | `generate-runtime-secrets.sh:126` | nginx-s3-gateway | with the Spaces creds |
| `loopruntime-registry-pull` | loopruntime-system (workload) | `bootstrap-cluster.sh:76` | every `loopruntime-*` imagePullSecret | `doctl registry kubernetes-manifest` |

**Registries** follow `docs/decisions.md` ("Two image classes, two homes", 2026-09-30): a cluster pulls only from its region's cloud registry — ECR on AWS, read by the dev cluster through the ECR kubelet credential provider — and the DOCR row above predates it. Tenant images stay in their region; GHCR is never a runtime source.

**ESO-managed:** the ClusterCredential kubeconfig Secret in `loopruntime-system` (core) is synced into the
workload cluster as `cell-0.kubeconfig` for loopruntime-api.

### Chart-hardcoded credentials

Dev fallbacks compiled into chart values. Each is a known default that can reach production through
`helmfile -e standalone sync`, which is why they are enumerated rather than left implicit.

| File:Line | Literal |
|---|---|
| `infra/helm/postgresql/values.yaml:19` | `loopruntime-dev-password` |
| `infra/helm/minio/values.yaml:7` | `loopruntime-dev-password` |
| `infra/helm/loki/values.yaml:25` | `loopruntime-dev-password` |
| `infra/helm/monitoring/values.yaml:57` | `loopruntime-dev-password` (Grafana admin) |
| `infra/helm/gitea/values.yaml:7` | `loopruntime-gitea-dev-2026` |
| `infra/helm/gitea/values.yaml:19` | `loopruntime-dev-password` (PASSWD env) |
| `infra/helm/ory-hydra/values.yaml.gotmpl:18` | `loopruntime-dev-password` (DSN inline) |
| `infra/helm/ory-kratos/values.yaml.gotmpl:14` | `loopruntime-dev-password` (DSN inline) |
| `infra/helmfile/scripts/generate-secrets.sh:140` | `loopruntime-dev-password` fallback |
| `infra/helmfile/scripts/generate-runtime-secrets.sh:119` | `loopruntime-dev-password` fallback |

### Rotation playbook

**`loopruntime-db-credentials`** — write the new value into the Secret, `ALTER USER loopruntime WITH PASSWORD`
against Postgres, then restart the consumers (they cache the DSN at boot: `loopruntime-api`, plus `hydra`
and `kratos` in `loopruntime-auth`), and confirm with `task doctor`.

**`gitea-admin`** — use the idempotent re-run path, which refreshes both the Secret and the Gitea
API token: `generate-secrets.sh standalone mgmt` then `provision-api-tokens.sh`. Restart
`argocd-repo-server` afterwards to invalidate its cache.

**`cosign-signing-key`** — quarterly, with a dual-sign window. Procedure in
[`runbooks.md`](runbooks.md) § Cosign signing key rotation.

**Pulumi config secrets** — `pulumi config set --secret <key>` on the target stack, then re-apply.

**Everything else** — re-run the generator script that owns the Secret; each row above names it.

`LOOPRUNTIME_ADMIN_API=true` is a separate defense-in-depth flag: without it, admin endpoints
return 404 even with a valid `X-Service-Auth`. Production sets it via the infra layer;
a standalone Tilt stack must set it explicitly.

## Troubleshooting

Every entry here is a failure someone actually hit, and each one looks like a different
problem than it is. That is why they are written down: the symptom points at the wrong
cause in all of them.

### `go build ./...` from the repository root says nothing is wrong

There is **no `go.mod` at the root**. `go.work` unions nine modules, so a root-level
build walks almost no code and exits 0 over a broken tree. Build per module, or use the
Taskfile:

```bash
cd api && go build ./...          # and pkg, operators, operators/api, cmd/loopruntime-ops, …
task test:unit                    # the whole set
```

### The build fails to resolve `../loopruntime-contracts`

`go.work` unions a **sibling repository**. Absent, the workspace does not build. Either
check it out beside this one, or build the six modules that do not need it with the
workspace off:

```bash
GOWORK=off go build ./...         # works in cmd/loopruntime-ops, operators, operators/api, tools/…
```

`api` and `pkg` are the two that genuinely require it.

### The e2e suite reports SUCCESS having run zero specs

`BeforeSuite` probes `<apiURL>/api/v1/health` with a 5s timeout and **skips the whole
suite** when it fails — 132 specs skipped reads as green, with no FAIL anywhere. The
default `apiURL()` is `http://localhost:3001`, the Tilt port-forward. Against a k3d
deployment, loopruntime-api is on host port 13001:

```bash
LOOPRUNTIME_API_URL=http://localhost:13001 go test ./e2e/... -v
```

If a suite finishes suspiciously fast, count the specs that ran before believing it.

### The suite times out at 60 minutes although `-timeout 120m` was passed

Ginkgo v2 keeps **its own** suite timeout, default 60 minutes, independent of
`go test -timeout`. Pass both:

```bash
go test ./e2e/... -v -timeout 120m -args -ginkgo.timeout=2h
```

The `-args` separator is required for the flag to reach the test binary.

### Seeding fails with `password authentication failed for user "loopruntime"`

The default in `infra/scripts/seed/seed.go` is legacy from an in-process Postgres that
no longer exists. Port-forward first, then point at it with the deterministic standalone
password:

```bash
kubectl --context k3d-loopruntime-core -n loopruntime-system port-forward svc/postgresql 15432:5432 &
export LOOPRUNTIME_E2E_DATABASE_URL='postgres://loopruntime:loopruntime-dev-password@127.0.0.1:15432/loopruntime?sslmode=disable'
```

### A quality gate exits 5 on a file under `.claude/`

`.claude/` is the installed tooling kit and is **not versioned**, so anything a gate
reads from it is absent in every clone and on every runner. Gates treat a missing
allowlist as *no exemptions declared* — the strictest reading — rather than as a fatal
error. If one still exits 5 on such a path, that is the bug, not your checkout.

### A workflow's pre-push gate refuses and names no failing check

`.git/hooks/pre-push` runs `task quality:gates` before anything leaves the machine. Its
escapes are deliberate and documented in its own header: `git push --no-verify`, or
`LOOPRUNTIME_SKIP_PREPUSH=1 git push`. Use them to see the push itself fail rather than the
gate — then fix what the gate found.

The hook this replaced parsed each push command, and 26 Go tests covered it — push via
`git -C`, `--dry-run` elsewhere in a compound command, pushes in subshells. They were
removed with it on 2026-09-16. The pre-push hook receives no command to parse, so that
class of bypass cannot occur in the same form; nothing tests that, and nothing yet
asserts the hook is installed and armed on a given machine. CI's own guard is
`.github/scripts/assert_delivery_gate_armed.py`, which first asks whether CI gates the
pull request at all.

## Local-prod divergence matrix (LP-NNN)

Rule 10 is *local equals production*. Where the local k3d stack cannot match production,
the code says so with a `LOCAL ONLY (LP-NNN)` comment and the divergence is recorded here.
`loopruntime-ops quality divergence-matrix` holds the two together: a marker whose id has no row
fails, and an `OPEN` row whose marker is gone fails. To retire a divergence, delete the
marker and set its row to `RESOLVED` in the same commit.

| LP-NNN | Status | Code site(s) | What diverges | Why local needs it | Production behaviour |
|---|---|---|---|---|---|
| LP-001 | OPEN | `api/internal/builds/argo/syft.go`, `infra/helm/cert-manager/templates/self-signed-issuer.yaml` | TLS — the local registry is HTTP-only, and cert-manager `letsencrypt-prod` cannot issue for `*.loopruntime.local` (no public DNS for the ACME challenge) | k3d uses private IPs and no public hostname; a self-signed CA issuer fills the in-cluster TLS gap | CF-managed TLS at the edge; the registry is HTTPS and pulls are cosign-verified |
| LP-002 | OPEN | `infra/k3d/cell-1-cluster.yaml` | Port mapping — each k3d cluster publishes its ingress on a distinct local port | every k3d cluster shares the host loopback and cannot bind the same port | each cell has its own DNS name and serves HTTPS on 443 with no port translation |
| LP-003 | OPEN | `api/internal/builds/argo/cosign.go` | Cosign keyless signing — k3d service-account tokens are not accepted by the public Fulcio OIDC validator, so local builds sign with a keyed dev key | a local cluster has no public CI OIDC token | keyless signing with the CI workflow's OIDC identity, verified against Fulcio and Rekor |

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
