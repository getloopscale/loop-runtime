---
title: Restore governance-gate.py + meeting-orchestrator + decision-record-moderator
date: 2026-09-06
status: ACCEPTED
tier: T3
signed_by: sponsor (via chat approval 2026-09-06)
supersedes: none
related: B-136 (halt report documenting the gap), B-197 (this restoration item)
---

# Restore selective apparatus deleted in commit `8cb41f660`

## Context

On 2026-08-06, commit `8cb41f660` ("Remove docs inconsistent") performed a bulk `.claude/` removal that deleted 20+ agents and the `governance-gate.py` hook — the only mechanism enforcing `pre-release-governance.md` T2/T3 classification. B-136's 2026-09-04 halt (see `.claude/records/maintenance-runs/b136-cell-rename-governance-BLOCKED.md` and the amendment section landed today in `.claude/rules/pre-release-governance.md`) named 11 of these missing agents. `pre-release-governance.md` explicitly ran as HONOR-SYSTEM for 30 days.

The sponsor authorized restoration on 2026-09-06. Before executing, measurement revealed that:

1. **The deletion was broader than 11 agents** — ~20 agent files, `.claude/CHANGELOG.md`, `.claude/README.md`, `.claude/.loopzero/.sync-manifest.json`, and extensive `docs/wiki/` were also deleted.
2. **The squad architecture changed after the deletion** — the current `~/dev/squad/agents/` carries 14 decision-role-based agents (iris, kairos, hermes, daedalus, vera, eureka, nemesis, aesculapius, argus, clio, metis, leonardo, hecate, vigil), which supersede several deleted repo-role-based agents.

## Decision

Restore **selectively**, only what has no substitute in the current architecture:

1. `.claude/hooks/governance-gate.py` — the enforcement hook (291 lines, restored from `8cb41f660^`)
2. `.claude/agents/meeting-orchestrator.md` — no equivalent in the 14 current squad agents (215 lines)
3. `.claude/agents/decision-record-moderator.md` — no equivalent (clio historia, does not moderate) (209 lines)

## Alternatives rejected

- **(A) Restore all 20+ deleted agents** — creates two coexisting architectures (repo-role-based deleted set + decision-role-based current set); violates parsimony ladder rung 5 (redundant abstraction). Overlaps identified: `change-governor` ↔ `daedalus-tech-lead` + `hermes-scrum-master`; `production-design-reviewer` ↔ `vera-technical-arbiter`; `security-blast-radius-reviewer` ↔ `eureka-defect-hunter` + `vera`; `cli-engineer`/`api-engineer`/`operator-engineer`/`infra-engineer` ↔ domain specialists that per current `agents/README.md` are "derived per project, never shipped" (would live in per-project `.claude/agents/`).

- **(C) Write a new governance-gate.py integrated with `decision-delegation.txt`** — turning today's HONOR-SYSTEM into BLOCKING for the delegation classes. Correct in spirit but requires its own plan-write cycle + halt-loop. Deferred as a separate future item; the deleted hook is fully sufficient for T2/T3 enforcement per the original design.

## Wire-up

Added to `.claude/settings.json` PreToolUse Edit|Write alongside `boundary-check.py`:

```json
{
  "type": "command",
  "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/governance-gate.py\"",
  "timeout": 10
}
```

## Approval ledger

`.claude/governance-gate.local.md` created with schema explained and `Status: PENDING`. **No production scope authorized.** Every future edit to `api/`, `pkg/`, `operators/`, `infra/helm/`, `infra/pulumi/`, `infra/scripts/`, `.github/workflows/`, `Taskfile.yml`, or `Tiltfile` will BLOCK (exit 2) until the sponsor (or delegated Change Governor if re-derived per project) appends an `## Approved Scope` section for that change.

Exempt paths (bypass without approval) enumerated in the hook's `EXEMPT_PATTERNS` and re-stated in the local ledger.

## Verification

- Production path test: `echo '{"tool_input":{"file_path":".../api/internal/test.go"}}' | python3 .claude/hooks/governance-gate.py` → exit 2 (BLOCKED)
- Exempt path test: `echo '{"tool_input":{"file_path":".../CHANGELOG.md"}}' | python3 .claude/hooks/governance-gate.py` → exit 0 (PASS)

## Consequence for the backlog

- **B-136 bullet 3** was: *"pre-release-governance.md ganha o que faltou: hoje a classificação T2/T3 é honor-system"*. This restoration answers it — T2/T3 is now HONOR-SYSTEM + BLOCKING at Edit/Write time.
- **B-057, B-058, B-134, B-113** — T2/T3 items previously halted awaiting `change-governor` agent. `change-governor` was NOT restored (superseded by daedalus). Their governance path becomes: sponsor writes decision in `.claude/governance-gate.local.md § Approved Scope` and delegates execution to `daedalus-tech-lead` per `decision-delegation.txt`. If a change reaches `pre-release-governance.md § Meeting Flow`, the sponsor uses `meeting-orchestrator` + `decision-record-moderator` (both restored) to produce the MEET record + ADR.

## Rollback

If governance-gate.py causes excessive friction: remove the hook line from `.claude/settings.json` PreToolUse Edit|Write. The hook is idempotent and safe to disable/re-enable.
