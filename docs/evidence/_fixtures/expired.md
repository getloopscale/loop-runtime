---
claim_id: example-expired
dimension: tests-confidence
score_impact: 0.2
evidence:
  - type: T1
    ref: commit:abc:test.go
  - type: T2
    ref: live:2025-01-01T00:00:00Z:old.log
  - type: T3
    ref: ci:old.yml:1
ttl_days: 30
last_validated: 2025-01-01
expires: 2025-02-01
---
