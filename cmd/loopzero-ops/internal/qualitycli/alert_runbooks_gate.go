// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/alertrunbooks"
)

func init() {
	register(Gate{
		Name:    "alert-runbooks",
		Summary: "Every Prometheus alert has a linked runbook annotation.",
		// The gate is right; the tree is not. Fix the finding, then wire.
		// Re-measured 2026-08-06 with the counts, so the size of each debt is on
		// the record rather than implied.
		//
		// 33 alerts without runbook_url. Worth knowing before "just add the
		// annotation": the URLs the conformant alerts already carry are 404 —
		// https://docs.loopzero.example/runbooks/vault-auth-failure-spike returns 404
		// while the site root returns 200. The gate checks that the string is
		// present, not that a runbook exists, so adding 33 more dead links would
		// buy a green gate and nothing else. An on-call operator following a 404 at
		// 3am is worse off than one who knows there is no runbook.
		// Duas correcoes medidas em 2026-08-19 (B-161, B-164), porque o paragrafo acima
		// contava metade da historia:
		//
		//  1. O gate JA' distingue link de runbook, na parte que da' para provar sem rede:
		//     caminho deste repositorio que nao existe em disco, e URL do GitHub em org
		//     diferente da canonica (`getloopscale`, tirada do caminho de modulo dos go.mod).
		//     O que ele nao pode afirmar — se `docs.loopzero.example/runbooks/x` esta' viva —
		//     fica CONTADO como nao-verificavel em vez de silenciado.
		//
		//  2. "33 alertas" era o numero de UM diretorio. O B-164 fechou esse escopo: o gate
		//     varre os QUATRO diretorios de `DefaultRulesDirs`. Re-medido 2026-09-06 (B-067):
		//     110 + 86 + 19 + 18 = **233 sites, e o gate ve os 233** — o "47%" acima descrevia
		//     o estado ANTERIOR ao conserto e ficou no presente do indicativo depois dele.
		//
		//  3. Sites e alertas nao sao o mesmo numero, e confundi-los infla a divida. Os 233
		//     sites sao **145 alertas distintos**: `infra/helm/...` e `charts/...` sao copias
		//     espelhadas, entao 88 sites repetem um alerta ja' contado. Na parte que reprova,
		//     a diferenca e' 46 sites para **37 alertas**. A mensagem do gate agora diz os
		//     dois numeros, e a violacao carrega o diretorio — sem ele, os 9 pares espelhados
		//     saiam como linhas identicas e consertar uma copia deixava a outra reprovando
		//     com a mesma string.
		Wiring: failingGate,
		Setup:  setupAlertRunbooks,
	})
}

func setupAlertRunbooks(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o := alertrunbooks.Check(alertrunbooks.Config{Root: inv.Root})
		if !o.Pass {
			for _, v := range o.Violations {
				inv.Stdout.Printf("RUNBOOK-MISSING VIOLATION: %s\n", v)
			}
			// SITES and ALERTS are different numbers, and the old message said "alert(s)"
			// while counting sites. Nine alert files exist twice in the tree
			// (`infra/helm/...` and the mirrored copy in `charts/...`), so the site count
			// is higher than the alert count — measured 2026-09-06: 46 sites, 37 alerts.
			// Calling 46 "alerts without runbook" inflates the debt by 24%.
			inv.Stderr.Printf("\nFAIL — %d alert(s) without runbook annotation, in %d site(s).\n",
				o.DistinctAlerts(), len(o.Violations))
			if o.DistinctAlerts() != len(o.Violations) {
				inv.Stderr.Printf("(%d site(s) are mirrored copies of the same alert — fix every site.)\n",
					len(o.Violations)-o.DistinctAlerts())
			}
			inv.Stderr.Println("To fix: add a `runbook_url:` annotation linking to recovery docs, OR add the")
			inv.Stderr.Println("alert name to ExceptionList (cmd/loopzero-ops/internal/quality/alertrunbooks) with an explanation.")
			return Fail, nil
		}
		inv.Stdout.Println("PASS — every alert has a runbook (or is in the exception list).")
		return Pass, nil
	}
}
