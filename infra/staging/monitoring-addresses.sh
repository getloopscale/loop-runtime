#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# monitoring-addresses.sh <loki|metrics> [--wait <seconds>]
#
# Prints, as shell exports for `eval`, the DNSNames of the internal ALBs staging's
# monitoring is reached through. The AWS Load Balancer Controller writes each one into
# the status of the Ingress it was made for, once the sync that created the Ingress is
# done, so `task staging:platform` reads them between the syncs:
#
#   loki      LOOPZERO_LOKI_ALB_DNS_NAME: the build cluster's Loki (monitoring/loki-internal),
#             read after the build sync, for the Promtail of the cells and of core.
#   metrics   LOOPZERO_METRICS_ALB_DNS_NAMES: `<cluster>=<DNSName>,...`, the federation
#             Prometheus of the build cluster and of each cell
#             (monitoring/prometheus-federation), read after the cells, for the core
#             Prometheus.
#
# Each value is printed double-quoted and unescaped: `published` admits only DNS name
# characters, and Task's shell interpreter keeps the backslashes `printf %q` would put
# before a comma, which ended up inside the scrape paths (deploy of a49a25d).
#
# Without --wait it prints what the clusters already publish (a previous deploy's ALBs)
# and nothing when one name is missing, which is the state of a rebuild before the syncs.
# With --wait it waits up to <seconds> for every name and fails, naming the Ingress,
# when one does not appear.
#
# Contexts, one per cluster, each named after the cluster (taskfiles/staging.yml):
#   BUILD_CONTEXT   the build cluster (default: build)
#   CELL_CONTEXTS   the cells, space-separated (default: "cell-0 cell-1")
set -euo pipefail

WHAT="${1:?usage: $0 <loki|metrics> [--wait <seconds>]}"
WAIT=""
if [[ $# -ge 2 ]]; then
  [[ "$2" == "--wait" && "${3:-}" =~ ^[0-9]+$ ]] || { echo "usage: $0 <loki|metrics> [--wait <seconds>]" >&2; exit 2; }
  WAIT="$3"
fi
BUILD_CONTEXT="${BUILD_CONTEXT:-build}"
CELL_CONTEXTS="${CELL_CONTEXTS:-cell-0 cell-1}"
INTERVAL="${MONITORING_ADDRESSES_INTERVAL:-5}"

# dns_name <context> <ingress>: the DNSName in the Ingress status, or "" while there is none.
dns_name() {
  kubectl --context "$1" -n monitoring get ingress "$2" \
    -o 'jsonpath={.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true
}

# published <context> <ingress>: the DNSName, waited for under --wait. Returns 1 when
# there is none and nothing is waited for, 2 when the wait ran out or the name is not a
# DNS name. It runs in a command substitution, so it returns rather than exits.
published() {
  local name deadline
  deadline=$(( $(date +%s) + ${WAIT:-0} ))
  while :; do
    name="$(dns_name "$1" "$2")"
    if [[ -n "$name" ]]; then
      if [[ ! "$name" =~ ^[A-Za-z0-9.-]+(:[0-9]+)?$ ]]; then
        echo "monitoring-addresses: $1 monitoring/$2 publishes '$name', which is not a DNS name" >&2
        return 2
      fi
      printf '%s' "$name"
      return 0
    fi
    if [[ -z "$WAIT" ]]; then
      return 1
    fi
    if (( $(date +%s) >= deadline )); then
      echo "monitoring-addresses: no ALB DNSName in the status of Ingress monitoring/$2 on $1 after ${WAIT}s" >&2
      return 2
    fi
    sleep "$INTERVAL"
  done
}

# read_name <context> <ingress>: sets name to published's DNSName; a missing name without --wait
# ends the script with nothing printed, any other failure ends it with an error.
read_name() {
  local rc=0
  name="$(published "$1" "$2")" || rc=$?
  case "$rc" in
    0) ;;
    1) exit 0 ;;
    *) exit 1 ;;
  esac
}

case "$WHAT" in
  loki)
    read_name "$BUILD_CONTEXT" loki-internal
    printf 'export LOOPZERO_LOKI_ALB_DNS_NAME="%s"\n' "$name"
    ;;
  metrics)
    pairs=()
    for ctx in "$BUILD_CONTEXT" $CELL_CONTEXTS; do
      read_name "$ctx" prometheus-federation
      pairs+=("$ctx=$name")
    done
    printf 'export LOOPZERO_METRICS_ALB_DNS_NAMES="%s"\n' "$(IFS=,; echo "${pairs[*]}")"
    ;;
  *)
    echo "usage: $0 <loki|metrics> [--wait <seconds>]" >&2
    exit 2
    ;;
esac
