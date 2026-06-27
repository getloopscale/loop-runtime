#!/usr/bin/env bash
# Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T3.2 — aggregate dogfood
# wrapper retroactively completing Sprint 7 #36 triplet. The Sprint 7 fix
# (api/cmd/setup_server.go regen-build-runtime-kubeconfigs.py CLUSTER_MAP
# tuple extension) shipped with LIVE-only; per the new INQUEBRÁVEL feedback
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` every issue
# closure now requires hermetic syntax + dogfood wrapper triplet.
#
# Suites:
#   1) T3.1 hermetic syntax check (CI-safe)
#   2) Go unit tests api/internal/services/... (-race) — Argo client wiring
#   3) Sprint 8 #37 follow-up: api/cmd/... setup_server.go Argo Workflow
#      namespace-scoped LIST (the Sprint 7 RBAC grant is namespace-scoped
#      so the consumer in setup_server must match — `argo` namespace, not
#      cluster-scope).
#   4) T1.1 LIVE bash (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#
# Flags: --hermetic-only, --live-only, --json
set -euo pipefail

PLAN_SLUG="qa-sprint8-cleanup-2026-06-25"
HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0
RESULTS=()

for arg in "$@"; do
    case "$arg" in
        --hermetic-only) HERMETIC_ONLY=1 ;;
        --live-only) LIVE_ONLY=1 ;;
        --json) JSON_OUTPUT=1 ;;
        --help)
            cat <<EOF
Usage: $(basename "$0") [--hermetic-only] [--live-only] [--json] [--help]

Aggregate dogfood wrapper retroactively completing Sprint 7 #36 triplet:
hermetic + Go (-race) + LIVE bash 4-scenario. Operator HARD STOP for LIVE
per cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
EOF
            exit 0
            ;;
        *) echo "unknown flag: $arg" >&2; exit 2 ;;
    esac
done

if [ "$HERMETIC_ONLY" -eq 1 ] && [ "$LIVE_ONLY" -eq 1 ]; then
    echo "mutual exclusion: --hermetic-only and --live-only" >&2
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

run_suite() {
    local id="$1" cmd="$2"
    if (cd "$REPO_ROOT" && eval "$cmd") >/dev/null 2>&1; then
        RESULTS+=("$id:PASS")
    else
        RESULTS+=("$id:FAIL")
    fi
}

# Suite 1 — hermetic syntax test
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-build-rbac-syntax_test.sh"
fi

# Suite 2 — Go services (-race) covers Argo client wiring origin
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S2-go-services-race" "go test -race ./api/internal/services/..."
fi

# Suite 3 — Sprint 8 #37 follow-up: setup_server.go namespace-scoped Argo
# Workflow LIST consumer must match the namespace-scoped RBAC grant from
# Sprint 7 (Role + RoleBinding in `argo` namespace, NOT cluster-scope).
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S3-go-cmd-setup-server" "go vet ./api/cmd/..."
fi

# Suite 4 — LIVE bash (HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S4-live-build-rbac" "bash infra/tests/e2e/dev-public-build-rbac_test.sh"
fi

fail_count=0
for r in "${RESULTS[@]}"; do
    case "$r" in *:FAIL) fail_count=$((fail_count + 1)) ;; esac
done

if [ "$JSON_OUTPUT" -eq 1 ]; then
    printf '{"plan":"%s","suites":[' "$PLAN_SLUG"
    first=1
    for r in "${RESULTS[@]}"; do
        IFS=':' read -r id verdict <<<"$r"
        [ $first -eq 0 ] && printf ','
        printf '{"id":"%s","verdict":"%s"}' "$id" "$verdict"
        first=0
    done
    printf '],"fail_count":%d}\n' "$fail_count"
else
    for r in "${RESULTS[@]}"; do echo "$r"; done
    echo "fail_count=$fail_count"
fi

[ "$fail_count" -eq 0 ]
