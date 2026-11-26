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

// M10 T2.1 — consumer-driven contract (dashboard ↔ engine).
//
// The LoopRuntimeCloud dashboard (loopruntime-cloud/dashboard/src) is the real consumer of the
// engine REST contract: the loopruntime-cloud Go BFF is a TRANSPARENT reverse proxy
// (cmd/main.go `r.Route("/engine", ...)` strips the prefix and forwards), so it
// enumerates no engine paths of its own. These tests assert the engine's published
// spec (engine.yaml) covers every engine path the dashboard depends on, so a
// producer change that drops/renames a consumed route fails here BEFORE the
// dashboard breaks at runtime — the same low-coupling guarantee the cli pair gives.
//
// Reuses normalizePath / braceParm / cleanPath / specPathSetNormalized from
// cli_consumer_test.go + engine_conformance_test.go (DRY — one normalization,
// two consumers).

const (
	dashboardPathsFile = "testdata/dashboard-consumed-paths.txt"
	dashboardSrcDir    = "../../../../loopruntime-cloud/dashboard/src"
)

// enginePrefixedRE matches a /engine/api/v1/... literal (the proxied engine call
// shape the dashboard uses) up to a query/dynamic/quote boundary.
var enginePrefixedRE = regexp.MustCompile(`/engine/api/v1/[A-Za-z0-9/_${}().:?=&-]*`)

// knownDashboardGaps: engine paths the dashboard references that the engine does
// NOT register at HEAD. Documented, not silently hidden. Empty today (M10 T2.1
// added the 4 real-but-unspecced routes to engine.yaml via EC-1); kept for
// symmetry with cli_consumer_test.go's knownConsumerGaps.
var knownDashboardGaps = map[string]bool{}

func readDashboardPathList(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(dashboardPathsFile)
	if err != nil {
		t.Fatalf("open %s: %v", dashboardPathsFile, err)
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

// dashboardSrcPresent reports whether the sibling loopruntime-cloud dashboard tree is
// checked out next to this repo. When absent, the live drift guard SKIPs — the
// static presence check (TestDashboardConsumer_AllPathsInSpec) still runs.
func dashboardSrcPresent() bool {
	info, err := os.Stat(dashboardSrcDir)
	return err == nil && info.IsDir()
}

// isDashboardTestFixture reports whether a dashboard source file is a test/story/
// mock fixture (not production client code). Such files embed literal example URLs
// (my-app, abc) that are not real engine calls.
func isDashboardTestFixture(path string) bool {
	for _, marker := range []string{
		".test.", ".spec.", ".stories.", "/__tests__/", "/__mocks__/", "/test-utils/",
		"dev-mocks", "-mock.", ".mock.", "/mocks/",
	} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

// stripComments removes `//` line comments and `/* … */` block comments from TS/TSX
// source before the path regex runs.
//
// Without it, a line of PROSE that cites a route becomes a "call" to this guard.
// Measured 2026-08-10: `dashboard/src/main.tsx:41` documents a 2026-08-07
// measurement listing seven requests that escaped the interceptor — among them
// `/engine/api/v1/auth/whoami` — and the guard started demanding that route in the
// list of consumed paths. The dashboard calls it nowhere outside mocks and tests,
// which were already excluded per file (isDashboardTestFixture).
//
// Declaring the route in the list to silence the guard would have been worse than
// the false positive: it would assert a consumption that does not exist, and the
// contract would start lying in the direction nobody checks. A comment describing a
// bug is not the bug (B-008).
//
// The sweep is deliberately simple — it does not handle `//` inside a string
// literal, which could at most produce a false NEGATIVE on a commented-out literal
// URL. A guard that errs low is reviewable; one that errs high trains the team to
// silence it.
func stripComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		for len(line) > 0 {
			if inBlock {
				end := strings.Index(line, "*/")
				if end < 0 {
					line = ""
					break
				}
				line = line[end+2:]
				inBlock = false
				continue
			}
			lineIdx := strings.Index(line, "//")
			blockIdx := strings.Index(line, "/*")
			switch {
			case blockIdx >= 0 && (lineIdx < 0 || blockIdx < lineIdx):
				out.WriteString(line[:blockIdx])
				line = line[blockIdx+2:]
				inBlock = true
			case lineIdx >= 0:
				out.WriteString(line[:lineIdx])
				line = ""
			default:
				out.WriteString(line)
				line = ""
			}
		}
		out.WriteString("\n")
	}
	return out.String()
}

// liveDashboardEnginePaths greps dashboard/src for /engine/api/v1/... calls, strips
// the /engine proxy prefix, and returns the normalized set of engine paths.
func liveDashboardEnginePaths(t *testing.T) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	err := filepath.WalkDir(dashboardSrcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		// Scan PRODUCTION client code only. Test/story fixtures use literal example
		// values (my-app, abc, x) that normalizePath cannot collapse to {} and that
		// are not real API calls — excluding them keeps the drift guard honest.
		if isDashboardTestFixture(path) {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// Comments out BEFORE the regex: prose that cites a route is not a call to it.
		for _, m := range enginePrefixedRE.FindAllString(stripComments(string(src)), -1) {
			n := normalizePath(strings.TrimPrefix(m, "/engine"))
			if cleanPath.MatchString(n) {
				set[n] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk dashboard/src: %v", err)
	}
	if len(set) == 0 {
		t.Fatalf("no /engine/api/v1 paths found under %s — wrong path?", dashboardSrcDir)
	}
	return set
}

// (1) every dashboard-needed engine path is present in the engine spec.
func TestDashboardConsumer_AllPathsInSpec(t *testing.T) {
	want := readDashboardPathList(t)
	spec := specPathSetNormalized(t)
	var missing []string
	for p := range want {
		if !spec[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("engine.yaml is missing %d dashboard-consumed path(s): %v", len(missing), missing)
	}
}

// (2) drift guard: the checked-in list stays in sync with the dashboard source
// (engine paths only, minus known gaps). SKIPs when the sibling repo is absent.
func TestDashboardConsumer_ListMatchesSource(t *testing.T) {
	if !dashboardSrcPresent() {
		t.Skipf("dashboard sibling absent at %s — live drift guard skipped", dashboardSrcDir)
	}
	want := readDashboardPathList(t)
	live := liveDashboardEnginePaths(t)

	var surprises []string // consumed in source, not in list, not a known gap
	for p := range live {
		if !want[p] && !knownDashboardGaps[p] {
			surprises = append(surprises, p)
		}
	}
	sort.Strings(surprises)
	if len(surprises) > 0 {
		t.Errorf("dashboard/src calls %d engine path(s) not in %s (add to the list + engine.yaml): %v",
			len(surprises), dashboardPathsFile, surprises)
	}
}

// TestStripComments_ProseCitingARouteIsNotACall pins the B-008 false positive.
//
// The middle cases are the real one: `main.tsx` describes, in a comment, a
// measurement listing routes that escaped the interceptor. Before this cut, the
// guard read that prose as consumption and demanded the route in the declared list.
func TestStripComments_ProseCitingARouteIsNotACall(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool // must the route survive the cut?
	}{
		{"a real call", `fetch("/engine/api/v1/auth/whoami")`, true},
		{"line comment", `// escaped ("/engine/api/v1/auth/whoami")`, false},
		{"block comment", "/* see /engine/api/v1/auth/whoami */", false},
		{"multi-line block", "/*\n/engine/api/v1/auth/whoami\n*/", false},
		{"code after a block", "/* x */ fetch(\"/engine/api/v1/auth/whoami\")", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Contains(stripComments(c.src), "/engine/api/v1/auth/whoami")
			if got != c.want {
				t.Errorf("route survived=%v, want %v\nsource: %q\noutput: %q",
					got, c.want, c.src, stripComments(c.src))
			}
		})
	}
}
