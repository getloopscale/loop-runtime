#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-20 T2.5 ext — hermetic gate that validates EVERY values.schema.json
# in infra/helm/* parses as valid JSON Schema draft/2020-12 AND that the
# default values.yaml (or environment-templated overlay) satisfies the schema.
#
# Why this gate matters
# ---------------------
# Without this gate, broken schemas (typos, draft-mismatch, illegal keywords)
# would only surface at `helmfile sync` time — typically during a LIVE drill
# iteration where we're already burning ~$15 cloud per run. Catching at lint
# time = ZERO cost.
#
# Velero placeholder rejection
# ----------------------------
# Velero's chart values intentionally contain REPLACE-AT-INSTALL-TIME literals
# that are templated by helmfile `set:` directives at install time
# (`mgmt.helmfile.yaml.gotmpl:273-278`). This test SKIPS direct validation of
# velero's raw values.yaml because the schema correctly rejects the placeholder
# (that IS the schema's purpose). Instead, this test confirms the placeholder
# IS present in the raw values (regression guard against silent template drift).
#
# Exit codes:
#   0 — All schemas valid + default values pass (velero skipped per design)
#   1 — Schema JSON parse error
#   2 — Schema draft validation error (e.g., illegal $schema URI)
#   3 — values.yaml does not satisfy schema
#   4 — velero raw values lack placeholder (regression — schema purpose lost)
#   5 — missing test dependency

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

log() { printf '[helm-values-schemas-test] %s\n' "$*" >&2; }

# --- Dependency check ---
if ! command -v python3 >/dev/null 2>&1; then
  log "ERROR: python3 required"
  exit 5
fi

# Check jsonschema + yaml available (auto-install hint if missing)
if ! python3 -c "import jsonschema, yaml" 2>/dev/null; then
  log "ERROR: python3 jsonschema + PyYAML required"
  log "  Install: pip3 install --user jsonschema pyyaml"
  exit 5
fi

# --- Discover all schemas ---
SCHEMAS=()
while IFS= read -r schema; do
  SCHEMAS+=("$schema")
done < <(find "$ROOT/infra/helm" -name "values.schema.json" -type f | sort)

if [[ ${#SCHEMAS[@]} -eq 0 ]]; then
  log "ERROR: no values.schema.json found under infra/helm/"
  exit 5
fi

log "Discovered ${#SCHEMAS[@]} chart schemas"

# --- Per-schema validation ---
for schema in "${SCHEMAS[@]}"; do
  chart_dir=$(dirname "$schema")
  chart_name=$(basename "$chart_dir")
  TESTS_RUN=$((TESTS_RUN + 1))

  # Step 1: schema parses as valid JSON
  if ! python3 -c "import json; json.load(open('$schema'))" 2>/dev/null; then
    log "  [FAIL] $chart_name: schema is not valid JSON"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    continue
  fi

  # Step 2: schema declares draft/2020-12 (project convention)
  if ! python3 -c "
import json
with open('$schema') as f:
    s = json.load(f)
declared = s.get('\$schema', '')
if 'draft/2020-12' not in declared:
    print(f'expected draft/2020-12, got: {declared}')
    exit(1)
" 2>/dev/null; then
    log "  [FAIL] $chart_name: schema does not declare draft/2020-12"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    continue
  fi

  # Step 3: jsonschema library validates schema itself
  if ! python3 -c "
import json
import jsonschema
with open('$schema') as f:
    s = json.load(f)
jsonschema.Draft202012Validator.check_schema(s)
" 2>/dev/null; then
    log "  [FAIL] $chart_name: jsonschema rejects schema as invalid draft/2020-12"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    continue
  fi

  # Step 4: validate default values.yaml against schema (chart-specific exception
  #   for velero — placeholder rejection is by design, see header)
  values_yaml="$chart_dir/values.yaml"
  if [[ ! -f "$values_yaml" ]]; then
    log "  [WARN] $chart_name: values.yaml missing (skipping value validation)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
    continue
  fi

  if [[ "$chart_name" == "velero" ]]; then
    # Special case: confirm placeholder IS present (regression guard)
    if grep -q "REPLACE-AT-INSTALL-TIME" "$values_yaml"; then
      log "  [PASS] $chart_name: schema valid + placeholder regression guard intact"
      TESTS_PASSED=$((TESTS_PASSED + 1))
    else
      log "  [FAIL] $chart_name: REPLACE-AT-INSTALL-TIME placeholder MISSING from values.yaml"
      log "    Schema purpose was to reject this placeholder at lint time —"
      log "    if it is gone, either schema is now redundant OR templating regressed."
      TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
    continue
  fi

  # Standard case: values.yaml MUST satisfy schema
  if python3 -c "
import json
import jsonschema
import yaml
with open('$schema') as f:
    schema_obj = json.load(f)
with open('$values_yaml') as f:
    values_obj = yaml.safe_load(f) or {}
jsonschema.validate(instance=values_obj, schema=schema_obj)
" 2>/dev/null; then
    log "  [PASS] $chart_name: schema valid + values.yaml satisfies"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    err=$(python3 -c "
import json
import jsonschema
import yaml
with open('$schema') as f:
    schema_obj = json.load(f)
with open('$values_yaml') as f:
    values_obj = yaml.safe_load(f) or {}
try:
    jsonschema.validate(instance=values_obj, schema=schema_obj)
except jsonschema.ValidationError as e:
    print(f'{e.message} at path {list(e.absolute_path)}')
" 2>&1)
    log "  [FAIL] $chart_name: values.yaml violates schema"
    log "    $err"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
done

# --- Summary ---
echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 3
fi
exit 0
