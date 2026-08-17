// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package envprefixparity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// As SEIS variantes que a revisão da fatia `slice-11` mediu em 2026-08-12 — todas
// passavam. Elas estavam enumeradas em prosa no doc-comment; agora são fixtures, que
// é a terceira bala do DoD do B-090.
//
// A escolha de desenho que as fecha é a segunda bala: **a asserção acompanha a
// EXPRESSÃO COMPARADA, não a forma do laço.** O escopo antigo era "dentro de um
// `range` sobre o símbolo", e por isso bastava trocar a forma do laço — ou tirar o
// laço — para escapar.

// variantFile monta um arquivo com a função de matching escrita de uma das formas.
func variantFile(body string) string {
	return `package sample

import (
	"bytes"
	"slices"
	"strings"
	str "strings"
)

var reservedEnvKeyPrefixes = []string{"AWS_", "DO_"}

var _, _, _ = bytes.ToUpper, slices.ContainsFunc, str.ToUpper

func matches(key string) bool {
` + body + `
}
`
}

func siteFor(t *testing.T, body string) (string, []Site) {
	t.Helper()
	root := t.TempDir()
	rel := "sample/sample.go"
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(variantFile(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, []Site{{RelPath: rel, Symbol: "reservedEnvKeyPrefixes"}}
}

func TestSixEvasionVariantsAreDetected(t *testing.T) {
	variants := map[string]string{
		// A que um refactor normal produz: o fold içado para ACIMA do laço.
		"içada": `	uk := strings.ToUpper(key)
	for _, p := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(uk, p) {
			return true
		}
	}
	return false`,

		// Laço por índice — não é um *ast.RangeStmt.
		"índice": `	for i := 0; i < len(reservedEnvKeyPrefixes); i++ {
		if strings.HasPrefix(strings.ToUpper(key), reservedEnvKeyPrefixes[i]) {
			return true
		}
	}
	return false`,

		// `range` sobre uma variável que aliasa o slice.
		"aliasada": `	ps := reservedEnvKeyPrefixes
	for _, p := range ps {
		if strings.HasPrefix(strings.ToUpper(key), p) {
			return true
		}
	}
	return false`,

		// Sem laço nenhum.
		"sem laço": `	return slices.ContainsFunc(reservedEnvKeyPrefixes, func(p string) bool {
		return strings.HasPrefix(strings.ToUpper(key), p)
	})`,

		// Pacote com alias no import.
		"pacote aliasado": `	for _, p := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(str.ToUpper(key), p) {
			return true
		}
	}
	return false`,

		// Outro pacote inteiro.
		"outro pacote": `	for _, p := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(string(bytes.ToUpper([]byte(key))), p) {
			return true
		}
	}
	return false`,
	}

	for name, body := range variants {
		t.Run(name, func(t *testing.T) {
			root, sites := siteFor(t, body)
			found, err := checkCaseMatching(root, sites)
			if err != nil {
				t.Fatalf("checkCaseMatching: %v", err)
			}
			if len(found) == 0 {
				t.Fatalf("a variante %q dobra a caixa da chave comparada e precisa ser detectada", name)
			}
		})
	}
}

// A FRONTEIRA que impede a mudança de virar ruído: o fold LEGÍTIMO, sobre a palavra
// da instrução do Dockerfile, que a especificação torna insensível a caixa. Ele vive
// uma linha acima da comparação da chave, e um scan de texto não os separa.
//
// O discriminador é o OUTRO argumento: literal é instrução, variável do laço é chave.
func TestLegitimateInstructionFoldIsNotAFinding(t *testing.T) {
	root, sites := siteFor(t, `	line := key
	if !strings.HasPrefix(strings.ToUpper(line), "ENV ") && !strings.HasPrefix(strings.ToUpper(line), "ARG ") {
		return false
	}
	for _, p := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false`)
	found, err := checkCaseMatching(root, sites)
	if err != nil {
		t.Fatalf("checkCaseMatching: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("dobrar a caixa da PALAVRA DA INSTRUÇÃO é correto por especificação e não pode ser achado; got %+v", found)
	}
}

// A forma canônica — comparação exata — continua verde. Sem esta fronteira, endurecer
// a asserção viraria uma regra que o próprio contrato não satisfaz.
func TestExactComparisonStaysGreen(t *testing.T) {
	root, sites := siteFor(t, `	for _, p := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false`)
	found, err := checkCaseMatching(root, sites)
	if err != nil {
		t.Fatalf("checkCaseMatching: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("comparação exata é o contrato; got %+v", found)
	}
}

// O passo vazio continua fechado: um site sem NENHUMA comparação contra os prefixos
// não teve a caixa verificada, e reportar sucesso sobre isso é o `PASS — 0 routes` do
// B-040 noutro lugar.
func TestSiteWithoutAnyComparisonStillFails(t *testing.T) {
	root, sites := siteFor(t, `	_ = reservedEnvKeyPrefixes
	return false`)
	if _, err := checkCaseMatching(root, sites); err == nil || !strings.Contains(err.Error(), "case axis was not verified") {
		t.Fatalf("site sem comparação alguma precisa reprovar; err=%v", err)
	}
}
