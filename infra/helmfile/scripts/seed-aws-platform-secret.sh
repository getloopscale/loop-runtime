#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# seed-aws-platform-secret.sh <secret-id>
#
# Fills in the keys the platform generates for itself in the Secrets Manager secret
# infra-live creates for that purpose (`security/secrets`, loopzero/<env>/platform). The
# aws-secrets-manager chart reads every one of them through External Secrets.
#
# Only MISSING keys are generated; a key that already has a value keeps it, so re-running
# is a no-op and a rotated value is never overwritten. A secret that does not hold a JSON
# object yet (infra-live creates it with a placeholder) starts from an empty one.
#
# The AWS CLI runs with whatever the caller's environment points it at: AWS itself, or
# the emulator in staging. No value is ever printed; the script reports key names only.
set -euo pipefail

SECRET_ID="${1:?usage: $0 <secret-id>}"

log() { printf '[seed-aws-platform-secret] %s\n' "$*" >&2; }

command -v aws >/dev/null || { log "ERROR: aws CLI not on PATH"; exit 2; }
command -v python3 >/dev/null || { log "ERROR: python3 not on PATH"; exit 2; }
command -v openssl >/dev/null || { log "ERROR: openssl not on PATH"; exit 2; }

current="$(aws secretsmanager get-secret-value --secret-id "$SECRET_ID" --query SecretString --output text)"

# key=generator. hex32 is 32 random bytes as hex (the API's AES key format, and a safe
# password alphabet for URIs); alnum32 is 32 characters for the Ory cookie/cipher keys,
# which require at least 32; placeholder marks a credential a third party issues.
generated="$(CURRENT="$current" python3 -I - <<'PY'
import json, os, secrets, string, sys

wanted = {
    "database-api-password": "hex32",
    "database-worker-password": "hex32",
    "database-cloud-password": "hex32",
    "api-bootstrap-secret": "hex32",
    "api-argo-callback-secret": "hex32",
    "api-secret-encryption-key": "hex32",
    "service-auth-secret": "hex32",
    "edge-route-token": "hex32",
    "session-token-secret": "hex32",
    "kratos-secret-default": "alnum32",
    "kratos-secret-cookie": "alnum32",
    "kratos-secret-cipher": "alnum32",
    "hydra-secret-system": "alnum32",
    "hydra-secret-cookie": "alnum32",
    # looptrace: its role on platform-postgres, and the token its first platform-admin
    # key is minted with.
    "looptrace-db-password": "hex32",
    "looptrace-admin-bootstrap-token": "hex32",
    # Resend's API key is issued by Resend; the placeholder fails loudly on the first
    # send until the operator puts the real key in the secret.
    "resend-api-key": "placeholder",
}

try:
    doc = json.loads(os.environ["CURRENT"])
except ValueError:
    doc = None
if not isinstance(doc, dict):
    doc = {}

alphabet = string.ascii_letters + string.digits
added = []
for key, kind in wanted.items():
    if doc.get(key):
        continue
    if kind == "placeholder":
        doc[key] = "re_NOT_SET"
    elif kind == "hex32":
        doc[key] = secrets.token_hex(32)
    else:
        doc[key] = "".join(secrets.choice(alphabet) for _ in range(32))
    added.append(key)

print(json.dumps({"added": added, "doc": doc}))
PY
)"

added="$(python3 -I -c 'import json,sys; print(" ".join(json.load(sys.stdin)["added"]))' <<<"$generated")"
if [[ -z "$added" ]]; then
  log "$SECRET_ID already holds every key; nothing to do"
  exit 0
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
chmod 600 "$tmp"
python3 -I -c 'import json,sys; print(json.dumps(json.load(sys.stdin)["doc"]))' <<<"$generated" >"$tmp"
aws secretsmanager put-secret-value --secret-id "$SECRET_ID" --secret-string "file://$tmp" >/dev/null
log "$SECRET_ID: generated $added"
