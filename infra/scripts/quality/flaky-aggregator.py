#!/usr/bin/env python3
"""Aggregate flaky test events from artifacts + open GitHub issues.

Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.2 + EC-4.
Invoked by .github/workflows/flaky-aggregator.yml.

Reads /tmp/flaky-artifacts/**/*.json (downloaded artifacts) +
opens issues for tests with >= 3 marks in the past 7 days.
"""
import collections
import glob
import json
import subprocess

THRESHOLD = 3

counts = collections.Counter()
events_per_test = collections.defaultdict(list)

for f in glob.glob("/tmp/flaky-artifacts/**/*.json", recursive=True):
    try:
        with open(f) as fh:
            data = json.load(fh)
        for e in data.get("events", []):
            counts[e["test"]] += 1
            events_per_test[e["test"]].append(e)
    except Exception as ex:
        print(f"skipping {f}: {ex}")

for test, count in counts.most_common():
    if count < THRESHOLD:
        continue
    # Avoid duplicate issues (search by title prefix)
    search = subprocess.run(
        ["gh", "issue", "list", "--search", f'flaky: "{test}"',
         "--state", "open", "--json", "number"],
        capture_output=True, text=True,
    )
    if search.stdout and search.stdout.strip() != "[]":
        print(f"issue already exists for {test}")
        continue
    recent = "\n".join(
        f"- {e['timestamp']} (run {e.get('ci_run', 'unknown')})"
        for e in events_per_test[test][:10]
    )
    body = f"""## Flaky test detected: `{test}`

This test has failed and retried-pass **{count} times** in the past 7 days.

Per Quality Gate T6.2 + EC-4 protocol:
1. Investigate root cause (race, ordering, env dep, time dep, etc.)
2. Fix or mark intentionally skipped with issue link
3. If quarantined > 14 days, this test will be flagged for delete

Recent events:
{recent}
"""
    subprocess.run([
        "gh", "issue", "create",
        "--title", f"flaky: {test}",
        "--body", body,
        "--label", "flaky,quality-gates",
    ])
    print(f"opened issue for {test} ({count} marks)")

print(f"aggregator done: {len(counts)} unique flaky tests, opened issues for those with >={THRESHOLD} marks")
