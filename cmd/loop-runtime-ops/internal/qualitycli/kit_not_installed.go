// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

// reportKitNotInstalled explains a Skip verdict from a gate over the installed
// kit on a machine that does not have it. `.claude/` is gitignored, so this is
// every fresh clone: the gate verified nothing, and the named reason keeps that
// skip apart from a pass (owner decision, 2026-09-26, docs/decisions.md).
func reportKitNotInstalled(inv Invocation, why error) {
	inv.Stdout.Printf("%s: SKIP — %v\n", inv.Name, why)
	inv.Stdout.Printf("  %s checks the installed kit, which .gitignore excludes; a clone without it\n"+
		"  has nothing of this repository's own to verify, so nothing was concluded here.\n", inv.Name)
}
