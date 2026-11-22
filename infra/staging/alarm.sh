#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Staging's alarms, raised as GitHub issues on getloopscale/loopzero with the
# staging-alarm label: GitHub notifies the repository's watchers, and the issue keeps the
# history. One open issue per alarm; raising it again comments on it, and clearing it
# comments and closes it.
#
#   alarm.sh raise <alarm> <summary> [details-file]
#   alarm.sh clear <alarm> <summary>
#   alarm.sh check-disk     raises or clears "disk" against DISK_LIMIT_PERCENT of /
#
# The token is the host's status token, with Issues read and write added.
set -euo pipefail

REPO="${DEPLOY_REPO:-getloopscale/loopzero}"
GITHUB_API="${DEPLOY_GITHUB_API:-https://api.github.com}"
TOKEN_FILE="${DEPLOY_STATUS_TOKEN_FILE:-/etc/loopzero-staging/github-status-token}"
DISK_PATH="${ALARM_DISK_PATH:-/}"
DISK_LIMIT_PERCENT="${ALARM_DISK_LIMIT_PERCENT:-80}"
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

case "${1:-}" in
  raise) [[ $# -ge 3 ]] || fail "usage: alarm.sh raise <alarm> <summary> [details-file]"; raise "$2" "$3" "${4:-}" ;;
  clear) [[ $# -ge 3 ]] || fail "usage: alarm.sh clear <alarm> <summary>"; clear_alarm "$2" "$3" ;;
  check-disk) check_disk ;;
  *) fail "usage: alarm.sh raise|clear|check-disk ..." ;;
esac
