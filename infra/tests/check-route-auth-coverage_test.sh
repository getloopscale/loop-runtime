#!/usr/bin/env bash
# T-cov-1 of plan 2026-06-15-100pct-dashboard-coverage-roadmap Phase 2A
# (closes R2-F8 HIGH from the 49-findings code review).
#
# Hermetic gate: every user-facing API route in api/internal/server/router.go
# MUST be registered INSIDE the `middleware.Auth` group OR appear in the
# explicit public-allowlist below. New routes added without auth + without
# allowlist entry fail this gate.
#
# Algorithm:
#   1. Find the line range of the `middleware.Auth` group block in router.go
#      via grep + brace-counting via awk
#   2. Enumerate every `r.{Get,Post,Put,Patch,Delete}` call
#   3. For each, check: is line inside the Auth group line range?
#   4. If NOT inside Auth group, must match public-allowlist
#   5. Emit finding for routes that fail both checks
#
# Public-allowlist (by-design endpoints; see api/internal/server/router.go
# for full rationale per endpoint):
#
#   /api/v1/health           — k8s liveness probe + status pages (no auth)
#   /api/v1/health/deep      — operator deep-check (no auth)
#   /api/v1/version          — LoopZero CLI compat negotiation (no auth)
#   /internal/bootstrap      — service-auth (LoopZero platform-operator only)
#   /internal/argocd-callback — service-auth (ArgoCD notification webhook)
#   /internal/v1/secret-fetch — service-auth (Argo Workflow secret fetch)
#   /api/v1/webhooks/github  — GitHub HMAC-signature auth (NOT user JWT)
#   /api/v1/admin/tenants/evict     — admin service-auth
#   /api/v1/admin/tenants/pin       — admin service-auth
#   /api/v1/admin/releases/{id}/inspect — admin service-auth

set -euo pipefail

ROUTER=api/internal/server/router.go
ALLOWLIST=(
  "/api/v1/health"
  "/api/v1/health/deep"
  "/api/v1/version"
  "/internal/bootstrap"
  "/internal/argocd-callback"
  "/internal/v1/secret-fetch"
  "/api/v1/webhooks/github"
  "/api/v1/admin/tenants/evict"
  "/api/v1/admin/tenants/pin"
  "/api/v1/admin/releases"
)

if [ ! -f "$ROUTER" ]; then
  echo "FAIL: $ROUTER not found"
  exit 2
fi

# Step 1: find Auth group line range via awk brace-counting.
# A Go `r.Group(func(r chi.Router) { ... })` block where the function body
# contains `middleware.Auth(` is the protected group. Find its start line
# (where `r.Group(` appears with a following line containing `middleware.Auth`)
# and end line (matching `})` at brace-depth 0).

AUTH_GROUP_RANGE=$(awk '
  /r\.Group\(func\(r chi\.Router\) \{/ {
    start_line = NR
    depth = 1
    has_auth = 0
    next
  }
  start_line > 0 {
    # crude brace counting (good enough for chi router idiom).
    # NOTE: `close` is a reserved word in awk; use `closes` instead.
    opens = gsub(/\{/, "{")
    closes = gsub(/\}/, "}")
    depth += opens - closes
    if (/middleware\.Auth/) has_auth = 1
    if (depth == 0) {
      if (has_auth) {
        printf "%d %d\n", start_line, NR
      }
      start_line = 0
    }
  }
' "$ROUTER")

if [ -z "$AUTH_GROUP_RANGE" ]; then
  echo "FAIL: could not locate the middleware.Auth group block in $ROUTER"
  exit 1
fi

AUTH_START=$(echo "$AUTH_GROUP_RANGE" | awk '{print $1}')
AUTH_END=$(echo "$AUTH_GROUP_RANGE" | awk '{print $2}')

# Step 1b: locate `registerProtectedRoutes` function body line range.
# This helper function is called from within the Auth group (lines ~250)
# so every route registered inside it is also auth-protected. Treat the
# function body as part of the protected range.
PROTECTED_FUNC_RANGE=$(awk '
  /^func registerProtectedRoutes\(/ {
    start_line = NR
    depth = 0
    in_body = 0
    next
  }
  start_line > 0 {
    opens = gsub(/\{/, "{")
    closes = gsub(/\}/, "}")
    if (!in_body && opens > 0) in_body = 1
    depth += opens - closes
    if (in_body && depth == 0) {
      printf "%d %d\n", start_line, NR
      exit
    }
  }
' "$ROUTER")

PROT_START=0
PROT_END=0
if [ -n "$PROTECTED_FUNC_RANGE" ]; then
  PROT_START=$(echo "$PROTECTED_FUNC_RANGE" | awk '{print $1}')
  PROT_END=$(echo "$PROTECTED_FUNC_RANGE" | awk '{print $2}')
fi

echo "[auth-coverage] middleware.Auth group spans lines $AUTH_START..$AUTH_END"
echo "[auth-coverage] registerProtectedRoutes spans lines $PROT_START..$PROT_END"

# Step 2-4: enumerate routes + check coverage
PASS=0
FAIL=0
VIOLATIONS=()

while IFS=: read -r line_no rest; do
  # Extract the route path (best-effort grep)
  route=$(echo "$rest" | grep -oE '"/[^"]+"' | head -1 | tr -d '"')
  [ -z "$route" ] && continue

  # Check if inside Auth group OR inside registerProtectedRoutes function
  if [ "$line_no" -ge "$AUTH_START" ] && [ "$line_no" -le "$AUTH_END" ]; then
    PASS=$((PASS+1))
    continue
  fi
  if [ "$PROT_START" -gt 0 ] && [ "$line_no" -ge "$PROT_START" ] && [ "$line_no" -le "$PROT_END" ]; then
    PASS=$((PASS+1))
    continue
  fi

  # Check allowlist (prefix match)
  in_allowlist=0
  for allowed in "${ALLOWLIST[@]}"; do
    if [[ "$route" == "$allowed" || "$route" == "$allowed"/* ]]; then
      in_allowlist=1
      break
    fi
  done

  if [ "$in_allowlist" -eq 1 ]; then
    PASS=$((PASS+1))
  else
    FAIL=$((FAIL+1))
    VIOLATIONS+=("router.go:$line_no  $route  (NOT in Auth group, NOT in allowlist)")
  fi
done < <(grep -nE '^\s*r\.(Get|Post|Put|Patch|Delete|Method)\(' "$ROUTER")

# Step 5: report
if [ "$FAIL" -gt 0 ]; then
  echo
  echo "[auth-coverage] FAIL — $FAIL routes lack auth + lack allowlist entry:"
  for v in "${VIOLATIONS[@]}"; do
    echo "  $v"
  done
  echo
  echo "Fix: either move the route inside the middleware.Auth group OR add"
  echo "an explicit ALLOWLIST entry above with rationale comment."
  exit 1
fi

echo "[auth-coverage] PASS — $PASS routes either in Auth group or allowlisted"
