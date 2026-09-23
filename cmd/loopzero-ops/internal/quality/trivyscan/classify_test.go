// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock: `trivy fs --exit-code 1` returns **1 for two unrelated
// outcomes** — "matching-severity vulnerabilities were found" and "the scan
// could not complete". Check() separates them (exit 1 => findings, any other
// exit => execution error), but the exit code alone cannot, because trivy
// spends the same 1 on a FATAL.
//
// Observed 2026-07-30 during a `task quality:gates` run:
//
//	trivy-scan: FAIL
//	trivy fs found vulnerabilities (severity=CRITICAL,HIGH):
//	FATAL  Error  remote Maven repository returned 429 Too Many Requests
//	       for https://repo.maven.apache.org/... Retry-After: 1800.
//
// Zero vulnerabilities were found. Maven Central rate-limited the host. The
// gate reported a security failure, aborted the suite, and every gate wired
// after it never ran.
//
// Both directions of that mistake are bad, and the second is worse:
//   - a red suite blamed on security when the cause is a network rate limit
//     trains people to re-run until green;
//   - and because the message says "found vulnerabilities", a reader has no
//     signal that the scan did not actually happen. Silence about coverage is
//     how a security gate becomes decorative.
//
// The honest classification is an execution error: Check() returns
// ErrScanFailed carrying the FATAL line. The gate still goes red — no scan
// happened, so there is no verdict to report as green — but it says why, and it
// never claims vulnerabilities were found.
package trivyscan

import "testing"

func TestClassifyExitOne(t *testing.T) {
	cases := []struct {
		name       string
		out        string
		wantExec   bool
		wantDetail string // substring expected in the surfaced detail
	}{
		{
			name: "maven rate limit is an execution error, not a finding",
			out: "2026-07-30T13:51:06-03:00\tFATAL\tError\tremote Maven repository returned 429 Too Many Requests " +
				"for https://repo.maven.apache.org/maven2/org/sonatype/oss/oss-parent/9/oss-parent-9.pom. Retry-After: 1800.",
			wantExec:   true,
			wantDetail: "429",
		},
		{
			name:       "db download failure is an execution error",
			out:        "2026-07-30T10:00:00Z\tFATAL\tinit error: DB error: failed to download vulnerability DB",
			wantExec:   true,
			wantDetail: "vulnerability DB",
		},
		{
			name: "a real vulnerability table is a finding",
			out: "go.sum (gomod)\n==============\nTotal: 2 (HIGH: 1, CRITICAL: 1)\n" +
				"┌─────────┬────────────────┬──────────┐\n│ Library │ Vulnerability  │ Severity │\n",
			wantExec: false,
		},
		{
			name:     "empty output on exit 1 is treated as a finding, not silently skipped",
			out:      "",
			wantExec: false,
		},
		{
			name: "the word FATAL inside a package name must not trigger the exec branch",
			out: "Total: 1 (HIGH: 1)\n│ github.com/acme/fatalizer │ CVE-2026-1234 │ HIGH │\n" +
				"│ com.example.FATALParser   │ CVE-2026-9999 │ HIGH │\n",
			wantExec: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotExec, detail := classifyExitOne(tc.out)
			if gotExec != tc.wantExec {
				t.Fatalf("classifyExitOne(...) execError = %v, want %v\n--- output ---\n%s",
					gotExec, tc.wantExec, tc.out)
			}
			if tc.wantDetail != "" && !contains(detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q — an operator must be able to see WHY the scan could not run, not just that it did not",
					detail, tc.wantDetail)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
