#!/usr/bin/env bash
# The wildcard certificate of the tenant apps, *.<EDGE_APPS_DOMAIN>, issued by Let's Encrypt over
# DNS-01 with lego's Hostinger provider; HTTP-01 cannot issue a wildcard, and Caddy has no module
# for Hostinger's DNS. lego 5's `run` issues the certificate when there is none and renews it
# when it is due, so `task staging:edge` and the renewal timer run the same thing. A renewal
# restarts the edge, which reads the certificate as a systemd credential when it starts.
#
#   EDGE_APPS_DOMAIN     the apps domain, e.g. apps.staging.getloopscale.com
#   EDGE_LEGO_DIR        lego's directory (account and certificates), kept across rebuilds
#   EDGE_DNS_TOKEN_FILE  file holding the Hostinger API token; it is never printed
#   EDGE_CERT_EMAIL      the ACME account's contact
set -euo pipefail

: "${EDGE_APPS_DOMAIN:?}" "${EDGE_LEGO_DIR:?}" "${EDGE_DNS_TOKEN_FILE:?}" "${EDGE_CERT_EMAIL:?}"
if [ ! -r "$EDGE_DNS_TOKEN_FILE" ]; then
  echo "cannot read the Hostinger API token at $EDGE_DNS_TOKEN_FILE" >&2
  exit 1
fi
HOSTINGER_API_TOKEN=$(<"$EDGE_DNS_TOKEN_FILE")
export HOSTINGER_API_TOKEN

install -d -m 0700 "$EDGE_LEGO_DIR"
lego --dns hostinger --domains "*.$EDGE_APPS_DOMAIN" --email "$EDGE_CERT_EMAIL" --accept-tos \
  --path "$EDGE_LEGO_DIR" --deploy-hook "systemctl try-restart loopzero-edge.service" run
