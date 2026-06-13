#!/usr/bin/env bash
# Hermetic test: loopzero-api BASE_DOMAIN / LOOPZERO_PUBLIC_DOMAIN canonical split
# per-env, plus pkg/naming.AppURL single-source-of-truth delegation.
#
# Closes catches #21+#22 from T6.2 session 2026-06-12 (sponsor walkthrough
# caught dashboard rendering `teste-production.loopzero.local` for production
# deploys in dev-public). Final FAANG canonical fix REPLACES the v1 patch-
# the-symptom approach (which bent BASE_DOMAIN to carry the public wildcard
# and broke the IngressRoute Host match via Caddy Host-rewrite chain).
#
# ADR D3 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability:
#   BASE_DOMAIN        = cluster-internal SNI for IngressRoute Host rule.
#                        Caddy rewrites public Host header to <sub>.<BASE_DOMAIN>
#                        before forwarding to Traefik — IngressRoute MUST
#                        be emitted under loopzero.local for the rewrite to land.
#   LOOPZERO_PUBLIC_DOMAIN = wildcard root composed into status.url + dashboard
#                        "Visit" link by pkg/naming.AppURL. Shape:
#                          https://<tenant>-<project>-<entryApp>-<env>.<PUBLIC>
#                        Matches IngressRoute Host after Caddy rewrite.
#
# Mirror of LOOPZERO_PREVIEW_PUBLIC_DOMAIN/BaseDomain split already shipped
# for J-23 previews.
#
# Asserts:
#   A1-A2: dev-public overlay has the canonical split (BASE=loopzero.local
#          + PUBLIC=prod-dev.loopzero.example)
#   A3:    dev-public overlay does NOT mis-set BASE_DOMAIN to the public
#          wildcard (v1 patch-the-symptom regression defense)
#   A4:    LOOPZERO_PREVIEW_PUBLIC_DOMAIN canonical preserved (J-23 regression
#          defense)
#   A5:    status.go uses naming.AppURL (DRY single source of truth) — no
#          inline fmt.Sprintf for the URL formula
#   A6:    renderer_helpers.go delegates appHostname to naming.AppHostname
#          (DRY single source of truth)
#   A7-A8: 4-layer drift defense — Tilt-consumed values-dev.yaml has the
#          same canonical split
#   A9-A10: Caddy *.prod-dev.loopzero.example block routes to Traefik with the
#          canonical Host rewrite
#
# Pure-text grep on source files; ZERO kubectl/helm/network.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
OVERLAY="$REPO_ROOT/infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl"
VALUES_DEV="$REPO_ROOT/infra/helm/loopzero-api/values-dev.yaml"
STATUS_GO="$REPO_ROOT/api/internal/routes/status.go"
RENDERER_HELPERS="$REPO_ROOT/api/internal/manifests/renderer_helpers.go"
CADDYFILE="$REPO_ROOT/infra/local/Caddyfile"

PASS=0
FAIL=0
log()  { printf '[base-domain-per-env] %s\n' "$*" >&2; }
pass() { printf '  PASS: %s\n' "$1"; PASS=$((PASS + 1)); }
fail() { printf '  FAIL: %s\n' "$1"; FAIL=$((FAIL + 1)); }

# A1: dev-public overlay file exists
if [[ -f $OVERLAY ]]; then
  pass "A1 dev-public loopzero-api overlay file exists"
else
  fail "A1 overlay missing at $OVERLAY"
  exit 1
fi

# A2: canonical split present in dev-public overlay
# BASE_DOMAIN=loopzero.local (cluster-internal SNI)
# LOOPZERO_PUBLIC_DOMAIN=prod-dev.loopzero.example (status.url wildcard root)
if grep -q '^  BASE_DOMAIN: "loopzero.local"' "$OVERLAY" \
   && grep -q '^  LOOPZERO_PUBLIC_DOMAIN: "prod-dev.loopzero.example"' "$OVERLAY"; then
  pass "A2 dev-public overlay declares canonical split (BASE=loopzero.local + PUBLIC=prod-dev.loopzero.example)"
else
  fail "A2 dev-public overlay missing canonical split — expected BASE_DOMAIN=loopzero.local AND LOOPZERO_PUBLIC_DOMAIN=prod-dev.loopzero.example"
fi

# A3: ANTI-REGRESSION — dev-public must NOT set BASE_DOMAIN to public wildcard
# (the v1 patch-the-symptom that broke the IngressRoute Host match)
if grep -qE '^  BASE_DOMAIN: "prod-dev\.loopzero\.dev"' "$OVERLAY"; then
  fail "A3 REGRESSION: dev-public overlay sets BASE_DOMAIN=prod-dev.loopzero.example (v1 anti-pattern — must be loopzero.local; public wildcard goes to LOOPZERO_PUBLIC_DOMAIN)"
else
  pass "A3 dev-public overlay does NOT mis-set BASE_DOMAIN to public wildcard (FAANG canonical split preserved)"
fi

# A4: LOOPZERO_PREVIEW_PUBLIC_DOMAIN canonical (regression defense for J-23)
if grep -q '^  LOOPZERO_PREVIEW_PUBLIC_DOMAIN: "preview-dev.loopzero.example"' "$OVERLAY"; then
  pass "A4 J-23 preview wildcard canonical preserved"
else
  fail "A4 LOOPZERO_PREVIEW_PUBLIC_DOMAIN canonical missing — J-23 regression"
fi

# A5: status.go DRY canonical — uses naming.AppURL, NOT inline fmt.Sprintf
if [[ -f $STATUS_GO ]]; then
  if grep -q 'naming.AppURL(' "$STATUS_GO"; then
    pass "A5 status.go delegates to naming.AppURL (DRY single source of truth)"
  else
    fail "A5 status.go does NOT use naming.AppURL — inline formula regression"
  fi
  # Also assert the inline fmt.Sprintf for the URL formula is GONE
  if grep -qE 'fmt\.Sprintf\("https://%s-%s\.%s"' "$STATUS_GO"; then
    fail "A5b REGRESSION: status.go still has inline fmt.Sprintf for the URL formula (v1 anti-pattern)"
  else
    pass "A5b status.go no longer contains inline URL formula (canonical delegation preserved)"
  fi
else
  fail "A5 status.go missing at $STATUS_GO"
fi

# A6: renderer_helpers.go DRY canonical — delegates to naming.AppHostname
if [[ -f $RENDERER_HELPERS ]]; then
  if grep -q 'naming.AppHostname(' "$RENDERER_HELPERS"; then
    pass "A6 renderer_helpers.appHostname delegates to naming.AppHostname (DRY single source of truth)"
  else
    fail "A6 renderer_helpers.appHostname does NOT use naming.AppHostname — inline composition regression"
  fi
else
  fail "A6 renderer_helpers.go missing at $RENDERER_HELPERS"
fi

# A7: 4-layer drift defense — values-dev.yaml has canonical split (Tilt-canonical)
# Per memory `feedback_tilt_owned_deployment_yaml_drift`: Tilt-rendered ConfigMap
# consumes values-dev.yaml, NOT helmfile overlay; without this layer aligned,
# helmfile overlay change is invisible to the running Tilt deployment.
if grep -qE '^  BASE_DOMAIN: "loopzero\.local"' "$VALUES_DEV" \
   && grep -qE '^  LOOPZERO_PUBLIC_DOMAIN: "prod-dev\.loopzero\.dev"' "$VALUES_DEV"; then
  pass "A7 4-layer drift defense: values-dev.yaml declares canonical split (Tilt-canonical)"
else
  fail "A7 4-LAYER DRIFT: values-dev.yaml MUST declare BASE_DOMAIN=loopzero.local AND LOOPZERO_PUBLIC_DOMAIN=prod-dev.loopzero.example (Tilt consumes this; helmfile overlay alone is insufficient)"
fi

# A8: ANTI-REGRESSION on values-dev.yaml — must NOT mis-set BASE_DOMAIN to wildcard
if grep -qE '^  BASE_DOMAIN: "prod-dev\.loopzero\.dev"' "$VALUES_DEV"; then
  fail "A8 REGRESSION: values-dev.yaml sets BASE_DOMAIN=prod-dev.loopzero.example (v1 anti-pattern; public wildcard goes to LOOPZERO_PUBLIC_DOMAIN)"
else
  pass "A8 values-dev.yaml does NOT mis-set BASE_DOMAIN to public wildcard (4-layer canonical split preserved)"
fi

# A9: Caddy block *.prod-dev.loopzero.example declared (parallel ao J-23 preview-dev)
if grep -q '^\*\.prod-dev\.loopzero\.dev {' "$CADDYFILE"; then
  pass "A9 Caddyfile has *.prod-dev.loopzero.example wildcard block (Vercel-canonical production deploy URLs)"
else
  fail "A9 Caddyfile MISSING *.prod-dev.loopzero.example block — production URLs will NOT be reachable from browser"
fi

# A10: Host header rewrite canonical (labels.3 → loopzero.local matches BASE_DOMAIN)
if awk '/^\*\.prod-dev\.loopzero\.dev {/,/^}/' "$CADDYFILE" | grep -q 'header_up Host {http.request.host.labels.3}.loopzero.local'; then
  pass "A10 Caddy prod-dev block rewrites Host → <sub>.loopzero.local (Traefik IngressRoute match)"
else
  fail "A10 Caddy prod-dev block MISSING canonical Host header rewrite"
fi

log ""
log "Verdict: PASS=$PASS FAIL=$FAIL"
[[ $FAIL -eq 0 ]] && exit 0 || exit 1
