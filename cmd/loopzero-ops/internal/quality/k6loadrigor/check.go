// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package k6loadrigor is a hermetic structural test for the k6 load-harness
// rigor fixes (plan k6-load-test-rigor-fixes). Pure text-presence + YAML-syntax
// checks over local files — NO cluster, NO network, NO k6 run.
//
// Asserts the review findings are closed:
//  1. dropped_iterations threshold present in lib/thresholds.js `base`
//     (count<, since dropped_iterations is a k6 Counter) — EC-2.
//  2. testrun-l5-distributed.yaml exists, is valid YAML (EC-1), has the
//     TestRun shape, a pinned image, Prometheus RW and a secretKeyRef; the
//     k6:distributed:l5 task applies it.
//  3. README documents the per-level runner path.
//
// Whether this gate is wired into quality:gates is not asserted here: a gate
// cannot meaningfully check its own registration, and the wiring is owned by
// the registry and TestQualityGatesWiring.
//
// Migrated from infra/tests/check-k6-load-rigor_test.sh
// (M-Migrate2 continuation 2026-07-07).
package k6loadrigor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// ErrViolations means at least one assertion failed.
var ErrViolations = errors.New("k6 load-harness rigor invariants violated")

// Assertion targets.
const (
	thresholdsPath = "infra/tests/performance/k6/scripts/lib/thresholds.js"
	l5ManifestPath = "infra/tests/performance/k6/manifests/testrun-l5-distributed.yaml"
	testfilePath   = "taskfiles/test.yml"
	readmePath     = "infra/tests/performance/k6/README.md"
)

// phase checks the body of one file and returns its failures.
type phase struct {
	tag   string // "[1]", "[2]", ...
	path  string
	name  string // how a missing file is described
	check func(body string) []string
}

var phases = []phase{
	{"[1]", thresholdsPath, "thresholds.js", checkThresholds},
	{"[2]", l5ManifestPath, "testrun-l5-distributed.yaml", checkManifest},
	{"[2]", testfilePath, "taskfiles/test.yml", checkTaskfile},
	{"[3]", readmePath, "k6 README", checkReadme},
}

var (
	droppedCountRe  = regexp.MustCompile(`dropped_iterations.*count<`)
	inlineSecretRe  = regexp.MustCompile(`value: *"?[a-f0-9]{32,}"?`)
	perLevelRe      = regexp.MustCompile(`(?i)which runner per level|runner per level|per-level`)
	singlePodRe     = regexp.MustCompile(`(?i)single.pod|single pod`)
	parallelismRe   = regexp.MustCompile(`parallelism: *[0-9]+`)
	manifestNeedles = []struct{ desc, needle string }{
		{"kind: TestRun", "kind: TestRun"},
		{"apiVersion k6.io/v1alpha1", "apiVersion: k6.io/v1alpha1"},
		{"pinned grafana/k6:0.52.0", "image: grafana/k6:0.52.0"},
		{"Prometheus remote-write", "K6_PROMETHEUS_RW_SERVER_URL"},
		{"secretKeyRef (no inline secret)", "secretKeyRef"},
		{"references l5-multicell.js", "file: scripts/l5-multicell.js"},
	}
)

// Check runs every phase.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	var failures []string
	for _, p := range phases {
		body, err := os.ReadFile(filepath.Join(root, p.path)) // #nosec G304 -- fixed paths under the repo root
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s %s missing at %s: %v", p.tag, p.name, p.path, err))
			continue
		}
		for _, f := range p.check(string(body)) {
			failures = append(failures, p.tag+" "+f)
		}
	}
	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

func checkThresholds(body string) []string {
	var out []string
	if !strings.Contains(body, "dropped_iterations") {
		out = append(out, "dropped_iterations missing in thresholds.js")
	}
	if !isDroppedInBase(body) {
		out = append(out, "dropped_iterations not in `base` block (must apply to all levels — before `perLevel`)")
	}
	if !droppedCountRe.MatchString(body) {
		out = append(out, "threshold not `count<` (EC-2: dropped_iterations is a Counter; `rate<` would be a silent no-op)")
	}
	return out
}

func checkManifest(body string) []string {
	var out []string
	if err := validYAML(body); err != nil {
		out = append(out, fmt.Sprintf("L5 manifest is NOT valid YAML (EC-1): %v", err))
	}
	for _, m := range manifestNeedles {
		if !strings.Contains(body, m.needle) {
			out = append(out, "L5 manifest missing: "+m.desc)
		}
	}
	if !parallelismRe.MatchString(body) {
		out = append(out, "L5 manifest missing: parallelism set")
	}
	if inlineSecretRe.MatchString(body) {
		out = append(out, "L5 manifest contains inline secret-looking value")
	}
	return out
}

func checkTaskfile(body string) []string {
	var out []string
	if !strings.Contains(body, "k6:distributed:l5:") {
		out = append(out, "k6:distributed:l5 task not defined in taskfiles/test.yml")
	}
	if !strings.Contains(body, "testrun-l5-distributed.yaml") {
		out = append(out, "k6:distributed:l5 task does not apply the L5 manifest")
	}
	return out
}

func checkReadme(body string) []string {
	var out []string
	if !perLevelRe.MatchString(body) {
		out = append(out, "README missing per-level path section")
	}
	if !strings.Contains(strings.ToLower(body), "distributed") || !singlePodRe.MatchString(body) {
		out = append(out, "README does not name both single-pod and distributed paths")
	}
	return out
}

// isDroppedInBase returns true when `dropped_iterations` appears inside the
// `export const base` block, i.e. BEFORE the `export const perLevel` marker.
func isDroppedInBase(body string) bool {
	inBase := false
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "export const base") {
			inBase = true
			continue
		}
		if strings.Contains(line, "export const perLevel") {
			inBase = false
			continue
		}
		if inBase && strings.Contains(line, "dropped_iterations") {
			return true
		}
	}
	return false
}

// validYAML decodes every document in body, returning the first error.
func validYAML(body string) error {
	dec := yaml.NewDecoder(strings.NewReader(body))
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
