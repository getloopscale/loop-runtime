#!/usr/bin/env bash
# AM6 — hermetic wiring assertion on the dual-channel artifact pipeline (no toolchain; grep only).
# The real build/sign/SBOM/GHCR proof is run separately (build-publish.sh + cosign verify).
set -euo pipefail
cd "$(dirname "$0")/../.."   # loopzero repo root
fail(){ echo "FAIL: $1"; exit 1; }

W=.github/workflows/build-publish.yml
R=.github/workflows/release-artifacts.yml
[ -f "$W" ] || fail "missing $W"
[ -f "$R" ] || fail "missing $R"
[ -f hack/build-publish.sh ] || fail "missing hack/build-publish.sh"

# --- reusable build-publish.yml: keyless cosign + SBOM (sign-gated) + cache + community tags ---
grep -q 'packages: write'        "$W" || fail "build-publish must request packages: write"
grep -q 'id-token: write'        "$W" || fail "build-publish must request id-token: write (keyless cosign)"
grep -q 'cosign sign'            "$W" || fail "build-publish must cosign sign (release channel)"
grep -q 'sbom: ${{ inputs.sign' "$W" || fail "build-publish SBOM must be sign-gated (sbom: inputs.sign)"
grep -q 'provenance: ${{ inputs.sign' "$W" || fail "build-publish provenance must be sign-gated"
grep -q 'docker/metadata-action' "$W" || fail "build-publish must compute tags via metadata-action (community channels)"
grep -q 'cache-from: type=gha'   "$W" || fail "build-publish must use GHA layer cache (low-cost)"
grep -q 'if: ${{ inputs.sign'    "$W" || fail "cosign step must be guarded by inputs.sign (release-only signing)"

# --- caller release-artifacts.yml: dual-trigger + change-detection + matrix + charts ---
grep -q 'branches: \[develop\]'  "$R" || fail "caller must trigger on develop push (DEV channel)"
grep -qE 'tags: \["?v\*' "$R" || fail "caller must trigger on v* tags (RELEASE channel)"
grep -q 'dorny/paths-filter'     "$R" || fail "caller must use paths-filter change-detection (low-cost)"
grep -q 'cancel-in-progress'     "$R" || fail "caller must cancel superseded DEV runs (low-cost)"
grep -q 'sign: ${{ needs.plan.outputs.sign' "$R" || fail "caller must pass sign (true only on release tag)"
for img in loopzero-api loopzero-app-operator loopzero-platform-operator loopzero-tenant-operator loopzero-preview-operator; do
  grep -q "$img" "$R" || fail "release-artifacts matrix missing $img"
done
grep -q 'helm push' "$R" || fail "release-artifacts must helm-push the charts (OCI)"
# sign must be gated to a real release tag, never blanket-true
grep -q 'sign=$IS_TAG' "$R" || fail "caller must set sign ONLY from a real release tag"

# --- EC-2: no literal token anywhere in workflows/scripts ---
if grep -rIlE 'ghp_[A-Za-z0-9]{20,}' .github hack 2>/dev/null; then
  fail "EC-2: a literal GitHub token (ghp_...) is present in workflows/hack — must use a secret/env"
fi

# --- build-publish.sh local mirror: buildx SBOM + cosign + no literal token ---
grep -q -- '--sbom' hack/build-publish.sh || fail "build-publish.sh must build with --sbom"
grep -q 'cosign sign' hack/build-publish.sh || fail "build-publish.sh must cosign sign"

echo "PASS: AM6 dual-channel pipeline wiring (loopzero) — develop(dev,no-sign)+tag(release,signed) · change-detection · GHA cache · matrix(5)+charts · no literal token"
