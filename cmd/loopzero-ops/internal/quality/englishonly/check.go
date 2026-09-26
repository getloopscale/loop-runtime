// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package englishonly is the english-only gate: everything this repository
// versions is written in English — comments, docstrings, log and error text,
// YAML comments, test names, fixtures.
//
// The scope is what git tracks (git ls-files), not what sits on disk, so a
// local note never fails the gate and a committed one always does. Three
// things are outside it:
//
//   - .squad/, the per-machine write root of the maintenance kit, whose run
//     output is not this repository's source;
//   - generated files (a "Code generated ... DO NOT EDIT." header), which are
//     fixed at their generator, never by hand;
//   - binary files.
//
// A line keeps Portuguese only with an exemption marker on the line itself:
// the word english-only, a colon, and the reason. The reason is mandatory and
// must be at least minReasonWords words, because a silent opt-out is the thing
// the gate exists to prevent. The legitimate cases are copy whose language is
// a product requirement, verbatim quotations, and fixtures that must be
// Portuguese to exercise what they test. A marker with no reason is itself a
// finding. JSON cannot carry a comment, so a JSON file that stores other
// files' lines verbatim is registered in verbatimCaptures instead, with the
// keys that hold the captures and the reason.
//
// Prose and names are read separately (names.go). The prose detector skips
// identifiers and paths; Go identifiers and the components of tracked paths
// are judged against their own word list, so a Portuguese package, file,
// type or constant is a finding too. A Go identifier keeps its name with the
// same line marker; a path has no line to carry one and is renamed instead.
//
// Why a repository gate: the only language check was an advisory editor hook
// in a local, gitignored kit. It ran on the file being edited, never on the
// tree, and ~313 lines of Portuguese had accumulated in 74 files (#380).
package englishonly

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/linescan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// marker is the exemption key. It is assembled rather than written with its
// colon so this file does not carry a reasonless marker of its own.
const marker = "english-only"

// minReasonWords is the shortest reason an exemption may give. "quote" or
// "verbatim label" says that an exemption was taken, not why this line needs
// one.
const minReasonWords = 5

// Finding kinds.
const (
	KindPortuguese             = "portuguese"
	KindExemptionWithoutReason = "exemption-without-reason"
)

var (
	markerRE = regexp.MustCompile(regexp.QuoteMeta(marker+":") + `(.*)$`)
	// commentClosers end a comment that carried the marker; they are not part
	// of the reason.
	commentClosers = strings.NewReplacer("*/", " ", "-->", " ", "#}", " ", "}}", " ")
	generatedRE    = regexp.MustCompile(`(?m)^.*Code generated .* DO NOT EDIT\.`)
)

// generatedHeaderBytes is how far into a file the generated header is looked
// for; the Go convention puts it before the package clause.
const generatedHeaderBytes = 2048

// excludedPrefixes are repository-relative directories outside the gate.
var excludedPrefixes = []string{".squad/"}

// clusterRenameBaseline is the one tracked file today that stores other files'
// lines verbatim.
const clusterRenameBaseline = "infra/quality/snapshots/cluster-topology-naming-generic-baseline.json"

// capture is a JSON file whose values under keys are byte-for-byte copies of
// lines captured from other files. JSON has no comments, so these lines cannot
// carry an inline exemption; the registry is where their reason is written.
// Only the listed keys are exempt: the file's authored fields (notes, reasons)
// are scanned like any other line.
type capture struct {
	keys   []string
	reason string
}

var verbatimCaptures = map[string]capture{
	clusterRenameBaseline: {
		keys: []string{"content", "before", "after"},
		reason: "cluster-rename-guard asserts that each captured line still exists byte for byte; " +
			"a translated capture no longer matches the file it was taken from, so the proof would break",
	},
}

// jsonKeyRE reads the key of a one-line JSON member: `  "content": "..."`.
var jsonKeyRE = regexp.MustCompile(`^\s*"([^"]+)"\s*:`)

// isVerbatimCapture reports whether line holds a registered capture of rel.
func isVerbatimCapture(rel, line string) bool {
	c, ok := verbatimCaptures[rel]
	if !ok {
		return false
	}
	m := jsonKeyRE.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	for _, k := range c.keys {
		if m[1] == k {
			return true
		}
	}
	return false
}

// Config configures a run.
type Config struct {
	RepoRoot string
}

// Finding is one line the gate refuses.
type Finding struct {
	Path     string
	Line     int
	Kind     string
	Evidence []string
	Text     string
}

func (f Finding) String() string {
	switch f.Kind {
	case KindPortugueseIdentifier:
		return fmt.Sprintf("%s:%d: Portuguese identifier %s (%s)", f.Path, f.Line, f.Text, strings.Join(f.Evidence, ", "))
	case KindPortuguesePath:
		return fmt.Sprintf("%s: Portuguese file or directory name (%s)", f.Path, strings.Join(f.Evidence, ", "))
	case KindExemptionWithoutReason:
		return fmt.Sprintf("%s:%d: exemption marker without a reason of at least %d words: %s", f.Path, f.Line, minReasonWords, f.Text)
	default:
		return fmt.Sprintf("%s:%d: Portuguese (%s): %s", f.Path, f.Line, strings.Join(f.Evidence, ", "), f.Text)
	}
}

// Report is the outcome of a run.
type Report struct {
	// Files counts the tracked text files scanned.
	Files int
	// Generated counts the tracked files skipped as generated.
	Generated int
	// Exempted counts lines kept by a marker with a reason.
	Exempted int
	Findings []Finding
}

// Verdict is Fail when any line, identifier or path is not English.
func (r Report) Verdict() verdict.Verdict { return verdict.Of(r.Findings) }

// Check scans every tracked text file under cfg.RepoRoot.
func Check(cfg Config) (Report, error) {
	var r Report
	paths, err := trackedFiles(cfg.RepoRoot)
	if err != nil {
		return r, err
	}
	judgedPaths := map[string]bool{}
	for _, rel := range paths {
		if excluded(rel) {
			continue
		}
		r.Findings = append(r.Findings, pathFindings(rel, judgedPaths)...)
		full := filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			return r, fmt.Errorf("stat %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			continue // a symlink or submodule; its target is scanned where it is tracked
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return r, fmt.Errorf("read %s: %w", rel, err)
		}
		if isBinary(data) {
			continue
		}
		if isGenerated(data) {
			r.Generated++
			continue
		}
		findings, exempted, err := scan(rel, data)
		if err != nil {
			return r, err
		}
		r.Files++
		r.Exempted += exempted
		r.Findings = append(r.Findings, findings...)
	}
	if r.Files == 0 {
		return r, errors.New("no tracked text file was scanned; a scan over nothing is not a pass")
	}
	return r, nil
}

// ScanLines scans one file's content as if it were tracked at rel, and returns
// its findings and how many lines an exemption kept.
func ScanLines(rel, content string) ([]Finding, int, error) {
	return scan(rel, []byte(content))
}

func scan(rel string, data []byte) ([]Finding, int, error) {
	lines, err := linescan.Lines(data)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", rel, err)
	}
	var (
		findings    []Finding
		exempted    int
		exemptLines = map[int]bool{}
	)
	for i, line := range lines {
		if m := markerRE.FindStringSubmatch(line); m != nil {
			if reasonWords(m[1]) >= minReasonWords {
				exempted++
				exemptLines[i+1] = true
				continue
			}
			findings = append(findings, Finding{Path: rel, Line: i + 1, Kind: KindExemptionWithoutReason, Text: strings.TrimSpace(line)})
			continue
		}
		if isVerbatimCapture(rel, line) {
			exempted++
			continue
		}
		if v := Classify(line); v.Portuguese {
			findings = append(findings, Finding{Path: rel, Line: i + 1, Kind: KindPortuguese, Evidence: v.Evidence, Text: strings.TrimSpace(line)})
		}
	}
	if strings.HasSuffix(rel, ".go") {
		findings = append(findings, scanGoIdentifiers(rel, data, exemptLines)...)
	}
	return findings, exempted, nil
}

// reasonWords counts the words of an exemption reason: fields with a letter.
func reasonWords(reason string) int {
	n := 0
	for _, f := range strings.Fields(commentClosers.Replace(reason)) {
		if strings.IndexFunc(f, unicode.IsLetter) >= 0 {
			n++
		}
	}
	return n
}

func excluded(rel string) bool {
	for _, p := range excludedPrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

func isGenerated(data []byte) bool {
	head := data
	if len(head) > generatedHeaderBytes {
		head = head[:generatedHeaderBytes]
	}
	return generatedRE.Match(head)
}

// trackedFiles lists what git tracks under root, repository-relative.
func trackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w: %s", root, err, strings.TrimSpace(stderr.String()))
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
