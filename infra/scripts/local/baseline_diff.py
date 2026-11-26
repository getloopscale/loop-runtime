#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
"""Structural JSON diff for baseline-diff.sh (de-costumed from the inline
python3 heredoc per plan home-unhomed-logic-suspect-keeps T1.2).

Compares two baseline snapshots, classifies deltas (additive/missing/mismatch),
emits the JSON report to stdout + optional markdown to stderr, and exits with
the verdict (0 OK / 1 FAIL). Invoked by baseline-diff.sh as:

  python3 baseline_diff.py SNAPSHOT_A SNAPSHOT_B MARKDOWN(0|1)
"""
import json
import sys

VOLATILE_KEYS = {
    "resourceVersion", "generation", "managedFields",
    "creationTimestamp", "uid", "lastTransitionTime",
}


def normalize(obj):
    """Recursively strip volatile fields. EC-7 defensive second pass."""
    if isinstance(obj, dict):
        return {k: normalize(v) for k, v in obj.items() if k not in VOLATILE_KEYS}
    if isinstance(obj, list):
        return [normalize(item) for item in obj]
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
        a_by = {array_key(it): it for it in a}
        b_by = {array_key(it): it for it in b}
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


def main() -> int:
    with open(sys.argv[1]) as f:
        a_raw = json.load(f)
    with open(sys.argv[2]) as f:
        b_raw = json.load(f)
    a = normalize(a_raw)
    b = normalize(b_raw)

    additive, missing, mismatch = [], [], []
    walk(a, b, "", additive, missing, mismatch)

    verdict = "OK" if (not missing and not mismatch) else "FAIL"

    report = {
        "snapshot_a": sys.argv[1],
        "snapshot_b": sys.argv[2],
        "verdict": verdict,
        "classification": {
            "additive": additive,
            "missing": missing,
            "mismatch": mismatch,
        },
        "counts": {
            "additive": len(additive),
            "missing": len(missing),
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

    return 0 if verdict == "OK" else 1


if __name__ == "__main__":
    sys.exit(main())
