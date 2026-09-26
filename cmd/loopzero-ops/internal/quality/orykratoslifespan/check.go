// Package orykratoslifespan enforces canonical self-service flow lifespans
// declared in infra/helm/ory-kratos/values.yaml.gotmpl:
//
//	selfservice.flows.login.lifespan        = 10m
//	selfservice.flows.registration.lifespan = 10m
//	selfservice.flows.recovery.lifespan     = 15m
//
// Defense-in-depth pair with edge rate_limit zones (T1.2 of plan
// kratos-self-service-rate-limit-systemic v1.1): tighter flow lifespan +
// per-IP buckets together close Red Team loopzero-cloud#89 root cause
// (email-bomb via unlimited recovery flow-creates).
//
// The bash guard used source-level grep on values.yaml.gotmpl as the
// canonical gate and helmfile-template only as an optional operator check.
// This Go port ships the source-level assertions and drops the optional
// helmfile-template branch (Anti-Pattern #8: Go shell-wrap of helmfile adds
// no value — operators run `task quality:gates helmfile-render` when needed).
//
// Migrated from infra/tests/helm/ory-kratos-lifespan_test.sh (M-Migrate2
// continuation 2026-07-07). Hermetic — pure text walk.
package orykratoslifespan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

const (
	valuesPath = "infra/helm/ory-kratos/values.yaml.gotmpl"
	planSlug   = "kratos-self-service-rate-limit-systemic"
	maxLoC     = 500
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
var ErrViolations = errors.New("ory-kratos lifespan invariants violated")

var lifespanRE = regexp.MustCompile(`(?m)^\s*lifespan:`)

// Check runs the 6 source-level assertions from the bash guard.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var failures []string

	// Read the values file.
	full := filepath.Join(root, valuesPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("values.yaml.gotmpl missing at %s: %v", valuesPath, err)}, Violation: ErrViolations}, nil
	}
	text := string(body)

	// A1 total lifespan keys ≥ 3
	if len(lifespanRE.FindAllString(text, -1)) < 3 {
		failures = append(failures, "source has fewer than 3 `lifespan:` keys (expected login+registration+recovery)")
	}

	// A2-A4 login/registration/recovery lifespan values.
	//
	// The window is scoped to lines that stay INSIDE the section body — i.e.
	// stop at the next line whose leading indent is ≤ the header line's indent.
	// This is a stricter port of the bash `grep -A N` heuristic (N=12 in bash);
	// the section-scoped version prevents false-PASS when one section is
	// wrong but the next section's lifespan value happens to be within N lines.
	checkLifespan := func(section, expected string) {
		body, ok := extractSectionBody(text, section)
		if !ok {
			failures = append(failures, fmt.Sprintf("section `%s:` not located", section))
			return
		}
		want := fmt.Sprintf("lifespan: %s", expected)
		if !strings.Contains(body, want) {
			failures = append(failures, fmt.Sprintf("`%s` section missing `%s` (within its own block)", section, want))
		}
	}
	checkLifespan("login", "10m")
	checkLifespan("registration", "10m")
	checkLifespan("recovery", "15m")

	// A5 all 3 lifespan keys cite the plan slug — count occurrences ≥ 3.
	if strings.Count(text, planSlug) < 3 {
		failures = append(failures, fmt.Sprintf("plan slug `%s` should appear ≥ 3 times in values.yaml.gotmpl (one per lifespan key), for grep+blame traceability", planSlug))
	}

	// A6 source file under 500 LoC (per architecture.md § file size budget).
	if lines := strings.Count(text, "\n"); lines >= maxLoC {
		failures = append(failures, fmt.Sprintf("values.yaml.gotmpl at %d lines exceeds soft budget of %d — split or refactor", lines, maxLoC))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// extractSectionBody returns the lines belonging to the YAML block whose
// header line is `\s*<name>:`. The body ends at the first subsequent line
// whose leading indent is <= the header's leading indent (i.e. the next
// same-or-outer-level key). Improves on the bash `grep -A 12` heuristic by
// scoping strictly to the section body — sibling sections cannot leak in.
func extractSectionBody(text, name string) (string, bool) {
	lines := strings.Split(text, "\n")
	headerSuffix := name + ":"
	// Locate the header line and its indent.
	headerIdx := -1
	headerIndent := 0
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, headerSuffix) {
			continue
		}
		// After stripping the header suffix, either EOL, whitespace + comment,
		// or nothing may follow.
		rest := strings.TrimSpace(trimmed[len(headerSuffix):])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			continue
		}
		headerIdx = i
		headerIndent = len(line) - len(trimmed)
		break
	}
	if headerIdx < 0 {
		return "", false
	}
	// Walk forward collecting lines until we hit a same-or-outer-indent key.
	var body []string
	for j := headerIdx + 1; j < len(lines); j++ {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			body = append(body, l)
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		trimmed := strings.TrimSpace(l)
		if indent <= headerIndent && !strings.HasPrefix(trimmed, "#") {
			break
		}
		body = append(body, l)
	}
	return strings.Join(body, "\n"), true
}
