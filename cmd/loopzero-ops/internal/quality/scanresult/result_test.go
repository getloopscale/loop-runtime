// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scanresult

import (
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func TestResult_NothingScannedIsNotAPass(t *testing.T) {
	r := Result{}
	if !errors.Is(r.Err(), ErrNothingScanned) {
		t.Fatalf("want ErrNothingScanned, got %v", r.Err())
	}
	if r.Pass() {
		t.Fatal("a scan that read nothing must not pass")
	}
}

func TestResult_ScannedWithoutFindingsPasses(t *testing.T) {
	r := Result{Scanned: 3}
	if r.Err() != nil || !r.Pass() {
		t.Fatalf("want a pass, got err=%v pass=%v", r.Err(), r.Pass())
	}
}

func TestResult_FindingsFail(t *testing.T) {
	r := Result{Scanned: 3, Findings: []string{"a.go:1 violation"}}
	if r.Err() != nil {
		t.Fatalf("findings are a verdict, not a structural error: %v", r.Err())
	}
	if r.Pass() {
		t.Fatal("a scan with findings must not pass")
	}
}

func TestResult_DeclaredPartialScanOfNothingIsAccepted(t *testing.T) {
	r := Result{Partial: "optional subtree absent in this checkout"}
	if r.Err() != nil || !r.Pass() {
		t.Fatalf("a declared partial scan must pass, got err=%v", r.Err())
	}
	if !strings.Contains(r.Summary(), "PARTIAL: optional subtree absent") {
		t.Errorf("the summary must say the scan was partial: %q", r.Summary())
	}
}

func TestResult_SummaryCarriesTheCounts(t *testing.T) {
	got := Result{Scanned: 4, Findings: []string{"x"}}.Summary()
	if got != "4 scanned, 1 finding(s)" {
		t.Errorf("Summary() = %q", got)
	}
}

func TestResult_VerdictOfANothingScannedIsNotVerified(t *testing.T) {
	if got := (Result{}).Verdict(); got != verdict.NotVerified {
		t.Fatalf("Verdict() = %v, want NOT VERIFIED — reading nothing verifies nothing", got)
	}
}

func TestResult_VerdictOfFindingsIsFail(t *testing.T) {
	if got := (Result{Scanned: 1, Findings: []string{"x"}}).Verdict(); got != verdict.Fail {
		t.Fatalf("Verdict() = %v, want FAIL", got)
	}
}

func TestResult_VerdictOfACleanScanIsPass(t *testing.T) {
	if got := (Result{Scanned: 1}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

func TestResult_VerdictOfADeclaredPartialScanIsPass(t *testing.T) {
	if got := (Result{Partial: "sibling absent"}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}
