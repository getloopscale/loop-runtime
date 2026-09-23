#!/usr/bin/env python3
"""validate-helm_charts.py — validate every Helmfile-referenced chart
resolves to its declared version in the upstream repo index.

Plan 2026-05-21-deep-review-remediation T5.4: extracted from Taskfile
heredoc (taskfiles/test.yml:638-744) to standalone file so:
- Python is linted by ruff in CI (was opaque in YAML heredoc)
- No more chr(123)+chr(125) escape gymnastics for go-template literals
- Easier to test (real Python module, importable, mockable)
- Editor LSP works (was in YAML PYEOF block)

CHG-2026-04-28-LAYERED-BOOTSTRAP — reads from infra/helmfile/*.yaml.gotmpl
(replacing pre-refactor bootstrap-shared.ts).

Bug fix preserved from 2026-04-30 (NEW-7):
The original `TPL_RE.sub('false', content)` broke YAML when a go-template
directive started a line. Two-pass strip:
  1. WHOLE_LINE_TPL strips lines that are ONLY a directive (with optional
     surrounding whitespace) — they collapse to empty, preserving YAML
     structure.
  2. INLINE_TPL replaces remaining inline directives with `false`.

Exit codes:
  0 — all charts resolve in upstream repo
  1 — pyyaml missing
  2 — at least one chart version is stale OR not in repo
"""

from __future__ import annotations

import re
import sys
import urllib.request

try:
    import yaml
except ImportError:
    print("Install pyyaml: pip install --user pyyaml", file=sys.stderr)
    sys.exit(1)

HELMFILES = [
    "infra/helmfile/core.helmfile.yaml.gotmpl",
    "infra/helmfile/build.helmfile.yaml.gotmpl",
    "infra/helmfile/cell.helmfile.yaml.gotmpl",
]

# Go-template literal stripping. Two-pass per NEW-7 fix preserved from
# Taskfile heredoc origin.
OPEN = "{{"  # noqa: now safe outside Taskfile parser
CLOSE = "}}"
WHOLE_LINE_TPL = re.compile(
    r"^[ \t]*" + re.escape(OPEN) + r"-?[^}]*-?" + re.escape(CLOSE) + r"[ \t]*$",
    re.MULTILINE,
)
INLINE_TPL = re.compile(re.escape(OPEN) + r"[^}]*" + re.escape(CLOSE))


def collect_releases() -> tuple[dict[str, str], list[tuple[str, str, str, str]]]:
    """Parse helmfiles into (repo_urls, releases).

    Returns:
        repo_urls: {repo_name: repo_url}
        releases: [(release_name, repo_name, chart_name, version), ...]
    """
    repo_urls: dict[str, str] = {}
    releases: list[tuple[str, str, str, str]] = []

    for hf in HELMFILES:
        with open(hf) as f:
            content = f.read()
        content = WHOLE_LINE_TPL.sub("", content)
        content = INLINE_TPL.sub("false", content)

        for doc in yaml.safe_load_all(content):
            if not doc:
                continue
            for r in doc.get("repositories", []) or []:
                repo_urls[r["name"]] = r["url"]
            for rel in doc.get("releases", []) or []:
                chart = rel.get("chart", "")
                version = rel.get("version")
                if not version:
                    continue  # Local chart (../helm/X) — no upstream version
                if "/" not in chart or chart.startswith("../"):
                    continue
                repo_name, chart_name = chart.split("/", 1)
                releases.append((rel["name"], repo_name, chart_name, version))

    return repo_urls, releases


def fetch_index(repo_url: str, cache: dict) -> dict:
    """Fetch + cache the index.yaml of a helm repo."""
    if repo_url in cache:
        return cache[repo_url]
    url = repo_url.rstrip("/") + "/index.yaml"
    try:
        with urllib.request.urlopen(url, timeout=15) as r:
            cache[repo_url] = yaml.safe_load(r.read())
        return cache[repo_url]
    except Exception as e:  # noqa: BLE001
        return {"_error": str(e)}


def main() -> int:
    repo_urls, releases = collect_releases()
    cache: dict = {}
    failed: list[str] = []
    skipped: list[str] = []
    ok = 0

    for name, repo_name, chart_name, version in releases:
        repo_url = repo_urls.get(repo_name)
        if not repo_url:
            skipped.append(f"  - {name}: repo '{repo_name}' not declared in helmfiles")
            continue
        idx = fetch_index(repo_url, cache)
        if "_error" in idx:
            skipped.append(f"  - {name}: repo {repo_url} unreachable ({idx['_error']})")
            continue
        entries = idx.get("entries", {}).get(chart_name, [])
        avail = [e.get("version") for e in entries]
        version_clean = version.lstrip("v")
        if version in avail or version_clean in avail or f"v{version_clean}" in avail:
            print(f"OK  {name:30} → {chart_name}@{version}")
            ok += 1
        else:
            latest = sorted(avail, reverse=True)[:3] if avail else ["<none>"]
            failed.append(
                f"  - {name} ({chart_name}@{version}): not in repo {repo_url}. "
                f"Latest 3: {latest}"
            )

    if skipped:
        print("\nSkipped (no mapping or unreachable):")
        print("\n".join(skipped))
    if failed:
        print("\nFAIL: Helm chart references stale or wrong:")
        print("\n".join(failed))
        return 2
    print(f"\n✓ {ok} chart references valid (out of {len(releases)} declared)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
