#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C20.
# Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
#
# Audits that the union of permissions across the 4 split operator
# ClusterRoles is a SUBSET (or, with explicit exceptions, a delta with
# justification) of the monolith ClusterRole defined in
# infra/helm/tenant-controller/templates/rbac.yaml.
#
# Why this gate exists:
#   The split operators inherit the monolith's privileges. If the split
#   widens permissions (adds an apiGroup/resource/verb combo not in the
#   monolith), that's a privilege escalation no Security review approved.
#   The MEET decision flagged 5 tenant-operator widenings (priorityclasses,
#   apps/deployments, pods+pods/log, argo workflowtaskresults+
#   workflowtasksets, argo workflows+workflowtemplates+
#   clusterworkflowtemplates). Those need:
#     • Documented justification in docs/runbooks/operator-split-rbac-audit.md
#       (closes condition C10), AND
#     • Listed in this script's allowlist below.
#
# Algorithm:
#   1. Parse monolith ClusterRole rules into a normalized set
#      {(apiGroup, resource, verb)} triples.
#   2. Render each of the 4 split charts via `helm template`, parse the
#      ClusterRole rules into the same triple form.
#   3. Compute split_union − monolith. Subtract the explicit allowlist.
#   4. If anything remains, FAIL with the diff.
#
# This is intentionally permissive about EXCLUSIONS: the monolith may
# have privileges the split operators don't need; that's expected
# (least privilege per operator). The audit only flags ADDITIONS.
#
# Exit codes:
#   0 — split union ⊆ monolith ∪ allowlist
#   1 — privilege escalation found
#   2 — tooling missing (helm, python3, yq optional)
#
# Usage:
#   bash infra/scripts/rbac-subset-check.sh
#   bash infra/scripts/rbac-subset-check.sh --verbose    # print the diff sets
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERBOSE=0
[[ "${1:-}" == "--verbose" ]] && VERBOSE=1

log() { printf '[rbac-subset-check] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

command -v helm >/dev/null 2>&1 || fail "helm not in PATH" 2
command -v python3 >/dev/null 2>&1 || fail "python3 not in PATH (needed for YAML parsing)" 2

MONOLITH_CHART="${REPO_ROOT}/infra/helm/tenant-controller"
[[ -d "$MONOLITH_CHART" ]] || fail "monolith chart not found at $MONOLITH_CHART" 1

# Allowlist for explicit, security-reviewed widenings. Format:
#   apiGroup|resource|verb (one per line; "*" matches anything).
# Source-of-truth for these entries: condition C10 (operator-split-rbac-audit
# runbook). Each entry MUST be cross-referenced there with justification.
#
# As of 2026-04-30 the runbook is pending; this allowlist starts EMPTY so
# the gate fails loud on every widening until docs land. To unblock a
# widening: add row here AND a justification block in the runbook.
ALLOWLIST_FILE="${REPO_ROOT}/infra/scripts/rbac-subset-allowlist.txt"

# Render monolith + 4 splits into a single buffer; let Python parse.
read -r -d '' PY_SCRIPT <<'PYEOF' || true
import sys, yaml, os, subprocess

repo_root = sys.argv[1]
verbose = sys.argv[2] == "1"
allowlist_path = sys.argv[3]

monolith_chart_path = os.path.join(repo_root, "infra", "helm", "tenant-controller")

# --- helpers ---
def normalize(rule):
    """Expand a rule into a set of (apiGroup, resource, verb) triples."""
    triples = set()
    api_groups = rule.get("apiGroups", [""])
    if api_groups == [""]:
        api_groups = [""]
    for g in api_groups:
        for r in rule.get("resources", []):
            for v in rule.get("verbs", []):
                triples.add((g or "", r, v))
    return triples

def parse_clusterrole_rules(yaml_text):
    """Return all rules across all ClusterRole docs in the YAML buffer."""
    out = []
    try:
        for doc in yaml.safe_load_all(yaml_text):
            if not isinstance(doc, dict):
                continue
            if doc.get("kind") != "ClusterRole":
                continue
            for rule in doc.get("rules", []) or []:
                out.append(rule)
    except yaml.YAMLError as e:
        print(f"YAML parse error: {e}", file=sys.stderr)
        sys.exit(1)
    return out

def expand_rules(rules):
    out = set()
    for r in rules:
        out |= normalize(r)
    return out

# --- monolith --- (helm template to resolve {{ .Release.Namespace }} etc.)
try:
    result = subprocess.run(
        ["helm", "template", "test", monolith_chart_path],
        capture_output=True, text=True, check=True,
    )
except subprocess.CalledProcessError as e:
    print(f"helm template failed for monolith: {e.stderr}", file=sys.stderr)
    sys.exit(1)
monolith_rules = parse_clusterrole_rules(result.stdout)
monolith_set = expand_rules(monolith_rules)

# Special-case the wildcard rule: monolith has
#   apiGroups: ["loopzero.io"], resources: ["*"]
# We expand "*" (in resource position) so any loopzero.io resource the
# splits ask for matches. Same for apiGroups: ["*"] resources: ["*"].
# This is a documented privilege of the monolith; splits inherit it
# implicitly when they touch the same apiGroup.
def matches_monolith(triple):
    g, r, v = triple
    # exact match
    if triple in monolith_set:
        return True
    # wildcard resource within same apiGroup + verb
    if (g, "*", v) in monolith_set:
        return True
    # wildcard apiGroup
    if ("*", r, v) in monolith_set or ("*", "*", v) in monolith_set:
        return True
    # wildcard verb
    if (g, r, "*") in monolith_set or (g, "*", "*") in monolith_set:
        return True
    return False

# --- splits ---
splits = ["loopzero-tenant-operator", "loopzero-app-operator", "loopzero-preview-operator", "loopzero-platform-operator"]
split_union = set()
per_chart = {}
for chart in splits:
    chart_path = os.path.join(repo_root, "charts", chart)
    if not os.path.isdir(chart_path):
        print(f"[skip] chart dir missing: {chart_path}", file=sys.stderr)
        continue
    try:
        result = subprocess.run(
            ["helm", "template", "test", chart_path],
            capture_output=True, text=True, check=True,
        )
    except subprocess.CalledProcessError as e:
        print(f"helm template failed for {chart}: {e.stderr}", file=sys.stderr)
        sys.exit(1)
    rules = parse_clusterrole_rules(result.stdout)
    expanded = expand_rules(rules)
    per_chart[chart] = expanded
    split_union |= expanded

# --- allowlist (loose format: lines like "apiGroup|resource|verb"; comments allowed) ---
allow = set()
if os.path.exists(allowlist_path):
    with open(allowlist_path) as f:
        for line in f:
            line = line.split("#", 1)[0].strip()
            if not line:
                continue
            parts = line.split("|")
            if len(parts) != 3:
                print(f"[allowlist] malformed line: {line!r}", file=sys.stderr)
                sys.exit(1)
            allow.add(tuple(parts))

# --- diff ---
widenings = set()
for triple in split_union:
    if matches_monolith(triple):
        continue
    if triple in allow:
        continue
    widenings.add(triple)

if verbose:
    print(f"monolith rules: {len(monolith_set)}")
    for chart, s in per_chart.items():
        print(f"{chart}: {len(s)} triples")
    print(f"split union: {len(split_union)}")
    print(f"allowlist: {len(allow)}")
    print(f"widenings: {len(widenings)}")

if widenings:
    print("PRIVILEGE ESCALATION — split operators add permissions not in monolith:", file=sys.stderr)
    for g, r, v in sorted(widenings):
        # Find which charts contributed this triple.
        attribution = sorted([c for c, s in per_chart.items() if (g, r, v) in s])
        gprint = "(core)" if g == "" else g
        print(f"  + {gprint} / {r} / {v}  (chart(s): {','.join(attribution)})", file=sys.stderr)
    print("", file=sys.stderr)
    print("To unblock: add an entry to infra/scripts/rbac-subset-allowlist.txt", file=sys.stderr)
    print("AND document the justification in docs/runbooks/operator-split-rbac-audit.md (C10).", file=sys.stderr)
    sys.exit(1)

print("[rbac-subset-check] PASS — split union ⊆ monolith ∪ allowlist", file=sys.stderr)
sys.exit(0)
PYEOF

# Ensure PyYAML is available; fallback to a clear error message rather
# than the cryptic ModuleNotFoundError.
if ! python3 -c "import yaml" 2>/dev/null; then
  fail "python3 -m yaml (PyYAML) not available — install via 'pip install pyyaml' or use the system package" 2
fi

python3 -c "$PY_SCRIPT" "$REPO_ROOT" "$VERBOSE" "$ALLOWLIST_FILE"
