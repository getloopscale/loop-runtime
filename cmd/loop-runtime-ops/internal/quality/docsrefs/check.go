// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package docsrefs is the fitness function for normative references: a
// document or an ADR that production code and CLAUDE.md tell a reader to
// consult must actually exist.
//
// It exists because both halves failed at once, measured 2026-08-11:
//
//   - CLAUDE.md:206 declares nine contracts living in .claude/rules/. Four
//     exist. Among the missing are loop-runtime-design (cited at :159 as the
//     single-status-writer contract) and workflow (cited at :193 as the 95%
//     confidence gate) — both named as sources of Unbreakable Rules.
//   - operators/internal/conditions/retry.go and internal/metrics/metrics.go
//     cited ADR-0024 as the authority mandating migration to
//     UpdateStatusWithRetry. Before this gate was written, the only mention of
//     ADR-0024 anywhere in the repository was the plan that introduced the gate.
//     RESOLVED 2026-08-12 (ADR 2026-08-12-status-writer-contract): the phantom
//     ADR mandated the very pattern CLAUDE.md forbids, so the helper it pointed
//     at was deleted along with all eight citations, and the allowlist entry that
//     covered them was retired with them. Kept here as the motivating case.
//
// A reference that does not resolve is worse than no reference: it is followed.
// The gate does NOT try to restore the missing documents — part of their content
// migrated elsewhere, and recreating them by guesswork would produce invented
// documentation, which is strictly worse than absence. Known gaps go in a DATED
// allowlist instead, the same shape infra/quality/shell-scripts-allowlist.md and
// code-quality-allowlist.txt already use.
package docsrefs

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/calendarday"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrNoReferencesEnumerated means the scan found no normative reference at all.
//
// A gate that examined nothing has not passed — it abstained. This is the same
// invariant B-040 added to route-auth-coverage next door, and it belongs in the
// checker for the same reason ADR D1 gives: an assertion in a test fixture
// protects that fixture, while the library stays able to hand a vacuous success
// to any future consumer. It was missing here until the review measured it —
// the slice reproduced, in a gate it authored, the exact shape it exists to remove.
var ErrNoReferencesEnumerated = errors.New("scanned the tree and found ZERO normative references — CLAUDE.md is unreadable or the scan roots are wrong; the gate examined nothing, so it cannot vouch for anything")

// ErrMalformedAllowlist means the allowlist could not be parsed. It is a hard
// error rather than a skipped line: a line that reads as an exemption and
// exempts nothing is the exact defect this gate exists to remove.
var ErrMalformedAllowlist = errors.New("allowlist line is malformed")

// Kind distinguishes the two reference families, because they resolve by
// different rules.
type Kind string

const (
	KindRule Kind = "rule" // a .claude/rules/*.md contract
	KindADR  Kind = "adr"  // an ADR-NNNN identifier cited from code
)

// Reference is a value object: one normative pointer found in the tree.
type Reference struct {
	Kind   Kind
	Raw    string // ".claude/rules/loop-runtime-design.md" or "ADR-0024"
	Source string // repo-relative file the reference was found in
	Line   int
}

func (r Reference) String() string {
	return fmt.Sprintf("%s %s (%s:%d)", r.Kind, r.Raw, r.Source, r.Line)
}

// Config is the gate's input.
type Config struct {
	RepoRoot string
	// AllowlistPath defaults to <RepoRoot>/cmd/loop-runtime-ops/internal/quality/docsrefs/allowlist.txt.
	AllowlistPath string
	// Now is injectable so the sunset boundary is testable without a clock.
	Now time.Time
}

// Outcome reports what was examined and what failed.
type Outcome struct {
	Checked     int
	Allowlisted int
	Unresolved  []Reference
	// NotVerified holds rule references whose target lives in the ecosystem
	// kit, which is a gitignored install: on a checkout without the kit they
	// cannot be checked either way. They make the verdict partial, not red.
	NotVerified []Reference
	// MalformedAllowlist is the parse failure of the allowlist (wrapping
	// ErrMalformedAllowlist). A line that reads as an exemption and exempts
	// nothing is the defect this gate exists to remove, so it fails the gate.
	MalformedAllowlist error
}

// Verdict is Fail when the allowlist is malformed or any reference does not
// resolve, and Pass otherwise. References into a kit that is not installed make
// the pass partial, not red.
func (o Outcome) Verdict() verdict.Verdict {
	if o.MalformedAllowlist != nil || len(o.Unresolved) > 0 {
		return verdict.Fail
	}
	return verdict.Pass
}

// resolution is a resolver's answer for one reference.
type resolution int

const (
	resolved resolution = iota
	unresolved
	unverifiable // the place the reference points into is not present here
)

// resolver decides whether a reference points at something real. One
// implementation per Kind keeps the resolution rules from growing into a switch
// that every new kind has to edit (OCP).
type resolver interface {
	// collect finds every reference of this kind in the tree. Each Reference it
	// returns carries its own Kind, so the interface deliberately does NOT
	// expose a kind() accessor: no consumer needs to ask the resolver what it
	// resolves, and a method nobody calls is a method every implementer is
	// forced to carry (ISP).
	collect(repoRoot string) ([]Reference, error)
	// resolve reports whether the reference points at something that exists,
	// or that the place it points into is not present on this checkout.
	resolve(repoRoot string, ref Reference) resolution
}

// --- rule references ------------------------------------------------------

// pathRefPattern matches a reference written as a path, with or without the
// parent-relative prefix that points at the umbrella.
var pathRefPattern = regexp.MustCompile(`(\.\./)?\.claude/rules/([a-z0-9._-]+\.md)`)

// bareNamePattern matches a backticked bare filename.
var bareNamePattern = regexp.MustCompile("`([a-z0-9._-]+\\.md)`")

// rulesDirMention matches the rules directory quoted AS A DIRECTORY —
// `.claude/rules/` with the trailing slash inside the backticks. That is how
// CLAUDE.md:206 introduces the list of contracts, and that line is the most
// consequential one in the file.
//
// The trailing slash is doing real work. An earlier version fired the bare-name
// rule on any line containing the substring ".claude/rules/", which produced a
// FALSE POSITIVE on CLAUDE.md:215: that line cites `.claude/rules/auto-docs.md`
// as a full path AND mentions `index.md`, which belongs to the knowledge base,
// not to the rules directory. Claiming a missing contract that was never
// claimed is the same class of error as missing a real one.
var rulesDirMention = regexp.MustCompile("`\\.\\.?/?\\.?claude/rules/`|`\\.claude/rules/`")

type ruleResolver struct{}

func (ruleResolver) collect(repoRoot string) ([]Reference, error) {
	const src = "CLAUDE.md"
	body, err := os.ReadFile(filepath.Join(repoRoot, src))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", src, err)
	}
	var refs []Reference
	seen := map[string]bool{}
	for i, line := range strings.Split(string(body), "\n") {
		for _, m := range pathRefPattern.FindAllString(line, -1) {
			if !seen[m] {
				seen[m] = true
				refs = append(refs, Reference{Kind: KindRule, Raw: m, Source: src, Line: i + 1})
			}
		}
		// The bare-name form only counts on a line that quotes the rules
		// DIRECTORY, not one that merely contains a path into it.
		if !rulesDirMention.MatchString(line) {
			continue
		}
		for _, m := range bareNamePattern.FindAllStringSubmatch(line, -1) {
			raw := ".claude/rules/" + m[1]
			if !seen[raw] {
				seen[raw] = true
				refs = append(refs, Reference{Kind: KindRule, Raw: raw, Source: src, Line: i + 1})
			}
		}
	}
	return refs, nil
}

func (ruleResolver) resolve(repoRoot string, ref Reference) resolution {
	// A `../`-prefixed reference points at the umbrella, so it must be resolved
	// against the parent. Resolving it against the repo root would report a
	// false miss for a file that is exactly where it says it is.
	rel := ref.Raw
	base := repoRoot
	if strings.HasPrefix(rel, "../") {
		base = filepath.Dir(repoRoot)
		rel = strings.TrimPrefix(rel, "../")
	}
	// The rules directory is the installed kit, which is gitignored. Without
	// it the reference can be neither confirmed nor refuted on this checkout,
	// and reporting it as dangling would make the verdict depend on whether
	// one machine happens to have the kit installed.
	if fi, err := os.Stat(filepath.Join(base, ".claude", "rules")); err != nil || !fi.IsDir() {
		return unverifiable
	}
	if _, err := os.Stat(filepath.Join(base, rel)); err != nil {
		return unresolved
	}
	return resolved
}

// --- ADR references -------------------------------------------------------

var adrPattern = regexp.MustCompile(`ADR-[0-9]{4}`)

type adrResolver struct{}

// collect scans Go COMMENTS only. A raw text scan would count an identifier or
// a URL containing the token, which is the false-positive class servicesacyclic
// already solved with AST for import paths.
func (adrResolver) collect(repoRoot string) ([]Reference, error) {
	var refs []Reference
	seen := map[string]bool{}
	roots := []string{"api", "pkg", "operators", "cmd", "tools", "infra"}
	for _, r := range roots {
		dir := filepath.Join(repoRoot, r)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, p, nil, parser.ParseComments)
			if perr != nil {
				// An unparseable Go file is not this gate's business to report;
				// the compiler and the linter already own that failure.
				return nil
			}
			relPath, _ := filepath.Rel(repoRoot, p)
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					for _, m := range adrPattern.FindAllString(c.Text, -1) {
						if seen[m] {
							continue
						}
						seen[m] = true
						refs = append(refs, Reference{
							Kind: KindADR, Raw: m, Source: relPath,
							Line: fset.Position(c.Pos()).Line,
						})
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// resolve asks whether the ADR is documented ANYWHERE in the versioned docs
// tree. The gitignored .claude/ tree is not consulted: a decision recorded only
// there reaches nobody who clones, so it cannot document an ADR the code cites.
// The bar is deliberately the weaker one: ADRs here are filed by date, not by
// number (`2026-04-26-...`), so requiring a filename match would reject
// identifiers that are genuinely documented — ADR-0008 is described across
// docs/architecture and docs/operations without a file bearing its number.
func (adrResolver) resolve(repoRoot string, ref Reference) resolution {
	for _, dir := range []string{"docs"} {
		base := filepath.Join(repoRoot, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		found := false
		_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || found {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".md") {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			if !strings.Contains(string(b), ref.Raw) {
				return nil
			}
			// A DECISION DOCUMENT resolves only the ADR it DECLARES itself to be, not
			// every id it mentions. Without this, the document that records the
			// ABSENCE of an ADR fabricated its presence — which is what happened
			// with `ADR-0024`, cited five times by an ADR precisely to say that it
			// does not exist (B-086): a record of an absence must never fabricate a
			// presence.
			//
			// The criterion is STRUCTURAL and does not come from the directory: any
			// other documentation — runbook, architecture doc — keeps resolving by
			// mention, and a decision document that declares the id in its own
			// header resolves normally. Measured 2026-08-19: no decision document
			// declares an ADR id today (they identify themselves by date + slug),
			// and only TWO ids resolved exclusively through them — one is a test
			// fixture of this package, the other is not cited by production code.
			// Zero overshoot across the 35 references that resolve.
			//
			// The two ids are deliberately not written here: this gate reads Go
			// comments, so naming them would turn THIS explanation into two
			// normative citations that do not resolve. That is what happened in the
			// first version of this comment — the same trap as B-086, one level up:
			// writing about an absent ADR fabricated a citation to it.
			if isDecisionDoc(repoRoot, p) && !declaresADR(string(b), ref.Raw) {
				return nil
			}
			found = true
			return nil
		})
		if found {
			return resolved
		}
	}
	return unresolved
}

// --- allowlist ------------------------------------------------------------

type allowEntry struct {
	kind   Kind
	raw    string
	sunset time.Time
}

// loadAllowlist parses `kind | raw | YYYY-MM-DD | rationale`. A line it cannot
// parse is an error, never a skip.
func loadAllowlist(path string) (map[string]allowEntry, error) {
	out := map[string]allowEntry{}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read allowlist: %w", err)
	}
	for i, line := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		parts := strings.Split(s, "|")
		if len(parts) < 4 {
			return nil, fmt.Errorf("%w: line %d needs `kind | reference | YYYY-MM-DD | rationale`, got %q", ErrMalformedAllowlist, i+1, s)
		}
		kind := Kind(strings.TrimSpace(parts[0]))
		if kind != KindRule && kind != KindADR {
			return nil, fmt.Errorf("%w: line %d has unknown kind %q (want %q or %q)", ErrMalformedAllowlist, i+1, kind, KindRule, KindADR)
		}
		sunset, perr := calendarday.Parse(strings.TrimSpace(parts[2]))
		if perr != nil {
			return nil, fmt.Errorf("%w: line %d sunset %q is not YYYY-MM-DD", ErrMalformedAllowlist, i+1, strings.TrimSpace(parts[2]))
		}
		if strings.TrimSpace(parts[3]) == "" {
			return nil, fmt.Errorf("%w: line %d has an empty rationale — an exemption nobody justified is one nobody can retire", ErrMalformedAllowlist, i+1)
		}
		out[string(kind)+"|"+strings.TrimSpace(parts[1])] = allowEntry{kind: kind, raw: strings.TrimSpace(parts[1]), sunset: sunset}
	}
	return out, nil
}

// --- the gate -------------------------------------------------------------

// DefaultAllowlistRelPath is where the committed allowlist lives.
const DefaultAllowlistRelPath = "cmd/loop-runtime-ops/internal/quality/docsrefs/allowlist.txt"

// Check runs every resolver and reports the references that do not resolve.
// A violation is on the outcome; the error means the check could not measure.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.AllowlistPath == "" {
		cfg.AllowlistPath = filepath.Join(cfg.RepoRoot, DefaultAllowlistRelPath)
	}
	now := cfg.Now
	if now.IsZero() {
		now = time.Now()
	}

	allow, err := loadAllowlist(cfg.AllowlistPath)
	if errors.Is(err, ErrMalformedAllowlist) {
		return Outcome{MalformedAllowlist: err}, nil
	}
	if err != nil {
		return Outcome{}, err
	}

	var out Outcome
	for _, r := range []resolver{ruleResolver{}, adrResolver{}} {
		refs, cerr := r.collect(cfg.RepoRoot)
		if cerr != nil {
			return out, cerr
		}
		for _, ref := range refs {
			answer := r.resolve(cfg.RepoRoot, ref)
			if answer == unverifiable {
				out.NotVerified = append(out.NotVerified, ref)
				continue
			}
			out.Checked++
			if answer == resolved {
				continue
			}
			// The sunset date is INCLUSIVE: an entry dated today still exempts,
			// and expires the day after. Without fixing the boundary the gate
			// would fail in one timezone and pass in another.
			// Compare CALENDAR DAYS, not instants. Truncating a timestamp to
			// 24h works in UTC and silently shifts the boundary by a day for
			// anyone west of Greenwich — the sunset would expire early in BRT.
			if e, ok := allow[string(ref.Kind)+"|"+ref.Raw]; ok && !calendarday.Of(now).After(e.sunset) {
				out.Allowlisted++
				continue
			}
			out.Unresolved = append(out.Unresolved, ref)
		}
	}
	// An unresolved reference was counted in Checked, so a tree full of broken
	// references is reported as broken references, never as an empty scan.
	if out.Checked == 0 {
		return out, ErrNoReferencesEnumerated
	}
	return out, nil
}

// decisionDocDirs are the trees holding documents that DECIDE about ADRs.
// A document here is an actor in the matter, not a neutral description of it.
var decisionDocDirs = []string{
	filepath.Join("docs", "decisions"),
}

func isDecisionDoc(repoRoot, path string) bool {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return false
	}
	for _, d := range decisionDocDirs {
		if strings.HasPrefix(rel, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// declaresADR reports whether the document IDENTIFIES itself as that ADR, rather
// than merely mentioning it.
//
// The criterion is the TITLE: the first non-empty line, and nothing else. A
// document whose title carries the id IS that ADR; one that only cites it in
// the body is talking ABOUT it — which is how a record stating that an ADR
// does not exist stopped fabricating its presence (B-086).
func declaresADR(content, raw string) bool {
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		// first non-empty line — the title
		return strings.Contains(t, raw)
	}
	return false
}
