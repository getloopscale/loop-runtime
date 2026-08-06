# SEPA pre-COMMIT — iter 1 / T0.1 / loopzero-contracts
Diff: 2 CostMetric constants + 5 explicit Unlimited cap entries + 1 test. No money-write, no 402, observe-only preserved.
## Findings
- INFO — additive vocabulary only; caps Unlimited on all tiers (verified in diff). Acyclic invariant intact (no engine import). Conventional-commit + T-id + wiring summary present. No Co-Authored-By.
- INFO — pillar (a) defer-to-T1.1 is correct for a contracts constant (production caller is the cross-repo consumer, lands next task).
## Recommended action
- Commit + tag v0.3.0 (annotated). Proceed.
