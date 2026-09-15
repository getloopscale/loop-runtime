// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"os"
	"strings"
	"testing"
)

// B-022: o `loopzero-cloud` e' o proxy autenticador do motor. `r.Route("/engine")` tira o prefixo,
// aplica requireJWT e repassa ao `loopzero-api`; quando ele cai, `/engine/*` e `/v1/*` devolvem 502
// JUNTOS, mesmo com o motor saudavel — medido em 2026-08-10.
//
// Ele era o unico componente com esse acoplamento que NINGUEM sondava. Sem serie de
// disponibilidade, a bala do B-022 que exige "risco medido e nao desenho no quadro" nao tinha
// como ser respondida — e a decisao de fronteira de autenticacao que ela condiciona ficava presa.
//
// Este teste existe para que a sonda nao suma em silencio: um alvo removido nao quebra nada
// visivelmente, so' apaga o dado, e a falta do dado e' invisivel ate' alguem precisar dele.
func TestProbeCoversLoopZeroCloud(t *testing.T) {
	const path = "../../helm/monitoring/manifests/probe-targets.yaml"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("probe-targets ausente: %v", err)
	}
	s := string(b)

	if !strings.Contains(s, "loopzero-cloud.loopzero-system.svc") {
		t.Error("o `loopzero-cloud` saiu dos alvos de sonda: sem ele nao ha' serie de " +
			"disponibilidade do componente cuja queda derruba tambem o caminho do motor (B-022)")
	}
	// A contrapartida: os alvos que ja' existiam nao podem ter sido substituidos pelo novo.
	for _, alvo := range []string{
		"loopzero-api.loopzero-system.svc",
		"gitea-http.loopzero-system.svc",
		"traefik.loopzero-system.svc",
		"argocd-server.argocd.svc",
	} {
		if !strings.Contains(s, alvo) {
			t.Errorf("o alvo %q sumiu dos probes", alvo)
		}
	}
}
