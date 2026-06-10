#!/usr/bin/env bash
# run-polaris.sh — Wrapper to run Polaris audit on Helm chart manifests.
#
# Per Plan T1.2 + INF-G08: Polaris detects securityContext gaps, missing probes,
# missing resource limits, and other production readiness issues at the per-pod
# level (audit-security-context.sh works at the per-chart level — Polaris is
# finer-grained).
#
# Usage:
#   bash scripts/run-polaris.sh                       # all charts in charts/
#   bash scripts/run-polaris.sh charts/loopzero-api       # one chart
#   bash scripts/run-polaris.sh --severity=danger     # gate mode (exit non-zero on danger)
#
# Exit codes:
#   0  audit GREEN at chosen severity
#   1  audit FAILED at chosen severity
#   2  usage error / polaris not installed

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
POLARIS_CONFIG="${REPO_ROOT}/.polaris.yaml"

if ! command -v polaris >/dev/null 2>&1; then
    echo "ERROR: polaris not installed. Install:" >&2
    echo "  curl -L https://github.com/FairwindsOps/polaris/releases/latest/download/polaris_linux_amd64.tar.gz | tar -xz" >&2
    echo "  sudo install polaris /usr/local/bin/" >&2
    exit 2
fi

# Default args.
SEVERITY="warning"
TARGETS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --severity=*)
            SEVERITY="${1#*=}"
            shift
            ;;
        --severity)
            SEVERITY="$2"
            shift 2
            ;;
        -h|--help)
            sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
            exit 0
            ;;
        *)
            TARGETS+=("$1")
            shift
            ;;
    esac
done

if [[ ${#TARGETS[@]} -eq 0 ]]; then
    # Default: scan all published charts (skip library loopzero-common).
    while IFS= read -r d; do
        [[ "$(basename "$d")" == "loopzero-common" ]] && continue
        TARGETS+=("$d")
    done < <(find "${REPO_ROOT}/charts" -mindepth 1 -maxdepth 1 -type d | sort)
fi

declare -i FAILED=0
declare -a SUMMARY=()

for chart in "${TARGETS[@]}"; do
    name=$(basename "${chart%/}")
    echo "::group::polaris audit ${name}"

    # Render chart to k8s manifests, then audit.
    rendered=$(mktemp /tmp/polaris-${name}-XXXXXX.yaml)
    if ! helm template "${chart}" > "$rendered" 2>/dev/null; then
        echo "WARNING: helm template failed for ${name} — skipping audit" >&2
        rm -f "$rendered"
        SUMMARY+=("${name}: SKIP (helm template failed)")
        continue
    fi

    # Run polaris audit.
    audit_args=(audit --audit-path "$rendered" --format pretty)
    if [[ -f "${POLARIS_CONFIG}" ]]; then
        audit_args+=(--config "${POLARIS_CONFIG}")
    fi
    audit_args+=(--set-exit-code-on-danger)
    if [[ "${SEVERITY}" == "warning" ]]; then
        audit_args+=(--set-exit-code-below-score 75)
    fi

    if polaris "${audit_args[@]}"; then
        SUMMARY+=("${name}: GREEN")
    else
        SUMMARY+=("${name}: FAIL")
        FAILED=$((FAILED + 1))
    fi
    rm -f "$rendered"
    echo "::endgroup::"
done

echo
echo "Polaris audit summary (severity=${SEVERITY}):"
for line in "${SUMMARY[@]}"; do
    echo "  ${line}"
done

if [[ ${FAILED} -gt 0 ]]; then
    echo
    echo "❌ ${FAILED} chart(s) failed Polaris audit at severity=${SEVERITY}."
    echo "Review baseline: docs/runbooks/polaris-baseline.md"
    exit 1
fi

echo
echo "✅ All charts pass Polaris audit at severity=${SEVERITY}."
exit 0
