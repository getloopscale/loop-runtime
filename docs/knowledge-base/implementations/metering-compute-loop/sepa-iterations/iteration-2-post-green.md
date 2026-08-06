---
type: sepa-iteration
title: "SEPA post-green — iter 2 / T1.1"
---

# SEPA post-green — iter 2 / T1.1
MODE=TIGHT. Mechanical: 2 enforcement aliases (re-export plan.Metric*) + 3 .Issue test-arg fixes (v0.2.0 5th param) + go.work adds loopzero-contracts (workspace-dev, go.mod bump deferred to v0.3.0 publish).
## Findings
- INFO — additive aliases mirror existing MetricEgressBytes pattern (DRY-consistent). No money-write. Observe-only intact. .Issue fixes are test-only (no prod caller). go.work coupling reversible + flagged.
- INFO — pillar (a) alias defer-to-T2.3/T3.1 consumer (correct for a re-export).
## Recommended action
- Commit pkg/enforcement + jwt_validator_test + go.work. go.mod stays v0.1.0 (workspace override) — real bump at publish. No CHANGELOG (internal plumbing, not consumer-visible).
