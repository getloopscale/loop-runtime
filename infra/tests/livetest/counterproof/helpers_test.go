// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package counterproof

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// T3.4 helper-level hermetic tests. Validates byte-grep parsers without
// invoking LIVE deploy ($LOOPRUNTIME commands stay in LIVE-tagged spec).

func TestLayer1_ChaosSummaryByteGrep_PASS_when_10_pass_0_fail(t *testing.T) {
	tmp := t.TempDir()
	summary := filepath.Join(tmp, "summary.md")
	body := ""
	for i := 0; i < 10; i++ {
		body += "Scenario C0X → PASS\n"
	}
	if err := os.WriteFile(summary, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := Layer1_ChaosSummaryByteGrep(summary)
	if !c.OK {
		t.Errorf("OK = false, want true (Detail: %s)", c.Detail)
	}
}

func TestLayer1_ChaosSummaryByteGrep_FAIL_on_any_fail(t *testing.T) {
	tmp := t.TempDir()
	summary := filepath.Join(tmp, "summary.md")
	body := ""
	for i := 0; i < 9; i++ {
		body += "Scenario C0X → PASS\n"
	}
	body += "Scenario C0X → FAIL no recovery\n"
	if err := os.WriteFile(summary, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := Layer1_ChaosSummaryByteGrep(summary)
	if c.OK {
		t.Errorf("OK = true, want false on FAIL present")
	}
}

func TestLayer2_DogfoodJUnitParse_PASS_on_zero_failures(t *testing.T) {
	tmp := t.TempDir()
	xml := filepath.Join(tmp, "junit.xml")
	body := `<?xml version="1.0"?><testsuite tests="102" failures="0" errors="0" skipped="29"/>`
	if err := os.WriteFile(xml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := Layer2_DogfoodJUnitParse(xml)
	if !c.OK {
		t.Errorf("OK = false, want true (Detail: %s)", c.Detail)
	}
}

func TestLayer2_DogfoodJUnitParse_PASS_on_1_failure_baseline(t *testing.T) {
	// Per v1 baseline 2026-05-29: main suite fails<=1 acceptable
	tmp := t.TempDir()
	xml := filepath.Join(tmp, "junit.xml")
	body := `<?xml version="1.0"?><testsuite tests="103" failures="1" errors="0" skipped="29"/>`
	if err := os.WriteFile(xml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := Layer2_DogfoodJUnitParse(xml)
	if !c.OK {
		t.Errorf("OK = false, want true (1 failure baseline acceptable per Detail: %s)", c.Detail)
	}
}

func TestLayer2_DogfoodJUnitParse_FAIL_on_many_failures(t *testing.T) {
	tmp := t.TempDir()
	xml := filepath.Join(tmp, "junit.xml")
	body := `<?xml version="1.0"?><testsuite tests="100" failures="5" errors="0" skipped="0"/>`
	if err := os.WriteFile(xml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := Layer2_DogfoodJUnitParse(xml)
	if c.OK {
		t.Errorf("OK = true, want false (5 failures > 1 baseline threshold)")
	}
}

func TestRunCounterProof_Rejected_on_layer1_fail(t *testing.T) {
	// All fixtures fail-prone → Rejected expected
	tmp := t.TempDir()
	failingChaos := filepath.Join(tmp, "chaos.md")
	failingJunit := filepath.Join(tmp, "junit.xml")
	_ = os.WriteFile(failingChaos, []byte("Scenario → FAIL\n"), 0644)
	_ = os.WriteFile(failingJunit, []byte(`<testsuite tests="10" failures="5"/>`), 0644)

	o := RunCounterProof(context.Background(), tmp, failingChaos, failingJunit)
	if o.Verdict != Rejected {
		t.Errorf("Verdict = %s, want Rejected (failing fixtures)", o.Verdict)
	}
}
