# SEPA pre-RED — iter 1 / T0.1 / loopzero-contracts
MODE=TIGHT. Initial brief already covered T0.1 (the v0.2.0→v0.3.0 tag risk, folded into plan v1.7).
## Findings
- INFO — additive-only: 2 CostMetric constants + explicit Unlimited caps. No money-write path (Phase 0 vocabulary). Observe-only preserved (caps Unlimited). No new concern vs initial brief.
## Recommended action
- Proceed. Tag v0.3.0 (NOT v0.2.0 — released) at COMMIT. Keep the acyclic invariant (contracts imports nothing engine-internal).
