#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP iter 11 — schema-validate helmfile-rendered output.
#
# Renders every helmfile of the dev environment (core, build, cell) and
# validates each rendered object against its K8s schema via kubeconform.
# Catches:
#   - Deprecated apiVersion (e.g., policy/v1beta1 in PDBs)
#   - Invalid spec values (additionalProperties: false strict mode)
#   - Schema drift between chart upgrades
#
# Uses local CRD schemas at infra/kubeconform-schemas/ for kinds not in the
# default kubeconform schema repository (CustomResourceDefinition).
#
# Why a separate script: Task uses Go templates, which conflicts with
# kubeconform's `{{ .ResourceKind }}` placeholder syntax. Shell-only avoids
# the conflict.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SCHEMA_DIR="${SCRIPT_DIR}/master-standalone-strict"

if [[ ! -d "$SCHEMA_DIR" ]]; then
  echo "ERROR: schema dir not found: $SCHEMA_DIR" >&2
  exit 2
fi

cd "$ROOT_DIR"

# Pin K8s minor for chart compatibility decisions (matches Pulumi.dev.yaml).
KUBE_VERSION="${KUBE_VERSION:-1.32}"

# CRDs and other kinds that don't yet have local schemas.
# Schemas in infra/kubeconform-schemas/master-standalone-strict/ already cover:
#   - cert-manager (Certificate, Issuer, ClusterIssuer, CertificateRequest, Challenge, Order)
#   - traefik (IngressRoute, IngressRouteTCP, Middleware, TLSOption, ServersTransport, ...)
#   - sigstore (ClusterImagePolicy v1alpha1+v1beta1, TrustRoot v1alpha1)  [iter 13]
#   - keda.sh (ScaledObject, ScaledJob, TriggerAuthentication, ClusterTriggerAuthentication, CloudEventSource)  [iter 13]
#   - apiextensions.k8s.io/v1 CustomResourceDefinition
#   - velero.io (Schedule, BackupStorageLocation, Backup, Restore, ...) — generated
#     by `loopzero-ops kubeconform-schemas --chart infra/helm/velero` from the CRDs the
#     chart version pinned in infra/helm/velero/Chart.lock ships (#391)
# Still skipped (no local schema yet):
#   - prometheus monitoring.coreos.com (ServiceMonitor, PrometheusRule, Probe)
#   - admissionregistration.k8s.io VAP (validated upstream)
#   - http.keda.sh HTTPScaledObject (chart-internal, not in standard KEDA)
# Only kinds that genuinely have no schema available locally or in defaults.
# VAP/VAPBinding ARE in kubeconform's default schemas (K8s native since 1.30).
# Iter 13 added: cert-manager, traefik, KEDA, sigstore CRDs (~30 schemas).
# Iter 15 added: prometheus-operator CRDs (Probe, ServiceMonitor, PrometheusRule,
# Alertmanager, AlertmanagerConfig, PodMonitor, PrometheusAgent, Prometheus,
# ScrapeConfig, ThanosRuler) — 10 schemas extracted from kube-prometheus-stack.
# Only HTTPScaledObject remains skipped (keda-add-ons-http-specific CRD not yet
# extracted into the local schema set).
SKIP_KINDS="HTTPScaledObject"

# kubeconform schema lookup template — local CRD schemas first, then defaults.
LOCAL_SCHEMA_TPL='{{.ResourceKind}}-{{.Group}}-{{.ResourceAPIVersion}}.json'

# The helmfiles this validation renders. The list named `mgmt build runtime`
# until 2026-09-23, two of which had been renamed away (B-001: core, cell), so
# the loop could never pass. secrets.helmfile is not listed: it declares no
# `dev` environment and its releases are gated off by default.
HELMFILES=(core build cell)

# cell.helmfile declares `kubeContext: requiredEnv "LOOPZERO_CELL_CONTEXT"`.
# Rendering contacts no cluster, so any cell context satisfies it.
export LOOPZERO_CELL_CONTEXT="${LOOPZERO_CELL_CONTEXT:-k3d-loopzero-cell-0}"

# The ory-kratos and ory-hydra values call `requiredEnv "LOOPZERO_DB_PASSWORD"` to
# build their DSNs. This render is offline and only schema-checked, so it must
# never need the real secret: a fixed, render-only placeholder is set
# unconditionally — an operator's real value in the environment is deliberately
# NOT used, so it never reaches the rendered stream (#391).
export LOOPZERO_DB_PASSWORD="render-only-placeholder-not-a-secret"

FAIL=0
for hf in "${HELMFILES[@]}"; do
  file="infra/helmfile/${hf}.helmfile.yaml.gotmpl"
  if [[ ! -f "$file" ]]; then
    echo "ERROR: $file not found — the HELMFILES list drifted from infra/helmfile/" >&2
    exit 2
  fi
  echo "=== $hf helmfile ==="
  # Chart dependencies are resolved here rather than assumed (no --skip-deps):
  # on a fresh clone the local charts' charts/ directories are empty and a
  # skip turns a correct tree into a failed render. Render errors reach stderr.
  helmfile -f "$file" -e dev template \
    --kube-version "$KUBE_VERSION" \
    --args "--api-versions=policy/v1/PodDisruptionBudget" | \
    kubeconform --strict --summary \
      --schema-location 'default' \
      --schema-location "${SCHEMA_DIR}/${LOCAL_SCHEMA_TPL}" \
      --skip "$SKIP_KINDS" || FAIL=1
done

if [[ "$FAIL" -ne 0 ]]; then
  echo "validate:helmfile-templates FAILED" >&2
  exit 1
fi
echo "✓ All helmfile-rendered K8s resources are schema-valid"
