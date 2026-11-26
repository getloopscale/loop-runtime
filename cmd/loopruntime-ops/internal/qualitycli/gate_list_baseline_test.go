// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Regression-lock: `task quality:gate-list` reads the same frozen gate set the
// oracle test owns.
//
// The oracle moved to testdata/gateset.frozen (B-006) and its old home,
// docs/operations/baselines/, was deleted by the 2026-09-11 docs
// consolidation. The Taskfile kept `BASE=docs/operations/baselines/…`, so the
// gate-count assertion ran `grep -vcE` on a missing file and could never pass.
// Two copies of one path drifted; this test makes the Taskfile name the one the
// oracle test declares.
package qualitycli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var reGateListBase = regexp.MustCompile(`(?m)^\s*BASE=(\S+)\s*$`)

func TestQualityGateListTaskReadsTheOracleBaseline(t *testing.T) {
	body, err := os.ReadFile("../../../../taskfiles/quality.yml")
	if err != nil {
		t.Fatalf("read taskfiles/quality.yml: %v", err)
	}
	m := reGateListBase.FindSubmatch(body)
	if m == nil {
		t.Fatal("taskfiles/quality.yml: gate-list sets no BASE= — the gate-count assertion has no baseline")
	}
	base := string(m[1])
	want := filepath.ToSlash(filepath.Join("cmd/loopruntime-ops/internal/qualitycli", oracleBaselinePath))
	if base != want {
		t.Errorf("gate-list reads BASE=%s; the oracle test owns %s — two baselines drift apart", base, want)
	}
	if _, err := os.Stat(filepath.Join("../../../..", base)); err != nil {
		t.Errorf("gate-list baseline %s does not resolve from the repo root: %v", base, err)
	}
}
