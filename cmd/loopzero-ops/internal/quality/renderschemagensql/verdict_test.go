// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package renderschemagensql

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestOutcomeVerdictSkipsAnAdvisoryRunWithAMissingTool(t *testing.T) {
	if got := (Outcome{Warn: true, WarnReason: "docker not installed on PATH"}).Verdict(); got != verdict.Skip {
		t.Fatalf("advisory, tool missing: Verdict() = %v, want SKIP", got)
	}
	if got := (Outcome{SchemaLines: 10}).Verdict(); got != verdict.Pass {
		t.Fatalf("completed: Verdict() = %v, want PASS", got)
	}
}
