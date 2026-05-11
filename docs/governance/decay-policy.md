# Decay TTL Policy

**Status:** Canonical (per plan `status-validation-and-doc-consolidation-plan.md` T0.3, ADR D3)
**TTL:** 90 days (this policy itself is strategic — revalidate quarterly)
**Last validated:** 2026-05-11

## Purpose

Toda claim em `docs/STATUS.md` e cada arquivo em `docs/evidence/` tem **TTL visível**. Sem TTL, claims viram folclore — alguém escreveu uma vez, ninguém revalidou, virou "verdade institucional" mesmo desatualizada. Esse documento define os 3 tiers de TTL aplicáveis a cada tipo de claim e o protocolo de revalidação.

## TTL Tiers

### Tier 1 — Operational (30 dias)

**Aplica a:**
- Test counts (`1210+ CLI tests` é claim operacional)
- LIVE validations (cleanroom 12/12, journey 80/80)
- Journey results (dogfood pass rates)
- Metric counts (`43+ loopzero_* metrics`)
- Sprint progress claims (X/Y tasks)

**Rationale:** Esses números mudam toda semana. 30 dias é o limite onde claim ainda é "approximately true". Após 30 dias, número provavelmente está stale.

### Tier 2 — Architectural (60 dias)

**Aplica a:**
- State machine wired (11 state machines em `pkg/statemachine/`)
- CRD reconcile flows (8 controllers, 10 CRDs)
- Plan tier capabilities (Starter 2 replicas, Pro 5, etc.)
- Helm chart inventory (32 bootstrap + 13 published)
- Multi-cell topology (3 cells, ClusterCredential per cell)

**Rationale:** Arquitetura muda por sprint, não por semana. 60 dias permite 1 sprint pleno antes de exigir revalidação.

### Tier 3 — Strategic (90 dias)

**Aplica a:**
- ADRs (decisões arquiteturais)
- Bias taxonomy (esse documento próprio)
- Evidence bar policy
- Decay policy (recursivo — esse documento)
- Persona definitions
- Sprint plan high-level (sprints 0-11 completion structure)

**Rationale:** Decisões estratégicas mudam por trimestre. 90 dias força revalidação consciente sem ser fricção operacional.

## TTL Override (Explicit)

Claim pode ter `ttl_override: <days>` no frontmatter para extender (ex: 180 dias) **se houver ADR-link justificando**.

**Schema:**
```yaml
ttl_days: 30           # Tier default
ttl_override: 180      # Extended
ttl_override_adr: docs/meetings/adr/00XX-extended-ttl-for-Y.md
last_validated: 2026-05-11
expires: 2026-11-07    # 180 days from last_validated
```

Validator rejeita `ttl_override` sem `ttl_override_adr`.

## Decay Lifecycle

```
Created → Fresh → Aging → Expiring → Expired → Revalidated → Fresh
                                          │
                                          └─ (if not revalidated) → Stale Folklore (BLOCKED from STATUS.md)
```

| State | Definition | Action |
|-------|------------|--------|
| **Fresh** | `today < last_validated + 0.5 × ttl_days` | None |
| **Aging** | `0.5 × ttl_days ≤ days_since_validated < 0.8 × ttl_days` | None (informational) |
| **Expiring** | `0.8 × ttl_days ≤ days_since_validated < ttl_days` | Yellow flag — schedule revalidation |
| **Expired** | `days_since_validated ≥ ttl_days` | Red flag — open issue, block STATUS.md inclusion |
| **Revalidated** | Operator reran revalidation commands + updated `last_validated` | Returns to Fresh |
| **Stale Folklore** | Expired + 30 dias sem revalidação | Quarantine — move to `docs/evidence/_archived/` |

## Revalidation Protocol

Toda evidence file MUST ter section **"How to revalidate"** com comandos exatos:

```markdown
## How to revalidate
1. Run: `task dogfood:journey-04` against fresh cleanroom
2. Verify: 12/12 journeys PASS in logs
3. Update frontmatter `last_validated` to today's date
4. Recompute `expires = last_validated + ttl_days`
5. Commit: `docs(evidence): revalidate {claim-id} — {summary}`
```

Sem essa section, decay scanner emite WARNING (não block).

## Decay Scanner

`infra/scripts/governance/scan-decay.sh` lê frontmatter de:
- `docs/evidence/**/*.md`
- `docs/STATUS.md` (se existir, after Phase 4)

Output:
```
EXPIRED CLAIMS REPORT — 2026-05-15
================================================================

CRITICAL (>60 days overdue):
  - claim-id: example-foo
    file: docs/evidence/security/foo.md
    last_validated: 2025-12-01
    expires: 2026-01-01
    days_overdue: 134
    revalidation_steps: See section "How to revalidate"

HIGH (>30 days overdue):
  - claim-id: example-bar
    ...

MEDIUM (≤30 days overdue):
  - ...

================================================================
Summary: 3 CRITICAL, 7 HIGH, 12 MEDIUM (out of 80 total claims)
```

## CI Integration

`.github/workflows/decay-scan.yml` runs weekly (Sunday 02:00 UTC, after `cell2-soaks.yml` 01:00 UTC).

On decay detection:
1. Scanner outputs JSON list of expired claims
2. Workflow opens/updates GitHub issue `[decay] N expired claims need revalidation`
3. (Optional T5.4) Notify Slack channel

## Edge Cases

### Claim with no `last_validated`
Default to `git_last_modified_date(claim.source.file)` (T1.3 init script).

### File renamed
Use `git log -1 --follow --format=%aI -- <file>` to track through renames (EC-4 from edge-case review).

### TTL_override without ADR
Validator exit 1 with `TTL_OVERRIDE_REQUIRES_ADR`.

### Bulk revalidation
Operator can run `task revalidate:dimension SECURITY` to refresh all security-dim claims after a big sprint.

## Cross-References

- `docs/governance/evidence-bar.md` — evidence schema (consumes TTL fields)
- `infra/scripts/governance/scan-decay.sh` — implementation
- `.github/workflows/decay-scan.yml` — CI gate
- ADR D3 (plan): TTL policy rationale
- **Visual:** `docs/architecture/governance/concept-decay-lifecycle.excalidraw` — state machine + decision diamond (T0.5b)
