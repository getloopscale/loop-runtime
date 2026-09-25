#!/usr/bin/env bash
# T1.1 of plan 2026-06-04-dev-public-domain-canonicalization-plan.md
# Sync infra/local/Caddyfile (canonical, tracked) -> .local/Caddyfile (runtime)
# + start/reload Caddy + validate certs issued for 6 hosts.
#
# Caddy obtains TLS via Let's Encrypt HTTP-01 challenge on :80, so the droplet
# must have port 80 + 443 reachable from the internet (no CF proxy in front).
# DNS records (auth/app/cloud {,-dev}.loopzero.example) must already point at this
# droplet — run infra/scripts/local/setup-dev-public-dns.sh first.
#
# Idempotent: if Caddy already running with same config, reload via API.
# Otherwise spawn new caddy process backgrounded.
#
# Flags:
#   --dry-run    show plan without applying
#   --reload     skip sync; just `caddy reload`
#   --verify     after start, curl each /health endpoint via HTTPS

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CANONICAL="$REPO_ROOT/infra/local/Caddyfile"
RUNTIME="$REPO_ROOT/.local/Caddyfile"

DRY_RUN=0
RELOAD_ONLY=0
VERIFY=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --reload) RELOAD_ONLY=1; shift ;;
    --verify) VERIFY=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

log() { printf '[caddy-up-dev-public] %s\n' "$*" >&2; }

command -v caddy >/dev/null 2>&1 || {
  log "ERROR: caddy binary not found in PATH"
  exit 1
}

sync_runtime() {
  [[ -f "$CANONICAL" ]] || { log "ERROR: canonical $CANONICAL missing"; exit 1; }
  mkdir -p "$(dirname "$RUNTIME")"
  if cmp -s "$CANONICAL" "$RUNTIME" 2>/dev/null; then
    log "runtime $RUNTIME already in sync with canonical"
    return 1   # no-change signal
  fi
  if [[ $DRY_RUN -eq 1 ]]; then
    log "would sync canonical -> runtime ($(stat -c %s "$CANONICAL") bytes)"
    return 0
  fi
  cp "$CANONICAL" "$RUNTIME"
  log "synced canonical -> $RUNTIME"
  return 0
}

is_caddy_running() {
  pgrep -f "caddy run.*$RUNTIME" >/dev/null 2>&1
}

reload_or_start() {
  if [[ $DRY_RUN -eq 1 ]]; then
    if is_caddy_running; then
      log "would 'caddy reload' (pid $(pgrep -f "caddy run.*$RUNTIME" | head -1))"
    else
      log "would start caddy with $RUNTIME"
    fi
    return
  fi

  if is_caddy_running; then
    log "Caddy running; reloading config"
    caddy reload --config "$RUNTIME"
    return
  fi

  log "starting Caddy in background"
  # NOTE: --pingback removed (canonical 2026-06-16). The flag dials a
  # confirmation address after startup; intended for parent-forked Caddy
  # under a supervisor that opens the listener first. This script uses
  # `nohup &` (no parent supervisor) and `sleep 3 + pgrep` as the readiness
  # check, which is sufficient. With --pingback, Caddy exits 1 with
  # "dial confirmation address ... connection refused".
  nohup caddy run --config "$RUNTIME" >/tmp/caddy.log 2>&1 &
  sleep 3
  if ! is_caddy_running; then
    log "ERROR: Caddy failed to start; see /tmp/caddy.log"
    tail -20 /tmp/caddy.log >&2
    exit 1
  fi
  log "Caddy started (pid $(pgrep -f "caddy run.*$RUNTIME" | head -1))"
}

verify_certs() {
  [[ $VERIFY -eq 1 && $DRY_RUN -eq 0 ]] || return 0
  log "waiting 8s for cert issuance + reload..."
  sleep 8
  # cloud-dev.loopzero.example left this list on 2026-08-18. The Caddyfile removed it
  # deliberately on 2026-08-10 (it was a public host serving a redirect, with an
  # auth surface granted to nobody) and this list did not follow. The result
  # was a verifier that demanded a certificate for a hostname nobody serves — and
  # that therefore failed 100% of the time against the correct configuration. A gate that
  # cannot pass is a gate nobody runs, and that is how the GitHub webhook
  # spent weeks delivering against `tls: internal error` without anything warning.
  # The webhook payload lives on `app-dev`, which already has the /api/v1/webhooks/github handle.
  # B-025 (2026-08-19): the three hosts WITHOUT a suffix left the Caddyfile — they served
  # development processes under a production name. Verifying their TLS here would fail forever,
  # which is exactly the gate-that-cannot-pass shape described in the comment above.
  local hosts=(
    auth-dev.loopzero.example app-dev.loopzero.example
  )
  # tls_ok if openssl handshake succeeds (cert issued + chain valid).
  # http_code informational only — 200/404 means upstream is up, 502 means
  # upstream down (legitimate during Phase 1 if loopzero-cloud not yet deployed).
  local all_tls_ok=1
  for h in "${hosts[@]}"; do
    local code
    code=$(curl -ksS -o /dev/null -w '%{http_code}' --max-time 5 "https://$h/health/ready" 2>/dev/null || echo "ERR")
    local issuer
    issuer=$(echo | timeout 5 openssl s_client -servername "$h" -connect "$h:443" 2>/dev/null \
      | openssl x509 -noout -issuer 2>/dev/null | head -1)
    if echo "$issuer" | grep -qi "let.s encrypt\|^issuer=C = US, O = Let"; then
      case "$code" in
        200|404) log "  $h -> HTTP $code (TLS LE OK, upstream UP)" ;;
        502) log "  $h -> HTTP 502 (TLS LE OK, upstream DOWN — expected if loopzero-cloud absent)" ;;
        *) log "  $h -> HTTP $code (TLS LE OK, upstream status: $code)" ;;
      esac
    else
      log "  $h -> TLS NOT-LE (got '$issuer') FAIL"
      all_tls_ok=0
    fi
  done
  [[ $all_tls_ok -eq 1 ]] || exit 1
}

main() {
  if [[ $RELOAD_ONLY -eq 0 ]]; then
    sync_runtime || log "no-op sync (already current)"
  fi
  reload_or_start
  verify_certs
  log "done"
}

main "$@"
