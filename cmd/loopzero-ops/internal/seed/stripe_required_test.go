// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"strings"
	"testing"
)

// O verde falso que estes testes matam.
//
// MEDIDO em 2026-08-11: `seed stripe-webhook-secret --verify` numa máquina sem
// `dev-credentials.json` imprime `SKIP — dev-credentials.json absent (SKIP-graceful)` e
// sai com código 0. `Verdict.ExitCode()` (seed.go:58) devolve 0 para PASS e para SKIP,
// então a corrente pós-cleanroom inteira fica verde num ambiente que não consegue nem
// cobrar nem verificar um pagamento.
//
// SKIP é a semântica certa quando o insumo é opcional. Deixa de ser quando o ambiente
// declarou que oferece a capacidade — aí a ausência é defeito, e um defeito que reporta
// sucesso é pior que um que reporta falha.

// stripePreconditioner é o que os dois seeds de Stripe têm em comum.
//
// Declarada aqui, no teste, e não na produção: é o teste que precisa tratar os dois
// uniformemente. Criar a interface no código de produção para servir só ao teste seria
// abstração sem segundo consumidor — o que `rules/parsimony-ladder.md` rung 1 recusa.
type stripePreconditioner interface {
	precondition(Deps) (Result, bool)
}

func seedsDeStripe() map[string]stripePreconditioner {
	return map[string]stripePreconditioner{
		"webhook-secret": &StripeWebhookSecretSeed{},
		"api-keys":       &StripeAPIKeysSeed{},
	}
}

// TestSeedFalhaSemCredencialQuandoACobrancaEExigida — o caso que inverte o resultado.
func TestSeedFalhaSemCredencialQuandoACobrancaEExigida(t *testing.T) {
	for nome, s := range seedsDeStripe() {
		t.Run(nome, func(t *testing.T) {
			d := Deps{Creds: nil, Policy: NewEnvRequirementPolicy("dev-public")}
			res, parou := s.precondition(d)
			if !parou {
				t.Fatal("sem credencial o seed não pode seguir para a mutação")
			}
			if res.Verdict != FAIL {
				t.Fatalf("um ambiente que declara cobrança e não tem a credencial está "+
					"defeituoso, não configurado; queria FAIL, veio %s (%s)", res.Verdict, res.Reason)
			}
			if res.Verdict.ExitCode() == 0 {
				t.Error("o código de saída precisa ser diferente de zero, senão a corrente " +
					"continua verde e nada disto muda")
			}
		})
	}
}

// TestSeedPulaSemCredencialQuandoNaoEExigida é a proteção contra regressão.
//
// O hermético roda sem credencial de Stripe DE PROPÓSITO. Se esta mudança o quebrasse,
// teria trocado um verde falso por um vermelho falso, que não é progresso.
func TestSeedPulaSemCredencialQuandoNaoEExigida(t *testing.T) {
	for nome, s := range seedsDeStripe() {
		t.Run(nome, func(t *testing.T) {
			for _, amb := range []string{"", "standalone-multi"} {
				d := Deps{Creds: nil, Policy: NewEnvRequirementPolicy(amb)}
				res, parou := s.precondition(d)
				if !parou || res.Verdict != SKIP {
					t.Errorf("ambiente %q não promete cobrar; queria SKIP, veio %s", amb, res.Verdict)
				}
			}
		})
	}
}

// TestSeedSemPoliticaPreservaOComportamentoAnterior — nil-safe de ponta a ponta.
func TestSeedSemPoliticaPreservaOComportamentoAnterior(t *testing.T) {
	for nome, s := range seedsDeStripe() {
		t.Run(nome, func(t *testing.T) {
			res, _ := s.precondition(Deps{Creds: nil})
			if res.Verdict != SKIP {
				t.Errorf("Deps sem política deve pular como antes; veio %s", res.Verdict)
			}
		})
	}
}

// TestPlaceholderTambemFalhaQuandoExigido fecha o segundo caminho para o mesmo estado.
//
// Há DOIS jeitos de o insumo faltar: o arquivo ausente e o valor-placeholder. Corrigir só
// o primeiro deixa o segundo produzindo exatamente o mesmo verde falso — e o segundo é o
// mais provável em produção, porque um arquivo presente parece configurado.
func TestPlaceholderTambemFalhaQuandoExigido(t *testing.T) {
	creds := &Creds{}
	creds.Stripe.WebhookSecret = stripePlaceholder
	creds.Stripe.APIKey = stripePlaceholder

	for nome, s := range seedsDeStripe() {
		t.Run(nome, func(t *testing.T) {
			d := Deps{Creds: creds, Policy: NewEnvRequirementPolicy("dev-public")}
			res, parou := s.precondition(d)
			if !parou || res.Verdict != FAIL {
				t.Fatalf("placeholder é ausência com aparência de configuração; queria FAIL, veio %s (%s)",
					res.Verdict, res.Reason)
			}
		})
	}
}

// TestARazaoDoFailDizOQueFazer — uma falha que não diz o que fazer vira uma escavação.
func TestARazaoDoFailDizOQueFazer(t *testing.T) {
	for nome, s := range seedsDeStripe() {
		t.Run(nome, func(t *testing.T) {
			d := Deps{Creds: nil, Policy: NewEnvRequirementPolicy("dev-public")}
			res, _ := s.precondition(d)
			r := strings.ToLower(res.Reason)
			if !strings.Contains(r, "dev-credentials.json") {
				t.Errorf("a razão precisa nomear o arquivo que falta; veio %q", res.Reason)
			}
			if !strings.Contains(r, string(CapabilityBilling)) {
				t.Errorf("a razão precisa nomear a capacidade exigida; veio %q", res.Reason)
			}
		})
	}
}
