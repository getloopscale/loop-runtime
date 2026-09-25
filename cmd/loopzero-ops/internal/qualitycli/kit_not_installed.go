// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"fmt"
)

// errNotVerified marks a check that could not verify anything on this machine
// and has already said why. The runner exits Incomplete without printing a
// second, generic line over the gate's own explanation.
var errNotVerified = errors.New("not verified")

// kitNotInstalled is the one verdict a gate over the installed kit gives on a
// machine that does not have it. `.claude/` is gitignored, so this is every
// fresh clone: the gate verified nothing, and exit 5 keeps that apart from both
// a pass and a violation found in the kit.
func kitNotInstalled(inv Invocation, err error) (Outcome, error) {
	inv.Stderr.Printf("%s: NOT VERIFIED — %v\n", inv.Name, err)
	inv.Stderr.Printf("  %s checks the installed kit under .claude/, which .gitignore excludes;\n"+
		"  it can only be verified on a machine where the kit is installed.\n", inv.Name)
	return Fail, fmt.Errorf("%w: %w", errNotVerified, err)
}
