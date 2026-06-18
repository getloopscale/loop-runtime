#!/usr/bin/env bash
# AM6 — local mirror of the build-publish pipeline (dev + hermetic proof).
# Builds an image from a Dockerfile via buildx with a native SBOM + provenance attestation,
# pushes it by digest to GHCR, and cosign-signs it by digest with a local key.
# The CI reusable workflow (.github/workflows/build-publish.yml) does the same with KEYLESS cosign.
#
# Credential: GHCR auth comes from `docker login ghcr.io` (env/credential store) — NEVER a literal
# token in this file. cosign signing key comes from --sign-key (a local key-pair), NEVER committed.
set -euo pipefail

REGISTRY="${REGISTRY:-ghcr.io/getloopscale}"
IMAGE="" DOCKERFILE="" CONTEXT="." TAG="${TAG:-latest}" SIGN_KEY=""
while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE="$2"; shift 2 ;;
    --dockerfile) DOCKERFILE="$2"; shift 2 ;;
    --context) CONTEXT="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    --sign-key) SIGN_KEY="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done
[ -n "$IMAGE" ] && [ -n "$DOCKERFILE" ] || { echo "usage: build-publish.sh --image NAME --dockerfile PATH [--context DIR] [--tag T] [--sign-key KEY]" >&2; exit 2; }

REF="${REGISTRY}/${IMAGE}:${TAG}"
META="$(mktemp)"

echo ">> buildx build (SBOM + provenance) + push: ${REF}"
docker buildx build \
  --sbom=true --provenance=true \
  --push \
  -t "${REF}" \
  -f "${DOCKERFILE}" \
  --metadata-file "${META}" \
  "${CONTEXT}"

DIGEST="$(jq -r '."containerimage.digest"' "${META}")"
[ -n "${DIGEST}" ] && [ "${DIGEST}" != "null" ] || { echo "no digest in build metadata" >&2; exit 1; }
DIGEST_REF="${REGISTRY}/${IMAGE}@${DIGEST}"
echo ">> pushed by digest: ${DIGEST_REF}"

if [ -n "${SIGN_KEY}" ]; then
  echo ">> cosign sign (key) ${DIGEST_REF}"
  COSIGN_PASSWORD="${COSIGN_PASSWORD:-}" cosign sign --key "${SIGN_KEY}" --yes "${DIGEST_REF}"
else
  echo ">> cosign sign (keyless / OIDC) ${DIGEST_REF}"
  cosign sign --yes "${DIGEST_REF}"
fi

echo "${DIGEST_REF}"
rm -f "${META}"
