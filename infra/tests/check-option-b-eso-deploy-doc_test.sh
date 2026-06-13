#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-option-b-eso-deploy-doc_test.sh — iter 33 productive-idle hermetic
# contract test for `docs/operations/option-b-mgmt-helmfile-eso-deploy.md`.
#
# Locks the contract that the Option B analysis doc:
#   - Documents 4 sub-paths (B1, B2, B3, B4) with each rated
#   - Names B1 as recommended for dev-public iter 1
#   - Names B4 as REJECTED (mandate violation)
#   - Includes canonical commands for B1 (helmfile + kubectl)
#   - Cites the iter 31 architectural finding origin
#   - Documents rollback path
#   - References the runtime helmfile canonical pattern source
#   - Stays in sync with the ESO chart version pinned in runtime helmfile
#     (drift defense — if runtime bumps ESO 0.14.3 → 0.15.0, this doc
#     needs the same bump)
#
# Hermetic — NO kubectl, NO network, NO helm. Pure grep on the doc.

set -euo pipefail

DOC="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/docs/operations/option-b-mgmt-helmfile-eso-deploy.md"
RUNTIME_HELMFILE="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/helmfile/runtime.helmfile.yaml.gotmpl"
TMPDIR=$(mktemp -d -t option-b-doc-XXXXXX)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

PASS=0
FAIL=0
assert() {
  local desc="$1" cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "FAIL: $desc"
    FAIL=$((FAIL+1))
  fi
}

# A — file exists
assert "A1 Option B analysis doc exists" "test -f '$DOC'"

# B — 4 sub-paths named (B1, B2, B3, B4)
for path in B1 B2 B3 B4; do
  assert "B-${path}: sub-path ${path} named" \
    "grep -E '^### ${path} ' '$DOC'"
done

# C — Decision rationale
assert "C1 B1 marked RECOMMENDED for dev-public iter 1" \
  "grep -E 'B1.*upstream.*RECOMMENDED|^### B1.*RECOMMENDED' '$DOC'"

assert "C2 B4 marked REJECTED (mandate violation)" \
  "grep -E 'B4.*REJECTED|REJECTED.*defeats|REJECTED.*mandate' '$DOC'"

assert "C3 B3 marked NOT recommended for iter 1" \
  "grep -E 'B3.*NOT recommended|NOT recommended for iter 1' '$DOC'"

# D — Canonical commands for B1
assert "D1 B1 commands include helmfile sync with --selector name=external-secrets" \
  "grep -E 'helmfile.*--selector name=external-secrets sync' '$DOC'"

assert "D2 B1 commands include kubectl wait for ESO Deployment" \
  "grep -A 2 -E 'kubectl.*wait' '$DOC' | grep -q -E 'Available'"

assert "D3 B1 commands include CRD verification kubectl get crd" \
  "grep -E 'kubectl.*get crd' '$DOC'"

# E — ESO version pin matches runtime helmfile canonical (drift defense).
# Look for the release block in runtime helmfile — pattern matches the
# specific release within the releases: list (chart line locks the match
# to the actual ESO release entry, not chart-search-results elsewhere).
RUNTIME_ESO_VERSION="$(awk '
  /^  - name: external-secrets$/ {hit=NR}
  hit && NR > hit && NR < hit + 6 && /version: / {
    sub(/^[[:space:]]*version: /, "")
    print
    exit
  }
' "$RUNTIME_HELMFILE" 2>/dev/null)"

# Doc records `version: X.Y.Z` line inside the suggested release block.
DOC_ESO_VERSION="$(grep -oE 'version: [v0-9][0-9]*\.[0-9]+\.[0-9]+' "$DOC" | head -1 | awk '{print $2}')"

assert "E1 ESO version in doc ($DOC_ESO_VERSION) matches runtime helmfile ($RUNTIME_ESO_VERSION)" \
  "[ -n '$DOC_ESO_VERSION' ] && [ -n '$RUNTIME_ESO_VERSION' ] && [ '$DOC_ESO_VERSION' = '$RUNTIME_ESO_VERSION' ]"

# F — Architectural finding citation
assert "F1 doc cites iter 31 architectural finding origin" \
  "grep -E 'iter 31.*architectural|architectural finding.*iter 31' '$DOC'"

assert "F2 doc cites the runbook Open Question cross-link" \
  "grep -E 'openbao-transit-cutover-runbook.md|runbook.*Open Question' '$DOC'"

# G — Rollback path present
assert "G1 doc has Rollback path section" \
  "grep -E '^## Rollback' '$DOC'"

# Fix awk range: skip the start line itself by also requiring NR > start.
# Pattern '/^## Rollback/{f=1;next} f && /^## /{f=0} f' captures body only.
assert "G2 rollback includes helmfile destroy command" \
  "awk '/^## Rollback/{f=1;next} f && /^## /{f=0} f' '$DOC' | grep -q 'helmfile.*destroy'"

assert "G3 rollback includes git restore command" \
  "awk '/^## Rollback/{f=1;next} f && /^## /{f=0} f' '$DOC' | grep -q 'git restore'"

# H — Decision matrix
assert "H1 doc has Decision matrix section" \
  "grep -E '^## Decision matrix' '$DOC'"

# Fix awk range same way + escape pipe in grep regex
assert "H2 matrix table includes Time + Risk columns" \
  "awk '/^## Decision matrix/{f=1;next} f && /^## /{f=0} f' '$DOC' | grep -qE 'Time .* Risk'"

# I — Cross-references section + canonical pattern source
assert "I1 doc has Cross-references section" \
  "grep -E '^## Cross-references' '$DOC'"

# Cross-references is the LAST section — no end ## delimiter. Capture
# from start of section to EOF.
assert "I2 cross-refs cite runtime helmfile as canonical pattern source" \
  "awk '/^## Cross-references/{f=1;next} f' '$DOC' | grep -q 'runtime.helmfile.yaml.gotmpl'"

# J — Anti-patterns
assert "J1 no Co-Authored-By trailers" \
  "! grep -i 'Co-Authored-By' '$DOC'"

assert "J2 Rule 4 mandate explicitly cited (no destructive without authorization)" \
  "grep -E 'Rule 4|sponsor.*verbatim|sponsor.*authorization' '$DOC'"

# K — Plan slug + canonical memories cited
assert "K1 doc cites J-22 plan slug" \
  "grep -E '2026-06-10-j22-openbao-transit-eso-sync' '$DOC'"

assert "K2 doc cites productive-idle mandate memory" \
  "grep -E 'feedback_productive_idle_runbook_preauthor_2026_06_10' '$DOC'"

# Summary
echo ""
echo "option-b-eso-deploy doc contract: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot preserved: $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
