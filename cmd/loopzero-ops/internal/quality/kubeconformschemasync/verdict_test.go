// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package kubeconformschemasync

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/kubeconformschema"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestVerdictIsPassOnlyWithoutDrift(t *testing.T) {
	if got := (Outcome{Schemas: 8}).Verdict(); got != verdict.Pass {
		t.Fatalf("no drift: Verdict() = %v, want PASS", got)
	}
	drifted := Outcome{Drift: []kubeconformschema.Drift{{Kind: kubeconformschema.Stale, Name: "x-loopzero.io-v1.json"}}}
	if got := drifted.Verdict(); got != verdict.Fail {
		t.Fatalf("drift: Verdict() = %v, want FAIL", got)
	}
}
