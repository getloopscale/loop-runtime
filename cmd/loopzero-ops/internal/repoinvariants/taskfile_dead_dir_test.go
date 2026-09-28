// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock for issue #254: a Taskfile task that `cd`s into a directory
// which does not exist in this repository.
//
// `cli:link` and `cli:build` ran `cd cli && npm …` long after `cli/` was
// extracted to the sibling repo `loopzero-cli` at M2 of the monorepo decomposition.
// Nothing invoked them, so nothing went red — they simply sat there, and an
// operator who typed `task cli:build` got `cd: cli: No such file or directory`.
//
// The M2/M4 extractions make this a CLASS, not a one-off: `cli/` and `cloud/`
// both left, and any task still reaching into them is the same defect. This
// guard is directory-shaped rather than task-shaped for that reason — it will
// catch the next one without anybody remembering to look.
//
// SCOPE: only `cd <dir>` into a repo-relative path that is absent on disk.
// It does not attempt to validate commands, or `cd` into paths built from
// variables (`cd {{.FOO}}`) — a guess there would produce false positives on
// legitimately dynamic tasks, and a guard that cries wolf gets deleted.
package repoinvariants

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// taskfilesToScan lists the Taskfile plus its includes, relative to the repository root.
var taskfilesToScan = []string{
	"Taskfile.yml",
	"taskfiles/db.yml",
	"taskfiles/dev.yml",
	"taskfiles/drill.yml",
	"taskfiles/helm.yml",
	"taskfiles/infra.yml",
	"taskfiles/k3d.yml",
	"taskfiles/observability.yml",
	"taskfiles/quality.yml",
	"taskfiles/security.yml",
	"taskfiles/test.yml",
}

// reCdLiteral matches `cd <path>` where the path is a literal (no Go-template
// interpolation, no variable expansion, no absolute path).
var reCdLiteral = regexp.MustCompile(`(?:^|[;&|]\s*|\s)cd\s+([A-Za-z0-9_./-]+)`)

// reGoPkgPattern matches the package pattern of a `go test|build|vet|run`
// invocation: the first argument that is not a flag.
//
// It exists because stat'ing the `cd` argument alone was measured insufficient.
// The #254 defect was `cd pkg && go test ./quality/...`: the directory existed
// and the PATTERN did not, so the guard reported clean while the task failed.
var reGoPkgPattern = regexp.MustCompile(`\bgo\s+(?:test|build|vet)\s+((?:\./)?[A-Za-z0-9_./-]+)`)

// deadGoPattern reports the directory a `go` package pattern resolves to when
// that directory does not exist, or "" when the pattern is fine or out of scope.
//
// Scope, deliberately narrow — a guard that flags a working task is a guard
// somebody turns off:
//
//   - `./...` is the whole module and is valid whenever the cd target exists;
//   - a templated pattern is not ours to resolve;
//   - only the directory part is checked, because `...` is a wildcard over
//     packages BELOW a real directory, and that directory is what can go missing.
func deadGoPattern(repoRoot, cdTarget, line string) string {
	m := reGoPkgPattern.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	pat := m[1]
	if strings.Contains(pat, "{{") || strings.HasPrefix(pat, "-") {
		return ""
	}
	pat = strings.TrimPrefix(pat, "./")
	pat = strings.TrimSuffix(pat, "...")
	pat = strings.Trim(pat, "/")
	if pat == "" { // `./...` — the module itself
		return ""
	}
	rel := filepath.Join(cdTarget, pat)
	if _, err := os.Stat(filepath.Join(repoRoot, rel)); err != nil {
		return rel
	}
	return ""
}

// deadCdFinding is one `cd` into a path absent from the repo.
// findingKind distinguishes the two ways a task can point at nothing, because
// the remedy differs and a shared message sends the reader to the wrong line:
// a dead `cd` is visible verbatim in the file, a dead package pattern is a path
// the reader has to compose from two arguments.
type findingKind int

const (
	deadCdTarget findingKind = iota
	deadPkgPattern
)

type deadCdFinding struct {
	file string
	line int
	path string
	kind findingKind
}

// auditTaskfileDeadCd finds `cd <literal>` targets that do not resolve,
// interpreting them relative to repoRoot the way `task` does.
func auditTaskfileDeadCd(repoRoot, relFile, body string) []deadCdFinding {
	var out []deadCdFinding
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, m := range reCdLiteral.FindAllStringSubmatch(line, -1) {
			target := m[1]
			switch {
			case strings.HasPrefix(target, "/"): // absolute — operator machine, not ours to assert
				continue
			case strings.HasPrefix(target, ".."): // escapes the repo (sibling repos) — see note below
				continue
			case strings.Contains(target, "{{"): // templated
				continue
			case target == "-" || target == "~":
				continue
			}
			if _, err := os.Stat(filepath.Join(repoRoot, target)); err != nil {
				out = append(out, deadCdFinding{file: relFile, line: i + 1, path: target, kind: deadCdTarget})
				continue // the cd itself is dead; the pattern under it is moot
			}
			// The cd target exists. The package pattern under it may still not.
			if dead := deadGoPattern(repoRoot, target, line); dead != "" {
				out = append(out, deadCdFinding{file: relFile, line: i + 1, path: dead, kind: deadPkgPattern})
			}
		}
	}
	return out
}

// Note on `..`: a task deliberately reaching a sibling repo (`cd ../loopzero-cli`)
// is a different judgement call — it may be intentional and guarded by an
// `if [ -d … ]`. Asserting on it here would conflate "broken" with "cross-repo
// by design". If cross-repo reach is to be policed, that belongs in its own
// guard with its own rationale.

// TestTaskfilesDoNotCdIntoMissingDirs is the #254 guard.
func TestTaskfilesDoNotCdIntoMissingDirs(t *testing.T) {
	root := qtestutil.MustRepoRoot()

	scanned := 0
	for _, rel := range taskfilesToScan {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("cannot read %s: %v — the scan list is stale, so this file is silently unguarded", rel, err)
			continue
		}
		scanned++
		for _, f := range auditTaskfileDeadCd(root, rel, string(b)) {
			switch f.kind {
			case deadCdTarget:
				t.Errorf("%s:%d — `cd %s` targets a path that does not exist in this repo.\n"+
					"    Invoking the task fails with a raw `cd: %s: No such file or directory` (issue #254).\n"+
					"    If the directory moved to a sibling repo (the M2/M4 extractions), delete the task or\n"+
					"    point it at the sibling behind an existence guard — do not leave it as a trap.",
					f.file, f.line, f.path, f.path)
			case deadPkgPattern:
				t.Errorf("%s:%d — the `go` package pattern resolves to %s, which does not exist.\n"+
					"    The `cd` target is fine; the pattern under it is not, so the task fails with\n"+
					"    `no such file or directory` while every path written on the line looks plausible.\n"+
					"    This is the shape that broke quality:all after M9 moved pkg/quality (B-087).",
					f.file, f.line, f.path)
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("scanned 0 taskfiles — guard would pass vacuously")
	}
}

// TestTaskfilesDeadCd_NegativeControl proves the matcher sees the #254 shape,
// and that it does not fire on the forms it deliberately ignores.
func TestTaskfilesDeadCd_NegativeControl(t *testing.T) {
	root := qtestutil.MustRepoRoot()

	broken := "  cmds:\n    - cd cli && npm run build\n"
	if got := auditTaskfileDeadCd(root, "synthetic.yml", broken); len(got) == 0 {
		t.Fatalf("negative control did NOT fire on `cd cli` — the guard cannot see the exact #254 shape")
	} else if got[0].line != 2 || got[0].path != "cli" {
		t.Errorf("negative control reported %+v, want line 2 / path \"cli\" — attribution is off, so real failures point at the wrong line", got[0])
	}

	// A directory that exists must NOT be flagged.
	if got := auditTaskfileDeadCd(root, "synthetic.yml", "    - cd api && go test ./...\n"); len(got) != 0 {
		t.Errorf("false positive on `cd api`, which exists: %+v — a guard that flags healthy tasks gets deleted", got)
	}
	// Templated and sibling-repo targets are out of scope by design.
	for _, ignored := range []string{
		"    - cd {{.DIR}} && echo hi\n",
		"    - cd ../loopzero-cli && npm test\n",
		"    - cd /tmp && ls\n",
	} {
		if got := auditTaskfileDeadCd(root, "synthetic.yml", ignored); len(got) != 0 {
			t.Errorf("guard fired on a deliberately-ignored form %q: %+v", strings.TrimSpace(ignored), got)
		}
	}
}

// reGlobalContextSwitch matches a mutation of the operator's GLOBAL kubecontext.
var reGlobalContextSwitch = regexp.MustCompile(`kubectl\s+config\s+use-context`)

// TestTaskfilesDoNotMutateGlobalKubecontext locks the #117 root cause out.
//
// `task setup` used to wrap one script in
//
//	kubectl config use-context k3d-loopzero-build
//	bash …/generate-build-secrets.sh standalone
//	kubectl config use-context k3d-loopzero-core
//
// Under `set -e` a failure in the middle line skips the restore, leaving the
// operator pinned to the build cluster. Every later step then runs against the
// wrong target — which is verbatim the "residue from a prior session" repro of
// issue #117. The bootstrap chain manufactured the condition that broke its own
// next run.
//
// The durable fix is that scripts pin their own context (see
// infra/tests/charts/helmfile_scripts_context_pin_test.go); once they do, a
// global switch is never needed, and reintroducing one silently re-arms the
// trap. Pass `--context` per invocation, or set KUBECONFIG for the child —
// both scope to the command instead of mutating shared operator state.
func TestTaskfilesDoNotMutateGlobalKubecontext(t *testing.T) {
	root := qtestutil.MustRepoRoot()
	scanned := 0
	for _, rel := range taskfilesToScan {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("cannot read %s: %v — the scan list is stale, so this file is silently unguarded", rel, err)
			continue
		}
		scanned++
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if reGlobalContextSwitch.MatchString(line) {
				t.Errorf("%s:%d mutates the operator's GLOBAL kubecontext.\n"+
					"    Under `set -e` a failure before the restore leaves the operator pinned to the wrong\n"+
					"    cluster, and every later step runs against it — the root cause of issue #117.\n"+
					"    Scope it instead: `--context` per invocation, or KUBECONFIG for the child process.",
					rel, i+1)
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("scanned 0 taskfiles — guard would pass vacuously")
	}
}

// TestTaskfilesDeadCd_SeesTheDeadPackagePattern is the half the guard was
// missing, and it is the half that would have caught the defect the guard was
// written for.
//
// The #254 shape that broke `task quality:all` was:
//
//	cd pkg && go test ./quality/... -count=1
//
// `pkg` EXISTS — it is a real module. What did not exist was `./quality/`, moved
// out of the shared base at M9. The matcher stat'ed only the `cd` argument, so it
// reported clean while the target failed with
// `lstat ./quality/: no such file or directory`.
//
// An adversarial audit of the B-087 closure reintroduced that exact line and the
// guard passed, exit 0. The item had been closed against a guard that does not
// assert what the item describes — which is the same defect the audit found in
// two sibling items: closing against the easiest thing to measure while the
// bullet asking for the regression guard went unowned.
func TestTaskfilesDeadCd_SeesTheDeadPackagePattern(t *testing.T) {
	root := qtestutil.MustRepoRoot()

	// The #254 line, verbatim.
	got := auditTaskfileDeadCd(root, "synthetic.yml", "    - cd pkg && go test ./quality/... -count=1\n")
	if len(got) == 0 {
		t.Fatal("the guard did not see `cd pkg && go test ./quality/...`. `pkg` exists, so stat'ing the " +
			"cd argument alone reports clean — and this is the exact line that broke quality:all (#254).")
	}
	if got[0].path != "pkg/quality" {
		t.Errorf("the finding names %q; it should name the path that does not exist, resolved against "+
			"the cd target, so the reader can act on it", got[0].path)
	}

	// And it must NOT fire on the healthy forms, or it gets disabled.
	for _, healthy := range []string{
		"    - cd api && go test ./...\n",
		"    - cd api && go test ./internal/builds/... -count=1\n",
		"    - cd cmd/loopzero-ops && go build ./...\n",
		"    - cd api && go test ./internal/... -run TestFoo -v\n",
		"    - cd pkg && go test {{.PATTERN}}\n",
	} {
		if f := auditTaskfileDeadCd(root, "synthetic.yml", healthy); len(f) != 0 {
			t.Errorf("false positive on a healthy target %q: %+v — a guard that flags working tasks is a "+
				"guard somebody turns off", strings.TrimSpace(healthy), f)
		}
	}
}
