---
type: audit
title: "Deps Audit: operator-glue-go-migration (M15)"
---

# Deps Audit: operator-glue-go-migration (M15)

**Date:** 2026-07-08
**Mode:** plan-bound:operator-glue-go-migration
**Verdict:** PASS
**Hard caps triggered:** [] (nenhum)

## Summary
- Ecosystems detected: Go
- Deps declaradas pelo plano: 6 (4 NEW k8s + testify + loopzero/pkg)
- Vulnerabilities nas deps DECLARADAS: 0 CRITICAL / 0 HIGH / 0 MEDIUM / 0 LOW
- Auditor coverage: { osv-scanner v1.9.2: ran (autoritativo, OSV.dev), govulncheck: toolchain-mismatch em runtime — cross-check por osv-scanner sobre `operators/go.sum` que já vendoriza as versões exatas }

## Plan validation (Mode 2)

Scan de `operators/go.mod` (módulo que JÁ vendoriza as 4 deps k8s nas versões exatas do plano — v0.35.2 client-go/apimachinery, v0.35.1 cli-runtime, v0.23.3 controller-runtime):

| Plan dep | Section | Manifest match | Audit clean? | Rule 9 OK? | Verdict |
|---|---|---|---|---|---|
| `k8s.io/client-go@v0.35.2` | NEW | yes (operators/api) | **yes — 0 CVE** | yes (2 alts rejeitadas) | OK |
| `k8s.io/apimachinery@v0.35.2` | NEW | yes | **yes — 0 CVE** | yes | OK |
| `k8s.io/cli-runtime@v0.35.1` | NEW | yes (família 0.35.x) | **yes — 0 CVE** | yes | OK |
| `sigs.k8s.io/controller-runtime@v0.23.3` | NEW | yes | **yes — 0 CVE** | yes (2 alts rejeitadas) | OK |
| `github.com/stretchr/testify@v1.11.1` | Existing | yes | yes — 0 CVE | n/a | OK |
| `github.com/getloopscale/loopzero/pkg` | Existing | yes (workspace) | yes | n/a | OK |

**Evidência:** `osv-scanner --lockfile=operators/go.mod --format=json` → findings nas 4 deps alvo = **NENHUM** (verificado por parse programático).

## Pre-existing findings (OUT of plan scope — honestidade, anti-pattern 3)

O módulo `operators` (destino das deps) carrega CVEs transitivos/stdlib PRÉ-EXISTENTES, NÃO introduzidos por este plano (nenhum é dep declarada do M15):

| Pacote | Findings | Natureza | Ação |
|---|---|---|---|
| `stdlib@1.26.1` | 20 (GO-2026-*) | CVEs do compilador Go — afetam o monorepo inteiro, resolvem com bump de toolchain | Tracking separado (hygiene monorepo, não-M15) |
| `golang.org/x/crypto@0.51.0` | 27 (GHSA-*) | Transitivo pré-existente | Tracking separado |
| `github.com/prometheus/prometheus@0.303.0` | 8 (GHSA-*) | Transitivo pré-existente | Tracking separado |

Estes NÃO afetam o veredito plan-bound (a regra capa por CVE em dep DECLARADA; as declaradas estão limpas). Registrados por honestidade — recomenda-se um `/deps-audit` standalone do monorepo + bump de toolchain como follow-up fora do M15.

## Recommended next steps
1. Nenhum bump necessário nas deps do M15 (todas limpas).
2. Follow-up (fora do M15): `/deps-audit` standalone + toolchain/x-crypto bump para os findings pré-existentes.
3. Proceder com `/plan-confidence`.
