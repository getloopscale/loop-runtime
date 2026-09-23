// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package gitref holds the git-ref checks shared by the quality gates that
// compare a base ref against a head ref. A gate that cannot resolve its base
// has measured nothing, so it must stop before diffing rather than read an
// empty diff as a clean one.
package gitref

import (
	"fmt"
	"os/exec"
)

// VerifyRefReachable returns a BASE_REF_UNREACHABLE error when ref does not
// resolve to a commit in the repository at repoRoot — typically a shallow CI
// clone that never fetched the base.
func VerifyRefReachable(repoRoot, ref string) error {
	// #nosec G204 -- ref is a CI-controlled git ref, not user input.
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"BASE_REF_UNREACHABLE: base ref %q not found in local git history. "+
				"Likely a shallow clone (default GitHub Actions depth=1). "+
				"Fix: set `fetch-depth: 0` on actions/checkout, OR `git fetch origin %s`. Underlying: %v",
			ref, ref, err)
	}
	return nil
}
