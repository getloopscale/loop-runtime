#!/bin/bash
# Sanitizer v4 — strips all helm-generated non-determinism.
sed -E \
  -e 's/uid: [a-f0-9-]+/uid: <REDACTED>/g' \
  -e 's/creationTimestamp: .+/creationTimestamp: <REDACTED>/g' \
  -e 's/resourceVersion: ".+"/resourceVersion: <REDACTED>/g' \
  -e 's/release: ".+"/release: <REDACTED>/g' \
  -e 's/(postgres-password|admin-password|password|secret|secretsDefault|CSRF_KEY|JOBSERVICE_SECRET|REGISTRY_HTTP_SECRET|REGISTRY_HTPASSWD|HARBOR_ADMIN_PASSWORD): "?[^"]*"?$/\1: <REDACTED>/g' \
  -e 's|checksum/[^:]+: [a-f0-9]+|checksum/<KEY>: <HASH>|g' \
  -e 's/^([[:space:]]+ca\.crt:) .*/\1 <REDACTED>/g' \
  -e 's/^([[:space:]]+ca\.key:) .*/\1 <REDACTED>/g' \
  -e 's/^([[:space:]]+tls\.crt:) .*/\1 <REDACTED>/g' \
  -e 's/^([[:space:]]+tls\.key:) .*/\1 <REDACTED>/g' \
  -e 's/^([[:space:]]+caBundle:) .*/\1 <REDACTED>/g' \
  -e 's/^([[:space:]]+rollme:) .*/\1 <REDACTED>/g' \
  -e 's/^([[:space:]]+token:) [A-Za-z0-9_-]{20,}/\1 <REDACTED>/g'
