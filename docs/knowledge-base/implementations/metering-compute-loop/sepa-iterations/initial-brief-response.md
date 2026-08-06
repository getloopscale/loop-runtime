# SEPA Initial Brief — metering-compute-loop v1.6 (2026-07-01, pre-flight)

See full brief in the /implement launch transcript. HEADLINE [CRITICAL]:
v0.2.0 is ALREADY a released loopzero-contracts tag (2026-06-27, jwt AccountID feature).
The plan's "tag v0.2.0 / consume v0.2.0" is a STALE version number — the compute
metrics must go on the next free additive minor. Plus: go.mod pins v0.1.0, so the
bump pulls in v0.2.0's BREAKING SessionTokenIssuer.Issue 5th-param change → .Issue()
caller fan-out at T1.1. Highest-risk tasks: T2.3 (SC-1/SC-2/SC-6 in-CTE), T3.1 (SC-3
short-circuit + label-free attribution), T2.1 (metering_cursor PK no-period = SC-4 lock).
T6.1 LIVE half = honest BLOCKED (sponsor JUNTOS).
