// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Regression-lock: a Taskfile command that go-task cannot even RENDER.
//
// `task helm:sync-crds` — the remediation the `crd-semantic-diff` gate tells
// operators to run — died with
//
//	template: :11: bad character U+0024 '$'
//
// because its heredoc emits Helm templates (`{{- if .Values.crds.${short} }}`)
// and go-task renders every command as a Go template BEFORE handing it to the
// shell. It saw `{{- if …` as its own action and choked on the shell `$`.
//
// The consequence was not a loud failure but a silent one: the CRD chart drifted
// three features behind kubebuilder (`project` on environments, `cellId` on
// previewenvironments, the `Cleaned` terminal state on loopruntimeapplications), the
// gate correctly reported drift, and the fix it prescribed could never run. A
// gate whose remediation is broken reports a problem nobody can close.
//
// `task --dry` renders the templates without executing them, so this guard is
// cheap and side-effect-free — and it covers the whole class: any task that
// emits Helm/Go-template braces without escaping them fails here at test time
// instead of in an operator's terminal.
package repoinvariants

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// taskDryRunTargets are the tasks whose bodies emit `{{ … }}` and are therefore
// exposed to the go-task/Helm template collision. Add a task here when it starts
// generating templated YAML.
var taskDryRunTargets = []string{
	"helm:sync-crds",
}

// TestTaskfileCommandsRender asserts go-task can render each listed task's
// command body. It does NOT execute the task (`--dry`), so it neither writes
// files nor touches a cluster.
func TestTaskfileCommandsRender(t *testing.T) {
	if _, err := exec.LookPath("task"); err != nil {
		// A failure, not a skip: go-task is a declared prerequisite, and a skip
		// would erase the guard on exactly the machine that lacks it.
		t.Fatal("go-task is not on PATH, and it is a declared prerequisite of this repository")
	}

	for _, target := range taskDryRunTargets {
		t.Run(target, func(t *testing.T) {
			cmd := exec.Command("task", "--dry", target)
			cmd.Dir = qtestutil.MustRepoRoot()
			out, err := cmd.CombinedOutput()

			if err != nil {
				t.Errorf("`task --dry %s` failed to RENDER: %v\n%s\n\n"+
					"    go-task renders every command as a Go template before the shell sees it, so a\n"+
					"    heredoc emitting Helm braces (`{{- if .Values… }}`) is parsed as a go-task action.\n"+
					"    Escape the literal braces (`{{ \"{{\" }}`) so the task can run.\n"+
					"    This is not cosmetic: while this task cannot run, the remediation that\n"+
					"    `crd-semantic-diff` prescribes is unavailable and the CRD chart silently drifts.",
					target, err, out)
				return
			}
			// go-task can exit 0 while still printing a template error for a
			// sub-command; treat that text as failure too.
			if s := string(out); strings.Contains(s, "bad character") || strings.Contains(s, "template:") {
				t.Errorf("`task --dry %s` exited 0 but printed a template error:\n%s", target, s)
			}
		})
	}
}
