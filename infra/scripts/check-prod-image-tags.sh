#!/usr/bin/env bash
# T1.16 — Mutable Prod Image Tag Enforcement
# Reject PRs that introduce ":prod" OR ":latest" image tags in production
# manifest paths. Production deploys MUST use immutable :sha7 (or fuller :sha)
# tags so the audit trail can identify exactly which build is running.
#
# Lesson source: MEET-2026-05-12 plan T1.16 + ADR-0020 (D1 image tag policy).
# Without mechanical enforcement, mutable tags creep into Helm values and
# manifests, breaking the "Git is desired state for workloads (INV-1)" invariant
# because the same Git SHA could deploy different images over time.
#
# Usage:
#   ./check-prod-image-tags.sh [SCAN_PATHS...]
#   ./check-prod-image-tags.sh charts/ infra/helm/ infra/helmfile/environments/prod.yaml
#
# Exit codes:
#   0 — No violations
#   1 — Found :prod or :latest in prod-scoped manifests
#   2 — Usage error
#
# Allowed exceptions (per ADR-0020 D1):
#   - .dev or staging environment files (path contains 'dev.' or 'staging.')
#   - Comments inside YAML files (lines starting with '#')
#   - Lines tagged with "# loopzero:mutable-tag-allowed" (explicit override)

set -euo pipefail

# Default scan paths if none provided
DEFAULT_PATHS=(
  "infra/helmfile/environments/prod.yaml"
  "infra/helm/"
  "charts/"
)

if [[ $# -gt 0 ]]; then
  SCAN_PATHS=("$@")
else
  SCAN_PATHS=("${DEFAULT_PATHS[@]}")
fi

VIOLATIONS=()
SCANNED_FILES=0

for scan in "${SCAN_PATHS[@]}"; do
  if [[ ! -e "$scan" ]]; then
    continue
  fi

  # Find YAML/values files in scope
  while IFS= read -r file; do
    SCANNED_FILES=$((SCANNED_FILES + 1))

    # Skip dev/staging-scoped files (mutable tags allowed there)
    if [[ "$file" =~ dev\.ya?ml$ ]] || [[ "$file" =~ staging\.ya?ml$ ]]; then
      continue
    fi

    # Look for image: <repo>:prod or :latest patterns (excluding comments + opt-in override)
    while IFS=: read -r lineno match; do
      # Skip commented lines
      stripped=$(echo "$match" | sed -E 's/^\s*//')
      if [[ "$stripped" =~ ^# ]]; then
        continue
      fi
      # Skip explicit override
      if [[ "$match" =~ loopzero:mutable-tag-allowed ]]; then
        continue
      fi
      VIOLATIONS+=("$file:$lineno: $stripped")
    done < <(grep -nE '(image|tag):\s*["\x27]?[^"\x27#]+:(prod|latest)([^a-zA-Z0-9_]|$)' "$file" 2>/dev/null || true)
  done < <(find "$scan" -type f \( -name "*.yaml" -o -name "*.yml" \) 2>/dev/null)
done

if [[ ${#VIOLATIONS[@]} -eq 0 ]]; then
  echo "✓ Scanned $SCANNED_FILES file(s). No :prod or :latest tags found in production manifests."
  exit 0
fi

echo "✗ VIOLATION: Found ${#VIOLATIONS[@]} mutable image tag(s) in production-scoped manifests:"
printf '  %s\n' "${VIOLATIONS[@]}"
echo ""
echo "Per ADR-0020 D1 (image tag policy) + MEET-2026-05-12 plan T1.16:"
echo "Production manifests MUST use immutable :sha7 tags (or longer SHA)."
echo ""
echo "To fix:"
echo "  1. Replace :prod / :latest with a specific image digest or :sha7 tag"
echo "  2. If this is dev/staging, ensure file path contains 'dev.' or 'staging.'"
echo "  3. For genuinely intentional mutable tags, add '# loopzero:mutable-tag-allowed'"
echo "     comment on the same line + reference an ADR justifying the exception"
echo ""
echo "Reference: ADR-0020 D1 image-tag-format + audit M12 (mutable :prod tag risk)"
exit 1
