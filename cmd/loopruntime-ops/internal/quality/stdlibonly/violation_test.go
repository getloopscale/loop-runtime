// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package stdlibonly_test

import (
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/stdlibonly"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// violationOf folds the check's two channels into one error for the
// assertions in this package: the error when the check could not run,
// otherwise the violation on its outcome. A violation still returned as an
// error breaks the verdict contract, so it is refused, not passed through.
func violationOf(out stdlibonly.Outcome, err error) (stdlibonly.Outcome, error) {
	if err != nil {
		if errors.Is(err, stdlibonly.ErrExternalImport) {
			return out, errors.New("a violation was returned as an error instead of on the outcome: " + err.Error())
		}
		return out, err
	}
	return out, out.Violation
}

func TestVerdictIsFailOnlyWhenTheCheckFoundAViolation(t *testing.T) {
	if got := (stdlibonly.Outcome{Violation: stdlibonly.ErrExternalImport}).Verdict(); got != verdict.Fail {
		t.Fatalf("with a violation: Verdict() = %v, want FAIL", got)
	}
	if got := (stdlibonly.Outcome{}).Verdict(); got != verdict.Pass {
		t.Fatalf("without a violation: Verdict() = %v, want PASS", got)
	}
}
