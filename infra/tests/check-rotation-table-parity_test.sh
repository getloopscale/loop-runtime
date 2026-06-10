#!/usr/bin/env bash
# check-rotation-table-parity_test.sh — Parity gate for iter 17 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8.
#
# Validates that the embedded ROTATION_TABLE in rotate-secret.sh is in
# sync with the canonical ExternalSecret inventory at
# infra/helm/loopzero-external-secrets/values.yaml.
#
# Why this matters:
# - rotate-secret.sh embeds a static table mapping Vault D6 path → consumer
#   deployments + ExternalSecret CR names. EC-22 typo guard rejects rotation
#   for paths NOT in the table — protecting against silent failure modes.
# - But that protection becomes a DOS if someone adds a NEW ExternalSecret to
#   the inventory and forgets to update the rotation table. Operator tries to
#   rotate the new secret → script fails with "path not in rotation table"
#   right when they're under incident pressure.
# - This test catches the drift at CI time: every path in the inventory MUST
#   exist in the rotation table.
#
# Failure mode this prevents: silent operational drift between two SoTs that
# must stay aligned. EC-22 mitigation only works if ROTATION_TABLE is kept
# fresh.
#
# Test logic:
#   1. Extract all Vault paths from values.yaml inventory:
#      <vaultBinary>/<vaultCategory>/<vaultKey> (with vaultBinaryOverride
#      applied per-key).
#   2. Extract all paths from ROTATION_TABLE in rotate-secret.sh.
#   3. Assert inventory-paths ⊆ rotation-table-paths.
#   4. Also: warn (not fail) if rotation table has paths NOT in inventory
#      (extras may be legitimate for future-state paths).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ROTATE_SCRIPT="$REPO_ROOT/scripts/openbao/rotate-secret.sh"
INVENTORY="$REPO_ROOT/helm/loopzero-external-secrets/values.yaml"

# Pre-flight
for tool in yq awk grep sort comm; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: $tool required"; exit 2; }
done
[[ -f "$ROTATE_SCRIPT" ]] || { echo "FATAL: rotate-secret.sh not found at $ROTATE_SCRIPT"; exit 2; }
[[ -f "$INVENTORY" ]] || { echo "FATAL: inventory not found at $INVENTORY"; exit 2; }

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# ----------------------------------------------------------------------------
# Step 1: Extract paths from canonical inventory
# Path = <effective-binary>/<vaultCategory>/<vaultKey>
# effective-binary = vaultBinaryOverride if set, else vaultBinary
# ----------------------------------------------------------------------------
es_count=$(yq '.externalSecrets | length' "$INVENTORY")

inventory_paths="$WORKDIR/inventory_paths"
: > "$inventory_paths"

for ((i=0; i<es_count; i++)); do
  es_binary=$(yq ".externalSecrets[$i].vaultBinary" "$INVENTORY")
  es_category=$(yq ".externalSecrets[$i].vaultCategory" "$INVENTORY")
  data_len=$(yq ".externalSecrets[$i].data | length" "$INVENTORY")

  for ((j=0; j<data_len; j++)); do
    vault_key=$(yq ".externalSecrets[$i].data[$j].vaultKey" "$INVENTORY")
    override=$(yq ".externalSecrets[$i].data[$j].vaultBinaryOverride // \"\"" "$INVENTORY")

    effective_binary="$es_binary"
    if [ -n "$override" ] && [ "$override" != "null" ]; then
      effective_binary="$override"
    fi

    echo "$effective_binary/$es_category/$vault_key" >> "$inventory_paths"
  done
done

sort -u "$inventory_paths" -o "$inventory_paths"
inventory_count=$(wc -l < "$inventory_paths")

# ----------------------------------------------------------------------------
# Step 2: Extract paths from rotate-secret.sh ROTATION_TABLE
# Pattern: <path>|<binaries>|<externalsecrets>
# ----------------------------------------------------------------------------
rotation_paths="$WORKDIR/rotation_paths"
awk '
  /^ROTATION_TABLE=\$\(cat <<.EOF/ { in_table=1; next }
  /^EOF/ && in_table { in_table=0 }
  in_table && /\|/ { split($0, a, "|"); print a[1] }
' "$ROTATE_SCRIPT" | sort -u > "$rotation_paths"
rotation_count=$(wc -l < "$rotation_paths")

# ----------------------------------------------------------------------------
# Step 3: Compute set differences
# ----------------------------------------------------------------------------
inventory_only="$WORKDIR/inventory_only"
rotation_only="$WORKDIR/rotation_only"

# Paths in inventory NOT in rotation table = MISSING (FAIL)
comm -23 "$inventory_paths" "$rotation_paths" > "$inventory_only"
missing_count=$(wc -l < "$inventory_only" | tr -d ' ')

# Paths in rotation table NOT in inventory = EXTRA (WARN)
comm -13 "$inventory_paths" "$rotation_paths" > "$rotation_only"
extra_count=$(wc -l < "$rotation_only" | tr -d ' ')

# ----------------------------------------------------------------------------
# Report
# ----------------------------------------------------------------------------
echo "Running rotation-table parity test..."
echo ""
echo "  Inventory paths (loopzero-external-secrets/values.yaml): $inventory_count"
echo "  Rotation table paths (rotate-secret.sh):             $rotation_count"
echo ""

PASS=1

if [ "$missing_count" != "0" ]; then
  echo "  FAIL: $missing_count path(s) in inventory MISSING from ROTATION_TABLE:"
  sed 's/^/    - /' "$inventory_only"
  echo ""
  echo "  Fix: add the missing paths to ROTATION_TABLE in rotate-secret.sh:"
  while IFS= read -r path; do
    echo "    $path|<deployment-name(s)>|<externalsecret-cr-name(s)>"
  done < "$inventory_only"
  echo ""
  echo "  Without this fix, operator running rotate-secret.sh on these paths"
  echo "  will hit EC-22 typo guard ('not in rotation table') as silent DOS."
  PASS=0
fi

if [ "$extra_count" != "0" ]; then
  echo "  WARN: $extra_count extra path(s) in ROTATION_TABLE not in inventory:"
  sed 's/^/    - /' "$rotation_only"
  echo "  These may be legitimate future-state paths (no FAIL emitted)."
  echo "  Review and clean up if they're stale."
  echo ""
fi

echo "═══════════════════════════════════════════════════════════"
if [ "$PASS" -eq 1 ]; then
  echo "  Verdict: PASS — inventory ⊆ rotation table"
  echo "═══════════════════════════════════════════════════════════"
  exit 0
else
  echo "  Verdict: FAIL — drift detected"
  echo "═══════════════════════════════════════════════════════════"
  exit 1
fi
