// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package skillscriptpaths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// tree writes a minimal skills root with one SKILL.md per named skill.
func tree(t *testing.T, docs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for skill, body := range docs {
		dir := filepath.Join(root, ".claude", "skills", skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBareInvocationFails(t *testing.T) {
	root := tree(t, map[string]string{
		"release": "```bash\npython3 skills/release/scripts/compute_next_version.py --x\n```\n",
	})
	out, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrUnreachableInvocation) {
		t.Fatalf("bare invocation must fail; got err=%v findings=%+v", err, out.Findings)
	}
	if len(out.Findings) != 1 || out.Findings[0].Line != 2 {
		t.Fatalf("want one finding on line 2; got %+v", out.Findings)
	}
}

// A path carrying the ecosystem prefix is correct wherever the plugin layout ships, and
// ten invocations in the real corpus already use it. Refusing it would turn a fix into a
// regression.
func TestExplicitEcosystemPrefixPasses(t *testing.T) {
	root := tree(t, map[string]string{
		"release": "```bash\npython3 .claude/skills/release/scripts/compute_next_version.py\n```\n",
	})
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("an explicit .claude/ prefix must pass: %v", err)
	}
}

// The form that resolves in BOTH layouts — the one this gate is steering toward.
func TestShellResolvedPrefixPasses(t *testing.T) {
	root := tree(t, map[string]string{
		"release": "```bash\nECO=$([ -d .claude/skills ] && echo .claude || echo .)\n" +
			"python3 \"$ECO/skills/release/scripts/compute_next_version.py\"\n```\n",
	})
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("the $ECO form must pass: %v", err)
	}
}

// A path NAMED in prose is not an invocation. The real corpus lists scripts beside
// `rules/…` and `commands/…` entries using the same ecosystem-root convention; demanding
// `$ECO` there would make the document worse to satisfy the checker.
func TestPathNamedInProseIsNotAnInvocation(t *testing.T) {
	root := tree(t, map[string]string{
		"auto-plan": "- `skills/auto-plan/scripts/select_next_milestone.py` — milestone selector\n" +
			"- `rules/cycle-review.md` — review sub-cycle\n",
	})
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("a named path is not an invocation: %v", err)
	}
}

// The distinction is the interpreter, so an invocation buried in prose must still fail —
// it is the form an agent actually runs.
func TestBareInvocationInProseAlsoFails(t *testing.T) {
	root := tree(t, map[string]string{
		"cycle-goal": "To cancel: `python3 skills/cycle-goal/scripts/install_goal_hook.py --clear`.\n",
	})
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrUnreachableInvocation) {
		t.Fatalf("a prose invocation must fail: %v", err)
	}
}

func TestBashAndShAreInterpretersToo(t *testing.T) {
	for _, exe := range []string{"bash", "sh"} {
		root := tree(t, map[string]string{
			"x": "```bash\n" + exe + " skills/x/scripts/run.sh\n```\n",
		})
		if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrUnreachableInvocation) {
			t.Fatalf("%s invocation must fail: %v", exe, err)
		}
	}
}

// Absence is a FAILURE, not a skip. A gate reporting green over a tree it never found is
// the exact shape of defect this gate exists to refuse.
func TestMissingSkillsRootIsAFailureNotASkip(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); !errors.Is(err, ErrSkillsRootMissing) {
		t.Fatalf("a missing skills root must fail loudly: %v", err)
	}
}

// The real tree is the population the gate exists for. A synthetic mutant proves the
// detector fires; only this proves it fires on what shipped (B-109).
func TestRealTreeIsClean(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Check(Config{RepoRoot: root})

	// No ecosystem installed: the contract is a LOUD refusal, and asserting it beats
	// vanishing — it proves the gate cannot report green over a tree it never found.
	if _, statErr := os.Stat(filepath.Join(root, ".claude", "skills")); statErr != nil {
		if !errors.Is(err, ErrSkillsRootMissing) {
			t.Fatalf("without a skills tree the gate must fail loudly: %v", err)
		}
		return
	}
	if err != nil {
		for _, f := range out.Findings {
			t.Errorf("%s:%d invokes %q\n  %s", f.File, f.Line, f.Path, f.Text)
		}
		t.Fatalf("%d unreachable invocation(s) across %d skill(s)", len(out.Findings), out.SkillsScanned)
	}
	if out.FilesScanned == 0 {
		t.Fatal("zero SKILL.md scanned — the scan found nothing to check, which is not the same as finding nothing wrong")
	}
}

// Two blind spots this gate had, reported by the kit after it ran a wider sweep: it found 39
// occurrences in 18 files where this gate saw 25 in 8.
//
// The gate matched only `skills/…` and read only each skill's SKILL.md. Both narrowings were
// invisible from inside: every file it opened was clean, so it reported PASS over the subset
// it could see. That is the shape this repository keeps finding — a green that measured less
// than it appeared to.
func TestPegaInvocacaoDeScriptsSemPrefixoDeSkills(t *testing.T) {
	// `scripts/…` at the ecosystem root has the same defect and the same fix: it resolves in
	// the standalone kit and nowhere else.
	linha := `python3 scripts/check_xrefs.py --strict`
	if achados := scanLine(linha); len(achados) == 0 {
		t.Errorf("uma invocacao de `scripts/…` sem prefixo nao foi acusada; ela quebra num "+
			"consumidor exatamente como `skills/…`, e o gate so' olhava a segunda forma: %q", linha)
	}
}

func TestNaoAcusaAFormaQueResolveNosDoisLayouts(t *testing.T) {
	// A contrapartida, e ela e' o que impede o conserto de virar ruido: a forma inline que o
	// kit passou a enviar tem de passar.
	for _, linha := range []string{
		`python3 "$([ -d .claude/skills ] && echo .claude || echo .)/scripts/check_xrefs.py"`,
		`python3 "$ECO/skills/release/scripts/compute_next_version.py"`,
		`python3 .claude/scripts/check_xrefs.py`,
	} {
		if achados := scanLine(linha); len(achados) > 0 {
			t.Errorf("acusou a forma que JA resolve nos dois layouts: %q", linha)
		}
	}
}

// Um `.local.md` na raiz do ecossistema e' estado de sessao, nao instrucao enviada: ele e'
// gitignored e existe so' na maquina de quem rodou. Reprovar o repositorio por causa dele
// seria o gate falhando por algo que nenhum consumidor recebe — e a raiz do `.claude/` carrega
// um hoje (`codebase-architect-loop.local.md`, 16 invocacoes).
func TestIgnoraArquivoLocalDeSessaoNaRaiz(t *testing.T) {
	raiz := t.TempDir()
	eco := filepath.Join(raiz, ".claude")
	if err := os.MkdirAll(filepath.Join(eco, "skills", "alguma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eco, "skills", "alguma", "SKILL.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A forma quebrada, num arquivo que nunca sai desta maquina.
	if err := os.WriteFile(filepath.Join(eco, "loop.local.md"),
		[]byte("python3 scripts/route_domain.py x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("err = %v; um .local.md nao pode reprovar o repositorio: %+v", err, out.Findings)
	}
}

// A SKILL.md that exists and cannot be read is not a skill without a SKILL.md:
// skipping it reports PASS over a document nobody read.
func TestUnreadableSkillDocIsAnError(t *testing.T) {
	root := tree(t, map[string]string{"ok": "fine\n"})
	dir := filepath.Join(root, ".claude", "skills", "broken")
	if err := os.MkdirAll(filepath.Join(dir, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Check(Config{RepoRoot: root})
	if err == nil || errors.Is(err, ErrUnreachableInvocation) {
		t.Fatalf("an unreadable SKILL.md was skipped, got %v", err)
	}
}

func TestUnreadableRuleDocIsAnError(t *testing.T) {
	root := tree(t, map[string]string{"ok": "fine\n"})
	rules := filepath.Join(root, ".claude", "rules")
	if err := os.MkdirAll(filepath.Join(rules, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(rules, "real"), filepath.Join(rules, "linked.md")); err != nil {
		t.Fatal(err)
	}
	_, err := Check(Config{RepoRoot: root})
	if err == nil || errors.Is(err, ErrUnreachableInvocation) {
		t.Fatalf("an unreadable rule document was skipped, got %v", err)
	}
}
