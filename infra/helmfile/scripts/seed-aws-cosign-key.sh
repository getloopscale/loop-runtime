#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# seed-aws-cosign-key.sh <secret-id>
#
# Creates the key pair the build signs tenant images with, as the Secrets Manager secret
# <secret-id>: a JSON object with `cosign.key` (the encrypted private key), `cosign.pub` and
# `cosign.password`. The aws-secrets-manager chart writes it into the build cluster as the
# Secret argo/cosign-signing-key, the one the build's signing step mounts.
#
# A secret that already holds a private key is left alone: that key signed every image
# built so far, and replacing it would orphan them. Rotation is a deliberate act, not a
# re-run of this script.
#
# cosign comes from PATH. The AWS CLI runs with whatever the caller's environment points
# it at: AWS itself, or the emulator in staging. No key or password is ever printed.
set -euo pipefail

SECRET_ID="${1:?usage: $0 <secret-id>}"

log() { printf '[seed-aws-cosign-key] %s\n' "$*" >&2; }

command -v aws >/dev/null || { log "ERROR: aws CLI not on PATH"; exit 2; }
command -v cosign >/dev/null || { log "ERROR: cosign not on PATH (https://docs.sigstore.dev/cosign/system_config/installation/)"; exit 2; }
command -v python3 >/dev/null || { log "ERROR: python3 not on PATH"; exit 2; }
command -v openssl >/dev/null || { log "ERROR: openssl not on PATH"; exit 2; }

exists=true
if ! current="$(aws secretsmanager get-secret-value --secret-id "$SECRET_ID" --query SecretString --output text 2>"${TMPDIR:-/tmp}/seed-cosign-err.$$")"; then
  if grep -q ResourceNotFoundException "${TMPDIR:-/tmp}/seed-cosign-err.$$"; then
    exists=false
    current=""
  else
    cat "${TMPDIR:-/tmp}/seed-cosign-err.$$" >&2
    rm -f "${TMPDIR:-/tmp}/seed-cosign-err.$$"
    exit 1
  fi
fi
rm -f "${TMPDIR:-/tmp}/seed-cosign-err.$$"

if CURRENT="$current" python3 -I -c '
import json, os, sys
try:
    doc = json.loads(os.environ["CURRENT"])
except ValueError:
    sys.exit(1)
sys.exit(0 if isinstance(doc, dict) and "BEGIN ENCRYPTED SIGSTORE PRIVATE KEY" in doc.get("cosign.key", "") else 1)
'; then
  log "$SECRET_ID already holds a signing key; nothing to do"
  exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

COSIGN_PASSWORD="$(openssl rand -hex 24)"
export COSIGN_PASSWORD
(cd "$work" && cosign generate-key-pair >/dev/null 2>&1) || { log "ERROR: cosign generate-key-pair failed"; exit 1; }

python3 -I - "$work" <<'PY' >"$work/secret.json"
import json, os, sys
work = sys.argv[1]
with open(os.path.join(work, "cosign.key")) as key, open(os.path.join(work, "cosign.pub")) as pub:
    print(json.dumps({
        "cosign.key": key.read(),
        "cosign.pub": pub.read(),
        "cosign.password": os.environ["COSIGN_PASSWORD"],
    }))
PY

if [[ "$exists" == true ]]; then
  aws secretsmanager put-secret-value --secret-id "$SECRET_ID" --secret-string "file://$work/secret.json" >/dev/null
else
  aws secretsmanager create-secret --name "$SECRET_ID" --secret-string "file://$work/secret.json" >/dev/null
fi
log "$SECRET_ID: generated a cosign signing key pair"
