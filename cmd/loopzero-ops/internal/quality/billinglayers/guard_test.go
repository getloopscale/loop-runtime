// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func repoCom(t *testing.T, arquivos map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, corpo := range arquivos {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(corpo), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func resolvedorCom(t *testing.T, arquivos map[string]string) *GuardResolver {
	t.Helper()
	root := repoCom(t, arquivos)
	return NewGuardResolver(root, root, filepath.Join(root, "BACKLOG.md"))
}

// Um gate que ninguém roda não guarda nada — a razão de resolver por `quality.yml`
// e não pela existência do pacote.
func TestGateResolveQuandoRodado(t *testing.T) {
	r := resolvedorCom(t, map[string]string{
		"taskfiles/quality.yml": "cmds:\n  - /tmp/loopzero-ops quality billing-secrets\n",
	})
	if err := r.Resolve(Guard{Kind: GuardGate, Target: "billing-secrets"}, "loopzero"); err != nil {
		t.Fatalf("gate rodado resolve: %v", err)
	}
}

func TestGateNaoRodadoReprova(t *testing.T) {
	r := resolvedorCom(t, map[string]string{
		"taskfiles/quality.yml": "cmds:\n  - /tmp/loopzero-ops quality outra-coisa\n",
	})
	err := r.Resolve(Guard{Kind: GuardGate, Target: "billing-secrets"}, "loopzero")
	if !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("gate registrado mas não rodado não guarda nada; err=%v", err)
	}
}

func TestTesteResolveQuandoOArquivoExiste(t *testing.T) {
	r := resolvedorCom(t, map[string]string{"a/b_test.go": "package a\n"})
	if err := r.Resolve(Guard{Kind: GuardTest, Target: "a/b_test.go"}, "loopzero"); err != nil {
		t.Fatalf("teste presente resolve: %v", err)
	}
}

func TestTesteApagadoReprova(t *testing.T) {
	r := resolvedorCom(t, map[string]string{"outro.go": "package a\n"})
	if err := r.Resolve(Guard{Kind: GuardTest, Target: "a/b_test.go"}, "loopzero"); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("teste apagado reprova; err=%v", err)
	}
}

func backlogCom(status string) string {
	return "## B-033 — teto de custo   [ ]\n\ndomain: control-plane\nstatus: " + status + "\n"
}

// `planned` conta como aberto: a catraca existe para pegar FECHAMENTO sem cobertura, e um
// item planejado não está fechado. Excluí-lo faria o gate travar TODO push do repositório
// justamente na janela em que alguém implementa o conserto — custo desproporcional ao
// risco, e o jeito conhecido de uma trava ser desligada.
func TestDividaResolveEnquantoOItemEstaAberto(t *testing.T) {
	for _, st := range []string{"raw", "triaged", "planned"} {
		r := resolvedorCom(t, map[string]string{"BACKLOG.md": backlogCom(st)})
		if err := r.Resolve(Guard{Kind: GuardOpen, Target: "B-033"}, "loopzero"); err != nil {
			t.Fatalf("dívida com item %s resolve: %v", st, err)
		}
	}
}

// A CATRACA (ADR-2). Sem ela, fechar o B-033 deixaria a camada descoberta e silenciosa,
// com o registro ainda afirmando que alguém cuida dela.
func TestDividaComItemFechadoReprova(t *testing.T) {
	for _, st := range []string{"shipped", "killed"} {
		r := resolvedorCom(t, map[string]string{"BACKLOG.md": backlogCom(st)})
		err := r.Resolve(Guard{Kind: GuardOpen, Target: "B-033"}, "loopzero")
		if !errors.Is(err, ErrGuardUnresolved) {
			t.Fatalf("item %s não é dívida aberta — a guarda tem que virar gate: ou test:; err=%v", st, err)
		}
	}
}

func TestDividaComItemInexistenteReprova(t *testing.T) {
	r := resolvedorCom(t, map[string]string{"BACKLOG.md": backlogCom("triaged")})
	if err := r.Resolve(Guard{Kind: GuardOpen, Target: "B-999"}, "loopzero"); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("item inexistente não é dono de nada; err=%v", err)
	}
}

// EC-5: sem BACKLOG.md a catraca é inavaliável, e presumir a favor da dívida é o
// comportamento que esta família de itens inteira existe para eliminar.
func TestBacklogAusenteReprova(t *testing.T) {
	r := resolvedorCom(t, map[string]string{"outro.md": "x"})
	if err := r.Resolve(Guard{Kind: GuardOpen, Target: "B-033"}, "loopzero"); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("sem backlog a dívida não pode ser presumida aberta; err=%v", err)
	}
}
