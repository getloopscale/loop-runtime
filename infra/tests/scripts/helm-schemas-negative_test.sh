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

# Shadow bug (2026-05-20): un-anchored '/\$' accepted strings with leading
# whitespace. Anchored '^[^\s]+/\$' catches it. Test locks in fix.
assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry with space/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images" \
  "admission-policies — docrPrefix with whitespace REJECTED (anchored pattern)"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.with whitespace/loopzero/'
clusterImagePolicy:
  enabled: true
  name: loopzero-images" \
  "admission-policies — harborPrefix with whitespace REJECTED (anchored pattern)"

assert_rejects \
  "infra/helm/admission-policies/values.schema.json" \
  "registries:
  docrPrefix: 'registry.digitalocean.com/loopzero-system/'
  harborPrefix: 'harbor.dev.loopzero.example/loopzero-platform/'
  additionalPrefixes: ['bad prefix with space/']
clusterImagePolicy:
  enabled: true
  name: loopzero-images" \
  "admission-policies — additionalPrefixes item with whitespace REJECTED"

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

# --- ory-kratos uiBaseUrl pattern (EC-8 no trailing slash) ---

assert_rejects \
  "infra/helm/ory-kratos/values.schema.json" \
  "kratos: {}" \
  "ory-kratos — missing uiBaseUrl REJECTED (required field)"

assert_rejects \
  "infra/helm/ory-kratos/values.schema.json" \
  "uiBaseUrl: 'http://localhost:4455/'" \
  "ory-kratos — uiBaseUrl with trailing slash REJECTED (EC-8)"

assert_rejects \
  "infra/helm/ory-kratos/values.schema.json" \
  "uiBaseUrl: 'not-a-url'" \
  "ory-kratos — uiBaseUrl without scheme REJECTED"

assert_rejects \
  "infra/helm/ory-kratos/values.schema.json" \
  "uiBaseUrl: 'ftp://example.com'" \
  "ory-kratos — uiBaseUrl wrong scheme REJECTED (only http/https)"

assert_rejects \
  "infra/helm/ory-kratos/values.schema.json" \
  "uiBaseUrl: 'https://app.loopzero-drill.example/login'" \
  "ory-kratos — uiBaseUrl with path REJECTED (host-only per pattern)"

# --- ory-hydra uiBaseUrl pattern (same constraint as kratos) ---

assert_rejects \
  "infra/helm/ory-hydra/values.schema.json" \
  "hydra: {}" \
  "ory-hydra — missing uiBaseUrl REJECTED (required field)"

assert_rejects \
  "infra/helm/ory-hydra/values.schema.json" \
  "uiBaseUrl: 'http://localhost:4455/'" \
  "ory-hydra — uiBaseUrl with trailing slash REJECTED (EC-8)"

# --- loopzero-api image.tag + replicas constraints ---

assert_rejects \
  "infra/helm/loopzero-api/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-api'
  tag: ''
kubeContexts:
  mgmt: 'mgmt-ctx'
  build: 'build-ctx'
  runtime: 'runtime-ctx'" \
  "loopzero-api — empty image.tag REJECTED (minLength=1)"

assert_rejects \
  "infra/helm/loopzero-api/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-api'
  tag: 'v1.0.0'
kubeContexts:
  mgmt: ''
  build: 'build-ctx'
  runtime: 'runtime-ctx'" \
  "loopzero-api — empty kubeContexts.mgmt REJECTED (minLength=1)"

assert_rejects \
  "infra/helm/loopzero-api/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-api'
  tag: 'v1.0.0'
replicas:
  api: 0" \
  "loopzero-api — replicas.api=0 REJECTED (minimum=1)"

assert_rejects \
  "infra/helm/loopzero-api/values.schema.json" \
  "image:
  repository: 'registry.digitalocean.com/loopzero-system/loopzero-api'
  tag: 'v1.0.0'
replicas:
  api: -1" \
  "loopzero-api — replicas.api negative REJECTED (minimum=1)"

# --- OCI published loopzero-admission-policies (symmetric fix verification) ---
# Per CLAUDE.md Helm Chart Dual System: 3 charts overlap (loopzero-api, loopzero-cloud,
# loopzero-admission-policies). When patching one, patch the other. This block
# verifies the symmetric fix to charts/loopzero-admission-policies/values.schema.json
# (commit 52278ab9 fixed infra/helm version; this verifies OCI version).

assert_rejects \
  "charts/loopzero-admission-policies/values.schema.json" \
  "clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless
  fulcioURL: 'garbage-string'
  rekorURL: https://rekor.sigstore.dev
  oidcIssuer: https://token.actions.githubusercontent.com
  imageGlobs: ['registry.digitalocean.com/loopzero-system/**']
  identityRegexp: '^https://github\\.com/getloopscale/loopzero/'" \
  "OCI loopzero-admission-policies — fulcioURL garbage REJECTED (symmetric fix)"

assert_rejects \
  "charts/loopzero-admission-policies/values.schema.json" \
  "clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless
  fulcioURL: https://fulcio.sigstore.dev
  rekorURL: 'not-a-url'
  oidcIssuer: https://token.actions.githubusercontent.com
  imageGlobs: ['registry.digitalocean.com/loopzero-system/**']
  identityRegexp: '^https://github\\.com/getloopscale/loopzero/'" \
  "OCI loopzero-admission-policies — rekorURL garbage REJECTED (symmetric fix)"

assert_rejects \
  "charts/loopzero-admission-policies/values.schema.json" \
  "clusterImagePolicyKeyless:
  enabled: true
  name: loopzero-keyless
  fulcioURL: https://fulcio.sigstore.dev
  rekorURL: https://rekor.sigstore.dev
  oidcIssuer: 'plain-string'
  imageGlobs: ['registry.digitalocean.com/loopzero-system/**']
  identityRegexp: '^https://github\\.com/getloopscale/loopzero/'" \
  "OCI loopzero-admission-policies — oidcIssuer garbage REJECTED (symmetric fix)"

# --- OCI loopzero-platform global.registry first-char enforcement (2026-05-20 shadow bug audit) ---

assert_rejects \
  "charts/loopzero-platform/values.schema.json" \
  "global:
  registry: '.bad-leading-dot'" \
  "OCI loopzero-platform — registry leading-dot REJECTED (first char [a-z0-9])"

assert_rejects \
  "charts/loopzero-platform/values.schema.json" \
  "global:
  registry: '-bad-leading-dash'" \
  "OCI loopzero-platform — registry leading-dash REJECTED (first char [a-z0-9])"

assert_rejects \
  "charts/loopzero-platform/values.schema.json" \
  "global:
  registry: 'UPPERCASE.com'" \
  "OCI loopzero-platform — registry uppercase REJECTED"

assert_rejects \
  "charts/loopzero-platform/values.schema.json" \
  "global:
  registry: 'registry.com/path with space'" \
  "OCI loopzero-platform — registry whitespace REJECTED"

# --- cert-manager INF-G08 hardening invariants (4 critical cases) ---

assert_rejects \
  "infra/helm/cert-manager/values.schema.json" \
  "resources:
  limits: {cpu: 200m, memory: 128Mi}
  requests: {cpu: 50m, memory: 64Mi}
securityContext:
  runAsNonRoot: false
  runAsUser: 1000
  seccompProfile: {type: RuntimeDefault}
containerSecurityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  runAsNonRoot: true
  capabilities: {drop: [ALL]}" \
  "cert-manager INF-G08 — runAsNonRoot=false REJECTED (const:true)"

assert_rejects \
  "infra/helm/cert-manager/values.schema.json" \
  "resources:
  limits: {cpu: 200m, memory: 128Mi}
  requests: {cpu: 50m, memory: 64Mi}
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  seccompProfile: {type: RuntimeDefault}
containerSecurityContext:
  readOnlyRootFilesystem: false
  allowPrivilegeEscalation: false
  runAsNonRoot: true
  capabilities: {drop: [ALL]}" \
  "cert-manager INF-G08 — readOnlyRootFilesystem=false REJECTED (const:true)"

assert_rejects \
  "infra/helm/cert-manager/values.schema.json" \
  "resources:
  limits: {cpu: 200m, memory: 128Mi}
  requests: {cpu: 50m, memory: 64Mi}
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  seccompProfile: {type: Unconfined}
containerSecurityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  runAsNonRoot: true
  capabilities: {drop: [ALL]}" \
  "cert-manager INF-G08 — seccompProfile Unconfined REJECTED (enum)"

assert_rejects \
  "infra/helm/cert-manager/values.schema.json" \
  "resources:
  limits: {cpu: 200m, memory: 128Mi}
  requests: {cpu: 50m, memory: 64Mi}
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  seccompProfile: {type: RuntimeDefault}
containerSecurityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  runAsNonRoot: true
  capabilities: {drop: [NET_RAW]}" \
  "cert-manager INF-G08 — capabilities drop missing ALL REJECTED"

assert_rejects \
  "infra/helm/cert-manager/values.schema.json" \
  "resources: {}
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  seccompProfile: {type: RuntimeDefault}
containerSecurityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  runAsNonRoot: true
  capabilities: {drop: [ALL]}" \
  "cert-manager — resources without limits/requests REJECTED (VAP)"

# --- network-policies egress posture protection ---

assert_rejects \
  "infra/helm/network-policies/values.schema.json" \
  "defaultDenyIngress: 'true'
defaultDenyEgress: false
loopzeroSystemAllowEgress: false
namespaces: ['loopzero-system']" \
  "network-policies — defaultDenyIngress as string REJECTED (boolean)"

assert_rejects \
  "infra/helm/network-policies/values.schema.json" \
  "defaultDenyIngress: true
defaultDenyEgress: false
loopzeroSystemAllowEgress: false
namespaces: []" \
  "network-policies — empty namespaces REJECTED (minItems=1)"

assert_rejects \
  "infra/helm/network-policies/values.schema.json" \
  "defaultDenyIngress: true
defaultDenyEgress: false
loopzeroSystemAllowEgress: false
namespaces: ['UPPERCASE-NS']" \
  "network-policies — uppercase namespace REJECTED (RFC 1123 pattern)"

assert_rejects \
  "infra/helm/network-policies/values.schema.json" \
  "defaultDenyIngress: true
defaultDenyEgress: false
loopzeroSystemAllowEgress: false
namespaces: ['loopzero-system']
unknownToggle: true" \
  "network-policies — unknown key REJECTED (additionalProperties:false catches typos)"

# --- postgresql B19/B21/B27/B29 protection ---

assert_rejects \
  "infra/helm/postgresql/values.schema.json" \
  "image:
  registry: docker.io
  repository: 'UPPER/postgresql'
auth:
  username: loopzero
  password: loopzero-dev-password
  database: loopzero" \
  "postgresql B19 — uppercase image repository REJECTED"

assert_rejects \
  "infra/helm/postgresql/values.schema.json" \
  "image:
  registry: docker.io
  repository: 'bitnami/postgresql'
auth:
  username: 'LoopZero-User'
  password: loopzero-dev-password
  database: loopzero" \
  "postgresql — username with dash+uppercase REJECTED (PG identifier)"

assert_rejects \
  "infra/helm/postgresql/values.schema.json" \
  "image:
  registry: docker.io
  repository: 'bitnami/postgresql'
auth:
  username: loopzero
  password: ''
  database: loopzero" \
  "postgresql — empty auth.password REJECTED (no auto-gen)"

assert_rejects \
  "infra/helm/postgresql/values.schema.json" \
  "image:
  registry: docker.io
  repository: 'bitnami/postgresql'
auth:
  username: loopzero
  password: loopzero-dev-password
  database: loopzero
primary:
  persistence:
    size: '10gb'" \
  "postgresql — PVC size '10gb' (wrong unit) REJECTED (Gi required)"

assert_rejects \
  "infra/helm/postgresql/values.schema.json" \
  "image:
  registry: ''
  repository: 'bitnami/postgresql'
auth:
  username: loopzero
  password: loopzero-dev-password
  database: loopzero" \
  "postgresql — empty image.registry REJECTED (minLength=1)"

# --- argocd B17 + T-05 + INF-G08 protection ---

assert_rejects \
  "infra/helm/argocd/values.schema.json" \
  "notifications:
  services:
    webhook: 'invalid-key'" \
  "argocd B17 — notifications.services (deprecated key) REJECTED"

assert_rejects \
  "infra/helm/argocd/values.schema.json" \
  "controller:
  replicas: 0" \
  "argocd T-05 — controller.replicas=0 REJECTED (minimum=1)"

assert_rejects \
  "infra/helm/argocd/values.schema.json" \
  "controller:
  replicas: 50" \
  "argocd T-05 — controller.replicas=50 REJECTED (maximum=10)"

assert_rejects \
  "infra/helm/argocd/values.schema.json" \
  "server:
  service:
    type: NodePort
    nodePortHttp: 8080" \
  "argocd — nodePort outside K8s default range REJECTED (30000-32767)"

assert_rejects \
  "infra/helm/argocd/values.schema.json" \
  "global:
  image:
    tag: 'latest'" \
  "argocd — image.tag=latest REJECTED (semver required)"

assert_rejects \
  "infra/helm/argocd/values.schema.json" \
  "server:
  containerSecurityContext:
    runAsNonRoot: false" \
  "argocd INF-G08 — server.runAsNonRoot=false REJECTED (const:true)"

# --- argo-workflows build cluster RBAC + scale protection ---

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner-no-tag'
sourceBucket: loopzero-builds" \
  "argo-workflows — cloneRunner image without tag REJECTED"

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner:v1'
sourceBucket: 'UPPER-BUCKET'" \
  "argo-workflows — uppercase sourceBucket REJECTED (RFC 1123)"

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner:v1'
sourceBucket: '-bad-leading'" \
  "argo-workflows — sourceBucket leading-dash REJECTED"

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner:v1'
sourceBucket: loopzero-builds
controller:
  workflowWorkers: 0" \
  "argo-workflows — workflowWorkers=0 REJECTED (minimum=1)"

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner:v1'
sourceBucket: loopzero-builds
controller:
  workflowWorkers: 500" \
  "argo-workflows — workflowWorkers=500 REJECTED (maximum=128 OOM bound)"

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner:v1'
sourceBucket: loopzero-builds
controller:
  metricsConfig:
    port: 80" \
  "argo-workflows — metrics port 80 REJECTED (privileged, non-root would fail)"

assert_rejects \
  "infra/helm/argo-workflows/values.schema.json" \
  "images:
  cloneRunner: 'registry.do/loopzero/runner:v1'
sourceBucket: loopzero-builds
controller:
  securityContext:
    runAsNonRoot: false" \
  "argo-workflows INF-G08 — controller runAsNonRoot=false REJECTED"

# --- chaos-mesh ADR-0016 D-4 + PSA restricted protection ---

assert_rejects \
  "infra/helm/chaos-mesh/values.schema.json" \
  "chaos-mesh:
  dashboard:
    create: true" \
  "chaos-mesh — dashboard.create=true REJECTED (attack surface, const:false)"

assert_rejects \
  "infra/helm/chaos-mesh/values.schema.json" \
  "chaos-mesh:
  controllerManager:
    podSecurityContext:
      runAsNonRoot: false
      runAsUser: 1000
      seccompProfile: {type: RuntimeDefault}" \
  "chaos-mesh PSA — controller runAsNonRoot=false REJECTED (const:true)"

assert_rejects \
  "infra/helm/chaos-mesh/values.schema.json" \
  "chaos-mesh:
  controllerManager:
    securityContext:
      allowPrivilegeEscalation: true
      capabilities: {drop: [ALL]}
      readOnlyRootFilesystem: true" \
  "chaos-mesh PSA — allowPrivilegeEscalation=true REJECTED (const:false)"

assert_rejects \
  "infra/helm/chaos-mesh/values.schema.json" \
  "chaos-mesh:
  chaosDaemon:
    socketPath: '/no-sock-suffix'" \
  "chaos-mesh — chaosDaemon socketPath without .sock REJECTED"

assert_rejects \
  "infra/helm/chaos-mesh/values.schema.json" \
  "chaos-mesh:
  chaosDaemon:
    runtime: 'rkt'" \
  "chaos-mesh — chaosDaemon runtime 'rkt' REJECTED (enum: containerd|docker|cri-o)"

assert_rejects \
  "infra/helm/chaos-mesh/values.schema.json" \
  "chaos-mesh:
  controllerManager:
    securityContext:
      allowPrivilegeEscalation: false
      capabilities: {drop: [NET_RAW]}
      readOnlyRootFilesystem: true" \
  "chaos-mesh PSA — capabilities drop missing ALL REJECTED"

# --- cloudflared CF Tunnel deployment protection ---

assert_rejects \
  "infra/helm/cloudflared/values.schema.json" \
  "replicaCount: 0
image: {repository: cloudflare/cloudflared, tag: '2024.12.2'}
tokenSecret: {name: tok, key: token}
resources:
  requests: {cpu: 50m, memory: 64Mi}
  limits: {cpu: 200m, memory: 256Mi}" \
  "cloudflared — replicaCount=0 REJECTED (minimum=1 for HA)"

assert_rejects \
  "infra/helm/cloudflared/values.schema.json" \
  "replicaCount: 2
image: {repository: cloudflare/cloudflared, tag: latest}
tokenSecret: {name: tok, key: token}
resources:
  requests: {cpu: 50m, memory: 64Mi}
  limits: {cpu: 200m, memory: 256Mi}" \
  "cloudflared — image tag=latest REJECTED (not:const, Bug #16 pattern)"

assert_rejects \
  "infra/helm/cloudflared/values.schema.json" \
  "replicaCount: 2
image: {repository: cloudflare/cloudflared, tag: '2024.12.2'}
tokenSecret: {name: '', key: token}
resources:
  requests: {cpu: 50m, memory: 64Mi}
  limits: {cpu: 200m, memory: 256Mi}" \
  "cloudflared — empty tokenSecret.name REJECTED (would cause CrashLoopBackOff)"

assert_rejects \
  "infra/helm/cloudflared/values.schema.json" \
  "replicaCount: 2
image: {repository: cloudflare/cloudflared, tag: '2024.12.2'}
tokenSecret: {name: 'UPPER-SEC', key: token}
resources:
  requests: {cpu: 50m, memory: 64Mi}
  limits: {cpu: 200m, memory: 256Mi}" \
  "cloudflared — uppercase secret name REJECTED (RFC 1123)"

assert_rejects \
  "infra/helm/cloudflared/values.schema.json" \
  "replicaCount: 2
image: {repository: cloudflare/cloudflared, tag: '2024.12.2'}
tokenSecret: {name: tok, key: token}
resources:
  requests: {cpu: 50m, memory: 64Mi}
  limits: {cpu: 200m, memory: 256Mi}
metricsPort: 80" \
  "cloudflared — metricsPort=80 REJECTED (privileged port, non-root conflict)"

# --- harbor MEET-2026-04-11-048 mandates protection ---

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  trivy:
    enabled: true" \
  "harbor MEET — trivy enabled REJECTED (const:false, syft+grype in pipeline)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  notary:
    enabled: true" \
  "harbor MEET — notary enabled REJECTED (const:false, cosign instead)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  chartmuseum:
    enabled: true" \
  "harbor — chartmuseum enabled REJECTED (deprecated by Harbor 2.10+)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  expose:
    type: loadBalancer" \
  "harbor — expose.type=loadBalancer REJECTED (bypasses Traefik)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  expose:
    tls:
      enabled: true" \
  "harbor — expose.tls.enabled=true REJECTED (double TLS with Traefik)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  externalURL: 'http://harbor.dev.loopzero.example'" \
  "harbor — HTTP externalURL REJECTED (pattern requires https://)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  database:
    type: internal" \
  "harbor MEET — database.type=internal REJECTED (state isolation violation)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  database:
    type: external
    external:
      maxOpenConns: 100" \
  "harbor MEET — maxOpenConns=100 REJECTED (>50 PG pool budget burn)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  nginx:
    replicas: 1" \
  "harbor — nginx.replicas=1 REJECTED (const:0, Traefik is the ingress)"

assert_rejects \
  "infra/helm/harbor/values.schema.json" \
  "harbor:
  database:
    type: external
    external:
      port: 'abc'" \
  "harbor — database port non-numeric string REJECTED (pattern: ^[0-9]+\$)"

# --- Summary ---
echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
exit 0
