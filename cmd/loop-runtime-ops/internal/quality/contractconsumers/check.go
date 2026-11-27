// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package contractconsumers provides the `contract-consumers` quality gate.
//
// Detects when a backend response shape NARROWS — a Go struct field with a
// `json:"<tag>"` annotation is REMOVED in the PR diff — AND at least one
// consumer in the dashboard / CLI / API still references that field. Forces
// the contract-shift to be visible at PR review time instead of surfacing as
// a silent UX regression during sponsor LIVE walkthrough.
//
// Scope: Trivial-tier MVP. Covers the regression shape observed in the
// 2026-06-08 dashboard-screens-review-fixes T5.3 walkthrough (3 of 5
// LIVE-only bugs were of this exact class: T2.1 SQL refactor dropped legacy
// fields from the listing response, and the detail page + list page
// silently inherited the regression because their consumers still read
// those fields).
//
// What this gate detects:
//   - Removed `<Field> <Type> `json:"<tag>"“ lines in tracked Go files under:
//     api/internal/ports/*.go
//     api/internal/<feature>/<name>http/*.go and <feature>/ports.go (the
//     handlers and the ports a feature owns)
//     api/internal/db/generated/*.go
//   - For each removed field, greps consumers in:
//     ../loop-console/dashboard/src/  (TS/TSX, sibling checkout)
//     ../loop-runtime-cli/src/              (TS, sibling checkout)
//     api/internal/                 (Go)
//     for `.<tag>` references (e.g., `p.apps_count`, `b.apps`,
//     `project.last_deploy`). A sibling checkout that is absent is reported
//     in MissingRoots and makes the outcome Partial — the TypeScript readers
//     are the ones the Go compiler cannot see, so a verdict without them
//     must say it covered less.
//   - Findings reported with file:line + the dropped field's JSON tag.
//
// What this gate does NOT detect (out-of-scope, by design):
//   - CSS cascade / visual regressions (needs headless browser; sponsor walkthrough)
//   - Empty-state UX honesty (needs cross-data-state semantic check)
//   - SQL SELECT-list drops without corresponding Go struct change (the
//     sqlc-sync gate covers this indirectly via generated/* diff)
//
// Plan ref: post-2026-06-08 dashboard-screens-review-fixes T5.3 walkthrough
// closure. Sponsor verbatim mandate "Sponsor walkthrough manual continua
// sendo o gate final" — this gate REDUCES the bug rate that reaches sponsor,
// NOT replaces the sponsor as the final visual gate.
package contractconsumers

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/featurefence"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config controls the gate behavior.
type Config struct {
	// RepoRoot is the repository root. Defaults to "." in callers.
	RepoRoot string
	// BaseRef is the git ref the PR branches from (e.g., origin/develop).
	// When empty, defaults to "origin/develop".
	BaseRef string
	// HeadRef is the git ref of the PR HEAD. When empty, defaults to "HEAD".
	HeadRef string
	// AcknowledgementsFile lists drops decided on purpose (tag | file | reason).
	// Relative paths resolve against RepoRoot. Empty uses
	// DefaultAcknowledgementsFile; an absent file acknowledges nothing.
	AcknowledgementsFile string
}

// Finding represents one consumer that still reads a dropped field.
type Finding struct {
	// DroppedTag is the JSON tag of the removed struct field (e.g., "apps_count").
	DroppedTag string
	// ConsumerFile is the file where the field is still read.
	ConsumerFile string
	// ConsumerLine is the line number in ConsumerFile.
	ConsumerLine int
	// ConsumerSnippet is the line content with the field reference.
	ConsumerSnippet string
}

// Outcome reports the gate verdict.
type Outcome struct {
	Pass        bool
	Findings    []Finding
	DroppedTags []string // sorted list of dropped JSON tags detected
	ScanRoots   []string // consumer scan roots used
	// TrackedFiles is the declared contract surface, mirroring ScanRoots: without it a
	// caller can say HOW MANY tracked paths went missing but not out of how many.
	TrackedFiles []string
	// MissingRoots are the declared consumer roots that do not exist on disk
	// (typically a sibling checkout that is not cloned). They were not scanned.
	MissingRoots []string
	// Partial is true when at least one consumer root was not scanned: the
	// verdict then covers less than the declared consumer surface.
	Partial bool
	// MissingTracked holds the declared TRACKED paths that do not exist in the tree.
	// Same cause as MissingRoots, and the graver half: trackedFiles decides WHICH tags
	// the gate ever looks at, so a path missing here does not merely shrink the search
	// for the reader — it makes the field drop never visible at all, and the verdict
	// becomes PASS over an empty population.
	MissingTracked []string
	// Acknowledged are dropped tags whose every removed declaration is recorded
	// in the acknowledgements file. They do not fail the gate and are always
	// reported, so an acknowledged drop is never a silent one.
	Acknowledged []string
	// StaleAcknowledgements are acknowledgements that match no current drop.
	// They fail the gate: left in place, one would pre-approve the next
	// removal of that tag from that file.
	StaleAcknowledgements []string
	CommandError          string // populated if git or grep failed
}

// Verdict is Fail when a tracked path is missing (the gate would read nothing),
// when an acknowledgement outlived its drop, or when a consumer still reads a
// dropped tag; Pass otherwise. A missing consumer root makes the pass partial,
// not a failure. CommandError is not a verdict: a check whose git or grep
// failed could not measure.
func (o Outcome) Verdict() verdict.Verdict {
	if len(o.MissingTracked) > 0 || len(o.StaleAcknowledgements) > 0 || !o.Pass {
		return verdict.Fail
	}
	return verdict.Pass
}

// trackedFiles enumerates source files whose struct field drops trigger the gate.
// Restricted to the backend API contract surface — see package doc.
//
// These paths are `git diff` and `git grep` pathspecs, and both SILENTLY IGNORE a
// pathspec that matches nothing (measured: `git diff -- <nonexistent path>` exits 0,
// with nothing on stderr). Absence is therefore measured by missingTrackedPaths and
// reported; otherwise a path that moved becomes less coverage under the same verdict.
//
// Notice for whoever performs the rename: `api/internal/domain` is slated to become
// `api/internal/ports` (B-057, bullet 1). This list has to move with it — it carries
// 93 `json:` tags (measured 2026-09-05), among them ProjectSummary, the struct behind
// the 2026-06-08 regression this package doc cites as the reason the gate exists.
var trackedFiles = []string{
	"api/internal/ports/types.go", // the sink file the 2026-06-08 regression struct lives in
	"api/internal/ports/",
	"api/internal/db/generated/", // any *.go under this dir
}

// featureSurface names the feature half of the contract surface in reports.
// The handlers used to live under one directory, api/internal/routes/, and every
// port type under api/internal/ports/. Since api/internal was organized by
// feature (2026-09-25) the handlers are the `<feature>/<name>http` packages and a
// port only one feature consumes lives in that feature's `ports.go`. Both are
// discovered rather than declared, and a discovery that finds neither is
// reported under this name.
//
// What is tracked is exactly the code that was tracked before the move, at its
// new address: the handlers and the port types. A feature's service code came
// from api/internal/services/, which was never tracked, and tracking it now
// would count at head declarations the base never counted — masking a real drop.
const featureSurface = "api/internal/<feature>/{<name>http/,ports.go}"

// legacyRoutesDir is the handler tree before the feature layout. It stays in the
// pathspec handed to git even where it no longer exists in the checkout: the
// comparison base may predate the move, and without it a declaration removed
// from a file under routes/ would vanish from the diff instead of showing up as
// a drop — or as the rename it is. It is never reported missing; git ignores a
// pathspec that matches nothing, which is exactly the behaviour wanted here.
const legacyRoutesDir = "api/internal/routes/"

// featurePaths returns every HTTP package directory (with a trailing slash, so
// git reads it as a directory pathspec) and every feature `ports.go`.
func featurePaths(root string) []string {
	var out []string
	if dirs, err := featurefence.HTTPPackageDirs(root); err == nil {
		for _, d := range dirs {
			out = append(out, filepath.ToSlash(d)+"/")
		}
	}
	ports, err := filepath.Glob(filepath.Join(root, "api", "internal", "*", "ports.go"))
	if err == nil {
		for _, p := range ports {
			if rel, rerr := filepath.Rel(root, p); rerr == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	return out
}

// trackedPaths is every pathspec handed to git: the declared files, the feature
// handlers and ports, and the legacy routes tree.
func trackedPaths(root string) []string {
	paths := append(append([]string{}, trackedFiles...), featurePaths(root)...)
	return append(paths, legacyRoutesDir)
}

// consumerRoot is one directory grepped for dropped-field references. Repo is
// the git checkout that holds it, relative to Config.RepoRoot ("." for this
// repository); the dashboard and the CLI left this repository at the
// decomposition (M4, M2) and are read from their sibling checkouts.
type consumerRoot struct {
	Repo string
	Path string
}

// String is the root as a path relative to Config.RepoRoot.
func (c consumerRoot) String() string { return filepath.ToSlash(filepath.Join(c.Repo, c.Path)) + "/" }

// consumerRoots enumerates the directories grepped for dropped-field references.
var consumerRoots = []consumerRoot{
	{Repo: "../loop-console", Path: "dashboard/src/"},
	{Repo: "../loop-runtime-cli", Path: "src/"},
	{Repo: ".", Path: "api/internal/"},
}

// scanRootNames renders consumerRoots for the Outcome.
func scanRootNames() []string {
	names := make([]string, 0, len(consumerRoots))
	for _, c := range consumerRoots {
		names = append(names, c.String())
	}
	return names
}

// jsonFieldRegex matches `Name Type `json:"tag[,opts]"“ style declarations.
// Captures the JSON tag (group 1) before any comma options like `,omitempty`.
var jsonFieldRegex = regexp.MustCompile("json:\"([^\",]+)")

// Check runs the gate. Config.RepoRoot must point at a git checkout. Returns
// an Outcome describing any consumers found that still read dropped fields.
// A nil outcome is returned only on git command failure (still reported via
// the returned Outcome.CommandError when convertible).
func Check(cfg Config) (*Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	base := cfg.BaseRef
	if base == "" {
		base = "origin/develop"
	}
	head := cfg.HeadRef
	if head == "" {
		head = "HEAD"
	}

	// Computed BEFORE the first return: on the git-error path coverage is lost
	// too, and that is the path nobody looks at.
	missingRoots := missingConsumerRoots(root)
	missingTracked := missingTrackedPaths(root)

	diff, err := diffTracked(root, base, head)
	var droppedTags []string
	if err == nil {
		droppedTags, err = droppedTagsIn(root, base, head, diff)
	}
	if err != nil {
		return &Outcome{
			CommandError:   err.Error(),
			ScanRoots:      scanRootNames(),
			TrackedFiles:   trackedPaths(root),
			MissingRoots:   missingRoots,
			Partial:        len(missingRoots) > 0,
			MissingTracked: missingTracked,
		}, nil
	}

	acks, err := loadAcknowledgements(root, cfg.AcknowledgementsFile)
	if err != nil {
		return nil, err
	}
	droppedTags, acknowledged, stale, err := applyAcknowledgements(root, base, head, droppedTags, acks, renamesIn(diff))
	if err != nil {
		return nil, err
	}

	out := &Outcome{
		DroppedTags:           droppedTags,
		Acknowledged:          acknowledged,
		StaleAcknowledgements: stale,
		ScanRoots:             scanRootNames(),
		TrackedFiles:          trackedPaths(root),
		MissingRoots:          missingRoots,
		Partial:               len(missingRoots) > 0,
		MissingTracked:        missingTracked,
		Pass:                  len(stale) == 0,
	}
	// A missing consumer root is lost coverage and must surface even when no
	// tag dropped — that PASS is exactly where it would vanish. It does not fail
	// the gate on its own; it makes the verdict Partial.
	if len(droppedTags) == 0 {
		return out, nil
	}

	for _, tag := range droppedTags {
		findings, err := grepConsumersForTag(root, tag, consumerRoots)
		if err != nil {
			out.CommandError = err.Error()
			out.Pass = false
			return out, nil
		}
		out.Findings = append(out.Findings, findings...)
	}

	if len(out.Findings) > 0 {
		out.Pass = false
	}
	return out, nil
}

// collectDroppedJSONTags runs `git diff <base>..<head>` over the tracked
// files and extracts JSON tags from REMOVED lines (lines starting with "-"
// in unified diff) that match `json:"<tag>"`. Returns the unique sorted list.
func collectDroppedJSONTags(root, base, head string) ([]string, error) {
	diff, err := diffTracked(root, base, head)
	if err != nil {
		return nil, err
	}
	return droppedTagsIn(root, base, head, diff)
}

// diffTracked runs `git diff <base>..<head>` over the tracked paths once; the
// dropped tags and the renames are both read from its output.
func diffTracked(root, base, head string) (string, error) {
	args := []string{"-C", root, "diff", "--unified=0", base + ".." + head, "--"}
	args = append(args, trackedPaths(root)...)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff failed: %w (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

// renamesIn maps each file the diff reports as renamed to its new path, from
// the `rename from` / `rename to` header pairs git writes with rename detection
// on (its default).
func renamesIn(diff string) map[string]string {
	renamed := map[string]string{}
	from := ""
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "rename from "):
			from = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to ") && from != "":
			renamed[from] = strings.TrimPrefix(line, "rename to ")
			from = ""
		}
	}
	return renamed
}

// droppedTagsIn extracts the JSON tags from the REMOVED lines of diff and keeps
// those declared fewer times at head than at base.
func droppedTagsIn(root, base, head, diff string) ([]string, error) {
	seen := map[string]struct{}{}
	for _, line := range strings.Split(diff, "\n") {
		// Removed line in unified diff starts with "-" (but skip "---" header).
		if !strings.HasPrefix(line, "-") || strings.HasPrefix(line, "---") {
			continue
		}
		matches := jsonFieldRegex.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}
		tag := strings.TrimSpace(matches[1])
		if tag == "" || tag == "-" {
			continue // empty tag OR explicit-skip `json:"-"`
		}
		seen[tag] = struct{}{}
	}

	// A removed LINE is not a removed TAG. The diff says the line changed; only
	// HEAD says whether the field still exists. Any edit that rewrites the line
	// — gofmt collapsing padding before a trailing comment, a renamed Go field,
	// a reworded comment — puts the tag on the "-" side while the contract is
	// untouched.
	//
	// Measured on the routes split (B-080): `gofmt -w` turned
	//   -\tRepo string `json:"repo"`            // "owner/repo"
	//   +\tRepo string `json:"repo"` // "owner/repo"
	// into a reported drop of `repo`, with nine named "consumers", and blocked
	// a push that changed no contract at all. A gate that blames whitespace for
	// a wire break is a gate people learn to push past.
	tags := make([]string, 0, len(seen))
	for t := range seen {
		before, err := structsDeclaringTagAt(root, base, t)
		if err != nil {
			return nil, err
		}
		after, err := structsDeclaringTagAt(root, head, t)
		if err != nil {
			return nil, err
		}
		// One declaration FEWER is the drop. Equal or more means the tag changed
		// struct name, file or package — none of which touches the wire
		// contract.
		if after >= before {
			continue
		}
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags, nil
}

// structsDeclaringTagAt counts HOW MANY structs declare `json:"<tag>"` in the
// tracked files, at the given revision.
//
// The previous version only asked whether the tag existed in ANY struct, and that
// had a measured hole: `repositories.go` declared `json:"repo"` TWICE, and
// removing it from one was invisible because the other answered "still exists" (B-100).
//
// Counting, rather than identifying the struct by NAME, is deliberate. Scoping by
// name would catch the hole but create a new false positive: renaming the struct —
// or moving it to another package, which the original design explicitly protects —
// does not change the wire contract and would start being reported as a break. A
// gate that blames a rename is a gate people learn to work around, which is the
// cost this very item records. The count drops when a declaration disappears and
// does not move when a declaration changes name or place.
//
// REMAINING LIMIT, declared: if a single commit REMOVES the tag from one struct and
// ADDS it to another, the count does not move and the swap passes. That is narrower
// than the previous hole and has no cure without type information on both sides.
func structsDeclaringTagAt(root, rev, tag string) (int, error) {
	perFile, err := declarationsPerFileAt(root, rev, tag)
	total := 0
	for _, n := range perFile {
		total += n
	}
	return total, err
}

// declarationsPerFileAt is structsDeclaringTagAt broken down by file, which is
// what an acknowledgement is scoped to.
func declarationsPerFileAt(root, rev, tag string) (map[string]int, error) {
	pattern := `json:"` + regexp.QuoteMeta(tag) + `[",]`

	args := []string{"-C", root, "grep", "-cE", pattern, rev, "--"}
	args = append(args, trackedPaths(root)...)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// `git grep -c` exits 1 with no match at all, which is the answer and not a failure.
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return nil, fmt.Errorf("git grep for %q at %s failed: %w (stderr: %s)", tag, rev, err, stderr.String())
	}

	perFile := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		// format: `<rev>:<file>:<n>`
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		n, cerr := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
		if cerr != nil {
			continue
		}
		perFile[strings.TrimPrefix(line[:idx], rev+":")] += n
	}
	return perFile, nil
}

// missingConsumerRoots returns the declared consumer roots absent under root.
//
// It exists because `git grep -- a b c` silently ignores a pathspec matching
// nothing: a root that moved would become less coverage under the same verdict.
func missingConsumerRoots(root string) []string {
	var missing []string
	for _, c := range consumerRoots {
		if _, err := os.Stat(filepath.Join(root, c.Repo, c.Path)); err != nil {
			missing = append(missing, c.String())
		}
	}
	return missing
}

// missingTrackedPaths returns the paths declared in trackedFiles that do not exist
// under root.
//
// Twin of missingConsumerRoots, and it exists because the asymmetry between the two WAS the
// hole: the CONSUMER list got an absence guard on 2026-08-19 (B-100/B-040) while the
// TRACKED list, handed to the very same `git diff --` and `git grep --`, got none.
//
// The missing half is the one that matters more. An absent consumer root still lets
// the gate see a tag drop; an absent tracked path means the tag never shows up as
// dropped at all, and the gate answers PASS over a contract surface it never read.
func missingTrackedPaths(root string) []string {
	var missing []string
	for _, f := range trackedFiles {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			missing = append(missing, f)
		}
	}
	if len(featurePaths(root)) == 0 {
		missing = append(missing, featureSurface)
	}
	return missing
}

// grepConsumersForTag runs `git grep` for the dropped tag in every present
// consumer root, looking for `.tag\b` access patterns (struct field references
// or JS object property access). Absent roots are skipped — Check reports them
// as MissingRoots. `git grep` exit 1 means "no match"; any other failure is an
// error, never zero findings.
func grepConsumersForTag(root, tag string, roots []consumerRoot) ([]Finding, error) {
	pattern := `\.` + regexp.QuoteMeta(tag) + `\b`
	var findings []Finding
	for _, c := range roots {
		repo := filepath.Join(root, c.Repo)
		if _, err := os.Stat(filepath.Join(repo, c.Path)); err != nil {
			continue
		}
		cmd := exec.Command("git", "-C", repo, "grep", "-nE", pattern, "--", c.Path)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				return nil, fmt.Errorf("git grep for %q in %s failed: %w (stderr: %s)",
					tag, c.String(), err, strings.TrimSpace(stderr.String()))
			}
		}
		findings = append(findings, parseGrepFindings(stdout.String(), tag, c.Repo)...)
	}
	return findings, nil
}

// parseGrepFindings turns `file:line:content` grep output into findings,
// skipping the declaration site itself and test files.
func parseGrepFindings(output, tag, repo string) []Finding {
	var findings []Finding
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		lineno, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		snippet := strings.TrimSpace(parts[2])
		// The struct field declaration `json:"<tag>"` itself matches `.<tag>`.
		if strings.Contains(snippet, fmt.Sprintf(`json:"%s"`, tag)) ||
			strings.Contains(snippet, fmt.Sprintf(`json:"%s,`, tag)) {
			continue
		}
		// Test files reference fields for assertion fixtures, not real reads.
		if isTestFile(parts[0]) {
			continue
		}
		findings = append(findings, Finding{
			DroppedTag:      tag,
			ConsumerFile:    filepath.ToSlash(filepath.Join(repo, parts[0])),
			ConsumerLine:    lineno,
			ConsumerSnippet: snippet,
		})
	}
	return findings
}

func isTestFile(path string) bool {
	for _, suf := range []string{"_test.go", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx"} {
		if strings.HasSuffix(path, suf) {
			return true
		}
	}
	return false
}

// ActionableFix returns the recommended remediation surface for FAIL output.
func ActionableFix() string {
	return strings.Join([]string{
		"Audit each consumer file listed above. For each:",
		"  (a) Update the consumer to use the new canonical field exposed by the contract change, OR",
		"  (b) Add the dropped field back to the struct (backward-compat additive completion).",
		"If the drop is intentional + consumers were already migrated in the same PR,",
		"verify the dashboard renders correctly via sponsor walkthrough BEFORE merging.",
	}, "\n  ")
}

// FormatFindings renders findings in a stable order for human + audit output.
func FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].DroppedTag != findings[j].DroppedTag {
			return findings[i].DroppedTag < findings[j].DroppedTag
		}
		if findings[i].ConsumerFile != findings[j].ConsumerFile {
			return findings[i].ConsumerFile < findings[j].ConsumerFile
		}
		return findings[i].ConsumerLine < findings[j].ConsumerLine
	})
	var buf strings.Builder
	currentTag := ""
	for _, f := range findings {
		if f.DroppedTag != currentTag {
			fmt.Fprintf(&buf, "\nDropped JSON tag: %q\n", f.DroppedTag)
			currentTag = f.DroppedTag
		}
		fmt.Fprintf(&buf, "  %s:%d  %s\n",
			filepath.ToSlash(f.ConsumerFile), f.ConsumerLine, f.ConsumerSnippet)
	}
	return buf.String()
}
