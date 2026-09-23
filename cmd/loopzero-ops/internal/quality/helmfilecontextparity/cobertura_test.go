// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmfilecontextparity

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// O escopo varrido e' DECLARADO — `Taskfile.yml` + `taskfiles/*.yml` — e os
// invocadores em script ficam de fora por razao medida, escrita em
// `globsPadrao`. Este teste fixa o denominador para que ele nao encolha em
// silencio: uma varredura que passe a alcancar menos continua reportando PASS,
// e o PASS passa a significar menos sem que nada avise (B-078).
//
// O numero e' medido, nao escolhido: 24 invocacoes com `--kube-context` no
// escopo declarado, em 2026-08-18.
func TestDenominadorDoEscopoDeclaradoNaoEncolhe(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	total := out.ComparedCount + out.SkippedParameterised
	if total < 24 {
		t.Errorf("apenas %d invocacao(oes) alcancada(s); o escopo declarado carregava 24 em "+
			"2026-08-18 — ou a varredura encolheu, ou invocacoes sairam da arvore sem que "+
			"este piso fosse remedido", total)
	}
}

// A fracao precisa aparecer de forma que ninguem a confunda com cobertura: um
// PASS sobre 4 de 28 invocacoes nao e' a mesma coisa que um PASS sobre 28 de 28,
// e o texto anterior dava os dois numeros sem nunca dizer que um e' do outro.
func TestSummaryDizAFracaoEnaoApenasOsNumeros(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	total := out.ComparedCount + out.SkippedParameterised
	// A assercao precisa exigir a FRACAO, e nao qualquer " of " — o texto anterior
	// ja' dizia "out of static reach", e uma checagem frouxa passaria por causa
	// dessa expressao sem que a fracao existisse. Foi o que aconteceu na primeira
	// versao deste teste.
	quer := fmt.Sprintf("%d of %d", out.ComparedCount, total)
	if !strings.Contains(out.Summary(), quer) {
		t.Errorf("o resumo nao diz %q — sem a fracao, um PASS sobre %d de %d le-se como cobertura:\n%s",
			quer, out.ComparedCount, total, out.Summary())
	}
}
