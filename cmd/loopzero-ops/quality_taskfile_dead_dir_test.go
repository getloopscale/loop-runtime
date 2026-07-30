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
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// taskfilesToScan lists the Taskfile plus its includes, relative to cmd/loopzero-ops.
var taskfilesToScan = []string{
	"../../Taskfile.yml",
	"../../taskfiles/db.yml",
	"../../taskfiles/dev.yml",
	"../../taskfiles/drill.yml",
	"../../taskfiles/helm.yml",
	"../../taskfiles/infra.yml",
	"../../taskfiles/k3d.yml",
	"../../taskfiles/observability.yml",
	"../../taskfiles/quality.yml",
	"../../taskfiles/security.yml",
	"../../taskfiles/test.yml",
}

// reCdLiteral matches `cd <path>` where the path is a literal (no Go-template
// interpolation, no variable expansion, no absolute path).
var reCdLiteral = regexp.MustCompile(`(?:^|[;&|]\s*|\s)cd\s+([A-Za-z0-9_./-]+)`)

// deadCdFinding is one `cd` into a path absent from the repo.
type deadCdFinding struct {
	file string
	line int
	path string
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
				out = append(out, deadCdFinding{file: relFile, line: i + 1, path: target})
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

func repoRootFromCmd(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// TestTaskfilesDoNotCdIntoMissingDirs is the #254 guard.
func TestTaskfilesDoNotCdIntoMissingDirs(t *testing.T) {
	root := repoRootFromCmd(t)

	scanned := 0
	for _, rel := range taskfilesToScan {
		b, err := os.ReadFile(rel)
		if err != nil {
			t.Errorf("cannot read %s: %v — the scan list is stale, so this file is silently unguarded", rel, err)
			continue
		}
		scanned++
		for _, f := range auditTaskfileDeadCd(root, rel, string(b)) {
			t.Errorf("%s:%d — `cd %s` targets a path that does not exist in this repo.\n"+
				"    Invoking the task fails with a raw `cd: %s: No such file or directory` (issue #254).\n"+
				"    If the directory moved to a sibling repo (the M2/M4 extractions), delete the task or\n"+
				"    point it at the sibling behind an existence guard — do not leave it as a trap.",
				f.file, f.line, f.path, f.path)
		}
	}
	if scanned == 0 {
		t.Fatalf("scanned 0 taskfiles — guard would pass vacuously")
	}
}

// TestTaskfilesDeadCd_NegativeControl proves the matcher sees the #254 shape,
// and that it does not fire on the forms it deliberately ignores.
func TestTaskfilesDeadCd_NegativeControl(t *testing.T) {
	root := repoRootFromCmd(t)

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
