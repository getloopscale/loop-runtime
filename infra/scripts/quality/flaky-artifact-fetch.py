#!/usr/bin/env python3
"""Fetch flaky-events artifacts from past 7 days.

Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.2 + EC-4.
Invoked by .github/workflows/flaky-aggregator.yml.

Reads `gh api .../actions/artifacts` JSON listing + downloads each
artifact named "flaky-events*" created in past 7 days into OUT_DIR.
"""
import datetime
import json
import os
import subprocess
import sys

if len(sys.argv) != 3:
    print("usage: flaky-artifact-fetch.py <artifacts-json> <out-dir>", file=sys.stderr)
    sys.exit(2)

artifacts_json, out_dir = sys.argv[1:]
os.makedirs(out_dir, exist_ok=True)

with open(artifacts_json) as f:
    data = json.load(f)

cutoff = datetime.datetime.utcnow() - datetime.timedelta(days=7)
repo = os.environ.get("GITHUB_REPOSITORY", "")
if not repo:
    print("GITHUB_REPOSITORY env var required", file=sys.stderr)
    sys.exit(2)

fetched = 0
for art in data.get("artifacts", []):
    name = art.get("name", "")
    if not name.startswith("flaky-events"):
        continue
    created = datetime.datetime.fromisoformat(
        art["created_at"].replace("Z", "+00:00")
    ).replace(tzinfo=None)
    if created < cutoff:
        continue

    aid = art["id"]
    zip_path = os.path.join(out_dir, f"{aid}.zip")
    extract_path = os.path.join(out_dir, str(aid))

    # Download via gh api
    with open(zip_path, "wb") as zf:
        subprocess.run(
            ["gh", "api", f"/repos/{repo}/actions/artifacts/{aid}/zip"],
            stdout=zf, check=False,
        )
    # Unzip
    subprocess.run(
        ["unzip", "-q", "-o", zip_path, "-d", extract_path],
        check=False,
    )
    fetched += 1

print(f"fetched {fetched} flaky-events artifacts (past 7d)")
