// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package revrange is the pre-flight the diff-scoped gates share before they
// read `git diff base...head`: the base must resolve to a commit in local
// history. On a shallow clone it does not, git fails, and a gate that treats
// that failure as an empty diff reports PASS over a comparison it never made.
package revrange

import (
	"errors"
	"fmt"
	"os/exec"
)

// ErrBaseRefUnreachable means the base ref is not a commit in local history.
var ErrBaseRefUnreachable = errors.New("BASE_REF_UNREACHABLE: base ref not found in local git history")

// VerifyReachable confirms ref resolves to a commit in the repository at
// repoRoot ("" is the working directory). The error names the ref and the fix.
func VerifyReachable(repoRoot, ref string) error {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %q. This typically means a shallow clone (default GitHub Actions "+
			"checkout depth=1). Fix: set `fetch-depth: 0` on the checkout step, OR run "+
			"`git fetch origin %s` before invoking the gate. Underlying: %v",
			ErrBaseRefUnreachable, ref, ref, err)
	}
	return nil
}
