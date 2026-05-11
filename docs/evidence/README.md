# Evidence Files

**Purpose:** This directory holds the canonical evidence per claim referenced in `docs/STATUS.md`. Each evidence file is the human-readable proof of one maturity claim, structured per `docs/governance/evidence-bar.md`.

## Directory Layout

```
docs/evidence/
├── README.md              ← this file (index + convention)
├── _fixtures/             ← validator test fixtures (not real claims)
├── core-deploy/           ← claims for Core Deploy Flow dimension
├── dx-cli/                ← claims for DX / CLI dimension
├── infra-ops/             ← claims for Infrastructure / Operations
├── security/              ← claims for Security
├── scale-mt/              ← claims for Scale / Multi-tenancy
├── product-completeness/  ← claims for Product Completeness
└── tests-confidence/      ← claims for Tests / Confidence
```

Each subdirectory holds `*.md` files, one per claim, with **YAML frontmatter** per the schema in `evidence-bar.md`.

## Naming Convention

`{date}-{claim-slug}.md` ou `{claim-slug}.md` para claims persistentes.

**Examples:**
- `2026-05-05-cleanroom-12-12.md` (dated event)
- `loopzero-deploy-e2e-validated.md` (persistent claim)

Slugs em kebab-case, sem espaços ou acentos.

## File Schema

```yaml
---
claim_id: <kebab-case-slug>          # unique across all evidence files
dimension: <one of 7 maturity dims>
score_impact: <0.0-5.0>               # how much this claim contributes
evidence:
  - type: T1                          # code + test
    ref: commit:<sha>:<test-file-path>
  - type: T2                          # LIVE run log
    ref: live:<iso-timestamp>:<log-path>
  - type: T3                          # CI run
    ref: ci:<workflow-name>:<run-id>
ttl_days: <30|60|90>                  # per decay-policy.md
last_validated: <YYYY-MM-DD>
expires: <YYYY-MM-DD>                 # last_validated + ttl_days
---

# {claim title}

## Why this claim matters
{1-2 paragraphs explaining the claim and its score impact}

## How to revalidate
{exact commands/runs an operator must execute to refresh the evidence}

## Notes / Open concerns
{anything that doesn't fit elsewhere}
```

## Validation

Each file MUST pass `infra/scripts/governance/validate-evidence.sh <file>`. CI gate (`task validate:evidence`) runs this against the whole directory.

**Exit codes:**
- `0` — EVIDENCED (3+ valid sources) or PARTIAL_CONFIDENCE (1-2 sources, not score-eligible)
- `1` — UNEVIDENCED / BROKEN / INDEPENDENCE_VIOLATION (refs not distinct)
- `2` — EXPIRED (TTL vencido — needs revalidation)

## Revalidation Workflow

1. Operator detects expired claim (via decay scan, CI, or audit)
2. Reruns the commands listed in "How to revalidate" section
3. Updates `last_validated` + `expires` fields
4. Commit message: `docs(evidence): revalidate {claim_id} — {evidence-summary}`
5. PR triggers `task validate:evidence` gate

## Cross-References

- `docs/governance/evidence-bar.md` — full policy + tier definitions
- `docs/governance/decay-policy.md` — TTL rules
- `docs/STATUS.md` — claims that consume these evidence files
- `infra/scripts/governance/validate-evidence.sh` — validator

## Anti-Patterns (Forbidden)

- ❌ Evidence file without YAML frontmatter
- ❌ Same `ref` in T1+T2+T3 slots (violates independence)
- ❌ Claim about design ("arquitetura coesa") — design claims belong in `docs/architecture/`, not here
- ❌ Editing an `expires` date without rerunning the revalidation commands
- ❌ Log files > 1MB inline — store in `${HOME}/.loopzero/artifacts/` and reference by checksum
