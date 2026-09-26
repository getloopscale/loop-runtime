// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

// reportKitNotInstalled explains a NotVerified verdict from a gate over the
// installed kit on a machine that does not have it. `.claude/` is gitignored,
// so this is every fresh clone: the gate verified nothing, and exit 5 keeps
// that apart from both a pass and a violation found in the kit.
func reportKitNotInstalled(inv Invocation, why error) {
	inv.Stderr.Printf("%s: NOT VERIFIED — %v\n", inv.Name, why)
	inv.Stderr.Printf("  %s checks the installed kit under .claude/, which .gitignore excludes;\n"+
		"  it can only be verified on a machine where the kit is installed.\n", inv.Name)
}
