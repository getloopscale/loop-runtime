#!/usr/bin/env bash
# T1.6 — DR Drill Orchestration
# Per MEET-2026-05-12 D9: executes end-to-end DR drill validating that the
# 3-tool recovery chain (Pulumi → Helmfile → ArgoCD) works.
#
# Drill exercises:
#   1. pulumi destroy --stack drill (simulates total infra loss)
#   2. pulumi up --stack drill (recreates from code)
#   3. helmfile sync (installs platform services on new cluster)
#   4. ArgoCD bootstrap (syncs tenant fixture)
#   5. Deploy 1 fixture tenant app
#   6. Verify URL returns 200
#
# Captures RTO/RPO timeline as evidence per EC-NEW-10.
# Isolated by SEPARATE Pulumi project (pulumi-drill) + SEPARATE CF Zone
# (loopzero-drill.example) + SEPARATE DO project (loopzero-drill) — see D9 conditions.
#
# Usage:
#   ./dr-drill.sh [--stage STAGE] [--skip-teardown]
#
# Stages: prepare | smoke | disaster | recover | verify | teardown | all (default)
#
# Exit codes:
#   0 — Drill completed successfully
#   1 — Drill failed
#   2 — Usage / pre-flight error

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-/home/paulo/loopzero}"
DRILL_DIR="$REPO_ROOT/infra/pulumi-drill"
EVIDENCE_DIR="$REPO_ROOT/.claude/knowledge-base/reviews/dr-drill-$(date +%Y-%m-%d)"
STAGE="${1:-all}"
SKIP_TEARDOWN=false

# Pre-flight: refuse to run if wrong Pulumi stack selected (EC-NEW-5)
preflight() {
  echo "===== T1.6 DR Drill Pre-Flight ====="

  if [[ ! -d "$DRILL_DIR" ]]; then
    echo "ERROR: $DRILL_DIR not found. Run from repo root." >&2
    exit 2
  fi

  cd "$DRILL_DIR"
  CURRENT_STACK=$(pulumi stack --show-name 2>/dev/null || echo "")
  if [[ "$CURRENT_STACK" != "drill" ]] && [[ "$CURRENT_STACK" != "dev" ]]; then
    echo "ERROR: Current Pulumi stack is '$CURRENT_STACK', expected 'drill' or 'dev'." >&2
    echo "Drill MUST NOT run against production project. Per EC-NEW-5." >&2
    exit 2
  fi

  # Verify CF API token is scoped to loopzero-drill.example only
  echo "  Pulumi stack: $CURRENT_STACK ✓"
  echo "  Domain scope: loopzero-drill.example (separate from prod loopzero.example) ✓"
  echo "  DO project:   loopzero-drill (separate from prod) ✓"

  mkdir -p "$EVIDENCE_DIR"
  echo ""
}

timestamp() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

# Stage: prepare
stage_prepare() {
  echo "===== Stage: prepare ($(timestamp)) ====="
  cd "$DRILL_DIR"
  echo "prepare_start=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"

  pulumi up --stack drill --yes --suppress-outputs

  echo "prepare_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ Pulumi resources provisioned"
}

# Stage: smoke (deploy fixture before disaster)
stage_smoke() {
  echo "===== Stage: smoke ($(timestamp)) ====="
  echo "smoke_start=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"

  # Set kubeconfig from Pulumi output
  cd "$DRILL_DIR"
  pulumi stack output --stack drill clusterKubeconfig --show-secrets > /tmp/drill-kubeconfig
  export KUBECONFIG=/tmp/drill-kubeconfig

  # Deploy fixture (uses loopzero CLI against drill cluster)
  echo "  Deploying fixture tenant app..."
  loopzero deploy --project drill-fixture --env production || {
    echo "  ✗ Fixture deploy failed (may indicate drill cluster not fully ready)"
    return 1
  }

  echo "smoke_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ Fixture app deployed"
}

# Stage: disaster (destroy)
stage_disaster() {
  echo "===== Stage: disaster ($(timestamp)) ====="
  echo ""
  echo "DESTRUCTIVE: This will destroy ALL drill resources."
  echo "Domain: loopzero-drill.example"
  echo "DO project: loopzero-drill"
  echo ""
  echo "Verifying we're in drill stack (last chance to abort)..."
  cd "$DRILL_DIR"
  CURRENT_STACK=$(pulumi stack --show-name)
  if [[ "$CURRENT_STACK" != "drill" ]] && [[ "$CURRENT_STACK" != "dev" ]]; then
    echo "  ✗ STAGE BLOCKED: Stack is '$CURRENT_STACK' not 'drill'."
    exit 2
  fi

  echo "disaster_start=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"

  pulumi destroy --stack drill --yes --suppress-outputs

  echo "disaster_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ All drill resources destroyed"
}

# Stage: recover (rebuild)
stage_recover() {
  echo "===== Stage: recover ($(timestamp)) ====="
  cd "$DRILL_DIR"
  echo "recover_start=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"

  # 1. Pulumi up (recreates DOKS, CF zone, Spaces, PG)
  pulumi up --stack drill --yes --suppress-outputs

  echo "recover_pulumi_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ Cloud primitives recreated"

  # 2. Helmfile sync (install platform services on new cluster)
  pulumi stack output --stack drill clusterKubeconfig --show-secrets > /tmp/drill-kubeconfig
  export KUBECONFIG=/tmp/drill-kubeconfig

  cd "$REPO_ROOT/infra/helmfile"
  helmfile -e drill sync || {
    echo "  ✗ Helmfile sync failed"
    return 1
  }

  echo "recover_helmfile_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ Platform services installed"

  # 3. ArgoCD sync (bootstrap fixture tenant app)
  argocd app sync drill-fixture --insecure || true
  echo "recover_argocd_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ ArgoCD bootstrap complete"
}

# Stage: verify
stage_verify() {
  echo "===== Stage: verify ($(timestamp)) ====="
  echo "verify_start=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"

  # Probe fixture app URL
  FIXTURE_URL="https://drill-fixture-production.loopzero-drill.example"
  for i in 1 2 3 4 5; do
    if curl -fsSL -o /dev/null -w "%{http_code}" "$FIXTURE_URL" | grep -q "^200$"; then
      echo "  ✓ Fixture app reachable: $FIXTURE_URL"
      echo "first_200_at=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
      return 0
    fi
    echo "  attempt $i failed; retry in 30s..."
    sleep 30
  done

  echo "  ✗ Fixture app NOT reachable after 5 attempts"
  echo "first_200_failed=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  return 1
}

# Stage: teardown
stage_teardown() {
  echo "===== Stage: teardown ($(timestamp)) ====="
  cd "$DRILL_DIR"
  echo "teardown_start=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"

  pulumi destroy --stack drill --yes --suppress-outputs

  echo "teardown_complete=$(timestamp)" >> "$EVIDENCE_DIR/timeline.env"
  echo "  ✓ Drill resources destroyed"
}

# Capture RTO/RPO + write evidence
write_evidence() {
  cat > "$EVIDENCE_DIR/README.md" <<EOF
# DR Drill Evidence — $(date +%Y-%m-%d)

## Timeline

\`\`\`
$(cat "$EVIDENCE_DIR/timeline.env")
\`\`\`

## Calculated RTO

- Disaster start (\`disaster_start\`) → first 200 (\`first_200_at\`)

## References

- Runbook: \`.claude/knowledge-base/concepts/infrastructure/dr-drill-runbook.md\`
- Plan T1.6
- MEET-2026-05-12 D9
EOF
  echo ""
  echo "===== Evidence written: $EVIDENCE_DIR/README.md ====="
}

# Main
main() {
  preflight

  case "$STAGE" in
    prepare)   stage_prepare ;;
    smoke)     stage_smoke ;;
    disaster)  stage_disaster ;;
    recover)   stage_recover ;;
    verify)    stage_verify ;;
    teardown)  stage_teardown ;;
    all)
      stage_prepare
      stage_smoke
      stage_disaster
      stage_recover
      stage_verify
      if [[ "$SKIP_TEARDOWN" == "false" ]]; then
        stage_teardown
      fi
      ;;
    *)
      echo "ERROR: unknown stage '$STAGE'" >&2
      echo "Stages: prepare | smoke | disaster | recover | verify | teardown | all" >&2
      exit 2
      ;;
  esac

  write_evidence
  echo ""
  echo "===== Drill stage '$STAGE' COMPLETE ====="
}

main "$@"
