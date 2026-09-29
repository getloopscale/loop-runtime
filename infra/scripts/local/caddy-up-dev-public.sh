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

# The Caddyfile's cell edge substitutes {$LOOPZERO_EDGE_ROUTE_TOKEN} when the config is
# adapted, i.e. by this shell's `caddy reload` / `caddy run`. Read it from the Secret
# generate-secrets.sh keeps, unless the caller already exported it. Without it the
# engine refuses the lookup and every app URL answers 401 — the dashboard and auth
# hosts keep working, so this warns rather than aborting.
# The dashboard the edge serves at app-dev. The Caddyfile reads LOOPZERO_DASHBOARD_DIST and falls
# back to a sibling loopzero-cloud checkout's build, relative to wherever Caddy runs. On the
# droplet that directory does not exist: a reload without the variable replaced the
# dashboard's root with nothing, and every page answered 404 (2026-09-29). Resolve it to an
# absolute path here and refuse to touch the live edge when it holds no build.
#
# The bundle's source of record is the loopzero-dashboard image pinned in
# environments/dev-public.yaml (#462): with LOOPZERO_DASHBOARD_IMAGE set and no
# LOOPZERO_DASHBOARD_DIST override, it is extracted once per tag into ~/.cache.
resolve_dashboard() {
  if [[ -z "${LOOPZERO_DASHBOARD_DIST:-}" && -n "${LOOPZERO_DASHBOARD_IMAGE:-}" ]]; then
    LOOPZERO_DASHBOARD_DIST="$HOME/.cache/loopzero/dashboard/${LOOPZERO_DASHBOARD_IMAGE##*:}"
    if [[ ! -f "$LOOPZERO_DASHBOARD_DIST/index.html" ]]; then
      extract_dashboard "$LOOPZERO_DASHBOARD_IMAGE" "$LOOPZERO_DASHBOARD_DIST"
      [[ $DRY_RUN -eq 1 ]] && return
    fi
  fi
  local dir="${LOOPZERO_DASHBOARD_DIST:-$REPO_ROOT/../loopzero-cloud/dashboard/dist}"
  if [[ ! -f "$dir/index.html" ]]; then
    log "ERROR: no dashboard build at $dir (index.html missing). Set LOOPZERO_DASHBOARD_DIST to the built dashboard; the live edge was not touched."
    exit 1
  fi
  LOOPZERO_DASHBOARD_DIST="$(cd "$dir" && pwd)"
  export LOOPZERO_DASHBOARD_DIST
}

# extract_dashboard copies the static bundle out of the pinned image. It runs a
# short-lived pod on the core cluster rather than pulling with docker: the cluster's
# nodes already authenticate to the registry (ECR, through the kubelet credential
# provider), and this host's docker does not. The pod is removed however the copy ends.
extract_dashboard() {
  local image="$1" dest="$2" ctx="${LOOPZERO_CORE_CONTEXT:-k3d-loopzero-core}"
  local pod="loopzero-dashboard-dist-$$"
  if [[ $DRY_RUN -eq 1 ]]; then
    log "would extract the dashboard from $image into $dest"
    return
  fi
  log "extracting the dashboard from $image into $dest"
  kubectl --context "$ctx" -n loopzero-system run "$pod" --image="$image" --restart=Never \
    --command -- sleep 300 >/dev/null
  # shellcheck disable=SC2064 # expand now: the trap must name this pod
  trap "kubectl --context '$ctx' -n loopzero-system delete pod '$pod' --wait=false >/dev/null 2>&1 || true" EXIT
  kubectl --context "$ctx" -n loopzero-system wait --for=condition=Ready "pod/$pod" --timeout=180s >/dev/null
  local tmp="$dest.partial"
  rm -rf -- "$tmp"
  kubectl --context "$ctx" -n loopzero-system cp "$pod:/usr/share/nginx/html" "$tmp" >/dev/null
  [[ -f "$tmp/index.html" ]] || { log "ERROR: $image holds no dashboard at /usr/share/nginx/html"; exit 1; }
  mkdir -p "$(dirname "$dest")"
  mv -- "$tmp" "$dest"
  kubectl --context "$ctx" -n loopzero-system delete pod "$pod" --wait=false >/dev/null
  trap - EXIT
}

load_edge_route_token() {
  if [[ -z "${LOOPZERO_EDGE_ROUTE_TOKEN:-}" ]] && command -v kubectl >/dev/null 2>&1; then
    LOOPZERO_EDGE_ROUTE_TOKEN="$(kubectl --context "${LOOPZERO_CORE_CONTEXT:-k3d-loopzero-core}" -n loopzero-system \
      get secret loopzero-edge-route-token -o 'jsonpath={.data.LOOPZERO_EDGE_ROUTE_TOKEN}' 2>/dev/null \
      | base64 -d 2>/dev/null || true)"
  fi
  export LOOPZERO_EDGE_ROUTE_TOKEN="${LOOPZERO_EDGE_ROUTE_TOKEN:-}"
  if [[ -z "$LOOPZERO_EDGE_ROUTE_TOKEN" ]]; then
    log "WARN: LOOPZERO_EDGE_ROUTE_TOKEN unavailable (no loopzero-edge-route-token Secret); app URLs will answer 401"
  fi
}

# The live edge is whichever Caddy answers the admin API. Matching a process on this
# script's config path missed an edge started any other way: the script then started a
# second Caddy on the same ports and failed, and the new edge-route token never reached
# the edge that serves traffic (#455). `caddy reload` goes through the same API.
is_caddy_running() {
  curl -fsS --max-time 2 http://localhost:2019/config/ >/dev/null 2>&1
}

reload_or_start() {
  if [[ $DRY_RUN -eq 1 ]]; then
    if is_caddy_running; then
      log "would 'caddy reload' the Caddy answering localhost:2019"
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
  # `nohup &` (no parent supervisor) and `sleep 3` + the admin API as the readiness
  # check, which is sufficient. With --pingback, Caddy exits 1 with
  # "dial confirmation address ... connection refused".
  nohup caddy run --config "$RUNTIME" >/tmp/caddy.log 2>&1 &
  sleep 3
  if ! is_caddy_running; then
    log "ERROR: Caddy failed to start; see /tmp/caddy.log"
    tail -20 /tmp/caddy.log >&2
    exit 1
  fi
  log "Caddy started (admin API answering on localhost:2019)"
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
  # 200 means the page is served. 404 fails: at app-dev it is the dashboard missing, the
  # outage resolve_dashboard now prevents. 502 is logged and tolerated — loopzero-cloud may
  # not be deployed yet on a cold install.
  local all_tls_ok=1
  for h in "${hosts[@]}"; do
    local code
    code=$(curl -ksS -o /dev/null -w '%{http_code}' --max-time 5 "https://$h/health/ready" 2>/dev/null || echo "ERR")
    local issuer
    issuer=$(echo | timeout 5 openssl s_client -servername "$h" -connect "$h:443" 2>/dev/null \
      | openssl x509 -noout -issuer 2>/dev/null | head -1)
    if echo "$issuer" | grep -qi "let.s encrypt\|^issuer=C = US, O = Let"; then
      case "$code" in
        200) log "  $h -> HTTP $code (TLS LE OK, upstream UP)" ;;
        404) log "  $h -> HTTP 404 (TLS LE OK, but nothing serves the page) FAIL"; all_tls_ok=0 ;;
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
  resolve_dashboard
  if [[ $RELOAD_ONLY -eq 0 ]]; then
    sync_runtime || log "no-op sync (already current)"
  fi
  load_edge_route_token
  reload_or_start
  verify_certs
  log "done"
}

main "$@"
