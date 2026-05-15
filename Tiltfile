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
allow_k8s_contexts("k3d-loopzero-mgmt")

# ---------------------------------------------------------------------------
# Helm dependencies (PostgreSQL, Gitea, MinIO)
# ---------------------------------------------------------------------------
# These are installed via `task infra:install:mgmt` during `task setup`.
# Tilt observes them as existing resources for dependency ordering.
# They are NOT managed by Tilt — `tilt down` will NOT uninstall them.
# This prevents accidental data loss when stopping the dev loop.

# Probe existing Helm releases so Tilt can track readiness without managing lifecycle.
local_resource(
    "postgresql",
    cmd="kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status statefulset/postgresql --timeout=60s",
    resource_deps=[],
    labels=["infra"],
)

local_resource(
    "gitea",
    cmd="kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status deployment/gitea --timeout=60s",
    resource_deps=["postgresql"],
    labels=["infra"],
)

local_resource(
    "minio",
    cmd="kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status deployment/minio --timeout=60s",
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
local_resource(
    "db-migrate",
    cmd="""
        kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/postgresql 15432:5432 &
        PF_PID=$!
        trap "kill $PF_PID 2>/dev/null; wait $PF_PID 2>/dev/null" EXIT
        sleep 3
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
    only=["api/", "pkg/", "go.work", "go.work.sum", "operators/go.mod", "operators/go.sum", "infra/tests/go.mod", "infra/tests/go.sum", "infra/scripts/"],
)

k8s_yaml("infra/tilt/api-deployment.yaml")
k8s_resource(
    "loopzero-api",
    # No port_forwards — k3d mgmt-cluster.yaml already maps localhost:3001 → NodePort 30001.
    # Adding port_forwards here would conflict with k3d's loadbalancer binding.
    resource_deps=["db-migrate", "postgresql"],
    labels=["loopzero"],
)

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
        kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/postgresql 15432:5432 &
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
