# Golden Harvest Meeting — Iteration 1

**Phase:** 1 (READ)
**Date:** 2026-03-29
**Attendees:** chief-harvester, test-miner, behavior-extractor

## Status
- First iteration — project context established
- Reference: Capsule (CNCF Sandbox, multi-tenancy framework for K8s)
- Target: LoopZero (CLI-first K8s PaaS, custom operators for tenant management)

## Decisions
1. Focus extraction on 3 domains: tenant-isolation, resource-quotas, network-policies
2. Skip Capsule domains not relevant to LoopZero (ingress hostname collision, image pull policy, service metadata)
3. Extract behavioral contracts, not Capsule implementation specifics

## Results
- **21 source tests extracted** (12 tenant-isolation, 6 resource-quotas, 3 network-policies)
- **8 behavioral contracts mapped** (protocol: 3, invariant: 5)
- Test catalog written to harvested/test_catalog.md
- Contract map written to harvested/contract_map.md

## Key Insights from Capsule
1. **Pruning pattern**: Set-difference deletion using label selectors — LoopZero should adopt this
2. **Proportional ResourceQuota**: Dynamic distribution based on (tenantHard - totalUsed + nsUsed)
3. **Deferred status writes**: Always write status conditions via defer, even on error
4. **Metadata tracking**: Store managed labels/annotations in status to enable orphan cleanup

## Next Phase
Advance to Phase 2 (GOLDEN) — normalize 21 source tests into portable golden tests for LoopZero's envtest suite.
