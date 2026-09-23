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

// fakeEco writes scripts under `.claude/mechanisms/gates/` to match the
// post-relocation kit layout (formerly `.claude/scripts/`). The Rels supplied
// by the tests below use `mechanisms/gates/<name>` so `resolveEco` returns
// `<root>/.claude` and the joined path lands on the fixture file.
func fakeEco(t *testing.T, scripts map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range scripts {
		p := filepath.Join(root, ".claude", "mechanisms", "gates", name)
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
		{Name: "a", Rel: "mechanisms/gates/a.py"}, {Name: "b", Rel: "mechanisms/gates/b.py"},
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
		{Name: "ok", Rel: "mechanisms/gates/ok.py"}, {Name: "bad", Rel: "mechanisms/gates/bad.py"},
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
	cfg := Config{RepoRoot: root, Validators: []Validator{{Name: "warn", Rel: "mechanisms/gates/warn.py"}}, Timeout: 30 * time.Second}

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

// A kit that is installed but lacks a declared validator is a FAILURE, never a
// skip — that is the defect this gate exists to remove.
func TestMissingValidatorInAnInstalledKitIsAFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "mechanisms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrValidatorsMissing) && pythonAvailable() {
		t.Fatalf("a missing validator in an installed kit must fail loudly; err=%v", err)
	}
}

// The kit is gitignored in this repository, so a clone without it has no
// validators to run. That is ADVISORY with the reason named, not a FAIL: a
// versioned gate must not fail on every clone over an unversioned install.
func TestKitNotInstalledIsAdvisory(t *testing.T) {
	out, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("no kit is advisory, not an error: %v", err)
	}
	if !out.Advisory || out.AdvisoryReason == "" || len(out.Results) != 0 {
		t.Fatalf("want an advisory outcome naming why nothing ran, got %+v", out)
	}
}

// Os dois layouts. Fixar um faria o gate reportar ausência no outro — o defeito do
// B-120, noutro lugar. A sonda mudou de `scripts/` para `mechanisms/` porque o kit
// relocou tudo em `mechanisms/<família>/`; o teste segue esse contrato.
func TestBothLayoutsResolve(t *testing.T) {
	plugin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(plugin, ".claude", "mechanisms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveEco(plugin, ""); got != filepath.Join(plugin, ".claude") {
		t.Errorf("layout plugin: got %q", got)
	}
	standalone := t.TempDir()
	if err := os.MkdirAll(filepath.Join(standalone, "mechanisms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveEco(standalone, ""); got != standalone {
		t.Errorf("layout standalone: got %q", got)
	}
}

// The real tree: the gate either runs every declared validator (kit installed)
// or reports ADVISORY with its reason (kit or interpreter absent). It never
// fails over the kit's absence. Whether the kit's own validators are green is
// the gate's verdict, reported by `loopzero-ops quality ecosystem-validators`;
// asserting it here would make this module's suite depend on an unversioned
// install.
func TestRealTreeRunsTheDeclaredValidatorsOrIsAdvisory(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot, Timeout: 3 * time.Minute})
	if out.Advisory {
		if err != nil || out.AdvisoryReason == "" || len(out.Results) != 0 {
			t.Fatalf("advisory must carry a reason and no results; err=%v out=%+v", err, out)
		}
		return
	}
	if errors.Is(err, ErrValidatorsMissing) {
		t.Fatalf("an installed kit must carry the declared validators: %v", err)
	}
	if len(out.Results) != len(DefaultValidators) {
		t.Fatalf("all %d declared validators must have run; got %+v", len(DefaultValidators), out.Results)
	}
}
