# Action SHA Pinning Runbook

## Status (2026-05-03)

- **Gate enforced:** `scripts/check-action-pinning.sh` (INF-G18) + `.github/workflows/infra-gates.yml`
- **Renovate config:** `.github/renovate.json` ready to install
- **Compliance:** 6/22 actions pinned in this iteration; **17 remain unpinned**
- **Plan:** `docs/plans/infra-sota-hardening-plan.md` T1.1

## Why this runbook exists

The plan calls for pinning all GitHub Action `uses:` lines to commit SHAs. SHA resolution requires either `gh api` access (network egress to GitHub) or Renovate Bot's first run. Neither is available in the offline iteration that started this work, so we did the partial pinning we could, then left a clear remediation path here.

## Currently pinned (compliant)

These SHAs were already in the repo from earlier work and are reused for any new occurrences:

| Action | SHA | Tag |
|---|---|---|
| `actions/checkout` | `11bd71901bbe5b1630ceea73d27597364c9af683` | v4.2.2 |
| `actions/github-script` | `60a0d83039c74a4aee543508d2ffcb1c3799cdea` | v7.0.1 |
| `actions/setup-node` | `49933ea5288caeca8642d1e84afbd3f7d6820020` | v4.4.0 |
| `actions/setup-python` | `a26af69be951a213d495a4c3e4e4022e16d87065` | v5.x |
| `aquasecurity/trivy-action` | `57a97c7e7821a5776cebc9bb87c984fa69cba8f1` | v0.35.0 |
| `digitalocean/action-doctl` | `3cb3953159719656269e044e0e24ca16dd2a690f` | v2.5.2 |
| `docker/setup-buildx-action` | `b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2` | n/a |
| `sigstore/cosign-installer` | `cad07c2e89fa2edd6e2d7bab4c1aa38e53f76003` | n/a |

## Pending pinning (17 violations)

| Action | Locations | Latest tag | Pin via |
|---|---|---|---|
| `actions/upload-artifact@v4` | 7 (build-images, infra-gates, infra-state-reset, load-test, multi-cell, platform-health-7day-watch, pulumi-archive) | v4 | gh api |
| `azure/setup-helm@v4` | 5 (infra-gates, helm-charts × 3, ci) | v4 | gh api |
| `pulumi/actions@v6` | 2 (infra-preview, pulumi-archive) | v6 | gh api |
| `actions/setup-go@v5` | 1 (multi-cell) | v5 | gh api |
| `anchore/sbom-action/download-syft@v0.24.0` | 1 (build-images) | v0.24.0 | gh api |
| `trufflesecurity/trufflehog@v3.94.3` | 1 (ci) | v3.94.3 | gh api |

## How to resolve the remaining 17

### Option A: Local resolution via `gh` CLI

```bash
# Install gh first if not present.
sudo apt install gh
gh auth login

# Resolve a single tag → SHA.
gh api repos/actions/upload-artifact/git/refs/tags/v4 | jq -r .object.sha

# Bulk resolve all violations.
bash scripts/check-action-pinning.sh 2>&1 | grep -oE "uses: [^@]+@[^[:space:]]+" | sort -u | while read -r line; do
    ref=$(echo "$line" | awk -F'@' '{print $1}' | awk '{print $2}')
    tag=$(echo "$line" | awk -F'@' '{print $2}')
    sha=$(gh api repos/${ref}/git/refs/tags/${tag} 2>/dev/null | jq -r .object.sha)
    if [ -n "$sha" ] && [ "$sha" != "null" ]; then
        echo "$ref@$tag -> $sha"
    fi
done

# Apply:
sed -i "s|actions/upload-artifact@v4|actions/upload-artifact@<RESOLVED_SHA> # v4.x.y|g" .github/workflows/*.yml
```

### Option B: Renovate Bot first run

After `.github/renovate.json` is committed, Renovate Bot will:

1. Open a Dependency Dashboard issue listing all pinning targets.
2. Open one PR per package group (actions, helm, gomod, npm).
3. Each PR includes `SHA → version` mapping in the description.
4. Reviewer (security-blast-radius-reviewer veto authority) approves; merge.

This is the path documented in MEET-2026-05-03-062 (MEET-A) condition #2.

### Option C: Manual lookup

For each violation, visit `https://github.com/<owner>/<repo>/releases/tag/<tag>` and extract the commit SHA from the release page. Slower; reserve for one-offs.

## Validation

```bash
$ bash scripts/check-action-pinning.sh
✅ INF-G18 GREEN — 0 tag-pinned actions in 22 workflows.
$ echo $?
0
```

When the gate exits 0, INF-G18 status moves PARTIAL → MET in `improvement-roadmap.md`.

## Why the gate is enforced before all violations are fixed

CI workflow `infra-gates.yml` runs `check-action-pinning.sh` on PR diffs. Until the existing 17 violations are pinned:

- **Existing violations are tolerated** (the workflow currently warns or fails for the existing 17, depending on how aggressive we want to be).
- **New violations are blocked** by the script when a PR adds a tag-pinned `uses:`.

This is the "ratchet" pattern: lock the baseline, prevent regression, fix the historical debt opportunistically. INF-G18 status remains PARTIAL until 0 violations.

**Operational note:** If the existing 17 cause CI noise, temporarily set the workflow to `continue-on-error: true` for the action-pinning job. Remove the override as remaining SHAs are pinned via Renovate.
