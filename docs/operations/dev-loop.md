# LoopZero Dev Loop — Canonical Guide

This is the **canonical** developer workflow for local LoopZero development.
Adopted via plan `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md` v1.3
Phase 3 (T3.1+T3.2).

---

## TL;DR

```bash
task setup    # one-shot: clusters + helm releases + CRDs + tokens (15-25min)
tilt up       # dev loop: hot reload of API/operators/migrations
task doctor   # health check: 38+ checks across clusters/pods/tokens
```

`task setup` builds infrastructure. `tilt up` is the day-to-day dev loop.

---

## What `task setup` does (one-shot)

Per Phase 2 of plan 2026-05-22 (T2.3), `task setup` delegates to **Helmfile**
as the canonical L2 orchestrator (no more bare `helm upgrade --install`):

1. `registry:create` — k3d registry container
2. `clusters:create` — 4 k3d clusters (mgmt + build + workload + runtime-2)
3. `images:build` — pre-build LoopZero images (loopzero-api, operators, dashboard)
4. `infra:install` (mgmt + build + workload) — `helmfile sync` per cluster:
   - **mgmt cluster:** postgresql, gitea-redis, gitea, minio, ory-kratos,
     ory-hydra, argocd, prometheus, grafana, opencost (T7.2), velero,
     loopzero-api, network-policies
   - **build cluster:** argo-workflows, loopzero-build-cwts, loki, promtail
   - **workload cluster:** cert-manager, traefik, keda, argo-rollouts,
     policy-controller, admission-policies, cloudflared, static-assets
5. `networking:setup` — Cloudflare Tunnel (if configured)
6. `crds:install` — LoopZero CRDs (Tenant, LoopZeroApplication, etc.)
7. `db:migrate` — golang-migrate forward-only migrations
8. `dev:seed` — bootstrap user JWT + tenant CR

Runtime: **15-25min** on cold start (Linux), longer on Mac due to Docker Desktop.

## What `tilt up` does (dev loop)

`tilt up` is the **hot-reload** loop:

- Watches Go source in `api/`, `operators/*` — rebuilds + reloads on change
- Watches TypeScript in `cli/` — auto-rebuilds binary
- Port-forwards API to `localhost:13001`
- Port-forwards Postgres to `localhost:15432`
- Tails logs from loopzero-system pods

Typical session: open Tiltfile UI at http://localhost:10350 to see services
+ recent rebuilds + log streams.

## What `task doctor` does (health check)

Validates 38+ invariants across the platform:

- 4 k3d clusters Ready
- All critical pods Running in mgmt/build/workload namespaces
- API `/health` responds 200
- JWT seeded (`~/.loopzero/local-dogfood-token.txt`)
- DB schema_migrations populated
- Cross-cluster connectivity (build → mgmt Gitea)
- Registry accessible from all clusters
- ArgoCD repo creds configured
- Cell metrics emitted

Baseline: `PASS=N FAIL=0` where N depends on enabled features (~38).

---

## Common operations

### Fresh start (cleanroom)

```bash
task clean:all   # destroy all clusters + registry (~3-5min)
task setup       # rebuild (15-25min)
tilt up          # enter dev loop
```

Or use the `/cleanroom` skill: `~30-45min` total automated.

### After mid-day source change

Tilt auto-reloads. Just save the file and watch the Tiltfile UI for the rebuild.
No manual `task` invocations needed.

### After Helm chart change (`infra/helm/X/`)

```bash
# Re-run helmfile sync for the affected release
cd infra/helmfile
helmfile -e standalone-multi --kube-context k3d-loopzero-mgmt --selector name=X sync

# OR easier: rebuild from setup state
task infra:install:mgmt
```

### After CRD schema change (`operators/api/v1alpha1/`)

```bash
task crds:install    # apply updated CRDs
# Operators auto-restart via tilt
```

### After DB migration added (`api/migrations/00NNNN_X.up.sql`)

```bash
task db:migrate
# loopzero-api auto-restarts (Tilt picks up DB connection refresh)
```

---

## When NOT to use `task setup`

- **Just changing Go code:** `tilt up` already running? Save and watch. No need to re-setup.
- **Just changing TS in CLI:** save → tilt rebuilds binary → use `./cli/bin/loopzero` directly.
- **Want to test a specific Helm chart:** use targeted helmfile sync (see above).

`task setup` is for: cold start, recovery from broken state, fresh cleanroom validation.

---

## Local sqlc Workflow (Phase 0 of `2026-05-29-sqlc-migration-plan.md` v1.2)

The sqlc migration introduces a codegen step in `api/internal/db/`. Local workflow:

**Install (pinned per plan D1 + EC-1):**

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
```

`task doctor` checks for this binary at the pinned version under `=== Required Tools ===`. A version mismatch or absent binary produces a FAIL line — install before continuing.

**Regenerate after editing `.sql` files:**

```bash
task db:generate                # regenerate api/internal/db/generated/*.go from queries/*.sql
task db:verify-sync             # pkg/quality/sqlcsync gate: assert committed generated/ matches a fresh regen (CI runs this too)
```

**Re-render schema after applying a new migration:**

```bash
task db:render-schema           # spins ephemeral PostgreSQL 16, replays golang-migrate up, writes api/internal/db/schema.gen.sql
git diff api/internal/db/schema.gen.sql   # MUST be the only change; if other diffs appear → render is non-deterministic
git add api/internal/db/schema.gen.sql
```

**Authoritative pins (any drift = CI red):**

- sqlc CLI: `v1.31.1` (pinned in `taskfiles/dev.yml` Required Tools check)
- pgx driver: `pgx/v5` (pinned in `api/internal/db/sqlc.yaml` `gen.go.sql_package`)
- PostgreSQL: `16` (pinned in `infra/scripts/ci/render-schema-gen-sql.sh` for render replay; matches DOKS managed PG)
- golang-migrate: pinned in both CI render script AND `infra/scripts/production/run-migrations.sh` (version mismatch = render diff false positive)

**When to NOT re-render `schema.gen.sql`:**

- Editing only `queries/*.sql` (no schema change) → run `task db:generate` only
- Editing only Go consumer code → no codegen action needed
- Editing only tests → no codegen action needed

`schema.gen.sql` re-renders ONLY when a new `.up.sql` migration lands.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `task setup` fails at `infra:install:mgmt` step | LOOPZERO_DB_PASSWORD not in `loopzero-db-credentials` Secret | Run `bash infra/helmfile/scripts/generate-secrets.sh standalone-multi mgmt` first |
| `task doctor` reports `[FAIL] sqlc (v1.31.1)` | sqlc CLI absent or wrong version | `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` |
| `task db:verify-sync` red on freshly-pulled branch | Locally-generated `generated/` diverges from committed (sqlc version drift) | Verify `sqlc version` returns `v1.31.1`; run `task db:generate` and commit any diff |
| `task db:render-schema` produces non-empty diff after fresh run | Migration changed OR render non-determinism | If migration changed: `git add api/internal/db/schema.gen.sql && git commit`. If no migration change: render script is non-deterministic — see ADR-2 §"Reproducibility" |
| Gitea pod CrashLoopBackOff `password authentication failed` | Wrong PASSWD in gitea chart values | Should be FIXED by T2.3 rewrite (helmfile chart values reference Secret) |
| Kratos/Hydra render error `unexpected \\ in operand` | Old YAML escape `\"` | Should be FIXED by T2.2 (single-quoted YAML); if regression, see EC-02 docs |
| Mac contributor: `base64 -d` unknown flag | macOS BSD vs GNU difference | FIXED by T2.3.b (uses `kubectl -o go-template` or `openssl base64 -d -A`) |
| `task doctor` PASS=0 FAIL=N | clusters not Ready OR API not booted | Wait 60s, re-run; check `tilt up` Tiltfile UI for build errors |
| Port 13001 already in use | Previous `tilt up` not cleaned up | `pkill -f "tilt up"` then re-start |

---

## Phase 3 plan reference

This document materializes T3.1 of `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md` v1.3.

Key cross-references:
- T2.3 (Taskfile delegation to Helmfile): `docs/scripts-audit/2026-05-22-helmfile-mapping.md`
- T3.2 deprecation notice: see `Taskfile.yml` `setup:` task desc
- Plan dependency graph: Phase 3 follows Phase 2 (cleanroom unblocked)
