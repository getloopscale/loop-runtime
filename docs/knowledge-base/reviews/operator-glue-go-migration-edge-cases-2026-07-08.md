---
type: review
title: "Edge Case Review — operator-glue-go-migration (M15)"
---

# Edge Case Review — operator-glue-go-migration (M15)

Date: 2026-07-08
Plan: .claude/knowledge-base/plans/operator-glue-go-migration-plan.md (v1.0)
Tasks analyzed: 12 (T0.1-T0.4, T1.x, T2.x, T3.x+T3.9, T4.1-T4.4, T5.1-T5.2)
Cases found: 10 (EDGE: 4, NEGATIVE: 6 | MUST FIX: 3, SHOULD TEST: 3, DOCUMENT: 4)

## MUST FIX

### EC-1: Contrato de paridade indefinido — vereditos bash não são estruturados
- **Affected task:** T1.x/T2.x/T3.x (recipe, passo 3 "vereditos idênticos")
- **Kind:** NEGATIVE (comparação inválida → falsa paridade)
- **Family:** Format / Boundary
- **Scenario:** os seeds bash não têm verdito estruturado uniforme (`--verify` existe em alguns; saída é texto livre); "vereditos idênticos" por string-match é incomparável e pode passar/falhar arbitrariamente.
- **Impact:** paridade fabricada ou falso-negativo → DELETE inseguro OU wave travada.
- **Suggested fix:** definir o contrato de paridade na recipe como: **(a) exit code igual E (b) efeito no cluster igual, provado por probe pós-execução idêntica** (ex.: Secret existe com keys X; Deployment READY) — nunca string-match de output.

### EC-2: `seed all` divergiria do comportamento do master bash em FAIL
- **Affected task:** T3.9
- **Kind:** EDGE (comportamento no limite da chain com step FAIL)
- **Family:** State
- **Scenario:** o TDD do T3.9 declara `_StopsOnHardFail`, mas o master bash CONTINUA em FAIL e agrega (evidência: chain-run-1 "Seeds PASS=19 FAIL=6 SKIP=1" — a chain completou). Parar no primeiro FAIL quebra a paridade e muda a semântica operacional do Cleanroom.
- **Impact:** paridade do orchestrator impossível; Cleanroom pós-migração pararia no step 4b (hydra DNS) que hoje é FAIL conhecido e tolerado.
- **Suggested fix:** T3.9 TDD trocar `_StopsOnHardFail` por `_ContinuesOnFailAndAggregates` (exit ≠ 0 se ≥1 FAIL; roda todos os steps; `--fail-fast` opcional como flag NOVA, default off).

### EC-3: Sweep de referências ignora sourcing intra-bash (`infra/**`)
- **Affected task:** T1.x-T3.x passo 4, T4.3, T5.2
- **Kind:** NEGATIVE (delete quebra consumidor vivo)
- **Family:** State / Integration
- **Scenario:** scripts bash vivos fazem `source`/invocação de outros scripts (ex.: `infra/scripts/lib/*.sh` sourced por drill; master chama seeds); o sweep declarado cobre `taskfiles/`, `docs/`, `.claude/skills/` mas NÃO `infra/**`.
- **Impact:** `git rm` de uma lib sourced quebra scripts KEEP silenciosamente (só falha em runtime de operador).
- **Suggested fix:** incluir `infra/` no grep-sweep de cada lote: `grep -rn "<basename>" infra/ taskfiles/ docs/ .claude/skills/ = 0 hits` (excluindo o próprio arquivo deletado).

## SHOULD TEST

### EC-4: creds file presente porém JSON malformado
- **Affected task:** T0.2 (creds.go)
- **Kind:** NEGATIVE
- **Suggested test:** `TestCreds_MalformedJSONIsFail` — arquivo 0600 com JSON truncado → **FAIL tipado** (`ErrCredsMalformed` com path + posição), NUNCA SKIP (arquivo presente e corrompido = erro do operador, distinto de ausente).

### EC-5: estado do cluster muda entre a execução bash e a Go da paridade
- **Affected task:** T1.x-T3.x passo 3
- **Kind:** NEGATIVE (falsa divergência)
- **Suggested test:** procedimento (não unit): rodar o par bash→Go imediatamente em sequência; em divergência, re-rodar o par 1× antes de declarar quebra de paridade; registrar ambos os pares no relatório.

### EC-6: porta local ocupada no port-forward
- **Affected task:** T2.x (httpapi.go)
- **Kind:** NEGATIVE
- **Suggested test:** `TestPortForward_UsesEphemeralPort` — forward pede porta `:0` (efêmera) e o helper retorna a porta alocada; nunca porta fixa hardcoded (bash usava 15432/4445 fixas — fonte de flake).

## DOCUMENT

### EC-7: allowlist exatamente 30 vs 31 (limite do gate)
- **Kind:** EDGE — já coberto pelo fixture test do T5.1 (30=PASS, 31=FAIL). Registrado como coberto.

### EC-8: contexto k3d ausente em seeds multi-cluster
- **Kind:** EDGE — paridade por-ramo: onde o bash skipa com "cluster ausente", o Go skipa com a mesma razão; onde falha, falha. A recipe já exige preservar invariantes por seed; nota explícita adicionada à P3.

### EC-9: destino de migração precisa ser pickup pelo runner existente
- **Kind:** EDGE — `infra/tests/livetest` e `api/internal/contract/dev_public` já rodam via `go test` dos módulos do go.work (pre-flight do to-plan verificado). Aceito.

### EC-10: master durante as waves invoca o binário Go
- **Kind:** EDGE — steps swapped usam `go run ./cmd/loopzero-ops seed <nome>` a partir da raiz do workspace (lento porém correto e sem instalação); aceito para a janela de migração; `seed all` final elimina.

## Summary

| Task | EDGE | NEGATIVE | MUST FIX | SHOULD TEST | DOCUMENT |
|------|------|----------|----------|-------------|----------|
| T0.2 | 0 | 1 | 0 | 1 (EC-4) | 0 |
| T0.4 | 0 | 0 | 0 | 0 | 0 |
| T1.x-T3.x recipe | 1 | 3 | 2 (EC-1, EC-3) | 1 (EC-5) | 2 (EC-8, EC-10) |
| T2.x | 0 | 1 | 0 | 1 (EC-6) | 0 |
| T3.9 | 1 | 0 | 1 (EC-2) | 0 | 0 |
| T4.2/T4.3 | 1 | 1 | (EC-3 compartilhado) | 0 | 1 (EC-9) |
| T5.1 | 1 | 0 | 0 | 0 | 1 (EC-7) |

**Coverage check:** toda task com fronteira de input tem ≥1 EDGE e ≥1 NEGATIVE considerados (T0.1/T0.4 e T5.2 são mecânicas sem fronteira de input — lente n/a anotada).

**Verdict:** PLAN NEEDS ADJUSTMENT (3 MUST FIX → absorver em v1.1)
