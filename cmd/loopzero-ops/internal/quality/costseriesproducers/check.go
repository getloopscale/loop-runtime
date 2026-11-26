// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package costseriesproducers implements the `cost-series-producers` gate.
//
// PROBLEM (B-181). loopzero-cloud's cost aggregator queries Prometheus for three
// `loopzero_*` series (internal/billing/cost_aggregator.go, the `promQueries`
// map). The producers that would emit those series live in THIS repository.
// Nobody joined the two sides. Measured 2026-09-05 against k3d-loopzero-core: all
// three queries returned an empty vector, Prometheus knew 48 `loopzero_*` series
// names, and none of the three was among them. The aggregator ran every five
// minutes against nothing and `cost_counters` stayed at zero by construction,
// with no error on either side -- the consumer logs only on query FAILURE, and
// an empty result is a success.
//
// WHAT THIS GATE ANSWERS. For every series the aggregator queries, exactly one
// of three things, named in the output:
//
//  1. the series is emitted by this repo -- the declaration names the producing
//     file, and the gate opens it and verifies the metric is declared there;
//  2. the series is emitted by NOBODY -- the defect. It is visible on every
//     run, and a NEW one (a series the aggregator queries that the declaration
//     does not list) FAILS;
//  3. the sibling repository is not on disk -- the gate says so, loudly, and
//     reports PARTIAL rather than PASS. It never claims a drift check it did
//     not perform.
//
// WHY IT IS NOT A SIBLING-STAT-AND-SKIP. B-169 removed a test that stat'd a
// sibling path, took an early return when the sibling was not cloned, and
// reported `ok` having verified nothing -- the skip branch was the only branch
// ever taken. This gate is split so that absence of the sibling costs one half
// of the verdict, not all of it:
//
//   - the PRODUCER CONTRACT (declaration <-> this repo's metric declarations)
//     is hermetic. It needs no sibling and runs everywhere, always.
//   - the DRIFT CHECK (declaration <-> loopzero-cloud's promQueries) needs the
//     sibling. When it cannot run, the outcome carries SiblingSkipReason and
//     the gate prints PARTIAL, naming what was not compared.
//
// And the drift code path is not dead when the sibling is missing in
// production: Check takes SiblingRoot as data, so the unit tests exercise
// present-and-matching, present-and-drifted, present-but-file-moved and
// absent against synthetic trees on every run. The branch that production may
// rarely take is the branch the suite takes every time.
//
// SCOPE. Only `loopzero_*` series are extracted from the queries. A query naming a
// series outside that namespace (a kube-state or cAdvisor metric, say) has no
// producer in this repository by definition, and asserting about it here would
// be asserting about a chart this gate does not read.
package costseriesproducers

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qgshell"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultDeclarationFile is the pinned contract, repo-relative. It sits beside
// api/internal/contract/testdata/cli-consumed-paths.txt, which pins the same
// KIND of fact for a different sibling: what another repository consumes from
// this one.
const DefaultDeclarationFile = "api/internal/contract/testdata/cost-aggregator-series.txt"

// DefaultAggregatorFile is the query source, relative to the sibling root.
const DefaultAggregatorFile = "internal/billing/cost_aggregator.go"

// DefaultSiblingDir is where loopzero-cloud is expected relative to the repo root.
const DefaultSiblingDir = "../loopzero-cloud"

// Config drives the gate.
type Config struct {
	// RepoRoot is this repository's root. Defaults to ".".
	RepoRoot string
	// SiblingRoot is loopzero-cloud's root. Defaults to RepoRoot/../loopzero-cloud.
	// May legitimately not exist -- see the package doc.
	SiblingRoot string
	// DeclarationFile overrides DefaultDeclarationFile (repo-relative).
	DeclarationFile string
	// AggregatorFile overrides DefaultAggregatorFile (sibling-relative).
	AggregatorFile string
}

// Declaration is one pinned line: a series the aggregator queries, plus who
// emits it -- a file in this repo, or nobody under a tracked work item.
type Declaration struct {
	Series   string
	Producer string // repo-relative path; empty for a tracked absence
	GapItem  string // work item; empty when Producer is set
	Line     int
}

// TrackedAbsence reports whether this line declares that nobody emits Series.
func (d Declaration) TrackedAbsence() bool { return d.Producer == "" }

// Outcome is the verdict. Pass covers the whole gate; SiblingChecked says
// whether the drift half of it actually ran.
type Outcome struct {
	Pass bool

	Declarations      []Declaration
	VerifiedProducers []string // "<series> <- <path>"
	TrackedAbsences   []string // "<series> (<item>)"
	Violations        []string

	// SiblingRoot is the path that was looked for, absolute where resolvable.
	SiblingRoot string
	// SiblingChecked is true only when the declaration was actually compared
	// against the aggregator's promQueries map.
	SiblingChecked bool
	// SiblingSkipReason names why it was not, and is non-empty exactly when
	// SiblingChecked is false. It is the gate's "I could not check this".
	SiblingSkipReason string
	// QueriedSeries is what the aggregator was observed to query. Empty when
	// SiblingChecked is false -- nothing was observed.
	QueriedSeries []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

var (
	// metricDeclRe matches a prometheus Opts metric name declaration, which is
	// how every producer in this repo names its series:
	//     Name:    "loopzero_build_duration_seconds",
	metricDeclRe = regexp.MustCompile(`Name:\s*"(loopzero_[a-zA-Z0-9_]+)"`)
	// seriesRefRe matches a loopzero_* series identifier inside a PromQL string.
	seriesRefRe = regexp.MustCompile(`loopzero_[a-zA-Z0-9_]+`)
	// declLineRe parses one pinned line.
	declLineRe = regexp.MustCompile(`^(\S+)\s+(PRODUCER|ABSENT)\s+(\S+)$`)
)

// derivedSuffixes are the series Prometheus synthesises from a histogram or
// summary. `loopzero_build_duration_seconds_sum` has no declaration of its own;
// its producer declares `loopzero_build_duration_seconds`.
//
// `_total` is deliberately NOT here: it is part of the declared name of a
// counter in this repo (loopzero_builds_total), so stripping it would look up a
// metric that does not exist and report a producer as missing.
var derivedSuffixes = []string{"_sum", "_count", "_bucket"}

// producerKey maps a queried series to the metric name a producer declares.
func producerKey(series string) string {
	for _, suffix := range derivedSuffixes {
		if strings.HasSuffix(series, suffix) {
			return strings.TrimSuffix(series, suffix)
		}
	}
	return series
}

// errNoDeclarations guards against a pin that parses but pins nothing: an
// empty file would let every check below iterate zero times and report a
// clean verdict over an empty population.
var errNoDeclarations = errors.New("declaration file lists no series -- a gate over an empty population reports PASS while asserting nothing")

// Check runs the gate. It returns an error only for a broken SETUP (an
// unreadable or malformed declaration); a contract that is violated comes back
// as Outcome.Violations with Pass false.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	declFile := cfg.DeclarationFile
	if declFile == "" {
		declFile = DefaultDeclarationFile
	}
	aggFile := cfg.AggregatorFile
	if aggFile == "" {
		aggFile = DefaultAggregatorFile
	}
	sibling := cfg.SiblingRoot
	if sibling == "" {
		sibling = filepath.Join(root, DefaultSiblingDir)
	}

	decls, err := parseDeclarations(filepath.Join(root, declFile))
	if err != nil {
		return nil, err
	}

	out := &Outcome{Declarations: decls, SiblingRoot: absOrRaw(sibling)}

	emitters, err := scanEmitters(root)
	if err != nil {
		return nil, fmt.Errorf("scanning %s for metric declarations: %w", root, err)
	}

	checkProducers(out, root, emitters, declFile)
	checkDrift(out, sibling, aggFile, declFile)

	out.Pass = len(out.Violations) == 0
	return out, nil
}

// checkProducers is the hermetic half: every pinned series is either emitted
// by the file it names, or emitted by nobody under a tracked item.
func checkProducers(out *Outcome, root string, emitters map[string]string, declFile string) {
	for _, d := range out.Declarations {
		key := producerKey(d.Series)
		emitter, emitted := emitters[key]

		if d.TrackedAbsence() {
			if emitted {
				out.Violations = append(out.Violations, fmt.Sprintf(
					"%s:%d: %s is declared ABSENT (%s) but this repo NOW emits it, at %s -- "+
						"the gap closed; replace that line with `%s PRODUCER %s`",
					declFile, d.Line, d.Series, d.GapItem, emitter, d.Series, emitter))
				continue
			}
			out.TrackedAbsences = append(out.TrackedAbsences,
				fmt.Sprintf("%s (%s)", d.Series, d.GapItem))
			continue
		}

		// #nosec G304 -- d.Producer is a repo-relative path read from the pinned
		// declaration and joined under root; opening it IS the check.
		data, err := os.ReadFile(filepath.Join(root, d.Producer))
		if err != nil {
			out.Violations = append(out.Violations, fmt.Sprintf(
				"%s:%d: %s names producer %s, which cannot be read (%v) -- "+
					"the file moved or was deleted, and the declaration did not follow",
				declFile, d.Line, d.Series, d.Producer, err))
			continue
		}
		if !declaresMetric(data, key) {
			out.Violations = append(out.Violations, fmt.Sprintf(
				"%s:%d: %s names producer %s, but that file declares no `Name: %q` -- "+
					"a producer column that is not verified is prose, not a contract",
				declFile, d.Line, d.Series, d.Producer, key))
			continue
		}
		out.VerifiedProducers = append(out.VerifiedProducers,
			fmt.Sprintf("%s <- %s", d.Series, d.Producer))
	}
}

// checkDrift is the half that needs the sibling. It never reports a clean
// drift result it did not observe: SiblingChecked stays false and
// SiblingSkipReason says what stopped it.
func checkDrift(out *Outcome, sibling, aggFile, declFile string) {
	info, err := os.Stat(sibling)
	if err != nil || !info.IsDir() {
		out.SiblingSkipReason = fmt.Sprintf(
			"loopzero-cloud is not on disk at %s, so the declaration was NOT compared against "+
				"its promQueries map. A series added to the aggregator since this file was "+
				"last frozen would not be seen by this run. Point the gate at the sibling "+
				"with --loopzero-cloud=<path> or LOOPZERO_CLOUD_ROOT=<path>.", out.SiblingRoot)
		return
	}

	aggPath := filepath.Join(sibling, aggFile)
	// #nosec G304 -- aggPath is the operator-supplied sibling root joined with the
	// configured aggregator path; reading it is the gate's whole purpose.
	data, err := os.ReadFile(aggPath)
	if err != nil {
		// The sibling IS here and the query source is not where it was. That is
		// an observation, not an absence, so it fails rather than skips.
		out.Violations = append(out.Violations, fmt.Sprintf(
			"loopzero-cloud is present at %s but %s cannot be read (%v) -- the aggregator's "+
				"queries moved; re-point --aggregator or re-freeze %s",
			out.SiblingRoot, aggFile, err, declFile))
		out.SiblingSkipReason = "the aggregator source could not be read (reported as a violation above)"
		return
	}

	block, ok := promQueriesBlock(string(data))
	if !ok {
		out.Violations = append(out.Violations, fmt.Sprintf(
			"%s has no `var promQueries` map -- the aggregator's query surface was renamed or "+
				"restructured, and this gate can no longer see what it queries", aggPath))
		out.SiblingSkipReason = "the promQueries map was not found (reported as a violation above)"
		return
	}

	queried := seriesIn(block)
	if len(queried) == 0 {
		// Floor. An extractor that matches nothing gives a green run that proves
		// the opposite of what it looks like.
		out.Violations = append(out.Violations, fmt.Sprintf(
			"the promQueries map in %s names no loopzero_* series -- either the map is empty or "+
				"this gate's extractor no longer matches its shape; a silent zero here would "+
				"turn every drift check into a no-op", aggPath))
		out.SiblingSkipReason = "the promQueries map yielded no series (reported as a violation above)"
		return
	}

	out.SiblingChecked = true
	out.QueriedSeries = queried

	declared := make(map[string]Declaration, len(out.Declarations))
	for _, d := range out.Declarations {
		declared[d.Series] = d
	}
	for _, s := range queried {
		if _, ok := declared[s]; !ok {
			out.Violations = append(out.Violations, fmt.Sprintf(
				"the aggregator queries %s, which %s does not declare -- add a line saying who "+
					"emits it (`PRODUCER <path>`) or that nobody does (`ABSENT <work item>`)",
				s, declFile))
		}
	}
	queriedSet := make(map[string]bool, len(queried))
	for _, s := range queried {
		queriedSet[s] = true
	}
	for _, d := range out.Declarations {
		if !queriedSet[d.Series] {
			out.Violations = append(out.Violations, fmt.Sprintf(
				"%s:%d declares %s, which the aggregator no longer queries -- remove the stale "+
					"line so the pin keeps meaning what it says",
				declFile, d.Line, d.Series))
		}
	}
}

// parseDeclarations reads the pinned contract.
func parseDeclarations(path string) ([]Declaration, error) {
	// #nosec G304 -- path is the declaration file the gate is configured to read.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading the pinned declaration %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var decls []Declaration
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := declLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%s:%d: cannot parse %q -- expected `<series> PRODUCER <path>` or `<series> ABSENT <work item>`", path, n, line)
		}
		if prev, dup := seen[m[1]]; dup {
			return nil, fmt.Errorf("%s:%d: %s is declared twice (first at line %d) -- one series, one answer", path, n, m[1], prev)
		}
		seen[m[1]] = n
		d := Declaration{Series: m[1], Line: n}
		if m[2] == "PRODUCER" {
			d.Producer = m[3]
		} else {
			d.GapItem = m[3]
		}
		decls = append(decls, d)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(decls) == 0 {
		return nil, fmt.Errorf("%s: %w", path, errNoDeclarations)
	}
	return decls, nil
}

// scanEmitters maps every `loopzero_*` metric declared in this repo's non-test Go
// sources to the file declaring it. Test files are excluded on purpose: a
// metric named only in a _test.go is not a producer, and counting one would
// report a tracked absence as closed.
func scanEmitters(root string) (map[string]string, error) {
	emitters := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", ".cache":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// #nosec G304,G122 -- path comes from filepath.WalkDir of the repo root; this is a
		// read-only scan of tracked sources, so there is nothing for a TOCTOU race to win.
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			// The unreadable file may be the one that declares a pinned metric.
			return fmt.Errorf("%s: unreadable, so its metric declarations are unknown: %w", path, rerr)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		for _, m := range metricDeclRe.FindAllStringSubmatch(string(data), -1) {
			if _, dup := emitters[m[1]]; !dup {
				emitters[m[1]] = rel
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return emitters, nil
}

// declaresMetric reports whether src declares the named metric.
func declaresMetric(src []byte, name string) bool {
	for _, m := range metricDeclRe.FindAllSubmatch(src, -1) {
		if string(m[1]) == name {
			return true
		}
	}
	return false
}

// promQueriesBlock returns the body of `var promQueries = map[...]{ ... }`.
// Brace counting rather than a line-shape guess: the map is a composite
// literal, and its closing brace is the one that balances the opening.
func promQueriesBlock(src string) (string, bool) {
	return qgshell.ExtractBraceBlock(src, promQueriesOpenRe)
}

// promQueriesOpenRe opens the `var promQueries = map[...]...{` literal.
var promQueriesOpenRe = regexp.MustCompile(`var promQueries\b[^{]*\{`)

// seriesIn returns the sorted, de-duplicated loopzero_* series named in a PromQL
// fragment.
func seriesIn(block string) []string {
	set := map[string]bool{}
	for _, m := range seriesRefRe.FindAllString(block, -1) {
		set[m] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func absOrRaw(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
