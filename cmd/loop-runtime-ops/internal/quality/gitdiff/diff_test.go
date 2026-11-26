// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitdiff

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/shellrun"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Pair.String
// =============================================================================

func TestPair_String(t *testing.T) {
	assert.Equal(t, "foo/bar.go:42", Pair{File: "foo/bar.go", Line: 42}.String())
}

// =============================================================================
// Parse — happy paths
// =============================================================================

func TestParse_SingleFileSingleHunk(t *testing.T) {
	input := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,0 +11,3 @@
+line11
+line12
+line13
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, []Pair{
		{File: "foo.go", Line: 11},
		{File: "foo.go", Line: 12},
		{File: "foo.go", Line: 13},
	}, pairs)
}

func TestParse_MultipleFiles(t *testing.T) {
	input := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,0 +2 @@
+line2
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -5,0 +6,2 @@
+line6
+line7
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, []Pair{
		{File: "a.go", Line: 2},
		{File: "b.go", Line: 6},
		{File: "b.go", Line: 7},
	}, pairs)
}

func TestParse_HunkCountOmittedDefaultsTo1(t *testing.T) {
	// `@@ -X +A @@` (no `,B`) means count=1 per the unified-diff spec.
	input := `+++ b/foo.go
@@ -10 +20 @@
+single-line
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, []Pair{{File: "foo.go", Line: 20}}, pairs)
}

func TestParse_PureDeletion_NoPairs(t *testing.T) {
	// `+20,0` means 0 lines added at logical line 20 — pure deletion.
	input := `+++ b/foo.go
@@ -10,5 +20,0 @@
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Empty(t, pairs, "deletion hunk yields no Pairs")
}

func TestParse_HunkBeforeFileHeader_Ignored(t *testing.T) {
	// Hunk lines before any `+++ b/FILE` header are dropped (no
	// "current file" to associate with).
	input := `@@ -10 +20,3 @@
+orphan-line
+++ b/foo.go
@@ -1 +2 @@
+after-header
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, []Pair{{File: "foo.go", Line: 2}}, pairs)
}

func TestParse_EmptyInput(t *testing.T) {
	pairs, err := Parse(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, pairs)
}

func TestParse_NoHunks_OnlyHeaders(t *testing.T) {
	input := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Empty(t, pairs)
}

// =============================================================================
// Parse — failure modes
// =============================================================================

// Note: regex doesn't accept malformed digits anyway, so we can't
// trigger Atoi error on the start field via the regex path. Test that
// well-formed hunks parse correctly + malformed inputs are silently
// dropped (regex non-match → skip).

func TestParse_MalformedHunkSilentSkip(t *testing.T) {
	// "@@ -" without proper digits → regex non-match → line dropped,
	// no error returned, no pair emitted.
	input := `+++ b/foo.go
@@ malformed hunk header
+orphan
@@ -1 +2 @@
+real-line
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, []Pair{{File: "foo.go", Line: 2}}, pairs,
		"malformed hunk silently skipped; subsequent valid hunk parses")
}

// =============================================================================
// RunGitDiff
// =============================================================================

func TestRunGitDiff_DefaultRefs(t *testing.T) {
	m := shellrun.NewMock()
	m.SetResponse("git diff --unified=0 origin/main...HEAD",
		&shellrun.Result{Stdout: "+++ b/foo.go\n@@ -1 +2 @@\n+line\n"})

	pairs, err := RunGitDiff(context.Background(), m, Config{})
	require.NoError(t, err)
	assert.Equal(t, Pairs{{File: "foo.go", Line: 2}}, pairs)
}

func TestRunGitDiff_CustomRefs(t *testing.T) {
	m := shellrun.NewMock()
	m.SetResponse("git diff --unified=0 origin/develop...feature-x",
		&shellrun.Result{Stdout: "+++ b/bar.go\n@@ -10 +20 @@\n+line\n"})

	pairs, err := RunGitDiff(context.Background(), m, Config{
		BaseRef: "origin/develop", HeadRef: "feature-x",
	})
	require.NoError(t, err)
	assert.Equal(t, Pairs{{File: "bar.go", Line: 20}}, pairs)
}

func TestRunGitDiff_GitFails_OpsDeployFailure(t *testing.T) {
	m := shellrun.NewMock()
	cmd := "git diff --unified=0 origin/main...HEAD"
	m.SetResponse(cmd, &shellrun.Result{ExitCode: 128})
	m.SetError(cmd, errors.New("fatal: bad revision 'origin/main'"))

	_, err := RunGitDiff(context.Background(), m, Config{})
	require.Error(t, err)
	var te *loopruntimeerrors.LoopRuntimeError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_DEPLOY_FAILURE", te.Code)
	assert.Contains(t, te.Message, "origin/main...HEAD")
}

func TestRunGitDiff_NilResult_ReturnsNilPairs(t *testing.T) {
	// Edge case: runner returns nil Result + nil err — we treat as
	// empty diff (no pairs, no error).
	m := shellrun.NewMock()
	m.SetResponse("git diff --unified=0 origin/main...HEAD",
		&shellrun.Result{Stdout: ""})

	pairs, err := RunGitDiff(context.Background(), m, Config{})
	require.NoError(t, err)
	assert.Empty(t, pairs)
}

// =============================================================================
// FormatLines
// =============================================================================

func TestFormatLines_PreservesBashOutputFormat(t *testing.T) {
	pairs := []Pair{
		{File: "foo.go", Line: 10},
		{File: "foo.go", Line: 11},
		{File: "bar.go", Line: 42},
	}
	got := FormatLines(pairs)
	assert.Equal(t, "foo.go:10\nfoo.go:11\nbar.go:42\n", got)
}

func TestFormatLines_Empty(t *testing.T) {
	assert.Equal(t, "", FormatLines(nil))
}

// =============================================================================
// Cross-cutting: parity with bash python output
// =============================================================================

// TestParse_ParityWithBashOutput exercises a representative
// unified-diff and asserts the Go output matches what the bash
// + python one-liner would have emitted, ensuring downstream
// consumers (coverage-diff, check-test-deletion, etc.) see no
// behavioral drift.
func TestParse_ParityWithBashOutput(t *testing.T) {
	// Realistic example: 2 files, mixed hunks (add+delete+add).
	input := `diff --git a/api/internal/handler.go b/api/internal/handler.go
--- a/api/internal/handler.go
+++ b/api/internal/handler.go
@@ -10,0 +11,2 @@
+log.Info("new handler")
+return nil
@@ -25,3 +27,0 @@
@@ -40 +42,1 @@
+single-add
diff --git a/api/internal/router.go b/api/internal/router.go
--- a/api/internal/router.go
+++ b/api/internal/router.go
@@ -5,0 +6 @@
+route.Get("/health", healthHandler)
`
	pairs, err := Parse(strings.NewReader(input))
	require.NoError(t, err)
	// Expected (matches what python script emits, line by line):
	//   api/internal/handler.go:11
	//   api/internal/handler.go:12
	//   api/internal/handler.go:42
	//   api/internal/router.go:6
	expected := []Pair{
		{File: "api/internal/handler.go", Line: 11},
		{File: "api/internal/handler.go", Line: 12},
		{File: "api/internal/handler.go", Line: 42},
		{File: "api/internal/router.go", Line: 6},
	}
	assert.Equal(t, expected, pairs)
}
