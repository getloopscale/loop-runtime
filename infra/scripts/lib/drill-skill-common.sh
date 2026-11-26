#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# drill-skill-common.sh — Shared library for /cleanroom-drill, /dogfood-drill, /chaos-test-drill
#
# Plan: 2026-05-18-drill-skills-trio-plan.md
# ADR D2: extracted from session 2026-05-17/18 patterns (cost confirmation, audit init,
# prod-parity invariants, mandatory teardown, sponsor accountability).
#
# Sourced (not executed) by drill-{cleanroom,dogfood,chaos}-orchestrator.sh.
# All functions prefixed `drill_skill_` to avoid namespace collision.

set -euo pipefail

# Source guard — prevent double-sourcing
if [[ -n "${_DRILL_SKILL_COMMON_SOURCED:-}" ]]; then
  return 0
fi
_DRILL_SKILL_COMMON_SOURCED=1
# The drill scripts live in ../drill relative to this lib, wherever it is
# sourced from.
_DRILL_SKILL_SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../drill" && pwd)"

# --- Logging helper ---
drill_skill_log() {
  printf '[drill-skill] %s\n' "$*" >&2
}

# --- .env auto-load ---
# Auto-detects and sources a .env file from common locations so that operators
# don't need to `source .env` before invoking. Honors DRILL_SKILL_DOTENV_PATH
# override; falls back to ./env, $PWD/.env, then $(git rev-parse --show-toplevel)/.env.
# Idempotent: skips reload if _DRILL_SKILL_DOTENV_LOADED is set.
# Safe: no-op if no .env found, silent on parse errors (best-effort).
drill_skill_load_dotenv() {
  if [[ -n "${_DRILL_SKILL_DOTENV_LOADED:-}" ]]; then
    return 0
  fi
  local candidates=()
  if [[ -n "${DRILL_SKILL_DOTENV_PATH:-}" ]]; then
    candidates+=("$DRILL_SKILL_DOTENV_PATH")
  fi
  candidates+=("$PWD/.env")
  local repo_root
  repo_root=$(git rev-parse --show-toplevel 2>/dev/null || echo "")
  if [[ -n "$repo_root" ]]; then
    candidates+=("$repo_root/.env")
  fi
  local dotenv=""
  local cand
  for cand in "${candidates[@]}"; do
    if [[ -f "$cand" ]]; then
      dotenv="$cand"
      break
    fi
  done
  if [[ -z "$dotenv" ]]; then
    return 0
  fi
  drill_skill_log ".env auto-load: parsing $dotenv"
  # Robust parser: read line-by-line, accept only KEY=VALUE where KEY matches
  # the POSIX shell name rule. Skips comments, blanks, and any line that
  # doesn't match — prevents `source` from choking on free-form documentation
  # lines inside .env (a common source of bootstrap failures).
  local line key value loaded=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    # Strip trailing CR (Windows line endings) + leading whitespace
    line="${line%$'\r'}"
    line="${line#"${line%%[![:space:]]*}"}"
    # Skip blank + comment lines
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^# ]] && continue
    # Accept "export KEY=VALUE" or "KEY=VALUE"
    line="${line#export }"
    if [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
      key="${BASH_REMATCH[1]}"
      value="${BASH_REMATCH[2]}"
      # Strip surrounding single or double quotes if present
      if [[ "$value" =~ ^\"(.*)\"$ ]]; then
        value="${BASH_REMATCH[1]}"
      elif [[ "$value" =~ ^\'(.*)\'$ ]]; then
        value="${BASH_REMATCH[1]}"
      fi
      # Only set if not already exported (caller env wins over .env)
      if [[ -z "${!key:-}" ]]; then
        export "$key=$value"
        loaded=$((loaded + 1))
      fi
    fi
  done < "$dotenv"
  drill_skill_log ".env auto-load: $loaded variables exported"

  # --- SPACES_* → AWS_* translation (S3-compatible backend) ---
  # Pulumi uses AWS SDK for `s3://` backend URLs (DO Spaces is S3-API-compatible).
  # The AWS SDK reads AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY. Operators
  # typically store DO Spaces creds as SPACES_* in .env, so we map them here.
  # Caller-set AWS_* always wins. Idempotent.
  if [[ -z "${AWS_ACCESS_KEY_ID:-}" ]] && [[ -n "${SPACES_ACCESS_KEY_ID:-}" ]]; then
    export AWS_ACCESS_KEY_ID="$SPACES_ACCESS_KEY_ID"
    drill_skill_log "  + mapped SPACES_ACCESS_KEY_ID → AWS_ACCESS_KEY_ID"
  fi
  if [[ -z "${AWS_SECRET_ACCESS_KEY:-}" ]] && [[ -n "${SPACES_SECRET_ACCESS_KEY:-}" ]]; then
    export AWS_SECRET_ACCESS_KEY="$SPACES_SECRET_ACCESS_KEY"
    drill_skill_log "  + mapped SPACES_SECRET_ACCESS_KEY → AWS_SECRET_ACCESS_KEY"
  fi
  # DO Spaces S3-compat uses "us-east-1" sigil region (NOT the actual DO region).
  # See taskfiles/drill.yml:up — region=us-east-1 in backend URL.
  if [[ -z "${AWS_REGION:-}" ]]; then
    export AWS_REGION="us-east-1"
  fi

  export _DRILL_SKILL_DOTENV_LOADED=1
}

# Auto-invoke on source so consumers don't have to remember
drill_skill_load_dotenv

# --- Constants ---
DRILL_SKILL_LOCK_FILE="/tmp/drill-skill.lock"
DRILL_SKILL_AUDIT_BASE="/tmp/drill-audit"
DRILL_SKILL_AUDIT_TTL_DAYS=30

# --- D8: Sponsor accountability ---
# Validates LOOP_RUNTIME_DRILL_SKILL_SPONSOR env (operator initials) OR CI bypass.
drill_skill_sponsor_check() {
  if [[ "${CI:-false}" == "true" ]]; then
    drill_skill_log "CI=true — sponsor check bypassed (audit logs CI as sponsor)"
    export DRILL_SKILL_SPONSOR_VALUE="ci"
    return 0
  fi
  if [[ -z "${LOOP_RUNTIME_DRILL_SKILL_SPONSOR:-}" ]]; then
    drill_skill_log "✗ LOOP_RUNTIME_DRILL_SKILL_SPONSOR env not set (operator initials required)"
    drill_skill_log "  hint: export LOOP_RUNTIME_DRILL_SKILL_SPONSOR=<your-initials>"
    return 2
  fi
  # Sanitize: max 8 chars alphanumeric
  if ! [[ "$LOOP_RUNTIME_DRILL_SKILL_SPONSOR" =~ ^[A-Za-z0-9]{1,8}$ ]]; then
    drill_skill_log "✗ LOOP_RUNTIME_DRILL_SKILL_SPONSOR invalid format (1-8 alphanumeric)"
    return 2
  fi
  export DRILL_SKILL_SPONSOR_VALUE="$LOOP_RUNTIME_DRILL_SKILL_SPONSOR"
  return 0
}

# --- D3: Cost guardrail ---
# Args: <estimate_usd> <duration_min>
# Requires --confirm-cost arg OR CI=true env, else exits 2.
drill_skill_confirm_cost() {
  local estimate_usd="$1"
  local duration_min="$2"

  drill_skill_log "Cost estimate: ~\$${estimate_usd} (${duration_min}min × \$0.32/hr drill stack)"

  if [[ "${CI:-false}" == "true" ]]; then
    drill_skill_log "CI=true — cost auto-confirmed"
    return 0
  fi

  if [[ "${DRILL_SKILL_CONFIRM_COST:-false}" != "true" ]]; then
    drill_skill_log "✗ --confirm-cost flag required for dispatch (or set CI=true env)"
    drill_skill_log "  hint: re-invoke with --confirm-cost to acknowledge cost"
    return 2
  fi

  drill_skill_log "Cost confirmed by operator"
  return 0
}

# --- EC-1: Concurrent skill dispatch guard ---
# When invoked by a sibling orchestrator (dogfood-drill or chaos-test-drill
# calling cleanroom-drill as foundation), the parent already holds the lock.
# A child trying to acquire it would deadlock. We detect chained invocations
# via _DRILL_SKILL_LOCK_HELD env (exported by the parent's acquire) and treat
# the child's call as a no-op pass-through.
drill_skill_acquire_lock() {
  local skill_name="$1"
  if [[ -n "${_DRILL_SKILL_LOCK_HELD:-}" ]]; then
    drill_skill_log "lock inherited from parent orchestrator ($_DRILL_SKILL_LOCK_HELD) — no re-acquire"
    return 0
  fi
  exec 200>"$DRILL_SKILL_LOCK_FILE"
  if ! flock -n 200; then
    drill_skill_log "✗ Another drill skill already holds the lock ($DRILL_SKILL_LOCK_FILE)"
    drill_skill_log "  Check running processes or remove stale lock manually."
    return 2
  fi
  echo "$$ $skill_name $(date -u +%FT%TZ)" > "$DRILL_SKILL_LOCK_FILE"
  # Mark held so child processes (sub-orchestrators) see and pass through
  export _DRILL_SKILL_LOCK_HELD="$skill_name"
  return 0
}

# --- EC-4: Audit dir cleanup (>30 days) ---
drill_skill_cleanup_old_audits() {
  find "$DRILL_SKILL_AUDIT_BASE"-* -maxdepth 0 -type d -mtime "+$DRILL_SKILL_AUDIT_TTL_DAYS" \
       -exec rm -rf {} \; 2>/dev/null || true
}

# --- D8 + audit-trail init ---
# Args: <skill_name>
# Creates audit dir + log file + markdown skeleton.
# Sets globals: DRILL_SKILL_AUDIT_DIR, DRILL_SKILL_AUDIT_MD, DRILL_SKILL_LOG_FILE
drill_skill_init_audit() {
  local skill_name="$1"
  local ts
  ts=$(date -u +%FT%H%M%SZ)
  export DRILL_SKILL_AUDIT_DIR="${DRILL_SKILL_AUDIT_BASE}-${skill_name}-${ts}"
  export DRILL_SKILL_AUDIT_MD="${DRILL_SKILL_AUDIT_DIR}/audit.md"
  export DRILL_SKILL_LOG_FILE="${DRILL_SKILL_AUDIT_DIR}/run.log"
  mkdir -p "$DRILL_SKILL_AUDIT_DIR"
  cat > "$DRILL_SKILL_AUDIT_MD" <<EOF
---
type: audit
created_at: $(date -u +%FT%TZ)
skill: $skill_name
sponsor: ${DRILL_SKILL_SPONSOR_VALUE:-unknown}
status: IN_PROGRESS
---

# Drill Skill Audit — $skill_name

**Start:** $(date -u +%FT%TZ)
**Sponsor:** ${DRILL_SKILL_SPONSOR_VALUE:-unknown}

## Steps

EOF
  drill_skill_log "Audit dir: $DRILL_SKILL_AUDIT_DIR"

  # Clean old audits in background
  drill_skill_cleanup_old_audits &
}

# --- Fixture helper: ensure source.bundle exists for hello-server e2e fixture ---
# J03 dogfood journey uploads a git bundle of the hello-server fixture as the
# tenant's source. We don't commit the binary bundle (rots with fixture changes);
# instead, this helper generates it on-demand from the fixture files.
# Args: <fixture_dir> (default: infra/tests/e2e/fixtures/hello-server)
# Idempotent: skips if bundle already exists and is newer than fixture sources.
drill_skill_ensure_source_bundle() {
  local fixture_dir="${1:-infra/tests/e2e/fixtures/hello-server}"
  local bundle_path="$fixture_dir/source.bundle"
  if [[ -f "$bundle_path" ]]; then
    # Check if any source file is newer than the bundle (excludes bundle itself).
    # `find -newer` returns matches that are strictly newer than the reference.
    local newer_src
    newer_src=$(find "$fixture_dir" -type f \
        \( -name "*.js" -o -name "*.json" -o -name "*.yaml" -o -name "*.ts" \) \
        ! -name "source.bundle" \
        -newer "$bundle_path" 2>/dev/null | head -1)
    if [[ -z "$newer_src" ]]; then
      return 0
    fi
    drill_skill_log "source.bundle older than $newer_src — regenerating"
  fi
  drill_skill_log "Generating $bundle_path from fixture..."
  local tmpgit
  tmpgit=$(mktemp -d -t source-bundle-XXXXXX)
  cp -r "$fixture_dir"/* "$tmpgit/"
  (
    cd "$tmpgit" && \
    git init -q -b main && \
    git -c user.email=drill@loop-runtime.local -c user.name=drill add . && \
    git -c user.email=drill@loop-runtime.local -c user.name=drill commit -q -m "drill fixture" && \
    git bundle create "$bundle_path" --all 2>&1
  ) >/dev/null
  rm -rf "$tmpgit"
  if [[ -f "$bundle_path" ]]; then
    drill_skill_log "  ✓ source.bundle generated ($(stat -c%s "$bundle_path") bytes)"
    return 0
  fi
  drill_skill_log "  ✗ source.bundle generation failed"
  return 1
}

# --- D6: Prod-parity invariants check ---
# Args: <kubeconfig> <tenant_namespace>
# Returns 0 if all 5 invariants PASS, N where N=violation count.
drill_skill_check_invariants() {
  local kubeconfig="$1"
  local tenant_ns="$2"
  local violations=0

  drill_skill_log "Checking 5 prod-parity invariants in $tenant_ns..."

  # Invariant 1: image registry = DOCR
  local image
  image=$(kubectl --kubeconfig "$kubeconfig" -n "$tenant_ns" get pod -l runtime.getloopscale.com/app-type=server \
    -o jsonpath='{.items[0].spec.containers[0].image}' 2>/dev/null || echo "")
  if [[ "$image" != registry.digitalocean.com/loop-runtime-system/* ]]; then
    drill_skill_emit_finding "S0" "Invariant #1 FAIL: image not DOCR ($image)"
    violations=$((violations + 1))
  else
    drill_skill_log "  ✓ #1 image DOCR: $image"
  fi

  # Invariant 2: TLS cert = Let's Encrypt (best-effort — needs Secret access)
  local cert_issuer
  cert_issuer=$(kubectl --kubeconfig "$kubeconfig" -n "$tenant_ns" get secret \
    -l "controller.cert-manager.io/fao=true" -o jsonpath='{.items[0].metadata.annotations.cert-manager\.io/issuer-name}' 2>/dev/null || echo "")
  if [[ "$cert_issuer" != "letsencrypt-prod" ]]; then
    drill_skill_emit_finding "S0" "Invariant #2 FAIL: TLS issuer not letsencrypt-prod ($cert_issuer)"
    violations=$((violations + 1))
  else
    drill_skill_log "  ✓ #2 cert Issuer: $cert_issuer"
  fi

  # Invariant 3: PG host = DO Managed (firewall enabled)
  local pg_host
  pg_host=$(kubectl --kubeconfig "$kubeconfig" -n loop-runtime-system get secret loop-runtime-db-credentials \
    -o jsonpath='{.data.host}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
  if [[ "$pg_host" != *.db.ondigitalocean.com ]]; then
    drill_skill_emit_finding "S0" "Invariant #3 FAIL: PG host not DO Managed ($pg_host)"
    violations=$((violations + 1))
  else
    drill_skill_log "  ✓ #3 PG host: $pg_host (DO Managed)"
  fi

  # Invariant 4: Valkey TLS port 25061
  local valkey_port
  valkey_port=$(kubectl --kubeconfig "$kubeconfig" -n loop-runtime-system get secret loop-runtime-db-credentials \
    -o jsonpath='{.data.redis_port}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
  # Allow either explicit Valkey port secret OR omitted if non-applicable
  if [[ -n "$valkey_port" ]] && [[ "$valkey_port" != "25061" ]]; then
    drill_skill_emit_finding "S0" "Invariant #4 FAIL: Valkey port not 25061 ($valkey_port)"
    violations=$((violations + 1))
  else
    drill_skill_log "  ✓ #4 Valkey TLS port: ${valkey_port:-N/A}"
  fi

  # Invariant 5: DNS resolves via CF nameservers
  local domain ns_check
  domain="${DRILL_BASE_DOMAIN:-drill.getloopscale.com}"
  ns_check=$(dig +short "$domain" NS 2>/dev/null | head -1 || echo "")
  if [[ "$ns_check" != *.ns.cloudflare.com. ]]; then
    drill_skill_emit_finding "S1" "Invariant #5 WARN: NS not CF for $domain ($ns_check)"
    # S1 not S0 because local /etc/hosts override CAN bypass for testing
  else
    drill_skill_log "  ✓ #5 NS via CF: $ns_check"
  fi

  return "$violations"
}

# --- D7: Emit finding to audit md ---
# Args: <severity> <description>
drill_skill_emit_finding() {
  local severity="$1"
  local description="$2"
  if [[ -z "${DRILL_SKILL_AUDIT_MD:-}" ]]; then
    drill_skill_log "warn: audit md not initialized — finding lost: [$severity] $description"
    return 0
  fi
  printf -- '- [%s] %s\n' "$severity" "$description" >> "$DRILL_SKILL_AUDIT_MD"
}

# --- D4: Mandatory teardown wrapper ---
# Args: (none — uses DRILL_SKILL_AUDIT_DIR + invokes drill/teardown-validator.sh)
drill_skill_teardown() {
  drill_skill_log "Mandatory teardown initiated..."

  # Step 1: capture evidence pre-teardown (best-effort)
  # evidence-capture.sh expects an identifier (regex ^[a-zA-Z0-9._-]+$),
  # NOT a path. Pass only the basename of DRILL_SKILL_AUDIT_DIR.
  local _evidence="$_DRILL_SKILL_SCRIPTS_DIR/evidence-capture.sh"
  if [[ -f "$_evidence" ]]; then
    local _audit_id="${DRILL_SKILL_AUDIT_DIR:-/tmp/drill-audit-evac}"
    _audit_id="$(basename "$_audit_id")"
    bash "$_evidence" "$_audit_id" 2>&1 \
      | tail -5 | sed 's/^/  /' || drill_skill_log "  WARN: evidence capture failed (continuing to teardown)"
  else
    drill_skill_log "  WARN: $_evidence not found — no pre-teardown evidence"
  fi

  # Step 2: Pulumi destroy (direct invocation — bypasses Task v3 dotenv gap)
  # task drill:down inherits .env via Task `dotenv:` directive, but that does NOT
  # auto-translate SPACES_* → AWS_* required by Pulumi S3 backend. The lib's
  # drill_skill_load_dotenv (called on source) HAS already done that translation,
  # so by calling pulumi directly here we use the correct AWS_* env vars.
  # Includes refresh-then-destroy retry sequence to absorb state drift from
  # validator-only teardowns (real cloud deleted, Pulumi state out of sync).
  local _backend="${DRILL_BACKEND_URL:-s3://loop-runtime-drill-pulumi-state?endpoint=sfo3.digitaloceanspaces.com&region=us-east-1&s3ForcePathStyle=true}"
  drill_skill_log "  Step 2a: pulumi refresh (sync state with reality)"
  local _pr_log
  _pr_log=$(mktemp -t drill-pulumi-refresh-XXXXXX.log)
  (
    cd "${PULUMI_DRILL_DIR:-infra/pulumi-drill}" && \
    npx pulumi login "$_backend" >/dev/null 2>&1 && \
    npx pulumi refresh --stack drill --yes --skip-preview > "$_pr_log" 2>&1
  ) || true  # refresh failure is non-fatal; destroy will surface real errors
  tail -3 "$_pr_log" | sed 's/^/    /' || true
  drill_skill_log "  Step 2b: pulumi destroy (max 3 iterations for residual cleanup)"
  # Pulumi destroy can leave 2-4 orphan resources in state when DOKS / managed
  # DB deletions cascade out-of-order (deletion completes async server-side after
  # pulumi gives up waiting). Solution: refresh + destroy up to 3 times, stop
  # when resource count reaches 0 (idempotent — destroy on clean state is no-op).
  local _pd_log _resource_count _pd_rc
  _pd_log=$(mktemp -t drill-pulumi-destroy-XXXXXX.log)
  local _retry
  for _retry in 1 2 3; do
    _pd_rc=0
    (
      cd "${PULUMI_DRILL_DIR:-infra/pulumi-drill}" && \
      npx pulumi destroy --stack drill --yes --skip-preview > "$_pd_log" 2>&1
    ) || _pd_rc=$?
    tail -5 "$_pd_log" | sed "s/^/    [iter $_retry] /"
    # Check if state is now empty via `pulumi stack` (formatted output).
    # Pulumi prints "Current stack resources (N):" — extract N from parens.
    # Also matches legacy "N resources" format as fallback.
    _resource_count=$(
      cd "${PULUMI_DRILL_DIR:-infra/pulumi-drill}" && \
      npx pulumi stack --stack drill 2>/dev/null | \
        grep -oE 'Current stack resources \([0-9]+\)|[0-9]+ resources' | head -1 | \
        grep -oE '[0-9]+' || echo ""
    )
    if [[ -z "$_resource_count" ]]; then
      # Fallback: if rc=0 and we can't get count, trust the rc
      if [[ "$_pd_rc" -eq 0 ]]; then
        drill_skill_log "  ✓ pulumi destroy complete (rc=0, count unparseable)"
        _drill_skill_finalize_teardown "CLEAN"
        return 0
      fi
      _resource_count="?"
    fi
    if [[ "$_resource_count" == "0" ]]; then
      drill_skill_log "  ✓ pulumi destroy complete (state empty after iter $_retry)"
      _drill_skill_finalize_teardown "CLEAN"
      return 0
    fi
    drill_skill_log "  iter $_retry: rc=$_pd_rc, $_resource_count residual resources — refreshing + retrying"
    (
      cd "${PULUMI_DRILL_DIR:-infra/pulumi-drill}" && \
      npx pulumi refresh --stack drill --yes --skip-preview > /dev/null 2>&1
    ) || true
  done
  drill_skill_log "  ✗ pulumi destroy still has $_resource_count residual resources after 3 iters"
  drill_skill_log "  full log: $_pd_log"

  # Step 3: pulumi state still holds resources. Only the validator can say
  # whether the cloud does too, so a missing or failing validator is a failed
  # teardown, never a clean one.
  local _validator="$_DRILL_SKILL_SCRIPTS_DIR/teardown-validator.sh"
  if [[ ! -f "$_validator" ]]; then
    drill_skill_log "  ✗ $_validator not found — cannot verify the residuals"
    _drill_skill_finalize_teardown "RESIDUAL ($_resource_count in pulumi state, unverified)"
    return 7
  fi
  if bash "$_validator" 2>&1 | tail -5 | sed 's/^/  /'; then
    drill_skill_log "  ✓ teardown validator PASS"
  else
    drill_skill_log "  ✗ teardown validator FAIL — residuals may exist"
    _drill_skill_finalize_teardown "RESIDUAL ($_resource_count in pulumi state, validator failed)"
    return 7
  fi

  _drill_skill_finalize_teardown "CLEAN"
  return 0
}

# _drill_skill_finalize_teardown appends the end time and the teardown outcome
# to the run's audit markdown, when there is one.
_drill_skill_finalize_teardown() {
  if [[ -n "${DRILL_SKILL_AUDIT_MD:-}" ]] && [[ -f "$DRILL_SKILL_AUDIT_MD" ]]; then
    {
      echo ""
      echo "**End:** $(date -u +%FT%TZ)"
      echo "**Teardown:** $1"
    } >> "$DRILL_SKILL_AUDIT_MD"
  fi
}

# --- Cleanup on exit (trap) ---
drill_skill_cleanup_on_exit() {
  local exit_code=$?
  # Release lock
  flock -u 200 2>/dev/null || true
  rm -f "$DRILL_SKILL_LOCK_FILE" 2>/dev/null || true
  return "$exit_code"
}

# Auto-set trap when sourced
trap drill_skill_cleanup_on_exit EXIT
