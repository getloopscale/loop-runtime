// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platformcrnamespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func arvore(t *testing.T, arquivos map[string]string) Config {
	t.Helper()
	raiz := t.TempDir()
	for nome, corpo := range arquivos {
		p := filepath.Join(raiz, filepath.FromSlash(nome))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(corpo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Config{RepoRoot: raiz, Dirs: []string{"infra"}}
}

const crCanonico = "apiVersion: loopzero.io/v1\nkind: Tenant\nmetadata:\n  name: tn-x\n  namespace: loopzero-system\n"

func TestNamespaceCanonicoPassa(t *testing.T) {
	out, err := Check(arvore(t, map[string]string{"infra/a.yaml": crCanonico}))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(out.Conferidas) != 1 {
		t.Errorf("conferidas = %d; want 1", len(out.Conferidas))
	}
}

// O defeito MEDIDO (B-095): o bootstrap de producao aplicava o Tenant em `default`.
func TestNamespaceForaDoCanonicoReprova(t *testing.T) {
	fora := "apiVersion: loopzero.io/v1\nkind: Tenant\nmetadata:\n  name: tn-x\n  namespace: default\n"
	out, err := Check(arvore(t, map[string]string{"infra/a.yaml": fora}))
	if !errors.Is(err, ErrForaDoNamespace) {
		t.Fatalf("err = %v; want ErrForaDoNamespace", err)
	}
	if len(out.Violacoes) != 1 || out.Violacoes[0].Namespace != "default" {
		t.Errorf("violacoes = %+v", out.Violacoes)
	}
}

// Heredoc dentro de shell e' onde o defeito real vivia. Um parser de YAML nao le isso sem
// executar o shell antes — e' por isso que a deteccao e' textual.
func TestAchaDentroDeHeredocDeShell(t *testing.T) {
	sh := "#!/usr/bin/env bash\nkubectl apply -f - <<'EOF'\napiVersion: loopzero.io/v1\nkind: Tenant\nmetadata:\n  name: tn-x\n  namespace: default\nEOF\n"
	if _, err := Check(arvore(t, map[string]string{"infra/s.sh": sh})); !errors.Is(err, ErrForaDoNamespace) {
		t.Fatalf("err = %v; o CR estava num heredoc, que e' exatamente onde o defeito real estava", err)
	}
}

// Um `kind: Environment` de OUTRO grupo nao e' um CR de plataforma. Sem a ancora do apiVersion o
// gate acusaria qualquer homonimo.
func TestKindHomonimoDeOutroGrupoNaoConta(t *testing.T) {
	outro := "apiVersion: other.example.com/v1\nkind: Environment\nmetadata:\n  namespace: default\n"
	if _, err := Check(arvore(t, map[string]string{"infra/a.yaml": outro})); !errors.Is(err, ErrNadaVarrido) {
		t.Fatalf("err = %v; `Environment` de outro apiGroup nao e' CR de plataforma", err)
	}
}

// Variavel de shell: o valor e' de runtime. Julga-la aqui seria adivinhar.
func TestNamespaceEmVariavelNaoEAcusado(t *testing.T) {
	v := "apiVersion: loopzero.io/v1\nkind: Tenant\nmetadata:\n  namespace: ${NS}\n"
	if _, err := Check(arvore(t, map[string]string{"infra/s.sh": v})); err != nil {
		t.Fatalf("err = %v; o valor de ${NS} e' decidido em execucao", err)
	}
}

// Uma varredura que nao alcanca nada reporta verde sobre nada — a forma `PASS — 0 routes`.
func TestVarreduraVaziaEErro(t *testing.T) {
	if _, err := Check(arvore(t, map[string]string{"infra/a.yaml": "kind: ConfigMap\n"})); !errors.Is(err, ErrNadaVarrido) {
		t.Fatalf("err = %v; want ErrNadaVarrido", err)
	}
}

// A POPULACAO REAL (`rules/testing.md` § 4.2). O mutante acima prova que o detector dispara;
// so' isto diz o que ele encontra na arvore que existe.
//
// RESULTADO DA VARREDURA, medido em 2026-08-19: **uma** violacao —
// `infra/scripts/production/bootstrap-platform.sh:199`, um Tenant em `default`. Este teste
// falhava por causa dela antes do conserto do mesmo dia. Registrar o numero aqui e' o que
// distingue "nao achou nada" de "nao procurou".
func TestArvoreRealNaoTemCRForaDoNamespace(t *testing.T) {
	raiz := repoRoot(t)
	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(out.Conferidas) < 3 {
		t.Errorf("conferidas = %d (%+v); a arvore carregava tres seeds em 2026-08-19 — uma "+
			"varredura que acha menos esta' medindo menos que a arvore", len(out.Conferidas), out.Conferidas)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.work nao encontrado subindo")
	return ""
}
