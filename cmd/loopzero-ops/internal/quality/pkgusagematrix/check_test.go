// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package pkgusagematrix

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	return root
}

// =============================================================================
// listPkgDirs
// =============================================================================

func TestListPkgDirs_FiltersToolsAndSorts(t *testing.T) {
	dir := t.TempDir()
	// Each candidate needs its own .go to be an importable package — a bare
	// directory is not one (see hasOwnGoSource).
	for _, name := range []string{"zzz", "aaa", "tools"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, name, "doc.go"), []byte("package "+name+"\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(""), 0o600))

	got, err := listPkgDirs(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"aaa", "zzz"}, got)
}

func TestListPkgDirs_MissingDir(t *testing.T) {
	_, err := listPkgDirs("/tmp/zzz-no-pkg-usage")
	require.Error(t, err)
}

// =============================================================================
// fileContainsImport
// =============================================================================

func TestFileContainsImport_Match(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(p, []byte(`package x
import "github.com/getloopscale/loopzero/pkg/errors"
`), 0o600))
	assert.True(t, fileContainsImport(p, []byte(`"github.com/getloopscale/loopzero/pkg/errors"`)))
}

func TestFileContainsImport_NoMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(p, []byte("package x\n"), 0o600))
	assert.False(t, fileContainsImport(p, []byte(`"github.com/getloopscale/loopzero/pkg/errors"`)))
}

func TestFileContainsImport_MissingFile(t *testing.T) {
	assert.False(t, fileContainsImport("/tmp/zzz-no-file.go", []byte("x")))
}

// =============================================================================
// countConsumers — categorization
// =============================================================================

func TestCountConsumers_AllCategoriesCounted(t *testing.T) {
	root := setup(t, map[string]string{
		"pkg/errors/errors.go":              "package errors\n",
		"api/internal/foo.go":               `package foo
import "github.com/getloopscale/loopzero/pkg/errors"
`,
		"operators/internal/bar.go":         `package bar
import "github.com/getloopscale/loopzero/pkg/errors"
`,
		"cloud/main.go":                     `package main
import "github.com/getloopscale/loopzero/pkg/errors"
`,
		"cli/src/main.go":                   `package main
import "github.com/getloopscale/loopzero/pkg/errors"
`,
		"cmd/loopzero-ops/main.go":              `package main
import "github.com/getloopscale/loopzero/pkg/errors"
`,
	})
	row, err := countConsumers(root, "errors")
	require.NoError(t, err)
	assert.Equal(t, 5, row.Total)
	assert.Equal(t, 1, row.API)
	assert.Equal(t, 1, row.Operators)
	assert.Equal(t, 1, row.Cloud)
	assert.Equal(t, 1, row.CLI)
	assert.Equal(t, 1, row.Others)
}

func TestCountConsumers_SelfImportsExcluded(t *testing.T) {
	root := setup(t, map[string]string{
		"pkg/errors/errors.go":      "package errors\n",
		"pkg/errors/errors_test.go": `package errors
import _ "github.com/getloopscale/loopzero/pkg/errors"
`,
	})
	row, err := countConsumers(root, "errors")
	require.NoError(t, err)
	assert.Equal(t, 0, row.Total, "pkg/errors/ files MUST NOT count as consumers of pkg/errors")
}

func TestCountConsumers_NonGoFilesIgnored(t *testing.T) {
	root := setup(t, map[string]string{
		"pkg/errors/errors.go": "package errors\n",
		"docs/notes.md":        `mentions github.com/getloopscale/loopzero/pkg/errors as legacy`,
		"api/internal/x.txt":   `import "github.com/getloopscale/loopzero/pkg/errors"`,
	})
	row, err := countConsumers(root, "errors")
	require.NoError(t, err)
	assert.Equal(t, 0, row.Total)
}

func TestCountConsumers_VendorAndGitPruned(t *testing.T) {
	root := setup(t, map[string]string{
		"pkg/errors/errors.go":              "package errors\n",
		"vendor/example.com/x.go":           `import "github.com/getloopscale/loopzero/pkg/errors"`,
		".git/hooks/pre-commit.go":          `import "github.com/getloopscale/loopzero/pkg/errors"`,
		"node_modules/x/y.go":               `import "github.com/getloopscale/loopzero/pkg/errors"`,
	})
	row, err := countConsumers(root, "errors")
	require.NoError(t, err)
	assert.Equal(t, 0, row.Total, "vendor/, .git/, node_modules/ must be pruned")
}

// =============================================================================
// renderTable
// =============================================================================

func TestRenderTable_HeaderAndRows(t *testing.T) {
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	body := renderTable([]PkgRow{
		{Name: "errors", Total: 5, API: 2, Operators: 2, Cloud: 0, CLI: 0, Others: 1},
		{Name: "dead", Total: 0},
	}, now)
	assert.Contains(t, body, "## Real Usage Matrix")
	assert.Contains(t, body, "em **2026-05-21**")
	assert.Contains(t, body, "| `pkg/errors` | 5 | 2 | 2 | 0 | 0 | 1 |")
	assert.Contains(t, body, "dead code candidate")
}

// =============================================================================
// spliceDoc
// =============================================================================

func TestSpliceDoc_Roundtrip(t *testing.T) {
	original := "# Header\n\nbefore\n\n" + StartMark + "\n\nstale table\n\n" + EndMark + "\n\nafter\n"
	fresh := "FRESH TABLE BODY"
	out, err := spliceDoc(original, fresh)
	require.NoError(t, err)
	assert.Contains(t, out, "before\n")
	assert.Contains(t, out, "after\n")
	assert.Contains(t, out, "FRESH TABLE BODY")
	assert.NotContains(t, out, "stale table")
}

func TestSpliceDoc_MissingStartMarker(t *testing.T) {
	_, err := spliceDoc("no markers here\n"+EndMark+"\n", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), StartMark)
}

func TestSpliceDoc_MissingEndMarker(t *testing.T) {
	_, err := spliceDoc(StartMark+"\nbody\n", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), EndMark)
}

func TestSpliceDoc_InvertedOrder(t *testing.T) {
	_, err := spliceDoc(EndMark+"\nbefore start\n"+StartMark+"\n", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inverted")
}

// =============================================================================
// resolvePath
// =============================================================================

func TestResolvePath(t *testing.T) {
	assert.Equal(t, "/repo/docs/x.md", resolvePath("/repo", "docs/x.md"))
	assert.Equal(t, "/abs/path", resolvePath("/repo", "/abs/path"))
	assert.Equal(t, "docs/x.md", resolvePath("", "docs/x.md"))
}

// =============================================================================
// Run — end-to-end
// =============================================================================

func miniRepo(t *testing.T) string {
	t.Helper()
	return setup(t, map[string]string{
		"pkg/errors/errors.go":         "package errors\n",
		"pkg/naming/naming.go":         "package naming\n",
		"api/internal/svc.go":          `package svc
import "github.com/getloopscale/loopzero/pkg/errors"
`,
		"docs/technical/pkg/architecture.md": `# Pkg Architecture

before
` + StartMark + `

stale content

` + EndMark + `

after
`,
	})
}

func TestRun_CheckOnFreshlyWrittenIsOK(t *testing.T) {
	root := miniRepo(t)
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	// First write to populate.
	_, err := Run(Config{Root: root, Now: now}, ModeWrite)
	require.NoError(t, err)
	// Then check — must pass.
	out, err := Run(Config{Root: root, Now: now}, ModeCheck)
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "up-to-date")
}

func TestRun_CheckOnStaleFails(t *testing.T) {
	root := miniRepo(t)
	out, err := Run(Config{
		Root: root,
		Now:  time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
	}, ModeCheck)
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "out-of-date")
}

func TestRun_WriteSucceedsAndReportsPath(t *testing.T) {
	root := miniRepo(t)
	out, err := Run(Config{
		Root: root,
		Now:  time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
	}, ModeWrite)
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, ModeWrite, out.Mode)

	// Re-read the file and confirm marker bracket remains.
	got, err := os.ReadFile(filepath.Join(root, "docs/technical/pkg/architecture.md"))
	require.NoError(t, err)
	assert.Contains(t, string(got), StartMark)
	assert.Contains(t, string(got), EndMark)
	assert.Contains(t, string(got), "Real Usage Matrix")
	assert.NotContains(t, string(got), "stale content")
}

func TestRun_MissingDocReturnsOPS_USAGE(t *testing.T) {
	root := setup(t, map[string]string{
		"pkg/errors/errors.go": "package errors\n",
		// No architecture.md
	})
	_, err := Run(Config{Root: root}, ModeCheck)
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestRun_DocWithoutMarkersReturnsOPS_USAGE(t *testing.T) {
	root := setup(t, map[string]string{
		"pkg/errors/errors.go":               "package errors\n",
		"docs/technical/pkg/architecture.md": "# Doc with no markers\n",
	})
	_, err := Run(Config{Root: root}, ModeCheck)
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestRun_UnknownModeError(t *testing.T) {
	root := miniRepo(t)
	_, err := Run(Config{Root: root}, Mode(99))
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

// =============================================================================
// Constants
// =============================================================================

func TestConstants(t *testing.T) {
	assert.Equal(t, "docs/technical/pkg/architecture.md", DefaultDocPath)
	assert.Equal(t, "<!-- USAGE-MATRIX-START -->", StartMark)
	assert.Equal(t, "<!-- USAGE-MATRIX-END -->", EndMark)
}

// A directory that only holds sub-packages is not an importable Go package, so
// it must not appear in the matrix — and above all must not be labelled a dead
// code candidate.
//
// Measured 2026-08-06: pkg/pulumi/ contains no .go of its own, only
// pkg/pulumi/archive/, which cmd/loopzero-ops/pulumi_subcmd.go imports. The
// generator counted imports of the exact path "pkg/pulumi", found none, and
// published "0 ⚠️ dead code candidate" for live code. A generated document that
// invites someone to delete a package in use is worse than no document.
func TestListPkgDirsSkipsSubPackageOnlyDirs(t *testing.T) {
	root := t.TempDir()
	pkgRoot := filepath.Join(root, "pkg")

	// container/ holds only a sub-package; real/ has its own source.
	require.NoError(t, os.MkdirAll(filepath.Join(pkgRoot, "container", "archive"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgRoot, "container", "archive", "a.go"),
		[]byte("package archive\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(pkgRoot, "real"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgRoot, "real", "r.go"),
		[]byte("package real\n"), 0o644))

	dirs, err := listPkgDirs(pkgRoot)
	require.NoError(t, err)
	assert.Contains(t, dirs, "real")
	assert.NotContains(t, dirs, "container",
		"a directory with no .go of its own is not an importable package")
}

// A directory whose only .go files are tests is likewise not a package other
// code can import.
func TestListPkgDirsSkipsTestOnlyDirs(t *testing.T) {
	root := t.TempDir()
	pkgRoot := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(filepath.Join(pkgRoot, "testsonly"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgRoot, "testsonly", "x_test.go"),
		[]byte("package testsonly\n"), 0o644))

	dirs, err := listPkgDirs(pkgRoot)
	require.NoError(t, err)
	assert.NotContains(t, dirs, "testsonly")
}
