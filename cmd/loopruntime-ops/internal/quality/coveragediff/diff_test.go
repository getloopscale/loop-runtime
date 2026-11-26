// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package coveragediff

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile is a small helper for fixture creation.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// =============================================================================
// IsSourceFile
// =============================================================================

func TestIsSourceFile_Includes(t *testing.T) {
	for _, p := range []string{
		"api/internal/handler.go",
		"cli/src/cli.ts",
		"src/web/component.tsx",
	} {
		assert.True(t, IsSourceFile(p), "should be source: %s", p)
	}
}

func TestIsSourceFile_ExcludesTests(t *testing.T) {
	for _, p := range []string{
		"pkg/foo/bar_test.go",
		"cli/auth/login.test.ts",
		"cli/auth/login.spec.ts",
		"src/web/comp.test.tsx",
		"src/web/comp.spec.tsx",
	} {
		assert.False(t, IsSourceFile(p), "should NOT be source (test file): %s", p)
	}
}

func TestIsSourceFile_ExcludesNonSourceExtensions(t *testing.T) {
	for _, p := range []string{
		"docs/readme.md",
		"infra/config.yml",
		"package.json",
		"Dockerfile",
		"script.sh",
	} {
		assert.False(t, IsSourceFile(p), "should NOT be source: %s", p)
	}
}

// =============================================================================
// parseDiffLine
// =============================================================================

func TestParseDiffLine_HappyPath(t *testing.T) {
	path, ln, ok := parseDiffLine("api/handler.go:42")
	assert.True(t, ok)
	assert.Equal(t, "api/handler.go", path)
	assert.Equal(t, 42, ln)
}

func TestParseDiffLine_Malformed(t *testing.T) {
	for _, entry := range []string{
		"",
		"no-colon",
		":justline",       // no path
		"path-only:",      // no line
		"path:not-number", // bad line
	} {
		_, _, ok := parseDiffLine(entry)
		assert.False(t, ok, "should reject: %q", entry)
	}
}

// =============================================================================
// Run — EC-7 empty diff
// =============================================================================

func TestRun_EmptyDiff_Pass(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", "")
	out, err := Run(Config{DiffPath: diffPath})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, "empty-diff", out.SkipReason)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "empty diff — PASS")
}

// A missing diff file means the upstream gitdiff step did not run or
// failed. That is not EC-7 (a diff that exists and is empty): reporting
// PASS would grade a change nobody measured.
func TestRun_MissingDiffFile_FailsAsGateError(t *testing.T) {
	out, err := Run(Config{DiffPath: "/totally/missing/diff.txt"})
	require.Error(t, err)
	assert.Nil(t, out)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_DEPLOY_FAILURE", te.Code)
}

// An lcov path that exists but cannot be opened is not "no coverage data";
// it is a report the gate could not read.
func TestRun_UnreadableLCOV_FailsAsGateError(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", "pkg/foo/bar.go:10\n")
	dir := t.TempDir() // a directory: os.Open succeeds, reading fails
	_, err := Run(Config{DiffPath: diffPath, LCOVPath: dir})
	require.Error(t, err)
}

func TestRun_MissingDiffPath_OpsUsage(t *testing.T) {
	_, err := Run(Config{})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

// =============================================================================
// Run — no source lines (test-only / docs-only PR)
// =============================================================================

func TestRun_DiffHasOnlyTestFiles_PassSkip(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `pkg/foo/bar_test.go:10
pkg/foo/bar_test.go:11
cli/auth/login.test.ts:5
`)
	out, err := Run(Config{DiffPath: diffPath})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, "no-source", out.SkipReason)
	assert.Equal(t, 0, out.TotalSource)
}

func TestRun_DiffHasOnlyNonSource_PassSkip(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `docs/readme.md:1
infra/config.yml:5
`)
	out, err := Run(Config{DiffPath: diffPath})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, "no-source", out.SkipReason)
}

// =============================================================================
// Run — happy path with LCOV
// =============================================================================

func TestRun_AllLinesCovered_Pass(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `api/handler.go:10
api/handler.go:11
api/handler.go:12
`)
	lcovPath := writeFile(t, "merged.lcov", `SF:api/handler.go
DA:10,3
DA:11,5
DA:12,1
end_of_record
`)
	out, err := Run(Config{DiffPath: diffPath, LCOVPath: lcovPath})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, 3, out.TotalSource)
	assert.Equal(t, 3, out.CoveredSource)
	assert.Equal(t, 100.0, out.Ratio)
	assert.Empty(t, out.UncoveredLines)
}

func TestRun_PartialCoverageAboveThreshold_Pass(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `api/x.go:1
api/x.go:2
api/x.go:3
api/x.go:4
api/x.go:5
`)
	lcovPath := writeFile(t, "merged.lcov", `SF:api/x.go
DA:1,1
DA:2,1
DA:3,1
DA:4,1
DA:5,0
end_of_record
`)
	// 4/5 = 80% — exactly at threshold → PASS.
	out, err := Run(Config{DiffPath: diffPath, LCOVPath: lcovPath, Threshold: 80})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, 80.0, out.Ratio)
	assert.Len(t, out.UncoveredLines, 1)
	assert.Equal(t, "api/x.go:5", out.UncoveredLines[0])
}

// =============================================================================
// Run — FAIL below threshold
// =============================================================================

func TestRun_BelowThreshold_Fail(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `api/x.go:1
api/x.go:2
api/x.go:3
api/x.go:4
api/x.go:5
`)
	lcovPath := writeFile(t, "merged.lcov", `SF:api/x.go
DA:1,1
DA:2,0
DA:3,0
DA:4,0
DA:5,0
end_of_record
`)
	// 1/5 = 20% → FAIL.
	out, err := Run(Config{DiffPath: diffPath, LCOVPath: lcovPath, Threshold: 80})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.Equal(t, 20.0, out.Ratio)
	assert.Len(t, out.UncoveredLines, 4)

	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "FAIL (20.0% < 80%)")
	assert.Contains(t, joined, "uncovered lines:")
	assert.Contains(t, joined, "  api/x.go:2")
}

// =============================================================================
// Run — missing LCOV file
// =============================================================================

func TestRun_MissingLCOV_AllUncovered(t *testing.T) {
	// Missing LCOV → silent skip → every source line counted uncovered.
	diffPath := writeFile(t, "diff.txt", `api/x.go:1
api/x.go:2
`)
	out, err := Run(Config{
		DiffPath: diffPath,
		LCOVPath: "/totally/missing/lcov.info",
	})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.Equal(t, 0, out.CoveredSource)
	assert.Equal(t, 0.0, out.Ratio)
	assert.Len(t, out.UncoveredLines, 2)
}

// =============================================================================
// Run — Custom threshold
// =============================================================================

func TestRun_CustomThreshold50_LowerBar(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `api/x.go:1
api/x.go:2
`)
	lcovPath := writeFile(t, "lcov.info", `SF:api/x.go
DA:1,1
DA:2,0
end_of_record
`)
	out, err := Run(Config{DiffPath: diffPath, LCOVPath: lcovPath, Threshold: 50})
	require.NoError(t, err)
	assert.True(t, out.Pass, "50/2 = 50% → meets custom threshold 50")
	assert.Equal(t, 50.0, out.Ratio)
}

// =============================================================================
// Run — Report generation
// =============================================================================

func TestRun_ReportFile_GeneratedOnFail(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `api/x.go:1
api/x.go:2
`)
	lcovPath := writeFile(t, "lcov.info", `SF:api/x.go
DA:1,0
DA:2,0
end_of_record
`)
	reportPath := filepath.Join(t.TempDir(), "report.md")
	_, err := Run(Config{
		DiffPath:   diffPath,
		LCOVPath:   lcovPath,
		ReportPath: reportPath,
	})
	require.NoError(t, err)

	body, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	report := string(body)
	assert.Contains(t, report, "# Coverage Diff Report")
	assert.Contains(t, report, "**Touched source lines:** 2")
	assert.Contains(t, report, "**Covered:** 0")
	assert.Contains(t, report, "**Ratio:** 0.0%")
	assert.Contains(t, report, "**Threshold:** 80%")
	assert.Contains(t, report, "## Uncovered lines")
	assert.Contains(t, report, "- `api/x.go:1`")
	assert.Contains(t, report, "- `api/x.go:2`")
}

func TestRun_ReportFile_NoUncoveredSection_WhenAllCovered(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `api/x.go:1`)
	lcovPath := writeFile(t, "lcov.info", `SF:api/x.go
DA:1,1
end_of_record
`)
	reportPath := filepath.Join(t.TempDir(), "report.md")
	_, err := Run(Config{
		DiffPath:   diffPath,
		LCOVPath:   lcovPath,
		ReportPath: reportPath,
	})
	require.NoError(t, err)
	body, _ := os.ReadFile(reportPath)
	assert.NotContains(t, string(body), "## Uncovered lines")
}

// =============================================================================
// Run — malformed diff entries silently skipped
// =============================================================================

func TestRun_MalformedDiffEntries_Skipped(t *testing.T) {
	diffPath := writeFile(t, "diff.txt", `malformed-line
api/x.go:not-a-number
api/x.go:42
:no-path
empty-line-below

src/y.tsx:10
`)
	lcovPath := writeFile(t, "lcov.info", `SF:api/x.go
DA:42,1
end_of_record
SF:src/y.tsx
DA:10,1
end_of_record
`)
	out, err := Run(Config{DiffPath: diffPath, LCOVPath: lcovPath})
	require.NoError(t, err)
	// Only 2 well-formed source entries.
	assert.Equal(t, 2, out.TotalSource)
	assert.Equal(t, 2, out.CoveredSource)
}

// =============================================================================
// WriteMessages
// =============================================================================

func TestOutcome_WriteMessages(t *testing.T) {
	o := &Outcome{Messages: []string{"line1", "line2"}}
	var buf bytes.Buffer
	require.NoError(t, o.WriteMessages(&buf))
	assert.Equal(t, "line1\nline2\n", buf.String())
}
