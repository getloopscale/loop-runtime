#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-20 T2.5 ext — TDD red-side gate.
#
# helm-values-schemas_test.sh proved the schemas accept GOOD values.
# This complementary suite proves the schemas REJECT bad values — i.e.,
# the schemas actually constrain (not just permissive shells).
#
# Per .claude/rules/tdd-workflow.md: every constraint MUST have at least
# one negative test that exercises the rejection path. Without negative
# tests, a schema with `additionalProperties: true` and zero `required`
# fields would pass the positive test but provide ZERO actual protection.
#
# Coverage:
#   - B16 protection: registries.docrPrefix empty → REJECT
#   - B50 protection: loopzero-cloud service.port wrong type → REJECT
#   - velero placeholder: bucket='REPLACE-AT-INSTALL-TIME' → REJECT
#   - cluster-issuer ACME: issuerKind=acme + no acme block → REJECT
#   - admission-policies keyless: enabled=true + no fulcioURL → REJECT
#   - admission-policies mode: clusterImagePolicy.mode='invalid' → REJECT
#
# Exit codes:
#   0 — All negative cases correctly REJECTED by schemas
#   1 — Negative case ACCEPTED (schema does not enforce — bug)
#   5 — Missing test dependency

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

log() { printf '[helm-schemas-negative-test] %s\n' "$*" >&2; }

if ! python3 -c "import jsonschema, yaml" 2>/dev/null; then
  log "ERROR: python3 jsonschema + PyYAML required"
  exit 5
fi

# Helper: assert that jsonschema validation of $1 (schema path) against $2
# (values inline YAML literal) RAISES jsonschema.ValidationError. Test name $3.
#
# Iter 2 self-bug fix (2026-05-20): original implementation interpolated the
# YAML literal into a `python3 -c "..."` heredoc, which crashed on YAML
# strings containing backslash-escapes (e.g., `^https://github\.com/...`)
# with SyntaxError. Bash interpreted exit 1 (python crash) as "schema
# rejected" — FALSE POSITIVE. Triple-bug class:
#   1. Test infrastructure lied (rejected = crashed)
#   2. format:uri schema flaw was MASKED (test "passed" without testing)
#   3. Future regression would still be masked
# Fix (no workaround): use tempfiles for YAML + schema script. Cleanly
# separates python source from data. Python validation result is observable
# via marker file; bash distinguishes accept/reject/crash by 3 distinct
# exit codes (0=accepted=test FAIL, 1=rejected=test PASS, 2=crashed=test
# CRASH report).
assert_rejects() {
  local schema="$1"
  local values_yaml="$2"
  local test_name="$3"
  TESTS_RUN=$((TESTS_RUN + 1))

  local yaml_tmp
  yaml_tmp=$(mktemp -t helm-schema-neg-XXXXXX.yaml)
  printf '%s\n' "$values_yaml" > "$yaml_tmp"

  local validator_tmp
  validator_tmp=$(mktemp -t helm-schema-validator-XXXXXX.py)
  cat > "$validator_tmp" <<'PYEOF'
import json
import sys
import jsonschema
import yaml

schema_path = sys.argv[1]
values_path = sys.argv[2]

try:
    with open(schema_path) as f:
        schema_obj = json.load(f)
    with open(values_path) as f:
        values_obj = yaml.safe_load(f)
except Exception as e:
    print(f"CRASH: setup failed: {e}", file=sys.stderr)
    sys.exit(2)

try:
    jsonschema.validate(instance=values_obj, schema=schema_obj)
    sys.exit(0)  # accepted (caller treats as FAIL — we want rejection)
except jsonschema.ValidationError as e:
    sys.exit(1)  # rejected (caller treats as PASS)
except Exception as e:
    print(f"CRASH: validation engine error: {e}", file=sys.stderr)
    sys.exit(2)
PYEOF

  # Iter 3 self-bug fix: under `set -euo pipefail`, a non-zero exit from
  # python3 aborts the script BEFORE rc=$? captures the exit code. Use
  # an explicit guard (|| true) to disarm `set -e` for this call. Capture
  # rc immediately afterwards while still within the guard's scope.
  local rc=0
  python3 "$validator_tmp" "$ROOT/$schema" "$yaml_tmp" 2>/dev/null || rc=$?
  rm -f "$yaml_tmp" "$validator_tmp"

  case "$rc" in
    0)
      log "  [FAIL] $test_name — schema ACCEPTED bad values (constraint missing)"
      TESTS_FAILED=$((TESTS_FAILED + 1))
      ;;
    1)
      log "  [PASS] $test_name — schema correctly rejected"
      TESTS_PASSED=$((TESTS_PASSED + 1))
      ;;
    *)
      log "  [CRASH] $test_name — validation engine crashed (exit $rc); test inconclusive"
      TESTS_FAILED=$((TESTS_FAILED + 1))
      ;;
  esac
}

# --- admission-policies B16 protection ---

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: ''
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images" \
  "admission-policies B16 — empty docrPrefix REJECTED"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images" \
  "admission-policies B16 — docrPrefix without trailing / REJECTED"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images
  mode: 'invalid-mode'" \
  "admission-policies — clusterImagePolicy.mode invalid enum REJECTED"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images
clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless" \
  "admission-policies — keyless enabled without fulcioURL REJECTED (conditional)"

# --- loopzero-cloud B50 protection ---

assert_rejects \
  "infra/helm/loopzero-cloud/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-cloud'
  tag: 'latest'
service:
  type: ClusterIP
  port: 'not-an-integer'
wiringConfigMap: 'loopzero-platform-topology'
dbCredentialsSecret: 'loopzero-db-credentials'" \
  "loopzero-cloud B50 — service.port string instead of integer REJECTED"

assert_rejects \
  "infra/helm/loopzero-cloud/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-cloud'
  tag: 'latest'
service:
  type: ClusterIP
  port: 99999
wiringConfigMap: 'loopzero-platform-topology'
dbCredentialsSecret: 'loopzero-db-credentials'" \
  "loopzero-cloud — service.port > 65535 REJECTED"

assert_rejects \
  "infra/helm/loopzero-cloud/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-cloud'
  tag: 'latest'
service:
  type: ClusterIP
  port: 13002
wiringConfigMap: ''
dbCredentialsSecret: 'loopzero-db-credentials'" \
  "loopzero-cloud B33/B35 — empty wiringConfigMap REJECTED"

assert_rejects \
  "infra/helm/loopzero-cloud/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-cloud'
service:
  type: ClusterIP
  port: 13002
wiringConfigMap: 'loopzero-platform-topology'
dbCredentialsSecret: 'loopzero-db-credentials'" \
  "loopzero-cloud — image without tag REJECTED"

# --- velero placeholder rejection (the binding feedback_velero_must_be_in_drill protection) ---

assert_rejects \
  "infra/helm/velero/values.schema.json" \
  "velero:
  initContainers:
    - name: velero-plugin-for-aws
      image: velero/velero-plugin-for-aws:v1.10.1
  configuration:
    backupStorageLocation:
      - name: default
        provider: aws
        bucket: REPLACE-AT-INSTALL-TIME
        config:
          region: us-east-1
          s3Url: https://s3.example.com" \
  "velero — bucket='REPLACE-AT-INSTALL-TIME' REJECTED (placeholder leak)"

assert_rejects \
  "infra/helm/velero/values.schema.json" \
  "velero:
  initContainers:
    - name: velero-plugin-for-aws
      image: velero/velero-plugin-for-aws:v1.10.1
  configuration:
    backupStorageLocation:
      - name: default
        provider: aws
        bucket: loopzero-prod-backups
        config:
          region: REPLACE-AT-INSTALL-TIME
          s3Url: https://s3.example.com" \
  "velero — region='REPLACE-AT-INSTALL-TIME' REJECTED"

assert_rejects \
  "infra/helm/velero/values.schema.json" \
  "velero:
  initContainers: []
  configuration:
    backupStorageLocation:
      - name: default
        provider: aws
        bucket: loopzero-prod-backups" \
  "velero — initContainers empty REJECTED (plugin required)"

assert_rejects \
  "infra/helm/velero/values.schema.json" \
  "velero:
  initContainers:
    - name: velero-plugin-for-aws
      image: velero/velero-plugin-for-aws:v1.10.1
  configuration:
    backupStorageLocation: []" \
  "velero — backupStorageLocation empty REJECTED (BSL required)"

# --- cluster-issuer ACME conditional ---

assert_rejects \
  "infra/helm/cluster-issuer/values.schema.json" \
  "issuerKind: acme
loopzeroCaIssuerName: loopzero-ca-issuer" \
  "cluster-issuer — issuerKind=acme without acme block REJECTED (if/then)"

assert_rejects \
  "infra/helm/cluster-issuer/values.schema.json" \
  "issuerKind: invalid-kind" \
  "cluster-issuer — issuerKind invalid enum REJECTED"

assert_rejects \
  "infra/helm/cluster-issuer/values.schema.json" \
  "issuerKind: acme
acme:
  email: 'not-an-email'
  server: https://acme-v02.api.letsencrypt.org/directory
  privateKeySecretRef: letsencrypt-prod" \
  "cluster-issuer — acme.email invalid format REJECTED"

assert_rejects \
  "infra/helm/cluster-issuer/values.schema.json" \
  "issuerKind: acme
acme:
  email: 'ops@loopzero.example'
  server: https://malicious-acme.example.com/directory
  privateKeySecretRef: letsencrypt-prod" \
  "cluster-issuer — acme.server unknown URL REJECTED (only LE prod+staging)"

# --- admission-policies keyless URI bug class protection (TDD red found
#     format:'uri' is silently permissive in jsonschema; we use pattern instead) ---

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images
clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless
  mode: warn
  imageGlobs: ['registry.digitalocean.com/loopzero-system/**']
  fulcioURL: 'not-a-url'
  rekorURL: https://rekor.sigstore.dev
  oidcIssuer: https://token.actions.githubusercontent.com
  identityRegexp: '^https://github\\.com/getloopscale/loopzero/'" \
  "admission-policies — fulcioURL garbage string REJECTED (pattern not format)"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images
clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless
  mode: warn
  imageGlobs: ['registry.digitalocean.com/loopzero-system/**']
  fulcioURL: https://fulcio.sigstore.dev
  rekorURL: 'plain-string-not-url'
  oidcIssuer: https://token.actions.githubusercontent.com
  identityRegexp: '^https://github\\.com/getloopscale/loopzero/'" \
  "admission-policies — rekorURL garbage string REJECTED (pattern not format)"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images
clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless
  mode: warn
  imageGlobs: ['registry.digitalocean.com/loopzero-system/**']
  fulcioURL: https://fulcio.sigstore.dev
  rekorURL: https://rekor.sigstore.dev
  oidcIssuer: 'no-scheme-here'
  identityRegexp: '^https://github\\.com/getloopscale/loopzero/'" \
  "admission-policies — oidcIssuer garbage string REJECTED (pattern not format)"

# --- Summary ---
echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
exit 0
