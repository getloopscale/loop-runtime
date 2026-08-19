// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapterports_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/adapterports"
)

func arvore(t *testing.T, arquivos map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for nome, corpo := range arquivos {
		p := filepath.Join(root, "api", "internal", "adapters", nome)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(corpo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Todo adaptador declara, em tempo de compilacao, qual porta satisfaz.
//
// B-058, medido 2026-08-18: o pacote se chama `adapters` e o criterio de admissao dele era
// "e um adapter" — que nao recusa nada. Dos nove adaptadores, cinco NOMEAVAM a porta em
// prosa e apenas UM a verificava. Prosa nao compila.
//
// A assercao e o que faz o criterio conseguir dizer nao: quem nao souber nomear a porta que
// satisfaz nao pertence aqui.
func TestAdapterWithAssertionPasses(t *testing.T) {
	root := arvore(t, map[string]string{
		"x_adapter.go": "package adapters\n\ntype XAdapter struct{}\n\nvar _ foo.Port = (*XAdapter)(nil)\n",
	})
	out, err := adapterports.Check(adapterports.Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("adaptador com assercao deveria passar; veio %v", err)
	}
	if out.Scanned != 1 {
		t.Errorf("esperava 1 varrido, veio %d", out.Scanned)
	}
}

// Adaptador sem assercao e o caso que o criterio antigo nao recusava.
func TestAdapterWithoutAssertionIsRefused(t *testing.T) {
	root := arvore(t, map[string]string{
		"y_adapter.go": "package adapters\n\n// YAdapter adapta alguma coisa.\ntype YAdapter struct{}\n",
	})
	out, err := adapterports.Check(adapterports.Config{RepoRoot: root})
	if !errors.Is(err, adapterports.ErrNoPortDeclared) {
		t.Fatalf("esperava ErrNoPortDeclared, veio %v", err)
	}
	if len(out.Missing) != 1 || out.Missing[0] != "y_adapter.go" {
		t.Errorf("o achado precisa nomear o arquivo: %v", out.Missing)
	}
}

// Uma assercao de USO (`var _ = pkg.Tipo{}`) nao e declaracao de porta — ela nao afirma que
// o adaptador satisfaz interface alguma. Confundir as duas deixaria passar o defeito.
func TestUsageAssertionIsNotAPortDeclaration(t *testing.T) {
	root := arvore(t, map[string]string{
		"z_adapter.go": "package adapters\n\ntype ZAdapter struct{}\n\nvar _ = db.AlgumTipo{}\n",
	})
	if _, err := adapterports.Check(adapterports.Config{RepoRoot: root}); !errors.Is(err, adapterports.ErrNoPortDeclared) {
		t.Fatalf("assercao de uso nao declara porta; esperava recusa, veio %v", err)
	}
}

// Arquivo sem tipo exportado nao e adaptador — nao ha porta a exigir dele, e ele nao
// contamina a contagem.
//
// A primeira versao deste teste passava SO o arquivo de doc e esperava verde. Estava errada,
// e a guarda me contrariou: um pacote com zero adaptadores e indistinguivel de um pacote
// dissolvido, e a outra alternativa do DoD de B-058 e justamente DISSOLVER `adapters`. Ficar
// verde ali seria a guarda sobrevivendo ao proprio objeto. O contrato certo e o abaixo: o
// arquivo de doc nao precisa declarar nada, e o adaptador ao lado precisa.
func TestFileWithoutExportedTypeIsNotRequiredToDeclare(t *testing.T) {
	root := arvore(t, map[string]string{
		"adapters.go":  "// Package adapters ...\npackage adapters\n",
		"w_adapter.go": "package adapters\n\ntype WAdapter struct{}\n\nvar _ foo.Port = (*WAdapter)(nil)\n",
	})
	out, err := adapterports.Check(adapterports.Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("o arquivo de doc nao precisa declarar porta; veio %v", err)
	}
	if out.Scanned != 1 {
		t.Errorf("so o adaptador conta: esperava 1 varrido, veio %d", out.Scanned)
	}
}

// Pacote ausente reprova: um dissolve futuro (a outra alternativa do DoD) nao pode deixar a
// guarda verde sobre nada.
func TestMissingPackageIsRefused(t *testing.T) {
	if _, err := adapterports.Check(adapterports.Config{RepoRoot: t.TempDir()}); !errors.Is(err, adapterports.ErrNothingScanned) {
		t.Fatal("pacote ausente deveria reprovar")
	}
}

// A populacao real.
func TestRealTree(t *testing.T) {
	out, err := adapterports.Check(adapterports.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")})
	if err != nil {
		t.Fatalf("arvore real: %v (sem porta: %v)", err, out.Missing)
	}
	if out.Scanned == 0 {
		t.Fatal("zero adaptadores varridos — verde sobre nada")
	}
	t.Logf("populacao real: %d adaptador(es), todos declarando a porta que satisfazem", out.Scanned)
}
