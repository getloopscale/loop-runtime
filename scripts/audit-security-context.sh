#!/usr/bin/env bash
# audit-security-context.sh — Audit Helm bootstrap charts for securityContext compliance.
#
# Checks per chart in infra/helm/*:
#   - runAsNonRoot:     declared (in values.yaml or templates)
#   - readOnlyRootFilesystem: declared
#   - drop:[ALL]:       capability drop
#   - allowPrivilegeEscalation: false declared
#
# Modes:
#   report (default) — emit a markdown table to stdout
#   --gate           — exit 1 if any chart is NON-CONFORMANT and lacks an ADR exception
#                      (looks for: infra/helm/<chart>/ADR-securitycontext-exception.md)
#
# Per Plan ADR D7: charts that legitimately need privilege document an ADR-of-exception.
# Per Plan ADR D8: gates without CI enforcement are not gates — this script powers infra-gates.yml.
#
# Exit codes:
#   0  all charts CONFORMANT or have ADR exception
#   1  --gate mode and ≥ 1 chart is NON-CONFORMANT without ADR
#   2  usage error

set -euo pipefail

# Resolve repo root regardless of cwd.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HELM_DIR="${REPO_ROOT}/infra/helm"

if [[ ! -d "${HELM_DIR}" ]]; then
    echo "ERROR: ${HELM_DIR} not found — run from loopzero repo." >&2
    exit 2
fi

MODE="report"
case "${1:-}" in
    --gate)   MODE="gate" ;;
    --report|"") MODE="report" ;;
    -h|--help)
        sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
        exit 0
        ;;
    *)
        echo "ERROR: unknown argument: $1" >&2
        echo "Usage: $0 [--gate|--report]" >&2
        exit 2
        ;;
esac

# ---------------------------------------------------------------------------
# Per-chart inspection
# ---------------------------------------------------------------------------

# check_directive(<chart_dir>, <pattern>) — returns "yes" if found in
# values.yaml OR any template, "no" otherwise. The pattern is a grep -E regex.
check_directive() {
    local chart_dir="$1"
    local pattern="$2"
    if grep -rqE "${pattern}" "${chart_dir}" 2>/dev/null; then
        echo "yes"
    else
        echo "no"
    fi
}

# classify(<run_as>, <ro_fs>, <drop_all>, <no_priv_esc>) — returns CONFORMANT, PARTIAL, or NON-CONFORMANT
classify() {
    local run_as="$1" ro_fs="$2" drop_all="$3" no_priv_esc="$4"
    local yes_count=0
    for v in "$run_as" "$ro_fs" "$drop_all" "$no_priv_esc"; do
        [[ "$v" == "yes" ]] && yes_count=$((yes_count + 1))
    done
    case $yes_count in
        4) echo "CONFORMANT" ;;
        2|3) echo "PARTIAL" ;;
        *) echo "NON-CONFORMANT" ;;
    esac
}

# has_pod_bearing_resources(<chart_dir>) — returns "yes" if the chart deploys
# any pod-bearing resource. Three classifications:
#   1. Local templates: Deployment/StatefulSet/DaemonSet/Job/CronJob/Pod kinds
#   2. Chart.yaml dependencies: pulls upstream charts (which have pods)
#   3. Values-overlay pattern: no Chart.yaml but values.yaml configures pod fields
#      (image, replicas, resources, securityContext) — used by Helmfile to feed
#      upstream charts.
# Charts that only emit policy/CR resources (NetworkPolicy, Issuer, ClusterImagePolicy)
# return "no". Avoids false positives.
has_pod_bearing_resources() {
    local chart_dir="$1"

    # 1. Local pod-bearing resources.
    if grep -rqE "^kind:[[:space:]]*(Deployment|StatefulSet|DaemonSet|Job|CronJob|Pod)[[:space:]]*$" "${chart_dir}/templates" 2>/dev/null; then
        echo "yes"
        return
    fi

    # 2. Has Chart.yaml dependencies (pulls upstream).
    if [[ -f "${chart_dir}/Chart.yaml" ]] && grep -qE "^dependencies:" "${chart_dir}/Chart.yaml" 2>/dev/null; then
        echo "yes"
        return
    fi

    # 3. Values-overlay (no Chart.yaml, values.yaml configures pod fields).
    # Helmfile pattern: bootstrap charts in infra/helm/<name>/values.yaml feed
    # upstream Helm chart via release definition.
    if [[ -f "${chart_dir}/values.yaml" ]] && grep -qE "^(image|replicas|resources|securityContext|persistence|podSecurityContext|containerSecurityContext|deployment):" "${chart_dir}/values.yaml" 2>/dev/null; then
        echo "yes"
        return
    fi

    echo "no"
}

has_adr_exception() {
    local chart_dir="$1"
    [[ -f "${chart_dir}/ADR-securitycontext-exception.md" ]]
}

# ---------------------------------------------------------------------------
# Walk charts
# ---------------------------------------------------------------------------

declare -a violations=()
declare -a rows=()

# Header for report mode.
header_md="| Chart | runAsNonRoot | readOnlyRootFS | drop:[ALL] | allowPrivilegeEscalation:false | Status | ADR Exception |"
sep_md="|---|---|---|---|---|---|---|"

# Charts are sorted to keep output stable across runs.
for chart_path in $(find "${HELM_DIR}" -mindepth 1 -maxdepth 1 -type d | sort); do
    chart_name="$(basename "${chart_path}")"

    pod_bearing=$(has_pod_bearing_resources "${chart_path}")
    if [[ "${pod_bearing}" == "no" ]]; then
        rows+=("| ${chart_name} | n/a | n/a | n/a | n/a | N/A (no pods) | — |")
        continue
    fi

    run_as=$(check_directive "${chart_path}" 'runAsNonRoot:[[:space:]]*true')
    ro_fs=$(check_directive "${chart_path}" 'readOnlyRootFilesystem:[[:space:]]*true')
    drop_all=$(check_directive "${chart_path}" 'drop:.*ALL|- ALL|-[[:space:]]*ALL')
    no_priv_esc=$(check_directive "${chart_path}" 'allowPrivilegeEscalation:[[:space:]]*false')

    status=$(classify "$run_as" "$ro_fs" "$drop_all" "$no_priv_esc")

    adr_status="—"
    if has_adr_exception "${chart_path}"; then
        adr_status="✅ exists"
    elif [[ "$status" == "NON-CONFORMANT" ]]; then
        adr_status="❌ missing"
        violations+=("${chart_name}")
    fi

    rows+=("| ${chart_name} | ${run_as} | ${ro_fs} | ${drop_all} | ${no_priv_esc} | ${status} | ${adr_status} |")
done

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

if [[ "${MODE}" == "gate" ]]; then
    if [[ ${#violations[@]} -eq 0 ]]; then
        echo "✅ securityContext audit GREEN — 0 NON-CONFORMANT charts without ADR exception."
        exit 0
    fi
    echo "❌ securityContext audit FAILED — ${#violations[@]} NON-CONFORMANT chart(s) without ADR:"
    for v in "${violations[@]}"; do
        echo "  - ${v}"
        echo "    Action: either fix securityContext in infra/helm/${v}/values.yaml + templates,"
        echo "    OR create infra/helm/${v}/ADR-securitycontext-exception.md documenting the exception."
    done
    exit 1
fi

# Default: report mode — emit the matrix.
cat <<EOF
# securityContext Audit — infra/helm/

**Generated:** $(date -u +%Y-%m-%dT%H:%M:%SZ)
**Charts inspected:** ${#rows[@]}
**Auditor:** scripts/audit-security-context.sh

## Matrix

${header_md}
${sep_md}
EOF

printf '%s\n' "${rows[@]}"

# Summary
total=${#rows[@]}
conformant=0; partial=0; nonconf=0; na=0
for row in "${rows[@]}"; do
    case "$row" in
        *"| CONFORMANT |"*) conformant=$((conformant + 1)) ;;
        *"| PARTIAL |"*) partial=$((partial + 1)) ;;
        *"| NON-CONFORMANT |"*) nonconf=$((nonconf + 1)) ;;
        *"| N/A "*) na=$((na + 1)) ;;
    esac
done

cat <<EOF

## Summary

- **CONFORMANT:** ${conformant}/${total}
- **PARTIAL:** ${partial}/${total}
- **NON-CONFORMANT:** ${nonconf}/${total}
- **N/A (no pods):** ${na}/${total}
- **NON-CONFORMANT without ADR:** ${#violations[@]}

## Next Action

EOF

if [[ ${#violations[@]} -gt 0 ]]; then
    echo "${#violations[@]} chart(s) need either fix OR ADR-securitycontext-exception.md:"
    for v in "${violations[@]}"; do
        echo "- ${v}"
    done
else
    echo "All NON-CONFORMANT charts have ADR exception. Audit gate would PASS."
fi
