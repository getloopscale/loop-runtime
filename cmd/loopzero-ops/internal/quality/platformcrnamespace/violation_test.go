// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platformcrnamespace

import (
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// violationOf folds the check's two channels into one error for the
// assertions in this package: the error when the check could not run,
// otherwise the violation on its outcome. A violation still returned as an
// error breaks the verdict contract, so it is refused, not passed through.
func violationOf(out Output, err error) (Output, error) {
	if err != nil {
		if errors.Is(err, ErrOutsideNamespace) {
			return out, errors.New("a violation was returned as an error instead of on the outcome: " + err.Error())
		}
		return out, err
	}
	return out, out.Violation
}

func TestVerdictIsFailOnlyWhenTheCheckFoundAViolation(t *testing.T) {
	if got := (Output{Violation: ErrOutsideNamespace}).Verdict(); got != verdict.Fail {
		t.Fatalf("with a violation: Verdict() = %v, want FAIL", got)
	}
	if got := (Output{}).Verdict(); got != verdict.Pass {
		t.Fatalf("without a violation: Verdict() = %v, want PASS", got)
	}
}
