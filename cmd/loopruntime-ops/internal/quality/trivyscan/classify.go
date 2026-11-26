// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trivyscan

import "strings"

// classifyExitOne decides what `trivy fs --exit-code 1` actually meant.
//
// trivy spends exit code 1 on two unrelated outcomes: matching-severity
// vulnerabilities were found, and the scan could not complete. Treating every
// exit 1 as a finding reports a security failure for what is often a network
// problem — and, worse, hides the fact that no scan happened at all.
//
// The discriminator is trivy's own log level. A completed scan that found
// something prints a findings table; a scan that died prints a FATAL line.
// Returns (true, detail) when the output carries a FATAL, where detail is the
// FATAL line itself so the operator sees the cause rather than a verdict.
//
// Matching is anchored on the tab/space-delimited log field to avoid firing on
// a package or CVE description that merely contains the word — a false
// execution-error would relabel a real finding as a failed scan, hiding the
// findings table behind a network story — the one mistake this function must
// never make.
func classifyExitOne(out string) (execError bool, detail string) {
	for _, line := range strings.Split(out, "\n") {
		for _, field := range strings.FieldsFunc(line, func(r rune) bool {
			return r == '\t'
		}) {
			if strings.TrimSpace(field) == "FATAL" {
				return true, strings.TrimSpace(line)
			}
		}
		// trivy also emits `FATAL` space-delimited depending on the terminal;
		// require it to start a field so "fatalizer" / "FATALParser" cannot match.
		for _, field := range strings.Fields(line) {
			if field == "FATAL" {
				return true, strings.TrimSpace(line)
			}
		}
	}
	return false, ""
}
