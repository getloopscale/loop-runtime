// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmfilecontextparity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// tree writes a synthetic repo with one taskfile and the named helmfiles.
func tree(t *testing.T, taskfile string, helmfiles map[string]string) Config {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("taskfiles/x.yml", taskfile)
	for rel, body := range helmfiles {
		write(rel, body)
	}
	return Config{RepoRoot: root}
}

const declaresCore = "helmDefaults:\n  kubeContext: k3d-loopzero-core\nreleases: []\n"
const declaresBuild = "helmDefaults:\n  kubeContext: k3d-loopzero-build\nreleases: []\n"

// The whole point of the gate: two declarations of the same fact that disagree.
func TestCheck_DivergentContextFails(t *testing.T) {
	cfg := tree(t,
		"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n",
		map[string]string{"infra/helmfile/a.gotmpl": declaresBuild},
	)
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrContextDiverges) {
		t.Fatalf("a task pointing at a cluster the helmfile does not declare must FAIL; got %v", err)
	}
	if len(out.Divergences) != 1 {
		t.Fatalf("expected exactly one divergence; got %+v", out.Divergences)
	}
	d := out.Divergences[0]
	if d.Invoked != "k3d-loopzero-core" || d.Declared != "k3d-loopzero-build" {
		t.Errorf("the failure must NAME both sides so the reader knows which to change; got %+v", d)
	}
}

func TestCheck_MatchingContextPasses(t *testing.T) {
	cfg := tree(t,
		"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n",
		map[string]string{"infra/helmfile/a.gotmpl": declaresCore},
	)
	if _, err := violationOf(Check(cfg)); err != nil {
		t.Errorf("agreement is the passing case; got %v", err)
	}
}

// The floor. This is the B-040 lesson: a gate that enumerated nothing has
// verified nothing, and reporting PASS in that state is the failure mode the
// whole tripwire slice exists to remove.
func TestCheck_NoComparableInvocationsIsAnError(t *testing.T) {
	cfg := tree(t, "      - echo nada\n", map[string]string{"infra/helmfile/a.gotmpl": declaresCore})
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrNothingCompared) {
		t.Errorf("zero comparisons must be a LOUD error, never a silent PASS; got %v", err)
	}
}

// R2: cell.helmfile serves N cells, so its context is a parameter on BOTH
// sides. Demanding literal equality would reject the CORRECT design — the gate
// must be able to tell "parameterised" from "wrong".
func TestCheck_ParameterisedOnBothSidesIsNotADivergence(t *testing.T) {
	cfg := tree(t,
		// The cell invocation exports the variable its helmfile demands — the
		// shape the real taskfile had to be corrected to. Its earlier form did
		// not, and TestCheck_RequiredEnvMustBeSetByTheInvokingTask exists because
		// that omission broke an install.
		"      - LOOPZERO_CELL_CONTEXT=\"{{.CELL_CTX}}\" helmfile -f infra/helmfile/cell.gotmpl --kube-context \"{{.CELL_CTX}}\" sync\n"+
			"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n",
		map[string]string{
			"infra/helmfile/cell.gotmpl": "helmDefaults:\n  kubeContext: {{ requiredEnv \"LOOPZERO_CELL_CONTEXT\" }}\nreleases: []\n",
			"infra/helmfile/a.gotmpl":    declaresCore,
		},
	)
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("a context parameterised on both sides is out of static reach, not a violation; got %v", err)
	}
	if out.ComparedCount != 1 || out.SkippedParameterised != 1 {
		t.Errorf("the skip must be COUNTED and reported, never folded into the pass; got %+v", out)
	}
}

// A helmfile that declares nothing is the state this slice removes; it must be
// named, not silently skipped.
func TestCheck_UndeclaredHelmfileIsReported(t *testing.T) {
	cfg := tree(t,
		"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n",
		map[string]string{"infra/helmfile/a.gotmpl": "releases: []\n"},
	)
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrContextNotDeclared) {
		t.Fatalf("an invoked helmfile that declares no context must FAIL; got %v", err)
	}
	if len(out.Undeclared) != 1 || out.Undeclared[0] != "infra/helmfile/a.gotmpl" {
		t.Errorf("the failure must name the file that must declare; got %+v", out.Undeclared)
	}
}

// Invocations wrap across lines in the real taskfiles, with and without a
// trailing backslash. Missing them would make the gate under-count and pass by
// looking at less than it thinks.
func TestCheck_MultiLineInvocationIsParsed(t *testing.T) {
	cfg := tree(t,
		"      - |\n        helmfile -f infra/helmfile/a.gotmpl \\\n"+
			"          -e dev \\\n          --kube-context k3d-loopzero-build \\\n          sync\n",
		map[string]string{"infra/helmfile/a.gotmpl": declaresCore},
	)
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrContextDiverges) {
		t.Fatalf("a wrapped invocation must be parsed, not skipped; got %v out=%+v", err, out)
	}
}

// A comment is not an invocation. Measured false positive: taskfiles/infra.yml
// carries "# `helm upgrade -f values.yaml` would 404" a few lines below a real
// helmfile call, and the naive window swallowed it — reporting values.yaml as a
// helmfile that declares no context.
func TestCheck_CommentNearAnInvocationIsNotParsed(t *testing.T) {
	cfg := tree(t,
		"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n"+
			"      # `helm upgrade -f values.yaml` would 404 on the missing file\n",
		map[string]string{"infra/helmfile/a.gotmpl": declaresCore},
	)
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("a comment must not become a finding; got %v", err)
	}
	if len(out.Undeclared) != 0 {
		t.Errorf("prose describing a command is not an invocation; got %+v", out.Undeclared)
	}
}

// Against the real tree: after the helmfiles declare their context, every
// literal invocation must agree. A failure here is a real divergence, not a
// fixture artefact.
func TestCheck_RealTree(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: qtestutil.Root(t)}))
	if out.ComparedCount+out.SkippedParameterised == 0 {
		t.Fatalf("the real tree has helmfile invocations; enumerating none means the parser is broken, not that the tree is clean")
	}
	if err != nil {
		t.Logf("real-tree divergences (expected until B-047 removes the forbidden task): %+v undeclared=%+v", out.Divergences, out.Undeclared)
	}

	// Reconciliation against an INDEPENDENT count. Every --kube-context outside a
	// comment belongs to exactly one invocation, so when no helmfile is
	// undeclared the two numbers must agree exactly. A parser that silently drops
	// invocations still reports a confident PASS — this is what makes that
	// impossible, and it is how the 26-vs-16 over-count was caught.
	if len(out.Undeclared) == 0 {
		if n := countKubeContextLines(t); out.ComparedCount+out.SkippedParameterised != n {
			t.Errorf("the walk paired %d invocation(s) but the tree carries %d --kube-context occurrence(s) — the parser is over- or under-counting, and a confident verdict over a miscount is worth less than no verdict",
				out.ComparedCount+out.SkippedParameterised, n)
		}
	}
}

// countKubeContextLines counts --kube-context occurrences in taskfiles/*.yml,
// ignoring comment lines. Deliberately naive and independent of Check's parser:
// two implementations of the same count only agree when both are right.
func countKubeContextLines(t *testing.T) int {
	t.Helper()
	root := qtestutil.Root(t)
	// Reads the scanned set from `defaultGlobs`, the gate's single declaration,
	// instead of repeating the list here: omitting a source makes the
	// reconciliation compare TWO DIFFERENT TREES and fail for a reason that has
	// nothing to do with the parser. That is exactly what happened when the gate
	// began scanning script invokers and this list stayed behind (B-078).
	var paths []string
	for _, g := range defaultGlobs {
		found, err := filepath.Glob(filepath.Join(root, g))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, found...)
	}
	n := 0
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			n += strings.Count(l, "--kube-context")
		}
	}
	return n
}

// MEASURED false positive, found by counting: the tree has 16 lines carrying
// --kube-context, and the gate reported 26 pairings. A window that runs forward
// for a fixed number of lines walks straight out of one YAML block into the
// next, and lends a real invocation's operands to prose that has none.
//
// taskfiles/k3d.yml:255 is documentation inside a `desc:` — "helmfile sync
// --selector name=openbao-init" — with no -f and no --kube-context of its own.
// It was reported as a divergence because the window reached past `cmds:` into
// line 262 and borrowed from there. The finding underneath was real; the count
// and one of its line numbers were not.
func TestCheck_WindowStopsAtAYAMLBoundary(t *testing.T) {
	cfg := tree(t,
		"    desc: |\n"+
			"      passos manuais:\n"+
			"        helmfile sync --selector name=algo\n"+ // prose: no -f, no --kube-context
			"    cmds:\n"+
			"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n",
		map[string]string{"infra/helmfile/a.gotmpl": declaresCore},
	)
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("the real invocation agrees with its helmfile; got %v", err)
	}
	if out.ComparedCount != 1 {
		t.Errorf("prose in a desc: block is not an invocation and must not borrow the next command's operands; compared=%d", out.ComparedCount)
	}
}

// A helmfile whose context is `requiredEnv "X"` fails LOUDLY when X is unset —
// which is the desired fail-fast, and a trap for the task that invokes it.
//
// Measured, by making the mistake: taskfiles/infra.yml passed the cell context
// as a --kube-context FLAG and never exported it, so binding the helmfile to
// `requiredEnv "LOOPZERO_CELL_CONTEXT"` broke `task infra:install:cell` outright.
// A Task var is not an env var, and nothing in the tree said so.
func TestCheck_RequiredEnvMustBeSetByTheInvokingTask(t *testing.T) {
	hf := map[string]string{
		"infra/helmfile/cell.gotmpl": "helmDefaults:\n  kubeContext: {{ requiredEnv \"LOOPZERO_CELL_CONTEXT\" }}\nreleases: []\n",
	}
	unset := tree(t,
		"      - helmfile -f infra/helmfile/cell.gotmpl --kube-context \"{{.CELL_CTX}}\" sync\n", hf)
	out, err := violationOf(Check(unset))
	if !errors.Is(err, ErrRequiredEnvNotSet) {
		t.Fatalf("a helmfile demanding an env var that its caller never exports must FAIL; got %v out=%+v", err, out)
	}
	if len(out.MissingEnv) != 1 || !strings.Contains(out.MissingEnv[0], "LOOPZERO_CELL_CONTEXT") {
		t.Errorf("the failure must name the variable; got %+v", out.MissingEnv)
	}

	// A literal pair rides along so the tree has something to COMPARE: after the
	// floor was tightened to ComparedCount == 0, a fixture whose every context is
	// a template legitimately trips it. That is the floor working, not a bug.
	hf["infra/helmfile/a.gotmpl"] = declaresCore
	set := tree(t,
		"      - LOOPZERO_CELL_CONTEXT=\"{{.CELL_CTX}}\" helmfile -f infra/helmfile/cell.gotmpl --kube-context \"{{.CELL_CTX}}\" sync\n"+
			"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n", hf)
	if _, err := violationOf(Check(set)); err != nil {
		t.Errorf("exporting it from the same source as the flag is the correct shape; got %v", err)
	}
}

// The floor, from the two directions that matter operationally. The earlier
// floor test covers "a taskfile exists but runs no helmfile"; these cover the
// cases where the gate is pointed somewhere it finds nothing at all.
//
// The second is the dangerous one: a gate invoked with the wrong --root scans an
// empty tree and, without a floor, reports a confident PASS about a repository
// it never opened. That is `route-auth-coverage: PASS — 0 routes` with a
// different subject.
func TestFloor_EmptyTaskfilesDirFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "taskfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrNothingCompared) {
		t.Errorf("an empty taskfiles/ must fail; got %v", err)
	}
}

func TestFloor_WrongRootFails(t *testing.T) {
	if _, err := violationOf(Check(Config{RepoRoot: t.TempDir()})); !errors.Is(err, ErrNothingCompared) {
		t.Errorf("a root with no taskfiles/ at all must fail — a gate aimed at the wrong tree must say so, not vouch for a repository it never opened; got %v", err)
	}
}

// A taskfile is parsed input, so a `-f` operand can point anywhere. The gate
// only ever reads, so the risk is small — but containment is cheap, and a path
// that escapes the tree under audit is reported rather than followed.
func TestCheck_PathEscapingTheRootIsNotRead(t *testing.T) {
	cfg := tree(t,
		"      - helmfile -f ../../../../etc/hosts --kube-context k3d-loopzero-core sync\n",
		map[string]string{"infra/helmfile/a.gotmpl": declaresCore},
	)
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrContextNotDeclared) {
		t.Fatalf("a path outside the audited tree must surface as undeclared, never be read; got %v", err)
	}
	if len(out.Undeclared) != 1 {
		t.Errorf("the escape must be reported; got %+v", out.Undeclared)
	}
}

// The CLI invokes the gate with `--root .`, and the suite had never exercised a
// RELATIVE root — a blind spot that let a containment check ship which rejected
// every real helmfile ("0 compared, 4 undeclared") while every test stayed green,
// because they all build absolute temp dirs.
//
// A gate whose only real-tree assertion runs under conditions the CLI never uses
// is verifying a configuration nobody runs.
func TestCheck_RelativeRootBehavesLikeAbsolute(t *testing.T) {
	abs := qtestutil.Root(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(abs); err != nil {
		t.Fatal(err)
	}

	fromAbs, _ := violationOf(Check(Config{RepoRoot: abs}))
	fromRel, _ := violationOf(Check(Config{RepoRoot: "."}))
	if fromAbs.ComparedCount != fromRel.ComparedCount ||
		fromAbs.SkippedParameterised != fromRel.SkippedParameterised ||
		len(fromAbs.Undeclared) != len(fromRel.Undeclared) {
		t.Errorf("a relative root must measure the same tree as an absolute one; abs=%+v rel=%+v", fromAbs, fromRel)
	}
}

// H1, measured on the repository's own fix: in a folded `>-` block the env
// assignment sits on the line ABOVE the helmfile token, and a forward-only
// search reported taskfiles/infra.yml:264-265 — the correct shape — as a
// missing variable. The gate accused the fix it was written to protect.
func TestCheck_EnvAssignmentOnThePrecedingLineCounts(t *testing.T) {
	cfg := tree(t,
		"      - >-\n        LOOPZERO_CELL_CONTEXT=\"{{.CELL_CTX}}\"\n"+
			"        helmfile -f infra/helmfile/cell.gotmpl\n        --kube-context \"{{.CELL_CTX}}\"\n        sync\n"+
			"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n",
		map[string]string{
			"infra/helmfile/cell.gotmpl": "helmDefaults:\n  kubeContext: {{ requiredEnv \"LOOPZERO_CELL_CONTEXT\" }}\nreleases: []\n",
			"infra/helmfile/a.gotmpl":    declaresCore,
		},
	)
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("an assignment one line above the token is still an assignment; got %v missing=%+v", err, out.MissingEnv)
	}
}

// Two consecutive invocations must not share operands. Extending the operand
// window backwards to catch the env assignment made the second `- helmfile …`
// consume the FIRST one's -f, silently pairing an invocation with the wrong
// helmfile — a miscount that reads exactly like a correct run.
func TestCheck_ConsecutiveInvocationsDoNotShareOperands(t *testing.T) {
	cfg := tree(t,
		"      - helmfile -f infra/helmfile/a.gotmpl --kube-context k3d-loopzero-core sync\n"+
			"      - helmfile -f infra/helmfile/b.gotmpl --kube-context k3d-loopzero-build sync\n",
		map[string]string{
			"infra/helmfile/a.gotmpl": declaresCore,
			"infra/helmfile/b.gotmpl": declaresBuild,
		},
	)
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("both invocations agree with their own helmfile; got %v", err)
	}
	if out.ComparedCount != 2 {
		t.Errorf("each invocation must be paired with ITS OWN helmfile; compared=%d", out.ComparedCount)
	}
}
