#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-20 T2.5 ext — comprehensive pattern audit lock-in.
#
# This complements helm-schemas-negative_test.sh (which tests SCHEMA-level
# constraints) by exercising each UNIQUE regex pattern across the chart
# corpus systematically. Catches shadow bugs introduced when a chart
# copies a pattern from another schema and the pattern was wrong.
#
# Each pattern below has:
#   - Documentation (what it constrains)
#   - Used-by list (which charts reference it)
#   - Positive cases (must accept)
#   - Negative cases (must reject)
#
# Exit codes:
#   0 — All patterns behave as documented
#   1 — Pattern accepted something it should reject (or vice versa)
#   5 — Missing test dependency

set -euo pipefail

if ! command -v python3 >/dev/null; then
  echo "[helm-patterns-audit] ERROR: python3 required" >&2
  exit 5
fi

TOTAL_PASS=0
TOTAL_FAIL=0

log() { printf '[helm-patterns-audit] %s\n' "$*" >&2; }

# audit_pattern PATTERN LABEL ACCEPT_CASES_JSON REJECT_CASES_JSON
audit_pattern() {
  local pattern="$1"
  local label="$2"
  local accept_json="$3"
  local reject_json="$4"

  local pass fail
  read -r pass fail < <(python3 -c "
import json, re, sys
pat = sys.argv[1]
try:
    rx = re.compile(pat)
except re.error as e:
    print(f'0 1', file=sys.stdout)
    print(f'[INVALID PATTERN] {sys.argv[2]}: {e}', file=sys.stderr)
    sys.exit(0)
accepts = json.loads(sys.argv[3])
rejects = json.loads(sys.argv[4])
p = f = 0
for v in accepts:
    if rx.match(v):
        p += 1
    else:
        f += 1
        print(f'[REGRESS] should ACCEPT but rejected: {v!r}', file=sys.stderr)
for v in rejects:
    if not rx.match(v):
        p += 1
    else:
        f += 1
        print(f'[REGRESS] should REJECT but accepted: {v!r}', file=sys.stderr)
print(f'{p} {f}')
" "$pattern" "$label" "$accept_json" "$reject_json")

  TOTAL_PASS=$((TOTAL_PASS + pass))
  TOTAL_FAIL=$((TOTAL_FAIL + fail))

  if [[ "$fail" -eq 0 ]]; then
    log "  [PASS] $label ($pass cases)"
  else
    log "  [FAIL] $label ($pass pass, $fail fail)"
  fi
}

# ============================================================================
# Pattern 1: OCI image repository (used in 8 charts: 6 OCI operator charts +
#   loopzero-api + loopzero-cloud + loopzero-dashboard)
# ============================================================================
audit_pattern \
  '^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$' \
  "OCI image repo (8 charts)" \
  '["loopzero-api", "library/loopzero-api", "registry.digitalocean.com/loopzero-system/loopzero-api", "a", "abc/abc/abc/abc/abc", "with-dash/repo_name.suffix"]' \
  '[".bad", "bad.", "a//b", "a/-b", "a/b-", "a/.b", "a/b.", "a:tag", "Capital/abc", "abc/", "/abc", "a/b "]'

# ============================================================================
# Pattern 2: sha256 digest (used in 7 charts)
# ============================================================================
audit_pattern \
  '^sha256:[a-f0-9]{64}$' \
  "sha256 digest (7 charts)" \
  '["sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sha256:REDACTED_SECRET_010"]' \
  '["sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "sha256:aaaaa", "sha512:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sha256:", "sha256:gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"]'

# ============================================================================
# Pattern 3: URL pattern for fulcio/rekor/oidc (used in 6 fields across 2 charts)
# ============================================================================
audit_pattern \
  '^https?://[^\s/$.?#].[^\s]*$' \
  "fulcio/rekor/oidc URL (6 fields)" \
  '["https://fulcio.sigstore.dev", "https://rekor.sigstore.dev", "https://token.actions.githubusercontent.com", "http://example.com/path", "https://a.b"]' \
  '["https:///path", "https://#fragment", "https://?query", "https://.dot", "https:// space", "ftp://example.com", "not-a-url", ""]'

# ============================================================================
# Pattern 4: Registry prefix trailing slash anchored (used in 4 fields,
#   admission-policies + loopzero-admission-policies)
# ============================================================================
audit_pattern \
  '^[^\s]+/$' \
  "Registry prefix trailing-slash (4 fields)" \
  '["registry.digitalocean.com/loopzero-system/", "harbor.dev.loopzero.example/loopzero-platform/", "k3d-loopzero-registry:5000/tenant/", "x/"]' \
  '["no-trailing-slash", "with space/", "/", "", "x"]'

# ============================================================================
# Pattern 5: Registry FQDN (used in 3 fields, postgresql + cloudflared +
#   argo-workflows cloneRunner)
# ============================================================================
audit_pattern \
  '^[a-z0-9][a-z0-9.-]*(/[a-z0-9._-]+)*$' \
  "Registry FQDN (3 fields)" \
  '["docker.io", "registry.digitalocean.com", "registry.digitalocean.com/loopzero-system", "cloudflare/cloudflared", "bitnamilegacy/postgresql"]' \
  '[".leading-dot", "-leading-dash", "UPPER.com", "registry.com/", "registry.com//double", "registry.com/path with space"]'

# ============================================================================
# Pattern 6: Ory uiBaseUrl tightened (used in 2 charts: kratos + hydra, fixed
#   2026-05-20 from old greedy '[^/]+' pattern)
# ============================================================================
audit_pattern \
  '^https?://[a-z0-9][a-z0-9.-]*(:[0-9]+)?$' \
  "Ory uiBaseUrl tightened (2 charts)" \
  '["http://localhost:4455", "http://localhost", "https://app.loopzero-drill.example", "https://app.loopzero.example:443"]' \
  '["http://localhost:abc", "http://localhost:4455:8080", "http://localhost?query", "http://localhost#frag", "http://user:pass@localhost", "http://localhost/", "https://UPPER.com"]'

# ============================================================================
# Pattern 7: PG identifier (lowercase + underscore, used in 2 fields,
#   postgresql auth.username + auth.database)
# ============================================================================
audit_pattern \
  '^[a-z_][a-z0-9_]*$' \
  "PG identifier (2 fields)" \
  '["loopzero", "loopzero_db", "user_1", "_underscore_leading", "a"]' \
  '["LoopZero", "1loopzero", "loopzero-db", "loopzero db", "", "loopzero.db"]'

# ============================================================================
# Pattern 8: RFC 1123 DNS label (used in 4 fields: tokenSecret name, secret
#   name, namespace name)
# ============================================================================
audit_pattern \
  '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' \
  "RFC 1123 DNS label (4 fields)" \
  '["loopzero-system", "default", "argocd", "a", "ab", "a1-b2-c3"]' \
  '["-leading-dash", "trailing-dash-", "UPPER-CASE", "with.dot", "with space", ""]'

# ============================================================================
# Pattern 9: SemVer with optional v prefix (used in 1 field: argocd image.tag)
# ============================================================================
audit_pattern \
  '^v?[0-9]+(\.[0-9]+){2}(-[a-z0-9.-]+)?$' \
  "SemVer image tag (1 field)" \
  '["v2.13.3", "2.13.3", "v1.0.0", "v2.13.3-rc1", "v2.13.3-pre.alpha.1"]' \
  '["latest", "v2.13", "2", "v2.13.3.4", "V2.13.3"]'

# ============================================================================
# Pattern 10: Email anchored (used in cluster-issuer acme.email)
# ============================================================================
audit_pattern \
  '^[^@\s]+@[^@\s]+\.[^@\s]+$' \
  "Email anchored (1 field)" \
  '["ops@loopzero.example", "a@b.c", "name.lastname@subdomain.example.org"]' \
  '["not-an-email", "@b.c", "a@.c", "a@b.", "a b@c.d", "a@b c.d", "two@@b.c"]'

# ============================================================================
# Pattern 11: Bucket name (used in 4 fields: s3 buckets, sourceBucket)
# ============================================================================
audit_pattern \
  '^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$' \
  "S3 bucket name (4 fields)" \
  '["a", "ab", "loopzero-builds", "loopzero-static-assets", "ab-cd-12-34"]' \
  '["-leading", "trailing-", "UPPER", "with.dot", "with space", ""]'

# ============================================================================
# Pattern 12: chaosDaemon socketPath (used in 1 field, chaos-mesh)
# ============================================================================
audit_pattern \
  '^/[a-zA-Z0-9._/-]+\.sock$' \
  "Linux socket path (1 field)" \
  '["/run/containerd/containerd.sock", "/run/k3s/containerd/containerd.sock", "/var/run/docker.sock"]' \
  '["containerd.sock", "/no-suffix", "/path/file.txt", "/path/with space.sock"]'

# ============================================================================
# Summary
# ============================================================================
echo ""
echo "Total pattern audit cases: $((TOTAL_PASS + TOTAL_FAIL))"
echo "Passed: $TOTAL_PASS"
echo "Failed: $TOTAL_FAIL"

if [[ "$TOTAL_FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
