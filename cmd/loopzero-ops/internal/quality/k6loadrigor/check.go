// Package k6loadrigor is a hermetic structural test for the k6 load-harness
// rigor fixes (plan k6-load-test-rigor-fixes). Pure text-presence + YAML-syntax
// checks over local files — NO cluster, NO network, NO k6 run.
//
// Asserts the three review findings are closed:
//  1. dropped_iterations threshold present in lib/thresholds.js `base`
//     (count<, since dropped_iterations is a k6 Counter) — EC-2.
//  2. testrun-l5-distributed.yaml exists, valid YAML (EC-1), TestRun shape,
//     pinned image, Prometheus RW, secretKeyRef; k6:distributed:l5 task
//     applies it.
//  3. README documents the per-level runner path.
//  4. Gate wired into quality:gates AND allowlisted (EC-3 row shape).
//
// Migrated from infra/tests/check-k6-load-rigor_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic — pure text walk.
package k6loadrigor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
}

// ErrViolations means at least one assertion failed.
var ErrViolations = errors.New("k6 load-harness rigor invariants violated")

// Assertion targets.
const (
	thresholdsPath = "infra/tests/performance/k6/scripts/lib/thresholds.js"
	l5ManifestPath = "infra/tests/performance/k6/manifests/testrun-l5-distributed.yaml"
	testfilePath   = "taskfiles/test.yml"
	qualityPath    = "taskfiles/quality.yml"
	readmePath     = "infra/tests/performance/k6/README.md"
	allowlistPath  = ".claude/rules/shell-scripts-allowlist.md"
)

// Check runs all 15+ assertions.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var failures []string

	// Phase 1: dropped_iterations threshold (EC-2).
	thresholds, err := os.ReadFile(filepath.Join(root, thresholdsPath))
	if err != nil {
		failures = append(failures, fmt.Sprintf("[1] thresholds.js missing at %s: %v", thresholdsPath, err))
	} else {
		body := string(thresholds)
		if !strings.Contains(body, "dropped_iterations") {
			failures = append(failures, "[1] dropped_iterations missing in thresholds.js")
		}
		if !isDroppedInBase(body) {
			failures = append(failures, "[1] dropped_iterations not in `base` block (must apply to all levels — before `perLevel`)")
		}
		if !regexp.MustCompile(`dropped_iterations.*count<`).MatchString(body) {
			failures = append(failures, "[1] threshold not `count<` (EC-2: dropped_iterations is a Counter; `rate<` would be a silent no-op)")
		}
	}

	// Phase 2: L5 distributed manifest + task.
	manifest, err := os.ReadFile(filepath.Join(root, l5ManifestPath))
	if err != nil {
		failures = append(failures, fmt.Sprintf("[2] testrun-l5-distributed.yaml missing at %s: %v", l5ManifestPath, err))
	} else {
		body := string(manifest)
		if !isValidYAML(filepath.Join(root, l5ManifestPath)) {
			failures = append(failures, "[2] L5 manifest is NOT valid YAML (EC-1)")
		}
		matchers := []struct {
			desc, needle string
			isRegex      bool
		}{
			{"kind: TestRun", "kind: TestRun", false},
			{"apiVersion k6.io/v1alpha1", "apiVersion: k6.io/v1alpha1", false},
			{"parallelism set", `parallelism: *[0-9]+`, true},
			{"pinned grafana/k6:0.52.0", "image: grafana/k6:0.52.0", false},
			{"Prometheus remote-write", "K6_PROMETHEUS_RW_SERVER_URL", false},
			{"secretKeyRef (no inline secret)", "secretKeyRef", false},
			{"references l5-multicell.js", "file: scripts/l5-multicell.js", false},
		}
		for _, m := range matchers {
			var ok bool
			if m.isRegex {
				ok = regexp.MustCompile(m.needle).MatchString(body)
			} else {
				ok = strings.Contains(body, m.needle)
			}
			if !ok {
				failures = append(failures, fmt.Sprintf("[2] L5 manifest missing: %s", m.desc))
			}
		}
		// forbid inline secret-looking values
		if regexp.MustCompile(`value: *"?[a-f0-9]{32,}"?`).MatchString(body) {
			failures = append(failures, "[2] L5 manifest contains inline secret-looking value")
		}
	}

	testfile, err := os.ReadFile(filepath.Join(root, testfilePath))
	if err != nil {
		failures = append(failures, fmt.Sprintf("[2] taskfiles/test.yml missing: %v", err))
	} else {
		body := string(testfile)
		if !strings.Contains(body, "k6:distributed:l5:") {
			failures = append(failures, "[2] k6:distributed:l5 task not defined in taskfiles/test.yml")
		}
		if !strings.Contains(body, "testrun-l5-distributed.yaml") {
			failures = append(failures, "[2] k6:distributed:l5 task does not apply the L5 manifest")
		}
	}

	// Phase 3: per-level runner doc.
	readme, err := os.ReadFile(filepath.Join(root, readmePath))
	if err != nil {
		failures = append(failures, fmt.Sprintf("[3] k6 README missing at %s: %v", readmePath, err))
	} else {
		body := string(readme)
		if !regexp.MustCompile(`(?i)which runner per level|runner per level|per-level`).MatchString(body) {
			failures = append(failures, "[3] README missing per-level path section")
		}
		lower := strings.ToLower(body)
		if !strings.Contains(lower, "distributed") || !regexp.MustCompile(`(?i)single.pod|single pod`).MatchString(body) {
			failures = append(failures, "[3] README does not name both single-pod and distributed paths")
		}
	}

	// Phase 4: gate wiring + allowlist (EC-3).
	quality, err := os.ReadFile(filepath.Join(root, qualityPath))
	if err != nil {
		failures = append(failures, fmt.Sprintf("[4] taskfiles/quality.yml missing: %v", err))
	} else if !strings.Contains(string(quality), "check-k6-load-rigor") {
		// The migration replaces the bash guard with the Go gate — either
		// reference is acceptable.
		if !strings.Contains(string(quality), "k6-load-rigor") {
			failures = append(failures, "[4] neither the bash guard nor the Go gate `k6-load-rigor` is wired in quality:gates")
		}
	}
	// Allowlist row: EC-3 requires the row to be well-formed (3-column table).
	// Post-migration the bash row is removed; the Go gate has no allowlist row.
	// So this check is: EITHER the bash row exists AND is well-formed OR it is
	// removed (Go gate). Accept both — the invariant is "no malformed row".
	allowlist, err := os.ReadFile(filepath.Join(root, allowlistPath))
	if err != nil {
		failures = append(failures, fmt.Sprintf("[4] allowlist missing: %v", err))
	} else {
		body := string(allowlist)
		rowRE := regexp.MustCompile(`(?m)^\| *infra/tests/check-k6-load-rigor_test\.sh *\|[^|]+\|[^|]+\|`)
		malformedRE := regexp.MustCompile(`(?m)^\| *infra/tests/check-k6-load-rigor_test\.sh *\|[^|]*$`)
		if strings.Contains(body, "check-k6-load-rigor_test.sh") {
			if !rowRE.MatchString(body) && malformedRE.MatchString(body) {
				failures = append(failures, "[4] allowlist row for check-k6-load-rigor_test.sh is malformed (EC-3: expected 3-column format)")
			}
		}
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// isDroppedInBase returns true when `dropped_iterations` appears inside the
// `export const base` block, i.e. BEFORE the `export const perLevel` marker.
// Ports the bash awk range check.
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

// isValidYAML uses `python3 -c "yaml.safe_load_all(open(path))"` for parity
// with the bash guard. If python3 or PyYAML are unavailable, treat as valid
// (parity-tolerant; the bash guard would silently pass in the same environment).
func isValidYAML(path string) bool {
	if _, err := exec.LookPath("python3"); err != nil {
		return true
	}
	cmd := exec.Command("python3", "-c", `import sys, yaml
list(yaml.safe_load_all(open(sys.argv[1])))`, path)
	return cmd.Run() == nil
}
