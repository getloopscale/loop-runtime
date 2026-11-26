// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package repoinvariants

import "testing"

// A command that tests for a script before running it cannot fail with "No such
// file": the guard is what decides whether the script runs. That is how a task runs a
// script from the gitignored kit, whose absence is the state of every fresh clone. The
// exemption is narrow on purpose — only a positive existence test of the SAME path —
// so a dead reference cannot hide behind a guard on something else.
func TestScriptGuardedByExistenceCheck(t *testing.T) {
	const ref = ".claude/skills/x/scripts/check.py"
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"bracket -f on the same path", "if [ -f " + ref + " ]; then python3 " + ref + "; fi", true},
		{"test -e on the same path", "test -e ./" + ref + " && python3 " + ref, true},
		{"guard alongside another guard", "if [ -f BACKLOG.md ] && [ -f " + ref + " ]; then\n  python3 " + ref + " BACKLOG.md\nfi", true},
		{"no guard at all", "python3 " + ref, false},
		{"guard on a different path", "if [ -f other/check.py ]; then python3 " + ref + "; fi", false},
		{"guard on a longer path sharing the prefix", "if [ -f " + ref + ".bak ]; then python3 " + ref + "; fi", false},
		{"negated guard runs the script when it is absent", "if [ ! -f " + ref + " ]; then python3 " + ref + "; fi", false},
	}
	for _, c := range cases {
		if got := scriptGuardedByExistenceCheck(c.cmd, ref); got != c.want {
			t.Errorf("%s: scriptGuardedByExistenceCheck() = %v, want %v", c.name, got, c.want)
		}
	}
}
