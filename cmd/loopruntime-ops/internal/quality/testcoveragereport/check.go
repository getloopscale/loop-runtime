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

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// DefaultCoverageThreshold is the coverage floor.
//
// Raised 30% -> 75% on 2026-08-06. The 30% was set "conservative to avoid
// false-fails on first activation", back when the gate measured the wrong
// scope and reported 0.00%. Against the real scope the measurement is 85.33%,
// so a 30% floor could not fail on anything short of deleting most of the test
// suite — a gate that cannot go red is not a gate. 75% keeps ~10 points of
// headroom for normal movement while still catching a real regression.
//
// Honest limit: the figure is an UNWEIGHTED mean across packages, so a tiny
// 100% package offsets a large 40% one. It detects a broad regression, not a
// single under-tested hot spot. Weighting by statement count would need
// -coverprofile plus `go tool cover`, which is a bigger change than the defect
// being fixed here.
const DefaultCoverageThreshold = 75.0

// DefaultScopes are the packages measured when Config.ScopeGlobs is empty.
//
// They must hold PRODUCTION statements. The previous scopes were
// ./infra/tests/{unit,charts}/..., which are test-only packages: `go test
// -cover` reports "coverage: [no statements]" for them, the gate averaged that
// to 0.00%, and failed against its own floor — red for a reason unrelated to
// any code under test. That is why it sat quarantined as "measured broken".
//
// pkg/ (shared libraries) and cmd/loopruntime-ops/internal/quality/ (the gates
// themselves) are chosen because they are pure-logic and fast: 15s uncached,
// measured 2026-08-06. api/ and operators/ are deliberately out — they need a
// database and envtest control planes, which puts them minutes past the budget
// for something meant to run on a push.
var DefaultScopes = []string{
	"./pkg/...",
	"./cmd/loopruntime-ops/internal/quality/...",
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

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
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
		return Outcome{}, fmt.Errorf("%w: %w", ErrCoverageParseFailed, parseErr)
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
