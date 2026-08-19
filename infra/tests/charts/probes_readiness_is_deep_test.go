// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Prontidao e vivacidade NAO podem apontar para o mesmo handler incondicional (B-135).
//
// `/api/v1/health` responde 200 sempre — e' o certo para VIVACIDADE ("o processo esta'
// vivo"). Usa-lo tambem na prontidao deixa o pod `Ready` com o banco fora, recebendo
// trafego e devolvendo erro em 27 das 36 superficies. Nao existe estado observavel em que
// a instancia esteja viva e FORA do Service, que e' a unica coisa que prontidao serve
// para fazer.
//
// O teste afirma a FORMA, nao o caminho exato: exige que os dois difiram e que a prontidao
// aponte para um caminho de verificacao profunda. Fixar o literal faria qualquer renomeacao
// de rota reprovar sem que nada tivesse piorado.
func TestProntidaoEVivacidadeNaoCompartilhamOMesmoHandler(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		// FALHA, nao skip: `helm` e' pre-requisito declarado do repositorio, e um skip
		// apagaria esta guarda em silencio na maquina onde ela mais importa.
		t.Fatal("helm nao esta' no PATH, e e' pre-requisito declarado do repositorio")
	}
	raiz := repoRoot(t)

	for _, chart := range []string{
		filepath.Join("charts", "loopzero-api"),
		filepath.Join("infra", "helm", "loopzero-api"),
	} {
		t.Run(chart, func(t *testing.T) {
			out, err := exec.Command("helm", "template", "t", filepath.Join(raiz, chart)).CombinedOutput()
			if err != nil {
				t.Fatalf("helm template falhou: %v\n%s", err, truncate(string(out), 800))
			}
			vivo := caminhoDaSonda(string(out), "livenessProbe:")
			pronto := caminhoDaSonda(string(out), "readinessProbe:")
			if vivo == "" || pronto == "" {
				t.Skipf("chart nao declara as duas sondas (liveness=%q readiness=%q)", vivo, pronto)
			}
			if vivo == pronto {
				t.Errorf("vivacidade e prontidao apontam para o MESMO caminho (%q). O handler de "+
					"vivacidade responde 200 incondicionalmente, entao a prontidao nunca tira do "+
					"Service uma instancia que nao consegue servir (B-135).", vivo)
			}
			if !strings.Contains(pronto, "ready") && !strings.Contains(pronto, "deep") {
				t.Errorf("a prontidao aponta para %q, que nao parece verificacao profunda. O que "+
					"ela precisa refletir e' a capacidade DESTA instancia de servir.", pronto)
			}
		})
	}
}

// caminhoDaSonda devolve o `path:` que aparece logo apos o marcador da sonda.
func caminhoDaSonda(rendered, marcador string) string {
	i := strings.Index(rendered, marcador)
	if i < 0 {
		return ""
	}
	janela := rendered[i:]
	if len(janela) > 400 {
		janela = janela[:400]
	}
	j := strings.Index(janela, "path:")
	if j < 0 {
		return ""
	}
	resto := janela[j+len("path:"):]
	if fim := strings.IndexByte(resto, '\n'); fim >= 0 {
		resto = resto[:fim]
	}
	return strings.TrimSpace(resto)
}
