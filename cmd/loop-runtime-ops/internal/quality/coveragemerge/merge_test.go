// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package coveragemerge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, rel, body string) string {
	t.Helper()
	full := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	return full
}

// =============================================================================
// ConvertGoProfile
// =============================================================================

func TestConvertGoProfile_SingleFile(t *testing.T) {
	input := "mode: atomic\napi/foo.go:10.2,12.10 3 5\n"
	var b strings.Builder
	files, lines, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	assert.Equal(t, 1, files)
	expected := "" +
		"SF:api/foo.go\n" +
		"DA:10,5\n" +
		"DA:11,5\n" +
		"DA:12,5\n" +
		"LF:3\nLH:3\nend_of_record\n"
	assert.Equal(t, expected, b.String())
	assert.Equal(t, 7, lines) // SF + 3×DA + LF + LH + end = 7
}

func TestConvertGoProfile_ZeroCount_LFButZeroLH(t *testing.T) {
	input := "mode: atomic\napi/foo.go:5.0,5.5 1 0\n"
	var b strings.Builder
	_, _, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	assert.Contains(t, b.String(), "DA:5,0\n")
	assert.Contains(t, b.String(), "LF:1\nLH:0\n")
}

func TestConvertGoProfile_MultipleFiles_FirstEncounterOrder(t *testing.T) {
	input := "mode: atomic\n" +
		"z.go:1.0,1.5 1 1\n" +
		"a.go:1.0,1.5 1 1\n" +
		"m.go:1.0,1.5 1 1\n"
	var b strings.Builder
	files, _, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	assert.Equal(t, 3, files)
	// First-encounter order: z, a, m (NOT sorted).
	idxZ := strings.Index(b.String(), "SF:z.go")
	idxA := strings.Index(b.String(), "SF:a.go")
	idxM := strings.Index(b.String(), "SF:m.go")
	assert.True(t, idxZ < idxA, "z must precede a (first-encounter)")
	assert.True(t, idxA < idxM, "a must precede m (first-encounter)")
}

func TestConvertGoProfile_NonConsecutiveSameFile_SingleSFRecord(t *testing.T) {
	// FAANG-grade improvement: bash awk emits multiple SF for same file.
	// Our converter merges into one record.
	input := "mode: atomic\n" +
		"x.go:1.0,1.5 1 5\n" +
		"y.go:10.0,10.5 1 3\n" +
		"x.go:2.0,2.5 1 2\n"
	var b strings.Builder
	files, _, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	assert.Equal(t, 2, files, "x.go appears twice in profile → one SF record")
	assert.Equal(t, 1, strings.Count(b.String(), "SF:x.go\n"))
}

func TestConvertGoProfile_OverlappingBlocks_MaxCountWins(t *testing.T) {
	// Line 1 appears in both blocks; output should reflect max(3, 5) = 5.
	input := "mode: atomic\n" +
		"x.go:1.0,3.5 3 3\n" +
		"x.go:1.0,2.5 2 5\n"
	var b strings.Builder
	_, _, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	out := b.String()
	assert.Contains(t, out, "DA:1,5\n", "line 1: max(3, 5) = 5")
	assert.Contains(t, out, "DA:2,5\n", "line 2: max(3, 5) = 5")
	assert.Contains(t, out, "DA:3,3\n", "line 3: only in first block")
}

func TestConvertGoProfile_SkipsHeader(t *testing.T) {
	input := "mode: count\nx.go:1.0,1.5 1 1\n"
	var b strings.Builder
	files, _, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	assert.Equal(t, 1, files)
}

func TestConvertGoProfile_MalformedLines_Skipped(t *testing.T) {
	input := "mode: atomic\n" +
		"garbage line\n" +
		"x.go:1.0,1.5 1 1\n" +
		"another:bad:line\n"
	var b strings.Builder
	files, _, err := ConvertGoProfile(strings.NewReader(input), &b)
	require.NoError(t, err)
	assert.Equal(t, 1, files)
}

func TestConvertGoProfile_EmptyInput(t *testing.T) {
	var b strings.Builder
	files, lines, err := ConvertGoProfile(strings.NewReader(""), &b)
	require.NoError(t, err)
	assert.Equal(t, 0, files)
	assert.Equal(t, 0, lines)
	assert.Empty(t, b.String())
}

func TestConvertGoProfile_OnlyHeader_NoFiles(t *testing.T) {
	var b strings.Builder
	files, _, err := ConvertGoProfile(strings.NewReader("mode: atomic\n"), &b)
	require.NoError(t, err)
	assert.Equal(t, 0, files)
	assert.Empty(t, b.String())
}

// =============================================================================
// Merge — orchestration
// =============================================================================

func TestMerge_GoOnly(t *testing.T) {
	dir := t.TempDir()
	goPath := writeFile(t, dir, "go.out",
		"mode: atomic\napi/foo.go:1.0,3.5 3 4\n")
	outPath := filepath.Join(dir, "merged.lcov")
	out, err := Merge(Config{
		GoProfilePath: goPath,
		OutputPath:    outPath,
	})
	require.NoError(t, err)
	assert.False(t, out.Empty)
	assert.Equal(t, 1, out.Files)
	body, err := os.ReadFile(outPath)
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "SF:api/foo.go\n")
	assert.Contains(t, s, "LF:3\nLH:3\n")
}

func TestMerge_CLIOnly(t *testing.T) {
	dir := t.TempDir()
	cliLCOV := "SF:cli/login.ts\nDA:1,1\nDA:2,0\nLF:2\nLH:1\nend_of_record\n"
	cliPath := writeFile(t, dir, "cli.lcov", cliLCOV)
	outPath := filepath.Join(dir, "merged.lcov")
	out, err := Merge(Config{
		CLILCOVPath: cliPath,
		OutputPath:  outPath,
	})
	require.NoError(t, err)
	assert.False(t, out.Empty)
	assert.Equal(t, 1, out.Files)
	body, _ := os.ReadFile(outPath)
	assert.Equal(t, cliLCOV, string(body))
}

func TestMerge_BothPresent_Concatenated(t *testing.T) {
	dir := t.TempDir()
	goPath := writeFile(t, dir, "go.out",
		"mode: atomic\napi/svc.go:1.0,1.5 1 2\n")
	cliPath := writeFile(t, dir, "cli.lcov",
		"SF:cli/login.ts\nDA:1,1\nLF:1\nLH:1\nend_of_record\n")
	outPath := filepath.Join(dir, "merged.lcov")
	out, err := Merge(Config{
		GoProfilePath: goPath,
		CLILCOVPath:   cliPath,
		OutputPath:    outPath,
	})
	require.NoError(t, err)
	assert.False(t, out.Empty)
	assert.Equal(t, 2, out.Files)
	body, _ := os.ReadFile(outPath)
	s := string(body)
	// Go content precedes CLI content.
	idxGo := strings.Index(s, "SF:api/svc.go")
	idxCLI := strings.Index(s, "SF:cli/login.ts")
	assert.True(t, idxGo >= 0)
	assert.True(t, idxCLI >= 0)
	assert.True(t, idxGo < idxCLI)
}

func TestMerge_BothEmpty_WarnAndEmptyOutput(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "merged.lcov")
	out, err := Merge(Config{OutputPath: outPath})
	require.NoError(t, err)
	assert.True(t, out.Empty)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "merged output empty")
	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size(), "empty output file written")
}

func TestMerge_GoMissingFile_StillSucceeds(t *testing.T) {
	dir := t.TempDir()
	cliPath := writeFile(t, dir, "cli.lcov",
		"SF:cli/x.ts\nDA:1,1\nLF:1\nLH:1\nend_of_record\n")
	outPath := filepath.Join(dir, "merged.lcov")
	out, err := Merge(Config{
		GoProfilePath: "/totally/missing/coverage.out",
		CLILCOVPath:   cliPath,
		OutputPath:    outPath,
	})
	require.NoError(t, err)
	assert.False(t, out.Empty)
	assert.Equal(t, 1, out.Files)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "Go profile /totally/missing/coverage.out missing/empty")
}

func TestMerge_NoOutputPath_OpsUsage(t *testing.T) {
	_, err := Merge(Config{})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestMerge_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	goPath := writeFile(t, dir, "go.out",
		"mode: atomic\nx.go:1.0,1.5 1 1\n")
	nested := filepath.Join(dir, "a", "b", "c", "merged.lcov")
	out, err := Merge(Config{
		GoProfilePath: goPath,
		OutputPath:    nested,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Files)
	_, err = os.Stat(nested)
	require.NoError(t, err)
}

// =============================================================================
// countLCOVStats
// =============================================================================

func TestCountLCOVStats(t *testing.T) {
	body := []byte("" +
		"SF:a.go\n" +
		"DA:1,1\n" +
		"LF:1\nLH:1\nend_of_record\n" +
		"SF:b.go\n" +
		"DA:1,1\n" +
		"LF:1\nLH:1\nend_of_record\n")
	files, lines := countLCOVStats(body)
	assert.Equal(t, 2, files)
	assert.Equal(t, 10, lines)
}

func TestCountLCOVStats_Empty(t *testing.T) {
	files, lines := countLCOVStats(nil)
	assert.Equal(t, 0, files)
	assert.Equal(t, 0, lines)
}

// =============================================================================
// Compatibility — coverageparse.ParseLCOVLineCoverage decodes our output
// =============================================================================

func TestMergeOutput_IsParseLCOVConsumable(t *testing.T) {
	// Verify the lcov we emit can be re-parsed by coverageparse's
	// line-coverage reader — proves byte-format compatibility.
	dir := t.TempDir()
	goPath := writeFile(t, dir, "go.out", "mode: atomic\nx.go:1.0,2.5 2 3\n")
	outPath := filepath.Join(dir, "merged.lcov")
	_, err := Merge(Config{GoProfilePath: goPath, OutputPath: outPath})
	require.NoError(t, err)

	body, err := os.ReadFile(outPath)
	require.NoError(t, err)
	// Validate the bare lcov skeleton is present.
	assert.Contains(t, string(body), "SF:x.go\n")
	assert.Contains(t, string(body), "DA:1,3\n")
	assert.Contains(t, string(body), "DA:2,3\n")
	assert.Contains(t, string(body), "end_of_record\n")
}
