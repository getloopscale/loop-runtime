#!/usr/bin/env bash
# T3.1 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
# CR-7 (MEET-2026-05-28-001 FINDING-SEC-M07-005): the original Sigstore
# NetworkPolicies used a bare namespaceSelector — any pod in the
# sourceNamespace (default: argo) could reach Fulcio/Rekor. This test
# locks the refactor: rendered Fulcio + Rekor NPs MUST combine
# namespaceSelector AND podSelector inside a single `from` entry
# (AND semantics in NetworkPolicy v1) so only Argo-controller-managed
# workflow pods reach Sigstore.
#
# Hermetic: just `helm template` + grep / yq assertions on rendered YAML.
# CTLog is exempt per the comment in networkpolicy-ctlog.yaml — its only
# ingress source is fulcio-system (operator-managed); no podSelector
# narrowing buys security there.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
CHART_DIR="$REPO_ROOT/infra/helm/sigstore-extras"

if ! command -v helm >/dev/null 2>&1; then
  echo "[sigstore-extras-podselector] SKIP: helm CLI not in PATH"
  exit 0
fi

if [ ! -d "$CHART_DIR" ]; then
  echo "[sigstore-extras-podselector] SKIP: chart dir $CHART_DIR missing"
  exit 0
fi

RENDERED=$(helm template sigstore-extras "$CHART_DIR" 2>&1) || {
  echo "[sigstore-extras-podselector] FAIL: helm template failed" >&2
  echo "$RENDERED" >&2
  exit 1
}

failures=0

# Helpers ------------------------------------------------------------------

# fail prints + counts a failure.
fail() {
  echo "[sigstore-extras-podselector] FAIL: $*" >&2
  failures=$((failures + 1))
}

# extract_resource isolates one NetworkPolicy YAML doc from the multi-doc
# helm template output, identified by metadata.name. Splits on `---`
# separators and picks docs that contain the target name token.
extract_resource() {
  local name="$1"
  awk -v name="$name" '
    BEGIN { RS = "\n---\n"; ORS = "\n---\n" }
    $0 ~ "name: " name "\n" || $0 ~ "name: " name "$" { print }
  ' <<<"$RENDERED"
}

# require_combined_selector confirms that the YAML doc for the given NP name
# contains BOTH namespaceSelector AND podSelector inside the SAME `from`
# array entry — i.e. they are sibling YAML map keys of the same from-list
# element. The structural rule: in NP v1, sibling selectors in one `from`
# entry combine with AND semantics; separate from-array entries are OR.
# The CR-7 security gap was the OR form, where any pod in argo could bypass.
#
# Detection: find a `- namespaceSelector:` line followed by a sibling
# `podSelector:` at the same map-key indent within 8 subsequent lines.
# The rendered indent for the namespaceSelector dash-element is 8 spaces;
# its sibling podSelector indents to 10 spaces (one map-level deeper of
# the dash element, same level as namespaceSelector's content). awk
# walks both indent profiles.
require_combined_selector() {
  local label="$1" name="$2"
  local doc
  doc=$(extract_resource "$name")
  if [ -z "$doc" ]; then
    fail "$label — could not isolate NP $name in rendered YAML"
    return
  fi
  if ! awk '
    /^[[:space:]]+-[[:space:]]+namespaceSelector:/ { ns = NR; ns_indent = match($0, /[^ ]/) - 1; next }
    ns > 0 {
      if ((NR - ns) > 8) { ns = 0; next }
      if ($0 ~ /^[[:space:]]+podSelector:/) {
        # Sibling indent = namespaceSelector "- " removed (its content
        # indents 2 more than the dash). podSelector should indent to
        # the same column as namespaceSelector itself (without the dash).
        pod_indent = match($0, /[^ ]/) - 1
        # Accept any indent strictly greater than the dash column (4-12
        # are all reasonable across YAML emitters) — we just need them
        # to be inside the same from-array element block.
        if (pod_indent > ns_indent - 2) { print "MATCH"; exit }
      }
    }
  ' <<<"$doc" | grep -q MATCH; then
    fail "$label — NetworkPolicy $name still uses bare namespaceSelector (no sibling podSelector inside the same ingress.from entry — CR-7 fix missing)"
  fi
}

# require_pod_label_key confirms the value-substituted ingress.podLabel.key
# is the canonical Argo controller-managed label.
require_pod_label_key() {
  if ! grep -q "key: workflows.argoproj.io/workflow" <<<"$RENDERED"; then
    fail "expected `key: workflows.argoproj.io/workflow` in rendered podSelector matchExpressions (CR-7 contract)"
  fi
}

# Assertions ---------------------------------------------------------------

require_combined_selector "Fulcio" "allow-argo-to-fulcio"
require_combined_selector "Rekor"  "allow-argo-to-rekor"
require_pod_label_key

# Negative assertion: CTLog must NOT have the combined-selector refactor
# (its ingress is operator-internal, see networkpolicy-ctlog.yaml comment).
ctlog_doc=$(extract_resource "allow-sigstore-to-ctlog")
if echo "$ctlog_doc" | awk '
  /^    - namespaceSelector:/    { ns = NR; next }
  ns > 0 && /^          podSelector:/   { print "MATCH"; exit }
  ns > 0 && (NR - ns) > 10       { ns = 0 }
' | grep -q MATCH; then
  fail "CTLog ingress was unexpectedly narrowed via podSelector — per the comment in networkpolicy-ctlog.yaml, its only source is fulcio-system (operator-managed) and the narrowing buys no security. Revert or update the contract."
fi

# Final result -------------------------------------------------------------

if [ "$failures" -gt 0 ]; then
  echo "[sigstore-extras-podselector] $failures assertion(s) FAILED" >&2
  exit 1
fi

echo "[sigstore-extras-podselector] PASS — Fulcio+Rekor narrowed via combined namespaceSelector AND podSelector; CTLog correctly exempt"
