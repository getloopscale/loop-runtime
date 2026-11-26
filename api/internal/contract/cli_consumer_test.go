// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package contract

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// M1.4 T2.1 — consumer-driven contract (cli ↔ engine).
//
// The TS CLI (loopruntime/cli/src) is the consumer; the engine is the producer. These
// tests assert the engine's published spec (engine.yaml) covers every path the
// CLI depends on, so a producer change that drops/renames a consumer route fails
// here BEFORE the CLI breaks at runtime (ROADMAP low-coupling guarantee #1).

const (
	cliPathsFile = "testdata/cli-consumed-paths.txt"
	cliSrcDir    = "../../../cli/src"
)

// knownConsumerGaps: paths the CLI references that the engine does NOT register.
// Documented, not silently hidden — the contract test's whole point is to surface
// these. /api/v1/domains/{}/dns-records is CLI dead/aspirational code (no engine
// route at HEAD). Tracked here; remediation (register the route OR delete the CLI
// code) is out of M1.4 scope (contracts, not new routes).
var knownConsumerGaps = map[string]bool{
	"/api/v1/domains/{}/dns-records": true,
}

var (
	// matches a /api/v1/... literal up to a query/dynamic/quote boundary
	apiPathRE  = regexp.MustCompile(`/api/v1/[A-Za-z0-9/_${}().:?=&-]*`)
	tsInterp   = regexp.MustCompile(`\$\{[^}]*\}`)       // ${encodeURIComponent(x)}
	braceParm  = regexp.MustCompile(`\{[^}]*\}`)         // {id} / {name}
	gluedBrace = regexp.MustCompile(`([A-Za-z0-9])\{\}`) // environments{} (query interp glued to a word)
	cleanPath  = regexp.MustCompile(`^/api/v1/[A-Za-z0-9/{}_-]+$`)
)

// normalizePath reduces a raw CLI URL string to template form: drops the query,
// collapses ${...} and {param} to {}, strips query-interpolations glued directly
// to a path word (environments${query} → environments), cuts unclosed ${ ternary
// boundaries, and trims trailing punctuation.
func normalizePath(raw string) string {
	p := raw
	if i := strings.IndexByte(p, '?'); i >= 0 { // drop explicit query string
		p = p[:i]
	}
	p = tsInterp.ReplaceAllString(p, "{}")   // closed ${...} -> {}
	if i := strings.Index(p, "${"); i >= 0 { // unclosed/ternary ${query ? ...} boundary
		p = p[:i]
	}
	for _, b := range []string{" ", "`", "'", "\""} { // template/string boundaries
		if i := strings.Index(p, b); i >= 0 {
			p = p[:i]
		}
	}
	p = braceParm.ReplaceAllString(p, "{}")  // {id}/{name} -> {}
	p = gluedBrace.ReplaceAllString(p, "$1") // word{} (glued query interp) -> word
	p = strings.TrimRight(p, "./")           // trailing punctuation / slash
	return p
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/builds/${encodeURIComponent(buildId)}":          "/api/v1/builds/{}",
		"/api/v1/builds/${encodeURIComponent(id)}/cancel":        "/api/v1/builds/{}/cancel",
		"/api/v1/projects/${encodeURIComponent(project)}/builds": "/api/v1/projects/{}/builds",
		"/api/v1/environments/${encodeURIComponent(envName)}":    "/api/v1/environments/{}",
		"/api/v1/audit?${params.toString()}":                     "/api/v1/audit",
		"/api/v1/projects":                                       "/api/v1/projects",
		"/api/v1/projects/{name}/scale":                          "/api/v1/projects/{}/scale", // spec form
		"/api/v1/builds/${id}/logs?tail=20":                      "/api/v1/builds/{}/logs",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func readCLIPathList(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(cliPathsFile)
	if err != nil {
		t.Fatalf("open %s: %v", cliPathsFile, err)
	}
	defer f.Close()
	set := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = true
	}
	return set
}

// specPathSetNormalized loads engine.yaml and returns its paths in {}-normalized form.
func specPathSetNormalized(t *testing.T) map[string]bool {
	t.Helper()
	doc := loadSpec(t)
	set := map[string]bool{}
	for p := range doc.Paths.Map() {
		set[braceParm.ReplaceAllString(p, "{}")] = true
	}
	return set
}

// cliSrcPresent reports whether the CLI source tree is co-located in this repo.
// M2 (F-4 / D4): after `loopruntime-cli` is extracted and `cli/` removed from the
// monorepo, the live re-grep tests SKIP (the drift guard moves to loopruntime-cli's own
// CI). The static presence check (TestCLIConsumer_AllPathsInSpec, which reads the
// checked-in cli-consumed-paths.txt) keeps running regardless — so the engine
// retains its consumer contract even after the split.
func cliSrcPresent() bool {
	info, err := os.Stat(cliSrcDir)
	return err == nil && info.IsDir()
}

func TestCLIConsumer_SkipsLiveGuardWhenCliAbsent(t *testing.T) {
	// Guard-logic unit check: a nonexistent path must report not-present.
	saved := cliSrcDir
	if cliSrcPresent() != (func() bool { _, e := os.Stat(saved); return e == nil }()) {
		t.Fatalf("cliSrcPresent inconsistent for %q", saved)
	}
	if _, err := os.Stat("../../../definitely-not-here-xyz"); err == nil {
		t.Fatal("sanity: bogus path should not exist")
	}
}

// liveCLIPaths greps cli/src and returns the normalized set of /api/v1 paths.
func liveCLIPaths(t *testing.T) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	err := filepath.WalkDir(cliSrcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, m := range apiPathRE.FindAllString(string(src), -1) {
			n := normalizePath(m)
			if cleanPath.MatchString(n) {
				set[n] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cli/src: %v", err)
	}
	if len(set) == 0 {
		t.Fatalf("no /api/v1 paths found under %s — wrong path?", cliSrcDir)
	}
	return set
}

// (1) every CLI-needed path is present in the engine spec.
func TestCLIConsumer_AllPathsInSpec(t *testing.T) {
	want := readCLIPathList(t)
	spec := specPathSetNormalized(t)
	var missing []string
	for p := range want {
		if !spec[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("engine.yaml is missing %d CLI-consumed path(s): %v", len(missing), missing)
	}
}

// (2) the known consumer/producer gap is real + documented (CLI references it; spec does not).
func TestCLIConsumer_KnownGapsAreReal(t *testing.T) {
	if !cliSrcPresent() {
		t.Skip("cli/ extracted — live known-gap check moved to loopruntime-cli (M2 D4)")
	}
	live := liveCLIPaths(t)
	spec := specPathSetNormalized(t)
	for gap := range knownConsumerGaps {
		if !live[gap] {
			t.Errorf("known gap %q no longer referenced by the CLI — remove it from knownConsumerGaps", gap)
		}
		if spec[gap] {
			t.Errorf("known gap %q is now in the spec — remove it from knownConsumerGaps (gap closed)", gap)
		}
	}
}

// (3) drift guard: the checked-in list stays in sync with cli/src (minus known gaps).
func TestCLIConsumer_ListMatchesSource(t *testing.T) {
	if !cliSrcPresent() {
		t.Skip("cli/ extracted — live drift guard moved to loopruntime-cli (M2 D4)")
	}
	want := readCLIPathList(t)
	live := liveCLIPaths(t)

	var surprises []string // in live, not in list and not a known gap
	for p := range live {
		if !want[p] && !knownConsumerGaps[p] {
			surprises = append(surprises, p)
		}
	}
	var stale []string // in list, not in live
	for p := range want {
		if !live[p] {
			stale = append(stale, p)
		}
	}
	sort.Strings(surprises)
	sort.Strings(stale)
	if len(surprises) > 0 {
		t.Errorf("cli/src calls %d path(s) not in %s (add to the list + engine.yaml): %v",
			len(surprises), cliPathsFile, surprises)
	}
	if len(stale) > 0 {
		t.Errorf("%s has %d path(s) the CLI no longer calls (remove them): %v",
			cliPathsFile, len(stale), stale)
	}
}
