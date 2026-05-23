// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package pkgusagematrix generates and verifies the "Real Usage
// Matrix" of pkg/ shared libraries embedded between markers in
// docs/technical/pkg/architecture.md. CI uses --check; humans run
// --write locally before commit.
//
// Per pkg-sota-faang-grade-plan v1.0 T1.7 + gate PKG-G14
// (Documentation Reflects Real Usage).
//
// Replaces infra/scripts/pkg-usage-matrix.sh (~148 LOC) per Phase 7
// of plan docs/plans/2026-05-21-bash-to-go-migration-plan.md.
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
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// DefaultDocPath — bash default, relative to repo root.
const DefaultDocPath = "docs/technical/pkg/architecture.md"

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

	// Now is the timestamp injection point (Mode=Write writes a date
	// header). Zero → time.Now().UTC().
	Now time.Time
}

// Outcome captures the gate result.
type Outcome struct {
	Pass     bool
	Mode     Mode
	Rows     []PkgRow
	Generated string // the freshly generated table body
	Messages []string
}

// listPkgDirs returns the sorted list of pkg/<X> subdirectory names
// (excluding "tools" — explicit bash skip + reserved future namespace).
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
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// countConsumers walks all *.go files under root looking for the
// canonical import "github.com/loopzero/loopzero/pkg/<pkg>". Files within
// pkg/<pkg>/ itself are excluded (mirrors bash `grep -v "^pkg/<pkg>/"`).
//
// Returns one row of counts.
func countConsumers(root, pkg string) (PkgRow, error) {
	needle := []byte(`"github.com/loopzero/loopzero/pkg/` + pkg + `"`)
	row := PkgRow{Name: pkg}
	selfPrefix := "pkg/" + pkg + string(filepath.Separator)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
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
		if !fileContainsImport(path, needle) {
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

// fileContainsImport scans path line-by-line for the literal needle.
// Returns true on first hit. Returns false on I/O error (tolerated).
func fileContainsImport(path string, needle []byte) bool {
	f, err := os.Open(path) //nolint:gosec // operator-controlled
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if bytes.Contains(scanner.Bytes(), needle) {
			return true
		}
	}
	return false
}

// renderTable produces the bash-compatible markdown table body.
func renderTable(rows []PkgRow, now time.Time) string {
	var sb strings.Builder
	sb.WriteString("## Real Usage Matrix\n\n")
	fmt.Fprintf(&sb,
		"Tabela gerada via grep de `import \"github.com/loopzero/loopzero/pkg/X\"` em **%s**.\n",
		now.Format("2006-01-02"))
	sb.WriteString("Atualizar via `task pkg:usage-matrix:write` (script: `loopzero-ops quality pkg-usage-matrix --write`).\n")
	sb.WriteString("CI gate `task pkg:usage-matrix:check` falha se a tabela estiver out-of-date (gate PKG-G14).\n\n")
	sb.WriteString("| Pacote | Total imports | api/internal | operators/internal | cloud | cli | Outros |\n")
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
	now := cfg.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	resolvedDoc := resolvePath(root, docPath)
	pkgRoot := filepath.Join(root, "pkg")

	out := &Outcome{Mode: mode}

	pkgs, err := listPkgDirs(pkgRoot)
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("pkg-usage-matrix: list pkg/: %v", err),
			Cause:   err,
		}
	}

	for _, p := range pkgs {
		row, err := countConsumers(root, p)
		if err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("pkg-usage-matrix: count %s: %v", p, err),
				Cause:   err,
			}
		}
		out.Rows = append(out.Rows, row)
	}
	out.Generated = renderTable(out.Rows, now)

	docRaw, err := os.ReadFile(resolvedDoc) //nolint:gosec // operator-controlled
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pkg-usage-matrix: read %s: %v", resolvedDoc, err),
			Cause:   err,
		}
	}
	newDoc, err := spliceDoc(string(docRaw), out.Generated)
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pkg-usage-matrix: %v", err),
		}
	}

	switch mode {
	case ModeWrite:
		if err := os.WriteFile(resolvedDoc, []byte(newDoc), 0o600); err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
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
			fmt.Sprintf("ERROR: %s out-of-date — run `loopzero-ops quality pkg-usage-matrix --write` and commit.", resolvedDoc))
		return out, nil
	}
	return out, &loopzeroerrors.LoopZeroError{
		Code:    "OPS_USAGE",
		Message: fmt.Sprintf("pkg-usage-matrix: unknown mode %d", mode),
	}
}
