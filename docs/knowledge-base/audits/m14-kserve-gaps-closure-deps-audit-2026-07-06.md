# Deps Audit: m14-kserve-gaps-closure

**Date:** 2026-07-06
**Mode:** plan-bound:m14-kserve-gaps-closure
**Verdict:** PASS_WITH_CAVEATS
**Hard caps triggered:** [] (nenhum hard cap — plano declara zero deps novas; deps existentes citadas estão limpas)

## Summary

- Ecosystems detected: go (3 módulos: api, operators, pkg)
- Total deps audited: superfície das 3 deps declaradas (controller-runtime v0.23.3, prometheus/client_golang v1.23.2, aws-sdk-go via api/internal/s3) + stdlib
- Vulnerabilities found nas deps DECLARADAS do plano: **0** CRITICAL, 0 HIGH, 0 MEDIUM, 0 LOW
- Vulnerabilities repo-wide (pré-existentes, fora do escopo do M14): 54 advisories em **Go stdlib** (toolchain go1.26.1) — NÃO introduzidas por este plano
- Outdated: n/a (plano adiciona zero deps)
- Allowlist hits: 0
- Auditor coverage: { osv-scanner v1.9.2: ran; govulncheck: **UNAVAILABLE** — toolchain mismatch (binário go1.25 vs módulo go1.26), reachability não verificável localmente }

## Vulnerabilities (sorted by severity)

Nenhuma nas dependências que o plano declara. As 54 findings do osv-scanner são todas em `stdlib` (ex.: GO-2026-4947, GO-2026-4971, ..., GO-2026-5039) associadas à versão do Go toolchain (1.26.1), afetam o repositório inteiro independentemente do M14, e são um concern de bump-de-toolchain separado — **não** são deps declaradas nem introduzidas por este plano.

As linhas de `controller-runtime@v0.23.3` que apareceram no output são **build-warnings** de toolchain (govulncheck reclamando de go1.25 vs go1.24/1.26), NÃO advisories de segurança — o osv-scanner não reportou nenhum CVE em controller-runtime, prometheus ou aws-sdk.

## Outdated (non-vulnerable)

(n/a — o plano não adiciona nem faz bump de dependência)

## Plan validation (Mode 2)

| Plan dep | Section | Manifest match | Audit clean? | Rule 9 OK? | Verdict |
|---|---|---|---|---|---|
| `sigs.k8s.io/controller-runtime` v0.23.3 | Existing | yes (`operators/go.mod`) | yes (0 CVE) | n/a (existente) | OK |
| `github.com/prometheus/client_golang` v1.23.2 | Existing | yes (`api/go.mod`) | yes (0 CVE) | n/a (existente) | OK |
| `github.com/aws/aws-sdk-go` (via api/internal/s3) | Existing | yes | yes (0 CVE) | n/a (existente) | OK |
| (none) | NEW | — | — | — | OK — plano adiciona ZERO deps (D4) |

## Cavea­ts (por que PASS_WITH_CAVEATS e não PASS)

1. **govulncheck indisponível** — o binário local está em go1.25 e o módulo exige go1.26.1, então a análise de *reachability* (a parte que confirma se o código realmente chama o path vulnerável do stdlib) não pôde rodar. Per a regra do gate, um auditor indisponível NUNCA vira PASS limpo. Como as findings do osv (o auditor que rodou) são todas stdlib e NENHUMA toca as deps do plano, o risco para o M14 é nulo — mas a honestidade exige o caveat.
2. **54 advisories de Go stdlib pré-existentes** — repo-wide, concern de bump de toolchain, rastreável separadamente. Não bloqueia o M14 (que introduz zero deps).

## Recommended next steps

1. Nenhuma ação de dependência necessária para o M14 (zero deps novas).
2. (Separado do M14) considerar bump do Go toolchain para endereçar os 54 stdlib advisories repo-wide — fora do escopo deste plano.
3. Prosseguir com `/plan-confidence` — o veredito PASS_WITH_CAVEATS não bloqueia (nenhum CVE em dep declarada; nenhum hard cap do deps-audit-golden-rule fired).
