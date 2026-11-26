#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan v1.2 T4.3 incremental — Pulumi stack output reader lib.
#
# Wraps `pulumi stack output --stack <name> [--show-secrets]` with consistent
# error handling + global vs npx pulumi fallback. Extracted from
# drill-helmfile-bootstrap.sh's inline pulumi_out() — same logic, available
# for other consumers (e.g., future canonical bootstrap-helmfile.sh per T4.3
# full consolidation).
#
# Usage (sourced, not executed directly):
#
#   source infra/helmfile/scripts/lib/pulumi-output.sh
#
#   # Read non-secret output:
#   PG_HOST=$(pulumi_output --dir infra/pulumi-drill --stack drill pgHost)
#
#   # Read secret output:
#   KRATOS_DSN=$(pulumi_output --dir infra/pulumi-drill --stack drill --secret kratosDsn)
#
#   # Read JSON output (object):
#   KC_JSON=$(pulumi_output --dir infra/pulumi-drill --stack drill --secret --json mgmtClusterKubeconfig)
#
# Exit: 0 on success (value to stdout), 1 if pulumi binary missing AND npx
#   also missing, 2 if stack output absent (caller's responsibility to
#   distinguish "missing key" from "empty value").

# --- Helper: detect available pulumi runner ---
pulumi_runner() {
  if command -v pulumi >/dev/null 2>&1; then
    echo "pulumi"
  elif command -v npx >/dev/null 2>&1; then
    echo "npx pulumi"
  else
    echo "[pulumi-output] ERROR: neither 'pulumi' nor 'npx' on PATH" >&2
    return 1
  fi
}

# --- Read a single Pulumi stack output ---
# Args: --dir <pulumi-project-dir> --stack <stack-name> [--secret] [--json] <key>
pulumi_output() {
  local dir="" stack="" secret="false" as_json="false" key=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dir)    dir="$2"; shift 2 ;;
      --stack)  stack="$2"; shift 2 ;;
      --secret) secret="true"; shift ;;
      --json)   as_json="true"; shift ;;
      -*)
        echo "[pulumi-output] ERROR: unknown flag: $1" >&2
        return 1
        ;;
      *)
        key="$1"; shift
        ;;
    esac
  done

  if [[ -z "$dir" || -z "$stack" || -z "$key" ]]; then
    echo "[pulumi-output] ERROR: --dir, --stack, and <key> are all required" >&2
    return 1
  fi

  local runner
  runner=$(pulumi_runner) || return 1

  local flags=()
  [[ "$secret" == "true" ]] && flags+=(--show-secrets)
  [[ "$as_json" == "true" ]] && flags+=(--json)

  # Use --cwd if pulumi binary global; cd subshell if npx
  if [[ "$runner" == "pulumi" ]]; then
    pulumi stack output --stack "$stack" --cwd "$dir" "${flags[@]}" "$key" 2>/dev/null
  else
    (cd "$dir" && npx pulumi stack output --stack "$stack" "${flags[@]}" "$key" 2>/dev/null)
  fi
}

# --- Read multiple outputs in one pulumi invocation (efficiency) ---
# Returns a JSON map. Caller parses via jq/python3.
pulumi_outputs_all() {
  local dir="" stack="" secret="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dir)    dir="$2"; shift 2 ;;
      --stack)  stack="$2"; shift 2 ;;
      --secret) secret="true"; shift ;;
      *)
        echo "[pulumi-output] ERROR: unknown arg: $1" >&2
        return 1
        ;;
    esac
  done

  if [[ -z "$dir" || -z "$stack" ]]; then
    echo "[pulumi-output] ERROR: --dir and --stack required" >&2
    return 1
  fi

  local runner
  runner=$(pulumi_runner) || return 1

  local flags=(--json)
  [[ "$secret" == "true" ]] && flags+=(--show-secrets)

  if [[ "$runner" == "pulumi" ]]; then
    pulumi stack output --stack "$stack" --cwd "$dir" "${flags[@]}" 2>/dev/null
  else
    (cd "$dir" && npx pulumi stack output --stack "$stack" "${flags[@]}" 2>/dev/null)
  fi
}

# Self-test if invoked directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "infra/helmfile/scripts/lib/pulumi-output.sh — Pulumi stack output reader"
  echo ""
  echo "Sourced (not executed directly). Functions:"
  echo "  pulumi_runner          — detect pulumi/npx availability"
  echo "  pulumi_output ARGS     — read single output (key required)"
  echo "  pulumi_outputs_all     — read all outputs as JSON map"
  echo ""
  echo "Usage: --dir <pulumi-project-dir> --stack <stack-name> [--secret] [--json] <key>"
fi
