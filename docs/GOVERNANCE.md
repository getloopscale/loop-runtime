# Governance — LoopZero Platform

**Status:** Canonical policy document. Consolidates evidence-bar + decay-policy + bias-taxonomy + adversarial-personas + scoring-methodology.
**TTL:** 90 days (strategic; revalidate quarterly)
**Last validated:** 2026-05-11
**Related:** [`docs/STATUS.md`](STATUS.md) (current scores) · [`docs/meetings/adr/0051-claim-evidence-decay-contract.md`](meetings/adr/0051-claim-evidence-decay-contract.md) (canonical ADR) · [`docs/architecture/governance/`](architecture/governance/) (diagrams + AS-IS/PROPOSED)

---

## Purpose

This document defines **how LoopZero claims its maturity status**. Every score-affecting claim in `docs/STATUS.md` must follow these rules: 3 independent evidence sources, TTL-driven decay, 3-persona adversarial triangulation, 6 bias patterns actively defended against, pre-merge gate enforcement.

Per ADR-0051 (the immutable historic record), this contract exists because the 2026-05-11 working session demonstrated empirically that **awareness-only defenses fail** (+0.4 agent drift in 30 min observed). Material defenses (scripts, gates, CI) are the only path forward.

---

## §1. Evidence Bar — 3-Tier Schema

Every score-affecting claim requires **3 evidence sources with distinct refs**:

| Tier | Definition | Reference format |
|------|------------|------------------|
| **T1 — Code + Test** | Commit SHA + test file that verifies the behavior | `commit:<10-char-sha>:<test-file-path>` |
| **T2 — LIVE Run Log** | ISO timestamp + archived log proving execution against real infra | `live:<iso-timestamp>:<log-path>` |
| **T3 — CI Run** | GitHub Actions workflow + run ID with conclusion=success | `ci:<workflow-name>:<run-id>` |

**Independence requirement (EC-6):** The 3 refs MUST be **distinct**. Same ref in 2+ slots violates independence. `validate-evidence.sh` rejects with `EVIDENCE_REFS_NOT_INDEPENDENT`.

### Edge cases rejected per tier

| Tier | Rejected pattern | Classified as |
|------|-----------------|---------------|
| T1 | Test exists but `t.Skip()` or `@skip` | BROKEN (not T1) |
| T1 | Commit force-pushed (not in history) | BROKEN |
| T1 | Test file unmodified in commit (retroactive claim) | Reclassify — use commit-of-test, not commit-of-feature |
| T2 | Log file > 1MB without SHA256 checksum | BROKEN (must add checksum) |
| T2 | Log without parseable timestamp | BROKEN |
| T2 | Mocked simulation or unit test log | Not T2 (is T1) |
| T3 | Run conclusion: cancelled / failure | Doesn't count |
| T3 | Run ID invalid or PR closed | BROKEN |
| T3 | Workflow scheduled but never executed | Doesn't count |

### Classification output

After `validate-evidence.sh`:

| Label | Criterion | Treatment |
|-------|-----------|-----------|
| `EVIDENCED` | 3 distinct refs T1/T2/T3, all valid, TTL not expired | Counts for score |
| `PARTIAL_CONFIDENCE` | 2 distinct valid refs | Citable with asterisk; NOT score-eligible |
| `UNEVIDENCED` | 0-1 valid refs | Excluded from STATUS.md |
| `BROKEN` | Refs point to non-existent artifacts | Blocks merge if in STATUS.md |
| `EXPIRED` | TTL passed | NEEDS REVALIDATION; alert opens issue |

### Schema for evidence file frontmatter

```yaml
---
claim_id: <kebab-case-slug>
dimension: <one of: core-deploy|dx-cli|infra-ops|security|scale-mt|product-completeness|tests-confidence>
score_impact: <0.0-5.0>
evidence:
  - type: T1
    ref: commit:<sha>:<path>
  - type: T2
    ref: live:<iso-timestamp>:<path>
  - type: T3
    ref: ci:<workflow>:<run-id>
ttl_days: <30|60|90>
last_validated: <YYYY-MM-DD>
expires: <YYYY-MM-DD>  # last_validated + ttl_days
---
```

### Confidence math

3 independent sources, p≥0.9 each → loopzeroretical `1 − (0.1)³ = 99.9%`. **Realistic cap: 95-97%** considering:
- Fixture overlap between tests and LIVE runs (correlation ~0.2)
- CI re-invokes same tests in different env (correlation ~0.3)
- Same-model-family adversarial reviewers share training-data bias (correlation ~0.1-0.2)

For 99% real, exigir cross-vendor model em adversarial review (codex + Claude diversity).

### Design claims (non-testable)

Claims sobre design ("arquitetura coesa", "boundaries limpos") **NÃO vão para STATUS.md**. Eles pertencem a `docs/architecture/`. STATUS.md carrega apenas claims testáveis.

---

## §2. Decay Policy — TTL Tiers

Toda claim tem **TTL visível**. Sem TTL, claims viram folclore.

### Tier 1 — Operational (30 days)

- Test counts (`1210+ CLI tests`)
- LIVE validations (cleanroom 12/12, journey 80/80)
- Journey results (dogfood pass rates)
- Metric counts (`43+ loopzero_* metrics`)
- Sprint progress claims (X/Y tasks)

**Rationale:** Esses números mudam toda semana. 30 dias é limite onde claim ainda é "approximately true".

### Tier 2 — Architectural (60 days)

- State machine wired (11 state machines em `pkg/statemachine/`)
- CRD reconcile flows (8 controllers, 10 CRDs)
- Plan tier capabilities (Starter 2 replicas, Pro 5, etc.)
- Helm chart inventory (32 bootstrap + 13 published)
- Multi-cell topology (3 cells, ClusterCredential per cell)

**Rationale:** Arquitetura muda por sprint. 60 dias = 1 sprint pleno antes de revalidação.

### Tier 3 — Strategic (90 days)

- ADRs (decisões arquiteturais)
- Bias taxonomy
- Evidence bar policy
- Decay policy (recursivo)
- Persona definitions
- Sprint plan high-level

**Rationale:** Decisões estratégicas mudam por trimestre.

### TTL override (explicit)

`ttl_override: <days>` no frontmatter para extender (ex: 180d) **se houver ADR-link justificando**.

```yaml
ttl_days: 30
ttl_override: 180
ttl_override_adr: docs/meetings/adr/00XX-extended-ttl-for-Y.md
```

Validator rejeita `ttl_override` sem `ttl_override_adr`.

### Decay lifecycle

```
Created → Fresh → Aging → Expiring → Expired → Revalidated → Fresh
                                       │
                                       └─ (>30d sem revalidar) → Stale Folklore (quarantine)
```

| State | Definition | Action |
|-------|------------|--------|
| Fresh | `today < last_validated + 0.5 × ttl_days` | None |
| Aging | `0.5 × ttl < days_since_validated < 0.8 × ttl` | None (informational) |
| Expiring | `0.8 × ttl ≤ days_since_validated < ttl` | Yellow flag |
| Expired | `days_since_validated ≥ ttl` | Red flag — open issue, block STATUS.md |
| Revalidated | Operator reran + updated `last_validated` | Returns to Fresh |
| Stale Folklore | Expired + 30d sem revalidação | Quarantine → `docs/evidence/_archived/` |

### Revalidation protocol

Toda evidence file MUST ter section **"How to revalidate"** com comandos exatos:

```markdown
## How to revalidate
1. Run: `task dogfood:journey-04` against fresh cleanroom
2. Verify: 12/12 journeys PASS in logs
3. Update frontmatter `last_validated` to today's date
4. Recompute `expires = last_validated + ttl_days`
5. Commit: `docs(evidence): revalidate {claim-id} — {summary}`
```

### CI integration

`.github/workflows/decay-scan.yml` runs Sundays 02:00 UTC. On expired claims: opens/updates GitHub issue. Notify via Slack composite action when `SLACK_WEBHOOK_URL` configured.

**Visualização:** `docs/architecture/governance/concept-decay-lifecycle.excalidraw`

---

## §3. Bias Taxonomy — 6 Patterns + Active Defenses

A conversa 2026-05-11 demonstrou que **awareness implícita não funciona**. Defesa = codificar padrões + script que detecta. `bias-check.sh` flags severity 0-3.

### Inter-bias causation

```
self-attestation → counting → aggregation → STATUS DRIFT
                       ↓
                  recency masks
                       ↓
                lag preserves status quo
                       ↓
            "implemented but not wired" undetected
```

Self-attestation é root cause. Outros 5 são amplifiers.

### Pattern 1 — Counting Bias

**Def:** Contar items completos (✅) sem distinguir "código pronto" vs "validado LIVE" vs "dogfood passed".

**Examples:**
- `"29/30 ✅"` em plan progress sem evidence link
- `"83/83 PASS"` em journey sem coverage %
- `"All tasks completed"` em DoD sem validar testability

**Detection:** Regex `\d+/\d+ (?:✅|DONE|GREEN|PASS)` sem evidence link adjacente. Heurística: ratio ≥0.9 sem link → flag MEDIUM.

**Counter-defense:** `validate-evidence.sh` exige 3 distinct refs.

### Pattern 2 — Aggregation Bias

**Def:** Reportar média sem decomposition. Média esconde outliers — dimensão alta mascara baixa.

**Examples:**
- `"Score 3.9/5.0"` sem listar 7 dims individuais
- Média 3.79 reportada como 3.9 (round-up)
- "Customer satisfaction 4.5" sem distinguir tier

**Detection:** `(?:média|average|score)\s+\d\.\d` sem 3+ dims enumeradas próximas.

**Counter-defense:** STATUS.md §2 = 7 rows independentes; média é informativa, score per dim é canonical.

### Pattern 3 — Recency Bias

**Def:** Claims dos últimos 7-14 dias dominam narrative. Gaps antigos invisíveis.

**Examples:**
- "Build mode dispatch DONE (2026-05-08)" em CLAUDE.md 5x
- "AppSecret CRD unwired" mencionado 1x
- "10 failing tests" mencionado 1x

**Detection:** >50% das menções recentes (≤14d). Imbalance entre sucessos e gaps em "Known issues".

**Counter-defense:** Decay TTL força revalidation. STATUS.md "Open Gaps" section mandatory (não buried).

### Pattern 4 — Self-Attestation Bias (ROOT CAUSE)

**Def:** Autor da claim = juiz da claim. Sem reviewer independente, bias preservado.

**Examples:**
- Maturity assessment escrito pelo team que construiu
- Plan submission com DoD marcado pelo author
- Score em pitch deck sem revisão externa

**Detection:** commit author == claim author (via git blame em STATUS.md). 0 reviewers diferentes em score-affecting PR.

**Counter-defense:** Pre-merge gate (§6) exige `evidence-verified` label por author diferente. T3.1 adversarial personas (§4).

### Pattern 5 — Documentation Lag Bias

**Def:** Claim cita referência stale (>30d) sem revalidação, mas é tratada como current.

**Examples:**
- `maturity-assessment.md` (2026-05-03) ainda citado 2026-05-11 sem update
- Runbook 2026-02 referenciado para procedure em 2026-05
- "Test count 1210+" reportado quando real é 1456

**Detection:** Cross-reference com decay scan. Heurística: file modified-date > X dias sem `last_validated` field.

**Counter-defense:** Decay scanner abre issue. TTL visible em toda evidence file.

### Pattern 6 — "Implemented but Not Wired"

**Def:** Código existe mas não conectado ao caminho de produção. Documentado em CLAUDE.md Known Systemic Pattern #1.

**Examples:**
- `AppSecret` CRD definido mas controller não implementado
- `ScalingResolver` antes de B40: existia, mas Submit não invocava
- Funções com 0 callers em call graph mas docs dizem "shipped"

**Detection:** Cross-reference com `/dead-code` skill output. Grep `strings.Contains(err.Error(), ...)` callers.

**Counter-defense:** T2.2 do plano de validação roda `/dead-code`. STATUS.md claims só incluídas após cross-validation skill confirma wiring.

### False positive calibration

Run `bias-check.sh` against 5 existing plans. FP rate target: <20%. If >20%: tighten patterns + add exceptions list. Meta-discussion plans são FP source esperado (~30% acceptable for those).

**Visualização:** `docs/architecture/governance/concept-bias-taxonomy.excalidraw` (6 viéses em hexagon → Status Drift sumidouro central)

---

## §4. Adversarial Personas — 3 Independent Reviewers

Recalibração de score usa **3 personas adversariais blind** rodando em paralelo. Self-attestation bias só é defeitado por adversarial review. 3 personas com viéses opostos triangulam o "verdadeiro" score.

### The 3 personas

| Persona | Bias lens | Prompt file | Score pressure |
|---------|-----------|-------------|----------------|
| **P1 — Vercel DD Engineer** | DX/CLI/onboarding | `infra/scripts/governance/prompts/vercel-dd-engineer.md` | Penaliza fricção, internal terms, exit codes incoerentes |
| **P2 — Hostile VC Auditor** | Overclaim em pitch | `infra/scripts/governance/prompts/hostile-vc-auditor.md` | Penaliza aggregation + counting bias, "shipped" vagueness |
| **P3 — Paranoid SRE** | Production readiness | `infra/scripts/governance/prompts/paranoid-sre.md` | Penaliza chaos gaps, known-failing tests, runbook drilling absent |

### Independence requirement (T3.0 hardening — EC-1)

As 3 personas DEVEM rodar com **≥2 das 3 dimensões variadas** (validation in `infra/scripts/governance/verify-independence.sh`):

1. **Modelo:** 2+ modelos diferentes (opus/sonnet/haiku) OU cross-vendor (codex-rescue para 1 das 3)
2. **Temperature/seed:** P1 temp=0.7 seed=42 | P2 temp=0.3 seed=137 | P3 temp=0.5 seed=99
3. **System prompt:** persona prompt files já são textualmente distintos

**Config:** `docs/governance/personas/independence-config.yaml` (primary + fallback). Cross-vendor disponível: cap 95-97%. Fallback Claude-only family: cap 90-95%.

### Invocation pattern

```
Agent({
  subagent_type: "general-purpose",
  description: "P1 Vercel DD scoring",
  model: "opus",
  prompt: <load persona prompt file> + INPUT_ARTIFACTS_PROMPT
})
```

Inputs blind:
- `docs/governance/claim-catalog.yaml`
- `docs/architecture/governance/README.md` (AS-IS section)
- Sample evidence file

**Forbidden:** `docs/STATUS.md`, `docs/maturity-history/*.md`, CLAUDE.md status references (would bias scoring).

### Output schema

Each persona writes `docs/governance/scoring-YYYY-MM-DD/p{N}-{persona-slug}.yaml`:

```yaml
persona: p1_vercel_dd
ran_at: 2026-05-11T<HH:MM:SS>Z
model: claude-opus-4-7
temperature: 0.7
seed: 42

scores:
  core_deploy_flow:
    score: 4.3
    justification:
      - "Concrete bullet 1 citing artifact"
      - "Bullet 2"
      - "Bullet 3"
  dx_cli: ...
  # ... 7 dims total

overall_concerns:
  - "Persona-specific concerns across dims"
```

### Convergence rule (T3.2)

Per dim: compute `median(p1, p2, p3)` + `std_dev`. If `std_dev > 0.3` → flag (reconciliation meeting required per §4.1). Else: `final_score = median`.

### Reconciliation meeting (T3.3) — for flagged dims

Each persona presents 3 bullets justifying score. Arbiter (Principal Architect persona) asks: "Why the delta? Same evidence? Semantic vs factual disagreement?" Convergence target: single number with all 3 agreeing, OR document dissent.

Template: `infra/scripts/governance/templates/reconciliation-meeting.template.md`

### Edge cases

| Edge | Mitigation |
|------|------------|
| Cross-vendor codex unavailable in CI | Fallback: 3 Claude models + temp/seed variation; cap downgrades 95-97% → 90-95% |
| 1 of 3 yaml malformed (parse fails) | ERROR (not silently median-of-2); 2-source violates 3-source contract |
| All 3 agree numerically but different reasons | Manual review of `justification` bullets; semantic convergence ≠ numerical convergence |
| Persona refuses adversarial stance (soft alignment) | Prompt explicits stance + post-hoc manual spot-check |

**Visualização:** `docs/architecture/governance/concept-independence-matrix.excalidraw` (3x3 matrix + counter-example panel)

---

## §5. Scoring Methodology — End-to-End Pipeline

```
claim author → extract-claims.py → classify-evidence.py → init-decay-clock.py
                                                        ↓
                                              docs/governance/claim-catalog.yaml
                                                        ↓
operator runs T3.1 → 3 personas blind (per §4) → 3 yaml outputs
                                                        ↓
                          run-triangulation.sh → triangulation.yaml
                                                        ↓
                          generate-scorecards.py → 7 scorecard-{dim}.md
                                                        ↓
                          (if flagged) gen-reconciliation-records.py
                                                        ↓
                          operator runs /meeting per flagged dim
                                                        ↓
                          operator updates docs/STATUS.md §2
```

### 7 dimensions scored

1. **Core Deploy Flow** — `loopzero deploy` E2E, rollback, promote, canary
2. **DX / CLI** — commands, --json, exit codes, CI mode, onboarding
3. **Infrastructure / Operations** — clusters, observability, secrets, GitOps
4. **Security** — cosign, SBOM, policy-controller, RBAC, JWT
5. **Scale / Multi-tenancy** — cell architecture, KEDA, multi-cell routing
6. **Product Completeness** — preview envs, custom domains, plan tiers, audit log
7. **Tests / Confidence** — test counts, coverage, envtest, chaos, validation gates

### Scoring bands (per persona, all 0.0-5.0)

| Score | Meaning (universal) |
|-------|---------------------|
| 5.0 | Best-in-class; Tier 1 polish |
| 4.5 | Solid Tier 1, minor friction |
| 4.0 | Functional but missing 1-2 friction points |
| 3.5 | Multiple friction points |
| 3.0 | Works but operational gaps |
| 2.5 | Demo only; real user/audit would bounce |
| 2.0 | Multiple known-failing items |
| 1.0 | Material misrepresentation / broken in basic flows |

Each persona has additional auto-penalties (e.g., -0.5 for internal terms in CLI; -1.0 for zero external users in Product Completeness; -0.5 for chaos coverage absent in Scale).

---

## §6. Pre-Merge Gate (ADR D6)

PR touching `docs/STATUS.md` MUST also touch `docs/evidence/**`. Override: `evidence-waiver` label (requires justification + non-author approval).

Workflow: `.github/workflows/status-pr-gate.yml`

Validation per modified evidence file: `validate-evidence.sh` (exit 0/1/2).

---

## §7. Active Defenses Summary

| Defense | Implementation | Scope |
|---------|---------------|-------|
| 3-source bar | `infra/scripts/governance/validate-evidence.sh` | Per evidence file |
| Decay scanning | `infra/scripts/governance/scan-decay.sh` + weekly CI | Repo-wide |
| Bias detection | `infra/scripts/governance/bias-check.sh` | Per markdown file |
| Cross-validation | `infra/scripts/governance/cross-validate-plan.py` + weekly CI | Per plan |
| Adversarial review | 3 personas + `triangulate-scores.py` + `verify-independence.sh` | Per recalibration |
| Pre-merge gate | `.github/workflows/status-pr-gate.yml` | Per PR |
| Auto-refresh | `infra/scripts/governance/regenerate-status.sh` + daily CI | STATUS.md sections |
| Health check | `infra/scripts/governance/health-check.sh` (91+ checks) | Pre-Dogfood-Gate |

### Task wrappers

```bash
task validate:evidence         # Validates all docs/evidence/**.md
task decay:scan                # Detects expired claims
task decay:scan:catalog        # Decay scan on claim catalog
task bias:check                # Bias patterns per file
task bias:check:all            # Bias patterns on all plans
task cross-validate:plans      # Plan-vs-filesystem reality check
task governance:health         # 91+ aggregator check
task governance:lint           # markdownlint on docs/governance/
```

### CI workflows

| Workflow | Schedule | Purpose |
|----------|----------|---------|
| `decay-scan.yml` | Sun 02:00 UTC | Weekly decay alerts |
| `status-refresh.yml` | Daily 06:00 UTC | Auto-gen sections in STATUS.md |
| `cross-validation-weekly.yml` | Sun 03:00 UTC | Weekly plan cross-validation |
| `adversarial-spotcheck.yml` | Daily 04:00 UTC | Random dim+persona spotcheck |
| `status-pr-gate.yml` | On PR | Block STATUS.md edits w/o evidence |

---

## §8. How to Revalidate (Operator Workflow)

### When decay alert fires (CI opens issue)

1. Read the issue — lists expired claims by severity (CRITICAL >60d / HIGH >30d / MEDIUM)
2. For each claim: find the corresponding evidence file in `docs/evidence/{dim}/`
3. Read "How to revalidate" section in the evidence file
4. Run the documented commands
5. Update frontmatter `last_validated` to today + recompute `expires`
6. Commit: `docs(evidence): revalidate {claim-id} — {summary}`
7. CI re-runs decay scan; issue auto-closes

### When pre-merge gate fails

PR touches STATUS.md but no evidence/ delta:
1. Identify which claim was added/modified
2. Create/update `docs/evidence/{dim}/{claim-slug}.md` per schema (§1)
3. Run `task validate:evidence` locally — must exit 0
4. Push update
5. OR (rare): add `evidence-waiver` label with justification (requires reviewer != author)

### When triangulation flags dim (std-dev > 0.3)

1. Open `docs/governance/scoring-YYYY-MM-DD/reconciliation-{dim}.md` (auto-generated skeleton)
2. Read 3 persona justifications side-by-side
3. Run `/meeting` orchestrator OR manual arbiter analysis
4. Document final score + decision method (consensus / arbiter-decided / dissent-recorded)
5. Update `docs/STATUS.md` §2 row + scorecard
6. Mark reconciliation file as DONE

### Quarterly full recalibration

1. Verify independence: `bash infra/scripts/governance/verify-independence.sh`
2. Spawn 3 Agents in parallel (per `personas/independence-config.yaml`)
3. Each writes `docs/governance/scoring-YYYY-MM-DD/p{N}-*.yaml`
4. `bash infra/scripts/governance/run-triangulation.sh` → triangulation.yaml + raw-runs.md
5. `python3 infra/scripts/governance/generate-scorecards.py --scoring-dir docs/governance/scoring-YYYY-MM-DD/`
6. If flagged: `python3 infra/scripts/governance/gen-reconciliation-records.py --scoring-dir ...`
7. Update STATUS.md §2 + §7 (adversarial validation log)

---

## §9. Confidence Cap Reasoning (ADR D1)

| Configuration | Confidence cap |
|---------------|---------------|
| Single-source claim (no evidence file) | ~60-80% (folklore-level) |
| 2-source PARTIAL_CONFIDENCE | ~80-90% |
| 3-source same-author (no adversarial) | ~85% (self-attestation residual) |
| 3-source + 3 Claude-family personas (fallback mode) | **90-95%** |
| 3-source + cross-vendor personas (codex + Claude diversity) | **95-97%** |
| 99%+ | **Forbidden** — requires perfect independence, mathematically dishonest in practice |

Status reportado em `docs/STATUS.md` cap em 97%. Stating ≥98% violates ADR D1.

---

## §10. Cross-References

- **Current status:** [`docs/STATUS.md`](STATUS.md)
- **Canonical ADR:** [`docs/meetings/adr/0051-claim-evidence-decay-contract.md`](meetings/adr/0051-claim-evidence-decay-contract.md)
- **Architecture (diagrams):** [`docs/architecture/governance/`](architecture/governance/)
- **Implementation plan:** [`docs/plans/status-validation-and-doc-consolidation-plan.md`](plans/status-validation-and-doc-consolidation-plan.md)
- **CLAUDE.md Rule 11** — score-affecting STATUS.md edits require 3 evidence sources
- **Live Phase 3 outputs:** `docs/governance/scoring-2026-05-11/` (3 persona yamls + triangulation + 7 scorecards + 4 reconciliation records)
- **Claim catalog (348 claims):** `docs/governance/claim-catalog.yaml`
- **Persona prompts:** `infra/scripts/governance/prompts/{vercel-dd-engineer,hostile-vc-auditor,paranoid-sre}.md`
- **Scoring templates:** `infra/scripts/governance/templates/`
- **Independence config:** `docs/governance/personas/independence-config.yaml`

---

**This document is canonical for governance policy.** Run `task governance:health` to verify the entire governance system is healthy (91+ checks). Run `task governance:lint` to validate markdown style. ADR-0051 records the original decision context (immutable); this file is the live operational reference.
