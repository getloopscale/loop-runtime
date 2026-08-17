// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ecosystemvalidators

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// pythonAvailable reports whether the interpreter this gate shells out to exists.
//
// Nada aqui faz SKIP na ausência dele: `Check` documenta que devolve `Advisory` sem
// interpretador, e essa é a contrapartida — um teste que some naquele ambiente deixa
// o único caminho que roda ali sem prova.
func pythonAvailable() bool {
	_, err := exec.LookPath("python3")
	return err == nil
}

func fakeEco(t *testing.T, scripts map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range scripts {
		p := filepath.Join(root, ".claude", "scripts", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGreenValidatorsPass(t *testing.T) {
	root := fakeEco(t, map[string]string{
		"a.py": "print('ok')\n",
		"b.py": "print('ok')\n",
	})
	cfg := Config{RepoRoot: root, Validators: []Validator{
		{Name: "a", Rel: "scripts/a.py"}, {Name: "b", Rel: "scripts/b.py"},
	}, Timeout: 30 * time.Second}

	out, err := Check(cfg)
	if !pythonAvailable() {
		if err != nil || !out.Advisory {
			t.Fatalf("sem python3 o gate é advisory e não erro; err=%v out=%+v", err, out)
		}
		return
	}
	if err != nil {
		t.Fatalf("validadores verdes devem passar: %v", err)
	}
	if len(out.Results) != 2 {
		t.Fatalf("os dois precisam ter rodado; got %+v", out.Results)
	}
}

func TestRedValidatorFails(t *testing.T) {
	root := fakeEco(t, map[string]string{
		"ok.py":  "print('ok')\n",
		"bad.py": "import sys; sys.exit(1)\n",
	})
	cfg := Config{RepoRoot: root, Validators: []Validator{
		{Name: "ok", Rel: "scripts/ok.py"}, {Name: "bad", Rel: "scripts/bad.py"},
	}, Timeout: 30 * time.Second}

	out, err := Check(cfg)
	if !pythonAvailable() {
		return
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Fatalf("um validador vermelho reprova o gate: %v", err)
	}
	if f := out.Failed(); len(f) != 1 || f[0].Name != "bad" {
		t.Fatalf("o validador que reprovou precisa ser nomeado; got %+v", f)
	}
}

// A bala 3 do DoD: WARN não pode virar ruído que apaga o FAIL. Um validador que sai 0
// com avisos PASSA, e os avisos são contados separadamente.
func TestWarningsAreCountedButDoNotFail(t *testing.T) {
	root := fakeEco(t, map[string]string{
		"warn.py": "print('  [WARN] a')\nprint('  [WARN] b')\nprint('Overall: PASS')\n",
	})
	cfg := Config{RepoRoot: root, Validators: []Validator{{Name: "warn", Rel: "scripts/warn.py"}}, Timeout: 30 * time.Second}

	out, err := Check(cfg)
	if !pythonAvailable() {
		return
	}
	if err != nil {
		t.Fatalf("aviso não é reprovação: %v", err)
	}
	if out.Warnings() != 2 {
		t.Fatalf("os avisos precisam ser contados para que sua evolução seja visível; got %d", out.Warnings())
	}
}

// Ausência é FALHA, nunca skip — é o defeito que este gate existe para remover.
func TestMissingValidatorsIsAFailure(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); !errors.Is(err, ErrValidatorsMissing) {
		if pythonAvailable() {
			t.Fatalf("validador ausente precisa reprovar alto; err=%v", err)
		}
	}
}

// Os dois layouts. Fixar um faria o gate reportar ausência no outro — o defeito do
// B-120, noutro lugar.
func TestBothLayoutsResolve(t *testing.T) {
	plugin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(plugin, ".claude", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveEco(plugin, ""); got != filepath.Join(plugin, ".claude") {
		t.Errorf("layout plugin: got %q", got)
	}
	standalone := t.TempDir()
	if err := os.MkdirAll(filepath.Join(standalone, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveEco(standalone, ""); got != standalone {
		t.Errorf("layout standalone: got %q", got)
	}
}

// A população real (B-109): os dois validadores de verdade, na árvore de verdade. É
// esta asserção que impede o gate de ficar verde sobre scripts sintéticos enquanto os
// reais estão vermelhos — que é literalmente o estado que o B-129 mediu.
func TestRealValidatorsAreGreen(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot, Timeout: 3 * time.Minute})
	if out.Advisory {
		t.Skip("python3 ausente — a asserção de população precisa do interpretador")
	}
	if err != nil {
		for _, r := range out.Failed() {
			t.Errorf("%s: %s", r.Name, r.Tail)
		}
		t.Fatalf("os validadores reais precisam estar verdes: %v", err)
	}
	if len(out.Results) != len(DefaultValidators) {
		t.Fatalf("os %d validadores declarados precisam ter rodado; got %+v", len(DefaultValidators), out.Results)
	}
}
