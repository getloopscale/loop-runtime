// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package helmtopology

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func TestReportVerdictFollowsSyncInCheckMode(t *testing.T) {
	if got := (Report{Mode: ModeCheck, InSync: true}).Verdict(); got != verdict.Pass {
		t.Fatalf("in sync: Verdict() = %v, want PASS", got)
	}
	if got := (Report{Mode: ModeCheck}).Verdict(); got != verdict.Fail {
		t.Fatalf("out of sync: Verdict() = %v, want FAIL", got)
	}
}

func TestReportVerdictOfAWriteIsPass(t *testing.T) {
	if got := (Report{Mode: ModeWrite}).Verdict(); got != verdict.Pass {
		t.Fatalf("write: Verdict() = %v, want PASS", got)
	}
}
