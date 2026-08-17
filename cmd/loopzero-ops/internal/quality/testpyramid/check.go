// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testpyramid gates the test pyramid — DERIVANDO-A da árvore.
//
// Os alvos do ADR D7 (unit ≥70%, integração ≤25%, e2e ≤5%) continuam aqui como
// `DefaultThresholds`, mas hoje eles são REPORTADOS, não impostos: ver derived.go
// para a razão medida.
//
// # O que foi removido, e por quê
//
// Este pacote lia três percentuais escritos em `infra/quality/test-inventory.json`.
// Medido em 2026-08-17 (B-106): o arquivo descrevia 375 de 988 arquivos de teste —
// **62% cego** — e declarava 99,4% unit sobre essa amostra, que exclui a suíte
// `infra/tests/e2e` inteira. Um arquivo podia declarar a pirâmide que quisesse.
//
// A leitura do arquivo declarado saiu junto com o arquivo. O que sobrou é `Thresholds`
// (alvo do ADR) e a catraca sobre a medição, em derived.go.
package testpyramid

// Thresholds are the pyramid bounds (percentages).
type Thresholds struct {
	UnitMinPct        float64
	IntegrationMaxPct float64
	E2EMaxPct         float64
}

// DefaultThresholds per ADR D7.
func DefaultThresholds() Thresholds {
	return Thresholds{UnitMinPct: 70, IntegrationMaxPct: 25, E2EMaxPct: 5}
}
