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

# Helper: assert that python validation of $1 (schema path) against $2 (values
# inline YAML/JSON literal) RAISES jsonschema.ValidationError. Test name $3.
assert_rejects() {
  local schema="$1"
  local values_yaml="$2"
  local test_name="$3"
  TESTS_RUN=$((TESTS_RUN + 1))

  if python3 -c "
import json
import sys
import jsonschema
import yaml
with open('$ROOT/$schema') as f:
    schema_obj = json.load(f)
values_obj = yaml.safe_load('''$values_yaml''')
try:
    jsonschema.validate(instance=values_obj, schema=schema_obj)
    print('SCHEMA ACCEPTED — this is the bug', file=sys.stderr)
    sys.exit(0)  # ACCEPTANCE = test failure (we want rejection)
except jsonschema.ValidationError:
    sys.exit(1)  # REJECTION = test pass
" 2>/dev/null; then
    log "  [FAIL] $test_name — schema ACCEPTED bad values (constraint missing)"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  else
    log "  [PASS] $test_name — schema correctly rejected"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  fi
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

# --- Summary ---
echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
exit 0
