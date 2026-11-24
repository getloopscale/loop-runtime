#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Deploys the head of develop to staging, pulled from the staging host by
# loopzero-staging-deploy.timer. Nothing on GitHub reaches the host: no runner, no
# credential of the host in the repository. The host reports back through commit
# statuses, with a token that lives only on the host.
#
# One run:
#   1. Reads the head of develop. Nothing to do when that commit was already handled.
#   2. Waits for the `artifacts` workflow of the commit, which publishes
#      :develop-<sha7> for every engine image the commit changed. A failed build is
#      reported as staging/deploy failure, once, and the commit is not deployed until
#      the run is re-run: the failed attempt is remembered, not the commit.
#   3. Pins each engine image to the newest develop build: the image the commit
#      published, or else the tag the previous deploy ran. The first deploy starts
#      from the tags platformImages.tags pins in the staging environment. The console
#      images (loopzero-cloud, loopzero-dashboard) come from getloopscale/console and
#      keep the pins in git.
#   4. Runs `task staging:platform` at that commit with those pins, and reports
#      staging/deploy success or failure on the commit.
#   5. After a successful deploy, validates the commit with infra/staging/validate.sh of
#      its own checkout, which reports staging/validate. A failed validation fails the
#      run; the commit stays handled, so it is not validated again.
#
# Everything outside the host is reached through variables, so a test can stand in for
# GitHub, the registry and the platform task.
set -euo pipefail

REPO="${DEPLOY_REPO:-getloopscale/loopzero}"
REMOTE="${DEPLOY_REMOTE:-https://github.com/${REPO}.git}"
BRANCH="${DEPLOY_BRANCH:-develop}"
GITHUB_API="${DEPLOY_GITHUB_API:-https://api.github.com}"
REGISTRY="${DEPLOY_REGISTRY:-https://ghcr.io}"
IMAGE_NAMESPACE="${DEPLOY_IMAGE_NAMESPACE:-getloopscale}"
STATE_DIR="${DEPLOY_STATE_DIR:-/var/lib/loopzero-staging/deploy}"
TOKEN_FILE="${DEPLOY_STATUS_TOKEN_FILE:-/etc/loopzero-staging/github-status-token}"
PLATFORM_CMD="${DEPLOY_PLATFORM_CMD:-task staging:platform}"
# How long a commit with no artifacts run yet is waited for before it is deployed with
# the images it already has (a commit whose paths no build watches).
RUN_GRACE_SECONDS="${DEPLOY_RUN_GRACE_SECONDS:-900}"
# The images getloopscale/console publishes; their pins stay the ones in git.
CONSOLE_IMAGES="loopzero-cloud loopzero-dashboard"
STATUS_URL_BASE="${DEPLOY_STATUS_URL_BASE:-https://staging.getloopscale.com}"
# The validation of a deployed commit, given its SHA. Empty: the checkout's validate.sh.
VALIDATE_CMD="${DEPLOY_VALIDATE_CMD:-}"

CHECKOUT="$STATE_DIR/checkout"
LOGS="$STATE_DIR/logs"
mkdir -p "$STATE_DIR" "$LOGS"

log() { printf '%s staging-deploy: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
fail() { log "error: $*"; exit 1; }

[[ -r "$TOKEN_FILE" ]] || fail "no status token at $TOKEN_FILE: create a fine-grained token with Commit statuses read and write on $REPO"
TOKEN="$(<"$TOKEN_FILE")"

github() {
  curl -fsS --max-time 30 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" "$@"
}

# status <sha> <state> <description>: the staging/deploy commit status.
status() {
  local body
  body=$(jq -n --arg state "$2" --arg description "$3" --arg context "staging/deploy" \
    --arg target "$STATUS_URL_BASE" '{state: $state, description: $description, context: $context, target_url: $target}')
  github -X POST "$GITHUB_API/repos/$REPO/statuses/$1" -d "$body" >/dev/null \
    || fail "could not post staging/deploy $2 on $1"
}

sha=$(git ls-remote "$REMOTE" "refs/heads/$BRANCH" | cut -f1)
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || fail "no head for $BRANCH on $REMOTE (got '$sha')"
short=${sha:0:7}
if [[ -f "$STATE_DIR/handled" && "$(<"$STATE_DIR/handled")" == "$sha" ]]; then
  log "$short already handled"
  exit 0
fi

# The artifacts run of the commit: wait while it runs, refuse the commit when it failed.
runs=$(github "$GITHUB_API/repos/$REPO/actions/runs?head_sha=$sha&event=push&per_page=50") \
  || fail "could not list the workflow runs of $short"
run=$(jq -c '[.workflow_runs[] | select(.name == "artifacts")] | sort_by(.created_at) | last // empty' <<<"$runs")
if [[ -z "$run" ]]; then
  committed=$(github "$GITHUB_API/repos/$REPO/commits/$sha" | jq -r '.commit.committer.date') \
    || fail "could not read commit $short"
  age=$(( $(date -u +%s) - $(date -u -d "$committed" +%s) ))
  if (( age < RUN_GRACE_SECONDS )); then
    log "$short has no artifacts run yet (${age}s old); waiting"
    exit 0
  fi
  log "$short has no artifacts run after ${age}s: deploying the images it already has"
else
  case "$(jq -r '.status' <<<"$run")/$(jq -r '.conclusion' <<<"$run")" in
    completed/success) ;;
    completed/*)
      attempt="$sha $(jq -r '"\(.id)/\(.run_attempt // 1)"' <<<"$run")"
      if [[ -f "$STATE_DIR/failed-build" && "$(<"$STATE_DIR/failed-build")" == "$attempt" ]]; then
        log "$short: the artifacts run still failed; re-run it to deploy"
        exit 0
      fi
      status "$sha" failure "artifacts $(jq -r '.conclusion' <<<"$run"): not deployed"
      echo "$attempt" >"$STATE_DIR/failed-build"
      log "$short: the artifacts run did not succeed; not deployed"
      exit 0 ;;
    *)
      log "$short: artifacts run $(jq -r '.id' <<<"$run") is $(jq -r '.status' <<<"$run"); waiting"
      exit 0 ;;
  esac
fi

status "$sha" pending "deploying $short"

if [[ ! -d "$CHECKOUT/.git" ]]; then
  git clone --quiet "$REMOTE" "$CHECKOUT"
fi
git -C "$CHECKOUT" fetch --quiet origin "$BRANCH"
git -C "$CHECKOUT" switch --quiet --detach "$sha"

# The pins: the previous deploy's, else the ones in git, then what this commit published.
pins="$STATE_DIR/images.json"
yq -o json '.platformImages.tags' "$CHECKOUT/infra/helmfile/environments/staging.yaml" >"$STATE_DIR/git-pins.json"
if [[ ! -f "$pins" ]]; then
  cp "$STATE_DIR/git-pins.json" "$pins"
fi
# published <image> <tag>: whether the registry has the tag (anonymous pull of a public image).
published() {
  local token
  token=$(curl -fsS --max-time 30 "$REGISTRY/token?scope=repository:$IMAGE_NAMESPACE/$1:pull&service=ghcr.io" | jq -r '.token') || return 1
  curl -fsS --max-time 30 -o /dev/null -I -H "Authorization: Bearer $token" \
    -H "Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json" \
    "$REGISTRY/v2/$IMAGE_NAMESPACE/$1/manifests/$2"
}
next=$(cat "$pins")
for image in $(jq -r 'keys[]' "$STATE_DIR/git-pins.json"); do
  if [[ " $CONSOLE_IMAGES " == *" $image "* ]]; then
    next=$(jq --arg image "$image" --slurpfile git "$STATE_DIR/git-pins.json" '.[$image] = $git[0][$image]' <<<"$next")
  elif published "$image" "develop-$short"; then
    log "$image: develop-$short"
    next=$(jq --arg image "$image" --arg tag "develop-$short" '.[$image] = $tag' <<<"$next")
  elif [[ "$(jq -r --arg image "$image" '.[$image] // ""' <<<"$next")" == "" ]]; then
    next=$(jq --arg image "$image" --slurpfile git "$STATE_DIR/git-pins.json" '.[$image] = $git[0][$image]' <<<"$next")
  fi
done
jq '{platformImages: {tags: .}}' <<<"$next" >"$STATE_DIR/state-values.json"

deploy_log="$LOGS/$sha.log"
log "deploying $short (log: $deploy_log)"
if (cd "$CHECKOUT" && STAGING_STATE_VALUES="$STATE_DIR/state-values.json" $PLATFORM_CMD) >"$deploy_log" 2>&1; then
  echo "$next" >"$pins"
  echo "$sha" >"$STATE_DIR/handled"
  status "$sha" success "deployed $short"
  log "$short deployed"
  if [[ -z "$VALIDATE_CMD" ]]; then
    if [[ ! -f "$CHECKOUT/infra/staging/validate.sh" ]]; then
      log "$short has no infra/staging/validate.sh; not validated"
      exit 0
    fi
    VALIDATE_CMD="bash $CHECKOUT/infra/staging/validate.sh"
  fi
  $VALIDATE_CMD "$sha" || fail "$short: the validation exited $?; see $LOGS/$sha.validate.log"
else
  code=$?
  echo "$sha" >"$STATE_DIR/handled"
  status "$sha" failure "task staging:platform exited $code"
  fail "$short: task staging:platform exited $code; see $deploy_log"
fi
