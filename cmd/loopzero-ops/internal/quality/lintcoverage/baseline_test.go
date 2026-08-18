// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func base(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lint-baseline.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const congelado = `{"lint":{"api":0,"cmd/loopzero-ops":221},"gofmt":{"api":70,"cmd/loopzero-ops":0}}`

func TestIgualAoCongeladoPassa(t *testing.T) {
	b, err := LoadBaseline(base(t, congelado))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if v := b.Compare(map[string]int{"api": 0, "cmd/loopzero-ops": 221}, b.Lint, "lint"); len(v.Regressions) != 0 {
		t.Fatalf("igual ao congelado passa: %+v", v.Regressions)
	}
}

// A catraca. Um módulo em 0 que ganhe UM achado reprova, mesmo com 221 congelados ao lado —
// é isso que impede a linha de base de virar permissão.
func TestAcimaDoCongeladoReprovaNomeandoOModulo(t *testing.T) {
	b, _ := LoadBaseline(base(t, congelado))
	v := b.Compare(map[string]int{"api": 1, "cmd/loopzero-ops": 221}, b.Lint, "lint")
	if len(v.Regressions) != 1 || v.Regressions[0].Module != "api" {
		t.Fatalf("regressão precisa NOMEAR o módulo: %+v", v.Regressions)
	}
	if v.Regressions[0].Was != 0 || v.Regressions[0].Now != 1 {
		t.Fatalf("a mensagem precisa carregar antes e depois: %+v", v.Regressions[0])
	}
}

// Melhorar passa — e o gate DIZ que dá para recongelar, senão o número nunca desce.
func TestAbaixoDoCongeladoPassaESinaliza(t *testing.T) {
	b, _ := LoadBaseline(base(t, congelado))
	v := b.Compare(map[string]int{"api": 0, "cmd/loopzero-ops": 200}, b.Lint, "lint")
	if len(v.Regressions) != 0 {
		t.Fatalf("melhorar não reprova: %+v", v.Regressions)
	}
	if len(v.Improvements) != 1 || v.Improvements[0].Module != "cmd/loopzero-ops" {
		t.Fatalf("a melhoria precisa aparecer para que alguém recongele: %+v", v.Improvements)
	}
}

// Módulo novo entra DECLARADO. Sem isto, um módulo criado hoje nasceria com achados
// invisíveis — que é exatamente o defeito que esta fatia conserta.
func TestModuloNovoSemEntradaReprova(t *testing.T) {
	b, _ := LoadBaseline(base(t, congelado))
	v := b.Compare(map[string]int{"api": 0, "cmd/loopzero-ops": 221, "modulo/novo": 5}, b.Lint, "lint")
	if len(v.Undeclared) != 1 || v.Undeclared[0] != "modulo/novo" {
		t.Fatalf("módulo sem entrada precisa ser reportado: %+v", v.Undeclared)
	}
}

func TestBaseAusenteReprova(t *testing.T) {
	if _, err := LoadBaseline(filepath.Join(t.TempDir(), "nao-existe.json")); !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("base ausente reprova; err=%v", err)
	}
}

func TestBaseIlegivelReprova(t *testing.T) {
	if _, err := LoadBaseline(base(t, "{isto nao e json")); err == nil {
		t.Fatal("base ilegível não pode ser lida como vazia")
	}
}

// Regravar duas vezes precisa dar bytes idênticos — senão o diff é ruído e ninguém revisa.
func TestBaseEDeterministica(t *testing.T) {
	d := t.TempDir()
	lint := map[string]int{"pkg": 0, "api": 0, "cmd/loopzero-ops": 221}
	fmtc := map[string]int{"api": 70, "pkg": 30}
	p1, p2 := filepath.Join(d, "1.json"), filepath.Join(d, "2.json")
	for _, p := range []string{p1, p2} {
		if err := WriteBaseline(p, lint, fmtc); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Fatal("regravar duas vezes precisa dar bytes idênticos")
	}
	got, err := LoadBaseline(p1)
	if err != nil || got.Lint["cmd/loopzero-ops"] != 221 || got.Gofmt["api"] != 70 {
		t.Fatalf("ida e volta: %v %+v", err, got)
	}
}
