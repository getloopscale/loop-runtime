// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ecosystemvalidators

import (
	"strings"
	"testing"
)

// `verify_ecosystem` must be asked about THIS PUSH, not about the last 40 commits.
//
// The two are different questions and both are legitimate. The standalone audit asks
// whether the repository follows its conventions, and grading history is the point.
// A pre-push hook asks whether THIS PUSH may land, and there the only violations that
// matter are the ones the pusher can still fix.
//
// Measured 2026-09-16, before this test existed: the gate graded the last 40 commits,
// three of its four findings were already on `origin/workspace`, and the two nearest
// would leave the window in 11 and 13 commits — which could not happen, because the
// gate refused the commits that would have moved it. Every part was individually
// correct and together they were a deadlock, on a chain that runs
// `.git/hooks/pre-push` -> `task quality:gates` -> this package -> `verify_ecosystem`.
//
// `verify_ecosystem.py` grew `--introduced` for exactly this caller. The flag on the
// script is half the fix; a caller that does not pass it is the other half, and that
// half is what kept the deadlock alive for an hour after the script was fixed.
func TestVerifyEcosystemIsAskedAboutTheIntroducedRange(t *testing.T) {
	var found *Validator
	for i := range DefaultValidators {
		if DefaultValidators[i].Name == "verify_ecosystem" {
			found = &DefaultValidators[i]
			break
		}
	}
	if found == nil {
		t.Fatal("verify_ecosystem is not in DefaultValidators — the chain this test guards does not exist")
	}
	if len(found.Args) == 0 {
		t.Fatalf("verify_ecosystem is invoked with no arguments, so it grades the last 40 commits — " +
			"including commits already on the remote, which no amend can reach")
	}
	if !contains(found.Args, "--introduced") {
		t.Errorf("verify_ecosystem must be asked about the introduced range; got args %v", found.Args)
	}
}

// The other validator must NOT inherit the flag by accident: `check_xrefs` answers a
// question about the tree as it stands, and a range means nothing to it.
func TestOnlyTheRangeAwareValidatorCarriesTheFlag(t *testing.T) {
	for _, v := range DefaultValidators {
		if v.Name == "verify_ecosystem" {
			continue
		}
		if contains(v.Args, "--introduced") {
			t.Errorf("%s carries --introduced, which it has no range to apply it to", v.Name)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if strings.TrimSpace(x) == want {
			return true
		}
	}
	return false
}
