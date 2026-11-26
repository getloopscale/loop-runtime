// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package regressionmatrix

import "testing"

// fabricatedMatrix mirrors the synthetic matrix from audit-golden-test.sh. Its
// Portuguese headers and cell values ("Pode Recorrer?" = "can it recur?",
// CanRecurNo = "no", "SIM" = "yes", "Exercitado por L3?" = "exercised by L3?") are
// the real matrix format the parser reads, so they stay verbatim. The "no" cell is
// spliced in from CanRecurNo so the fixture and the gate share one spelling:
//
//	row 1: token resolves
//	row 2: token does NOT resolve, can-recur = CanRecurNo -> critical MISS
//	row 3: live verify -> SKIP
const fabricatedMatrix = `# Broken Matrix (golden test — synthetic)

| #  | Category | Problem | Resolved By | Protection | Type | Verification | Pode Recorrer? | Exercitado por L3? |
|----|----------|---------|-------------|------------|------|--------------|----------------|--------------------|
| 1  | TEST     | real    | something   | something  | (a)  | TestImageTagFormat exists | ` + CanRecurNo + ` | SIM |
| 2  | TEST     | broken  | something   | something  | (a)  | ghostdeadbeefcafetoken | ` + CanRecurNo + ` | SIM |
| 3  | TEST     | live    | something   | something  | (a)  | live verify by operator | ` + CanRecurNo + ` | SIM |
`

func TestParseMatrix_ExtractsDataRows(t *testing.T) {
	rows := ParseMatrix(fabricatedMatrix)
	if len(rows) != 3 {
		t.Fatalf("expected 3 data rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].ID != "1" || rows[0].Verification != "TestImageTagFormat exists" || rows[0].CanRecur != CanRecurNo {
		t.Errorf("row 0 parsed wrong: %+v", rows[0])
	}
	if rows[1].Verification != "ghostdeadbeefcafetoken" {
		t.Errorf("row 1 verification wrong: %q", rows[1].Verification)
	}
	if rows[2].Verification != "live verify by operator" {
		t.Errorf("row 2 verification wrong: %q", rows[2].Verification)
	}
}

func TestParseMatrix_IgnoresHeaderAndSeparator(t *testing.T) {
	// The header row starts with "| #" and the separator with "|---" — neither
	// has a numeric first cell, so both must be skipped.
	rows := ParseMatrix(fabricatedMatrix)
	for _, r := range rows {
		if !isNumeric(r.ID) {
			t.Errorf("non-numeric ID leaked into rows: %q", r.ID)
		}
	}
}

func TestExtractTokens_FiltersNoiseAndShortWords(t *testing.T) {
	tokens := ExtractTokens("helm template + regression-checks.sh test_b16 with grep")
	got := map[string]bool{}
	for _, tk := range tokens {
		got[tk] = true
	}
	// "regression-checks.sh" matches the filename alternative; "test_b16" and
	// "template" match the identifier alternative.
	if !got["regression-checks.sh"] {
		t.Errorf("expected filename token regression-checks.sh, got %v", tokens)
	}
	if !got["test_b16"] {
		t.Errorf("expected identifier token test_b16, got %v", tokens)
	}
	// "with" is < 5 chars (never extracted); "grep" is noise-filtered.
	if got["with"] || got["grep"] {
		t.Errorf("noise/short token leaked: %v", tokens)
	}
}

func TestResolveArtifact_Empty_IsMiss(t *testing.T) {
	if got := ResolveArtifact("", SetResolver{}); got != Miss {
		t.Errorf("empty verification: want Miss, got %v", got)
	}
}

func TestResolveArtifact_LiveRuntimeVerify_IsSkip(t *testing.T) {
	cases := []string{"live verify by operator", "runtime check", "verify manually"}
	for _, c := range cases {
		if got := ResolveArtifact(c, SetResolver{}); got != Skip {
			t.Errorf("%q: want Skip, got %v", c, got)
		}
	}
}

func TestResolveArtifact_NoUsableTokens_IsSkip(t *testing.T) {
	// "v1.0" has no >=5-char identifier and no filename form.
	if got := ResolveArtifact("v1.0", SetResolver{}); got != Skip {
		t.Errorf("no-token verification: want Skip, got %v", got)
	}
}

func TestResolveArtifact_TokenResolves_IsResolved(t *testing.T) {
	r := SetResolver{"TestImageTagFormat": true}
	if got := ResolveArtifact("TestImageTagFormat exists", r); got != Resolved {
		t.Errorf("want Resolved, got %v", got)
	}
}

func TestResolveArtifact_TokenAbsent_IsMiss(t *testing.T) {
	if got := ResolveArtifact("ghostdeadbeefcafetoken", SetResolver{}); got != Miss {
		t.Errorf("want Miss, got %v", got)
	}
}

// TestGolden_FabricatedBrokenMatrixIsDetected is the faithful port of
// audit-golden-test.sh: the audit MUST detect the broken row #2 as a critical
// miss and the gate MUST fail. This is the guarantee that protected against the
// audit silently regressing — now expressed as a Go test instead of a bash
// script that re-invoked the (set-e-broken) audit.
func TestGolden_FabricatedBrokenMatrixIsDetected(t *testing.T) {
	rows := ParseMatrix(fabricatedMatrix)
	// Only TestImageTagFormat resolves (row 1). Row 2 token is absent; row 3 is
	// a live-verify Skip.
	resolver := SetResolver{"TestImageTagFormat": true}
	v := Evaluate(rows, resolver, false)

	if v.Total != 3 {
		t.Errorf("expected total=3, got %d", v.Total)
	}
	if v.OK != 1 {
		t.Errorf("expected OK=1, got %d", v.OK)
	}
	if v.Skip != 1 {
		t.Errorf("expected Skip=1, got %d", v.Skip)
	}
	if v.Miss != 1 {
		t.Errorf("expected Miss=1, got %d", v.Miss)
	}
	if v.MissCritical != 1 {
		t.Errorf("expected MissCritical=1 (row 2 has can-recur=CanRecurNo), got %d", v.MissCritical)
	}
	if !v.Failed() {
		t.Error("expected gate to FAIL on critical miss")
	}
	if len(v.MissEntries) != 1 || v.MissEntries[0].ID != "2" {
		t.Errorf("expected the miss entry to be row #2, got %+v", v.MissEntries)
	}
}

func TestGolden_AllResolved_Passes(t *testing.T) {
	// A healthy matrix: row 1 resolves, row 3 skips, row 2 ALSO resolves now.
	rows := ParseMatrix(fabricatedMatrix)
	resolver := SetResolver{"TestImageTagFormat": true, "ghostdeadbeefcafetoken": true}
	v := Evaluate(rows, resolver, false)
	if v.Failed() {
		t.Errorf("healthy matrix should pass, got %+v", v)
	}
	if v.MissCritical != 0 {
		t.Errorf("expected 0 critical miss, got %d", v.MissCritical)
	}
}

func TestEvaluate_Strict_FailsOnNonCriticalMiss(t *testing.T) {
	matrix := `| #  | Cat | P | R | Prot | T | Verification | Pode Recorrer? | L3 |
| 1  | X   | y | z | w    | a | ghostabsenttoken | SIM | SIM |
`
	rows := ParseMatrix(matrix)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// CanRecur=SIM ("yes") so not critical. Non-strict passes; strict fails.
	if Evaluate(rows, SetResolver{}, false).Failed() {
		t.Error("non-strict should not fail on non-critical miss")
	}
	if !Evaluate(rows, SetResolver{}, true).Failed() {
		t.Error("strict should fail on any miss")
	}
}

func TestHasTokenBoundary(t *testing.T) {
	cases := []struct {
		text, token string
		want        bool
	}{
		{"func TestImageTagFormat(t *testing.T) {", "TestImageTagFormat", true},
		{"prefixTestImageTagFormat", "TestImageTagFormat", false}, // left alpha boundary fails
		{"TestImageTagFormatSuffix", "TestImageTagFormat", false}, // right alpha boundary fails
		{"test_b16 here", "test_b16", true},
		{"test_b160 longer", "test_b16", true}, // '0' is non-alpha boundary (matches bash [^A-Za-z])
		{"nothing matches", "absent", false},
	}
	for _, c := range cases {
		if got := hasTokenBoundary(c.text, c.token); got != c.want {
			t.Errorf("hasTokenBoundary(%q,%q)=%v want %v", c.text, c.token, got, c.want)
		}
	}
}
