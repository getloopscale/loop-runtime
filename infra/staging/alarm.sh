#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Staging's alarms, raised as GitHub issues on getloopscale/loop-runtime with the
# staging-alarm label: GitHub notifies the repository's watchers, and the issue keeps the
# history. One open issue per alarm; raising it again comments on it, and clearing it
# comments and closes it.
#
#   alarm.sh raise <alarm> <summary> [details-file]
#   alarm.sh clear <alarm> <summary>
#   alarm.sh check-disk     raises or clears "disk" against DISK_LIMIT_PERCENT of /
#   alarm.sh check-tokens   raises or clears "token-expiry" for the host's GitHub tokens
#
# The token is the host's status token, with Issues read and write added.
set -euo pipefail

REPO="${DEPLOY_REPO:-getloopscale/loop-runtime}"
GITHUB_API="${DEPLOY_GITHUB_API:-https://api.github.com}"
TOKEN_FILE="${DEPLOY_STATUS_TOKEN_FILE:-/etc/loopzero-staging/github-status-token}"
DISK_PATH="${ALARM_DISK_PATH:-/}"
DISK_LIMIT_PERCENT="${ALARM_DISK_LIMIT_PERCENT:-80}"
# The GitHub tokens the host keeps: a file holds the token itself, or a JSON file holds it
# as "password" (ghcr-pull.json). GitHub caps their lifetime, so one of them always expires.
TOKEN_FILES="${ALARM_TOKEN_FILES:-$TOKEN_FILE /etc/loopzero-staging/ghcr-pull.json}"
TOKEN_WARN_DAYS="${ALARM_TOKEN_WARN_DAYS:-14}"
LABEL=staging-alarm
HOST="$(hostname)"

fail() { printf 'staging-alarm: error: %s\n' "$*" >&2; exit 1; }
[[ -r "$TOKEN_FILE" ]] || fail "no token at $TOKEN_FILE"
TOKEN="$(<"$TOKEN_FILE")"

github() {
  curl -fsS --max-time 30 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" "$@"
}

title_of() { printf 'staging alarm: %s' "$1"; }

# open_issue <alarm>: the number of the open issue of the alarm, or nothing.
open_issue() {
  github "$GITHUB_API/repos/$REPO/issues?state=open&labels=$LABEL&per_page=100" \
    | jq -r --arg title "$(title_of "$1")" '[.[] | select(.title == $title)][0].number // empty'
}

comment() {
  github -X POST "$GITHUB_API/repos/$REPO/issues/$1/comments" -d "$(jq -n --arg body "$2" '{body: $body}')" >/dev/null
}

raise() {
  local alarm=$1 summary=$2 details="" body number
  if [[ -n "${3:-}" && -f "$3" ]]; then
    details=$(tail -n 60 "$3")
  fi
  body=$(printf '%s\n\nHost: `%s`, %s\n' "$summary" "$HOST" "$(date -u +%FT%TZ)")
  if [[ -n "$details" ]]; then
    body=$(printf '%s\n\n<details><summary>Last lines of %s</summary>\n\n```\n%s\n```\n</details>\n' "$body" "$3" "$details")
  fi
  number=$(open_issue "$alarm")
  if [[ -n "$number" ]]; then
    comment "$number" "$body"
    echo "alarm $alarm: commented on #$number"
  else
    number=$(github -X POST "$GITHUB_API/repos/$REPO/issues" \
      -d "$(jq -n --arg title "$(title_of "$alarm")" --arg body "$body" --arg label "$LABEL" '{title: $title, body: $body, labels: [$label]}')" \
      | jq -r '.number')
    echo "alarm $alarm: opened #$number"
  fi
}

clear_alarm() {
  local alarm=$1 summary=$2 number
  number=$(open_issue "$alarm")
  [[ -n "$number" ]] || return 0
  comment "$number" "$(printf 'Cleared: %s\n\nHost: `%s`, %s\n' "$summary" "$HOST" "$(date -u +%FT%TZ)")"
  github -X PATCH "$GITHUB_API/repos/$REPO/issues/$number" -d '{"state": "closed", "state_reason": "completed"}' >/dev/null
  echo "alarm $alarm: cleared #$number"
}

check_disk() {
  local used
  used=$(df --output=pcent "$DISK_PATH" | tail -n 1 | tr -dc '0-9')
  [[ -n "$used" ]] || fail "could not read the disk use of $DISK_PATH"
  if (( used > DISK_LIMIT_PERCENT )); then
    raise disk "$DISK_PATH is ${used}% full, above the ${DISK_LIMIT_PERCENT}% limit."
  else
    clear_alarm disk "$DISK_PATH is back to ${used}%."
  fi
}

# token_problem <file>: why the token in <file> needs renewing, or nothing. GitHub names a
# token's expiry in the github-authentication-token-expiration header of any API answer.
token_problem() {
  local file=$1 token headers expiry left_days
  if [[ "$file" == *.json ]]; then
    token=$(jq -r '.password // empty' "$file")
  else
    token=$(<"$file")
  fi
  [[ -n "$token" ]] || { printf '`%s` holds no token.' "$file"; return; }
  if ! headers=$(curl -fsS --max-time 30 -o /dev/null -D - -H "Authorization: Bearer $token" \
      -H "X-GitHub-Api-Version: 2022-11-28" "$GITHUB_API/rate_limit"); then
    printf 'GitHub rejected the token in `%s`.' "$file"
    return
  fi
  expiry=$(printf '%s' "$headers" | tr -d '\r' | sed -n 's/^github-authentication-token-expiration: *//Ip' | head -n 1)
  [[ -n "$expiry" ]] || return 0
  left_days=$(( ($(date -u -d "$expiry" +%s) - $(date -u +%s)) / 86400 ))
  if (( left_days <= TOKEN_WARN_DAYS )); then
    printf 'The token in `%s` expires on %s, in %d days.' "$file" "$expiry" "$left_days"
  fi
}

check_tokens() {
  local file problem problems=""
  for file in $TOKEN_FILES; do
    if [[ ! -r "$file" ]]; then
      problems+="- \`$file\` is missing."$'\n'
      continue
    fi
    problem=$(token_problem "$file")
    [[ -z "$problem" ]] || problems+="- $problem"$'\n'
  done
  if [[ -n "$problems" ]]; then
    raise token-expiry "$(printf 'Renew these GitHub tokens (where each is used and its scopes: taskfiles/staging.yml):\n\n%s' "$problems")"
  else
    clear_alarm token-expiry "every GitHub token is valid for more than ${TOKEN_WARN_DAYS} days."
  fi
}

case "${1:-}" in
  raise) [[ $# -ge 3 ]] || fail "usage: alarm.sh raise <alarm> <summary> [details-file]"; raise "$2" "$3" "${4:-}" ;;
  clear) [[ $# -ge 3 ]] || fail "usage: alarm.sh clear <alarm> <summary>"; clear_alarm "$2" "$3" ;;
  check-disk) check_disk ;;
  check-tokens) check_tokens ;;
  *) fail "usage: alarm.sh raise|clear|check-disk|check-tokens ..." ;;
esac
