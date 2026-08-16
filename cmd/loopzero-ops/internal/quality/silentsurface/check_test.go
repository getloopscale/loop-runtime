// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package silentsurface

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// umaGravadaEUmaMuda tem a forma canônica (grava nos dois ramos) ao lado da
// forma que a catraca existe para pegar.
const umaGravadaEUmaMuda = `package server
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	if cfg.GitHubWebhookHandler != nil {
		r.Post("/api/v1/webhooks/github", cfg.GitHubWebhookHandler.ServeHTTP)
		_ = cfg.Surfaces.Record(surfaces.GitHubWebhook, true, "")
	} else {
		_ = cfg.Surfaces.Record(surfaces.GitHubWebhook, false, "segredo ausente")
	}
	if cfg.AdminEvictor != nil {
		r.Post("/api/v1/admin/evict", cfg.AdminEvictor.Handle)
	}
	return r
}`

func TestAnalyze_ContaSuperficieGuardadaSemGravacao(t *testing.T) {
	got, err := Analyze(umaGravadaEUmaMuda)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got.Silent) != 1 {
		t.Fatalf("mudas = %d, esperado 1: %+v", len(got.Silent), got.Silent)
	}
	if got.Silent[0].Field != "AdminEvictor" {
		t.Errorf("campo = %q, esperado AdminEvictor", got.Silent[0].Field)
	}
	if got.Guarded != 2 {
		t.Errorf("guardadas = %d, esperado 2", got.Guarded)
	}
}

func TestAnalyze_GuardaSemRotaNaoConta(t *testing.T) {
	// Nem todo `if cfg.X != nil` registra superfície: alguns só montam
	// middleware ou configuram um handler já registrado. Contá-los inflaria a
	// linha de base com trabalho que não existe, e uma catraca que mede o
	// número errado ensina a ignorá-la.
	const semRota = `package server
func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	if cfg.IPRateLimit != nil {
		r.Use(cfg.IPRateLimit.Middleware)
	}
	return r
}`
	got, err := Analyze(semRota)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got.Silent) != 0 {
		t.Errorf("middleware foi contado como superfície: %+v", got.Silent)
	}
}

func TestEvaluate_RecusaAumentoSobreALinhaDeBase(t *testing.T) {
	out := Evaluate(Outcome{Guarded: 5, Silent: []Site{
		{Field: "A", Line: 1}, {Field: "B", Line: 2}, {Field: "C", Line: 3}, {Field: "D", Line: 4},
	}}, 3)
	if out.Pass {
		t.Fatal("aumento sobre a linha de base foi aprovado")
	}
	if !strings.Contains(out.Summary(), "4") || !strings.Contains(out.Summary(), "3") {
		t.Errorf("o resumo não confronta medido com base: %s", out.Summary())
	}
}

func TestEvaluate_DiminuirAprova(t *testing.T) {
	// Homing de verdade nunca é reprovado — é o ponto de a catraca ser
	// monotônica em vez de uma igualdade.
	out := Evaluate(Outcome{Guarded: 5, Silent: []Site{{Field: "A", Line: 1}}}, 3)
	if !out.Pass {
		t.Fatalf("diminuir a dívida foi reprovado: %s", out.Summary())
	}
}

func TestEvaluate_IgualARBaseAprova(t *testing.T) {
	out := Evaluate(Outcome{Guarded: 5, Silent: []Site{{Field: "A", Line: 1}}}, 1)
	if !out.Pass {
		t.Fatalf("igual à base foi reprovado: %s", out.Summary())
	}
}

func TestCheck_RoteadorSemGuardaNenhumaEhFalhaENaoAprovacao(t *testing.T) {
	// A mesma invariante do `status-writer-contract`: "não há o que checar" e
	// "está tudo limpo" são fatos diferentes, e reportar o segundo pelo
	// primeiro é como um gate fica mudo sem ninguém notar.
	//
	// A asserção é sobre `Check` e não sobre `Analyze`: uma fixture com zero
	// guardas é legítima — é para isso que `Analyze` existe —, mas o roteador
	// REAL sem nenhuma significa que ele mudou de forma e o gate parou de o
	// reconhecer.
	root := t.TempDir()
	dir := filepath.Join(root, filepath.Dir(RouterPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := "package server\nfunc NewRouter(cfg Config) {}\n"
	if err := os.WriteFile(filepath.Join(root, RouterPath), []byte(src), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("um roteador sem guarda nenhuma foi aceito como limpo — " +
			"nesse ponto o gate deixou de medir o que diz medir")
	}
}

func TestCheck_RoteadorAusenteFalhaAlto(t *testing.T) {
	if _, err := Check(Config{Root: t.TempDir()}); err == nil {
		t.Fatal("roteador ausente foi aceito")
	}
}

func TestAnalyze_FonteInvalidaFalhaAlto(t *testing.T) {
	if _, err := Analyze("isto não é Go"); err == nil {
		t.Fatal("fonte inválida foi aceita")
	}
}
