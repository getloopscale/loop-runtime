# Tiltfile — LoopZero local development loop
# Builds and deploys API server + TenantController into the k3d mgmt cluster.
# Prerequisites: k3d clusters + Helm dependencies must be running (task setup).

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
load("ext://helm_resource", "helm_resource", "helm_repo")
load("ext://namespace", "namespace_create")

# Ensure loopzero-system namespace exists
namespace_create("loopzero-system")

# Use the mgmt cluster context
allow_k8s_contexts("k3d-loopzero-mgmt")

# ---------------------------------------------------------------------------
# Helm dependencies (PostgreSQL, Gitea, MinIO)
# ---------------------------------------------------------------------------
# These are installed via `task infra:install:mgmt` during `task setup`.
# Tilt watches them as existing resources so we can set up dependency ordering.
# If you prefer Tilt to manage Helm installs directly, uncomment the helm_resource blocks.

helm_repo("bitnami", "https://charts.bitnami.com/bitnami")
helm_repo("gitea-charts", "https://dl.gitea.com/charts/")
helm_repo("minio-charts", "https://charts.min.io/")

helm_resource(
    "postgresql",
    "bitnami/postgresql",
    namespace="loopzero-system",
    flags=[
        "-f", "infra/helm/postgresql/values.yaml",
        "--wait",
        "--timeout", "3m",
    ],
    resource_deps=[],
)

helm_resource(
    "gitea",
    "gitea-charts/gitea",
    namespace="loopzero-system",
    flags=[
        "-f", "infra/helm/gitea/values.yaml",
        "--wait",
        "--timeout", "3m",
    ],
    resource_deps=["postgresql"],
)

helm_resource(
    "minio",
    "minio-charts/minio",
    namespace="loopzero-system",
    flags=[
        "-f", "infra/helm/minio/values.yaml",
        "--wait",
        "--timeout", "3m",
    ],
    resource_deps=["postgresql"],
)

# ---------------------------------------------------------------------------
# CRDs — must be applied before the operator starts
# ---------------------------------------------------------------------------
k8s_yaml(listdir("operators/config/crd/bases/"))
k8s_resource(
    objects=[
        "tenants.loopzero.io:customresourcedefinition",
        "appsecrets.loopzero.io:customresourcedefinition",
        "clustercredentials.loopzero.io:customresourcedefinition",
        "previewenvironments.loopzero.io:customresourcedefinition",
        "reconciliationreports.loopzero.io:customresourcedefinition",
    ],
    new_name="crds",
    resource_deps=[],
)

# ---------------------------------------------------------------------------
# Database migrations — run before API starts
# ---------------------------------------------------------------------------
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
    port_forwards=["3001:3001"],
    resource_deps=["db-migrate", "postgresql"],
    labels=["loopzero"],
)

# ---------------------------------------------------------------------------
# TenantController operator — build and deploy
# ---------------------------------------------------------------------------
docker_build(
    "tenant-controller",
    context=".",
    dockerfile="operators/Dockerfile",
    only=["operators/", "pkg/", "go.work", "go.work.sum", "api/go.mod", "api/go.sum", "infra/tests/go.mod", "infra/tests/go.sum", "infra/scripts/"],
)

k8s_yaml("infra/tilt/operator-deployment.yaml")
k8s_resource(
    "tenant-controller",
    resource_deps=["crds", "loopzero-api", "gitea", "minio"],
    labels=["loopzero"],
)

# ---------------------------------------------------------------------------
# Dev seed data (optional — runs after API is ready)
# ---------------------------------------------------------------------------
local_resource(
    "dev-seed",
    cmd="""
        kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/postgresql 15433:5432 &
        PF_PID=$!
        trap "kill $PF_PID 2>/dev/null; wait $PF_PID 2>/dev/null" EXIT
        sleep 3
        PGPASSWORD=loopzero-dev-password psql -h localhost -p 15433 -U loopzero -d loopzero <<'SQL'
        INSERT INTO users (id, email, github_id, display_name)
        VALUES ('github-paulohenriquevn', 'paulohenriquevn@gmail.com', 6766298, 'paulohenriquevn')
        ON CONFLICT (id) DO NOTHING;

        INSERT INTO tenants (id, display_name, plan, owner_id)
        VALUES ('acme', 'acme', 'starter', 'github-paulohenriquevn')
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
