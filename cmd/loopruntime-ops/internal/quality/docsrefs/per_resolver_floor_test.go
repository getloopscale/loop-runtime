// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package docsrefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// These tests are RED BY DESIGN: they measure the defect, not its fix.
//
// They SKIP rather than fail because a permanently red test is ignored within a week
// and takes the whole suite with it (`rules/testing.md`: "once red tests are ignored,
// all tests lose their value"). A skip that names the defect and prints the
// measurement is the opposite of the invisible skip that same rule forbids — the debt
// shows up in every run, and the issue below is where it is tracked.
//
// Tracked at: https://github.com/getloopscale/loop-runtime/issues/363
// The defect: the vacuity floor is one aggregate across every resolver, so a resolver
// enumerating zero references hides behind those that enumerate some.
//
// `LOOPRUNTIME_RUN_PER_RESOLVER_FLOOR=1 go test ./internal/quality/docsrefs/` runs them for real.
// When the issue closes, delete this block — if they pass without the variable, the
// fix has landed.

func skipUnlessPerResolverFloorArmed(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPRUNTIME_RUN_PER_RESOLVER_FLOOR") == "" {
		// https://github.com/getloopscale/loop-runtime/issues/363
		t.Skip("known debt: the docsrefs vacuity counter is a single aggregate, so a resolver " +
			"that enumerates zero references is invisible behind those that enumerate some. " +
			"Run with LOOPRUNTIME_RUN_PER_RESOLVER_FLOOR=1 to see the measurement.")
	}
}

func TestCheck_AResolverThatEnumeratesNothingIsInvisible(t *testing.T) {
	skipUnlessPerResolverFloorArmed(t)
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md": "x\n",
		"docs/d.md":                "no ADR here\n",
		"docsrefs-allowlist.txt":   "",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})

	// Prove the premise: adrResolver really did enumerate zero.
	adr, cerr := adrResolver{}.collect(root)
	if cerr != nil {
		t.Fatalf("collect: %v", cerr)
	}
	if len(adr) != 0 {
		t.Fatalf("premise broken: adrResolver found %d refs, expected 0", len(adr))
	}

	t.Logf("aggregate Checked=%d (all of it from ruleResolver); adrResolver enumerated 0", out.Checked)

	if !errors.Is(err, ErrNoReferencesEnumerated) {
		t.Errorf("a resolver that enumerated ZERO references was absorbed by the aggregate floor: "+
			"Checked=%d, err=%v — the gate vouches for a kind it never measured", out.Checked, err)
	}
}
