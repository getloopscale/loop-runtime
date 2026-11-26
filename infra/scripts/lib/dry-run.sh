#!/usr/bin/env bash
# infra/scripts/lib/dry-run.sh — shared --dry-run helper for state-writing scripts.
#
# Plan 2026-05-21-deep-review-remediation T2.2.
#
# Per memory feedback_dry_run_flag_mandatory_for_state_writers:
# every script that calls kubectl apply, helm install, pulumi up,
# doctl <delete/create>, git push, or migrate up MUST support --dry-run.
# This lib provides a uniform pattern: source it once, then use `apply`
# wrapper in place of direct destructive calls.
#
# Usage in a script:
#
#   #!/usr/bin/env bash
#   set -euo pipefail
#   source "$(dirname "$0")/../lib/dry-run.sh"   # adjust path per caller depth
#   parse_dry_run_flag "$@"  # consumes --dry-run from $@ + exports DRY_RUN
#
#   apply kubectl apply -f manifest.yaml
#   apply helm install foo charts/foo
#   apply doctl databases delete-firewall "$DB_ID"
#
# DRY_RUN modes:
#   DRY_RUN=0 (default) — execute the command
#   DRY_RUN=1 (set by --dry-run flag) — log + skip the command
#
# Exit codes preserved: dry-run NEVER produces non-zero exit (it's
# observability, not validation). Real errors still surface.

# Idempotent source guard
if [[ "${_LOOP_RUNTIME_DRY_RUN_LIB_LOADED:-}" == "1" ]]; then
  return 0
fi
_LOOP_RUNTIME_DRY_RUN_LIB_LOADED=1

# Default to not-dry-run. Caller can override before sourcing OR
# set via parse_dry_run_flag from $@.
: "${DRY_RUN:=0}"

# parse_dry_run_flag — scan "$@" for --dry-run and remove it.
# Caller invokes:
#   parse_dry_run_flag "$@"
#   set -- "${REMAINING_ARGS[@]}"   # rebuild positional args without --dry-run
parse_dry_run_flag() {
  REMAINING_ARGS=()
  for arg in "$@"; do
    case "$arg" in
      --dry-run|-n)
        DRY_RUN=1
        ;;
      *)
        REMAINING_ARGS+=("$arg")
        ;;
    esac
  done
  export DRY_RUN
}

# apply — execute a command unless DRY_RUN=1, in which case log the
# would-execute and return 0.
apply() {
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "DRY_RUN: $*" >&2
    return 0
  fi
  "$@"
}

# apply_pipe — for piped commands (e.g., `cmd | kubectl apply -f -`).
# Usage: cmd_producing_stdin | apply_pipe kubectl apply -f -
apply_pipe() {
  if [[ "$DRY_RUN" == "1" ]]; then
    cat >/dev/null  # consume stdin to avoid breaking caller pipe
    echo "DRY_RUN: <piped-stdin> | $*" >&2
    return 0
  fi
  "$@"
}

# Dry-run-aware echo for explanatory log messages that should still print
# in dry-run mode (e.g., "Would create $RESOURCE in $NAMESPACE").
dry_run_log() {
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "DRY_RUN: $*" >&2
  fi
}
