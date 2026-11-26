#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
"""Assert the three premises the post-merge CI decision rests on.

WHY THIS FILE EXISTS, AND WHY IT IS NOT A GO TEST.

`.github/workflows/ci.yml` triggers POST-MERGE on purpose, and the reason
written into it is that the gate stopping a bad merge is local:
`.claude/hooks/delivery-gate.sh` runs `task quality:all` before every
`git push` and blocks on red, with no bypass. That is a legitimate trade --
IF the hook exists, IF it is wired, and IF there is no bypass.

All three were false at the same time, measured 2026-09-05:

  1. the file was deleted in 9a6e6e903 (2026-09-02), third deletion;
  2. the wiring in .claude/settings.json went four days EARLIER, in
     63cc51c48 (2026-08-29), and the restore that put the file back the same
     day (53c1dc1ea) never noticed the wiring was gone;
  3. "no bypass" was false in four spellings, each a real push reaching the
     remote with the gate deciding nothing about it.

All three are fixed. What did not exist -- on any of those four days -- is
anything that would have SAID SO. `git log --oneline --since=2026-08-29
--until=2026-09-02 workspace` is 21 commits, an upper bound on what shipped
through an unarmed gate (the repository does not record when each push
happened, only when each commit was authored).

The assertions that DO exist for premises 1 and 3 live in
cmd/loop-runtime-ops/*_test.go, and they are not enough on their own for one reason
that decides this file's shape: THEY RUN WHERE THE GATE RUNS. They are
reached through `task quality:hermetic`, which is what the delivery gate
itself invokes. A guard whose only runner is the thing it guards reports on
itself; that is the defect family this repository measured eight times in the
week this was written, and the item that ordered this file forbids repeating
it. So this check runs on GitHub, on a machine that has no hook, no
`CLAUDE_PROJECT_DIR` and no stake in the answer.

WHY PYTHON AND NOT A NEW GO GATE. The Go tests in cmd/loop-runtime-ops need the Go
workspace, which needs the private loop-runtime-contracts module, which needs a
deploy key and two checkouts -- the setup `build-and-test` performs, in a job
that takes 2-4 minutes and whose last 15 runs all failed (measured
2026-09-06 over runs 32084704925..32197072865; the failing step is `Test the
hermetic modules`, and `go test ./... -count=1` in cmd/loop-runtime-ops is red on
`trufflehogscan.TestRealTreeIsAdjudicated` in a clean worktree today). A
new signal hung inside an already-red job is not a signal. This script needs
python3 and bash, both preinstalled on every runner, and finishes in seconds.

FAIL CLOSED. Every path that cannot reach an answer exits non-zero. A premise
this script did not get to check is reported as NOT CHECKED and never folded
into the pass -- "I could not look" must never print as "I looked and it was
fine", which is the exact failure this file was built to notice.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import traceback
from pathlib import Path

HOOK_RELPATH = "hooks/delivery-gate.sh"
SETTINGS_RELPATH = ".claude/settings.json"

# The four payloads an independent audit pushed to the remote on 2026-09-05
# with the gate deciding nothing about them, plus the plainest push as a
# control. Each must be recognised as a push.
#
# The observation is the same one cmd/loop-runtime-ops/delivery_gate_bypass_test.go
# makes and for the same reason: pointed at a directory that is not this
# repository, the hook refuses at the landmark check and exits 2 -- a decision
# it only reaches AFTER concluding the command was a push. So exit 2 means
# "seen as a push" and exit 0 means "waved through", and neither costs the six
# minutes `task quality:all` would.
MUST_BE_GATED = [
    "git push origin main",
    "git -C /tmp/lane-b192 push origin main",
    "echo --dry-run ; git push origin main",
    "git push origin main --force # --dry-run",
    "git push origin main; ls --dry-run",
]

# One control in the other direction. Without it a hook rewritten as a bare
# `exit 2` would satisfy every line above while checking nothing -- the gate
# has to DECIDE, not blanket-block. Kept to one deliberately: this script
# guards the premises ci.yml cites, and a gate that got STRICTER has not
# falsified any of them. Over-matching is a cost question, and it is
# cmd/loop-runtime-ops/delivery_gate_bypass_test.go that owns it.
MUST_NOT_BE_GATED = "git status"


class Report:
    """Collects verdicts so every premise is reported, not just the first bad one."""

    def __init__(self) -> None:
        self.failed = False

    def ok(self, line: str) -> None:
        print(f"  ok   {line}")

    def bad(self, line: str) -> None:
        self.failed = True
        print(f"::error::{line}")

    def unchecked(self, line: str) -> None:
        # Not a pass. A premise nobody could look at is a hole, and printing it
        # as anything softer than a failure is how the four days happened.
        self.failed = True
        print(f"::error::NOT CHECKED — {line}")


def premise_1_file_exists(root: Path, report: Report) -> bool:
    """The hook file is present. Returns whether premise 3 can be attempted."""
    hook = root / ".claude" / HOOK_RELPATH
    if not hook.exists():
        report.bad(
            f"PREMISE 1 — the hook file exists: {hook} is NOT THERE. "
            "ci.yml declares CI post-merge because this file gates every push; with it gone, "
            "nothing gates anything and the workflows' stated reason is false. "
            "It has been deleted three times (9a6e6e903 was the third)."
        )
        return False
    if not hook.is_file():
        report.bad(f"PREMISE 1 — the hook file exists: {hook} is not a regular file.")
        return False
    if hook.stat().st_size == 0:
        report.bad(f"PREMISE 1 — the hook file exists: {hook} is empty.")
        return False
    report.ok(f"PREMISE 1 — the hook file exists: {hook.relative_to(root)}")
    return True


def _applies_to_bash(matcher: object) -> bool:
    """Does this PreToolUse block cover the Bash tool?

    The harness treats `matcher` as a regular expression -- `Edit|Write` in this
    same file is an alternation -- so it is read as one here. An absent or empty
    matcher covers every tool.
    """
    if matcher is None or matcher == "" or matcher == "*":
        return True
    if not isinstance(matcher, str):
        raise ValueError(f"matcher is {type(matcher).__name__}, not a string: {matcher!r}")
    try:
        return re.search(matcher, "Bash") is not None
    except re.error as exc:
        raise ValueError(f"matcher {matcher!r} is not a valid regular expression: {exc}") from exc


def premise_2_is_wired(root: Path, report: Report) -> None:
    """The hook is wired into PreToolUse(Bash).

    THE HALF THAT ROTS. The file being present is not the hook running, and the
    wiring is the half that goes without anyone noticing: the kit installer owns
    the `hooks` KEY of settings.json wholesale (`mine["hooks"] = kit["hooks"]`),
    so a kit install deletes this entry while preserving the FILE. That is
    exactly 63cc51c48 -> 53c1dc1ea, and the four days between them.
    """
    settings = root / SETTINGS_RELPATH
    if not settings.exists():
        report.unchecked(
            f"PREMISE 2 — the hook is wired: {settings} does not exist, so nothing wires any hook "
            "in this tree and the gate cannot fire."
        )
        return
    try:
        data = json.loads(settings.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        report.unchecked(
            f"PREMISE 2 — the hook is wired: could not read {settings} ({exc}). "
            "Settings the harness cannot parse wire nothing, so this is not an inconclusive result."
        )
        return

    try:
        blocks = data.get("hooks", {}).get("PreToolUse", [])
        for block in blocks:
            if not _applies_to_bash(block.get("matcher")):
                continue
            for hook in block.get("hooks", []):
                if "delivery-gate.sh" in str(hook.get("command", "")):
                    report.ok(
                        "PREMISE 2 — the hook is wired: a PreToolUse block matching Bash runs "
                        "delivery-gate.sh"
                    )
                    return
    except (AttributeError, TypeError, ValueError) as exc:
        report.unchecked(
            f"PREMISE 2 — the hook is wired: {settings} does not have the shape this check reads "
            f"({exc}). A shape nobody can read is not a wiring anybody can rely on."
        )
        return

    report.bad(
        f"PREMISE 2 — the hook is wired: no PreToolUse(Bash) hook in {SETTINGS_RELPATH} runs "
        "delivery-gate.sh. THE FILE CAN BE PRESENT AND THE GATE STILL NEVER FIRE — that state "
        "lasted four days (63cc51c48 dropped the wiring on 2026-08-29; 53c1dc1ea restored the "
        "file the same day and not this), and 21 commits landed on `workspace` inside it."
    )


def _hook_verdict(bash: str, hook: Path, command: str, elsewhere: str) -> tuple[int, str]:
    """Run the hook on one payload and report what it decided.

    The environment is the real one with CLAUDE_PROJECT_DIR overridden, not a
    constructed one: a hand-built PATH that happens to omit python3 would send
    the hook down its degraded branch, and this check would then be measuring a
    path nobody runs. The Go suite owns the degraded branch
    (TestDeliveryGate_StillGatesAPushItCouldNotParse); this owns the real one.
    """
    payload = json.dumps({"tool_name": "Bash", "tool_input": {"command": command}})
    proc = subprocess.run(  # noqa: S603 - fixed argv, no shell
        [bash, str(hook)],
        input=payload,
        capture_output=True,
        text=True,
        timeout=60,
        env=dict(os.environ, CLAUDE_PROJECT_DIR=elsewhere),
    )
    return proc.returncode, proc.stderr


def premise_3_no_bypass(root: Path, report: Report) -> None:
    """The gate has no bypass -- the five payloads reach it, and it still decides."""
    hook = root / ".claude" / HOOK_RELPATH
    bash = shutil.which("bash")
    if bash is None:
        report.unchecked(
            "PREMISE 3 — there is no bypass: bash is not on PATH, so the hook could not be run "
            "and nothing here observed it."
        )
        return

    with tempfile.TemporaryDirectory() as elsewhere:
        # `elsewhere` holds no Taskfile.yml, so the hook refuses at the landmark
        # check and never spends minutes on `task quality:all`.
        escaped = []
        for command in MUST_BE_GATED:
            try:
                code, stderr = _hook_verdict(bash, hook, command, elsewhere)
            except (OSError, subprocess.SubprocessError) as exc:
                report.unchecked(
                    f"PREMISE 3 — there is no bypass: could not run the hook for {command!r} ({exc})."
                )
                return
            if code != 2 or "BLOCKED" not in stderr:
                escaped.append(f"{command!r} -> exit {code}")

        try:
            code, stderr = _hook_verdict(bash, hook, MUST_NOT_BE_GATED, elsewhere)
        except (OSError, subprocess.SubprocessError) as exc:
            report.unchecked(
                f"PREMISE 3 — there is no bypass: could not run the control ({exc})."
            )
            return

    if escaped:
        report.bad(
            "PREMISE 3 — there is no bypass: the gate did not decide anything about "
            f"{len(escaped)} of {len(MUST_BE_GATED)} payload(s) that reach the remote: "
            + "; ".join(escaped)
            + ". Each of these is a real push, and every one of them was measured escaping this "
            "hook on 2026-09-05. A gate that concludes 'not a push' about a push has reported a "
            "verdict on something it never looked at."
        )
        return

    if code != 0:
        report.bad(
            f"PREMISE 3 — there is no bypass: the control {MUST_NOT_BE_GATED!r} exited {code}, "
            "so this hook blocks things that are not pushes. It is not deciding, and a gate that "
            "fires on everything is removed within a day — which is how premises stop holding."
        )
        return

    report.ok(
        f"PREMISE 3 — there is no bypass: all {len(MUST_BE_GATED)} payloads are gated and the "
        "control is not"
    )


CI_WORKFLOW = ".github/workflows/ci.yml"


def premise_0_ci_gates_the_pull_request(root: Path, report: Report) -> bool:
    """Is the trunk guarded by CI itself, rather than by a hook on one machine?

    THE REGIME THIS FILE WAS WRITTEN FOR IS NOT THE ONLY ONE ANY MORE.

    The three premises below defend a specific trade: `ci.yml` may trigger
    post-merge BECAUSE a local hook blocks a bad push first. That trade was
    legitimate while this repository was private with one author, and it dies
    the moment anyone else can open a pull request -- a contributor has no
    `.claude/hooks/delivery-gate.sh`, cannot be made to have one, and a fork's
    workflow receives no secrets either. Measured 2026-09-16: `.claude/` stopped
    being tracked in 3d4075ca3 (2026-09-08), the last green run of this check was
    2026-09-09T14:39, and every run since has failed on a premise that can no
    longer become true.

    So the check asks first which regime is in force, and asserts what is
    actually true in it. When `ci.yml` gates the pull request, the guard is CI,
    it is verifiable by GitHub as a required check, and it covers every
    contributor rather than every contributor who installed a hook.
    """
    ci = root / CI_WORKFLOW
    if not ci.is_file():
        report.unchecked(f"PREMISE 0 — CI gates the pull request: {ci} is not there.")
        return False

    # Read by hand, not with a YAML library, for the same reason as the rest of this
    # file: the check runs on a bare runner, installing nothing, and one more
    # dependency is one more way for the check not to run. Comments are
    # discarded because this repository writes whole blocks of prose inside the
    # `on:` block -- a `# pull_request:` explaining why it is NOT there would be
    # read as if it were.
    lines = [l.split("#", 1)[0].rstrip() for l in ci.read_text(encoding="utf-8").splitlines()]
    try:
        on_at = next(i for i, l in enumerate(lines) if l.rstrip() == "on:")
    except StopIteration:
        report.bad(f"PREMISE 0 — CI gates the pull request: {CI_WORKFLOW} declares no `on:` block.")
        return False

    triggers = set()
    for line in lines[on_at + 1:]:
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip())
        if indent == 0:
            break                      # saiu do bloco `on:`
        if indent == 2 and line.rstrip().endswith(":"):
            triggers.add(line.strip().rstrip(":"))

    if "pull_request" not in triggers:
        report.bad(
            "PREMISE 0 — CI gates the pull request: "
            f"{CI_WORKFLOW} does NOT trigger on `pull_request` (found: "
            f"{', '.join(sorted(triggers)) or 'nothing'}). Falling back to the local-hook premises."
        )
        return False

    # A trigger is not a gate. Until 2026-09-23 this premise returned here on the
    # trigger alone, so a ci.yml that ran on the PR and checked nothing would have
    # certified the trunk as guarded. The gates are READ from the workflow, never
    # listed here a second time, and the verdict names them, so a reader sees how
    # much of the quality gate the pull request actually faces.
    ci_gates = sorted({m.group(1) for l in lines for m in QUALITY_GATE_CALL.finditer(l)})
    if not ci_gates:
        report.bad(
            "PREMISE 0 — CI gates the pull request: "
            f"{CI_WORKFLOW} triggers on `pull_request` but runs no quality gate "
            "(no `loop-runtime-ops quality <gate>` step). A trigger with nothing behind it protects nothing."
        )
        return False

    local_gates = sorted(_quality_gates_task_gates(root) - set(ci_gates))
    report.ok(
        "PREMISE 0 — CI gates the pull request: "
        f"{CI_WORKFLOW} triggers on `pull_request` and runs {len(ci_gates)} quality gate(s) "
        f"before the merge: {', '.join(ci_gates)}."
    )
    if local_gates:
        print(
            f"  note {len(local_gates)} gate(s) of `task quality:gates` run only where the "
            "task is run, NOT on the pull request: " + ", ".join(local_gates)
        )
    return True


# `loop-runtime-ops quality <gate>` as invoked from a workflow or a Taskfile line.
QUALITY_GATE_CALL = re.compile(r"loop-runtime-ops\s+quality\s+([a-z0-9][a-z0-9-]*)")


def _quality_gates_task_gates(root: Path) -> set[str]:
    """The gates `task quality:gates` runs, read from taskfiles/quality.yml."""
    taskfile = root / "taskfiles" / "quality.yml"
    if not taskfile.is_file():
        return set()
    gates: set[str] = set()
    in_gates = False
    for raw in taskfile.read_text(encoding="utf-8").splitlines():
        line = raw.split("#", 1)[0].rstrip()
        if line.startswith("  ") and not line.startswith("   ") and line.endswith(":"):
            in_gates = line.strip() == "gates:"
            continue
        if in_gates:
            gates.update(m.group(1) for m in QUALITY_GATE_CALL.finditer(line))
    return gates


def main(argv: list[str] | None = None) -> int:
    default_root = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", default=str(default_root), help="repository root to check")
    args = parser.parse_args(argv)
    root = Path(args.root).resolve()

    print(f"delivery gate armed? checking {root}")
    report = Report()

    # Which regime? Asking this first is the whole change: the three premises
    # below are the RIGHT check for a repository whose only guard is local, and
    # the WRONG one for a repository whose CI gates the pull request.
    if premise_0_ci_gates_the_pull_request(root, report):
        print()
        print(
            "CI gates the pull request, so the local-hook premises are not what protects\n"
            "the trunk here and are not checked. They remain in this file for a repository\n"
            "that still makes the post-merge trade."
        )
        return 0

    have_file = premise_1_file_exists(root, report)
    premise_2_is_wired(root, report)
    if have_file:
        premise_3_no_bypass(root, report)
    else:
        report.unchecked(
            "PREMISE 3 — there is no bypass: the hook file is not there, so there was nothing to "
            "run and nothing here observed a bypass either way."
        )

    if report.failed:
        print()
        print(
            "The local delivery gate is NOT armed as ci.yml and workflow-hygiene.yml claim it is.\n"
            "Those workflows are POST-MERGE because this gate is supposed to catch a bad change\n"
            "before the push. While the above is true, nothing does — restore the hook and its\n"
            "wiring, or change the workflows' trigger to match what is actually true."
        )
        return 1

    print()
    print("All three premises hold. The post-merge trigger in ci.yml rests on something real.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 - fail closed: an unhandled error is not a pass
        traceback.print_exc()
        print("::error::the armed-check itself failed to complete — it has NOT verified anything.")
        sys.exit(1)
