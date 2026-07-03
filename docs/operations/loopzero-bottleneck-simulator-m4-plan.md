# Plan — LoopZero Bottleneck Simulator M4 (cost-coupled review)

> Slug: `loopzero-bottleneck-simulator-m4`. Builds on M0-M3 engine. FAANG-level, no fabrication.
> Couple the bottleneck simulator with the cost model so `capsim_review`/`capsim_compare` answer
> resilience AND $ per region posture — the decision the AWS re-platform must make.

## Goal

M3 says "active-active is more resilient" but not the cost. M4 adds a grounded `cost(scenario)` and
a `comparePostures()` table so the tool reports, per posture: monthly $, region blast radius, SLA,
review score, and the **cost of resilience** (Δ$ vs single-region). Cost is driven by the engine's
**authoritative capacity** (maxPerCell=500 → derived cells) priced with the cost calculator's
**Vantage RI unit prices** — a consistency guard asserts the engine prices == calculator prices.

## Baseline Context (grounded — DISCOVER of aws-cost-calculator.html)

| Unit price ($/mo) | Value | Calc line |
|---|---|---|
| EC2 node (m8g.2xlarge RI) | 173 | ITEMS `ec2` |
| EKS cluster | 73 | `eks` |
| RDS memory/rag (m8g.2xlarge) | 160 ea | `rdsMem`/`rdsRag` |
| RDS lens (m8g.xlarge) | 80 | `rdsLens` |
| RDS promptly/trust (m8g.large) | 40 ea | `rdsProm`/`rdsTrust` |
| RDS Proxy | 50 | `rdsProxy` |
| ElastiCache r7g.large (Valkey) | 128 | `cache` |
| ALB / NAT GW / WAF / R53 / KMS / SM | 60 / 33 / 1300 / 0.90 / 1.10 / 0.45 | resp. ITEMS |
| S3 / ECR / CF egress / CF req / RDS storage / NAT GB | 0.023 / 0.10 / 0.085 / 0.75 / 0.115 / 0.045 | resp. ITEMS |

Node overhead: `overheadPerCell=2`, `fixedNodes=10` (calc defaults). **Divergence found:** the
calculator's manual `cells=25` violates `maxPerCell=500`; M4 uses the engine's derived cells.

## Cost model (grounded prices × engine capacity)

`totalNodes = appNodes + cells·overheadPerCell + fixedNodes` (cells = ceil(tenants/maxPerCell)).

| Bucket | Contents | Region scaling |
|---|---|---|
| **compute** | `ec2Node·totalNodes + eksCluster·cells` | sharded: ×1 (tenants split, total same); active-active: ×regions (replicated) |
| **controlPlane** | `eksCluster·2 + Σ RDS + rdsProxy·6 + cache·3 + alb·2 + nat·3` | ×regions for BOTH sharded & active-active (each region needs its own) |
| **global** | RDS storage + S3 + ECR + CF egress/req + WAF + R53 + KMS + SM + NAT GB | ×1 (global/shared/traffic) |

`cost(scenario)` = `global + compute·computeMult + controlPlane·controlMult` where
`computeMult = replication ? regions : 1`, `controlMult = regions>1 ? regions : 1`.
⇒ single < sharded (+controlPlane·(R−1)) < active-active (+compute·(R−1) more). The two Δs are the
**cost of blast-radius reduction** and the **cost of full failover**.

## Tasks
- **T1 — `cost()` in engine (TDD).** PRICES constant (cited) + bucket model + region scaling. RED first.
- **T2 — `comparePostures()`.** 3-row table {posture, costPerMonth, blastRadiusPct, sla, reviewScore,
  costOfResilienceVsSingle}. `review()` metrics gains `costPerMonth`.
- **T3 — MCP tools.** `capsim_cost` + `capsim_compare`; schema; integration test.
- **T4 — UI.** Cost KPI + posture-comparison table.
- **T5 — REVIEW.** Independent adversarial review.

## Acceptance Criteria / DoD (all evidenced 100%)
1. **No regression** — all prior tests green (exit 0; M4 → **42 engine + 8 MCP**).
2. **Keyed consistency guard** — a test parses each calculator `ITEMS` row by `id` and asserts the
   engine's `PRICES` **and** `COST_QTY` equal that item's `unitDef`/`qtyDef` **positionally** (not
   mere membership — an on-demand label like "on-dem 262" cannot false-pass; a negative control
   asserts the parser reads the unitDef column, not the label). Any per-item mismatch FAILS.
3. `cost()` grounded + deterministic: default (50k) returns finite buckets; `total = compute +
   controlPlane + global` (asserted); no NaN. Documented that engine cells (100) ≠ calc manual (25).
4. Posture cost ordering: single < sharded < active-active (asserted); Δ(sharded−single) ==
   controlPlane·(R−1) and Δ(aa−sharded) == compute·(R−1) (asserted exact).
5. `comparePostures()` returns 3 rows with cost + blastRadius + sla + score + costOfResilience;
   `review().metrics.costPerMonth` present + == `cost(scenario).total` (grounding).
6. MCP `capsim_cost` + `capsim_compare` exposed; passthrough proven; integration test green.
7. CODE-QUALITY: engine + server syntax OK; 0 dead exports; no fabrication. HTML wired + serves 200.

## Test Plan
- Unit (node:test): PRICES-vs-calculator guard, cost buckets, posture ordering + exact Δs, compare
  table shape, review.costPerMonth grounding, no-regression.
- Boundary: regions=1 (computeMult=controlMult=1); regions=2 active-active (compute ×2).
- Integration: MCP capsim_cost/compare respond; capsim_compare active-active cost > single.

## Risks / Drawbacks
- Region cost scaling is a first-order model. **Direction of error:** control-plane is duplicated at
  full single-region size ×regions (OVER-counts sharded/active-active, since a sharded region serving
  1/N tenants likely needs a smaller control plane); compute uses per-region `ceil` (slightly
  UNDER-counts). Traffic terms (CF/NAT-GB) held global; a full multi-region traffic split is deferred.
- **Engine vs calculator divergence (full disclosure).** Engine total $72,566 vs calculator default
  $48,407 (Δ +$24,159). Decomposition: cell overhead is **+$31,425** (engine cells=100 via
  `maxPerCell=500` vs calculator manual `cells=25`), OFFSET **−$7,266** because the engine's DEFAULTS
  also differ from the calculator on four capacity params — `activeFraction 0.08 vs 0.10`,
  `podsPerTenant 1 vs 1.5`, `prefixDelegation false vs true`, `reservePct 0.10 vs 0.15` — yielding
  69 app nodes (279 total) vs the calculator's 111 (171 total). The engine is authoritative (grounded
  in `engine.go:91`); the calculator's manual inputs are the divergent side. Reconciling the
  calculator to the derived model is a calculator-side follow-up (out of scope).

## Unresolved Questions
- (none blocking) — reconciling the calculator's manual cells input to the derived model is a
  calculator-side follow-up, out of this milestone's scope.
