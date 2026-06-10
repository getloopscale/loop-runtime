#!/usr/bin/env bash
# T2.3 of plan 2026-05-29-dashboard-public-integration-canonicalization
# Golden test: every dashboardRoute value across helmfile env yamls
# must map to a real React route in cloud/dashboard/src/app.tsx.
#
# Without this guard, Kratos can be configured to redirect to /registration
# while the React app only has /register → 404 on signup (the 2026-05-29
# LIVE bug).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

ROUTES_FILE=cloud/dashboard/src/app.tsx
ENV_DIR=infra/helmfile/environments

if [ ! -f "$ROUTES_FILE" ]; then
  echo "FAIL: $ROUTES_FILE not found"
  exit 1
fi

# Extract React routes (path prop in <Route>)
ROUTES=$(grep -oE 'Route path="/[a-zA-Z-]*"' "$ROUTES_FILE" | sed -E 's/.*path="([^"]+)".*/\1/' | sort -u)
echo "Discovered React routes: $(echo $ROUTES | tr ' ' ',')"

PASS=0
FAIL=0
FAIL_MSGS=()

for env_yaml in $ENV_DIR/prod.yaml $ENV_DIR/drill.yaml $ENV_DIR/standalone-public.yaml; do
  if [ ! -f "$env_yaml" ]; then
    continue
  fi
  echo ""
  echo "=== $(basename $env_yaml) ==="
  # Extract dashboardRoute values via python (handles multiline yaml)
  while IFS='=' read -r key value; do
    [ -z "$value" ] && continue
    # Check value is in ROUTES set
    if echo "$ROUTES" | tr ' ' '\n' | grep -Fxq "$value"; then
      echo "  PASS: dashboardRoute.$key=$value maps to a real React route"
      PASS=$((PASS+1))
    else
      echo "  FAIL: dashboardRoute.$key=$value does NOT match any React route"
      FAIL=$((FAIL+1))
      FAIL_MSGS+=("$(basename $env_yaml): $key=$value not in [$ROUTES]")
    fi
  done < <(python3 -c "
import yaml
d = yaml.safe_load(open('$env_yaml')) or {}
route = d.get('dashboardRoute') or {}
for k, v in route.items():
    print(f'{k}={v}')
")
done

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "Failures detail:"
  for msg in "${FAIL_MSGS[@]}"; do
    echo "  - $msg"
  done
fi
echo "============================="
[ "$FAIL" -eq 0 ]
