// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C17.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
//
// Audit that all 4 split operator main.go files register the
// `operators/internal/metrics` package via blank-import side-effect.
// Without this, controller-runtime metrics registry stays empty in the
// split binary, breaking observability dashboards/alerts that pre-date
// the split (e.g., loop_runtime_misplaced_tenant_total from ADR-0018).
//
// Lives in infra/tests/unit because each operators/cmd/{op}-operator
// is its own `package main` and can't host a test file from a
// different package; reading source as a string is the cleanest gate.

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func operatorsCmdDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "operators", "cmd")
}

func TestOperatorMetricsRegistration_AllSplitBinariesBlankImportMetrics(t *testing.T) {
	cmdDir := operatorsCmdDir()

	binaries := []string{
		"app-operator",
		"platform-operator",
		"preview-operator",
		"tenant-operator",
	}

	// Any import registers the collectors (package init); a binary that also
	// USES the package imports it by name instead of blank.
	expected := `"github.com/getloopscale/loop-runtime/operators/internal/metrics"`

	for _, b := range binaries {
		path := filepath.Join(cmdDir, b, "main.go")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), expected) {
			t.Errorf("%s does not import the metrics package: %s", b, expected)
		}
	}
}
