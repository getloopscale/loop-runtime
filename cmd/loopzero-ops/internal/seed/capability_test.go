// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import "testing"

// A distância entre DECLARAR uma capacidade e TÊ-LA é o que estes testes medem.
//
// Medido em 2026-08-11: o app-dev subiu 20h servindo um painel que mostra plano, preço e
// botão de upgrade, com o segredo que verifica pagamentos ausente. Nenhuma camada mentiu
// isoladamente — cada uma tratou a ausência como uma configuração legítima. É legítima,
// num ambiente hermético; não é, num que promete cobrar.

// TestPoliticaExigeCobrancaQuandoOAmbienteADeclara é o caso que muda o comportamento.
func TestPoliticaExigeCobrancaQuandoOAmbienteADeclara(t *testing.T) {
	p := NewEnvRequirementPolicy("dev-public")
	if !p.Requires(CapabilityBilling) {
		t.Fatal("dev-public serve o painel público com plano e preço; se ele declara cobrança, " +
			"a ausência da credencial é defeito e não configuração")
	}
}

// TestPoliticaNaoExigeNoAmbienteQueNaoDeclara protege o comportamento de hoje.
//
// O hermético roda sem credencial de Stripe de propósito, e continuar verde ali é o que
// permite que esta mudança entre sem quebrar quem não prometeu nada.
func TestPoliticaNaoExigeNoAmbienteQueNaoDeclara(t *testing.T) {
	for _, amb := range []string{"", "standalone-multi", "drill"} {
		if NewEnvRequirementPolicy(amb).Requires(CapabilityBilling) {
			t.Errorf("ambiente %q não declara cobrança; exigi-la ali quebra o hermético", amb)
		}
	}
}

// TestCapacidadeDesconhecidaNaoEExigida — a tabela é dado, e dado tem buraco.
//
// Uma capacidade que ninguém declarou não pode virar exigência por omissão: seria a mesma
// polaridade invertida que este trabalho existe para corrigir, só que no outro sentido.
func TestCapacidadeDesconhecidaNaoEExigida(t *testing.T) {
	if NewEnvRequirementPolicy("dev-public").Requires(Capability("coisa-que-nao-existe")) {
		t.Error("capacidade não declarada não pode ser exigida por omissão")
	}
}

// TestPoliticaNulaNaoExigeNada é a garantia de que a injeção é opcional.
//
// `Deps` é construído em vários lugares (testes, subcomandos antigos). Se uma `Deps` sem
// política passasse a exigir, esta mudança quebraria chamadores que nada têm a ver com
// cobrança — e o modo de falha seria um FAIL sem explicação.
func TestPoliticaNulaNaoExigeNada(t *testing.T) {
	var d Deps
	if d.RequiresCapability(CapabilityBilling) {
		t.Error("Deps sem política deve se comportar como hoje: não exigir")
	}
}

// TestDepsConsultaAPoliticaInjetada — o caminho que os seeds vão usar.
func TestDepsConsultaAPoliticaInjetada(t *testing.T) {
	d := Deps{Policy: NewEnvRequirementPolicy("dev-public")}
	if !d.RequiresCapability(CapabilityBilling) {
		t.Error("Deps deve delegar à política injetada, não reimplementar a decisão")
	}
}

// TestAmbienteQueDeclaraCobrancaEstaNaTabela trava a tabela real contra esvaziamento.
//
// Uma tabela de exigências que fica vazia não falha — ela passa a permitir tudo, em
// silêncio. É exatamente o modo de falha da lista de bloqueio do firewall, medido em
// 2026-08-10: uma lista que envelheceu para o lado inseguro sem que nada acusasse.
func TestAmbienteQueDeclaraCobrancaEstaNaTabela(t *testing.T) {
	if len(capabilitiesByEnv) == 0 {
		t.Fatal("a tabela de capacidades está vazia — nenhum ambiente exigiria nada")
	}
	caps, ok := capabilitiesByEnv["dev-public"]
	if !ok || len(caps) == 0 {
		t.Fatal("dev-public é o ambiente que serve o painel público; ele precisa declarar " +
			"o que oferece, ou a política não tem o que exigir")
	}
}
