#!/usr/bin/env bash
# Hermetic gate — loopzero-cloud dev-public MUST wire the loopzero-lens dashboard durably.
#
# Mirrors check-loopzero-cloud-rag-dashboard-wiring_test.sh (the RAG reference,
# shipped v0.68.0). Same class of durable-wiring gap: without LENS_BASE_URL the
# loopzero-lens Model-B broker in cmd/main.go stays nil, the read-only (M41) +
# management (M44) `/v1/dashboard/lens/*` BFF routes are skipped, and the
# dashboard lens surface (app-dev.loopzero.example/lens) breaks. A volatile
# `kubectl set env` would evaporate on the next `helmfile sync` — this gate
# locks the wiring into the chart.
#
# Contract asserted (helmfile-templated dev-public Deployment):
#   - env LENS_BASE_URL is set to a non-empty http(s):// URL
#   - env LENS_PLATFORM_ADMIN_KEY is sourced via valueFrom.secretKeyRef
#     (name=loopzero-lens-admin-key, key=LENS_PLATFORM_ADMIN_KEY, optional=true)
#   - ExternalSecret template file exists in the chart
#   - Rendered ExternalSecret targets Secret loopzero-lens-admin-key, remoteRef
#     key=loopzero/cloud property=lens_platform_admin_key, creationPolicy=Owner
#
# Runs against `helmfile template` output — hermetic, no live cluster required.
# Pure orchestration: helmfile + yq (canonical toolchain, per project deps).

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "=== Rendering helmfile dev-public loopzero-cloud... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/core.helmfile.yaml.gotmpl \
  --selector name=loopzero-cloud \
  template > "$TMP/rendered.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored"; cat "$TMP/err.log"; exit 1
  }

fail=0

pass() { echo "PASS — $1"; }
fail_() { echo "FAIL — $1"; fail=$((fail + 1)); }

# --- A1: LENS_BASE_URL env is a non-empty http(s):// URL on the Deployment ---
BASE_URL=$(yq eval-all '(select(.kind == "ConfigMap" and .metadata.name == "loopzero-cloud")
  | .data.LENS_BASE_URL) // ""' "$TMP/rendered.yaml")

case "$BASE_URL" in
  http://*|https://*) pass "LENS_BASE_URL=$BASE_URL" ;;
  "") fail_ "LENS_BASE_URL absent from ConfigMap/loopzero-cloud (or empty)" ;;
  *)  fail_ "LENS_BASE_URL value not http(s):// URL (got '$BASE_URL')" ;;
esac

# --- A2: LENS_PLATFORM_ADMIN_KEY via valueFrom.secretKeyRef name=loopzero-lens-admin-key ---
SR_NAME=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "cloud")
  | .env[]
  | select(.name == "LENS_PLATFORM_ADMIN_KEY")
  | .valueFrom.secretKeyRef.name) // ""' "$TMP/rendered.yaml")
SR_KEY=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "cloud")
  | .env[]
  | select(.name == "LENS_PLATFORM_ADMIN_KEY")
  | .valueFrom.secretKeyRef.key) // ""' "$TMP/rendered.yaml")
SR_OPT=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "cloud")
  | .env[]
  | select(.name == "LENS_PLATFORM_ADMIN_KEY")
  | .valueFrom.secretKeyRef.optional) // false' "$TMP/rendered.yaml")

if [ "$SR_NAME" = "loopzero-lens-admin-key" ] && [ "$SR_KEY" = "LENS_PLATFORM_ADMIN_KEY" ] && [ "$SR_OPT" = "true" ]; then
  pass "LENS_PLATFORM_ADMIN_KEY sourced via secretKeyRef name=$SR_NAME key=$SR_KEY optional=true"
else
  fail_ "LENS_PLATFORM_ADMIN_KEY wiring mismatch (name='$SR_NAME' key='$SR_KEY' optional='$SR_OPT'; expected loopzero-lens-admin-key/LENS_PLATFORM_ADMIN_KEY/true)"
fi

# --- A3: ExternalSecret template file present in chart ---
TPL="charts/loopzero-cloud/templates/externalsecret-admin-keys.yaml"
if [ -f "$TPL" ]; then
  pass "ExternalSecret template present at $TPL"
else
  fail_ "ExternalSecret template missing at $TPL"
fi

# --- A4: Rendered ExternalSecret shape ---
ES_TARGET=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-lens-admin-key")
  | .spec.target.name) // ""' "$TMP/rendered.yaml")
ES_KEY_PATH=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-lens-admin-key")
  | .spec.data[]
  | select(.secretKey == "LENS_PLATFORM_ADMIN_KEY")
  | .remoteRef.key) // ""' "$TMP/rendered.yaml")
ES_PROP=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-lens-admin-key")
  | .spec.data[]
  | select(.secretKey == "LENS_PLATFORM_ADMIN_KEY")
  | .remoteRef.property) // ""' "$TMP/rendered.yaml")
ES_POLICY=$(yq eval-all '(select(.kind == "ExternalSecret" and .metadata.name == "loopzero-lens-admin-key")
  | .spec.target.creationPolicy) // ""' "$TMP/rendered.yaml")

if [ -z "$ES_TARGET" ]; then
  fail_ "ExternalSecret/loopzero-lens-admin-key not rendered (lens.externalSecretEnabled off?)"
else
  [ "$ES_TARGET" = "loopzero-lens-admin-key" ] \
    && pass "ExternalSecret target.name=loopzero-lens-admin-key" \
    || fail_ "ExternalSecret target.name='$ES_TARGET' (expected loopzero-lens-admin-key)"
  [ "$ES_KEY_PATH" = "loopzero/cloud" ] && [ "$ES_PROP" = "lens_platform_admin_key" ] \
    && pass "ExternalSecret remoteRef key=loopzero/cloud property=lens_platform_admin_key" \
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
echo "=== GREEN: loopzero-cloud dev-public wires loopzero-lens dashboard durably ==="
