// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"regexp"
	"testing"
)

// sourceClause is one statement a script's source must (or must not) contain,
// with the reason it matters written where a failure prints it.
type sourceClause struct {
	pattern *regexp.Regexp
	why     string
}

// requireClauses fails once per clause the source does not contain.
//
// These are source contracts, not behaviour: they lock the SHAPE of a defensive
// path that only runs on a cold cluster, where an end-to-end run needs a k3d
// fleet. Each clause names the failure its absence would cause.
func requireClauses(t *testing.T, path string, clauses ...sourceClause) {
	t.Helper()
	src := readSource(t, path)
	for _, c := range clauses {
		if !c.pattern.MatchString(src) {
			t.Errorf("%s: no match for %q — %s", path, c.pattern, c.why)
		}
	}
}

func clause(pattern, why string) sourceClause {
	return sourceClause{pattern: regexp.MustCompile(pattern), why: why}
}
