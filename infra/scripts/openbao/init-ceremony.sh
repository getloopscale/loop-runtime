#!/usr/bin/env bash
# openbao-init-ceremony.sh
#
# F1.8 of plan 2026-06-02-openbao-adoption v1.2 — operator-only
# wrapper for OpenBao Shamir ceremony per Q13.
#
# RUNS THE INTERACTIVE INIT FLOW. Captures output to RAMdisk
# /dev/shm (NOT regular filesystem) + writes shard files for
# manual distribution + computes SHA-256 hashes for EC-05
# verification step.
#
# This script ONLY runs Phase 2-5 of openbao-init-runbook.md;
# it does NOT distribute shards (that's manual + air-gapped) and
# does NOT shred (operator runs `shred -uvz` after distribution
# confirmed).
#
# Pre-req:
#   - `bao` CLI in PATH
#   - kubectl port-forward to svc/openbao:8200 active
#   - VAULT_ADDR + VAULT_CACERT env set
#   - OpenBao Initialized: false + Sealed: true (run `bao status` first)
#
# Output (all in /dev/shm/, chmod 600):
#   /dev/shm/init.json          — full init output (PRESERVE during ceremony)
#   /dev/shm/shard-{1..5}.b64   — individual shards
#   /dev/shm/shard-hashes.txt   — SHA-256 hashes per shard
#   /dev/shm/root_token         — root token (revoked at end of ceremony)
#
# Exit codes:
#   0 — init complete, files written, hashes computed
#   1 — pre-flight failure (bao CLI missing, VAULT_ADDR unset, etc.)
#   2 — OpenBao not in expected state (already initialized OR not sealed)
#   3 — bao operator init failed
#   4 — shard parse / file write failed

set -euo pipefail

OUT_DIR="${OUT_DIR:-/dev/shm}"
INIT_FILE="${OUT_DIR}/init.json"
HASH_FILE="${OUT_DIR}/shard-hashes.txt"
TOKEN_FILE="${OUT_DIR}/root_token"

log() { printf '[init-ceremony] %s\n' "$*" >&2; }
fail() { log "ERROR: $*"; exit "${2:-1}"; }

# ---------- Phase 0: Pre-flight ----------
log "Phase 0 — Pre-flight checks"

command -v bao >/dev/null || fail "bao CLI not found in PATH" 1
command -v python3 >/dev/null || fail "python3 not found" 1
command -v sha256sum >/dev/null || fail "sha256sum not found" 1

[[ -n "${VAULT_ADDR:-}" ]] || fail "VAULT_ADDR env var not set" 1
[[ -n "${VAULT_CACERT:-}" ]] || fail "VAULT_CACERT env var not set" 1
[[ -r "$VAULT_CACERT" ]] || fail "VAULT_CACERT file unreadable: $VAULT_CACERT" 1

# Confirm OpenBao state
STATUS_JSON=$(bao status -format=json 2>/dev/null) || fail "Cannot connect to OpenBao at $VAULT_ADDR" 1
SEALED=$(echo "$STATUS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("sealed", True))')
INITIALIZED=$(echo "$STATUS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("initialized", False))')

if [[ "$INITIALIZED" == "True" ]]; then
  fail "OpenBao already initialized (cannot re-init without destroy). Run rekey procedure instead." 2
fi
if [[ "$SEALED" != "True" ]]; then
  fail "OpenBao not sealed (unexpected state)" 2
fi
log "OK: OpenBao sealed + uninitialized — ready for init"

# Pre-existing init residue check
for f in "$INIT_FILE" "$TOKEN_FILE" "$HASH_FILE" "$OUT_DIR"/shard-*.b64; do
  if [[ -e "$f" ]]; then
    fail "Pre-existing file: $f. Shred + remove before re-running ceremony." 1
  fi
done

# Confirmation gate
log ""
log "READY TO INIT — 5 shards / 3 quorum. Sponsor + backup person witnessing?"
read -p "[init-ceremony] Type 'PROCEED' to continue: " -r REPLY
[[ "$REPLY" == "PROCEED" ]] || { log "Aborted."; exit 0; }

# ---------- Phase 1: Run init ----------
log "Phase 1 — bao operator init -key-shares=5 -key-threshold=3"

bao operator init \
  -key-shares=5 \
  -key-threshold=3 \
  -recovery-shares=0 \
  -recovery-threshold=0 \
  -format=json > "$INIT_FILE" \
  || fail "bao operator init failed" 3

chmod 600 "$INIT_FILE"
log "Wrote $INIT_FILE (chmod 600)"

# ---------- Phase 2: Parse shards + root token ----------
log "Phase 2 — Parsing shards + root token"

python3 -c "
import json, sys, os
with open('$INIT_FILE') as f:
    d = json.load(f)
keys = d.get('unseal_keys_b64') or d.get('recovery_keys_b64')
if not keys or len(keys) != 5:
    print(f'ERROR: expected 5 shards, got {len(keys) if keys else 0}', file=sys.stderr)
    sys.exit(4)
for i, k in enumerate(keys, start=1):
    p = f'$OUT_DIR/shard-{i}.b64'
    with open(p, 'w') as out:
        out.write(k + '\n')
    os.chmod(p, 0o600)
with open('$TOKEN_FILE', 'w') as out:
    out.write(d['root_token'] + '\n')
os.chmod('$TOKEN_FILE', 0o600)
print('Wrote 5 shards + root token', file=sys.stderr)
" || fail "Shard parse failed" 4

# ---------- Phase 3: Compute SHA-256 hashes (EC-05) ----------
log "Phase 3 — SHA-256 hashes (EC-05 verification basis)"

> "$HASH_FILE"
for i in 1 2 3 4 5; do
  sha256sum "$OUT_DIR/shard-${i}.b64" >> "$HASH_FILE"
done
chmod 600 "$HASH_FILE"

log ""
log "Shard SHA-256 hashes (PRESERVE for EC-05 verification):"
cat "$HASH_FILE"
log ""

# ---------- Phase 4: Output instructions ----------
log "Phase 4 — Ceremony state ready for distribution"
log ""
log "Files written (all chmod 600, /dev/shm RAMdisk):"
log "  $INIT_FILE          — full init output"
log "  $OUT_DIR/shard-{1..5}.b64 — individual shards"
log "  $HASH_FILE          — SHA-256 hashes"
log "  $TOKEN_FILE         — root token (revoke after step 9)"
log ""
log "NEXT STEPS (manual, per docs/operations/openbao-init-runbook.md):"
log "  Step 6   — distribute 5 shards to 5 storages per Q13"
log "  Step 7.5 — verify SHA-256 hashes match this output"
log "  Step 7.6 — retrieve-verify cycle (sponsor + backup person)"
log "  Step 7.7 — unseal-test in throwaway k3d cluster"
log "  Step 7.8 — confirm bao status = Sealed: false"
log "  Step 7.9 — audit_log entry vault.shamir.ceremony.verified"
log "  Step 8   — shred /dev/shm/shard-*.b64 + init.json + shard-hashes.txt"
log "  Step 9   — bao login + bao token revoke -self"
log ""
log "DO NOT shred until distribution + verification CONFIRMED."
log "DO NOT proceed to step 9 if step 7 verification failed."
