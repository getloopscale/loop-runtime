// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import "testing"

// As rotas mutantes que o middleware de auditoria REALMENTE ve'. Medido em
// 2026-08-18 (B-059): `AuditLog` e' montado uma unica vez, em
// api/internal/server/router.go:239, dentro do grupo autenticado que vai das
// linhas 237 a 327. Rotas registradas fora dele — todas as `/internal/*`,
// `/api/v1/webhooks/github` (linha 180) e `/api/v1/admin/tenants/pin`
// (linha 212) — nao passam por este middleware e por isso NAO estao aqui:
// afirma-las seria inventar dano.
//
// Das 32 rotas mutantes do repositorio, 9 caiam no fallback de
// `extractSegment`, e destas 4 estao sob o middleware. O fallback devolve o
// ultimo segmento do caminho CONCRETO, entao a acao gravada passava a ser
// `unknown.<METODO>.<ultimo-segmento>`.
var rotasAuditadasMutantes = []struct {
	nome   string
	metodo string
	// caminho ja' com o parametro substituido pelo valor concreto que chega em
	// runtime — e' o caminho concreto, nao o padrao, que `extractSegment` le'.
	caminho string
}{
	{"exclusao de tenant", "DELETE", "/api/v1/tenants/tn-79611071399a2e5e"},
	{"restauracao de tenant", "POST", "/api/v1/tenants/tn-79611071399a2e5e/restore"},
	{"exclusao em massa de tenants", "POST", "/api/v1/tenants/bulk-delete"},
	{"troca de equipe ativa", "POST", "/api/v1/auth/team/switch"},
}

// Toda rota mutante sob o middleware precisa resolver para uma acao mapeada. A
// forma `unknown.*` existe como fail-loud (AUDIT-18), e o proprio codigo pede
// que operadores a "filtrem em paineis" — um aviso em log que ninguem observa
// nao e' verificacao. Este teste e' a verificacao.
func TestDeriveAction_RotasAuditadasNaoCaemEmUnknown(t *testing.T) {
	if len(rotasAuditadasMutantes) == 0 {
		t.Fatal("tabela vazia: um teste que nao percorre rota nenhuma passa por vacuidade")
	}
	for _, r := range rotasAuditadasMutantes {
		t.Run(r.nome, func(t *testing.T) {
			got := deriveAction(r.metodo, r.caminho)
			if len(got) >= 8 && got[:8] == "unknown." {
				t.Errorf("%s %s gravou acao %q; toda rota auditada precisa de um ramo em deriveAction",
					r.metodo, r.caminho, got)
			}
		})
	}
}

// O dano especifico da exclusao de tenant: o fallback devolvia o id concreto,
// entao a acao era DIFERENTE para cada tenant. Nenhuma consulta consegue
// filtrar "exclusoes de tenant" quando o proprio nome da acao carrega o id —
// e a exclusao de tenant e' a operacao mais destrutiva do produto.
func TestDeriveAction_ExclusaoDeTenantTemCardinalidadeLimitada(t *testing.T) {
	a := deriveAction("DELETE", "/api/v1/tenants/tn-79611071399a2e5e")
	b := deriveAction("DELETE", "/api/v1/tenants/tn-78d5c2d4f678b774")
	if a != b {
		t.Errorf("a acao varia com o id do tenant: %q vs %q — cardinalidade ilimitada", a, b)
	}
}

// `resource_type` sofria do MESMO fallback uma funcao adiante: o `default` de
// `extractResourceType` devolve o proprio segmento, entao a exclusao de tenant
// gravava o ID do tenant como TIPO do recurso. Limitar a acao sem limitar o
// tipo consertaria metade do dano.
func TestExtractResourceType_RotasAuditadasSaoTipoSingularConhecido(t *testing.T) {
	casos := map[string]string{
		"/api/v1/tenants/tn-79611071399a2e5e":         "tenant",
		"/api/v1/tenants/tn-79611071399a2e5e/restore": "tenant",
		"/api/v1/tenants/bulk-delete":                 "tenant",
		"/api/v1/auth/team/switch":                    "team",
	}
	for caminho, quer := range casos {
		if got := extractResourceType(caminho); got != quer {
			t.Errorf("%s -> resource_type %q; queria %q", caminho, got, quer)
		}
	}
}
