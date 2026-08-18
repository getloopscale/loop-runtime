// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trufflehogscan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mk(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func found(t *testing.T, lines ...string) []Finding {
	t.Helper()
	fs, err := ParseFindings(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestCandidatoForaDaBaseENovo(t *testing.T) {
	fs := found(t,
		line("Postgres", "a.md", pgURI(), false),
		line("Docker", "b.yml", "novo", false))
	b := Baseline{Entries: []Entry{{ID: fs[0].ID(), Detector: "Postgres", File: "a.md"}}}

	novos := b.Unadjudicated(fs)
	if len(novos) != 1 || novos[0].Detector != "Docker" {
		t.Fatalf("só o candidato ausente da base é novo; got %+v", novos)
	}
}

// Entrada obsoleta é higiene da catraca — REPORTA, não reprova. Um segredo removido da
// árvore não pode virar motivo para o gate travar.
func TestEntradaDaBaseNaoEncontradaEObsoleta(t *testing.T) {
	fs := found(t, line("Postgres", "a.md", pgURI(), false))
	b := Baseline{Entries: []Entry{
		{ID: fs[0].ID(), Detector: "Postgres", File: "a.md"},
		{ID: "0000000000000000", Detector: "Eraser", File: "sumiu.md"},
	}}
	if st := b.Stale(fs); len(st) != 1 || st[0].Detector != "Eraser" {
		t.Fatalf("entrada sem achado correspondente é obsoleta; got %+v", st)
	}
	if n := b.Unadjudicated(fs); len(n) != 0 {
		t.Fatalf("nada é novo aqui; got %+v", n)
	}
}

// EC-2. Apagar o arquivo não pode virar bypass: ausente é FALHA, nunca base vazia implícita.
func TestBaseAusenteNaoEBaseVazia(t *testing.T) {
	_, err := LoadBaseline(filepath.Join(t.TempDir(), "nao-existe.json"))
	if !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("base ausente precisa reprovar com erro tipado; err=%v", err)
	}
}

func TestBaseIlegivelReprova(t *testing.T) {
	if _, err := LoadBaseline(mk(t, "{isto não é json")); err == nil {
		t.Fatal("base ilegível não pode ser lida como vazia")
	}
}

// EC-1. O caminho pelo qual o mecanismo viraria o oposto de si mesmo.
func TestWriteRecusaEnquantoHouverVivo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.json")
	fs := found(t, line("DigitalOceanToken", "x.ts", fakeSecret(), true))
	if err := WriteBaseline(p, fs); !errors.Is(err, ErrLiveSecret) {
		t.Fatalf("regravar não pode silenciar credencial viva; err=%v", err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("a recusa precisa ser ANTES de escrever — o arquivo não pode existir")
	}
}

func TestBaseGravadaEDeterministicaESemSegredo(t *testing.T) {
	s := fakeSecret()
	fs := found(t,
		line("URI", "z.md", credURI(), false),
		line("Postgres", "a.md", pgURI(), false),
		line("DigitalOceanToken", "m.ts", s, false))

	d := t.TempDir()
	p1, p2 := filepath.Join(d, "1.json"), filepath.Join(d, "2.json")
	for _, p := range []string{p1, p2} {
		if err := WriteBaseline(p, fs); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Fatal("regravar duas vezes precisa dar bytes idênticos — senão o diff é ruído")
	}
	if strings.Contains(string(a), s) || strings.Contains(string(a), s[:20]) {
		t.Fatal("a base versionada carrega material de segredo")
	}
	loaded, err := LoadBaseline(p1)
	if err != nil || len(loaded.Entries) != 3 {
		t.Fatalf("ida e volta: %v %+v", err, loaded)
	}
	if loaded.Entries[0].File > loaded.Entries[2].File {
		t.Fatal("as entradas precisam sair ordenadas")
	}
}

// Achados duplicados (o mesmo segredo em vários commits) são UM candidato.
func TestAchadosRepetidosSaoUmaEntrada(t *testing.T) {
	l := line("Postgres", "a.md", pgURI(), false)
	fs := found(t, l, l, l)
	p := filepath.Join(t.TempDir(), "b.json")
	if err := WriteBaseline(p, fs); err != nil {
		t.Fatal(err)
	}
	b, _ := LoadBaseline(p)
	if len(b.Entries) != 1 {
		t.Fatalf("179 achados são 65 identidades; got %d", len(b.Entries))
	}
}
