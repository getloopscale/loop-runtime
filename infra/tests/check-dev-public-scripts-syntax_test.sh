#!/usr/bin/env bash
# Hermetic structural test for the remaining dev-public scripts:
# - setup-dev-public-dns.sh (Phase 0 — CF API DNS upsert)
# - caddy-up-dev-public.sh   (Phase 1 — Caddyfile sync + reload)
#
# Validates bash -n syntax + flag wiring + idempotency contracts.
# Runs in <1s. NO network, NO Cloudflare API, NO caddy.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DNS_SCRIPT="$REPO_ROOT/infra/scripts/local/setup-dev-public-dns.sh"
CADDY_SCRIPT="$REPO_ROOT/infra/scripts/local/caddy-up-dev-public.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0

assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n' "$desc"
    FAIL=$((FAIL + 1))
  fi
}

# ============================================================================
# setup-dev-public-dns.sh
# ============================================================================
printf '== setup-dev-public-dns.sh ==\n'

[[ -f "$DNS_SCRIPT" ]] || { printf 'FAIL: %s missing\n' "$DNS_SCRIPT"; exit 1; }
DNS_CONTENT=$(<"$DNS_SCRIPT")

if bash -n "$DNS_SCRIPT" 2>/dev/null; then
  assert "dns bash -n parses" 1
else
  assert "dns bash -n parses" 0
fi

# Flags
if printf '%s' "$DNS_CONTENT" | grep -q '\-\-dry-run'; then
  assert "dns --dry-run flag wired" 1
else
  assert "dns --dry-run flag wired" 0
fi

if printf '%s' "$DNS_CONTENT" | grep -q '\-\-verify'; then
  assert "dns --verify flag wired" 1
else
  assert "dns --verify flag wired" 0
fi

# Required env vars
if printf '%s' "$DNS_CONTENT" | grep -q 'CLOUDFLARE_API_TOKEN'; then
  assert "dns requires CLOUDFLARE_API_TOKEN" 1
else
  assert "dns requires CLOUDFLARE_API_TOKEN" 0
fi

if printf '%s' "$DNS_CONTENT" | grep -q 'DROPLET_IP'; then
  assert "dns requires DROPLET_IP" 1
else
  assert "dns requires DROPLET_IP" 0
fi

# 3 dev subdomains hard-coded (intentional — these are the canonical set)
for sub in auth-dev app-dev cloud-dev; do
  if printf '%s' "$DNS_CONTENT" | grep -q "for sub in.*$sub"; then
    assert "dns processes $sub subdomain" 1
  else
    assert "dns processes $sub subdomain" 0
  fi
done

# Idempotent upsert pattern: GET id → if exists PATCH else POST
if printf '%s' "$DNS_CONTENT" | grep -q 'PATCH'; then
  assert "dns uses PATCH for existing records (idempotent)" 1
else
  assert "dns uses PATCH for existing records (idempotent)" 0
fi

if printf '%s' "$DNS_CONTENT" | grep -q 'POST.*dns_records'; then
  assert "dns uses POST for new records" 1
else
  assert "dns uses POST for new records" 0
fi

# proxied=false (LE HTTP-01 challenge requires direct origin)
if printf '%s' "$DNS_CONTENT" | grep -q 'proxied":false'; then
  assert "dns sets proxied=false (preserves ACME HTTP-01)" 1
else
  assert "dns sets proxied=false (preserves ACME HTTP-01)" 0
fi

# Allowlist
if grep -q 'setup-dev-public-dns.sh' "$ALLOWLIST"; then
  assert "dns in allowlist" 1
else
  assert "dns in allowlist" 0
fi

# ============================================================================
# caddy-up-dev-public.sh
# ============================================================================
printf '\n== caddy-up-dev-public.sh ==\n'

[[ -f "$CADDY_SCRIPT" ]] || { printf 'FAIL: %s missing\n' "$CADDY_SCRIPT"; exit 1; }
CADDY_CONTENT=$(<"$CADDY_SCRIPT")

if bash -n "$CADDY_SCRIPT" 2>/dev/null; then
  assert "caddy bash -n parses" 1
else
  assert "caddy bash -n parses" 0
fi

# Flags
if printf '%s' "$CADDY_CONTENT" | grep -q '\-\-dry-run'; then
  assert "caddy --dry-run flag wired" 1
else
  assert "caddy --dry-run flag wired" 0
fi

if printf '%s' "$CADDY_CONTENT" | grep -q '\-\-reload'; then
  assert "caddy --reload flag wired" 1
else
  assert "caddy --reload flag wired" 0
fi

if printf '%s' "$CADDY_CONTENT" | grep -q '\-\-verify'; then
  assert "caddy --verify flag wired" 1
else
  assert "caddy --verify flag wired" 0
fi

# Canonical → runtime sync (canonical infra/local/Caddyfile, runtime .local/Caddyfile)
if printf '%s' "$CADDY_CONTENT" | grep -q 'infra/local/Caddyfile'; then
  assert "caddy reads canonical infra/local/Caddyfile" 1
else
  assert "caddy reads canonical infra/local/Caddyfile" 0
fi

if printf '%s' "$CADDY_CONTENT" | grep -q '\.local/Caddyfile'; then
  assert "caddy writes runtime .local/Caddyfile" 1
else
  assert "caddy writes runtime .local/Caddyfile" 0
fi

# Idempotency: cmp -s to detect no-op (saves unnecessary write)
if printf '%s' "$CADDY_CONTENT" | grep -q 'cmp -s'; then
  assert "caddy uses cmp -s for idempotent sync" 1
else
  assert "caddy uses cmp -s for idempotent sync" 0
fi

# Reload vs spawn: reload if already running
if printf '%s' "$CADDY_CONTENT" | grep -q 'caddy reload'; then
  assert "caddy reloads if already running (preserves cert cache)" 1
else
  assert "caddy reloads if already running (preserves cert cache)" 0
fi

# TLS verification (LE issuer)
if printf '%s' "$CADDY_CONTENT" | grep -qi "let.s encrypt"; then
  assert "caddy --verify asserts LE issuer" 1
else
  assert "caddy --verify asserts LE issuer" 0
fi

# 6 hostnames in verify
hosts_count=$(printf '%s' "$CADDY_CONTENT" \
  | grep -oE 'auth(-dev)?\.loopzero\.dev|app(-dev)?\.loopzero\.dev|cloud(-dev)?\.loopzero\.dev' \
  | sort -u | wc -l)
if [[ "$hosts_count" -ge 6 ]]; then
  assert "caddy verifies all 6 hostnames (found $hosts_count)" 1
else
  assert "caddy verifies all 6 hostnames (found $hosts_count)" 0
fi

# Allowlist
if grep -q 'caddy-up-dev-public.sh' "$ALLOWLIST"; then
  assert "caddy in allowlist" 1
else
  assert "caddy in allowlist" 0
fi

printf '\n== summary ==\n'
printf '  PASS: %d\n' "$PASS"
printf '  FAIL: %d\n' "$FAIL"

[[ "$FAIL" -eq 0 ]] || exit 1
