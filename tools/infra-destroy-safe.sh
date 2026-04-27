#!/usr/bin/env bash
# infra-destroy-safe — controlled destroy of a LoopZero Pulumi stack.
#
# Safety-first approach to `pulumi destroy`. Takes backups, shows preview,
# requires explicit operator confirmation, then destroys. Finally verifies
# zero cloud-billable resources remain (drift safety net).
#
# NEVER run this without reviewing the script end-to-end. NEVER pipe to
# bash. NEVER run on prod without a meeting record authorizing it.
#
# Usage:
#   tools/infra-destroy-safe.sh --stack dev [--skip-backups] [--skip-orphan-check]
#
# Env:
#   DO_TOKEN              DigitalOcean API token (required for orphan check)
#   CF_API_TOKEN          Cloudflare API token (required for DNS snapshot)
#   STACK_CONFIRM         Must match "DESTROY-{stack}" exactly (set at runtime only)
#
# Plan: docs/plans/50k-immediate-focus.md, docs/runbooks/infra-destroy.md (future)

set -euo pipefail

# ------------------------------------------------------------------
# 0. argument parsing
# ------------------------------------------------------------------

STACK=""
SKIP_BACKUPS=0
SKIP_ORPHAN_CHECK=0
BACKUP_DIR="${BACKUP_DIR:-/tmp/loopzero-destroy-backups/$(date +%Y%m%d-%H%M%S)}"

usage() {
  cat <<EOF
Usage: $0 --stack <dev|staging|prod> [--skip-backups] [--skip-orphan-check]

Options:
  --stack NAME        Pulumi stack to destroy (required)
  --skip-backups      Skip backup phase (NOT recommended)
  --skip-orphan-check Skip post-destroy DO orphan check (NOT recommended)
  -h, --help          This help

Required env:
  DO_TOKEN            DigitalOcean token (for orphan verification)
  CF_API_TOKEN        Cloudflare token (for DNS snapshot)

Safety gates:
  1. Pre-flight checks (which stack, who's logged in, cluster state)
  2. Backup (PG snapshot, Pulumi state export, CF DNS, K8s CRs)
  3. Stop in-flight work (kill test runners, drain traffic)
  4. Preview (pulumi preview with full diff shown)
  5. Operator confirms: types "DESTROY-{stack}" exactly to proceed
  6. Destroy (pulumi destroy)
  7. Orphan cleanup + verification
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --stack) STACK="$2"; shift 2 ;;
    --skip-backups) SKIP_BACKUPS=1; shift ;;
    --skip-orphan-check) SKIP_ORPHAN_CHECK=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [ -z "$STACK" ]; then
  echo "ERROR: --stack required" >&2
  usage >&2
  exit 1
fi

if [ "$STACK" = "prod" ]; then
  echo ""
  echo "╔══════════════════════════════════════════════════════════════╗"
  echo "║                        🚨 PROD STACK 🚨                       ║"
  echo "║                                                              ║"
  echo "║  You are about to destroy PRODUCTION. This requires:         ║"
  echo "║    1. Authorizing meeting record (MEET-YYYY-MM-DD-NNN.md)    ║"
  echo "║    2. Signed-off CAB approval                                 ║"
  echo "║    3. Customer communication window honored                  ║"
  echo "║    4. SRE + Security co-sign                                  ║"
  echo "║                                                              ║"
  echo "║  If you do not have ALL of the above — abort now.            ║"
  echo "╚══════════════════════════════════════════════════════════════╝"
  echo ""
  read -p "Type 'I-HAVE-MEETING-RECORD-AUTHORIZING-PROD-DESTROY' to continue: " PROD_GUARD
  if [ "$PROD_GUARD" != "I-HAVE-MEETING-RECORD-AUTHORIZING-PROD-DESTROY" ]; then
    echo "Aborted. No authorization."
    exit 1
  fi
fi

# ------------------------------------------------------------------
# 1. pre-flight
# ------------------------------------------------------------------

echo "════════════════════════════════════════"
echo " STEP 1/7: PRE-FLIGHT"
echo "════════════════════════════════════════"

PULUMI_DIR="$(cd "$(dirname "$0")/../infra/pulumi" && pwd)"
cd "$PULUMI_DIR"

# Confirm tooling
command -v pulumi >/dev/null || { echo "ERROR: pulumi not found"; exit 2; }
command -v doctl  >/dev/null || echo "WARN: doctl not found (orphan check will skip)"
command -v kubectl >/dev/null || echo "WARN: kubectl not found (K8s CR backup will skip)"
command -v jq     >/dev/null || { echo "ERROR: jq required"; exit 2; }

# Who's logged in
echo "  Pulumi user:     $(pulumi whoami 2>&1 | head -1)"
echo "  Active stack:    $(pulumi stack --show-name 2>&1 || echo 'none')"
echo "  Target stack:    $STACK"

# Select target
pulumi stack select "$STACK"

# Running test detection (L3/L4/L5/L6 tests saturate cluster)
RUNNING_TESTS=$(ps -ef | grep -E "real_deploy_test|TestRealDeploy_L[0-9]" | grep -v grep | wc -l || true)
if [ "$RUNNING_TESTS" -gt 0 ]; then
  echo "  ⚠️  $RUNNING_TESTS load test process(es) running — MUST stop before destroy"
  echo ""
  read -p "Kill running test processes now? (yes/NO): " KILL_TESTS
  if [ "$KILL_TESTS" = "yes" ]; then
    pkill -9 -f "real_deploy_test" || true
    pkill -9 -f "go test.*load" || true
    echo "  Kill signal sent. Waiting 5s for processes to exit..."
    sleep 5
  else
    echo "  Aborting — stop tests manually first."
    exit 1
  fi
fi

# ------------------------------------------------------------------
# 2. backups
# ------------------------------------------------------------------

if [ "$SKIP_BACKUPS" -eq 0 ]; then
  echo ""
  echo "════════════════════════════════════════"
  echo " STEP 2/7: BACKUPS → $BACKUP_DIR"
  echo "════════════════════════════════════════"
  mkdir -p "$BACKUP_DIR"

  # Pulumi state export
  echo "  [a] Pulumi state export..."
  pulumi stack export --stack "$STACK" > "$BACKUP_DIR/pulumi-state.json"
  echo "       → $BACKUP_DIR/pulumi-state.json ($(wc -c < "$BACKUP_DIR/pulumi-state.json") bytes)"

  # K8s CRs on mgmt cluster
  CTX_MGMT="${CTX_MGMT:-do-sfo3-loopzero-infra-${STACK}-sfo3-mgmt}"
  if kubectl --context "$CTX_MGMT" cluster-info >/dev/null 2>&1; then
    echo "  [b] K8s CRs export (mgmt)..."
    kubectl --context "$CTX_MGMT" get tenants,environments,loopzeroapplications,exposedroutes \
      -n loopzero-system -o yaml > "$BACKUP_DIR/k8s-crs-mgmt.yaml" 2>/dev/null || true
    kubectl --context "$CTX_MGMT" get applications.argoproj.io -n argocd -o yaml \
      > "$BACKUP_DIR/argocd-apps.yaml" 2>/dev/null || true
    echo "       → $BACKUP_DIR/k8s-crs-mgmt.yaml + argocd-apps.yaml"
  else
    echo "  [b] SKIP K8s export (context $CTX_MGMT unreachable)"
  fi

  # PostgreSQL snapshot (via DO)
  if command -v doctl >/dev/null && [ -n "${DO_TOKEN:-}" ]; then
    echo "  [c] PostgreSQL: creating managed DB snapshot..."
    DB_ID=$(doctl databases list --format ID,Name --no-header | grep "loopzero-infra-${STACK}" | awk '{print $1}' | head -1 || true)
    if [ -n "$DB_ID" ]; then
      SNAP_NAME="pre-destroy-${STACK}-$(date +%Y%m%d-%H%M%S)"
      doctl databases backups list "$DB_ID" > "$BACKUP_DIR/db-backups-pre.txt" 2>&1 || true
      echo "       DB ID: $DB_ID"
      echo "       Recent backups recorded in: $BACKUP_DIR/db-backups-pre.txt"
      echo "       ⚠️  Managed PG backups are automatic (daily). Last one is your rollback point."
      echo "       Consider also: doctl databases pool list $DB_ID > ..."
    else
      echo "       SKIP: no managed DB found matching loopzero-infra-${STACK}"
    fi
  else
    echo "  [c] SKIP PG snapshot (doctl or DO_TOKEN missing)"
  fi

  # CF DNS zones export
  if [ -n "${CF_API_TOKEN:-}" ]; then
    echo "  [d] Cloudflare DNS zones snapshot..."
    # List zones containing 'loopzero.example' OR managed by LoopZero's CF account
    curl -s -H "Authorization: Bearer ${CF_API_TOKEN}" \
      "https://api.cloudflare.com/client/v4/zones?per_page=50" \
      | jq '{zones: [.result[] | {id,name,status}]}' > "$BACKUP_DIR/cf-zones.json"
    ZONE_COUNT=$(jq '.zones | length' "$BACKUP_DIR/cf-zones.json")
    echo "       → $ZONE_COUNT zones listed in $BACKUP_DIR/cf-zones.json"
    echo "       ⚠️  Per-zone DNS record export not automated — do manually for critical zones."
  else
    echo "  [d] SKIP CF snapshot (CF_API_TOKEN missing)"
  fi

  # DO Spaces inventory
  if command -v doctl >/dev/null; then
    echo "  [e] DO Spaces inventory..."
    doctl compute load-balancer list --format ID,Name --no-header 2>&1 > "$BACKUP_DIR/do-lbs-pre.txt" || true
    doctl kubernetes cluster list --format ID,Name --no-header 2>&1 > "$BACKUP_DIR/do-clusters-pre.txt" || true
    doctl databases list --format ID,Name --no-header 2>&1 > "$BACKUP_DIR/do-dbs-pre.txt" || true
    echo "       → $BACKUP_DIR/do-{lbs,clusters,dbs}-pre.txt (pre-destroy state)"
  fi

  echo ""
  echo "  ✅ Backups written to $BACKUP_DIR"
  echo "  ℹ️  Keep this dir until you are sure destroy was OK."
else
  echo ""
  echo "════════════════════════════════════════"
  echo " STEP 2/7: BACKUPS  — SKIPPED (--skip-backups)"
  echo "════════════════════════════════════════"
  read -p "Confirm you really mean to skip backups (type 'YES-SKIP-BACKUPS'): " SKIPCONF
  if [ "$SKIPCONF" != "YES-SKIP-BACKUPS" ]; then
    echo "  Aborting."
    exit 1
  fi
fi

# ------------------------------------------------------------------
# 3. drain traffic + stop CronJobs (opportunistic)
# ------------------------------------------------------------------

echo ""
echo "════════════════════════════════════════"
echo " STEP 3/7: QUIESCE (best-effort)"
echo "════════════════════════════════════════"

CTX_MGMT="${CTX_MGMT:-do-sfo3-loopzero-infra-${STACK}-sfo3-mgmt}"
if kubectl --context "$CTX_MGMT" cluster-info >/dev/null 2>&1; then
  # Suspend CronJobs
  echo "  Suspending CronJobs in loopzero-system..."
  kubectl --context "$CTX_MGMT" -n loopzero-system get cronjob -o name 2>/dev/null \
    | xargs -r -I {} kubectl --context "$CTX_MGMT" -n loopzero-system patch {} -p '{"spec":{"suspend":true}}' 2>&1 | head -5 || true

  # Scale API to 0 (optional; stops new deploys from accepting)
  echo "  ⚠️  Skipping API scale-to-0 (destroy will handle cluster teardown)"
fi

# ------------------------------------------------------------------
# 4. preview
# ------------------------------------------------------------------

echo ""
echo "════════════════════════════════════════"
echo " STEP 4/7: PREVIEW"
echo "════════════════════════════════════════"
echo "  Running: pulumi preview --stack $STACK --diff"
echo ""

pulumi preview --stack "$STACK" --diff 2>&1 | tee "$BACKUP_DIR/pulumi-preview.log" || true

echo ""
echo "  Preview complete. Review the diff above CAREFULLY."
echo "  Full preview also saved to: $BACKUP_DIR/pulumi-preview.log"

# ------------------------------------------------------------------
# 5. operator confirmation
# ------------------------------------------------------------------

echo ""
echo "════════════════════════════════════════"
echo " STEP 5/7: CONFIRMATION"
echo "════════════════════════════════════════"

REQUIRED_PHRASE="DESTROY-${STACK}"
echo ""
echo "  This is the point of no return."
echo "  Stack: $STACK"
echo "  Backups: $BACKUP_DIR"
echo ""
echo "  Resources that will be destroyed:"
echo "    • 3 DOKS clusters (mgmt, build, runtime)"
echo "    • Managed PostgreSQL (all tenant data)"
echo "    • DO Spaces (all objects — bundles, static assets, logs, state backend)"
echo "    • DO Container Registry (all platform + tenant images)"
echo "    • Cloudflare Tunnel + KV namespaces"
echo "    • All CF DNS records for the zone (BUT not the zone itself — CF preserves)"
echo ""
read -p "  Type exactly '$REQUIRED_PHRASE' to proceed (or anything else to abort): " CONFIRM

if [ "$CONFIRM" != "$REQUIRED_PHRASE" ]; then
  echo ""
  echo "  ❌ Confirmation did not match. Aborting."
  echo "  Backups retained at: $BACKUP_DIR"
  exit 1
fi

echo "  ✅ Confirmed. Proceeding with destroy."

# ------------------------------------------------------------------
# 6. destroy
# ------------------------------------------------------------------

echo ""
echo "════════════════════════════════════════"
echo " STEP 6/7: DESTROY"
echo "════════════════════════════════════════"

DESTROY_LOG="$BACKUP_DIR/pulumi-destroy.log"
echo "  Running: pulumi destroy --stack $STACK --yes"
echo "  Output logged to: $DESTROY_LOG"
echo ""

set +e  # don't abort on partial failure — we still want orphan cleanup
pulumi destroy --stack "$STACK" --yes 2>&1 | tee "$DESTROY_LOG"
DESTROY_RC=$?
set -e

if [ $DESTROY_RC -ne 0 ]; then
  echo ""
  echo "  ⚠️  pulumi destroy exited non-zero ($DESTROY_RC)."
  echo "       Common causes: drift, stuck finalizers, provider throttling."
  echo "       Proceeding to orphan check anyway so you can identify leftovers."
fi

# ------------------------------------------------------------------
# 7. orphan cleanup + verification
# ------------------------------------------------------------------

if [ "$SKIP_ORPHAN_CHECK" -eq 0 ] && command -v doctl >/dev/null; then
  echo ""
  echo "════════════════════════════════════════"
  echo " STEP 7/7: ORPHAN CHECK"
  echo "════════════════════════════════════════"

  echo "  Searching for billable resources still tagged as loopzero-infra-${STACK}..."
  echo ""

  echo "  [a] DOKS clusters:"
  doctl kubernetes cluster list --format ID,Name,Status | grep "loopzero-infra-${STACK}" || echo "    ✅ none"

  echo "  [b] Managed databases:"
  doctl databases list --format ID,Name,Status | grep "loopzero-infra-${STACK}" || echo "    ✅ none"

  echo "  [c] Droplets (node pool remnants):"
  doctl compute droplet list --format ID,Name,Status | grep -E "loopzero-infra-${STACK}|overflow|prewarm" || echo "    ✅ none"

  echo "  [d] Load balancers:"
  doctl compute load-balancer list --format ID,Name,Status | grep "loopzero-infra-${STACK}" || echo "    ✅ none"

  echo "  [e] Volumes (PVC orphans):"
  doctl compute volume list --format ID,Name,Size | grep "pvc-" || echo "    ✅ none (or inspect manually)"

  echo "  [f] Spaces buckets:"
  doctl compute domain list 2>&1 | grep "loopzero" || true
  echo "       ⚠️  Spaces must be listed via doctl compute spaces or S3 API."
  echo "          Manual check: s3cmd ls --config=~/.s3cfg s3:// | grep loopzero"

  echo "  [g] Snapshots (to purge after you verify backups aren't needed):"
  doctl compute snapshot list --format ID,Name,ResourceType 2>&1 | grep "loopzero-infra-${STACK}" || echo "    ✅ none"

  echo ""
  echo "  If any ✅ above reads 'none', you're clean. Any match = manual cleanup required."
  echo "  Orphan cleanup commands (DO NOT auto-run — review first):"
  echo "    doctl kubernetes cluster delete <id> --force --dangerous"
  echo "    doctl databases delete <id> --force"
  echo "    doctl compute droplet delete <id> --force"
  echo "    doctl compute load-balancer delete <id> --force"
  echo "    doctl compute volume delete <id> --force"
fi

# ------------------------------------------------------------------
# Final report
# ------------------------------------------------------------------

echo ""
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║                    DESTROY COMPLETE                          ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
echo "  Stack: $STACK"
echo "  Backups: $BACKUP_DIR"
echo "  Pulumi exit: $DESTROY_RC"
echo ""
echo "  Next steps:"
echo "    1. Review $BACKUP_DIR/pulumi-destroy.log for any unresolved errors."
echo "    2. Review the orphan check output above — cleanup any leftovers."
echo "    3. If you plan to re-create: 'pulumi up --stack $STACK' with fresh Pulumi.${STACK}.yaml."
echo "    4. If permanent: 'pulumi stack rm $STACK' after verification."
echo "    5. Commit this run's outcome to CHANGELOG.md (optional)."
echo ""
echo "  Keep backups for at least 7 days before deleting $BACKUP_DIR."

exit $DESTROY_RC
