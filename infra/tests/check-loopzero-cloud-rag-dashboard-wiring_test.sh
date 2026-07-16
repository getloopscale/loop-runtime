#!/usr/bin/env bash
# T1+T2 hermetic gate — loopzero-cloud dev-public MUST wire RAG dashboard durably.
#
# Root cause (delegated brief 2026-07-15 DELEGATION-M7-rag-ui.md):
#   `RAG_BASE_URL` was empty on loopzero-cloud pod in dev-public → broker nil →
#   mountRagDashboard (cmd/main.go:998) skipped → `/v1/dashboard/rag/*` 404.
#
# Contract asserted by this test (helmfile-templated dev-public Deployment):
#   - env RAG_BASE_URL is set to a non-empty http(s):// URL
#   - env RAG_PLATFORM_ADMIN_KEY is sourced via valueFrom.secretKeyRef
#     (name=loopzero-rag-admin-key, optional=true)
#   - ExternalSecret template file exists in the chart
#   - Rendered ExternalSecret targets Secret loopzero-rag-admin-key, remoteRef
#     key=loopzero/cloud property=rag_platform_admin_key, creationPolicy=Owner
#
# Runs against `helmfile template` output — hermetic, no live cluster required.
# Pure orchestration: helmfile + yq (canonical toolchain, per project deps).

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "=== Rendering helmfile dev-public loopzero-cloud... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
  --selector name=loopzero-cloud \
  template > "$TMP/rendered.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored"; cat "$TMP/err.log"; exit 1
  }

fail=0

pass() { echo "PASS — $1"; }
fail_() { echo "FAIL — $1"; fail=$((fail + 1)); }

# --- A1: RAG_BASE_URL env is a non-empty http(s):// URL on the Deployment ---
BASE_URL=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "loopzero-cloud")
  | .env[]
  | select(.name == "RAG_BASE_URL")
  | .value) // ""' "$TMP/rendered.yaml")

case "$BASE_URL" in
  http://*|https://*) pass "RAG_BASE_URL=$BASE_URL" ;;
  "") fail_ "RAG_BASE_URL env absent from Deployment/loopzero-cloud (or empty)" ;;
  *)  fail_ "RAG_BASE_URL value not http(s):// URL (got '$BASE_URL')" ;;
esac

# --- A2: RAG_PLATFORM_ADMIN_KEY via valueFrom.secretKeyRef name=loopzero-rag-admin-key ---
SR_NAME=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "loopzero-cloud")
  | .env[]
  | select(.name == "RAG_PLATFORM_ADMIN_KEY")
  | .valueFrom.secretKeyRef.name) // ""' "$TMP/rendered.yaml")
SR_KEY=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "loopzero-cloud")
  | .env[]
  | select(.name == "RAG_PLATFORM_ADMIN_KEY")
  | .valueFrom.secretKeyRef.key) // ""' "$TMP/rendered.yaml")
SR_OPT=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "loopzero-cloud")
  | .env[]
  | select(.name == "RAG_PLATFORM_ADMIN_KEY")
  | .valueFrom.secretKeyRef.optional) // false' "$TMP/rendered.yaml")

if [ "$SR_NAME" = "loopzero-rag-admin-key" ] && [ "$SR_KEY" = "RAG_PLATFORM_ADMIN_KEY" ] && [ "$SR_OPT" = "true" ]; then
  pass "RAG_PLATFORM_ADMIN_KEY sourced via secretKeyRef name=$SR_NAME key=$SR_KEY optional=true"
else
  fail_ "RAG_PLATFORM_ADMIN_KEY wiring mismatch (name='$SR_NAME' key='$SR_KEY' optional='$SR_OPT'; expected loopzero-rag-admin-key/RAG_PLATFORM_ADMIN_KEY/true)"
fi

# --- A3: ExternalSecret template file present in chart ---
TPL="infra/helm/loopzero-cloud/templates/externalsecret-rag-admin-key.yaml"
if [ -f "$TPL" ]; then
  pass "ExternalSecret template present at $TPL"
else
  fail_ "ExternalSecret template missing at $TPL"
fi

# --- A4: Rendered ExternalSecret shape ---
ES_TARGET=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-rag-admin-key")
  | .spec.target.name) // ""' "$TMP/rendered.yaml")
ES_KEY_PATH=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-rag-admin-key")
  | .spec.data[]
  | select(.secretKey == "RAG_PLATFORM_ADMIN_KEY")
  | .remoteRef.key) // ""' "$TMP/rendered.yaml")
ES_PROP=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-rag-admin-key")
  | .spec.data[]
  | select(.secretKey == "RAG_PLATFORM_ADMIN_KEY")
  | .remoteRef.property) // ""' "$TMP/rendered.yaml")
ES_POLICY=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-rag-admin-key")
  | .spec.target.creationPolicy) // ""' "$TMP/rendered.yaml")

if [ -z "$ES_TARGET" ]; then
  fail_ "ExternalSecret/loopzero-rag-admin-key not rendered (rag.externalSecretEnabled off?)"
else
  [ "$ES_TARGET" = "loopzero-rag-admin-key" ] \
    && pass "ExternalSecret target.name=loopzero-rag-admin-key" \
    || fail_ "ExternalSecret target.name='$ES_TARGET' (expected loopzero-rag-admin-key)"
  [ "$ES_KEY_PATH" = "loopzero/cloud" ] && [ "$ES_PROP" = "rag_platform_admin_key" ] \
    && pass "ExternalSecret remoteRef key=loopzero/cloud property=rag_platform_admin_key" \
    || fail_ "ExternalSecret remoteRef mismatch (key='$ES_KEY_PATH' property='$ES_PROP')"
  [ "$ES_POLICY" = "Owner" ] \
    && pass "ExternalSecret target.creationPolicy=Owner (anti-orphan)" \
    || fail_ "ExternalSecret creationPolicy='$ES_POLICY' (expected Owner)"
fi

if [ $fail -gt 0 ]; then
  echo
  echo "=== RED: $fail assertion(s) failed ==="
  exit 1
fi
echo
echo "=== GREEN: loopzero-cloud dev-public wires RAG dashboard durably ==="
