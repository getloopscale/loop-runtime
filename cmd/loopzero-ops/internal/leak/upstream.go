// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import "net/http"

// UpstreamState separa "o motor esta fora" de "o painel esta fora".
//
// # O defeito, medido
//
// B-022, 2026-08-10: sincronizando o Caddyfile por cima do servidor, o `loopzero-cloud` ficou
// sem upstream alcancavel — e `/engine/*` caiu junto, porque o caminho do motor atravessa
// o painel. Os dois apareceram como 502, e a sonda nao tinha como dizer qual havia caido.
//
// As duas situacoes pedem acoes diferentes: motor fora e reconciliacao de deploy no cluster
// central; painel fora e o proxy da borda. Um relatorio que as confunde manda o operador
// para o lugar errado exatamente quando o tempo importa.
//
// # Como a separacao e possivel
//
// Porque as rotas de PRODUTO do painel nao atravessam o motor. Medido em B-002:
// `loopzero-cloud` proxia `/engine/*` para o `loopzero-api`, enquanto `/v1/auth`, `/v1/billing`,
// `/v1/memory` e as demais usam handlers e upstreams proprios. Entao uma rota de produto
// respondendo enquanto `/engine/*` nao responde e evidencia de que o painel esta de pe.
//
// # O limite
//
// Isto classifica DOIS codigos de status, nao a causa. Um 502 pode ser upstream fora,
// upstream lento ou rota mal configurada, e a sonda nao distingue esses tres. O que ela
// passa a distinguir e QUAL das duas superficies falhou — que era o que faltava.
type UpstreamState int

const (
	// UpstreamBothUp — nenhuma das duas superficies esta com falha de gateway.
	UpstreamBothUp UpstreamState = iota
	// UpstreamEngineDown — o painel responde e o caminho do motor nao.
	UpstreamEngineDown
	// UpstreamDashboardDown — as duas falham, e como o motor atravessa o painel, a causa
	// comum e o painel.
	UpstreamDashboardDown
	// UpstreamInconclusive — a leitura contradiz a topologia conhecida, ou falta dado.
	UpstreamInconclusive
)

// gatewayFailure reporta se o codigo indica upstream inalcancavel pela borda.
//
// 502/503/504 sao os tres que o proxy emite quando nao consegue falar com quem esta atras.
// Um 500 NAO entra: ele vem do proprio servico, que portanto esta de pe.
func gatewayFailure(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// ClassifyUpstream separa as duas falhas a partir de uma sonda no caminho do motor e outra
// numa rota de produto do painel.
func ClassifyUpstream(engineStatus, dashboardStatus int) UpstreamState {
	engineBad := gatewayFailure(engineStatus)
	dashBad := gatewayFailure(dashboardStatus)

	switch {
	case !engineBad && !dashBad:
		return UpstreamBothUp
	case engineBad && dashBad:
		return UpstreamDashboardDown
	case engineBad && !dashBad:
		return UpstreamEngineDown
	default:
		// Painel falhando com o motor respondendo contradiz a topologia: `/engine/*`
		// atravessa o painel, entao esta combinacao nao deveria existir. Inventar um
		// veredito aqui seria afirmar o que a sonda nao sabe.
		return UpstreamInconclusive
	}
}

// Explicacao diz ao operador o que a leitura significa e para onde olhar.
func (s UpstreamState) Explicacao() string {
	switch s {
	case UpstreamBothUp:
		return "motor e painel alcancaveis pela borda"
	case UpstreamEngineDown:
		return "o painel responde e o caminho do motor nao — olhe o loopzero-api no cluster central, nao o proxy da borda"
	case UpstreamDashboardDown:
		return "as duas superficies falham; como /engine/* atravessa o painel, comece pelo loopzero-cloud e pelo proxy da borda"
	case UpstreamInconclusive:
		return "a leitura contradiz a topologia conhecida (/engine/* atravessa o painel) — sonde de novo antes de agir"
	}
	return "estado desconhecido"
}
