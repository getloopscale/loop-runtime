// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"errors"
	"strings"
	"testing"
)

// fakeRenderer returns canned manifests per chart directory, so the comparison
// can be pinned without `helm` on the machine running the test.
//
// The gate must not require the toolchain it audits in order to be tested — that
// is the whole reason rendering sits behind an interface (DIP). A test that
// shelled out to helm would fail on a machine without it and teach the reader
// that the failure is environmental.
type fakeRenderer map[string]string

func (f fakeRenderer) Render(chartDir string) (string, error) {
	out, ok := f[chartDir]
	if !ok {
		return "", errors.New("fakeRenderer: sem manifesto para " + chartDir)
	}
	return out, nil
}

const manifestoComTeste = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: t-app
---
apiVersion: v1
kind: Pod
metadata:
  name: t-app-test-connection
  annotations:
    helm.sh/hook: test
`

// TestRenderedKinds is the measurement the gate rests on.
func TestRenderedKinds(t *testing.T) {
	t.Run("recolhe os kinds produzidos", func(t *testing.T) {
		got, err := renderedKinds(fakeRenderer{"c": "kind: Deployment\n---\nkind: Service\n"}, "c")
		if err != nil {
			t.Fatalf("renderedKinds: %v", err)
		}
		if len(got) != 2 || !got["Deployment"] || !got["Service"] {
			t.Errorf("kinds = %v, esperado Deployment e Service", got)
		}
	})

	t.Run("exclui o que `helm install` nao instala", func(t *testing.T) {
		// Um Pod anotado `helm.sh/hook: test` só roda em `helm test`. Contá-lo
		// reportaria uma assimetria sem efeito nenhum sobre o que fica no ar — e
		// medido nos charts reais, é exatamente a diferença que aparecia.
		got, err := renderedKinds(fakeRenderer{"c": manifestoComTeste}, "c")
		if err != nil {
			t.Fatalf("renderedKinds: %v", err)
		}
		if got["Pod"] {
			t.Error("o Pod de teste do Helm foi contado como recurso instalado")
		}
		if !got["Deployment"] {
			t.Error("o Deployment sumiu junto")
		}
	})

	t.Run("documento vazio entre separadores nao vira kind", func(t *testing.T) {
		// `helm template` emite `---` sobrando quando um bloco condicional nao
		// renderiza. Um nil no meio da sequencia nao pode virar entrada.
		got, err := renderedKinds(fakeRenderer{"c": "---\n\n---\nkind: Service\n---\n"}, "c")
		if err != nil {
			t.Fatalf("renderedKinds: %v", err)
		}
		if len(got) != 1 || !got["Service"] {
			t.Errorf("kinds = %v, esperado apenas Service", got)
		}
	})

	t.Run("falha do renderizador sobe, nao vira conjunto vazio", func(t *testing.T) {
		// Um render que falha e um chart que produz nada sao fatos diferentes, e
		// achatar o primeiro no segundo faria o gate reportar deriva total como
		// se fosse medicao.
		if _, err := renderedKinds(fakeRenderer{}, "inexistente"); err == nil {
			t.Fatal("falha do renderizador foi engolida")
		}
	})
}

// TestDiffKinds pins the comparison itself.
func TestDiffKinds(t *testing.T) {
	pub := map[string]bool{"Deployment": true, "Secret": true, "Service": true}
	boot := map[string]bool{"Deployment": true, "IngressRoute": true, "Service": true}

	soPub, soBoot := diffKinds(pub, boot)
	if strings.Join(soPub, ",") != "Secret" {
		t.Errorf("só no publicado = %v, esperado [Secret]", soPub)
	}
	if strings.Join(soBoot, ",") != "IngressRoute" {
		t.Errorf("só no bootstrap = %v, esperado [IngressRoute]", soBoot)
	}

	t.Run("conjuntos iguais nao produzem diferenca", func(t *testing.T) {
		a, b := diffKinds(pub, pub)
		if len(a) != 0 || len(b) != 0 {
			t.Errorf("conjuntos iguais produziram diferença: %v / %v", a, b)
		}
	})

	t.Run("ordem e deterministica", func(t *testing.T) {
		// Sem ordem estável a saída do gate muda a cada execução e o diff de dois
		// relatórios vira ruído.
		many := map[string]bool{"Zeta": true, "Alpha": true, "Meio": true}
		a, _ := diffKinds(many, map[string]bool{})
		if strings.Join(a, ",") != "Alpha,Meio,Zeta" {
			t.Errorf("ordem = %v, esperado alfabética", a)
		}
	})
}
