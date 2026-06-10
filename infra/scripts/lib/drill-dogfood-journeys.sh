#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# drill-dogfood-journeys.sh — CLI-real journey helpers for dogfood-drill.
#
# Plan: docs/plans/completed/2026-05-19-drill-skills-rigor-restoration-plan.md T3.2
# ADR D3: dogfood-drill é mirror de /dogfood (CLI-First Inviolável), NÃO de
# drill-test (infra probes).
#
# Provides:
#   resolve_loopzero_cli                 — builds + returns absolute path to $LOOPZERO
#   prova_de_vida(ns, app, [domain]) — verifies pod Running 1/1 + HTTPS 200
#   prova_de_morte(ns, app, app_arg) — verifies pod NotFound + ArgoCD pruned
#   run_cli_journey(id, desc, cmd)   — runs $LOOPZERO command, captures exit code
#   wait_for_argocd_prune(app, ns)   — polls ArgoCD app deletion (timeout 180s)
#
# Sourced (not executed). Side-effect free except for journey counters which
# the orchestrator initializes.

# Source guard
if [[ -n "${_DRILL_DOGFOOD_JOURNEYS_SOURCED:-}" ]]; then
  return 0
fi
_DRILL_DOGFOOD_JOURNEYS_SOURCED=1

# --- resolve_loopzero_cli ---
# Ensures CLI is built and returns absolute path. Validates node version >=20
# per EC-5 (operator with stale node 18 would fail npm run build cryptically).
# Args: repo_root
# Stdout: absolute path to loopzero binary
# Exit:   0 on success, non-zero with error message on failure
resolve_loopzero_cli() {
  local repo_root="${1:?repo_root required}"
  local node_major
  if ! command -v node >/dev/null 2>&1; then
    echo "resolve_loopzero_cli: ERROR — node binary not found in PATH" >&2
    return 1
  fi
  node_major=$(node --version 2>/dev/null | sed -E 's/^v([0-9]+).*/\1/')
  if [[ -z "$node_major" || "$node_major" -lt 20 ]]; then
    echo "resolve_loopzero_cli: ERROR — loopzero CLI requires node ≥ 20 (found v${node_major:-unknown})" >&2
    return 1
  fi
  local cli_dir="$repo_root/cli"
  if [[ ! -d "$cli_dir" ]]; then
    echo "resolve_loopzero_cli: ERROR — $cli_dir not found" >&2
    return 1
  fi
  (cd "$cli_dir" && npm run build >/dev/null 2>&1) || {
    echo "resolve_loopzero_cli: ERROR — npm run build failed in $cli_dir" >&2
    return 1
  }
  local bin="$cli_dir/bin/loopzero"
  if [[ ! -x "$bin" ]]; then
    echo "resolve_loopzero_cli: ERROR — $bin not executable after build" >&2
    return 1
  fi
  echo "$bin"
}

# --- prova_de_vida ---
# Verifies that a deployed app is alive on the workload cluster:
#   1. kubectl pod Running with container Ready (1/1)
#   2. Optional: HTTPS 200 from /health endpoint
# Args:
#   $1 kubectl_context — workload cluster context name
#   $2 namespace       — tenant namespace
#   $3 app_label       — pod label app=<value>
#   $4 health_url      — optional HTTPS URL to probe (skip if empty)
# Exit: 0 on alive, 1 on dead/unknown
prova_de_vida() {
  local ctx="${1:?ctx required}"
  local ns="${2:?ns required}"
  local app="${3:?app required}"
  local health_url="${4:-}"

  local phase ready
  phase=$(kubectl --context "$ctx" get pod -n "$ns" -l "app=$app" \
    -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")
  ready=$(kubectl --context "$ctx" get pod -n "$ns" -l "app=$app" \
    -o jsonpath='{.items[0].status.containerStatuses[0].ready}' 2>/dev/null || echo "")

  if [[ "$phase" != "Running" ]]; then
    echo "prova_de_vida: FAIL — pod phase=$phase (expected Running)" >&2
    return 1
  fi
  if [[ "$ready" != "true" ]]; then
    echo "prova_de_vida: FAIL — container ready=$ready (expected true)" >&2
    return 1
  fi

  if [[ -n "$health_url" ]]; then
    local status
    status=$(curl -sS -m 30 -o /dev/null -w "%{http_code}" "$health_url" 2>/dev/null || echo "000")
    if [[ "$status" != "200" ]]; then
      echo "prova_de_vida: FAIL — health probe $health_url returned $status (expected 200)" >&2
      return 1
    fi
  fi

  echo "prova_de_vida: PASS (phase=Running ready=true${health_url:+ health=200})"
  return 0
}

# --- wait_for_argocd_prune ---
# Polls ArgoCD until app is gone. Timeout configurable via DRILL_PRUNE_TIMEOUT
# (EC-6 — drill cross-cluster prune may take longer than local k3d).
# Args:
#   $1 mgmt_ctx — ArgoCD cluster context (mgmt)
#   $2 app_name — Application CR name
#   $3 ns       — ArgoCD namespace (default argocd)
# Exit: 0 if pruned within timeout, 1 if still present
wait_for_argocd_prune() {
  local ctx="${1:?ctx required}"
  local app="${2:?app required}"
  local ns="${3:-argocd}"
  local timeout="${DRILL_PRUNE_TIMEOUT:-180}"
  local elapsed=0
  local sleep_interval=5

  # Refresh hard once to invalidate repo-server cache (CLAUDE.md known pattern)
  kubectl --context "$ctx" annotate application "$app" -n "$ns" \
    argocd.argoproj.io/refresh=hard --overwrite >/dev/null 2>&1 || true

  while [[ $elapsed -lt $timeout ]]; do
    if ! kubectl --context "$ctx" get application "$app" -n "$ns" >/dev/null 2>&1; then
      echo "wait_for_argocd_prune: PASS (pruned in ${elapsed}s)"
      return 0
    fi
    sleep "$sleep_interval"
    elapsed=$((elapsed + sleep_interval))
  done

  echo "wait_for_argocd_prune: FAIL — app $app still present after ${timeout}s" >&2
  return 1
}

# --- prova_de_morte ---
# Verifies that a deleted project's resources are gone:
#   1. kubectl pod NotFound (no resources matching label)
#   2. ArgoCD application pruned
# Args:
#   $1 workload_ctx — workload cluster context
#   $2 mgmt_ctx     — mgmt cluster context (where ArgoCD runs)
#   $3 namespace    — tenant namespace
#   $4 app_label    — pod label app=<value>
#   $5 argocd_app   — ArgoCD Application CR name
# Exit: 0 if dead, 1 if zombie state detected
prova_de_morte() {
  local workload_ctx="${1:?workload_ctx required}"
  local mgmt_ctx="${2:?mgmt_ctx required}"
  local ns="${3:?ns required}"
  local app="${4:?app required}"
  local argocd_app="${5:?argocd_app required}"
  local timeout="${DRILL_PRUNE_TIMEOUT:-180}"
  local elapsed=0
  local sleep_interval=5

  # Step 1: wait for pods to disappear
  while [[ $elapsed -lt $timeout ]]; do
    local count
    count=$(kubectl --context "$workload_ctx" get pod -n "$ns" -l "app=$app" \
      --no-headers 2>/dev/null | wc -l)
    if [[ "$count" == "0" ]]; then
      break
    fi
    sleep "$sleep_interval"
    elapsed=$((elapsed + sleep_interval))
  done
  local final_count
  final_count=$(kubectl --context "$workload_ctx" get pod -n "$ns" -l "app=$app" \
    --no-headers 2>/dev/null | wc -l)
  if [[ "$final_count" != "0" ]]; then
    echo "prova_de_morte: FAIL — $final_count pod(s) still present after ${timeout}s" >&2
    return 1
  fi

  # Step 2: wait for ArgoCD prune
  if ! wait_for_argocd_prune "$mgmt_ctx" "$argocd_app"; then
    return 1
  fi

  echo "prova_de_morte: PASS (pods gone + ArgoCD pruned)"
  return 0
}

# --- run_cli_journey ---
# Runs a `$LOOPZERO <cmd>` invocation, captures exit code + duration.
# Args:
#   $1 id        — journey ID (e.g., J04)
#   $2 desc      — human description
#   $3 cmd       — the command line to execute (already includes $LOOPZERO)
#   $4 expected  — expected exit code (default 0)
# Stdout: journey log line
# Side-effects: increments JOURNEYS_PASSED or JOURNEYS_FAILED
# Exit: 0 if expected match, 1 otherwise
run_cli_journey() {
  local id="${1:?id required}"
  local desc="${2:?desc required}"
  local cmd="${3:?cmd required}"
  local expected="${4:-0}"

  local start_ts actual end_ts duration
  start_ts=$(date +%s)
  # shellcheck disable=SC2086
  eval "$cmd" >/dev/null 2>&1
  actual=$?
  end_ts=$(date +%s)
  duration=$((end_ts - start_ts))

  if [[ "$actual" == "$expected" ]]; then
    echo "  ✓ $id $desc (exit $actual, ${duration}s)"
    JOURNEYS_PASSED=$((${JOURNEYS_PASSED:-0} + 1))
    return 0
  fi
  echo "  ✗ $id $desc: exit $actual (expected $expected, ${duration}s)" >&2
  JOURNEYS_FAILED=$((${JOURNEYS_FAILED:-0} + 1))
  return 1
}

# --- discover_workload_context ---
# Auto-discover workload cluster context from pulumi output (EC-7).
# Fallback: use $WORKLOAD_CONTEXT env var or "k8s-drill-workload" default.
# Args: pulumi_stack_dir (optional, default $REPO_ROOT/infra/pulumi-drill)
# Stdout: kubectl context name
discover_workload_context() {
  local stack_dir="${1:-$PWD/infra/pulumi-drill}"
  if [[ -n "${WORKLOAD_CONTEXT:-}" ]]; then
    echo "$WORKLOAD_CONTEXT"
    return 0
  fi
  if [[ -d "$stack_dir" ]] && command -v npx >/dev/null 2>&1; then
    local ctx
    ctx=$(cd "$stack_dir" && npx pulumi stack output workloadContext --stack drill 2>/dev/null | tr -d '"' || echo "")
    if [[ -n "$ctx" ]]; then
      echo "$ctx"
      return 0
    fi
  fi
  echo "k8s-drill-workload"
}
