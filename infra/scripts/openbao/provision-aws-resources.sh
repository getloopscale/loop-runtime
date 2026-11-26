#!/usr/bin/env bash
#
# provision-aws-resources.sh — OpenBao auto-unseal AWS resources (per-env)
#
# Provisions (idempotent, per-env):
#   - AWS KMS CMK + alias  (alias/loopruntime-openbao-${ENV})
#   - AWS IAM user + inline policy (openbao-unseal-${ENV}, path /loopruntime/)
#   - AWS IAM access key (1 per user)
#   - DigitalOcean Spaces bucket (loopruntime-openbao-backups-${ENV}, per-env region)
#
# Output: $HOME/.openbao-${ENV}-resources.env  (chmod 600)
#
# Per ADR-2026-06-02-openbao-adoption Q2 (AWS KMS HSM root-of-trust)
# and SCRIPT plan ratified 2026-06-02 (DOxAWS lifecycle independence).
#
# Pre-flight: aws CLI + jq + .env with SPACES_* + `aws sts get-caller-identity` OK
#
# Flags:
#   --env <dev|staging|prod>     required
#   --destroy                    delete IAM user + bucket (KMS preserved by design)
#   --rotate-key                 delete + recreate IAM access key only (KMS+user kept)
#   --dry-run                    print actions, mutate nothing
#   -h | --help                  this message
#
# Idempotency: re-run = no-op. Each resource pre-checked; created only if absent.
# Allowlisted in .claude/rules/shell-scripts-allowlist.md (SCRIPT.4).

set -euo pipefail

# ---- constants & paths -------------------------------------------------------

readonly SCRIPT_NAME="$(basename "$0")"
readonly REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
readonly ENV_FILE="${REPO_ROOT}/.env"
readonly AWS_REGION="us-east-1"  # KMS HSM region (single region — KMS is regional)
readonly IAM_PATH="/loopruntime/"
readonly DO_ENDPOINT_TPL="https://%s.digitaloceanspaces.com"

# Per-env DO Spaces region matrix (Q12 + EC-13 cross-region DR for prod).
declare -A DO_REGION_MATRIX=(
  [dev]="sfo3"      # co-located with mgmt sfo3
  [staging]="nyc3"  # co-located with mgmt nyc3
  [prod]="sfo3"     # cross-region from mgmt nyc3 (DR survives region outage)
)

# ---- cli output helpers ------------------------------------------------------

if [[ -t 1 ]]; then
  C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YLW=$'\033[33m'
  C_BLU=$'\033[34m'; C_BLD=$'\033[1m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_YLW=""; C_BLU=""; C_BLD=""; C_RST=""
fi

log_info()  { echo "${C_BLU}[INFO]${C_RST} $*"; }
log_ok()    { echo "${C_GRN}[ OK ]${C_RST} $*"; }
log_warn()  { echo "${C_YLW}[WARN]${C_RST} $*" >&2; }
log_err()   { echo "${C_RED}[ERR ]${C_RST} $*" >&2; }
log_step()  { echo ""; echo "${C_BLD}== $* ==${C_RST}"; }

# ---- usage -------------------------------------------------------------------

usage() {
  cat <<EOF
${C_BLD}${SCRIPT_NAME}${C_RST} — OpenBao auto-unseal AWS resources (per-env)

USAGE
  $SCRIPT_NAME --env <dev|staging|prod>            # provision (idempotent)
  $SCRIPT_NAME --env <dev|staging|prod> --rotate-key  # rotate access key
  $SCRIPT_NAME --env <dev|staging|prod> --destroy     # destroy IAM+bucket
  $SCRIPT_NAME --env <dev|staging|prod> --dry-run     # preview
  $SCRIPT_NAME -h | --help

OUTPUT
  \$HOME/.openbao-<env>-resources.env  (chmod 600)

ENV
  AWS_PROFILE   override profile (default: from \`aws configure\`)
  VERBOSE=1     extra logging

NOTES
  - KMS CMK is NEVER deleted by --destroy (operator runs \`aws kms schedule-key-deletion\` manually).
  - --rotate-key deletes existing access key and creates a new one (user+policy preserved).
  - Bucket deletion via --destroy uses \`aws s3 rb --force\` (deletes all objects).
EOF
}

# ---- arg parse ---------------------------------------------------------------

ENV=""
ACTION="provision"
DRY_RUN=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)         ENV="${2:-}"; shift 2 ;;
    --destroy)     ACTION="destroy"; shift ;;
    --rotate-key)  ACTION="rotate-key"; shift ;;
    --dry-run)     DRY_RUN=1; shift ;;
    -h|--help)     usage; exit 0 ;;
    *)             log_err "Unknown arg: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$ENV" ]]; then
  log_err "Missing required --env"; usage; exit 1
fi
if [[ -z "${DO_REGION_MATRIX[$ENV]:-}" ]]; then
  log_err "Invalid --env: $ENV (must be dev|staging|prod)"; exit 1
fi

readonly ENV
readonly ACTION
readonly DRY_RUN
readonly DO_REGION="${DO_REGION_MATRIX[$ENV]}"
readonly DO_ENDPOINT="$(printf "$DO_ENDPOINT_TPL" "$DO_REGION")"

# Per-env resource names (suffix = ENV; AWS IAM global, DO bucket global).
readonly KMS_ALIAS="alias/loopruntime-openbao-${ENV}"
readonly IAM_USER="openbao-unseal-${ENV}"
readonly IAM_POLICY_NAME="openbao-unseal-policy"
readonly BUCKET="loopruntime-openbao-backups-${ENV}"
readonly OUTPUT_FILE="$HOME/.openbao-${ENV}-resources.env"

# ---- dry-run helper ----------------------------------------------------------

run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log_info "[DRY-RUN] $*"
    return 0
  fi
  "$@"
}

# ---- pre-flight --------------------------------------------------------------

preflight() {
  log_step "Pre-flight checks"

  command -v aws >/dev/null || { log_err "aws CLI not installed"; exit 1; }
  command -v jq  >/dev/null || { log_err "jq not installed"; exit 1; }
  log_ok "aws + jq present"

  if [[ ! -f "$ENV_FILE" ]]; then
    log_err ".env not found at: $ENV_FILE"; exit 1
  fi
  # Extract only SPACES_* keys via grep — avoids `source` choking on URL-style lines
  # (e.g. `URL=http://user:pass@host` would be parsed as commands by bash).
  SPACES_ACCESS_KEY_ID="$(grep -E '^SPACES_ACCESS_KEY_ID=' "$ENV_FILE" | head -1 | cut -d= -f2-)"
  SPACES_SECRET_ACCESS_KEY="$(grep -E '^SPACES_SECRET_ACCESS_KEY=' "$ENV_FILE" | head -1 | cut -d= -f2-)"
  export SPACES_ACCESS_KEY_ID SPACES_SECRET_ACCESS_KEY
  if [[ -z "${SPACES_ACCESS_KEY_ID:-}" || -z "${SPACES_SECRET_ACCESS_KEY:-}" ]]; then
    log_err ".env missing SPACES_ACCESS_KEY_ID or SPACES_SECRET_ACCESS_KEY"; exit 1
  fi
  log_ok ".env loaded (SPACES_* present)"

  # Validate AWS creds. Avoid leaking SPACES creds into AWS calls.
  local ident
  if ! ident=$(env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY \
        aws sts get-caller-identity --output json 2>&1); then
    log_err "aws sts get-caller-identity failed:"
    echo "$ident" >&2
    log_err "Hint: run \`aws configure\` or export AWS_PROFILE"
    exit 1
  fi
  local arn account
  arn=$(echo "$ident" | jq -r .Arn)
  account=$(echo "$ident" | jq -r .Account)
  log_ok "AWS identity: $arn (account $account)"

  log_info "Target env=${ENV} action=${ACTION} dry_run=${DRY_RUN}"
  log_info "  KMS alias:  $KMS_ALIAS  (region $AWS_REGION)"
  log_info "  IAM user:   $IAM_USER  (path $IAM_PATH)"
  log_info "  DO bucket:  $BUCKET  (region $DO_REGION)"
  log_info "  Output:     $OUTPUT_FILE"
}

# ---- aws wrappers ------------------------------------------------------------
# `aws_*` helpers strip SPACES_* env vars so they never leak into AWS API calls.

aws_kms()   { env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY aws kms   --region "$AWS_REGION" "$@"; }
aws_iam()   { env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY aws iam "$@"; }
aws_sts()   { env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY aws sts "$@"; }

# DO Spaces wrapper — pins SPACES_* creds only (not host AWS creds).
do_s3() {
  AWS_ACCESS_KEY_ID="$SPACES_ACCESS_KEY_ID" \
    AWS_SECRET_ACCESS_KEY="$SPACES_SECRET_ACCESS_KEY" \
    aws --endpoint-url "$DO_ENDPOINT" --region "$DO_REGION" s3 "$@"
}

# ---- KMS ---------------------------------------------------------------------

ensure_kms() {
  log_step "KMS CMK"

  local key_id key_arn
  if key_id=$(aws_kms describe-key --key-id "$KMS_ALIAS" --query 'KeyMetadata.KeyId' --output text 2>/dev/null); then
    key_arn=$(aws_kms describe-key --key-id "$KMS_ALIAS" --query 'KeyMetadata.Arn' --output text)
    log_ok "Reuse existing KMS key: $key_id"
  else
    log_info "Create new KMS CMK..."
    local create_json
    create_json=$(run aws_kms create-key \
      --description "OpenBao Raft seal key (${ENV}) — DO NOT DELETE. Loss permanently invalidates DO Spaces snapshots." \
      --key-usage ENCRYPT_DECRYPT \
      --key-spec SYMMETRIC_DEFAULT \
      --output json 2>&1) || { log_err "kms create-key failed: $create_json"; exit 1; }

    if [[ "$DRY_RUN" -eq 1 ]]; then
      key_id="DRY-RUN-key-id"
      key_arn="arn:aws:kms:${AWS_REGION}:DRYRUN:key/DRY-RUN"
    else
      key_id=$(echo "$create_json" | jq -r .KeyMetadata.KeyId)
      key_arn=$(echo "$create_json" | jq -r .KeyMetadata.Arn)
      run aws_kms enable-key-rotation --key-id "$key_id" >/dev/null
      run aws_kms create-alias --alias-name "$KMS_ALIAS" --target-key-id "$key_id"
    fi
    log_ok "KMS created: id=$key_id alias=$KMS_ALIAS rotation=enabled"
  fi

  export OUT_KMS_KEY_ID="$key_id"
  export OUT_KMS_KEY_ARN="$key_arn"
}

# ---- IAM user + policy + key -------------------------------------------------

ensure_iam_user() {
  log_step "IAM user + policy"

  if aws_iam get-user --user-name "$IAM_USER" >/dev/null 2>&1; then
    log_ok "Reuse existing IAM user: $IAM_USER"
  else
    log_info "Create IAM user: $IAM_USER (path $IAM_PATH)"
    run aws_iam create-user --user-name "$IAM_USER" --path "$IAM_PATH" >/dev/null
    log_ok "IAM user created"
  fi

  local policy_doc
  policy_doc=$(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "OpenBaoAutoUnseal",
      "Effect": "Allow",
      "Action": [
        "kms:Encrypt",
        "kms:Decrypt",
        "kms:DescribeKey",
        "kms:GenerateDataKey"
      ],
      "Resource": "${OUT_KMS_KEY_ARN}"
    }
  ]
}
EOF
)
  log_info "Put inline policy: $IAM_POLICY_NAME (resource-scoped to KMS key)"
  run aws_iam put-user-policy \
    --user-name "$IAM_USER" \
    --policy-name "$IAM_POLICY_NAME" \
    --policy-document "$policy_doc"
  log_ok "Inline policy attached"
}

ensure_access_key() {
  log_step "IAM access key"

  local key_count existing_keys
  if existing_keys=$(aws_iam list-access-keys --user-name "$IAM_USER" --output json 2>/dev/null); then
    key_count=$(echo "$existing_keys" | jq '.AccessKeyMetadata | length')
  else
    key_count=0
  fi

  if [[ "$key_count" -gt 0 ]]; then
    log_warn "IAM user already has $key_count access key(s). Re-run with --rotate-key to rotate."
    log_warn "Access key SECRET cannot be retrieved post-creation; output file will reuse cached values if present."
    if [[ -f "$OUTPUT_FILE" ]]; then
      log_ok "Existing output file found: $OUTPUT_FILE (will not overwrite secrets)"
    else
      log_err "No cached output file and access key secret unavailable from AWS."
      log_err "Run with --rotate-key to mint a new access key + capture secret."
      exit 1
    fi
    export OUT_ACCESS_KEY_ID=""  # signal: do not rewrite output file
    return 0
  fi

  log_info "Create new access key for $IAM_USER..."
  local key_json
  if [[ "$DRY_RUN" -eq 1 ]]; then
    key_json='{"AccessKey":{"AccessKeyId":"DRYRUN","SecretAccessKey":"DRYRUN"}}'
  else
    key_json=$(aws_iam create-access-key --user-name "$IAM_USER" --output json)
  fi
  export OUT_ACCESS_KEY_ID="$(echo "$key_json" | jq -r .AccessKey.AccessKeyId)"
  export OUT_SECRET_ACCESS_KEY="$(echo "$key_json" | jq -r .AccessKey.SecretAccessKey)"
  log_ok "Access key created: ${OUT_ACCESS_KEY_ID:0:8}***"
}

# ---- DO Spaces bucket --------------------------------------------------------

ensure_bucket() {
  log_step "DO Spaces bucket"

  if do_s3 ls "s3://${BUCKET}/" >/dev/null 2>&1; then
    log_ok "Reuse existing bucket: $BUCKET ($DO_REGION)"
  else
    log_info "Create bucket: $BUCKET in $DO_REGION..."
    run do_s3 mb "s3://${BUCKET}" --region "$DO_REGION"
    log_ok "Bucket created"
  fi
}

# ---- write output file -------------------------------------------------------

write_output() {
  log_step "Output file"

  if [[ -z "${OUT_ACCESS_KEY_ID:-}" ]]; then
    log_info "Skipping output write — access key was reused (secret not retrievable)."
    return 0
  fi

  if [[ "$DRY_RUN" -eq 1 ]]; then
    log_info "[DRY-RUN] would write $OUTPUT_FILE"
    return 0
  fi

  local ts
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  umask 077
  cat > "$OUTPUT_FILE" <<EOF
# Generated by infra/scripts/openbao/provision-aws-resources.sh
# ENV=${ENV}
# Generated: ${ts}
# DO NOT COMMIT. Permissions: chmod 600 (enforced).
#
# Consumed by infra/helmfile/core.helmfile.yaml.gotmpl (openbao release).
# See docs/runbooks.md for rotation.

OPENBAO_AWS_REGION=${AWS_REGION}
OPENBAO_AWS_KMS_KEY_ID=${OUT_KMS_KEY_ID}
OPENBAO_AWS_KMS_KEY_ARN=${OUT_KMS_KEY_ARN}
OPENBAO_AWS_ACCESS_KEY_ID=${OUT_ACCESS_KEY_ID}
OPENBAO_AWS_SECRET_ACCESS_KEY=${OUT_SECRET_ACCESS_KEY}

OPENBAO_BACKUP_BUCKET=${BUCKET}
OPENBAO_BACKUP_REGION=${DO_REGION}
OPENBAO_BACKUP_ENDPOINT=${DO_ENDPOINT}
OPENBAO_BACKUP_ACCESS_KEY_ID=${SPACES_ACCESS_KEY_ID}
OPENBAO_BACKUP_SECRET_ACCESS_KEY=${SPACES_SECRET_ACCESS_KEY}
EOF
  chmod 600 "$OUTPUT_FILE"
  log_ok "Wrote $OUTPUT_FILE (chmod 600)"
}

# ---- destroy -----------------------------------------------------------------

destroy_iam() {
  log_step "DESTROY: IAM user $IAM_USER"

  if ! aws_iam get-user --user-name "$IAM_USER" >/dev/null 2>&1; then
    log_info "IAM user absent — nothing to delete"
    return 0
  fi

  local key_ids
  key_ids=$(aws_iam list-access-keys --user-name "$IAM_USER" \
    --query 'AccessKeyMetadata[].AccessKeyId' --output text 2>/dev/null || true)
  for kid in $key_ids; do
    log_info "Delete access key: $kid"
    run aws_iam delete-access-key --user-name "$IAM_USER" --access-key-id "$kid"
  done

  if aws_iam get-user-policy --user-name "$IAM_USER" --policy-name "$IAM_POLICY_NAME" >/dev/null 2>&1; then
    log_info "Delete inline policy: $IAM_POLICY_NAME"
    run aws_iam delete-user-policy --user-name "$IAM_USER" --policy-name "$IAM_POLICY_NAME"
  fi

  log_info "Delete IAM user: $IAM_USER"
  run aws_iam delete-user --user-name "$IAM_USER"
  log_ok "IAM user destroyed"
}

destroy_bucket() {
  log_step "DESTROY: DO Spaces bucket $BUCKET"

  if ! do_s3 ls "s3://${BUCKET}/" >/dev/null 2>&1; then
    log_info "Bucket absent — nothing to delete"
    return 0
  fi

  log_warn "Bucket has objects; recursive delete via aws s3 rb --force"
  run do_s3 rb "s3://${BUCKET}" --force
  log_ok "Bucket destroyed"
}

destroy_output_file() {
  if [[ -f "$OUTPUT_FILE" ]]; then
    log_info "Remove output file: $OUTPUT_FILE"
    if [[ "$DRY_RUN" -ne 1 ]]; then
      rm -f "$OUTPUT_FILE"
    fi
    log_ok "Output file removed"
  fi
}

# ---- rotate-key --------------------------------------------------------------

rotate_access_key() {
  log_step "ROTATE access key for $IAM_USER"

  if ! aws_iam get-user --user-name "$IAM_USER" >/dev/null 2>&1; then
    log_err "IAM user $IAM_USER does not exist — run without --rotate-key first"
    exit 1
  fi

  local key_ids
  key_ids=$(aws_iam list-access-keys --user-name "$IAM_USER" \
    --query 'AccessKeyMetadata[].AccessKeyId' --output text 2>/dev/null || true)
  for kid in $key_ids; do
    log_info "Delete old access key: $kid"
    run aws_iam delete-access-key --user-name "$IAM_USER" --access-key-id "$kid"
  done

  # Resolve KMS ARN so write_output has full context.
  OUT_KMS_KEY_ID=$(aws_kms describe-key --key-id "$KMS_ALIAS" --query 'KeyMetadata.KeyId' --output text)
  OUT_KMS_KEY_ARN=$(aws_kms describe-key --key-id "$KMS_ALIAS" --query 'KeyMetadata.Arn' --output text)
  export OUT_KMS_KEY_ID OUT_KMS_KEY_ARN

  ensure_access_key
  write_output
}

# ---- main --------------------------------------------------------------------

main() {
  preflight

  case "$ACTION" in
    provision)
      ensure_kms
      ensure_iam_user
      ensure_access_key
      ensure_bucket
      write_output
      ;;
    rotate-key)
      rotate_access_key
      ;;
    destroy)
      destroy_iam
      destroy_bucket
      destroy_output_file
      log_warn "KMS key alias $KMS_ALIAS NOT deleted (operator runs \`aws kms schedule-key-deletion\` manually)."
      ;;
  esac

  log_step "Done"
  log_ok "Action: $ACTION  env=$ENV"
  if [[ "$ACTION" = "provision" || "$ACTION" = "rotate-key" ]]; then
    log_info "Output: $OUTPUT_FILE"
    log_info "Wire into Helmfile: ensure SOURCE_OPENBAO_ENV=$ENV is set when running \`helmfile sync\`."
  fi
}

main "$@"
