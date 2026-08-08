---
type: operational-runbook
title: LoopZero Bottleneck Simulator M1 (calibrate + deepen)
---

# Plan — LoopZero Bottleneck Simulator M1 (calibrate + deepen)

> Slug: `loopzero-bottleneck-simulator-m1`. Builds on M0 (`loopzero-capsim.mjs`). FAANG-level, no fabrication.
> Turn ESTIMATE constants into computed/documented facts where a formula exists; make the rest
> honestly calibratable. Add region-level chaos + SLA refinement.

## Goal

Raise the model's credibility from "estimate" toward "documented decision-support" by removing
fabricatable numbers: compute RDS `max_connections` from the real AWS formula per instance class,
model RDS Proxy multiplexing explicitly, add region-level chaos, and refine the SLA floor. The one
number that genuinely needs a load test (`muRps` per-replica throughput) stays a first-class
**calibratable input** with a documented procedure — never a fabricated measurement.

## Baseline Context (grounded)

| Fact | Value | Source |
|---|---|---|
| RDS Postgres max_connections | `LEAST(DBInstanceClassMemory/9531392, 5000)` | AWS RDS default parameter group |
| → db.m8g.2xlarge (32 GiB) | **3604** | computed (32·1024³/9531392) |
| → db.m8g.xlarge (16 GiB) | **1802** | computed |
| → db.m8g.large (8 GiB) | **901** | computed |
| RDS Proxy | pools idle conns; ceiling = concurrent *active* queries ≤ DB max | AWS RDS Proxy docs |
| promptly domain | credential broker, hit per active-agent request, on db.m8g.large | CLAUDE.md (Model B fabric) |
| LoopZero AWS target | single-region cells (per ADR 2026-06-17) → region loss = full outage until DR | ADR |

## Changes (each with its own test)

- **C1 — computed RDS max_connections.** `rdsMaxConns(memGiB)` = the AWS formula. Each RDS domain
  gets `instanceClassMemGiB` instead of a hardcoded `maxConns`. Removes the fabricatable constant.
- **C2 — RDS Proxy multiplex explicit.** `proxyMaxConnPct` (default 100). Read capacity =
  `rdsMaxConns * proxyMaxConnPct/100 * (1 + replicas)`. Documents that client conns >> DB conns.
- **C3 — promptly domain.** Add the credential-broker RDS (db.m8g.large → 901 conns) driven by
  agent RPS. Demonstrates per-class value: promptly can saturate before memory.
- **C4 — rhoOf correctness.** `cap==0 && used>0 => Infinity` (fully saturated), not 0. Needed for
  region chaos; also a latent-correctness fix.
- **C5 — region chaos.** `chaos(s,{killRegion:true})`: single-region target => survivorFactor 0 =>
  zone-distributed capacity 0 => those components down (ρ=Infinity), `capLossPct=1.0`,
  `sla=regionalOutageAvail*100`.
- **C6 — SLA region-outage floor.** `regionalOutageAvail` input (DR posture during a regional
  outage) — labeled POLICY/ESTIMATE, not a hard fact.
- **C7 — calibration.** `muRps`/`holdSec` stay inputs; add a documented calibration procedure
  (below) + a **sensitivity test** proving the model responds monotonically (2× muRps ⇒ ½ ρ).

## Calibration procedure (muRps / holdSec) — operator step, NOT fabricated here

`muRps` (per-replica throughput) and `holdSec` (query hold time) require measurement, not a formula.
To calibrate honestly:
1. Load-test one BFF/Engine replica in isolation (e.g., `k6`/`autocannon`) → record sustained rps at
   target p99 → that is `muRps`.
2. Measure DB query time under load (RDS Performance Insights `db.load` / `mean_exec_time`) → `holdSec`.
3. Plug the measured values into the sliders. The sensitivity test guarantees the model tracks them.

This mirrors LoopZero's own operator-HARD-STOP LIVE pattern: the harness is shipped + tested; the
measured number is an operator action. No synthetic measurement is invented.

## Acceptance Criteria / DoD (all evidenced)

1. **No regression** — all M0 tests still pass. Full suite green (`node --test`, exit 0).
2. C1 — `rdsMaxConns(32)=3604`, `(16)=1802`, `(8)=901` (asserted); no `maxConns:` literal remains
   for a modeled domain (grep-verified).
3. C2 — `proxyMaxConnPct` scales RDS read capacity linearly (asserted).
4. C3 — promptly domain present; under a high-agent-RPS scenario promptly saturates before memory
   (asserted re-rank among RDS domains).
5. C4 — `rhoOf(1,0)===Infinity`, `rhoOf(0,0)===0` (asserted).
6. C5 — `chaos(killRegion)` → `capLossPct===1`, ≥1 component ρ===Infinity, `sla` < base (asserted).
7. C6 — `regionalOutageAvail` drives the region-outage SLA (asserted); labeled POLICY in code.
8. C7 — sensitivity test: doubling `muRps` halves bff ρ (asserted).
9. UI contract still holds (every new field render() reads is returned) + engine stays pure + HTML
   loads over http (HTTP 200). CODE-QUALITY: 0 dead exports, 0 fabrication.

## Test Plan
- Unit (node:test): C1–C7 formulas + region chaos + sensitivity + no-regression (existing 14).
- Boundary/negative: memGiB huge → maxConns capped at 5000; killRegion with 0 tenants (no NaN).
- Integration (manual + curl): HTML region-chaos button, per-domain computed conns, calibration note.

## Risks / Drawbacks
- `regionalOutageAvail` + `muRps`/`holdSec` remain estimates — explicitly labeled; calibration
  procedure documented. Not passed off as measured.
- Single-region assumption — if LoopZero goes multi-region, killRegion needs failover modeling (M2+).

## Unresolved Questions
- (none blocking) — multi-region failover deferred to a later milestone (LoopZero target is single-region today).
