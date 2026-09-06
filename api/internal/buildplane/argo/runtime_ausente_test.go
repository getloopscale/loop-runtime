// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"
)

// Campo AUSENTE e valor NAO SUPORTADO sao erros diferentes e tinham a mesma
// mensagem: `loopzero: runtime "" is not supported`. Ela nomeia o que o usuario NAO
// escreveu como se fosse coisa que ele escreveu errado, e nao diz o que escrever
// (B-154).
func TestRuntimeAusenteDizQueOCampoFaltaEOQueEscrever(t *testing.T) {
	c := (&unsupportedStrategy{runtime: ""}).failStep("validate-workspace")
	if c == nil {
		t.Fatal("nenhum passo emitido")
	}
	msg := strings.Join(c.Args, "\n")

	for _, exigido := range []string{"runtime", "loopzero.yaml"} {
		if !strings.Contains(msg, exigido) {
			t.Errorf("a mensagem de campo ausente nao menciona %q:\n%s", exigido, msg)
		}
	}
	if !strings.Contains(msg, "python") || !strings.Contains(msg, "node") {
		t.Errorf("a mensagem nao diz o que escrever:\n%s", msg)
	}
	// O que separa este caso do outro: nao pode acusar um valor que ninguem deu.
	if strings.Contains(msg, `runtime "" is not supported`) {
		t.Errorf("campo ausente segue sendo reportado como valor nao suportado:\n%s", msg)
	}
}

// A contrapartida: valor de verdade nao suportado continua sendo reportado como
// tal, nomeando o valor. Sem esta assercao, "consertar" o caso ausente poderia
// apagar a distincao no outro sentido.
func TestRuntimeNaoSuportadoSegueNomeandoOValor(t *testing.T) {
	c := (&unsupportedStrategy{runtime: "rust"}).failStep("validate-workspace")
	if c == nil {
		t.Fatal("nenhum passo emitido")
	}
	if !strings.Contains(strings.Join(c.Args, "\n"), "rust") {
		t.Errorf("a mensagem nao nomeia o valor recusado:\n%s", strings.Join(c.Args, "\n"))
	}
}
