#!/bin/bash
# Render drill helmfile template with stub env vars for snapshot validation.
# In real drill runs, these env vars come from Pulumi stack outputs via
# drill-helmfile-bootstrap.sh. For golden file regression tests we provide
# deterministic stubs so the rendering completes for all charts.
set -euo pipefail
KRATOS_DSN="postgres://kratos:stub@stub:5432/kratos?sslmode=require" \
HYDRA_DSN="postgres://hydra:stub@stub:5432/hydra?sslmode=require" \
GITEA_HOST="stub.example.com" \
GITEA_PORT="5432" \
GITEA_DB="gitea" \
GITEA_USER="gitea" \
GITEA_PASSWORD="stub" \
GITEA_REDIS_HOST="stub.example.com" \
GITEA_REDIS_PORT="6379" \
GITEA_REDIS_PASSWORD="stub" \
MGMT_KUBE_CONTEXT="stub-mgmt" \
BUILD_KUBE_CONTEXT="stub-build" \
RUNTIME_KUBE_CONTEXT="stub-runtime" \
LOOPZERO_HARBOR_REDIS_HOST="stub" \
LOOPZERO_HARBOR_REDIS_PORT="6379" \
LOOPZERO_HARBOR_REDIS_PASSWORD="stub" \
SERVICE_AUTH_SECRET="stub" \
HARBOR_ADMIN_PASSWORD="stub" \
HARBOR_DB_PASSWORD="stub" \
DOCR_REGISTRY_HOST="registry.digitalocean.com/loopzero-drill" \
DOCR_TOKEN="stub" \
helmfile --file infra/helmfile/mgmt.helmfile.yaml.gotmpl -e drill template 2>/dev/null
