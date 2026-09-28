---
type: Component
title: "api/internal/audit — implemented, documented, and not wired"
description: "A closed audit-action enum written to stop a raw-SQL leak, which no code path calls."
resource: "api/internal/audit"
tags:
  - audit
  - orphan
  - enforcement
sources:
  - id: au-pkg
    resource: "api/internal/audit/domain_events.go#L3"
    note: "audit"
  - id: au-find
    resource: "api/internal/audit/domain_events.go#L43"
    note: "api/internal/audit is implemented and has no caller anywhere in the repository"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`api/internal/audit` documents itself as *the canonical engine-side enforcement* of a closed
audit-action enum. It was added after a QA run on 2026-06-21 found a raw SQL fragment leaking
into the audit log. It declares a two-phase rollout gated by `AUDIT_ENFORCE_DOMAIN_ENUM` and
exports `ValidateAction()`.[^au-pkg]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.audit` | `api/internal/audit` | package | domain | `api/internal/audit/domain_events.go:3` | Closed enum of allowed audit actions, added after a QA run found a raw SQL fragment leaking into the audit log. Two-phase rollout gated by AUDIT_ENFORCE_DOMAIN_ENUM. |

# The measurement

| Question | Answer |
|---|---|
| Workspace packages importing it | **0 of 318** |
| `.go` files outside its own directory referencing `api/internal/audit` | **none** |
| Callers of `ValidateAction` | **none** |

The enforcement the package describes is therefore **not in the request path**, and the leak it
was written to stop can still occur.[^au-find]

# Relationships

- The table it protects is `audit_log` in
  [`/entities/database-schema.md`](/entities/database-schema.md); its likely call site is the
  store wrapper in [`/components/persistence.md`](/components/persistence.md).
- It is the sharpest instance of the pattern `CLAUDE.md` itself names under *Known Systemic
  Patterns* as **"implemented but not wired"** — the repository documents the failure mode and
  then contains it. Three other packages sit in the same state and are listed in
  [`/operations/contradictions.md`](/operations/contradictions.md).
- A package that is dormant *on purpose* reads differently: `api/internal/payloadlog` says so in
  its own doc comment. This one does not.

# Evidence

[^au-pkg]: `api/internal/audit/domain_events.go:3`
[^au-find]: `api/internal/audit/domain_events.go:43`
