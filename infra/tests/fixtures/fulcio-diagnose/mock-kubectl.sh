#!/usr/bin/env bash
# Mock kubectl for fulcio-oidc-diagnose hermetic tests.
# Reads fixture path from $MOCK_KUBECTL_FIXTURE env var and returns
# fake responses per fixture fields. Hermetic — zero network calls.
#
# Coverage (per T0.2 plan v1.1 EC-7): run, exec, get cm, patch deploy,
# logs, delete pod, wait, rollout status, create.
#
# Tracks invocations to $MOCK_KUBECTL_INVOCATIONS_LOG (counter file)
# so cleanup tests can assert `patch deploy` (rollback) + `delete pod`
# were called even on error path.

set -euo pipefail

FIXTURE="${MOCK_KUBECTL_FIXTURE:-}"
INVOCATIONS_LOG="${MOCK_KUBECTL_INVOCATIONS_LOG:-/tmp/mock-kubectl-invocations.log}"

if [[ -z "$FIXTURE" || ! -f "$FIXTURE" ]]; then
  echo "MOCK ERROR: MOCK_KUBECTL_FIXTURE not set or file missing: '$FIXTURE'" >&2
  exit 99
fi

# Append full args to invocations log for test assertions.
echo "$*" >> "$INVOCATIONS_LOG"

# Find first non-flag arg = subcommand.
SUBCMD=""
for arg in "$@"; do
  case "$arg" in
    --*|-*) continue ;;
    *) SUBCMD="$arg"; break ;;
  esac
done

# Helper: extract a JSON field from fixture using jq.
field() {
  jq -r "$1 // empty" < "$FIXTURE"
}

case "$SUBCMD" in
  run)
    # `kubectl run sa-token-probe ... -- sh -c 'cat /var/run/secrets/sigstore/token'`
    # Returns fixture.sa_token_jwt
    if [[ "$*" == *"sa-token-probe"* || "$*" == *"sigstore/token"* ]]; then
      field '.sa_token_jwt'
    elif [[ "$*" == *"net-probe"* ]]; then
      # Net probe pod created — actual probes run via subsequent exec
      echo "pod/net-probe created"
    else
      echo "pod/$SUBCMD created (mock generic)"
    fi
    ;;
  exec)
    # `kubectl exec net-probe -- nslookup ...` OR `curl ...`
    if [[ "$*" == *"nslookup"* ]]; then
      DNS=$(field '.dns_status')
      if [[ "$DNS" == "ok" ]]; then
        echo "Server: 10.43.0.10"
        echo "Address: 10.43.0.10#53"
        echo "Name: kubernetes.default.svc.cluster.local"
        echo "Address: 10.43.0.1"
      else
        echo "** server can't find kubernetes.default.svc.cluster.local: NXDOMAIN" >&2
        exit 1
      fi
    elif [[ "$*" == *"openid-configuration"* ]]; then
      STATUS=$(field '.discovery_status')
      BODY=$(field '.discovery_body')
      if [[ "$STATUS" == "0" ]]; then
        # Timeout/connection error
        echo "$BODY" >&2
        exit 28
      fi
      # curl -w writes status code at end
      echo "$BODY"
      echo "$STATUS"
    elif [[ "$*" == *"jwks"* && "$*" == *"curl"* ]]; then
      STATUS=$(field '.jwks_status')
      if [[ "$STATUS" == "0" || -z "$STATUS" ]]; then
        echo "curl: (28) Connection timed out" >&2
        exit 28
      fi
      echo '{"keys":[{"kid":"mock-kid","kty":"RSA"}]}'
      echo "$STATUS"
    else
      echo "MOCK exec: $*" >&2
    fi
    ;;
  get)
    # `kubectl get cm fulcio-server-config -o jsonpath=...`
    if [[ "$*" == *"fulcio-server-config"* ]]; then
      ISSUERS=$(jq -c '.oidc_issuers' < "$FIXTURE")
      echo "{\"OIDCIssuers\":${ISSUERS}}"
    elif [[ "$*" == *"deploy"* && "$*" == *"fulcio-server"* ]]; then
      READY=$(field '.fulcio_ready')
      if [[ "$READY" == "true" ]]; then
        echo "fulcio-server   1/1     1            1           5h"
      else
        echo "fulcio-server   0/1     1            0           5h"
      fi
    else
      echo "MOCK get: $*" >&2
    fi
    ;;
  patch)
    # `kubectl patch deploy fulcio-server ...`
    # Idempotent noop — just track invocation.
    echo "deployment.apps/fulcio-server patched"
    ;;
  logs)
    # `kubectl logs deploy/fulcio-server`
    field '.fulcio_log_excerpt'
    ;;
  delete)
    # `kubectl delete pod ...`
    echo "pod \"$(echo "$@" | grep -oE 'sa-token-probe[^ ]*|net-probe[^ ]*' | head -1)\" deleted"
    ;;
  wait)
    # `kubectl wait --for=condition=Available deploy/fulcio-server`
    READY=$(field '.fulcio_ready')
    if [[ "$READY" == "true" ]]; then
      echo "deployment.apps/fulcio-server condition met"
      exit 0
    fi
    echo "error: timed out waiting for the condition" >&2
    exit 1
    ;;
  rollout)
    # `kubectl rollout status deploy/fulcio-server`
    READY=$(field '.fulcio_ready')
    if [[ "$READY" == "true" ]]; then
      echo "deployment \"fulcio-server\" successfully rolled out"
      exit 0
    fi
    echo "Waiting for deployment \"fulcio-server\" rollout to finish: 0 of 1 updated replicas are available..." >&2
    exit 1
    ;;
  create)
    # `kubectl create ...` (e.g., temporary configmap for net-probe)
    echo "$* created (mock)"
    ;;
  *)
    echo "MOCK unimplemented subcmd: $SUBCMD (full: $*)" >&2
    exit 99
    ;;
esac
