#!/usr/bin/env bash
# infra/tests/scripts/dockerfile-no-docker-hub_test.sh
#
# Regression for B8 (real-paas-cleanroom-dogfood-loop-plan iter 7→8):
# Dockerfiles in infra/images/ MUST NOT reference plain `alpine:`,
# `golang:`, `node:` etc. (which resolve to docker.io). They MUST use
# `mirror.gcr.io/library/<image>:<tag>` so `docker build` on the host
# does not hit Docker Hub's anonymous rate limit.
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
IMAGES_DIR="${REPO_ROOT}/infra/images"

PASS=0
FAIL=0

# Find all FROM directives in Dockerfiles under infra/images/.
mapfile -t froms < <(grep -rh "^FROM " "$IMAGES_DIR"/*/Dockerfile 2>/dev/null)

if [[ "${#froms[@]}" -eq 0 ]]; then
  printf 'FAIL: no FROM directives found under %s\n' "$IMAGES_DIR" >&2
  exit 1
fi

for from_line in "${froms[@]}"; do
  # Skip empty
  [[ -z "$from_line" ]] && continue
  # Extract image reference (third token after FROM and possible repeat).
  # FROM <image>[:tag] [AS stage]
  ref=$(awk '{print $2}' <<<"$from_line")
  case "$ref" in
    mirror.gcr.io/*)
      printf '  PASS  %s\n' "$from_line"
      PASS=$((PASS + 1))
      ;;
    registry.digitalocean.com/*|*/*/*)
      # DOCR-prefixed or full-path private registry refs are fine.
      printf '  PASS  %s (private/full path)\n' "$from_line"
      PASS=$((PASS + 1))
      ;;
    *)
      printf '  FAIL  %s — must use mirror.gcr.io/library/* (not Docker Hub default)\n' "$from_line" >&2
      FAIL=$((FAIL + 1))
      ;;
  esac
done

echo
printf '%d/%d Dockerfile FROMs OK\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
