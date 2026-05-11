---
claim_id: example-valid
dimension: core-deploy
score_impact: 0.5
evidence:
  - type: T1
    ref: commit:341181ae:api/internal/services/deploy_test.go
  - type: T2
    ref: live:2026-05-05T18:42:00Z:docs/evidence/core-deploy/2026-05-05-cleanroom-12-12.md
  - type: T3
    ref: ci:cell2-soaks.yml:12345
ttl_days: 30
last_validated: 2026-05-11
expires: 2026-06-10
---

# Fixture: valid three-source claim
