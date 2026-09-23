// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"strings"
	"testing"
)

const minimo = `
[ancora]
loopzero-stripe-webhook

[piso]
1

[camadas]
seed | loopzero | a.go,b.go | test:t_test.go | pula sem credencial

[referencias]
c.go | é a guarda, não a camada
`

func TestRegistroCompletoEParseado(t *testing.T) {
	r, err := ParseRegistry(strings.NewReader(minimo))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(r.Anchor) != 1 || r.Anchor[0] != "loopzero-stripe-webhook" {
		t.Fatalf("âncora: %+v", r.Anchor)
	}
	if r.Floor != 1 {
		t.Fatalf("piso: %d", r.Floor)
	}
	if len(r.Layers) != 1 || r.Layers[0].ID != "seed" || len(r.Layers[0].Files) != 2 {
		t.Fatalf("camadas: %+v", r.Layers)
	}
	if r.Layers[0].Guard.Kind != GuardTest || r.Layers[0].Guard.Target != "t_test.go" {
		t.Fatalf("guarda: %+v", r.Layers[0].Guard)
	}
	if len(r.References) != 1 || r.References[0].Reason == "" {
		t.Fatalf("referências: %+v", r.References)
	}
}

// EC-3: uma camada é vários arquivos. Campo único forçaria ids duplicados.
func TestCamadaAceitaListaDeArquivos(t *testing.T) {
	r, _ := ParseRegistry(strings.NewReader(minimo))
	if got := r.Layers[0].Files; got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("lista de arquivos: %+v", got)
	}
}

func TestLinhaMalformadaReprova(t *testing.T) {
	for _, ruim := range []string{
		"[camadas]\nseed | loopzero | a.go | test:t_test.go\n",       // 4 campos
		"[camadas]\nseed | loopzero | a.go | test:t_test.go |\n",     // descrição vazia
		"[camadas]\nseed | loopzero |  | test:t_test.go | por quê\n", // sem arquivo
		"[camadas]\nseed | loopzero | a.go | foo:x | por quê\n",      // guarda desconhecida
		"[referencias]\nc.go\n",                                  // referência sem razão
	} {
		if _, err := ParseRegistry(strings.NewReader(ruim)); err == nil {
			t.Fatalf("linha malformada precisa reprovar: %q", ruim)
		}
	}
}

// Duas camadas com o mesmo id: uma esconde a outra, e o piso continua satisfeito.
func TestIDDuplicadoReprova(t *testing.T) {
	dup := "[camadas]\nseed | loopzero | a.go | test:t | x\nseed | loopzero | b.go | test:t | y\n"
	if _, err := ParseRegistry(strings.NewReader(dup)); err == nil {
		t.Fatal("id duplicado precisa reprovar")
	}
}

func TestComentarioELinhaVaziaSaoIgnorados(t *testing.T) {
	com := "# comentário\n\n" + minimo
	r, err := ParseRegistry(strings.NewReader(com))
	if err != nil || len(r.Layers) != 1 {
		t.Fatalf("comentário não é conteúdo: %v %+v", err, r)
	}
}

// EC-7: `Gate: X` e `gate:X` são a mesma intenção.
func TestGuardaNormalizaEspacoECaixa(t *testing.T) {
	r, err := ParseRegistry(strings.NewReader(
		"[ancora]\nx\n\n[piso]\n1\n\n[camadas]\nseed | loopzero | a.go |   Gate: billing-secrets   | x\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g := r.Layers[0].Guard
	if g.Kind != GuardGate || g.Target != "billing-secrets" {
		t.Fatalf("normalização: %+v", g)
	}
}

// EC-4: apagar linhas não pode fazer o gate passar.
func TestPisoAusenteReprova(t *testing.T) {
	if _, err := ParseRegistry(strings.NewReader("[camadas]\nseed | loopzero | a.go | test:t | x\n")); err == nil {
		t.Fatal("registro sem piso não declara o mínimo, e o mínimo é o que impede o auto-silenciamento")
	}
}

func TestAncoraVaziaReprova(t *testing.T) {
	sem := "[piso]\n1\n\n[camadas]\nseed | loopzero | a.go | test:t | x\n"
	if _, err := ParseRegistry(strings.NewReader(sem)); !errors.Is(err, ErrRegistryMalformed) {
		t.Fatalf("sem âncora não há como detectar camada nova; err=%v", err)
	}
}

func TestDebtSectionIsParsed(t *testing.T) {
	r, err := ParseRegistry(strings.NewReader(minimo + "\n[debts]\nB-033 | approved | cost ceiling\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d, ok := r.Debts["B-033"]; !ok || d.Status != "approved" || d.Closes != "cost ceiling" {
		t.Fatalf("debts: %+v", r.Debts)
	}
}

// An unknown status is refused rather than read as open or closed: a status
// the gate cannot classify must not decide whether a layer is covered.
func TestDebtWithUnknownStatusIsMalformed(t *testing.T) {
	for _, bad := range []string{
		"[debts]\nB-033 | pending | x\n",
		"[debts]\nB-033 | approved\n",
		"[debts]\nB-033 | approved | x\nB-033 | raw | y\n",
		"[debts]\nB-033 | approved | \n",
	} {
		if _, err := ParseRegistry(strings.NewReader(minimo + "\n" + bad)); !errors.Is(err, ErrRegistryMalformed) {
			t.Errorf("%q: want ErrRegistryMalformed, got %v", bad, err)
		}
	}
}
