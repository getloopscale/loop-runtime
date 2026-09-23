// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billingsecrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// A camada mais silenciosa das quatro.
//
// MEDIDO em 2026-08-11: o pod do painel rodou 20h `Running` sem os dois Secrets de
// cobrança, e não emitiu um único evento. O seed ao menos imprime SKIP; o runtime ao menos
// registra uma linha no boot. `optional: true` não deixa rastro nenhum — é o que transforma
// "não temos credencial" em "subiu normalmente".
//
// O gate NÃO exige `optional: false` incondicional: um ambiente hermético monta sem
// credencial de propósito. O que ele exige é que a decisão seja um VALOR, e não um literal
// — porque um literal não pode ser diferente por ambiente, e é assim que ele acaba sendo
// `true` em todos.

func escreveChart(t *testing.T, raiz, rel, corpo string) {
	t.Helper()
	c := filepath.Join(raiz, rel)
	if err := os.MkdirAll(filepath.Dir(c), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte(corpo), 0o600); err != nil {
		t.Fatal(err)
	}
}

const montagemFixa = `
            - secretRef:
                name: loopzero-stripe-webhook
                optional: true
            - secretRef:
                name: loopzero-stripe-api-keys
                optional: true
`

const montagemPorValor = `
            - secretRef:
                name: loopzero-stripe-webhook
                optional: {{ not .Values.billing.required }}
            - secretRef:
                name: loopzero-stripe-api-keys
                optional: {{ not .Values.billing.required }}
`

// TestReprovaMontagemOpcionalFixa é o caso que hoje falha.
func TestReprovaMontagemOpcionalFixa(t *testing.T) {
	raiz := t.TempDir()
	escreveChart(t, raiz, "infra/helm/loopzero-cloud/templates/deployment.yaml", montagemFixa)

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Findings) != 2 {
		t.Fatalf("os dois segredos de cobrança estão fixos em opcional; queria 2 achados, veio %d: %+v",
			len(out.Findings), out.Findings)
	}
	if !strings.Contains(strings.ToLower(out.Findings[0].Why), "optional") {
		t.Errorf("a razão precisa nomear o que está errado; veio %q", out.Findings[0].Why)
	}
}

// TestAceitaMontagemDecididaPorValor — a forma correta passa.
func TestAceitaMontagemDecididaPorValor(t *testing.T) {
	raiz := t.TempDir()
	escreveChart(t, raiz, "infra/helm/loopzero-cloud/templates/deployment.yaml", montagemPorValor)

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Findings) != 0 {
		t.Errorf("uma montagem decidida por valor pode ser opcional no hermético e "+
			"obrigatória no público; não deveria reprovar: %+v", out.Findings)
	}
}

// TestVarreOsDoisChartsIrmaos trava a regra dos charts sobrepostos.
//
// `CLAUDE.md § Helm chart dual system`: `loopzero-cloud` existe em `infra/helm/` e em
// `charts/`, e corrigir um só deixa o outro publicando o defeito. Um gate que olhasse
// apenas o primeiro passaria enquanto o irmão continua quebrado — e o irmão é justamente
// o publicado por OCI, que é o que os consumidores instalam.
func TestVarreOsDoisChartsIrmaos(t *testing.T) {
	raiz := t.TempDir()
	escreveChart(t, raiz, "infra/helm/loopzero-cloud/templates/deployment.yaml", montagemPorValor)
	escreveChart(t, raiz, "charts/loopzero-cloud/templates/deployment.yaml", montagemFixa)

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Findings) != 2 {
		t.Fatalf("o irmão publicado continua fixo; queria 2 achados vindos de charts/, veio %d",
			len(out.Findings))
	}
	for _, f := range out.Findings {
		if !strings.Contains(f.File, "charts/loopzero-cloud") {
			t.Errorf("o achado devia apontar o irmão quebrado, veio %q", f.File)
		}
	}
}

// TestSemChartNaoInventaAchado — ausência de arquivo não é violação.
func TestSemChartNaoInventaAchado(t *testing.T) {
	out, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("um repositório sem o chart não é erro: %v", err)
	}
	if len(out.Findings) != 0 {
		t.Errorf("sem chart não há o que reprovar, veio %+v", out.Findings)
	}
}

// TestRepositorioRealPassa é o gate contra o repositório de verdade.
//
// Falha HOJE, de propósito: é a prova de que o defeito medido existe e de que o gate o
// enxerga. Passa quando as linhas 123-132 e 84-85 forem corrigidas.
func TestRepositorioRealPassa(t *testing.T) {
	raiz := qtestutil.Root(t)
	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check no repositório real: %v", err)
	}
	for _, f := range out.Findings {
		t.Errorf("%s:%d — %s", f.File, f.Line, f.Why)
	}
}
