---
claim_id: <kebab-case-slug>
dimension: <one of: core-deploy|dx-cli|infra-ops|security|scale-mt|product-completeness|tests-confidence>
score_impact: 0.0
evidence:
  - type: T1
    ref: commit:<10-char-sha>:<test-file-path>
  - type: T2
    ref: live:<ISO-timestamp>:<log-path>
  - type: T3
    ref: ci:<workflow-name>:<run-id>
ttl_days: 30
last_validated: YYYY-MM-DD
expires: YYYY-MM-DD
---

# Claim: <one-sentence statement>

## Why this claim matters

<1-2 paragraphs explaining the claim's score impact + customer-facing relevance>

## How to revalidate

```bash
# Exact commands an operator runs to refresh the evidence
task <revalidation:command>
# Verify expected output (paste relevant lines)
# Update frontmatter last_validated + expires
```

## Evidence detail

### T1 — Code + Test
- Commit: `<sha>` ([link](<github-url>))
- Test file: `<path>`
- Test name(s): `<TestXxx, TestYyy>`
- What the test asserts: <1-2 lines>

### T2 — LIVE Run
- Date: `<YYYY-MM-DD HH:MM:SS UTC>`
- Log location: `<path or s3 URL>` (checksum: `sha256:<hex>` if >1MB)
- Command executed: `<exact-command>`
- Output highlights: <relevant lines>

### T3 — CI Run
- Workflow: `<workflow-file-name.yml>`
- Run ID: `<github-run-id>` ([link](<github-actions-url>))
- Status: success (verify with `gh run view <run-id> --json conclusion`)

## Notes / Open concerns

<anything that doesn't fit above — caveats, future work, known limitations>

## Cross-references

- Plan / sprint that produced this claim:
- Related ADRs:
- Related runbooks:
