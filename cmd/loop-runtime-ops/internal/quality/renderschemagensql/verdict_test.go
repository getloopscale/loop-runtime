// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package renderschemagensql

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func TestOutcomeVerdictSkipsAnAdvisoryRunWithAMissingTool(t *testing.T) {
	if got := (Outcome{Warn: true, WarnReason: "docker not installed on PATH"}).Verdict(); got != verdict.Skip {
		t.Fatalf("advisory, tool missing: Verdict() = %v, want SKIP", got)
	}
	if got := (Outcome{SchemaLines: 10}).Verdict(); got != verdict.Pass {
		t.Fatalf("completed: Verdict() = %v, want PASS", got)
	}
}
