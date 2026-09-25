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

# Use the mgmt cluster context
allow_k8s_contexts("k3d-loopzero-core")

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

# db-migrate now relies on the long-lived PF (no inline kubectl).
# T1.2/T1.3 backfill semantics preserved (see ADR D1 of
# dogfood-2026-05-08-fixes).
local_resource(
    "db-migrate",
    cmd="""
        # Wait for pg-port-forward to be ready (up to 30s).
        for i in $(seq 1 30); do
            if nc -z localhost 15432 2>/dev/null; then break; fi
            sleep 1
        done
        for f in api/internal/db/migrations/*.up.sql; do
            PGPASSWORD=loopzero-dev-password psql -h localhost -p 15432 -U loopzero -d loopzero -f "$f"
        done
        # Pre-create schema_migrations so subsequent golang-migrate `up`
        # detects the existing schema correctly (otherwise migrate would
        # try to re-apply 1..10 from scratch).
        PGPASSWORD=loopzero-dev-password psql -h localhost -p 15432 -U loopzero -d loopzero -v ON_ERROR_STOP=1 -c "
            CREATE TABLE IF NOT EXISTS schema_migrations (
                version BIGINT NOT NULL PRIMARY KEY,
                dirty BOOLEAN NOT NULL
            );
            INSERT INTO schema_migrations (version, dirty)
            SELECT MAX(v.version), false FROM (VALUES (1),(2),(4),(5),(6),(7),(8),(9),(10)) AS v(version)
            ON CONFLICT (version) DO UPDATE SET dirty = false;
        "
    """,
    resource_deps=["postgresql", "pg-port-forward"],
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
# (gitignored) — re-rendered on every values-dev.yaml or template edit.
_API_RUNTIME_RENDER = "mkdir -p .tilt-rendered && helm template loopzero-api infra/helm/loopzero-api -f infra/helm/loopzero-api/values-dev.yaml -s templates/configmap-runtime.yaml -s templates/secret-runtime.yaml -n loopzero-system > .tilt-rendered/loopzero-api-runtime.yaml"

local_resource(
    "loopzero-api-runtime",
    cmd=_API_RUNTIME_RENDER,
    deps=[
        "infra/helm/loopzero-api/values-dev.yaml",
        "infra/helm/loopzero-api/templates/configmap-runtime.yaml",
        "infra/helm/loopzero-api/templates/secret-runtime.yaml",
    ],
    labels=["loopzero"],
)

# Renderiza uma vez durante o CARREGAMENTO do Tiltfile, antes do k8s_yaml abaixo.
#
# Sem isto o Tiltfile nao carrega num clone novo, e o erro nao aponta para a causa:
# `.tilt-rendered/` e gitignored, `k8s_yaml` le o arquivo no momento em que o Tiltfile
# e avaliado, e quem o produz e o `local_resource` acima — que so roda DEPOIS que o
# Tiltfile carregou. O recurso dependia do artefato que ele mesmo gera. Medido no
# droplet app-dev em 2026-08-10, durante a reconstrucao total: `tilt up` abortou com
# "open .tilt-rendered/loopzero-api-runtime.yaml: no such file or directory".
#
# O `local_resource` continua existindo e nao e redundante: ele re-renderiza quando os
# `deps` mudam, que e o laco de desenvolvimento. Este `local()` so garante o primeiro.
local(_API_RUNTIME_RENDER, quiet=True)
k8s_yaml(".tilt-rendered/loopzero-api-runtime.yaml")

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
