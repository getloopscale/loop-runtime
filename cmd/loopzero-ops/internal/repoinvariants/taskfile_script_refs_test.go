// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock: every script a Taskfile command runs exists in the tree.
//
// `task --list` advertised 28 tasks whose only command ran a script that had
// been deleted — the parity/*.sh suite, the archive/canonical-tests-v1 dogfood
// wrappers, two chaos scenarios, the security-context audit script. Each one
// failed with "No such file" for whoever typed it. The dangling-script-refs
// gate only looks at a DIFF, so a reference that rotted before any diff touched
// it was never re-examined. This test reads the WHOLE Taskfile set on every run.
//
// SCOPE: literal paths passed to bash/sh/python3 inside a task's cmds. A path
// built from a template variable is resolved at run time and is not judged here.
// Paths are resolved against the task's `dir:` (or the repo root) and, when the
// command `cd`s into a literal directory first, against that directory too.
//
// A command that tests for the SAME path before running it (`[ -f <path> ]`,
// `test -e <path>`) is not a dead reference: the guard decides whether the script
// runs, so the task cannot fail with "No such file". That is how a task reaches a
// script in the gitignored kit (docs/decisions.md, 2026-09-26). Only a positive
// existence test of the exact path counts; a guard on anything else does not.
package repoinvariants

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
	"gopkg.in/yaml.v3"
)

// reScriptInvocation matches `bash|sh|python3 <path>` where <path> contains a
// slash (a bare word is a command, not a file of this repository).
var reScriptInvocation = regexp.MustCompile(`(?:^|[\s;&|(])(?:bash|sh|python3)\s+((?:\./)?[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+)`)

// reCdTarget matches a literal `cd <dir>` preceding the invocation.
var reCdTarget = regexp.MustCompile(`(?:^|[\s;&|(])cd\s+([A-Za-z0-9_./-]+)`)

type scriptRefTask struct {
	Dir  string      `yaml:"dir"`
	Cmds []yaml.Node `yaml:"cmds"`
}

type scriptRefTaskfile struct {
	Tasks map[string]scriptRefTask `yaml:"tasks"`
}

// commandText returns the shell text of one cmds entry: a plain string, or the
// `cmd:` of a map entry. A `task:` entry runs no script and yields "".
func commandText(n yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "cmd" {
				return n.Content[i+1].Value
			}
		}
	}
	return ""
}

// scriptGuardedByExistenceCheck reports whether cmd tests that ref exists (`[ -f ref ]`,
// `[ -e ref ]`, `test -f ref`, `test -e ref`, with or without a leading `./`) before it
// could run it. A negated test does not count: it runs the script when it is absent.
func scriptGuardedByExistenceCheck(cmd, ref string) bool {
	path := regexp.QuoteMeta(strings.TrimPrefix(ref, "./"))
	guard := regexp.MustCompile(`(?:\[|\btest)\s+-[ef]\s+['"]?(?:\./)?` + path + `['"]?(?:\s|\]|;|&|$)`)
	return guard.MatchString(cmd)
}

func scriptResolves(repoRoot, taskDir, cmd, ref string) bool {
	bases := []string{filepath.Join(repoRoot, taskDir)}
	for _, m := range reCdTarget.FindAllStringSubmatch(cmd, -1) {
		if !strings.Contains(m[1], "{{") {
			bases = append(bases, filepath.Join(repoRoot, taskDir, m[1]))
		}
	}
	for _, b := range bases {
		if _, err := os.Stat(filepath.Join(b, ref)); err == nil {
			return true
		}
	}
	return false
}

func TestTaskfileCommandsRunScriptsThatExist(t *testing.T) {
	repoRoot := qtestutil.MustRepoRoot()
	var checked int
	for _, tf := range taskfilesToScan {
		body, err := os.ReadFile(filepath.Join(repoRoot, tf))
		if err != nil {
			t.Fatalf("read %s: %v", tf, err)
		}
		var doc scriptRefTaskfile
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("parse %s: %v", tf, err)
		}
		for name, task := range doc.Tasks {
			if strings.Contains(task.Dir, "{{") {
				continue
			}
			for _, c := range task.Cmds {
				cmd := commandText(c)
				for _, m := range reScriptInvocation.FindAllStringSubmatch(cmd, -1) {
					ref := m[1]
					if strings.Contains(ref, "{{") || strings.Contains(ref, "$") {
						continue
					}
					checked++
					if scriptGuardedByExistenceCheck(cmd, ref) {
						continue
					}
					if !scriptResolves(repoRoot, task.Dir, cmd, ref) {
						t.Errorf("%s: task %q runs %s, which does not exist — the task can only fail with \"No such file\"",
							tf, name, ref)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no script invocation found in any Taskfile; the guard is measuring nothing")
	}
}
