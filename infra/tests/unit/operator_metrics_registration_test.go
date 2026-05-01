// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C17.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
//
// Audit that all 4 split operator main.go files register the
// `operators/internal/metrics` package via blank-import side-effect.
// Without this, controller-runtime metrics registry stays empty in the
// split binary, breaking observability dashboards/alerts that pre-date
// the split (e.g., loopzero_misplaced_tenant_total from ADR-0018).
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

	expected := `_ "github.com/loopzero/loopzero/operators/internal/metrics"`

	for _, b := range binaries {
		path := filepath.Join(cmdDir, b, "main.go")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), expected) {
			t.Errorf("%s missing blank-import for metrics package: %s", b, expected)
		}
	}
}

func TestOperatorMetricsRegistration_MonolithStillImports(t *testing.T) {
	// The monolith (`cmd/main.go`) is the source-of-truth registration
	// during the migration window. Forward-only deletion of the monolith
	// happens at T2.5 (sprint 12 horizon per D10). Until then, this
	// test pins that the monolith continues to register as well.
	cmdDir := operatorsCmdDir()
	path := filepath.Join(cmdDir, "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read monolith main.go: %v", err)
	}
	if !strings.Contains(string(data), `"github.com/loopzero/loopzero/operators/internal/metrics"`) {
		t.Error("monolith main.go missing import of operators/internal/metrics")
	}
}
