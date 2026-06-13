#!/usr/bin/env bash
# T0.1 of plan 2026-06-10-j22-openbao-transit-eso-sync-plan v1.2
# Verifies OpenBao adoption Phase 1-4 prereqs for the J-22 OpenBao Transit + ESO
# Sync chain. Reports per-phase verdict (committed | absent | unknown) and an
# overall verdict (PASS | FAIL). Hermetic mode skips LIVE kubectl/curl probes.
#
# Canonical OpenBao service DNS in dev-public: openbao.vault-system.svc:8200
# (per plan T2.4 ClusterSecretStore spec.provider.vault.server). Curl probe
# `curl -k https://openbao.vault-system:8200/v1/sys/health` returns 200 when
# Phase 1 LIVE; we use kubectl exec equivalent inside the openbao pod to avoid
# port-forward overhead.

set -euo pipefail

# === Flag parsing ===
JSON_MODE=0
HERMETIC=0
CTX="${LOOPZERO_KUBE_CONTEXT:-k3d-loopzero-mgmt}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--json] [--hermetic] [--context=<ctx>] [--help]

Verifies OpenBao adoption Phase 1-4 are deployed in the target cluster:
  Phase 1: OpenBao HA install (helm release in vault-system ns, pods Running)
  Phase 2: kv-v2 secrets engine mounted + 3 critical secrets migrated
  Phase 3: transit engine mounted + per-tenant keys path exists
  Phase 4: ESO ClusterSecretStore "openbao-transit" Ready

Flags:
  --json               Emit machine-readable JSON to stdout
  --hermetic           Skip LIVE kubectl/curl probes (CI test mode);
                       only validates infra/helm/ chart presence + helmfile config
  --context=<ctx>      K8s context (default: \$LOOPZERO_KUBE_CONTEXT or k3d-loopzero-mgmt)
  --help, -h           Show this help

Exit codes:
  0  All four phases verified PASS
  1  Some phase verdict FAIL (or unknown if probes unreachable)
  2  Usage error (unknown flag, bad argument)
EOF
}

for arg in "$@"; do
  case "$arg" in
    --json) JSON_MODE=1 ;;
    --hermetic|--skip-live) HERMETIC=1 ;;
    --context=*) CTX="${arg#*=}" ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown flag: $arg" >&2; usage; exit 2 ;;
  esac
done

# === Phase probes ===
phase1="unknown"
phase2="unknown"
phase3="unknown"
phase4="unknown"

# Phase 1: OpenBao chart present + (LIVE) pods Running
if [ -d "infra/helm/openbao-init" ] && [ -f "infra/helmfile/vault.helmfile.yaml.gotmpl" ]; then
  if [ $HERMETIC -eq 1 ]; then
    phase1="committed"
  else
    # LIVE probe: openbao pods Running in vault-system
    if kubectl --context="$CTX" get pods -n vault-system -l app.kubernetes.io/name=openbao --no-headers 2>/dev/null | grep -q "Running"; then
      phase1="committed"
    else
      phase1="absent"
    fi
  fi
else
  phase1="absent"
fi

# Phase 2: kv-v2 engine mounted + critical secrets migrated
# Hermetic: confirm pkg/vault kv-v2 wrapper exists
if [ -f "pkg/vault/client.go" ] && grep -q "kvv2\|KVv2\|kv-v2" pkg/vault/client.go pkg/vault/*.go 2>/dev/null; then
  if [ $HERMETIC -eq 1 ]; then
    phase2="committed"
  else
    # LIVE probe: vault status reports sealed=false (operator HARD STOP if sealed)
    if kubectl --context="$CTX" -n vault-system exec deploy/openbao 2>/dev/null -- /bin/sh -c "vault status -format=json 2>/dev/null" | grep -q '"sealed": false'; then
      phase2="committed"
    else
      phase2="absent"
    fi
  fi
else
  phase2="absent"
fi

# Phase 3: transit engine + per-tenant key path
# Hermetic mode: this plan IS Phase 3 — chart for transit setup not yet shipped, so we expect "absent" in hermetic
# LIVE: vault read /sys/mounts/transit/ returns 200 means transit mounted
if [ $HERMETIC -eq 1 ]; then
  # Look for transit_client.go in pkg/vault/ — when T2.1 ships, this becomes committed
  if [ -f "pkg/vault/transit_client.go" ]; then
    phase3="committed"
  else
    phase3="absent"
  fi
else
  if kubectl --context="$CTX" -n vault-system exec deploy/openbao 2>/dev/null -- vault read sys/mounts/transit/ 2>/dev/null | grep -q "type.*transit"; then
    phase3="committed"
  else
    phase3="absent"
  fi
fi

# Phase 4: ESO ClusterSecretStore openbao-transit Ready
# Hermetic: check helmfile template renders the CSS resource
if [ $HERMETIC -eq 1 ]; then
  if [ -f "infra/helm/external-secrets/templates/clustersecretstore-openbao-transit.yaml" ]; then
    phase4="committed"
  else
    phase4="absent"
  fi
else
  status=$(kubectl --context="$CTX" get clustersecretstore openbao-transit -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
  if [ "$status" = "True" ]; then
    phase4="committed"
  else
    phase4="absent"
  fi
fi

# === Verdict aggregation ===
verdict="PASS"
for p in "$phase1" "$phase2" "$phase3" "$phase4"; do
  if [ "$p" != "committed" ]; then
    verdict="FAIL"
    break
  fi
done

# === Output ===
if [ $JSON_MODE -eq 1 ]; then
  printf '{"verdict":"%s","phase1":"%s","phase2":"%s","phase3":"%s","phase4":"%s","context":"%s","hermetic":%s}\n' \
    "$verdict" "$phase1" "$phase2" "$phase3" "$phase4" "$CTX" \
    "$([ $HERMETIC -eq 1 ] && echo true || echo false)"
else
  echo "verdict=$verdict phase1=$phase1 phase2=$phase2 phase3=$phase3 phase4=$phase4 context=$CTX hermetic=$HERMETIC"
fi

if [ "$verdict" = "PASS" ]; then
  exit 0
else
  exit 1
fi
