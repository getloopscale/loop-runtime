// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package coverageparse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// pkgFromFile
// =============================================================================

func TestPkgFromFile_StripsFileBaseName(t *testing.T) {
	assert.Equal(t, "github.com/x/y", pkgFromFile("github.com/x/y/foo.go"))
	assert.Equal(t, "src/cli/auth", pkgFromFile("src/cli/auth/login.ts"))
}

func TestPkgFromFile_BareFile_Empty(t *testing.T) {
	// No slash → no package association.
	assert.Equal(t, "", pkgFromFile("bare.go"))
}

// =============================================================================
// ParseGoProfile
// =============================================================================

func TestParseGoProfile_SinglePackage(t *testing.T) {
	input := `mode: atomic
github.com/getloopscale/loop-runtime/pkg/foo/bar.go:10.0,15.10 5 3
github.com/getloopscale/loop-runtime/pkg/foo/baz.go:20.0,30.10 10 10
github.com/getloopscale/loop-runtime/pkg/foo/baz.go:32.0,40.10 8 0
`
	pkgs, err := ParseGoProfile(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	// File bar.go: 5 stmts, 5 covered (count>0 → all 5 covered)
	// File baz.go: 18 stmts, 10 covered
	// Package total: 23 stmts, 15 covered = 65.2%
	assert.Equal(t, "github.com/getloopscale/loop-runtime/pkg/foo", pkgs[0].Package)
	assert.Equal(t, 65.2, pkgs[0].Coverage)
}

func TestParseGoProfile_MultiPackageSorted(t *testing.T) {
	input := `mode: atomic
pkg/z/a.go:1.0,2.0 10 10
pkg/a/b.go:1.0,2.0 10 10
pkg/m/c.go:1.0,2.0 10 10
`
	pkgs, err := ParseGoProfile(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 3)
	assert.Equal(t, "pkg/a", pkgs[0].Package)
	assert.Equal(t, "pkg/m", pkgs[1].Package)
	assert.Equal(t, "pkg/z", pkgs[2].Package)
}

func TestParseGoProfile_AllUncovered_ZeroPct(t *testing.T) {
	input := `mode: atomic
pkg/x/y.go:1.0,2.0 10 0
pkg/x/y.go:3.0,4.0 5 0
`
	pkgs, err := ParseGoProfile(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, 0.0, pkgs[0].Coverage)
}

func TestParseGoProfile_AllCovered_100Pct(t *testing.T) {
	input := `mode: atomic
pkg/x/y.go:1.0,2.0 10 5
pkg/x/y.go:3.0,4.0 5 3
`
	pkgs, err := ParseGoProfile(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, 100.0, pkgs[0].Coverage)
}

func TestParseGoProfile_MalformedLinesSilentlySkipped(t *testing.T) {
	input := `mode: atomic
malformed line with no colons
pkg/x/y.go:1.0,2.0 abc xyz
pkg/x/y.go:1.0,2.0 10 5
pkg/x/y.go:not-a-number 5 5
`
	pkgs, err := ParseGoProfile(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, 100.0, pkgs[0].Coverage, "only the one well-formed line counted")
}

func TestParseGoProfile_BareFilename_Excluded(t *testing.T) {
	// File without dirname → no package association → excluded.
	input := `mode: atomic
top-level.go:1.0,2.0 10 5
pkg/x/y.go:1.0,2.0 10 5
`
	pkgs, err := ParseGoProfile(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "pkg/x", pkgs[0].Package)
}

func TestParseGoProfile_EmptyInput(t *testing.T) {
	pkgs, err := ParseGoProfile(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

func TestParseGoProfile_HeaderOnly(t *testing.T) {
	pkgs, err := ParseGoProfile(strings.NewReader("mode: atomic\n"))
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

// =============================================================================
// ParseLCOV
// =============================================================================

func TestParseLCOV_SinglePackage(t *testing.T) {
	input := `SF:src/cli/auth/login.ts
DA:1,1
LF:100
LH:80
end_of_record
SF:src/cli/auth/logout.ts
LF:50
LH:50
end_of_record
`
	pkgs, err := ParseLCOV(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "src/cli/auth", pkgs[0].Package)
	// File 1: 80/100 = 80%; File 2: 50/50 = 100% → mean = 90%
	assert.Equal(t, 90.0, pkgs[0].Coverage)
}

func TestParseLCOV_MultiPackageSorted(t *testing.T) {
	input := `SF:src/z/a.ts
LF:10
LH:10
end_of_record
SF:src/a/b.ts
LF:10
LH:10
end_of_record
SF:src/m/c.ts
LF:10
LH:10
end_of_record
`
	pkgs, err := ParseLCOV(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 3)
	assert.Equal(t, "src/a", pkgs[0].Package)
	assert.Equal(t, "src/m", pkgs[1].Package)
	assert.Equal(t, "src/z", pkgs[2].Package)
}

func TestParseLCOV_LFZero_ZeroPct(t *testing.T) {
	// LF=0 (no lines) → 0% (avoid div by zero).
	input := `SF:src/empty/x.ts
LF:0
LH:0
end_of_record
`
	pkgs, err := ParseLCOV(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, 0.0, pkgs[0].Coverage)
}

func TestParseLCOV_BareFile_ExcludedFromAvg(t *testing.T) {
	// File without dirname → no package → not in output.
	input := `SF:top.ts
LF:10
LH:10
end_of_record
SF:src/x/y.ts
LF:10
LH:5
end_of_record
`
	pkgs, err := ParseLCOV(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "src/x", pkgs[0].Package)
	assert.Equal(t, 50.0, pkgs[0].Coverage)
}

func TestParseLCOV_EmptyInput(t *testing.T) {
	pkgs, err := ParseLCOV(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

func TestParseLCOV_EndOfRecordWithoutSF_Skipped(t *testing.T) {
	// `end_of_record` without preceding SF: → no package emitted.
	input := `end_of_record
SF:src/x/y.ts
LF:10
LH:7
end_of_record
`
	pkgs, err := ParseLCOV(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, 70.0, pkgs[0].Coverage)
}

// =============================================================================
// ParseLCOVLineCoverage + LineCoverage.IsCovered
// =============================================================================

func TestParseLCOVLineCoverage_BasicMapping(t *testing.T) {
	input := `SF:src/cli/auth/login.ts
DA:1,3
DA:2,0
DA:5,1
LF:3
LH:2
end_of_record
SF:src/cli/auth/logout.ts
DA:10,5
end_of_record
`
	cov, err := ParseLCOVLineCoverage(strings.NewReader(input))
	require.NoError(t, err)

	assert.True(t, cov.IsCovered("src/cli/auth/login.ts", 1))
	assert.False(t, cov.IsCovered("src/cli/auth/login.ts", 2), "hit=0 → uncovered")
	assert.True(t, cov.IsCovered("src/cli/auth/login.ts", 5))
	assert.True(t, cov.IsCovered("src/cli/auth/logout.ts", 10))

	// Lines not recorded → not covered.
	assert.False(t, cov.IsCovered("src/cli/auth/login.ts", 999))
	// Files not in coverage → not covered.
	assert.False(t, cov.IsCovered("totally/unknown.ts", 1))
}

func TestParseLCOVLineCoverage_RepeatedDARecord_TakesMax(t *testing.T) {
	// Some lcov emitters record multiple DA entries per line (e.g.,
	// when merging multiple test runs). Take the max (most permissive).
	input := `SF:src/x.ts
DA:1,0
DA:1,5
DA:1,2
end_of_record
`
	cov, err := ParseLCOVLineCoverage(strings.NewReader(input))
	require.NoError(t, err)
	assert.True(t, cov.IsCovered("src/x.ts", 1),
		"merged DA records take max hit count → covered when any record has count>0")
}

func TestParseLCOVLineCoverage_MalformedDARecord_Skipped(t *testing.T) {
	input := `SF:src/x.ts
DA:not-a-number,1
DA:5,not-a-number
DA:10,3
end_of_record
`
	cov, err := ParseLCOVLineCoverage(strings.NewReader(input))
	require.NoError(t, err)
	assert.True(t, cov.IsCovered("src/x.ts", 10), "valid record kept")
	assert.False(t, cov.IsCovered("src/x.ts", 5))
}

func TestParseLCOVLineCoverage_DABeforeSF_Ignored(t *testing.T) {
	input := `DA:1,5
SF:src/x.ts
DA:2,1
end_of_record
`
	cov, err := ParseLCOVLineCoverage(strings.NewReader(input))
	require.NoError(t, err)
	// First DA had no file context — dropped silently.
	assert.True(t, cov.IsCovered("src/x.ts", 2))
}

func TestParseLCOVLineCoverage_EmptyInput(t *testing.T) {
	cov, err := ParseLCOVLineCoverage(strings.NewReader(""))
	require.NoError(t, err)
	assert.False(t, cov.IsCovered("any", 1))
}

func TestLineCoverage_IsCovered_NilMap(t *testing.T) {
	// Defensive: nil map should not panic.
	var cov LineCoverage
	assert.False(t, cov.IsCovered("file", 1))
}

// =============================================================================
// RoundTo1Decimal
// =============================================================================

func TestRoundTo1Decimal(t *testing.T) {
	assert.Equal(t, 87.5, RoundTo1Decimal(87.5))
	assert.Equal(t, 87.5, RoundTo1Decimal(87.5499))
	assert.Equal(t, 87.6, RoundTo1Decimal(87.5501))
	assert.Equal(t, 0.0, RoundTo1Decimal(0.0))
	assert.Equal(t, 100.0, RoundTo1Decimal(100.0))
}
