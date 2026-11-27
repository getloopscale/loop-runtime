// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package repoinvariants

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

// `k3d:secrets:create` must not repoint the operator's kubectl in silence.
//
// B-024. `k3d cluster create` switches current-context by default, and that
// side effect was the defect: seeds inherit the active context, so any seed run
// after this task operated the secrets cluster believing it was core.
//
// The parsimony ladder stopped at rung 3, a native feature:
// `--kubeconfig-switch-context=false` exists in k3d v5.7.4. There is no restore
// logic to write, and `--kubeconfig-update-default` stays at its default `true`,
// so the context entry is still written and every `--context k3d-loop-runtime-secrets`
// in the task keeps working.
//
// Only this task changes. The other cluster-create calls run inside
// `task setup`, where switching to the cluster just built is the expected
// behaviour — a judgement, not a measurement. What is measured is that the
// taskfiles hold six `k3d cluster create` calls and none restores the context.
// The structural protection against inheriting it lives in CORE_CONTEXT, which
// pins the cluster at the seeds' composition root.
func TestSecretsCreate_DoesNotHijackTheOperatorsContext(t *testing.T) {
	cmds := taskCommands(readTaskfile(t, "k3d.yml"), "k3d:secrets:create:")
	if strings.TrimSpace(cmds) == "" {
		t.Fatal("task k3d:secrets:create not found — was it renamed?")
	}
	createLine := ""
	for _, l := range strings.Split(cmds, "\n") {
		if strings.Contains(l, "k3d cluster create") {
			createLine = l
			break
		}
	}
	if createLine == "" {
		t.Fatal("the task no longer creates a cluster — this test is measuring something else")
	}
	// The flag must be on the command's OWN line: across the whole block, a
	// comment about it would satisfy the assertion.
	if !strings.Contains(createLine, "--kubeconfig-switch-context=false") {
		t.Error("k3d:secrets:create switches the operator's active context in silence; " +
			"declare --kubeconfig-switch-context=false")
	}
}

// The context entry MUST still be written, or the task breaks itself: every
// one of its commands uses `--context k3d-loop-runtime-secrets`.
func TestSecretsCreate_StillWritesTheContextEntry(t *testing.T) {
	cmds := taskCommands(readTaskfile(t, "k3d.yml"), "k3d:secrets:create:")
	if strings.Contains(cmds, "--kubeconfig-update-default=false") {
		t.Error("turning update-default off deletes the context entry and breaks the task's own " +
			"`--context k3d-loop-runtime-secrets` commands")
	}
	if !strings.Contains(cmds, "--context k3d-loop-runtime-secrets") {
		t.Error("the task no longer pins the context on its own commands")
	}
}

func readTaskfile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(qtestutil.MustRepoRoot(), "taskfiles", name))
	if err != nil {
		t.Fatalf("read taskfiles/%s: %v", name, err)
	}
	return string(b)
}

// taskCommands cuts out one task's section and returns ONLY its command lines.
//
// Dropping comments is not a detail: the first version of this guard matched
// the whole block, and a comment added in the same commit carried both strings
// it searched for. Two mutants survived — the flag removed from the command, and
// the command line deleted outright — because the guard was satisfied by the
// text describing it. The runbook-parity gate paid for the same trap.
func taskCommands(body, header string) string {
	var b strings.Builder
	for _, line := range strings.Split(taskSection(body, header), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// taskSection cuts from the task header to the next task at the same indent.
func taskSection(body, header string) string {
	i := strings.Index(body, header)
	if i < 0 {
		return ""
	}
	rest := body[i+len(header):]
	// The next task starts with two spaces and a name ending in a colon.
	for _, next := range []string{"\n  k3d:", "\n  clusters:", "\n  registry:"} {
		if j := strings.Index(rest, next); j >= 0 {
			rest = rest[:j]
		}
	}
	return rest
}
