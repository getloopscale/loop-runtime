// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// insumo é COMPOSTO em tempo de execução.
//
// Este arquivo é varrido pela própria âncora que ele testa (EC-1). Escrever o insumo por
// extenso aqui faria o teste virar um arquivo não-declarado — e a trava reprovaria a si
// mesma. É a terceira vez que esta armadilha aparece na sessão; nas outras duas
// (`publishhygiene`, `trufflehogscan`) a saída foi a mesma.
func insumo() string { return "loopzero-stripe" + "-webhook" }

// gitRepo cria um repositório de verdade: o gate lê `git ls-files`, que é o que será
// publicado, e não o diretório de trabalho.
func gitRepo(t *testing.T, arquivos map[string]string) string {
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
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	return root
}

// base monta um repositório coerente: um registro, o arquivo da camada, o teste que a
// guarda, o taskfile e o backlog.
func base(t *testing.T, extra map[string]string) string {
	t.Helper()
	arquivos := map[string]string{
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n1\n\n" +
			"[camadas]\nseed | loopzero | camada.go | test:guarda_test.go | pula sem credencial\n",
		"camada.go":             "const s = \"" + insumo() + "\"\n",
		"guarda_test.go":        "package a\n",
		"taskfiles/quality.yml": "cmds:\n",
	}
	for k, v := range extra {
		arquivos[k] = v
	}
	return gitRepo(t, arquivos)
}

func run(t *testing.T, root, sibling string) (Outcome, error) {
	t.Helper()
	return Check(Config{RepoRoot: root, SiblingRoot: sibling})
}

func TestTodasAsGuardasResolvemPassa(t *testing.T) {
	out, err := run(t, base(t, nil), "")
	if err != nil {
		t.Fatalf("registro coerente passa: %v (%+v)", err, out)
	}
	if out.Layers != 1 || out.Scanned == 0 {
		t.Fatalf("contagens: %+v", out)
	}
}

func TestUmaGuardaQuebradaReprovaNomeandoACamada(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n1\n\n" +
			"[camadas]\nseed | loopzero | camada.go | test:apagado_test.go | pula sem credencial\n",
	})
	out, err := run(t, root, "")
	if !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("guarda quebrada reprova; err=%v", err)
	}
	if len(out.Broken) != 1 || out.Broken[0].Layer != "seed" {
		t.Fatalf("a camada precisa ser NOMEADA para ser consertável; got %+v", out.Broken)
	}
}

// O terceiro bullet do DoD: uma camada nova nasce dentro da verificação.
func TestArquivoNaoDeclaradoQueReferenciaInsumoReprova(t *testing.T) {
	root := base(t, map[string]string{"nova_camada.go": "const x = \"" + insumo() + "\"\n"})
	out, err := run(t, root, "")
	if !errors.Is(err, ErrUndeclaredSurface) {
		t.Fatalf("arquivo novo com insumo reprova; err=%v", err)
	}
	if len(out.Undeclared) != 1 || out.Undeclared[0] != "nova_camada.go" {
		t.Fatalf("o arquivo precisa ser nomeado; got %+v", out.Undeclared)
	}
}

func TestReferenciaDeclaradaNaoReprova(t *testing.T) {
	root := base(t, map[string]string{
		"nova_camada.go": "const x = \"" + insumo() + "\"\n",
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n1\n\n" +
			"[camadas]\nseed | loopzero | camada.go | test:guarda_test.go | pula sem credencial\n\n" +
			"[referencias]\nnova_camada.go | é a guarda, não a camada\n",
	})
	if _, err := run(t, root, ""); err != nil {
		t.Fatalf("referência declarada com razão não reprova: %v", err)
	}
}

// EC-1: o próprio registro cita os insumos — é a DEFINIÇÃO deles, não um uso.
func TestORegistroNaoReprovaASiMesmo(t *testing.T) {
	if _, err := run(t, base(t, nil), ""); err != nil {
		t.Fatalf("o registro não é uma camada não-declarada: %v", err)
	}
}

// EC-4: apagar linhas não pode fazer o gate passar.
func TestAbaixoDoPisoReprova(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n2\n\n" +
			"[camadas]\nseed | loopzero | camada.go | test:guarda_test.go | pula sem credencial\n",
	})
	if _, err := run(t, root, ""); !errors.Is(err, ErrBelowFloor) {
		t.Fatalf("menos camadas que o piso reprova; err=%v", err)
	}
}

// EC-8: presente-mas-incompleto não é ausente.
func TestArquivoDeclaradoQueSumiuReprova(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n1\n\n" +
			"[camadas]\nseed | loopzero | sumiu.go | test:guarda_test.go | pula sem credencial\n",
	})
	if _, err := run(t, root, ""); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("arquivo declarado que não existe reprova; err=%v", err)
	}
}

// Cobertura parcial é REPORTADA, nunca silenciada — e nunca reprova.
func TestIrmaoAusenteReportaParcialENaoVerde(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n2\n\n" +
			"[camadas]\nseed | loopzero | camada.go | test:guarda_test.go | pula sem credencial\n" +
			"custo | loopzero-cloud | cmd/main.go | open:B-033 | desliga sem prometheus\n",
	})
	out, err := run(t, root, "")
	if err != nil {
		t.Fatalf("irmão ausente não reprova: %v", err)
	}
	if !out.Partial || len(out.NotVerified) != 1 {
		t.Fatalf("o que não foi olhado precisa ser NOMEADO; got %+v", out)
	}
	if !strings.Contains(out.Reason, "PARCIAL") {
		t.Fatalf("a razão precisa dizer que a cobertura é parcial: %q", out.Reason)
	}
}

func TestRegistroAusenteReprova(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	if _, err := run(t, root, ""); !errors.Is(err, ErrRegistryMalformed) {
		t.Fatalf("registro ausente reprova; err=%v", err)
	}
}

// A população real (rules/testing.md § 4.2).
func TestArvoreRealPassa(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot, SiblingRoot: filepath.Join(repoRoot, "..", "loopzero-cloud")})
	if err != nil {
		for i, b := range out.Broken {
			if i == 8 {
				break
			}
			t.Errorf("guarda: %s — %s", b.Layer, b.Why)
		}
		for i, u := range out.Undeclared {
			if i == 8 {
				break
			}
			t.Errorf("não declarado: %s", u)
		}
		t.Fatalf("árvore real: %v", err)
	}
	if out.Scanned == 0 || out.Layers == 0 {
		t.Fatalf("varredura que não alcançou nada não pode reportar verde: %+v", out)
	}
}

// The debt ledger is versioned: the gate resolves `open:` guards from the
// registry's [debts] section and never from BACKLOG.md, which is gitignored
// and absent on every clone.
func TestOpenDebtResolvesFromTheVersionedRegistryWithoutABacklog(t *testing.T) {
	root := base(t, map[string]string{
		"infra/quality/billing-layers.txt": "[ancora]\n" + insumo() + "\n\n[piso]\n1\n\n" +
			"[camadas]\nseed | loopzero | camada.go | open:B-033 | skips without a credential\n\n" +
			"[debts]\nB-033 | approved | the cost ceiling ships with a gate\n",
	})
	if _, err := os.Stat(filepath.Join(root, "BACKLOG.md")); err == nil {
		t.Fatal("fixture must not carry a BACKLOG.md")
	}
	if _, err := run(t, root, ""); err != nil {
		t.Fatalf("an open debt declared in the registry resolves: %v", err)
	}
}

// A versioned file the sweep cannot read may be exactly the new layer it
// exists to catch; it is an error, never a file without billing inputs.
func TestUnreadableVersionedFileFailsTheSweep(t *testing.T) {
	root := base(t, nil)
	// A tracked dangling symlink is unreadable for every uid, root included.
	if err := os.Symlink("does-not-exist.go", filepath.Join(root, "locked.go")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "locked.go")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if _, err := run(t, root, ""); err == nil || !strings.Contains(err.Error(), "locked.go") {
		t.Fatalf("want an error naming the unreadable file, got %v", err)
	}
}
