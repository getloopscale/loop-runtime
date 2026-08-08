---
type: operational-runbook
title: Production Canary Stage 5 Runbook (Customer Trial)
---

# Production Canary Stage 5 Runbook (Customer Trial)

> **Plan reference:** T0.3 + T0.4 of [`docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`](../plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md)
>
> **Status:** Stage 5 = 1 production customer day-trial WITHOUT incidents. Gate (b) of CLAUDE.md "Path to maturity 4.0".

## Purpose

Validar LoopZero end-to-end com 1 customer real durante 24h consecutivas sem incidentes. Esta é a última gate antes de declarar M09 Multi-Cell = 4.0 e formalizar o portfolio em 3.95+ average.

## Sponsor Decision Required

**HARD STOP:** Antes de iniciar Stage 5, Sponsor + Sales devem decidir:

1. **Who is the customer?**
   - Bottom-up funnel (indie dev / early adopter via Pro plan signup)
   - Pre-existing relationship (operator-known dev community contact)
   - Beta opt-in pool

2. **Consent + scope:**
   - Customer acknowledges "early access" status
   - Customer signs SLA template (24h commitment)
   - Customer commits to escalation channel (Slack/email/calendar)

3. **Customer profile:**
   - Workload: deploy + rollback frequency expected (target: ≥ 1 deploy/4h to exercise pipeline)
   - Risk acceptance: production OR staging-equivalent traffic

## SLA Template (24h trial commitment)

> **LoopZeroCloud Early-Access Trial SLA (24h):**
>
> 1. **Availability:** LoopZeroCloud commits to 99.9% uptime over 24h window for deploy + rollback + status endpoints
> 2. **Response time:**
>    - S0 (Blocker): operator-acknowledged within 15min, mitigation within 1h
>    - S1 (Severity 1): acknowledged within 30min, mitigation within 4h
>    - S2-S4: best-effort, no SLA
> 3. **Escalation:** customer has 24/7 channel (Slack DM `@loopzero-oncall` + email `oncall@loopzero.example`)
> 4. **Rollback assistance:** LoopZeroCloud commits to assisted rollback if customer-initiated rollback fails
> 5. **Data:** customer-deployed app data treated per data-handling policy (no retention beyond cleanup)
> 6. **Termination:** customer can terminate trial at any time; resources cleaned within 30min
>
> Trial start: __________ Trial end: 24h later (__________)
> Customer name: __________
> Sponsor signature: __________

## Success Criteria

Day-trial considered SUCCESS quando, durante 24h consecutivas:

- [ ] **Zero S0** (Blocker) incidents
- [ ] **Zero S1** (Severity 1) incidents
- [ ] All customer-initiated deploys succeed dentro de `deploy_latency` SLO (95% within 5min)
- [ ] All customer-initiated rollbacks succeed dentro de `rollback_success` SLO (99.9%)
- [ ] Custom domain TLS provisioning successful se requested (`domain_provisioning` SLO 99%)
- [ ] Zero `loopzero_pg_circuit_breaker_open_total` triggers
- [ ] Zero internal-terms leakage em CLI output recebido pelo customer (Rule 3 check)
- [ ] Zero Loki ERROR rate spike > 10× baseline durante window
- [ ] Zero PagerDuty fires específicos do customer

## Pre-Trial Checklist (Operator)

- [ ] Stage 4 GREEN per [`production-canary-stage4-runbook.md`](./production-canary-stage4-runbook.md) (100% promote sem rollback)
- [ ] 24h soak post-Stage-4 com zero incidents
- [ ] Customer tenant provisioned via `loopzero admin tenants create`
- [ ] Customer onboarded via `loopzero login` + `loopzero project create`
- [ ] Customer billing record created (Stripe Customer or free tier)
- [ ] Operator oncall schedule confirmado (24h coverage)
- [ ] Customer comms channel setup (Slack channel `customer-<name>-trial`)
- [ ] Loki saved view criada: `{customer="<name>"} | json`
- [ ] Prometheus dashboard imported para customer scope
- [ ] Rollback script ready em case of emergency

## Trial Execution

### Hour 0 — Kickoff

```bash
# Mark trial start
psql -c "INSERT INTO audit_log (action, actor, target, metadata) VALUES (
  'customer_trial_start',
  '<operator>',
  '<customer-tenant>',
  '{\"trial_id\":\"<uuid>\",\"sla_signed\":true,\"start_ts\":\"NOW()\"}'
)"

# Notify customer via Slack/email: "Trial started. Channel: customer-<name>-trial"
```

### Hour 0 - 24 — Monitor

Continuous monitoring:
- Operator checks dashboard every 2h
- Slack alert on any S0/S1 event triggers immediate response
- Loki query saved view monitored
- Customer-initiated actions tracked in `audit_log`

```bash
# Real-time customer activity feed
psql -c "SELECT action, timestamp, metadata FROM audit_log
         WHERE target LIKE '<customer-tenant>%'
         AND timestamp > NOW() - INTERVAL '24 hours'
         ORDER BY timestamp DESC LIMIT 50"
```

### Hour 24 — Closeout

```bash
# Generate trial report
loopzero admin tenants report --tenant <customer> --since "24h ago" --json > trial-report.json

# Mark trial complete
psql -c "INSERT INTO audit_log (action, actor, target, metadata) VALUES (
  'customer_trial_complete',
  '<operator>',
  '<customer-tenant>',
  '{\"trial_id\":\"<uuid>\",\"s0_count\":0,\"s1_count\":0,\"verdict\":\"PASS\"}'
)"

# Customer feedback survey sent
# Operator decision: extend trial / promote to Pro / cleanup
```

## Escalation Path

Incident → operator triages → if S0/S1:

1. **Operator** (oncall) — first responder
2. **SRE** (escalation if mitigation requires infrastructure) — 30min response
3. **Sponsor** (escalation if customer-facing communication required) — 1h response

Communication template para customer:
> "Hi {customer}, we detected an issue on your account at {time}. We are actively working to resolve it. ETA: {time+1h}. We will keep you posted every 15min."

## Cleanup After Trial

```bash
# Customer wants to keep trial → promote
loopzero admin tenants update --tenant <customer> --plan pro

# Customer ends trial → cleanup
loopzero admin tenants delete --tenant <customer> --grace 7d
```

`grace 7d` retains customer data 7 days for recovery; auto-deletes thereafter.

## Failure Modes

| Sintoma | Mitigação |
|---|---|
| Customer reports "deploy stuck" | `loopzero status --build <id>`; if stuck > 30min, cancel + redeploy |
| TLS cert provisioning timeout | Check `cert-manager.io/v1/Certificate` status; manual renewal if needed |
| Customer DNS records conflict | Use `loopzero domain dns --conflict-resolve` (deletes A/AAAA at apex) |
| Customer hits plan cap (404 PROJECT_LIMIT) | Upgrade plan via admin OR explain to customer + ask if want upgrade |
| PG circuit breaker opens | Confirm not customer-induced; if customer-induced, rate-limit customer |

## After-Action Report

Post-trial, operator writes `.claude/knowledge-base/reviews/2026-XX-XX-customer-trial-<name>.md`:
- Customer profile
- Trial timeline (deploys, rollbacks, incidents)
- Issues encountered + mitigation
- Customer feedback
- Decision: M09 Multi-Cell → 4.0 (if PASS) OR re-iterate (if FAIL)

## Related

- ADR-0008 Multi-Cell + ADR-0021 Sticky placement
- [`production-canary-stage4-runbook.md`](./production-canary-stage4-runbook.md) (predecessor stage)
- CLAUDE.md "Path to maturity 4.0" gate (b)
- MEET-2026-05-12 (pricing decisions impacting trial scope)
