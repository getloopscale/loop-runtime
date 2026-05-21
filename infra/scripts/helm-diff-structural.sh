#!/usr/bin/env bash
# helm-diff-structural.sh — fail CI only on STRUCTURALLY RISKY diffs.
#
# Audit gap M06 #21 / active-execution-plan §6.E.6b:
# the helm-diff CI gate was advisory-only because the naïve diff fails
# every PR that touches charts/ (every PR produces a diff — that IS the
# point of the PR). This wrapper parses the diff output and exits non-zero
# ONLY when the change carries a structural risk that operators must
# explicitly approve:
#
#   STRUCT-1  Removal of a security-relevant resource Kind
#             (NetworkPolicy, RoleBinding, ClusterRoleBinding,
#              SecurityContextConstraints, PodSecurityPolicy)
#   STRUCT-2  Change of `namespace:` on any resource
#   STRUCT-3  Security-context downgrade:
#               runAsNonRoot:  true  → false (or removed)
#               readOnlyRootFilesystem: true → false (or removed)
#               allowPrivilegeEscalation: false → true
#               privileged: false → true (or added as true)
#
# Anything else (added Kinds, changed labels, changed image tags, added
# env vars, changed replica counts) is informational and prints to
# stdout — operator reviews in PR but CI does not block.
#
# Usage:
#   helm-diff-structural.sh <chart-name> <base-rendered-yaml> <pr-rendered-yaml>
#
# Exit codes:
#   0  No structural risk found (or no diff at all). Safe to merge.
#   1  Usage error (bad args, missing files).
#   2  Structural risk detected. CI MUST block.

set -euo pipefail

set -uo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <chart-name> <base-rendered-yaml> <pr-rendered-yaml>" >&2
  exit 1
fi

CHART="$1"
BASE="$2"
PR="$3"

if [[ ! -f "$BASE" ]]; then
  echo "::error::base-rendered-yaml not found: $BASE" >&2
  exit 1
fi
if [[ ! -f "$PR" ]]; then
  echo "::error::pr-rendered-yaml not found: $PR" >&2
  exit 1
fi

# If the two files are byte-identical, no work to do.
if diff -q "$BASE" "$PR" >/dev/null 2>&1; then
  echo "[$CHART] no diff — safe."
  exit 0
fi

violations=0

# ---------------------------------------------------------------------------
# STRUCT-1: Removal of a security-relevant resource Kind.
# ---------------------------------------------------------------------------
# Build the per-Kind line-count for each side; if the count drops to zero in
# the PR for a sensitive Kind that existed in base, that's a removal.
sensitive_kinds=(
  "NetworkPolicy"
  "RoleBinding"
  "ClusterRoleBinding"
  "SecurityContextConstraints"
  "PodSecurityPolicy"
)

for kind in "${sensitive_kinds[@]}"; do
  # Count documents in each rendering where `kind: X` appears (one per
  # document, anchored to a line start to avoid catching it inside a comment).
  base_count=$(grep -cE "^kind:[[:space:]]+${kind}[[:space:]]*$" "$BASE" || true)
  pr_count=$(grep -cE "^kind:[[:space:]]+${kind}[[:space:]]*$" "$PR" || true)

  if [[ "$base_count" -gt 0 && "$pr_count" -lt "$base_count" ]]; then
    echo "::error::[$CHART] STRUCT-1: ${kind} count dropped from ${base_count} to ${pr_count}. Removing a security-relevant resource requires explicit operator approval."
    violations=$((violations + 1))
  fi
done

# ---------------------------------------------------------------------------
# STRUCT-2: Change of `namespace:` on any resource.
# ---------------------------------------------------------------------------
# A namespace move can silently leak resources from a protected ns into a
# permissive one (or vice-versa, breaking RBAC). We compare sorted lists
# of (kind, name, namespace) tuples; any tuple that appears in BOTH with
# the same kind+name but DIFFERENT namespace is a STRUCT-2 violation.
extract_kind_name_ns() {
  local file="$1"
  # Simple awk pass: track current document's kind, name, namespace as
  # `kind:` and `name:` / `namespace:` appear under `metadata:`. We emit
  # one line per document at the YAML `---` boundary.
  awk '
    BEGIN { kind = ""; name = ""; ns = ""; in_meta = 0 }
    /^---$/ {
      if (kind != "" && name != "") {
        printf "%s|%s|%s\n", kind, name, ns
      }
      kind = ""; name = ""; ns = ""; in_meta = 0
      next
    }
    /^kind:[[:space:]]+/ {
      sub(/^kind:[[:space:]]+/, "")
      kind = $0
      next
    }
    /^metadata:[[:space:]]*$/ { in_meta = 1; next }
    in_meta && /^[[:space:]]+name:[[:space:]]+/ {
      sub(/^[[:space:]]+name:[[:space:]]+/, "")
      gsub(/"/, "")
      if (name == "") name = $0
      next
    }
    in_meta && /^[[:space:]]+namespace:[[:space:]]+/ {
      sub(/^[[:space:]]+namespace:[[:space:]]+/, "")
      gsub(/"/, "")
      ns = $0
      next
    }
    /^[a-zA-Z]/ && !/^---/ { in_meta = 0 }
    END {
      if (kind != "" && name != "") {
        printf "%s|%s|%s\n", kind, name, ns
      }
    }
  ' "$file" | sort -u
}

base_tuples=$(extract_kind_name_ns "$BASE")
pr_tuples=$(extract_kind_name_ns "$PR")

# For each (kind, name) present in both, compare the namespace component.
# We use process substitution + join with sorted (kind|name) keys.
while IFS='|' read -r kind name ns; do
  [[ -z "$kind" || -z "$name" ]] && continue
  pr_match=$(echo "$pr_tuples" | grep -E "^${kind}\\|${name}\\|" | head -1)
  if [[ -n "$pr_match" ]]; then
    pr_ns="${pr_match##*|}"
    if [[ "$ns" != "$pr_ns" ]]; then
      echo "::error::[$CHART] STRUCT-2: ${kind}/${name} namespace changed from '${ns}' to '${pr_ns}'. Namespace moves bypass RBAC and require explicit operator approval."
      violations=$((violations + 1))
    fi
  fi
done <<< "$base_tuples"

# ---------------------------------------------------------------------------
# STRUCT-3: Security-context downgrade.
# Detect line removals + line additions on the same patterns to spot
# the canonical downgrades. We grep the DIFF (not the rendered files) so
# we see only the changed lines.
# ---------------------------------------------------------------------------
diff_output=$(diff -u "$BASE" "$PR" 2>/dev/null || true)

# runAsNonRoot: true → false (or removed)
if echo "$diff_output" | grep -E "^\-[[:space:]]+runAsNonRoot:[[:space:]]+true" >/dev/null; then
  if ! echo "$diff_output" | grep -E "^\+[[:space:]]+runAsNonRoot:[[:space:]]+true" >/dev/null; then
    echo "::error::[$CHART] STRUCT-3a: runAsNonRoot:true was removed or set to false. Security downgrade requires explicit operator approval."
    violations=$((violations + 1))
  fi
fi

# readOnlyRootFilesystem: true → false (or removed)
if echo "$diff_output" | grep -E "^\-[[:space:]]+readOnlyRootFilesystem:[[:space:]]+true" >/dev/null; then
  if ! echo "$diff_output" | grep -E "^\+[[:space:]]+readOnlyRootFilesystem:[[:space:]]+true" >/dev/null; then
    echo "::error::[$CHART] STRUCT-3b: readOnlyRootFilesystem:true was removed or set to false. Security downgrade requires explicit operator approval."
    violations=$((violations + 1))
  fi
fi

# allowPrivilegeEscalation: false → true
if echo "$diff_output" | grep -E "^\+[[:space:]]+allowPrivilegeEscalation:[[:space:]]+true" >/dev/null; then
  echo "::error::[$CHART] STRUCT-3c: allowPrivilegeEscalation:true was added or flipped from false. Security downgrade requires explicit operator approval."
  violations=$((violations + 1))
fi

# privileged: false → true (or added as true)
if echo "$diff_output" | grep -E "^\+[[:space:]]+privileged:[[:space:]]+true" >/dev/null; then
  echo "::error::[$CHART] STRUCT-3d: privileged:true was added or flipped from false. Security downgrade requires explicit operator approval."
  violations=$((violations + 1))
fi

if [[ "$violations" -gt 0 ]]; then
  echo ""
  echo "[$CHART] FOUND ${violations} structural risk(s). CI blocks per audit M06 #21 / §6.E.6b."
  exit 2
fi

echo "[$CHART] diff present but NO structural risks detected — informational only."
exit 0
