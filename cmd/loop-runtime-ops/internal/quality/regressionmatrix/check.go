// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package regressionmatrix is the matrix-integrity gate: it audits the
// bootstrap regression matrix (docs/runbooks.md) to
// guarantee every entry's "Verification" artifact still resolves in the repo.
// If a test was renamed, deleted, or never existed, the matrix becomes a lie —
// a silent regression of the protection it claims.
//
// It replaces two bash scripts (Track B TB5 of plan
// 2026-05-24-script-minimization-program-plan):
//   - infra/tests/parity/audit-regression-matrix.sh — the audit itself, and
//   - infra/tests/parity/audit-golden-test.sh — the golden test that proves
//     the audit detects a fabricated broken entry.
//
// Why the migration (finding-2026-05-24-audit-regression-matrix-set-e-abort):
// the bash audit exited 2 BEFORE scanning. Its helper signalled "N/A" with a
// `return 2`, which under `set -euo pipefail` propagates as a fatal abort — so
// the gate was a silent no-op (printed its header, aborted, never reported a
// MISS). In Go the "N/A" category is a value (Skip), not a non-zero return, so
// no control-flow fragility exists. The golden test (TestGolden_*) is the
// faithful port of audit-golden-test.sh.
//
// Design constraints:
//   - Pure Go, hermetic, injectable: ResolveArtifact takes a TokenResolver so a
//     test can supply an in-memory corpus instead of scanning the real repo.
//   - Token extraction + boundary matching faithfully mirror the bash regex so
//     the verdict on the real matrix matches what the (working) bash intended.
package regressionmatrix

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// EntryResult is the resolution category of a single matrix row.
type EntryResult int

const (
	// Resolved — at least one verification token was found in the repo (OK).
	Resolved EntryResult = iota
	// Miss — no token resolved (broken protection if critical).
	Miss
	// Skip — verification is a live/runtime/operator step (offline-unresolvable)
	// or yields no usable tokens. Not a failure.
	Skip
)

// CanRecurNo is the matrix value (with the diacritic) marking an entry whose
// regression CANNOT recur safely — a MISS here is critical. The matrix is written
// in Portuguese and the value below means "NO"; this literal is DATA matched
// against the matrix cells, so it must stay exactly as the matrix spells it.
const CanRecurNo = "NÃO" // english-only: data matched verbatim against the matrix cells

// Row is one parsed data row of the matrix.
type Row struct {
	ID           string
	Verification string
	CanRecur     string
}

// TokenResolver answers whether a verification token resolves to repo content.
type TokenResolver interface {
	Resolves(token string) bool
}

// dataRowRe matches a markdown table data row whose first cell is a number.
var dataRowRe = regexp.MustCompile(`^\s*\|\s*[0-9]+\s*\|`)

// ParseMatrix extracts the data rows from the matrix markdown. It reads the
// pipe-delimited table, taking column 1 as ID, column 7 as Verification, and
// column 8 as "Pode Recorrer?" — the matrix's own header, meaning "can it recur?"
// (1-indexed among the cells BETWEEN the outer
// pipes — identical to the bash `IFS='|' read` positional parse).
func ParseMatrix(content string) []Row {
	var rows []Row
	for _, line := range strings.Split(content, "\n") {
		if !dataRowRe.MatchString(line) {
			continue
		}
		// Split on '|'. A leading/trailing pipe yields empty first/last fields,
		// mirroring `IFS='|' read -r _ id _cat _ _ _ _ verify can_recur _`.
		cells := strings.Split(line, "|")
		// cells[0] = "" (before first pipe). Data cells start at index 1.
		// id=cells[1], verify=cells[7], can_recur=cells[8].
		if len(cells) < 9 {
			continue
		}
		id := strings.TrimSpace(cells[1])
		if !isNumeric(id) {
			continue
		}
		rows = append(rows, Row{
			ID:           id,
			Verification: strings.TrimSpace(cells[7]),
			CanRecur:     strings.TrimSpace(cells[8]),
		})
	}
	return rows
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// tokenRe mirrors the bash extraction regex. Longest alternative first so the
// POSIX leftmost-longest engine prefers the filename form.
var tokenRe = regexp.MustCompilePOSIX(
	`[A-Za-z][A-Za-z0-9_/.-]{4,}\.(ts|go|sh|md|yaml|yml|gotmpl|toml|json)|[A-Za-z][A-Za-z0-9_]{4,}`)

// noiseTokens are common words filtered out (faithful port of the bash grep -vE
// list) so we don't grep the repo for "status"/"exists"/etc.
var noiseTokens = map[string]bool{
	"grep": true, "head": true, "tail": true, "count": true, "equal": true,
	"true": true, "false": true, "status": true, "exists": true, "when": true,
	"with": true, "then": true, "that": true, "using": true, "after": true,
	"before": true, "live": true, "runtime": true, "verify": true,
	"matches": true, "contains": true,
}

// ExtractTokens pulls candidate identifiers (>= 5 chars) from a verification
// string, drops noise words, and returns a sorted, de-duplicated set.
func ExtractTokens(verification string) []string {
	found := tokenRe.FindAllString(verification, -1)
	set := make(map[string]bool, len(found))
	for _, t := range found {
		if noiseTokens[t] {
			continue
		}
		set[t] = true
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// ResolveArtifact categorises one verification string. It mirrors the bash
// resolve_artifact ordering exactly:
//
//	empty                      -> Miss
//	"live "/"runtime"/"verify" -> Skip   (offline-unresolvable)
//	no usable tokens           -> Skip
//	any token resolves         -> Resolved
//	otherwise                  -> Miss
func ResolveArtifact(verification string, resolver TokenResolver) EntryResult {
	if verification == "" {
		return Miss
	}
	if strings.Contains(verification, "live ") ||
		strings.Contains(verification, "runtime") ||
		strings.Contains(verification, "verify") {
		return Skip
	}
	tokens := ExtractTokens(verification)
	if len(tokens) == 0 {
		return Skip
	}
	for _, tok := range tokens {
		if resolver.Resolves(tok) {
			return Resolved
		}
	}
	return Miss
}

// Audit aggregates the rows of the matrix.
type Audit struct {
	Total        int
	OK           int
	Miss         int
	Skip         int
	MissCritical int
	MissEntries  []Row // rows that missed (any criticality)
	Strict       bool  // if true, any Miss fails the gate
}

// Failed reports whether the gate should fail (non-zero exit).
func (v Audit) Failed() bool {
	if v.MissCritical > 0 {
		return true
	}
	if v.Strict && v.Miss > 0 {
		return true
	}
	return false
}

// Verdict is Fail when the audit fails the gate and Pass otherwise.
func (v Audit) Verdict() verdict.Verdict { return verdict.FromPass(!v.Failed()) }

// Evaluate runs every row through the resolver and aggregates an Audit.
func Evaluate(rows []Row, resolver TokenResolver, strict bool) Audit {
	v := Audit{Strict: strict}
	for _, r := range rows {
		v.Total++
		switch ResolveArtifact(r.Verification, resolver) {
		case Resolved:
			v.OK++
		case Skip:
			v.Skip++
		case Miss:
			v.Miss++
			v.MissEntries = append(v.MissEntries, r)
			if r.CanRecur == CanRecurNo {
				v.MissCritical++
			}
		}
	}
	return v
}

// Check parses the matrix at matrixPath, builds a repo-backed resolver rooted at
// root, and evaluates. This is the production entry point used by the
// loop-runtime-ops subcommand.
func Check(matrixPath, root string, strict bool) (Audit, error) {
	data, err := os.ReadFile(matrixPath) // #nosec G304 -- repo-internal matrix path
	if err != nil {
		return Audit{}, err
	}
	rows := ParseMatrix(string(data))
	corpus, err := BuildCorpus(root)
	if err != nil {
		return Audit{}, err
	}
	return Evaluate(rows, corpus, strict), nil
}

// --- repo-backed resolver ---------------------------------------------------

// searchRoots mirror the bash `roots` array (relative to repo root).
var searchRoots = []string{
	"cli", "api", "operators", "pkg", "infra", "docs", "charts", "scripts",
}

// includeExts mirror the bash `--include` globs (Dockerfile* handled by prefix).
var includeExts = map[string]bool{
	".ts": true, ".go": true, ".sh": true, ".md": true, ".yaml": true,
	".yml": true, ".gotmpl": true, ".toml": true, ".json": true,
}

// Corpus is an in-memory concatenation of all candidate files. Loaded once;
// each token is then resolved by boundary-aware substring search in memory
// (replacing the bash per-token recursive grep).
type Corpus struct {
	files []string // file contents
}

// Resolves reports whether token appears in any file with non-alpha boundaries
// on both sides (mirrors the bash `(^|[^A-Za-z])tok([^A-Za-z]|$)` regex).
func (c *Corpus) Resolves(token string) bool {
	for _, content := range c.files {
		if hasTokenBoundary(content, token) {
			return true
		}
	}
	return false
}

func hasTokenBoundary(text, token string) bool {
	if token == "" {
		return false
	}
	start := 0
	for {
		i := strings.Index(text[start:], token)
		if i < 0 {
			return false
		}
		pos := start + i
		leftOK := pos == 0 || !isAlphaByte(text[pos-1])
		end := pos + len(token)
		rightOK := end == len(text) || !isAlphaByte(text[end])
		if leftOK && rightOK {
			return true
		}
		start = pos + 1
	}
}

func isAlphaByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// BuildCorpus walks the search roots under root and loads every candidate file
// into memory.
func BuildCorpus(root string) (*Corpus, error) {
	c := &Corpus{}
	for _, sub := range searchRoots {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); err != nil {
			continue // missing root is fine (mirrors bash grep over absent dir)
		}
		walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable subtrees, don't abort the gate
			}
			if d.IsDir() {
				return nil
			}
			if !isCandidateFile(p) {
				return nil
			}
			b, readErr := os.ReadFile(p) // #nosec G304 -- repo-internal scan
			if readErr != nil {
				return nil
			}
			c.files = append(c.files, string(b))
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return c, nil
}

func isCandidateFile(p string) bool {
	base := filepath.Base(p)
	if strings.HasPrefix(base, "Dockerfile") {
		return true
	}
	return includeExts[filepath.Ext(p)]
}

// SetResolver is a trivial in-memory TokenResolver backed by a set of tokens.
// Used by the golden test to stay hermetic (no repo scan).
type SetResolver map[string]bool

// Resolves reports membership.
func (s SetResolver) Resolves(token string) bool { return s[token] }
