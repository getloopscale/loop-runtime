#!/usr/bin/env bash
# AM6 — hermetic wiring assertion on the artifact pipeline (no toolchain; grep only).
# The real build/sign/SBOM/GHCR proof is run separately (build-publish.sh + cosign verify).
set -euo pipefail
cd "$(dirname "$0")/../.."   # loopzero repo root
fail(){ echo "FAIL: $1"; exit 1; }

W=.github/workflows/build-publish.yml
R=.github/workflows/release-artifacts.yml
[ -f "$W" ] || fail "missing $W"
[ -f "$R" ] || fail "missing $R"
[ -f hack/build-publish.sh ] || fail "missing hack/build-publish.sh"

# reusable workflow: keyless cosign + SBOM + GHCR (D2/D3)
grep -q 'packages: write' "$W" || fail "build-publish must request packages: write"
grep -q 'id-token: write'  "$W" || fail "build-publish must request id-token: write (keyless cosign)"
grep -q 'cosign sign'      "$W" || fail "build-publish must cosign sign"
grep -q 'sbom: true'       "$W" || fail "build-publish must build with SBOM (D3)"
grep -q 'provenance: true' "$W" || fail "build-publish must attach provenance"

# matrix lists the 5 loopzero images + the charts step
for img in loopzero-api loopzero-app-operator loopzero-platform-operator loopzero-tenant-operator loopzero-preview-operator; do
  grep -q "$img" "$R" || fail "release-artifacts matrix missing $img"
done
grep -q 'helm push' "$R" || fail "release-artifacts must helm-push the charts (OCI)"

# EC-2: no literal token anywhere in workflows/scripts
if grep -rIlE 'ghp_[A-Za-z0-9]{20,}' .github hack 2>/dev/null; then
  fail "EC-2: a literal GitHub token (ghp_...) is present in workflows/hack — must use a secret/env"
fi

# build-publish.sh: buildx SBOM + cosign + no literal token
grep -q -- '--sbom' hack/build-publish.sh || fail "build-publish.sh must build with --sbom"
grep -q 'cosign sign' hack/build-publish.sh || fail "build-publish.sh must cosign sign"

echo "PASS: AM6 pipeline wiring (loopzero) — keyless cosign + SBOM + matrix(5)+charts + no literal token"
