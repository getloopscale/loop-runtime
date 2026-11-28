#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# seed-aws-platform-secret.sh <secret-id>
#
# Fills in the keys the platform generates for itself in the Secrets Manager secret
# infra-live creates for that purpose (`security/secrets`, loop-runtime/<env>/platform). The
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
# which require at least 32; placeholder marks a credential a third party issues;
# ed25519 is a signing key pair, made by openssl.
generated="$(CURRENT="$current" python3 -I - <<'PY'
import base64, json, os, secrets, string, subprocess, sys

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
    # The key loop-console signs its service calls with (LOOP_RUNTIME_SERVICE_AUTH_ED25519_PRIVATE_KEY):
    # base64 of the 32-byte seed followed by the 32-byte public key, the form Go's
    # crypto/ed25519 reads. The public half is stored again under
    # service-auth-ed25519-public-key, for the services that verify the signature.
    "service-auth-ed25519-private-key": "ed25519",
}


def ed25519_private_key():
    # PKCS#8 and SubjectPublicKeyInfo both end with the 32 raw bytes of the key.
    pkcs8 = subprocess.run(["openssl", "genpkey", "-algorithm", "ed25519", "-outform", "DER"],
                           check=True, capture_output=True).stdout
    spki = subprocess.run(["openssl", "pkey", "-inform", "DER", "-pubout", "-outform", "DER"],
                          input=pkcs8, check=True, capture_output=True).stdout
    return base64.b64encode(pkcs8[-32:] + spki[-32:]).decode()

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
    elif kind == "ed25519":
        doc[key] = ed25519_private_key()
    else:
        doc[key] = "".join(secrets.choice(alphabet) for _ in range(32))
    added.append(key)

# The public half follows the private key, also when an operator rotated the private one.
public = base64.b64encode(base64.b64decode(doc["service-auth-ed25519-private-key"])[32:]).decode()
if doc.get("service-auth-ed25519-public-key") != public:
    doc["service-auth-ed25519-public-key"] = public
    added.append("service-auth-ed25519-public-key")

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
