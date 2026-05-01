# Customer Communication Template — Cell-2 Canary Migration

> CHG-2026-04-30-INFRA-NORMALIZE / Phase 7 deliverable. Templates only —
> populate placeholders before sending and route through customer success
> for tone/legal review. All comms include explicit opt-in / opt-out lines.

## When to use

| Template | Trigger |
|---|---|
| **§1 Pre-canary opt-in invitation** | T7.1 — selecting 1 customer for stage 4 (7-day hold). |
| **§2 Migration scheduled notification** | 24h before pinning customer's tenant to cell-2. |
| **§3 Migration completion + observability access** | Within 1h of `loopzero deploy --cell cell-2` succeeding. |
| **§4 Daily soak update (days 1-7)** | Daily during the 7-day hold. Optional after day 3 if no anomalies. |
| **§5 Stage 4 success + thank-you** | End of 7-day hold with all SLOs green. |
| **§6 Incident notification (P1/P2)** | If EB consumption > 1% OR SLO breach. |
| **§7 Rollback to cell-1 notification** | If incident triggers `loopzero deploy --cell cell-1 <tenant>`. |
| **§8 Stage 5a/5b/5c rollout (10%/50%/100%)** | Status-page + changelog post per stage. |

---

## §1 — Pre-canary opt-in invitation

**Subject:** Invitation: pilot our new multi-region deployment (cell-2)

> Hi {customer_name},
>
> We're rolling out our second deployment region (we call it "cell-2") in
> {region_name}. Before opening it to all teams, we'd like 1 customer to
> pilot it for a week — and we think you'd be a great fit because
> {reason: latency, region locality, low-risk workload, etc}.
>
> **What changes for you:**
> - Your deploys will land on cell-2 instead of cell-1.
> - Same `loopzero deploy` command, same dashboards, same SLAs.
> - We'll watch metrics side-by-side with cell-1 for 7 days.
>
> **What we ask of you:**
> - Reply to confirm by {date}.
> - Let us know if you have a planned freeze window in the next 14 days.
>
> **Your control:**
> - Opt out at any time — we re-pin you to cell-1 in under 5 minutes.
> - We'll proactively rollback if any of your SLOs degrade.
>
> Thanks,
> {sender_name}
> {team_email}

---

## §2 — Migration scheduled notification (T-24h)

**Subject:** Heads up: cell-2 migration tomorrow at {timestamp_utc}

> Hi {customer_name},
>
> Tomorrow at {timestamp_utc}, we'll re-pin your tenant `{tenant_id}` to
> cell-2. The change is invisible to your apps — same domains, same TLS,
> same `loopzero` CLI commands.
>
> **What you'll see:**
> - One brief deploy event (under 60s) when we pin.
> - No HTTP 5xx, no DNS changes, no domain reissuing.
>
> **If anything looks off:**
> - Reach us at {oncall_channel}.
> - Or run `loopzero deploy --cell cell-1 {tenant_id}` to roll back yourself.
>
> Thanks,
> {sender_name}

---

## §3 — Migration completion + observability access

**Subject:** Done: your tenant is now on cell-2

> Hi {customer_name},
>
> Your tenant `{tenant_id}` is now pinned to cell-2 (since
> {timestamp_utc}). Quick numbers from the first hour:
>
> - p50 deploy latency: {value}ms (cell-1 baseline: {baseline}ms)
> - p99 request latency: {value}ms
> - Error rate: {pct}%
> - Pods running: {count}
>
> **Your dashboard** (filtered to your tenant):
> {grafana_link}
>
> Daily updates start tomorrow. We'll send a one-line "all green" or a
> detailed breakdown if anything diverges from cell-1.
>
> Thanks for piloting,
> {sender_name}

---

## §4 — Daily soak update (days 1-7)

**Subject:** Day {N}/7 cell-2 canary — {one_word_status}

> Hi {customer_name},
>
> Day {N} of 7, all clear. Numbers held within {%} of cell-1 baseline:
>
> | Metric | Cell-1 | Cell-2 | Delta |
> |---|---|---|---|
> | p99 request latency | {x}ms | {y}ms | {±%} |
> | Error rate | {x}% | {y}% | {±%} |
> | Deploy success rate | {x}% | {y}% | {±%} |
>
> No incidents. No EB consumption. We'll continue.
>
> {sender_name}

---

## §5 — Stage 4 success + thank-you

**Subject:** 7-day cell-2 canary GREEN — thank you for piloting

> Hi {customer_name},
>
> Your 7-day pilot on cell-2 wrapped today. All your SLOs stayed green.
> No EB consumption.
>
> What's next:
> - You stay on cell-2 (no re-pin needed).
> - We start stage 5a (10% of dev tenants on cell-2) on {date_utc}.
> - Production-wide rollout (stages 5b/5c) follows in {timeline}.
>
> Quick favor: a 1-line testimonial we can share when we promote cell-2 to
> all customers? Optional, no obligation.
>
> Thanks again,
> {sender_name}

---

## §6 — Incident notification (P1/P2 during canary)

**Subject:** [P{1|2}] cell-2 canary — investigating

> Hi {customer_name},
>
> We're seeing {symptom} on cell-2 affecting your tenant since
> {timestamp_utc}. Severity: {P1|P2}. Status page: {link}.
>
> **What we're doing right now:**
> - {action_1}
> - {action_2}
>
> **What we may do:**
> - If {threshold} is crossed, we'll roll you back to cell-1
>   (`loopzero deploy --cell cell-1 {tenant_id}`). ETA <5 min.
> - We'll send a follow-up within {time}.
>
> {oncall_engineer} ({oncall_channel})

---

## §7 — Rollback to cell-1 notification

**Subject:** Rolled you back to cell-1 — services stable

> Hi {customer_name},
>
> We rolled `{tenant_id}` back to cell-1 at {timestamp_utc} after
> {symptom}. Your services are stable on cell-1 now.
>
> **What happened (preliminary):**
> {one_paragraph_summary}
>
> **What's next:**
> - Full RCA within 5 business days.
> - We'll re-evaluate cell-2 stability before any second attempt.
> - You're not on the hook for re-piloting.
>
> {sender_name}

---

## §8 — Stage 5a/5b/5c rollout (status-page + changelog)

**Status page entry:**

> **{date_utc} — Cell-2 rollout: {N}% of tenants**
> We've migrated {N}% of tenants to our new cell-2 deployment region.
> Customer-visible behavior is unchanged. SLOs match cell-1 within {%}.
> Status: GREEN. Next: {next_milestone}.

**Changelog entry (format keep-a-changelog):**

```markdown
### Added
- Multi-cell deployment (cell-2 active) — {N}% of tenants now served by cell-2.
  Same `loopzero deploy` command, same SLAs. Tenant pinning is automatic;
  use `loopzero admin tenants pin --tenant <id> --cell cell-{1|2}` to override.
  (CHG-2026-04-30-INFRA-NORMALIZE)
```

---

## Send-list checklist (per template)

Before any of §1-§7 leaves the building:

- [ ] Sender from customer-success / on-call rotation (not engineering by default)
- [ ] All `{placeholders}` replaced with actual values
- [ ] Customer's primary contact pulled from CRM (not email scraped)
- [ ] BCC `infra-canary@loopzero.example` for audit trail
- [ ] If P1: also page on-call manager + post in `#incident-{date}`
- [ ] Status page entry (§8) reviewed by 2 humans before publishing

---

## Versioning

Templates v1.0 — 2026-04-30. Maintained by infra team. Changes require:
1. Update placeholders + tone for current product surface.
2. Customer-success sign-off.
3. Bump version + add row in CHANGELOG.md.
