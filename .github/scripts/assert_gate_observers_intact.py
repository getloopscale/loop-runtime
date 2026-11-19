#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""Assert that the things which report a disarmed delivery gate are still there.

WHAT THIS CLOSES, AND WHY IT IS A SECOND FILE.

`.github/workflows/delivery-gate-armed.yml` is the one check in this repository
that goes red when the local delivery gate is not armed. It was added because
the post-merge trigger in ci.yml rests on three premises about that gate and
nothing verified any of them; all three were false at once for four days
(B-192).

An audit of that fix measured the hole it left, and the hole is the shape of the
fix itself: A WORKFLOW CANNOT REPORT ITS OWN DELETION. Delete
delivery-gate-armed.yml and no run is queued, no check appears, and a check that
never runs is not a red check -- it is an absent one, which reads on a pull
request exactly like a repository that never had it. The same is true one step
softer and harder to see: leave the file in place and give it a
`paths: ['**/*.go']`, and it survives every listing while never firing for the
`.claude/**`-only commit that is the exact shape of all three historical
disarms.

So the reporter has to be something the deleting commit does not also delete.
This script is that something, and it is deliberately run from TWO substrates
that fail independently:

  LOCAL, before the push. `task quality:hermetic` reaches it through the Go
  tests in cmd/loopzero-ops, and `quality:hermetic` is what the delivery gate itself
  invokes. Here it costs ZERO ungated pushes: the deletion is refused on the
  machine that made it, before anything leaves.

  ON GITHUB, at the push and on the promotion. `.github/workflows/
  workflow-hygiene.yml` runs it on pushes to `workspace`, `develop` and `main`
  and on pull requests into `develop` and `main`. This is the half that still
  speaks when the local gate is the thing that died, which is the case the local
  half structurally cannot cover.

Neither substrate checks itself here. The armed-check watches the local hook;
this watches the workflow that runs the armed-check; the local hook's suite runs
this. That is a ring across two machines, not a guard reporting on itself, and
the difference is the whole reason this file exists rather than one more
assertion inside delivery-gate-armed.yml.

WHERE THE RING STILL ENDS -- read this before citing the coverage. One commit
that disarms the local gate AND deletes both workflow files is reported by
nothing, because every reporter named above is in it. Terminating that regress
needs an authority outside the file tree, and the only one GitHub offers is a
REQUIRED status check. Measured 2026-09-06 against this repository:

    gh api repos/getloopscale/engine/branches/develop/protection  -> 403
    gh api repos/getloopscale/engine/rulesets                     -> 403
    "Upgrade to GitHub Pro or make this repository public to enable this
     feature."   (org `getloopscale` is on the `free` plan; `loopzero` is PRIVATE)

So on this plan NO check in this repository can be made required -- not this
one, not the armed-check, not ci.yml. Every one of them notifies and none of
them blocks, and that is a purchase decision, not a checkbox somebody forgot.

FAIL CLOSED. Anything this script could not read is reported as NOT CHECKED and
counts as failure. "I could not look" printing as "I looked and it was fine" is
the precise defect the four days were made of.

LIMIT, DECLARED. The `on:` block is read by INDENTATION, not by a YAML parser,
for the same reason check-workflow-hygiene.sh reads `run:` blocks that way: the
faithful alternative is a non-stdlib dependency (PyYAML) inside a fail-closed
gate, and a gate that goes red because an import was missing on the runner is a
gate that gets switched off. Inline `# comments` and both list spellings (flow
`[a, b]` and block `- a`) are handled; exotic YAML -- anchors, multi-line keys,
quoted trigger names -- is not, and would be reported as NOT CHECKED rather than
passed.
"""

from __future__ import annotations

import argparse
import re
import fnmatch
import sys
import traceback
from pathlib import Path

ARMED_WORKFLOW = ".github/workflows/delivery-gate-armed.yml"
HYGIENE_WORKFLOW = ".github/workflows/workflow-hygiene.yml"
ARMED_SCRIPT = ".github/scripts/assert_delivery_gate_armed.py"
OBSERVERS_SCRIPT = ".github/scripts/assert_gate_observers_intact.py"

# `workspace` is the point of the list, not padding in it. The window B-192
# measured was four days and 21 commits on `workspace`; without it here the
# earliest a non-local check could speak is the promotion PR, which is the
# four-day delay reappearing under a new name.
PUSH_BRANCHES = ("workspace", "develop", "main")
PR_BRANCHES = ("develop", "main")


class Report:
    """Collects every verdict, so one break does not hide the next."""

    def __init__(self) -> None:
        self.failed = False

    def ok(self, line: str) -> None:
        print(f"  ok   {line}")

    def bad(self, line: str) -> None:
        self.failed = True
        print(f"::error::{line}")

    def unchecked(self, line: str) -> None:
        # Not a pass, and not a shrug. A property nobody could look at is a hole.
        self.failed = True
        print(f"::error::NOT CHECKED — {line}")


def strip_comment(line: str) -> str:
    """Drop a trailing `#` comment, respecting quotes.

    A widening in this repository was once defeated by a trailing comment, so
    the values below are read with the comment removed rather than matched as
    whole lines: `branches: [workspace, develop, main]  # workspace matters` has
    to read the same as the bare list.
    """
    quote = ""
    for i, ch in enumerate(line):
        if quote:
            if ch == quote:
                quote = ""
        elif ch in "\"'":
            quote = ch
        elif ch == "#":
            return line[:i]
    return line


def indent_of(line: str) -> int:
    return len(line) - len(line.lstrip())


def block_body(lines: list[str], start: int) -> list[str]:
    """Lines strictly more indented than lines[start], up to the next sibling."""
    base = indent_of(lines[start])
    body: list[str] = []
    for line in lines[start + 1 :]:
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if indent_of(line) <= base:
            break
        body.append(line)
    return body


def find_key(lines: list[str], key: str) -> int | None:
    """Index of `key:` among the SHALLOWEST keys of these lines, or None.

    Restricting to the shallowest depth is what keeps `branches:` under `push:`
    from answering for `pull_request:` -- the two live at the same depth and a
    plain scan would return whichever came first.
    """
    candidates = [i for i, line in enumerate(lines) if line.strip() and not line.lstrip().startswith("#")]
    if not candidates:
        return None
    top = min(indent_of(lines[i]) for i in candidates)
    for i in candidates:
        if indent_of(lines[i]) != top:
            continue
        if strip_comment(lines[i]).strip().startswith(f"{key}:"):
            return i
    return None


def value_list(lines: list[str], start: int) -> list[str] | None:
    """The list value of lines[start], flow or block style. None if unreadable."""
    head = strip_comment(lines[start]).strip()
    _, _, inline = head.partition(":")
    inline = inline.strip()
    if inline.startswith("[") and inline.endswith("]"):
        items = [item.strip().strip("\"'") for item in inline[1:-1].split(",")]
        return [item for item in items if item]
    if inline:
        return None  # a scalar where a list was expected -- not something to guess at
    items = []
    for line in block_body(lines, start):
        entry = strip_comment(line).strip()
        if not entry.startswith("- "):
            return None
        items.append(entry[2:].strip().strip("\"'"))
    return items or None


def on_block(path: Path, report: Report, label: str) -> list[str] | None:
    """The body of the top-level `on:` mapping of a workflow file."""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        report.unchecked(f"{label}: could not read {path} ({exc}).")
        return None
    for i, line in enumerate(lines):
        if indent_of(line) == 0 and strip_comment(line).strip() == "on:":
            body = block_body(lines, i)
            if not body:
                report.unchecked(f"{label}: the `on:` block of {path} is empty or unreadable.")
                return None
            return body
    report.unchecked(
        f"{label}: no top-level `on:` block in {path}. A workflow whose triggers this check "
        "cannot read is a workflow nobody can claim fires."
    )
    return None


def require_branches(body: list[str], event: str, wanted: tuple[str, ...], report: Report, label: str) -> None:
    idx = find_key(body, event)
    if idx is None:
        report.bad(
            f"{label}: no `{event}:` trigger. Without it this workflow does not run on "
            f"{'/'.join(wanted)}, and a check that does not run reads exactly like a repository "
            "that never had one."
        )
        return
    inner = block_body(body, idx)
    b_idx = find_key(inner, "branches")
    if b_idx is None:
        report.bad(f"{label}: `{event}:` has no `branches:` list, so which refs it covers is not stated.")
        return
    got = value_list(inner, b_idx)
    if got is None:
        report.unchecked(f"{label}: could not read the `branches:` list under `{event}:`.")
        return
    missing = [b for b in wanted if b not in got]
    if missing:
        report.bad(
            f"{label}: `{event}.branches` is {got} and does not cover {missing}. "
            "The four-day window this whole mechanism exists for was 21 commits on `workspace`; "
            "a trigger that skips a branch cannot report a disarm that happens on it."
        )
        return
    report.ok(f"{label}: `{event}` covers {list(wanted)}")


def forbid_paths_filter(body: list[str], report: Report, label: str) -> None:
    for key in ("paths", "paths-ignore"):
        for event in ("push", "pull_request"):
            idx = find_key(body, event)
            if idx is None:
                continue
            if find_key(block_body(body, idx), key) is not None:
                report.bad(
                    f"{label}: `{event}` carries a `{key}:` filter. This file has none ON PURPOSE — "
                    "`.claude/**` is the only place the gate can be disarmed from, and two of the "
                    "three commits that disarmed it touched nothing else. A filter here reproduces "
                    "the blindness the file was split out of ci.yml to escape."
                )
                return
    report.ok(f"{label}: no `paths`/`paths-ignore` filter — it fires for a `.claude/**`-only commit")


def require_paths_cover_watched(body: list[str], report: Report, label: str,
                                watched: str) -> None:
    """A filtered observer must see a change to the file it reports on.

    It used to ask whether ANY pattern started with `.github/`. An audit measured
    what that buys on 2026-09-06: `paths: ['.github/workflows/ci.yml']` satisfies it
    literally, covers nothing else, and the script printed

        ok  OBSERVER 2 ... any 'paths:' filter still covers '.github/'

    while deleting the very file this observer exists to report would not start it.
    The question is not where the pattern lives, it is whether the pattern MATCHES
    the watched path -- so it is asked with fnmatch against that path.

    `paths-ignore` is read too, and it was not read at all: naming the watched file
    there excludes it outright, and the old check had nothing to say about a key it
    never looked for. An exclusion is the same silence as a narrowing, spelled the
    other way.
    """
    for event in ("push", "pull_request"):
        idx = find_key(body, event)
        if idx is None:
            continue
        inner = block_body(body, idx)

        p_idx = find_key(inner, "paths")
        if p_idx is not None:
            patterns = value_list(inner, p_idx)
            if patterns is None:
                report.unchecked(f"{label}: could not read the `paths:` list under `{event}:`.")
            elif not any(fnmatch.fnmatch(watched, pat) for pat in patterns):
                report.bad(
                    f"{label}: `{event}.paths` is {patterns} and none of it matches "
                    f"{watched!r}, so a change to the file this observer reports on "
                    "would not start it."
                )

        i_idx = find_key(inner, "paths-ignore")
        if i_idx is not None:
            ignored = value_list(inner, i_idx)
            if ignored is None:
                report.unchecked(f"{label}: could not read `paths-ignore:` under `{event}:`.")
            elif any(fnmatch.fnmatch(watched, pat) for pat in ignored):
                report.bad(
                    f"{label}: `{event}.paths-ignore` is {ignored}, which excludes "
                    f"{watched!r} — the observer is switched off for exactly the change "
                    "it exists to report."
                )


def require_verdict_reaches_the_check(path: Path, report: Report, label: str) -> None:
    """A step that runs and whose failure is discarded is not a check.

    `continue-on-error: true` makes the step execute, exit non-zero, and the check go
    green; `if: false` stops the job running at all. Both were measured green by an
    audit on 2026-09-06 against a script whose final line said the observers "still
    invoke their checks". Invoking is not reporting, and the sentence was asserted
    where the second was what mattered.
    """
    for marker, why in (
        ("continue-on-error: true",
         "the step runs, exits non-zero, and the check is green anyway"),
        ("if: false",
         "the job never runs, so its verdict never exists"),
    ):
        if marker in path.read_text(encoding="utf-8"):
            report.bad(
                f"{label}: contains `{marker}` — {why}. A verdict that cannot reach "
                "the check is not a check."
            )
            return
    report.ok(f"{label}: every `paths:`/`paths-ignore:` filter still matches "
              f"{ARMED_WORKFLOW}, and the verdict can reach the check")


def require_runs(path: Path, needle: str, report: Report, label: str) -> None:
    """Some `run:` step invokes `needle`.

    THE MATCH DELIBERATELY SPANS THE BLOCK, not just the `run:` line. Requiring
    both on one line reads `run: python3 x.py` and MISSES

        run: |
          python3 x.py

    which is the spelling anyone reaches for the moment a second command joins
    the step. That would be a gate going red for a reformat, and a gate that is
    red for a reformat is a gate that gets switched off -- the failure mode this
    whole mechanism exists to avoid. So the `run:` block is delimited by
    indentation, the same approximation check-workflow-hygiene.sh already uses
    for G2 and with the same declared limit. Comment lines never match, so the
    long headers above -- which name these scripts repeatedly -- cannot answer
    for an invocation that is gone.
    """
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        report.unchecked(f"{label}: could not read {path} ({exc}).")
        return
    in_run, run_indent = False, 0
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if re.match(r"^-?\s*run:", stripped):
            in_run, run_indent = True, indent_of(line)
            if needle in stripped:
                report.ok(f"{label}: still invokes {needle}")
                return
            continue
        if in_run:
            if stripped and indent_of(line) <= run_indent:
                in_run = False
                continue
            if needle in stripped:
                report.ok(f"{label}: still invokes {needle}")
                return
    report.bad(
        f"{label}: nothing in {path} runs {needle} in a `run:` step. The file can be present, "
        "trigger correctly, and check nothing at all."
    )


def present(root: Path, relpath: str, report: Report, label: str) -> Path | None:
    path = root / relpath
    if not path.is_file() or path.stat().st_size == 0:
        report.bad(
            f"{label}: {relpath} IS NOT THERE. Deleting it does not turn a check red — it removes "
            "the check, and an absent check reads on a pull request exactly like a repository that "
            "never had one. Nothing else in this tree reports what it reported."
        )
        return None
    report.ok(f"{label}: {relpath} is present")
    return path


def main(argv: list[str] | None = None) -> int:
    default_root = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--root", default=str(default_root), help="repository root to check")
    args = parser.parse_args(argv)
    root = Path(args.root).resolve()

    print(f"are the gate observers intact? checking {root}")
    report = Report()

    # OBSERVER 1 — the check that reports a disarmed local gate.
    label = "OBSERVER 1 — delivery-gate-armed.yml"
    armed = present(root, ARMED_WORKFLOW, report, label)
    if armed is not None:
        body = on_block(armed, report, label)
        if body is not None:
            require_branches(body, "push", PUSH_BRANCHES, report, label)
            require_branches(body, "pull_request", PR_BRANCHES, report, label)
            forbid_paths_filter(body, report, label)
        require_runs(armed, ARMED_SCRIPT, report, label)

    # The armed-check's own script. Its absence already fails the workflow step
    # on GitHub; it is asserted here so the LOCAL run refuses the deletion before
    # the push instead of after it.
    label = "OBSERVER 1 — assert_delivery_gate_armed.py"
    present(root, ARMED_SCRIPT, report, label)
    if armed is not None:
        require_verdict_reaches_the_check(armed, report, label)

    # OBSERVER 2 — the workflow that reports OBSERVER 1's deletion.
    label = "OBSERVER 2 — workflow-hygiene.yml"
    hygiene = present(root, HYGIENE_WORKFLOW, report, label)
    if hygiene is not None:
        body = on_block(hygiene, report, label)
        if body is not None:
            require_branches(body, "push", PUSH_BRANCHES, report, label)
            require_branches(body, "pull_request", PR_BRANCHES, report, label)
            # The watched path is OBSERVER 1's file: this observer exists to report
            # its deletion, so a filter that does not match it is the whole defect.
            require_paths_cover_watched(body, report, label, ARMED_WORKFLOW)
        require_runs(hygiene, OBSERVERS_SCRIPT, report, label)
        require_verdict_reaches_the_check(hygiene, report, label)

    if report.failed:
        print()
        print(
            "The mechanism that reports an unarmed delivery gate is not intact.\n"
            "ci.yml is POST-MERGE because the local gate is supposed to stop a bad change before\n"
            "the push, and the only thing that checks whether that gate exists is the workflow\n"
            "above. While the report is red, the reason written into ci.yml is an assertion again\n"
            "rather than a measurement — restore what is named, or change ci.yml to say what is\n"
            "now true."
        )
        return 1

    print()
    print("Both observers are present, triggered where a disarm happens, and their\n"
          "verdicts can still reach the check. `still invoke their checks` was what\n"
          "this line said until 2026-09-06, when an audit made both steps run, fail,\n"
          "and report green with one token of `continue-on-error`.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 - fail closed: an unhandled error is not a pass
        traceback.print_exc()
        print("::error::the observer-check itself failed to complete — it has NOT verified anything.")
        sys.exit(1)
