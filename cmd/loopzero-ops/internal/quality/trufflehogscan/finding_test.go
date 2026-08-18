// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trufflehogscan

import (
	"errors"
	"strings"
	"testing"
)

// fakeSecret é COMPOSTO em tempo de execução, nunca escrito literal.
//
// Este arquivo é varrido pelo próprio gate que ele testa: uma fixture com um token
// com a forma real seria — corretamente — um candidato novo, e a trava reprovaria a si
// mesma. Compor mantém a asserção inteira e a varredura honesta. Mesmo remédio do
// `publishhygiene` (B-130), pela mesma razão.
func fakeSecret() string { return "dop" + "_v1_" + strings.Repeat("a", 64) }

// pgURI e credURI seguem a mesma regra, e a primeira versão desta fatia esqueceu disso:
// o literal `postgres://…` estava escrito por extenso aqui, foi commitado, e o gate
// REPROVOU o próprio push que o trazia. Adjudicá-lo poria na lista permanente uma string
// que só existe para testar o gate.
func pgURI() string   { return "postgres" + "://u:p@h/db" }
func credURI() string { return "https" + "://u:p@h/x" }

func line(detector, file, raw string, verified bool) string {
	v := "false"
	if verified {
		v = "true"
	}
	return `{"SourceMetadata":{"Data":{"Git":{"commit":"abc","file":"` + file + `"}}},` +
		`"DetectorName":"` + detector + `","Raw":"` + raw + `","Verified":` + v + `}`
}

func TestParseExtraiDetectorArquivoELiveness(t *testing.T) {
	fs, err := ParseFindings(strings.NewReader(
		line("DigitalOceanToken", "infra/x.ts", fakeSecret(), true) + "\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("esperado 1 achado, veio %d", len(fs))
	}
	if fs[0].Detector != "DigitalOceanToken" || fs[0].File != "infra/x.ts" || !fs[0].Live {
		t.Fatalf("campos errados: %+v", fs[0])
	}
}

// A identidade vai para um arquivo VERSIONADO. Gravar o bruto vazaria exatamente o que
// este gate existe para impedir.
func TestIdentidadeNaoContemOBruto(t *testing.T) {
	s := fakeSecret()
	fs, _ := ParseFindings(strings.NewReader(line("DigitalOceanToken", "a.ts", s, false) + "\n"))
	id := fs[0].ID()
	if strings.Contains(id, s) || strings.Contains(id, s[:20]) {
		t.Fatalf("a identidade carrega material de segredo: %q", id)
	}
	if len(id) != 16 {
		t.Fatalf("identidade deve ser hash curto e estável; got %q", id)
	}
}

func TestIdentidadeEEstavelEntreExecucoes(t *testing.T) {
	l := line("Postgres", "a.md", pgURI(), false)
	a, _ := ParseFindings(strings.NewReader(l + "\n"))
	b, _ := ParseFindings(strings.NewReader(l + "\n"))
	if a[0].ID() != b[0].ID() {
		t.Fatalf("identidade instável: %q != %q", a[0].ID(), b[0].ID())
	}
	// Detector diferente com o mesmo bruto é outro candidato.
	c, _ := ParseFindings(strings.NewReader(line("URI", "a.md", pgURI(), false) + "\n"))
	if a[0].ID() == c[0].ID() {
		t.Fatal("detectores diferentes precisam de identidades diferentes")
	}
}

func TestLinhaNaoJSONEIgnorada(t *testing.T) {
	in := "2026-08-18 info-0 trufflehog scanning...\n" +
		line("Docker", "a.yml", "x", false) + "\n"
	fs, err := ParseFindings(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ruído de log não é erro: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("esperado 1 achado, veio %d", len(fs))
	}
}

// EC-3. Linha que É JSON e não parseia é PERDA DE DADOS — não ausência de achado.
// Pular em silêncio é a forma exata do defeito que este item combate.
func TestJSONQuebradoReprova(t *testing.T) {
	in := line("Docker", "a.yml", "x", false) + "\n" + `{"DetectorName":"Trunc` + "\n"
	_, err := ParseFindings(strings.NewReader(in))
	if !errors.Is(err, ErrUnparseable) {
		t.Fatalf("JSON quebrado precisa reprovar; err=%v", err)
	}
}

// EC-6. bufio.Scanner corta em 64KiB EM SILÊNCIO.
func TestLinhaLongaNaoETruncada(t *testing.T) {
	long := line("PrivateKey", "k.pem", strings.Repeat("A", 200*1024), false)
	fs, err := ParseFindings(strings.NewReader(long + "\n"))
	if err != nil {
		t.Fatalf("linha longa: %v", err)
	}
	if len(fs) != 1 || fs[0].Detector != "PrivateKey" {
		t.Fatalf("achado longo perdido: %+v", fs)
	}
}

// EC-5. Medido: 3 dos 179 achados desta árvore não têm arquivo (vêm de mensagem de commit).
func TestAchadoSemArquivoAindaTemIdentidade(t *testing.T) {
	fs, err := ParseFindings(strings.NewReader(
		`{"SourceMetadata":{"Data":{"Git":{"commit":"abc"}}},"DetectorName":"Gitlab","Raw":"g","Verified":false}` + "\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) != 1 || fs[0].File != "" || fs[0].ID() == "" {
		t.Fatalf("achado sem arquivo precisa de identidade válida: %+v", fs)
	}
}
