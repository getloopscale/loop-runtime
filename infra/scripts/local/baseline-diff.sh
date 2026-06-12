#!/usr/bin/env bash
# baseline-diff.sh
#
# T1.1 of plan 2026-06-08-pre-release-test-rigor v1.2 — compares two snapshots
# produced by `baseline-capture.sh` and classifies the deltas with severity:
#
#   - additive   — key/item present in B but not in A. Treated as OK
#                  (post-rebuild rebuilds may add ephemeral state; not a regression).
#   - missing    — key/item present in A but not in B. Treated as FAIL
#                  (post-rebuild lost something we had pre-rebuild).
#   - mismatch   — same key, different value. Treated as FAIL
#                  (genuine drift).
#   - identical  — no change. Treated as OK (silent).
#
# Volatile field normalization (EC-7) is also applied here as a defensive
# second pass, in case A or B was captured by an older script version that
# didn't normalize.
#
# Output: JSON delta report to stdout + human markdown to stderr (when
# requested via --markdown). Exit code is the verdict:
#   0  identical OR only additive → verdict OK
#   1  at least one mismatch OR missing → verdict FAIL
#   2  invocation error (file not found, malformed JSON)
#
# Usage:
#   bash infra/scripts/local/baseline-diff.sh snapshot-a.json snapshot-b.json
#   bash infra/scripts/local/baseline-diff.sh snapshot-a.json snapshot-b.json --markdown
set -euo pipefail

[ $# -ge 2 ] || {
  echo "[baseline-diff] usage: $0 SNAPSHOT_A SNAPSHOT_B [--markdown]" >&2
  exit 2
}

SNAP_A="$1"
SNAP_B="$2"
MARKDOWN=0
shift 2
while [ $# -gt 0 ]; do
  case "$1" in
    --markdown) MARKDOWN=1 ; shift ;;
    *) echo "[baseline-diff] unknown arg: $1" >&2 ; exit 2 ;;
  esac
done

[ -f "$SNAP_A" ] || { echo "[baseline-diff] not found: $SNAP_A" >&2 ; exit 2 ; }
[ -f "$SNAP_B" ] || { echo "[baseline-diff] not found: $SNAP_B" >&2 ; exit 2 ; }

python3 - "$SNAP_A" "$SNAP_B" "$MARKDOWN" <<'PY'
import json, sys

VOLATILE_KEYS = {
    "resourceVersion", "generation", "managedFields",
    "creationTimestamp", "uid", "lastTransitionTime",
}

def normalize(obj):
    """Recursively strip volatile fields. EC-7 defensive second pass."""
    if isinstance(obj, dict):
        return { k: normalize(v) for k, v in obj.items() if k not in VOLATILE_KEYS }
    if isinstance(obj, list):
        return [ normalize(item) for item in obj ]
    return obj

def array_key(item):
    """Pick a stable identity for an array element so we can pair A and B."""
    if isinstance(item, dict):
        for k in ("name", "id", "kratos_identity_id", "key"):
            if k in item and isinstance(item[k], str):
                return item[k]
    return json.dumps(item, sort_keys=True)

def walk(a, b, path, additive, missing, mismatch):
    """Recursive structural diff. path is dot-joined dotted-path."""
    if isinstance(a, dict) and isinstance(b, dict):
        keys = set(a) | set(b)
        for k in sorted(keys):
            child = f"{path}.{k}" if path else k
            if k in a and k not in b:
                missing.append({"path": child, "value": a[k]})
            elif k in b and k not in a:
                additive.append({"path": child, "value": b[k]})
            else:
                walk(a[k], b[k], child, additive, missing, mismatch)
        return

    if isinstance(a, list) and isinstance(b, list):
        a_by = { array_key(it): it for it in a }
        b_by = { array_key(it): it for it in b }
        # Identity-pair elements; treat unpaired as additive/missing.
        for ak in sorted(set(a_by) | set(b_by)):
            child = f"{path}[{ak}]"
            if ak in a_by and ak not in b_by:
                missing.append({"path": child, "value": a_by[ak]})
            elif ak in b_by and ak not in a_by:
                additive.append({"path": child, "value": b_by[ak]})
            else:
                walk(a_by[ak], b_by[ak], child, additive, missing, mismatch)
        return

    # Leaf comparison
    if a != b:
        # Skip if either side is the synthetic 'captured_at' field
        if path.endswith("captured_at"):
            return
        mismatch.append({"path": path, "a": a, "b": b})

with open(sys.argv[1]) as f: a_raw = json.load(f)
with open(sys.argv[2]) as f: b_raw = json.load(f)
a = normalize(a_raw)
b = normalize(b_raw)

additive, missing, mismatch = [], [], []
walk(a, b, "", additive, missing, mismatch)

verdict = "OK" if (not missing and not mismatch) else "FAIL"

report = {
    "snapshot_a": sys.argv[1],
    "snapshot_b": sys.argv[2],
    "verdict":    verdict,
    "classification": {
        "additive": additive,
        "missing":  missing,
        "mismatch": mismatch,
    },
    "counts": {
        "additive": len(additive),
        "missing":  len(missing),
        "mismatch": len(mismatch),
    },
}
print(json.dumps(report, sort_keys=True, indent=2))

if sys.argv[3] == "1":
    md = []
    md.append(f"# Baseline diff — {sys.argv[1]} → {sys.argv[2]}")
    md.append("")
    md.append(f"**Verdict:** `{verdict}`")
    md.append(f"- additive: {len(additive)} (OK)")
    md.append(f"- missing: {len(missing)} (FAIL)")
    md.append(f"- mismatch: {len(mismatch)} (FAIL)")
    if missing:
        md.append("\n## Missing (present in A, absent in B)\n")
        for m in missing:
            md.append(f"- `{m['path']}`")
    if mismatch:
        md.append("\n## Mismatch (different value)\n")
        for m in mismatch:
            md.append(f"- `{m['path']}` A=`{m['a']}` B=`{m['b']}`")
    print("\n".join(md), file=sys.stderr)

sys.exit(0 if verdict == "OK" else 1)
PY
