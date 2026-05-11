---
claim_id: example-future
dimension: dx-cli
score_impact: 0.4
evidence:
  - type: T1
    ref: commit:future:test.go
  - type: T2
    ref: live:2099-01-01:future.log
  - type: T3
    ref: ci:future.yml:1
ttl_days: 30
last_validated: 2099-01-01
expires: 2099-02-01
---
