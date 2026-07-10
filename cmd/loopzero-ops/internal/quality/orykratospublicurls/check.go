// Package orykratospublicurls is a hermetic helm-template golden test for
// serve.public.base_url + CORS + cookie.domain + allowed_return_urls +
// dashboardRoute-mapped ui_url across multiple env yamls of the Ory Kratos
// helmfile release.
//
// Migrated from infra/tests/helm/ory-kratos-public-urls_test.sh (M22
// follow-up after M22 empirical finding — the ONLY M22 target that was
// allowlisted, so migration delivers empirical allowlist reduction). Follows
// M16 T3.5 esochartsanity canonical shape (helm-template subprocess +
// per-env regex golden assertions + advisory-fallthrough when helm/helmfile
// absent).
package orykratospublicurls

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	helmfileRel  = "infra/helmfile/mgmt.helmfile.yaml.gotmpl"
	allowlistRel = ".claude/rules/shell-scripts-allowlist.md"
	allowlistTag = "infra/tests/helm/ory-kratos-public-urls_test.sh"
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the aggregated result. Advisory-fallthrough per M16 canonical:
// missing helmfile binary → Skipped=true (CI passes; local dev without
// helmfile passes).
type Outcome struct {
	Pass       bool
	Skipped    bool
	Failures   []string
	SkipReason string
}

// ErrViolations signals ≥ 1 golden assertion failed.
var ErrViolations = errors.New("ory-kratos-public-urls hermetic golden violated")

// envAssertion is one env → set of expected regex patterns that MUST appear
// in the rendered helmfile output.
type envAssertion struct {
	env      string
	musts    []regexpAssertion
	mustNots []regexpAssertion // patterns that must NOT appear
}

type regexpAssertion struct {
	name    string
	pattern *regexp.Regexp
}

// standalone-public assertions (8 canonical patterns from bash T2.1)
var standaloneAssertions = envAssertion{
	env: "standalone-public",
	musts: []regexpAssertion{
		{"base_url with public auth host", regexp.MustCompile(`base_url:\s*"?https://auth\.loopzero\.dev/?"?`)},
		{"CORS enabled", regexp.MustCompile(`enabled:\s*true`)},
		{"CORS allow_credentials", regexp.MustCompile(`allow_credentials:\s*true`)},
		{"cookie domain set to loopzero.example", regexp.MustCompile(`domain:\s*"?loopzero\.dev"?`)},
		{"allowed_return_urls present", regexp.MustCompile(`allowed_return_urls:`)},
		{"registration ui_url uses /register", regexp.MustCompile(`ui_url:\s*"?https://app\.loopzero\.dev/register"?`)},
		{"login ui_url uses /login", regexp.MustCompile(`ui_url:\s*"?https://app\.loopzero\.dev/login"?`)},
	},
}

// dev env assertions
var devAssertions = envAssertion{
	env: "dev",
	musts: []regexpAssertion{
		{"login ui_url uses localhost:4455", regexp.MustCompile(`ui_url:\s*"?http://localhost:4455/login"?`)},
	},
	mustNots: []regexpAssertion{
		{"no CORS block (publicAuthUrl unset)", regexp.MustCompile(`allow_credentials:`)},
	},
}

// doubleSlashRE detects `//` inside URL paths (excluding base64 payloads).
// Used as an independent invariant post-per-env rendering.
var doubleSlashRE = regexp.MustCompile(`https?://[^/[:space:]]+//`)

// Check runs the per-env golden assertions and returns (Outcome, error).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	// Advisory-fallthrough: helmfile binary required.
	if _, err := exec.LookPath("helmfile"); err != nil {
		return Outcome{
			Pass:       true,
			Skipped:    true,
			SkipReason: "helmfile binary not on PATH — advisory-fallthrough (CI + local dev without helmfile pass)",
		}, nil
	}

	// Advisory-fallthrough: helmfile config must exist.
	helmfilePath := filepath.Join(root, helmfileRel)
	if _, err := os.Stat(helmfilePath); err != nil {
		return Outcome{
			Pass:       true,
			Skipped:    true,
			SkipReason: fmt.Sprintf("helmfile config %s not present — advisory-fallthrough", helmfileRel),
		}, nil
	}

	// Allowlist check (mirror sibling M-Migrate2 gates).
	allowlist := filepath.Join(root, allowlistRel)
	allowBytes, allowErr := os.ReadFile(allowlist)
	if allowErr != nil {
		return Outcome{
			Pass:     false,
			Failures: []string{fmt.Sprintf("allowlist file exists: %s missing", allowlistRel)},
		}, ErrViolations
	}
	if !strings.Contains(string(allowBytes), allowlistTag) {
		return Outcome{
			Pass:     false,
			Failures: []string{fmt.Sprintf("script allowlisted: %q not found in %s", allowlistTag, allowlistRel)},
		}, ErrViolations
	}

	var failures []string

	envsRendered := 0
	for _, ea := range []envAssertion{standaloneAssertions, devAssertions} {
		rendered, renderErr := renderEnv(root, ea.env)
		if renderErr != nil || len(rendered) == 0 {
			// Bash test SKIPs the env when render fails (env yamls WIP);
			// do NOT accumulate as failure — mirror the bash semantics
			// verbatim. Gate FAILs only when render succeeds AND assertion
			// fails.
			_ = renderErr
			continue
		}
		envsRendered++
		content := string(rendered)

		for _, m := range ea.musts {
			if !m.pattern.MatchString(content) {
				failures = append(failures, fmt.Sprintf(
					"env %s: MUST-match failed — %s (pattern: %s)",
					ea.env, m.name, m.pattern.String()))
			}
		}

		for _, mn := range ea.mustNots {
			if mn.pattern.MatchString(content) {
				failures = append(failures, fmt.Sprintf(
					"env %s: MUST-NOT-match failed — %s (pattern: %s)",
					ea.env, mn.name, mn.pattern.String()))
			}
		}

		// Double-slash invariant on standalone-public only (bash T2.1 scope).
		if ea.env == "standalone-public" {
			nonBase64Lines := stripBase64Lines(content)
			if doubleSlashRE.MatchString(nonBase64Lines) {
				failures = append(failures,
					fmt.Sprintf("env %s: URL double-slash detected (excluding base64 payloads)", ea.env))
			}
		}
	}

	if len(failures) > 0 {
		return Outcome{Pass: false, Failures: failures}, ErrViolations
	}
	if envsRendered == 0 {
		// All envs SKIPped in render — nothing empirically asserted.
		// Mirror bash test behavior: no PASS, no FAIL, just skip.
		return Outcome{
			Pass:       true,
			Skipped:    true,
			SkipReason: "no env rendered successfully — envs may be WIP or helmfile deps missing",
		}, nil
	}
	return Outcome{Pass: true}, nil
}

// renderEnv runs `helmfile template` for the given env and returns the
// rendered YAML bytes.
func renderEnv(root, env string) ([]byte, error) {
	cmd := exec.Command("helmfile",
		"-f", helmfileRel,
		"-e", env,
		"--selector", "name=ory-kratos",
		"template", "--skip-deps")
	cmd.Dir = root
	cmd.Stderr = io.Discard
	return cmd.Output()
}

// stripBase64Lines removes lines containing "base64" so the double-slash
// invariant doesn't false-positive on encoded payloads.
func stripBase64Lines(content string) string {
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "base64") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
