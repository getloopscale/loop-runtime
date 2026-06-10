#!/usr/bin/env bash
# T2.1 of plan 2026-06-05-dashboard-journey-j05-device-success.
#
# Hermetic source-level assertion that `infra/helm/ory-hydra/values.yaml.gotmpl`
# defines `urls.post_device_done_url` with the canonical gotmpl pattern.
#
# Why source-level (not `helm template`):
#   `infra/helm/ory-hydra/` is a VALUES OVERLAY over the upstream `ory/hydra`
#   chart (referenced in `infra/helmfile/mgmt.helmfile.yaml.gotmpl:463`). The
#   overlay has no Chart.yaml of its own, so `helm template` direct against
#   this directory exits non-zero. A `helmfile template` against a live
#   environment would require kubectl context + network — not hermetic.
#   Source-level grep covers the regression contract: the key is present
#   and uses `trimSuffix "/"` to defend against trailing-slash drift (EC-8).
#
# Asserts:
#   1. Key `post_device_done_url:` exists exactly once under `urls:`.
#   2. Value uses `{{ trimSuffix "/" ... }}` to handle trailing-slash (EC-8).
#   3. Value defaults to `http://localhost:4455` when uiBaseUrl is unset.
#   4. Value resolves to `<uiBaseUrl>/device-success` template.
#   5. Comment references plan ID + ADR for auditability.
#
# This defeats EC-2 silent-pass: a missing key OR a value without trimSuffix
# OR a value without the /device-success suffix all fail LOUD here.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
TARGET="$REPO_ROOT/infra/helm/ory-hydra/values.yaml.gotmpl"

pass=0
fail=0

log() { printf '[ory-hydra-device-urls] %s\n' "$*" >&2; }

if [[ ! -f "$TARGET" ]]; then
  log "FAIL: target file not found: $TARGET"
  exit 1
fi

assert_grep() {
  local desc="$1" pattern="$2"
  if grep -qE "$pattern" "$TARGET"; then
    log "PASS [$desc]"
    pass=$((pass + 1))
  else
    log "FAIL [$desc]: pattern not matched: $pattern"
    fail=$((fail + 1))
  fi
}

assert_exact_count() {
  local desc="$1" pattern="$2" expected="$3"
  local actual
  actual=$(grep -cE "$pattern" "$TARGET" || true)
  if [[ "$actual" -eq "$expected" ]]; then
    log "PASS [$desc] (count=$actual)"
    pass=$((pass + 1))
  else
    log "FAIL [$desc]: expected count=$expected got=$actual for $pattern"
    fail=$((fail + 1))
  fi
}

# (1) Key is present exactly once — no shadowing or duplicate definition.
assert_exact_count "single post_device_done_url key" \
  '^[[:space:]]+post_device_done_url:' \
  1

# (2) EC-8 trimSuffix "/" gotmpl helper present in the same line.
assert_grep "EC-8 trimSuffix / defense" \
  'post_device_done_url:.*trimSuffix.*"/".*uiBaseUrl'

# (3) Default fallback to localhost:4455 when uiBaseUrl unset.
assert_grep "default uiBaseUrl localhost:4455" \
  'post_device_done_url:.*default.*"http://localhost:4455"'

# (4) The template ends with /device-success suffix.
assert_grep "/device-success suffix" \
  'post_device_done_url:.*device-success"'

# (5) Comment references plan ID + ADR ID (auditability).
assert_grep "comment cites plan ID" \
  '# Plan 2026-06-05-dashboard-journey-j05-device-success T2.1 D1'

# (6) Bonus regression: bash syntax sanity — render file with simple gotmpl
#     stub substitutions to confirm the line is well-formed YAML keyspec.
#     We do this by extracting the value and checking it parses as
#     "key: <quoted-value>".
extracted=$(grep -E '^[[:space:]]+post_device_done_url:' "$TARGET" || true)
if [[ -z "$extracted" ]]; then
  log "FAIL [yaml shape]: extraction failed (no key)"
  fail=$((fail + 1))
elif echo "$extracted" | grep -qE '^[[:space:]]+post_device_done_url:[[:space:]]+".*"$'; then
  log "PASS [yaml shape]: key: \"value\" form well-formed"
  pass=$((pass + 1))
else
  log "FAIL [yaml shape]: line not in 'key: \"value\"' form: $extracted"
  fail=$((fail + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  ory-hydra-device-urls aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
