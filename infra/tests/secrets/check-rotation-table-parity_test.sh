#!/usr/bin/env bash
# check-rotation-table-parity_test.sh — every ExternalSecret a rotation can
# reach has a restart entry in rotate-secret.sh, and every entry names one.
#
# What is in parity, and why it is only this:
# - rotate-secret.sh resolves which ExternalSecrets read a D6 path from the
#   chart inventory (infra/helm/loopzero-external-secrets/values.yaml) itself, so
#   the path list has ONE source and cannot drift. It used to be a hand-kept
#   copy in the script; that copy refreshed the operator's ExternalSecret for
#   the api's gitea token, which the operator does not read (ff237c9b4).
# - What the chart does not say is which Deployments consume each target
#   Secret. That is the one table the script keeps, RESTARTS_BY_EXTERNALSECRET,
#   keyed by the inventory's targetSecret.
#
# The two directions fail differently and both fail:
# - an inventory ExternalSecret missing from the table: the script refuses the
#   rotation (it will not skip a restart), so an operator under incident
#   pressure is blocked on a secret the chart already ships;
# - a table entry naming no inventory ExternalSecret: no rotation can reach
#   it, and a restart list that outlived its secret describes a platform that
#   no longer exists.
#
# Exit codes: 0 = parity, 1 = drift, 2 = a required tool or file is missing.
set -uo pipefail

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/infra"
ROTATE_SCRIPT="$REPO_ROOT/scripts/openbao/rotate-secret.sh"
INVENTORY="$REPO_ROOT/helm/loopzero-external-secrets/values.yaml"

for tool in yq awk sort comm; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: $tool required"; exit 2; }
done
[[ -f "$ROTATE_SCRIPT" ]] || { echo "FATAL: rotate-secret.sh not found at $ROTATE_SCRIPT"; exit 2; }
[[ -f "$INVENTORY" ]] || { echo "FATAL: inventory not found at $INVENTORY"; exit 2; }

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

yq '.externalSecrets[].targetSecret' "$INVENTORY" | sort -u > "$WORKDIR/inventory"
awk '
  /^RESTARTS_BY_EXTERNALSECRET=\$\(cat <<.EOF/ { in_table=1; next }
  /^EOF/ && in_table { in_table=0 }
  in_table && /\|/ { split($0, a, "|"); print a[1] }
' "$ROTATE_SCRIPT" | sort -u > "$WORKDIR/table"

inventory_count=$(wc -l < "$WORKDIR/inventory" | tr -d ' ')
table_count=$(wc -l < "$WORKDIR/table" | tr -d ' ')

echo "Running rotation-table parity test..."
echo ""
echo "  ExternalSecrets in the inventory:              $inventory_count"
echo "  ExternalSecrets in RESTARTS_BY_EXTERNALSECRET: $table_count"
echo ""

# A table that parsed to nothing would make every inventory entry "missing"
# for the wrong reason; name the real one.
if [ "$table_count" = "0" ]; then
  echo "  FAIL: RESTARTS_BY_EXTERNALSECRET parsed to zero entries — the heredoc moved or was renamed."
  exit 1
fi

comm -23 "$WORKDIR/inventory" "$WORKDIR/table" > "$WORKDIR/missing"
comm -13 "$WORKDIR/inventory" "$WORKDIR/table" > "$WORKDIR/stale"

PASS=1
if [ -s "$WORKDIR/missing" ]; then
  echo "  FAIL: ExternalSecret(s) in the inventory with no restart entry — rotate-secret.sh refuses them:"
  sed 's/^/    - /' "$WORKDIR/missing"
  echo "  Fix: add '<targetSecret>|<comma-separated-deployments>' to RESTARTS_BY_EXTERNALSECRET."
  echo ""
  PASS=0
fi
if [ -s "$WORKDIR/stale" ]; then
  echo "  FAIL: restart entr(y/ies) naming no ExternalSecret in the inventory — no rotation reaches them:"
  sed 's/^/    - /' "$WORKDIR/stale"
  echo "  Fix: remove them from RESTARTS_BY_EXTERNALSECRET."
  echo ""
  PASS=0
fi

echo "═══════════════════════════════════════════════════════════"
if [ "$PASS" -eq 1 ]; then
  echo "  Verdict: PASS — every inventory ExternalSecret has exactly one restart entry"
  echo "═══════════════════════════════════════════════════════════"
  exit 0
fi
echo "  Verdict: FAIL — drift detected"
echo "═══════════════════════════════════════════════════════════"
exit 1
