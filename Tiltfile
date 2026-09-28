# Tiltfile — LoopZero local development loop
# Builds and deploys API server + TenantController into the k3d mgmt cluster.
# Prerequisites: k3d clusters + Helm dependencies must be running (task setup).
#
# Ownership Model:
# - `task setup` owns infrastructure lifecycle (clusters, Helm charts, CRDs, tokens)
# - `tilt up` owns application lifecycle (API server, TenantController, migrations, dev seed)
# - `tilt down` does NOT affect infrastructure — PostgreSQL, Gitea, MinIO survive
# - To destroy everything: `task clean:all`

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
load("ext://namespace", "namespace_create")

# Ensure loopzero-system namespace exists
namespace_create("loopzero-system")

# The dev loop runs against the core cluster. `allow_k8s_contexts` alone never stopped
# a wrong one — every k3d-* context already counts as local — and Tilt deploys into the
# CURRENT context: left on k3d-loopzero-secrets, it put the platform into the secrets
# cluster (#402). Pin it with `tilt up --context k3d-loopzero-core`.
allow_k8s_contexts("k3d-loopzero-core")
if k8s_context() != "k3d-loopzero-core":
    fail("Tilt must run against k3d-loopzero-core, not %s — start it with: tilt up --context k3d-loopzero-core" % k8s_context())

# ---------------------------------------------------------------------------
# gomod_token — required by api/Dockerfile to fetch private loopzero-contracts
# module (M5 decomposition, 2026-06-19). Without this, loopzero-api build fails
# with `could not read Username for 'https://github.com'`. Canonical SoT:
# `~/.loopzero/gomod-token.txt` mode 0600 (sibling of admin-cli-client-secret.txt
# + cleanroom-unseal.json). Fail-fast pre-flight so operator gets a clear
# error BEFORE Tilt spends ~2min on a build that will fail at step 8/11.
# ---------------------------------------------------------------------------
GOMOD_TOKEN_PATH = os.getenv("HOME") + "/.loopzero/gomod-token.txt"
if not os.path.exists(GOMOD_TOKEN_PATH):
    fail(
        "missing " + GOMOD_TOKEN_PATH + " — required for loopzero-api build " +
        "(private loopzero-contracts module). Generate a GitHub PAT with " +
        "`Contents: read` on getloopscale/loopzero-contracts and save it: " +
        "`echo \"ghp_xxx\" > " + GOMOD_TOKEN_PATH + " && chmod 600 " +
        GOMOD_TOKEN_PATH + "`"
    )

# ---------------------------------------------------------------------------
# Helm dependencies (PostgreSQL, Gitea, MinIO)
# ---------------------------------------------------------------------------
# These are installed via `task infra:install:core` during `task setup`.
# Tilt observes them as existing resources for dependency ordering.
# They are NOT managed by Tilt — `tilt down` will NOT uninstall them.
# This prevents accidental data loss when stopping the dev loop.

# Probe existing Helm releases so Tilt can track readiness without managing lifecycle.
local_resource(
    "postgresql",
    cmd="kubectl --context k3d-loopzero-core -n loopzero-system rollout status statefulset/postgresql --timeout=60s",
    resource_deps=[],
    labels=["infra"],
)

local_resource(
    "gitea",
    cmd="kubectl --context k3d-loopzero-core -n loopzero-system rollout status deployment/gitea --timeout=60s",
    resource_deps=["postgresql"],
    labels=["infra"],
)

local_resource(
    "minio",
    cmd="kubectl --context k3d-loopzero-core -n loopzero-system rollout status deployment/minio --timeout=60s",
    resource_deps=["postgresql"],
    labels=["infra"],
)

# ---------------------------------------------------------------------------
# PodDisruptionBudgets — protect stateful services during node drain
# ---------------------------------------------------------------------------
k8s_yaml([
    "infra/helm/postgresql/templates/pdb.yaml",
    "infra/helm/gitea/templates/pdb.yaml",
    "infra/helm/argocd/templates/pdb.yaml",
])
k8s_resource(
    objects=[
        "postgresql:poddisruptionbudget",
        "gitea:poddisruptionbudget",
        "argocd-server:poddisruptionbudget",
    ],
    new_name="pdbs",
    resource_deps=["postgresql", "gitea"],
)

# ---------------------------------------------------------------------------
# CRDs — must be applied before the operator starts
# ---------------------------------------------------------------------------
k8s_yaml(listdir("operators/config/crd/bases/"))
# 2026-05-13: appsecrets + reconciliationreports CRDs deleted (orphan CRD cleanup,
# §6.B.2 + §6.B.3 per decisions/2026-05-13-orphan-crd-deletion.md). Tiltfile drift
# fixed here — `tilt ci` was failing with "No object identified by the fragment
# appsecrets.loopzero.io:customresourcedefinition could be found" during cleanroom
# Phase 4. Now lists only the 8 wired CRDs.
k8s_resource(
    objects=[
        "clustercredentials.loopzero.io:customresourcedefinition",
        "environments.loopzero.io:customresourcedefinition",
        "exposedroutes.loopzero.io:customresourcedefinition",
        "gitrepositorybindings.loopzero.io:customresourcedefinition",
        "platformbootstraps.loopzero.io:customresourcedefinition",
        "previewenvironments.loopzero.io:customresourcedefinition",
        "tenants.loopzero.io:customresourcedefinition",
        "loopzeroapplications.loopzero.io:customresourcedefinition",
    ],
    new_name="crds",
    resource_deps=[],
)

# ---------------------------------------------------------------------------
# Database migrations — run before API starts
# ---------------------------------------------------------------------------
# Schema bootstrap: still uses psql loop because loopzero-api pod (canonical
# golang-migrate path) is not up yet at this stage. golang-migrate is
# applied AFTER the API is up via `task k3d:heal` H17 (which calls
# infra/scripts/run-migrations.sh) — that backfills schema_migrations
# with the highest version, registering the schema as fully applied.
# See dogfood-2026-05-08-fixes T1.2/T1.3 (ADR D1).
# Plan T3.1 (2026-05-14, F-01 fix): dedicated long-lived port-forward
# for PostgreSQL. Previously db-migrate wrapped kubectl port-forward in
# `trap "kill"` which terminated the PF after migrations completed; edge
# tests (~50min later) found port 15432 dead. By extracting the PF as a
# Tilt local_resource with `serve_cmd`, it stays up for the entire
# `tilt up` session and auto-restarts when postgresql-0 restarts.
local_resource(
    "pg-port-forward",
    serve_cmd="kubectl --context k3d-loopzero-core port-forward -n loopzero-system svc/postgresql 15432:5432",
    resource_deps=["postgresql"],
    auto_init=True,
    allow_parallel=True,
    labels=["loopzero"],
)

# db-migrate runs the canonical migration path, the one `task setup` uses: the
# request/worker roles first (000062 and 000068 grant to them only if they exist),
# then golang-migrate with version tracking, executed inside the Postgres pod. This
# resource used to carry its own copy — a host `psql -f` loop over every *.up.sql
# and a stamp of schema_migrations to version 10 — which failed on a machine
# without psql and would have moved a database at 68 back to 10 (#403).
local_resource(
    "db-migrate",
    cmd="task db:roles && task db:migrate",
    resource_deps=["postgresql"],
    deps=["api/internal/db/migrations"],
)

# ---------------------------------------------------------------------------
# API server — build and deploy
# ---------------------------------------------------------------------------
docker_build(
    "loopzero-api",
    context=".",
    dockerfile="api/Dockerfile",
    # M6 Phase 2 (commit 4984365b 2026-06-17) extracted operators/api as a
    # standalone Go module. api/Dockerfile COPYs operators/api/go.mod,
    # operators/api/go.sum, and operators/api/v1 explicitly. Without
    # adding operators/api/ to this `only=` list, those files are pruned
    # from the build context and BuildKit fails with
    # "/operators/api/go.sum: not found". Cleanroom catch 2026-06-18 —
    # same stale-helper-post-architecture-refactor class as PR #88.
    only=["api/", "pkg/", "go.work", "go.work.sum", "operators/api/", "operators/go.mod", "operators/go.sum", "infra/tests/go.mod", "infra/tests/go.sum", "infra/scripts/"],
    # gomod_token: BuildKit secret mount required by api/Dockerfile step 8/11
    # to fetch the PRIVATE github.com/getloopscale/loopzero-contracts@v0.4.0 module
    # (M5 decomposition). Without secret=..., Docker build fails with
    # `could not read Username for 'https://github.com'`. Cleanroom catch
    # 2026-06-19 — same stale-helper-post-architecture-refactor class as #91.
    secret=["id=gomod_token,src=" + GOMOD_TOKEN_PATH],
)

# T1.1.c (plan 2026-06-11-code-review-remediation-147-findings v1.2) per ADR D1:
# Render loopzero-api ConfigMap+Secret from helm chart BEFORE applying Deployment.
# Without this, the Deployment's envFrom: configMapRef/secretRef references resolve to
# missing resources and Pod fails CrashLoopBackOff. Output written to .tilt-rendered/
# (gitignored) — re-rendered on every values or template edit.
#
# The chart is charts/loopzero-api — the one loopzero-api chart Helmfile and ArgoCD install —
# rendered with infra/tilt/loopzero-api-values.yaml, and only its ConfigMap and Secret
# (both named loopzero-api). `helm dependency build` resolves the local loopzero-common
# library, which the chart's templates include; it needs no network.
# Replaces the rendered file only when its content changed. k8s_yaml below makes that
# file a Tiltfile dependency, and rewriting it on every load reloaded the Tiltfile,
# which rewrote it again — "(Tiltfile)" never left in_progress (#403). The render is
# deterministic, so an unchanged file now stays untouched.
_API_RUNTIME_RENDER = "mkdir -p .tilt-rendered && helm dependency build charts/loopzero-api >/dev/null && helm template loopzero-api charts/loopzero-api -f infra/tilt/loopzero-api-values.yaml -s templates/configmap.yaml -s templates/secret.yaml -n loopzero-system > .tilt-rendered/loopzero-api-runtime.yaml.next && { cmp -s .tilt-rendered/loopzero-api-runtime.yaml.next .tilt-rendered/loopzero-api-runtime.yaml && rm -f .tilt-rendered/loopzero-api-runtime.yaml.next || mv .tilt-rendered/loopzero-api-runtime.yaml.next .tilt-rendered/loopzero-api-runtime.yaml; }"

local_resource(
    "loopzero-api-runtime",
    cmd=_API_RUNTIME_RENDER,
    deps=[
        "infra/tilt/loopzero-api-values.yaml",
        "charts/loopzero-api/values.yaml",
        "charts/loopzero-api/templates/configmap.yaml",
        "charts/loopzero-api/templates/secret.yaml",
    ],
    labels=["loopzero"],
)

# Renders once while the Tiltfile is LOADING, before the k8s_yaml below.
#
# Without this the Tiltfile does not load on a fresh clone, and the error does not point
# at the cause: `.tilt-rendered/` is gitignored, `k8s_yaml` reads the file at the moment
# the Tiltfile is evaluated, and what produces it is the `local_resource` above — which
# only runs AFTER the Tiltfile has loaded. The resource depended on the artifact it
# generates itself. Measured on the app-dev droplet on 2026-08-10, during the full
# rebuild: `tilt up` aborted with
# "open .tilt-rendered/loopzero-api-runtime.yaml: no such file or directory".
#
# The `local_resource` still exists and is not redundant: it re-renders when the
# `deps` change, which is the development loop. This `local()` only guarantees the first render.
local(_API_RUNTIME_RENDER, quiet=True)
# The chart declares no namespace — a Helm release supplies it — so the raw render
# would land in `default`, and the loopzero-api Deployment in loopzero-system then failed with
# `configmap "loopzero-api" not found` (#404). Place every rendered object in loopzero-system.
_api_runtime_objects = decode_yaml_stream(read_file(".tilt-rendered/loopzero-api-runtime.yaml"))
for _obj in _api_runtime_objects:
    _obj["metadata"]["namespace"] = "loopzero-system"
k8s_yaml(encode_yaml_stream(_api_runtime_objects))

k8s_yaml("infra/tilt/api-deployment.yaml")
k8s_resource(
    "loopzero-api",
    # No port_forwards — k3d core-cluster.yaml already maps localhost:3001 → NodePort 30001.
    # Adding port_forwards here would conflict with k3d's loadbalancer binding.
    # loopzero-api-runtime added as dep: Pod needs the ConfigMap+Secret rendered before envFrom can resolve.
    resource_deps=["db-migrate", "postgresql", "loopzero-api-runtime"],
    labels=["loopzero"],
)

# ---------------------------------------------------------------------------
# loopzero-cloud — EXTRACTED to github.com/getloopscale/loopzero-cloud (M4, 2026-06-16).
# ---------------------------------------------------------------------------
# The cloud-API + dashboard now live in the standalone loopzero-cloud repo with their
# own dev loop (which assumes the engine + loopzero-data are running). The engine
# Tiltfile no longer builds/deploys cloud from source. The deployed loopzero-cloud
# SERVICE chart (charts/loopzero-cloud) remains platform infra, sourced from the
# loopzero-cloud repo's published image.

# ---------------------------------------------------------------------------
# Operator binaries — 4 independent deployments (multi-binary split)
# ---------------------------------------------------------------------------

# Group A: Tenant + Environment (heaviest — all provisioners + multi-cluster)
docker_build(
    "loopzero-tenant-operator",
    context=".",
    dockerfile="operators/Dockerfile.tenant-operator",
    only=["operators/", "pkg/", "go.work", "go.work.sum"],
)
k8s_yaml("infra/tilt/tenant-operator-deployment.yaml")
k8s_resource(
    "tenant-operator",
    resource_deps=["crds", "loopzero-api", "gitea", "minio"],
    labels=["loopzero"],
)

# Group B: Application + Exposure (lightest — mgmt cluster only)
docker_build(
    "loopzero-app-operator",
    context=".",
    dockerfile="operators/Dockerfile.app-operator",
    only=["operators/", "pkg/", "go.work", "go.work.sum"],
)
k8s_yaml("infra/tilt/app-operator-deployment.yaml")
k8s_resource(
    "app-operator",
    resource_deps=["crds"],
    labels=["loopzero"],
)

# Group C: Preview (self-contained — runtime cluster + TTL checks)
docker_build(
    "loopzero-preview-operator",
    context=".",
    dockerfile="operators/Dockerfile.preview-operator",
    only=["operators/", "pkg/", "go.work", "go.work.sum"],
)
k8s_yaml("infra/tilt/preview-operator-deployment.yaml")
k8s_resource(
    "preview-operator",
    resource_deps=["crds"],
    labels=["loopzero"],
)

# Group D: Platform infra (ClusterCredential + GitRepoBinding + PlatformBootstrap)
docker_build(
    "loopzero-platform-operator",
    context=".",
    dockerfile="operators/Dockerfile.platform-operator",
    only=["operators/", "pkg/", "go.work", "go.work.sum"],
)
k8s_yaml("infra/tilt/platform-operator-deployment.yaml")
k8s_resource(
    "platform-operator",
    resource_deps=["crds", "gitea"],
    labels=["loopzero"],
)

# ---------------------------------------------------------------------------
# Dev seed data (optional — runs after API is ready)
# ---------------------------------------------------------------------------
local_resource(
    "dev-seed",
    cmd="""
        kubectl --context k3d-loopzero-core port-forward -n loopzero-system svc/postgresql 15432:5432 &
        PF_PID=$!
        trap "kill $PF_PID 2>/dev/null; wait $PF_PID 2>/dev/null" EXIT
        sleep 3
        PGPASSWORD=loopzero-dev-password psql -h localhost -p 15432 -U loopzero -d loopzero <<'SQL'
        INSERT INTO users (id, email, github_id, display_name)
        VALUES ('github-paulohenriquevn', 'paulohenriquevn@gmail.com', 6766298, 'paulohenriquevn')
        ON CONFLICT (id) DO NOTHING;

        INSERT INTO tenants (id, display_name, slug, owner_id)
        VALUES ('acme', 'acme', 'acme', 'github-paulohenriquevn')
        ON CONFLICT (id) DO NOTHING;

        INSERT INTO tenant_members (tenant_id, user_id, role)
        VALUES ('acme', 'github-paulohenriquevn', 'owner')
        ON CONFLICT (tenant_id, user_id) DO NOTHING;
SQL
    """,
    resource_deps=["loopzero-api"],
    auto_init=False,
    labels=["loopzero"],
)
