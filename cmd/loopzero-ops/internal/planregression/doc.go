// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package planregression agrupa as suites que verificam que um plano JA'
// CONCLUIDO continua implementado — nao invariantes permanentes da plataforma.
//
// Elas viviam em `internal/quality/` com nomes terminados em `gate` e nunca
// foram registradas no `gateRegistry` (B-066). O efeito era o inventario mentir
// por omissao: `loopzero-ops quality --list`, que o `CLAUDE.md` designa como O
// ORACULO dos gates, nao conseguia enumera-las, e o `quality_gate_wiring_test.go`
// — que cobra que todo gate registrado esteja ligado ou isento — nao as
// alcancava, porque cobra o que esta' registrado.
//
// Registra-las como gates seria pior que o defeito: um gate e' um invariante
// permanente, e estas tres verificam fases de UM plano especifico
// (`2026-05-22-shell-script-strategic-plan` v1.3) que terminou. O inventario
// permanente encheria de checagem de obra acabada.
//
// Elas continuam valendo como protecao de regressao — alguem pode reintroduzir o
// que aquele plano removeu — e seguem rodando via `go test ./...`. O que mudou e'
// que nao vestem mais o nome de um diretorio ao qual nao pertencem.
package planregression
