// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testpyramid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInventory(t *testing.T, unit, integ, e2e float64) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-inventory.json")
	content := `{"summary":{"ratio_unit_pct":` + ftoa(unit) +
		`,"ratio_integration_pct":` + ftoa(integ) +
		`,"ratio_e2e_pct":` + ftoa(e2e) + `}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func ftoa(f float64) string {
	// simple float formatting for fixtures
	switch f {
	case 80:
		return "80.0"
	case 70:
		return "70.0"
	case 60:
		return "60.0"
	case 15:
		return "15.0"
	case 25:
		return "25.0"
	case 30:
		return "30.0"
	case 5:
		return "5.0"
	case 3:
		return "3.0"
	case 10:
		return "10.0"
	}
	return "0.0"
}

func defaultThresholds() Thresholds {
	return Thresholds{UnitMinPct: 70, IntegrationMaxPct: 25, E2EMaxPct: 5}
}

// TestCheck_HealthyPyramid_Pass — ratios within bounds → PASS.
func TestCheck_HealthyPyramid_Pass(t *testing.T) {
	inv := writeInventory(t, 80, 15, 3)
	o, err := Check(Config{InventoryFile: inv, Thresholds: defaultThresholds()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass, got %v", o.Violations)
	}
}

// TestCheck_TooFewUnit_Fail — unit below min → violation.
func TestCheck_TooFewUnit_Fail(t *testing.T) {
	inv := writeInventory(t, 60, 30, 10)
	o, err := Check(Config{InventoryFile: inv, Thresholds: defaultThresholds()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (unit 60% < 70%)")
	}
	if !containsSubstr(o.Violations, "unit") {
		t.Errorf("expected unit violation, got %v", o.Violations)
	}
}

// TestCheck_TooMuchIntegration_Fail — integration above max → violation.
func TestCheck_TooMuchIntegration_Fail(t *testing.T) {
	inv := writeInventory(t, 70, 30, 3)
	o, err := Check(Config{InventoryFile: inv, Thresholds: defaultThresholds()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (integration 30% > 25%)")
	}
}

// TestCheck_TooMuchE2E_Fail — e2e above max → violation.
func TestCheck_TooMuchE2E_Fail(t *testing.T) {
	inv := writeInventory(t, 80, 15, 10)
	o, err := Check(Config{InventoryFile: inv, Thresholds: defaultThresholds()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (e2e 10% > 5%)")
	}
}

// TestCheck_DeferLabel_Pass — pyramid-defer label overrides.
func TestCheck_DeferLabel_Pass(t *testing.T) {
	inv := writeInventory(t, 60, 30, 10) // out of bounds
	o, err := Check(Config{
		InventoryFile: inv,
		Thresholds:    defaultThresholds(),
		PRLabels:      "foo, pyramid-defer , bar",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (pyramid-defer label), got %v", o.Violations)
	}
	if !o.Deferred {
		t.Error("expected Deferred=true")
	}
}

// TestCheck_MissingInventory_Errors — no inventory file → error.
func TestCheck_MissingInventory_Errors(t *testing.T) {
	_, err := Check(Config{InventoryFile: "/nonexistent/inventory.json", Thresholds: defaultThresholds()})
	if err == nil {
		t.Error("expected error on missing inventory file")
	}
}

// TestCheck_BoundaryExact_Pass — exactly at thresholds → PASS (>= / <=).
func TestCheck_BoundaryExact_Pass(t *testing.T) {
	inv := writeInventory(t, 70, 25, 5) // exactly at bounds
	o, err := Check(Config{InventoryFile: inv, Thresholds: defaultThresholds()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass at exact boundaries, got %v", o.Violations)
	}
}

func containsSubstr(items []string, sub string) bool {
	for _, s := range items {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
