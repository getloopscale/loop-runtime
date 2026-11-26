// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package pkgusagematrix generates and verifies the "Real Usage
// Matrix" of pkg/ shared libraries embedded between markers in
// docs/architecture.md. CI uses --check; humans run
// --write locally before commit.
//
// Per pkg-sota-faang-grade-plan v1.0 T1.7 + gate PKG-G14
// (Documentation Reflects Real Usage).
//
// Replaces infra/scripts/pkg-usage-matrix.sh (~148 LOC) per Phase 7
// of plan 2026-05-21-bash-to-go-migration-plan.
package pkgusagematrix

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// DefaultDocPath — bash default, relative to repo root.
const DefaultDocPath = "docs/architecture.md"

// Markers bracket the generated content.
const (
	StartMark = "<!-- USAGE-MATRIX-START -->"
	EndMark   = "<!-- USAGE-MATRIX-END -->"
)

// Mode controls whether the gate writes or only checks freshness.
type Mode int

const (
	// ModeCheck — emit diff and return non-pass when doc is out of date.
	ModeCheck Mode = iota
	// ModeWrite — splice fresh table into the doc.
	ModeWrite
)

// PkgRow is the per-package count tuple.
type PkgRow struct {
	Name      string
	Total     int
	API       int // api/internal
	Operators int // operators/internal
	Cloud     int // cloud/
	CLI       int // cli/
	Others    int
}

// Config holds gate inputs.
type Config struct {
	// Root is the repo root. Default: ".".
	Root string

	// DocPath overrides DefaultDocPath.
	DocPath string
}

// Outcome captures the gate result.
type Outcome struct {
	Pass      bool
	Mode      Mode
	Rows      []PkgRow
	Generated string // the freshly generated table body
	Messages  []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// listPkgDirs returns the sorted list of pkg/<X> subdirectory names that are
// actually importable Go packages (excluding "tools" — explicit bash skip +
// reserved future namespace).
//
// A directory holding only sub-packages is skipped. cmd/loop-runtime-ops/internal/pulumi/ is the case
// that exposed this: it has no .go of its own, only its archive/ sub-package, which
// cmd/loop-runtime-ops/pulumi_subcmd.go imports. Counting imports of the exact path
// the parent path found none, and the generated matrix published
// "0 ⚠️ dead code candidate" for live code (measured 2026-08-06). A generated
// document that invites deleting a package in use is worse than no document.
func listPkgDirs(pkgRoot string) ([]string, error) {
	entries, err := os.ReadDir(pkgRoot)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "tools" {
			continue
		}
		ownSource, err := hasOwnGoSource(filepath.Join(pkgRoot, name))
		if err != nil {
			return nil, err
		}
		if !ownSource {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// hasOwnGoSource reports whether dir contains a non-test .go file directly —
// i.e. whether `import ".../pkg/<dir>"` resolves to anything. Sub-directories
// are not consulted: those are separate packages with their own import paths.
func hasOwnGoSource(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			return true, nil
		}
	}
	return false, nil
}

// countConsumers walks all *.go files under root looking for the
// canonical import "github.com/getloopscale/loop-runtime/pkg/<pkg>". Files within
// pkg/<pkg>/ itself are excluded (mirrors bash `grep -v "^pkg/<pkg>/"`).
//
// Returns one row of counts.
func countConsumers(root, pkg string) (PkgRow, error) {
	needle := []byte(`"github.com/getloopscale/loop-runtime/pkg/` + pkg + `"`)
	row := PkgRow{Name: pkg}
	selfPrefix := "pkg/" + pkg + string(filepath.Separator)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "node_modules" || n == "vendor" || n == "dist" || n == "build" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, selfPrefix) || rel == filepath.ToSlash(selfPrefix[:len(selfPrefix)-1]) {
			return nil
		}
		imports, err := fileContainsImport(path, needle)
		if err != nil {
			return err
		}
		if !imports {
			return nil
		}
		row.Total++
		switch {
		case strings.HasPrefix(rel, "api/internal/"):
			row.API++
		case strings.HasPrefix(rel, "operators/internal/"):
			row.Operators++
		case strings.HasPrefix(rel, "cloud/"):
			row.Cloud++
		case strings.HasPrefix(rel, "cli/"):
			row.CLI++
		default:
			row.Others++
		}
		return nil
	})
	if err != nil {
		return row, err
	}
	return row, nil
}

// fileContainsImport scans path line-by-line for the literal needle and
// reports the first hit. A file that cannot be read is an error: counting it as
// a non-consumer would publish a package in use as a dead-code candidate.
func fileContainsImport(path string, needle []byte) (bool, error) {
	f, err := os.Open(path) //nolint:gosec // operator-controlled
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if bytes.Contains(scanner.Bytes(), needle) {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	return false, nil
}

// renderTable produces the bash-compatible markdown table body.
func renderTable(rows []PkgRow) string {
	var sb strings.Builder
	sb.WriteString("## Real Usage Matrix\n\n")
	// D2 / plan helm-topology-index T1.2: the DATE deliberately stays out of the
	// compared body.
	//
	// A freshness gate answers "has the content diverged from its source?". With the date
	// inside, it started answering "was this generated today?" instead — a question whose
	// answer is "no" every day after midnight. Measured 2026-08-08: it blocked a push over
	// a matrix that was identical, and the only way out was regenerating a file that had
	// not changed, which trains a team to regenerate without looking. So the date simply
	// goes: git already records when the file changed, and a stamp nobody compares was not
	// paying for itself.
	sb.WriteString("Table generated by grepping for `import \"github.com/getloopscale/loop-runtime/pkg/X\"`.\n")
	sb.WriteString("Regenerate with `task pkg:usage-matrix:write` (`loop-runtime-ops quality pkg-usage-matrix --write`).\n")
	sb.WriteString("The `task pkg:usage-matrix:check` gate fails when the table is out of date (gate PKG-G14).\n\n")
	sb.WriteString("| Package | Total imports | api/internal | operators/internal | cloud | cli | Other |\n")
	sb.WriteString("|---|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		marker := ""
		if r.Total == 0 {
			marker = " ⚠️ **dead code candidate**"
		}
		fmt.Fprintf(&sb,
			"| `pkg/%s` | %d%s | %d | %d | %d | %d | %d |\n",
			r.Name, r.Total, marker, r.API, r.Operators, r.Cloud, r.CLI, r.Others)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// spliceDoc replaces the marker-bracketed region of docContent with
// the fresh table. Returns the new contents, or error if markers are
// absent.
func spliceDoc(docContent, freshTable string) (string, error) {
	startIdx := strings.Index(docContent, StartMark)
	endIdx := strings.Index(docContent, EndMark)
	if startIdx < 0 {
		return "", fmt.Errorf("doc missing %s marker", StartMark)
	}
	if endIdx < 0 {
		return "", fmt.Errorf("doc missing %s marker", EndMark)
	}
	if endIdx < startIdx {
		return "", fmt.Errorf("marker order inverted (%s precedes %s)", EndMark, StartMark)
	}

	prefix := docContent[:startIdx]
	suffix := docContent[endIdx+len(EndMark):]

	var sb strings.Builder
	sb.WriteString(prefix)
	sb.WriteString(StartMark)
	sb.WriteString("\n\n")
	sb.WriteString(freshTable)
	sb.WriteString("\n\n")
	sb.WriteString(EndMark)
	sb.WriteString(suffix)
	return sb.String(), nil
}

// resolvePath joins p under root unless absolute.
func resolvePath(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, p)
}

// Run executes the gate in either ModeCheck or ModeWrite.
func Run(cfg Config, mode Mode) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	docPath := cfg.DocPath
	if docPath == "" {
		docPath = DefaultDocPath
	}
	resolvedDoc := resolvePath(root, docPath)
	pkgRoot := filepath.Join(root, "pkg")

	out := &Outcome{Mode: mode}

	pkgs, err := listPkgDirs(pkgRoot)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("pkg-usage-matrix: list pkg/: %v", err),
			Cause:   err,
		}
	}

	for _, p := range pkgs {
		row, err := countConsumers(root, p)
		if err != nil {
			return nil, &loopruntimeerrors.LoopRuntimeError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("pkg-usage-matrix: count %s: %v", p, err),
				Cause:   err,
			}
		}
		out.Rows = append(out.Rows, row)
	}
	out.Generated = renderTable(out.Rows)

	docRaw, err := os.ReadFile(resolvedDoc) //nolint:gosec // operator-controlled
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pkg-usage-matrix: read %s: %v", resolvedDoc, err),
			Cause:   err,
		}
	}
	newDoc, err := spliceDoc(string(docRaw), out.Generated)
	if err != nil {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pkg-usage-matrix: %v", err),
		}
	}

	switch mode {
	case ModeWrite:
		if err := os.WriteFile(resolvedDoc, []byte(newDoc), 0o600); err != nil {
			return nil, &loopruntimeerrors.LoopRuntimeError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("pkg-usage-matrix: write %s: %v", resolvedDoc, err),
				Cause:   err,
			}
		}
		out.Pass = true
		out.Messages = append(out.Messages,
			fmt.Sprintf("Wrote refreshed Real Usage Matrix to %s", resolvedDoc))
		return out, nil
	case ModeCheck:
		if newDoc == string(docRaw) {
			out.Pass = true
			out.Messages = append(out.Messages, "OK: pkg usage matrix is up-to-date.")
			return out, nil
		}
		out.Pass = false
		out.Messages = append(out.Messages,
			fmt.Sprintf("ERROR: %s out-of-date — run `loop-runtime-ops quality pkg-usage-matrix --write` and commit.", resolvedDoc))
		return out, nil
	}
	return out, &loopruntimeerrors.LoopRuntimeError{
		Code:    "OPS_USAGE",
		Message: fmt.Sprintf("pkg-usage-matrix: unknown mode %d", mode),
	}
}
