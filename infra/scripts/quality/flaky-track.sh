#!/usr/bin/env bash
# Flaky test tracker — log retry events to artifact JSON for aggregator cron.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.2 + EC-4.
#
# Behavior:
#   - Reads GO_TEST_LOG (output of `go test -v -count=1`)
#   - Identifies tests that FAILed on first run + PASSed on retry
#   - Appends event to FLAKY_OUT JSON (one file per CI run)
#   - Daily cron (flaky-aggregator.yml) reads all artifacts → opens
#     issue if test has ≥3 marks in 7d
#
# Per EC-4: NO direct commit of tracker JSON; use artifacts + single-writer
# cron with `concurrency: { group: flaky-aggregator }` to avoid race.
#
# Env vars:
#   GO_TEST_LOG  — initial run + retry run combined log
#   FLAKY_OUT    — JSON output path (default: /tmp/flaky-events-$(date).json)

set -euo pipefail

GO_TEST_LOG="${GO_TEST_LOG:-/tmp/go-test-output.log}"
FLAKY_OUT="${FLAKY_OUT:-/tmp/flaky-events-$(date -u +%Y%m%dT%H%M%SZ).json}"

if [ ! -f "$GO_TEST_LOG" ]; then
  echo "[flaky-track] log not found: $GO_TEST_LOG — skip" >&2
  echo '{"events": []}' > "$FLAKY_OUT"
  exit 0
fi

# Build a Python report (handles JSON output cleanly)
python3 - "$GO_TEST_LOG" "$FLAKY_OUT" <<'PY'
import json, re, sys, os, datetime

log_path, out_path = sys.argv[1:]

# Parse test results: --- PASS: TestX (0.01s)  or  --- FAIL: TestX (0.01s)
result_re = re.compile(r'^\s*--- (PASS|FAIL|SKIP):\s+(\S+)\s+\(([\d.]+)s\)')

# Track each test's outcomes; if seen as FAIL then PASS, mark flaky
seen = {}
events = []

with open(log_path) as f:
    for line in f:
        m = result_re.search(line)
        if not m:
            continue
        outcome, name, _ = m.groups()
        if name not in seen:
            seen[name] = outcome
            continue
        prev = seen[name]
        if prev == "FAIL" and outcome == "PASS":
            events.append({
                "test": name,
                "first_outcome": "FAIL",
                "retry_outcome": "PASS",
                "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
                "ci_run": os.environ.get("GITHUB_RUN_ID", "local"),
            })
        seen[name] = outcome

output = {
    "schema_version": "1.0",
    "generated_at": datetime.datetime.utcnow().isoformat() + "Z",
    "events": events,
    "summary": {
        "total_tests_seen": len(seen),
        "flaky_count": len(events),
    },
}

with open(out_path, "w") as f:
    json.dump(output, f, indent=2)

print(f"[flaky-track] tracked {len(seen)} unique tests, {len(events)} flaky events → {out_path}")
PY
