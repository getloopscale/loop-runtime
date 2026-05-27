# Customer Trial Onboarding Plan

> **Plan reference:** T0.4 of `docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Status:** Planning document. Customer trial execution per `production-canary-stage5-runbook.md`.

## Purpose

Document **what needs to be decided** by Sponsor + Sales before 1st customer trial executes. This is NOT the trial procedure — it's the prerequisite decisions matrix.

## Decisions Required

| # | Decision | Owner | Status |
|---|---|---|---|
| 1 | Who is the customer? | Sponsor + Sales | OPEN |
| 2 | Consent process (NDA? signed SLA?) | Sponsor + Legal | OPEN |
| 3 | Trial scope (production OR staging-equivalent?) | Sponsor + Customer | OPEN |
| 4 | Customer plan during trial (Free, Pro, Team comped?) | Sponsor | OPEN |
| 5 | Trial duration (24h fixed OR open-ended?) | Sponsor | OPEN |
| 6 | Success criteria (zero S0/S1 vs SLO thresholds?) | SRE + Sponsor | OPEN |
| 7 | Failure → cleanup procedure (immediate vs grace?) | Operator | OPEN |
| 8 | Customer feedback collection method | Product | OPEN |
| 9 | Communications playbook (Slack? email? phone?) | Operator + Customer | OPEN |
| 10 | Escalation tiers (operator → SRE → sponsor latencies) | SRE + Sponsor | OPEN |
| 11 | Post-trial decision tree (PASS → 4.0 mark; FAIL → re-iterate) | Sponsor | OPEN |
| 12 | Public disclosure (case study? blog post?) | Marketing + Customer | OPEN |

## Candidate Customer Profiles

### Profile A: Indie Dev (bottom-up funnel)

- **Source:** Pro plan signup or Hackernews/Discord referral
- **Workload:** simple Node/Go API, 1-2 services, light traffic (≤ 1k req/day)
- **Risk:** LOW — customer accustomed to "early access" rough edges
- **Reward:** validates core deploy + rollback flow under real conditions
- **Trial fit:** Best for Stage 5 first run

### Profile B: Pre-existing relationship

- **Source:** Operator personal/professional network
- **Workload:** known fit (operator briefed)
- **Risk:** MEDIUM — reputation risk if trial fails
- **Reward:** strong post-trial feedback (technical depth)
- **Trial fit:** Good for Stage 5 second run

### Profile C: Beta opt-in pool (future)

- **Source:** Landing page "Join beta" form
- **Workload:** unknown, could be complex
- **Risk:** HIGH — no profile vetting
- **Reward:** larger pool, public validation
- **Trial fit:** v1.1 candidate (after Stage 5 first run validates infrastructure)

**Recommendation:** Profile A → first trial. Profile B → second trial.

## SLA Template

See `production-canary-stage5-runbook.md` § "SLA Template". Sponsor must:
- Review template
- Customize as needed (specific customer commitments)
- Sign + obtain customer signature
- Store em `.claude/knowledge-base/reviews/contracts/<trial-id>.md`

## Success Criteria Matrix

Operator + SRE alignment:

| Metric | Threshold | Source | Rationale |
|---|---|---|---|
| S0 incidents | 0 | PagerDuty + audit_log | Blocker = trial FAIL |
| S1 incidents | 0 | PagerDuty + Loki | Severity 1 = trial FAIL |
| Deploy success rate | ≥ 95% within 5min | `loopzero_deploy_success_total` | Aligns CTO Rule 5 |
| Rollback success rate | ≥ 99.9% | `loopzero_rollback_success_total` | Customer must be able to undo |
| Custom domain TLS | ≥ 99% if requested | `loopzero_domain_provisioning_total` | Vercel-parity expectation |
| PG circuit breaker | 0 trips | `loopzero_pg_circuit_breaker_open_total` | Healthy DB throughput |
| Internal-terms leak | 0 occurrences | Loki regex search CLI output | Rule 3 strict |
| Customer NPS | ≥ 7 | Post-trial survey | Adoption signal |

## Escalation Tiers + SLAs

| Tier | Who | Response SLA | Mitigation SLA |
|---|---|---|---|
| 1 | Operator (oncall) | 15min S0, 30min S1 | 1h S0, 4h S1 |
| 2 | SRE (if infra mitigation) | 30min | 2h S0 |
| 3 | Sponsor (customer comms) | 1h | N/A (decision-maker) |

Tier 3 invoked when:
- Customer-facing communication needed (apology, refund, trial extension)
- Decision to abort trial early
- Public disclosure required

## Cleanup Decision Tree

After 24h trial:

```
Trial verdict?
├─ PASS → Customer wants Pro plan? ─┬─ YES → Promote to Pro, document case study
│                                    └─ NO → Cleanup with 7d grace
├─ FAIL → Document root cause ──────┬─ Fixable in 1 week? → Re-run trial after fix
│                                    └─ Architectural fix? → Defer Stage 5; iterate
└─ INCONCLUSIVE → Extend trial 48h → re-evaluate
```

## Comms Playbook

Customer-facing templates (refine per customer):

**Trial start (Hour 0):**
> "Hi {customer}, your 24h LoopZeroCloud trial is now active. Here's your dashboard: {url}. Your dedicated channel: #customer-{name}-trial. Your oncall contact: @loopzero-oncall. Looking forward to your feedback!"

**Issue notification:**
> "Hi {customer}, we detected an issue affecting your account at {time}: {brief description}. We are actively investigating. ETA for first update: {time+15min}."

**Issue resolved:**
> "Hi {customer}, the issue from {time} is now resolved. Root cause: {brief}. Impact on your workloads: {brief}. Sorry for the disruption — we will follow up with a written post-mortem within 48h."

**Trial closeout:**
> "Hi {customer}, your 24h trial ends in {time}. Summary: {deploy_count} deploys, {rollback_count} rollbacks, {incident_count} incidents. We'd love your feedback — survey link: {url}. Want to continue with Pro plan? Reply here."

## Public Disclosure Decision

Post-trial, Sponsor + Marketing decide:

| Option | Pros | Cons |
|---|---|---|
| Case study (blog post) | Marketing asset; social proof | Requires customer consent + review cycles |
| Tweet / social post | Fast; lightweight | Limited depth |
| Quiet internal | Zero PR risk | No external value |

Default: **quiet internal** for first trial. **Case study** for second trial (post Profile B success).

## Open Questions for Sponsor

1. Budget for trial credits (Stripe comp)?
2. Legal review needed for SLA template?
3. Sales involvement in customer selection?
4. Marketing involvement post-trial?
5. Customer support headcount during trial window (1 operator suficiente?)?

## Reference

- `production-canary-stage5-runbook.md` (execution procedure)
- CLAUDE.md "Path to maturity 4.0" gate (b)
- MEET-2026-05-12 (pricing decisions D4 — Pro public, Team Early Access)
