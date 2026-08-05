// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import "testing"

// A migração 000058 escreveu o invariante em PROSA:
//
//	"lens_fired_alerts is the dangerous half of this migration. Its CHECK rejects the history
//	 INSERT that runs INSIDE the fire transaction — so the (correct) rollback undoes the status
//	 flip and the rule retries forever, never delivering. The feature would fail exactly on the
//	 path it exists for, while the rule reads 'OK' on the customer's screen. Both CHECKs move
//	 together or neither does."
//
// Um comentário não impede nada. Quem escrever a próxima migração de métrica pode ampliar só
// `lens_alert_rules` — e o modo de falha é o pior que existe: **silencioso e permanente**. A regra
// dispara, o INSERT de histórico viola o CHECK, a transação inteira volta atrás, o status volta a
// 'OK', e o avaliador tenta de novo no próximo tick. Para sempre. Ninguém é avisado, e a tela diz
// que está tudo bem.
//
// Nada em CI detectaria isso: as duas tabelas compilam, os testes de unidade do avaliador passam
// (não tocam no banco), e o defeito só aparece quando um alerta REAL precisa disparar em produção —
// que é exatamente quando ninguém está olhando para o CHECK.
//
// Este teste é a única coisa entre o comentário e esse silêncio.
func TestLensAlertMetricChecks_MovemEmLockstep(t *testing.T) {
	regras := effectiveMetricCheckSet(t, "lens_alert_rules")
	historico := effectiveMetricCheckSet(t, "lens_fired_alerts")

	for m := range regras {
		if !historico[m] {
			t.Errorf("métrica %q é aceita por lens_alert_rules mas NÃO por lens_fired_alerts — "+
				"uma regra com esta métrica dispara, o INSERT de histórico viola o CHECK, a transação "+
				"de disparo volta atrás e a regra tenta para sempre sem entregar, com a tela lendo 'OK'. "+
				"Amplie os DOIS CHECKs na mesma migração.", m)
		}
	}
	for m := range historico {
		if !regras[m] {
			t.Errorf("métrica %q é aceita por lens_fired_alerts mas NÃO por lens_alert_rules — "+
				"o histórico admite algo que nenhuma regra pode produzir; a assimetria é sempre um "+
				"erro de migração, mesmo sendo a metade inofensiva.", m)
		}
	}
}

// O escopo de prompt do M21 DoD-5. RED antes da 000060, GREEN depois.
//
// Não basta a métrica existir: ela tem de ser aceita pelas DUAS tabelas — e é por isso que este
// teste afirma as duas explicitamente em vez de confiar no lockstep acima. Um teste que só checasse
// `lens_alert_rules` passaria com a metade perigosa faltando.
func TestMetricaDePromptEAceitaPelasDuasTabelas(t *testing.T) {
	const metrica = "prompt_resolutions"

	if !effectiveMetricCheckSet(t, "lens_alert_rules")[metrica] {
		t.Errorf("lens_alert_rules não aceita %q — sem ela não há como DEFINIR um monitor de prompt", metrica)
	}
	if !effectiveMetricCheckSet(t, "lens_fired_alerts")[metrica] {
		t.Errorf("lens_fired_alerts não aceita %q — sem ela o monitor pode ser definido mas o "+
			"incidente nunca é REGISTRADO, e o DoD-5 exige o incidente registrado", metrica)
	}
}
