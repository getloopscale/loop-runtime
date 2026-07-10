// Package testcoveragereport is a hermetic coverage gate for infra/tests/ Go
// test packages. Runs `go test -cover` against a configurable scope + threshold
// and reports PASS/FAIL/SKIP per M9 canonical shape.
//
// M23 T5.1 (F22): closes the "coverage_pct not measured" finding from the
// 2026-07-10 code-review audit. Follows M16 canonical shape (Config struct +
// Check function + Outcome + advisory-fallthrough) — direct sibling of
// orykratospublicurls + esochartsanity per plan v1.1 blueprint Q5 dual exemplar.
package testcoveragereport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultCoverageThreshold is the initial coverage floor. Conservative to
// avoid false-fails on first activation; adjust per project maturity.
const DefaultCoverageThreshold = 30.0

// DefaultScopes are the packages measured when Config.ScopeGlobs is empty.
// Excludes e2e/livetest (require LIVE cluster) and load/ (concentrates on
// large-scale benchmarks that pull entire fixture trees).
var DefaultScopes = []string{
	"./infra/tests/unit/...",
	"./infra/tests/charts/...",
}

// Config controls the check.
type Config struct {
	RepoRoot          string
	CoverageThreshold float64  // 0-100; if 0, DefaultCoverageThreshold applies
	ScopeGlobs        []string // if empty, DefaultScopes applies
	BuildTags         string   // e.g., "helm_render"; empty = untagged
}

// Outcome is the aggregated result.
type Outcome struct {
	Pass           bool
	Skipped        bool
	CoveragePct    float64
	FailedPackages []string
	SkipReason     string
}

// ErrCoverageBelowThreshold signals aggregate coverage below floor.
var ErrCoverageBelowThreshold = errors.New("test coverage below threshold")

// ErrCoverageParseFailed signals unparseable `go test -json` output (EC-7).
var ErrCoverageParseFailed = errors.New("go test -json output parse failed")

// Check runs `go test -cover` for the configured scope and returns Outcome.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	// Advisory-fallthrough: go binary required.
	if _, err := exec.LookPath("go"); err != nil {
		return Outcome{
			Pass:       true,
			Skipped:    true,
			SkipReason: "go binary not on PATH — advisory-fallthrough",
		}, nil
	}

	threshold := cfg.CoverageThreshold
	if threshold <= 0 {
		threshold = DefaultCoverageThreshold
	}
	scopes := cfg.ScopeGlobs
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}

	// Build `go test -cover -json <scopes>` command
	args := []string{"test", "-cover", "-json", "-timeout", "5m"}
	if cfg.BuildTags != "" {
		args = append(args, "-tags", cfg.BuildTags)
	}
	args = append(args, scopes...)

	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Clean(root)
	stdout, err := cmd.Output()
	if err != nil {
		// Non-zero exit from `go test` may be legitimate (test failure); we still
		// want to parse the JSON to compute coverage. Continue with whatever output
		// was produced. Empty output = fatal.
		if len(stdout) == 0 {
			return Outcome{}, fmt.Errorf("go test -cover -json failed with no output: %w", err)
		}
	}

	pct, failedPkgs, parseErr := parseCoverageJSON(stdout)
	if parseErr != nil {
		return Outcome{}, fmt.Errorf("%w: %v", ErrCoverageParseFailed, parseErr)
	}

	pass := pct >= threshold && len(failedPkgs) == 0
	return Outcome{
		Pass:           pass,
		CoveragePct:    pct,
		FailedPackages: failedPkgs,
	}, nil
}

// goTestJSONEvent is a subset of `go test -json` event shape (see: go help
// testflag → -json for full schema).
type goTestJSONEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// parseCoverageJSON walks the `go test -json` event stream and computes an
// unweighted-mean coverage percentage across all packages that emitted a
// `coverage: N%` line. Failed packages (Action == "fail") are collected.
func parseCoverageJSON(raw []byte) (float64, []string, error) {
	if len(raw) == 0 {
		return 0, nil, errors.New("empty JSON stream")
	}
	failed := []string{}
	coverages := []float64{}

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt goTestJSONEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			// Skip malformed lines (e.g., partial output on crash) rather than fail
			// the whole parse — best-effort continues to next event.
			continue
		}
		if evt.Action == "fail" && evt.Package != "" {
			failed = append(failed, evt.Package)
		}
		if evt.Action == "output" && strings.Contains(evt.Output, "coverage:") {
			// Format: "coverage: 42.3% of statements"
			pct, ok := extractCoveragePct(evt.Output)
			if ok {
				coverages = append(coverages, pct)
			}
		}
	}

	if len(coverages) == 0 {
		return 0, failed, nil
	}
	var sum float64
	for _, c := range coverages {
		sum += c
	}
	return sum / float64(len(coverages)), failed, nil
}

// extractCoveragePct pulls the first "N.N%" from a "coverage: N.N% of ..."
// output line.
func extractCoveragePct(line string) (float64, bool) {
	idx := strings.Index(line, "coverage:")
	if idx < 0 {
		return 0, false
	}
	rest := line[idx+len("coverage:"):]
	rest = strings.TrimSpace(rest)
	// Find the % sign
	pctIdx := strings.Index(rest, "%")
	if pctIdx < 0 {
		return 0, false
	}
	pctStr := strings.TrimSpace(rest[:pctIdx])
	var pct float64
	_, err := fmt.Sscanf(pctStr, "%f", &pct)
	if err != nil {
		return 0, false
	}
	return pct, true
}
