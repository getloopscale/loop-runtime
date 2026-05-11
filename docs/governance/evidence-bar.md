# Evidence Bar Policy

**Status:** Canonical (per plan `status-validation-and-doc-consolidation-plan.md` T0.1)
**TTL:** 90 days (ADR — strategic decision)
**Last validated:** 2026-05-11

## Purpose

Toda claim com impacto em maturity score em `docs/STATUS.md` requer **3 fontes de evidência independentes**. Esse documento define o que conta como evidência, como cada tier é validado, e por que a barra existe.

## The 3-Tier Schema

### Tier 1 — Code + Test (T1)

**Definição:** Commit SHA + arquivo de teste mencionado nesse commit que verifica o comportamento da claim.

**Formato de referência:** `commit:<sha>:<test-file-path>`

**Exemplo:**
```
ref: commit:341181ae:api/internal/services/deploy_test.go
```

**Validation:** `git show <sha> -- <test-file>` exit 0 + grep `^func Test` ou `^func.*Spec` no diff.

**Edge cases rejected:**
- Test que existe mas é `t.Skip()` ou `@skip` → BROKEN, não T1 (vide EC-9 do edge-case review)
- Commit que foi force-pushed → BROKEN (commit não existe mais no histórico atual)
- Test file que existe mas não foi modificado no commit (claim retroativa) → reclassify (claim teve teste depois — usar commit do teste, não commit da feature)

### Tier 2 — LIVE Run Log (T2)

**Definição:** Timestamp + archived log file mostrando que comportamento foi executado contra infraestrutura real (não simulação).

**Formato de referência:** `live:<timestamp>:<log-path>`

**Exemplo:**
```
ref: live:2026-05-05T18:42:00Z:docs/evidence/core-deploy/2026-05-05-cleanroom-12-12.md
```

**Validation:** File path existe + timestamp parsing OK + log content menciona comando executado + output esperado.

**Storage locations aceitas:**
- `${HOME}/.loopzero/artifacts/` (operator local; preservar com checksum)
- `docs/evidence/` (in-repo; preferido para logs <1MB)
- `docs/runbooks/` (post-CHG validation logs já archived)

**Edge cases rejected:**
- Log file >1MB sem checksum → must add SHA256 to frontmatter
- Log sem timestamp parseable → BROKEN
- Log de simulação ou unit test mocked → não conta como T2 (é T1)

### Tier 3 — CI Workflow Run (T3)

**Definição:** GitHub Actions workflow URL + run ID com conclusion=success.

**Formato de referência:** `ci:<workflow-name>:<run-id>`

**Exemplo:**
```
ref: ci:cell2-soaks.yml:12345
```

**Validation:** `gh run view <run-id>` retorna `conclusion: success`. Workflow file existe em `.github/workflows/`.

**Edge cases rejected:**
- Workflow run com `conclusion: cancelled` ou `failure` → não conta
- Run ID inválido ou de PR fechado → BROKEN
- Workflow scheduled mas nunca executou (only `schedule` block sem trigger real) → não conta

## Independence Requirement (EC-6 from edge-case review)

As 3 referências DEVEM ser **distintas**. Mesma `ref` em 2+ slots viola independência matemática.

**Rejected example:**
```yaml
evidence:
  - type: T1
    ref: docs/evidence/core-deploy/2026-05-05-cleanroom.md  # ❌ mesmo path em T1
  - type: T2
    ref: docs/evidence/core-deploy/2026-05-05-cleanroom.md  # ❌ e T2
  - type: T3
    ref: ci:cell2-soaks.yml:12345
```

Validator retorna exit 1 com mensagem `EVIDENCE_REFS_NOT_INDEPENDENT`.

## Confidence Math (ADR D1)

Com 3 fontes independentes, cada com p≥0.9 de correta isoladamente:
- **Teórico:** 1 − (0.1)³ = 99.9%
- **Realístico cap:** **95-97%** (não 99%, vide EC-1)

Por que o cap < 99%:
- Testes e LIVE runs podem compartilhar fixtures (correlação ~0.2)
- CI invoca mesmos testes em ambiente diferente (correlação ~0.3)
- Agentes Claude da mesma model family que avaliam evidência têm bias compartilhado

Para alcançar 99% real, exigir cross-vendor model em adversarial review (vide T3.0 do plano).

## Classification Outputs

Após validation, cada claim recebe um label:

| Label | Critério | Tratamento |
|-------|----------|------------|
| **EVIDENCED** | 3 fontes distintas T1/T2/T3, todas válidas, TTL não expired | Conta para score oficial |
| **PARTIAL_CONFIDENCE** | 2 fontes distintas válidas | Citável com asterisco; não conta para score |
| **UNEVIDENCED** | 0-1 fontes válidas | Sai de STATUS.md; fica em claim-catalog para revalidação |
| **BROKEN** | Refs apontam para artefatos que não existem | Bloqueia merge se em STATUS.md |
| **EXPIRED** | TTL vencido (per decay-policy.md) | Marca como NEEDS REVALIDATION; alerta opens issue |

## Schema for Evidence File Frontmatter

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

## Validator Script

`infra/scripts/governance/validate-evidence.sh <evidence-file>`

Exit codes:
- **0** — valid (EVIDENCED ou PARTIAL_CONFIDENCE)
- **1** — invalid (UNEVIDENCED, BROKEN, refs not independent, schema violation)
- **2** — expired (EXPIRED)

## Design Claims (Non-Testable)

Claims sobre design ("arquitetura é coesa", "boundaries limpos") **não vão para STATUS.md**. Elas pertencem em `docs/architecture/`. STATUS.md carrega apenas claims testáveis.

## Cross-Reference

- ADR D1 (plano v1.1+): confidence cap 95-97%
- ADR D5 (plano): evidence files in-repo, não external
- `docs/governance/decay-policy.md`: TTL rules
- `docs/governance/personas/README.md`: adversarial verification
