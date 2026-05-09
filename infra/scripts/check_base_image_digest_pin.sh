#!/usr/bin/env bash
#
# T4.6 of deep-review-remediation-plan (find_cicd_002 MEDIUM).
#
# Lints production Dockerfiles for base images referenced by tag only.
# Tag mutability undermines SOURCE_DATE_EPOCH reproducibility (a tag that
# resolves to digest A today may resolve to digest B tomorrow).
#
# Allowed:
#   - Digest-pinned: FROM image:tag@sha256:<64-hex>
#   - Local k3d builds: FROM k3d-loopzero-registry:5000/...
#
# Forbidden by default:
#   - Tag-only: FROM golang:1.25.9-alpine3.22
#   - Floating: FROM alpine
#
# Existing pre-T4.6 baseline is catalogued in EXCEPTION_LIST. Renovate Bot
# (configured with pinDigests: true) will replace these with digests on
# its first run; the script then accepts any new tag-pinned-only PR via
# pragma comment (governance gate).

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"

# Production Dockerfiles to lint. Test fixtures are excluded.
DOCKERFILES=(
    "api/Dockerfile"
    "cloud/Dockerfile"
    "operators/Dockerfile.app-operator"
    "operators/Dockerfile.tenant-operator"
    "operators/Dockerfile.preview-operator"
    "operators/Dockerfile.platform-operator"
    "tools/loopzero-dockerfile-lint/Dockerfile"
    "infra/images/packs-runner/Dockerfile"
    "infra/images/clone-runner/Dockerfile"
    "infra/images/syft-scanner/Dockerfile"
    "infra/images/cosign-signer/Dockerfile"
    "cli/Dockerfile"
)

# Pre-T4.6 baseline: every line at this point that does NOT have a digest.
# Renovate replaces these on first bump. Catalogued so this commit doesn't
# fail CI for pre-existing state, but new PRs adding tag-only FROMs fail.
EXCEPTIONS_BASELINE=(
    "api/Dockerfile:FROM golang:1.25.9-alpine3.22 AS builder"
    "api/Dockerfile:FROM golang:1.25.9-alpine3.22 AS migrate-builder"
    "api/Dockerfile:FROM alpine:3.22"
    "cloud/Dockerfile:FROM golang:1.25.9-alpine3.22 AS builder"
    "cloud/Dockerfile:FROM alpine:3.22"
    "operators/Dockerfile.app-operator:FROM golang:1.25.9-alpine3.22 AS builder"
    "operators/Dockerfile.app-operator:FROM gcr.io/distroless/static-debian12:nonroot"
    "operators/Dockerfile.tenant-operator:FROM golang:1.25.9-alpine3.22 AS builder"
    "operators/Dockerfile.tenant-operator:FROM gcr.io/distroless/static-debian12:nonroot"
    "operators/Dockerfile.preview-operator:FROM golang:1.25.9-alpine3.22 AS builder"
    "operators/Dockerfile.preview-operator:FROM gcr.io/distroless/static-debian12:nonroot"
    "operators/Dockerfile.platform-operator:FROM golang:1.25.9-alpine3.22 AS builder"
    "operators/Dockerfile.platform-operator:FROM gcr.io/distroless/static-debian12:nonroot"
    "tools/loopzero-dockerfile-lint/Dockerfile:FROM golang:1.25.9-alpine3.22 AS build"
    "tools/loopzero-dockerfile-lint/Dockerfile:FROM alpine:3.22"
    "infra/images/packs-runner/Dockerfile:FROM mirror.gcr.io/library/golang:1.25.9-alpine3.22 AS build"
    "infra/images/packs-runner/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.6"
    "infra/images/syft-scanner/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.3"
    "infra/images/cosign-signer/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.3"
    "infra/images/clone-runner/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.6"
)

violations=0
total=0

for df in "${DOCKERFILES[@]}"; do
    full="$REPO_ROOT/$df"
    [ -e "$full" ] || continue

    while IFS= read -r line; do
        # Strip trailing whitespace + comments
        clean="$(echo "$line" | sed 's/#.*$//; s/[[:space:]]*$//')"
        [ -z "$clean" ] && continue

        total=$((total + 1))

        # Allow digest-pinned (contains @sha256:)
        if echo "$clean" | grep -qE '@sha256:[a-f0-9]{64}'; then
            continue
        fi
        # Allow local k3d registry references
        if echo "$clean" | grep -qE 'k3d-loopzero-registry:'; then
            continue
        fi
        # Allow scratch
        if [ "$clean" = "FROM scratch" ]; then
            continue
        fi

        # Check pragma allow-tag-pin within 2 lines before
        # Find this line's number to look up
        lineno="$(grep -n "^${clean}$" "$full" 2>/dev/null | head -1 | cut -d: -f1)"
        if [ -n "$lineno" ]; then
            start=$((lineno - 2))
            if [ "$start" -lt 1 ]; then start=1; fi
            if sed -n "${start},${lineno}p" "$full" | grep -q "pragma: allow-tag-pin"; then
                continue
            fi
        fi

        # Check baseline exceptions
        key="${df}:${clean}"
        excepted=0
        for ex in "${EXCEPTIONS_BASELINE[@]}"; do
            if [ "$key" = "$ex" ]; then
                excepted=1
                break
            fi
        done

        if [ "$excepted" -eq 0 ]; then
            echo "BASE-IMAGE-DIGEST VIOLATION: $df: $clean"
            violations=$((violations + 1))
        fi
    done < <(grep "^FROM " "$full")
done

echo ""
echo "Scanned $total FROM directives across ${#DOCKERFILES[@]} Dockerfiles."
if [ "$violations" -eq 0 ]; then
    echo "PASS — every base image is digest-pinned, in the baseline exception list, or carries a pragma."
    exit 0
fi

echo "FAIL — $violations new tag-only base image(s)."
echo ""
echo "To fix: replace 'tag' with 'tag@sha256:<digest>' (Renovate auto-bumps via pinDigests: true)."
echo "Or add '# pragma: allow-tag-pin' on the line ABOVE the FROM directive (governance gate)."
exit 1
