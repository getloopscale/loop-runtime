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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

const helmfileRel = "infra/helmfile/core.helmfile.yaml.gotmpl"

// Renderer renders one helmfile environment of the ory-kratos release.
type Renderer func(root, env string) ([]byte, error)

// Config controls the check.
type Config struct {
	RepoRoot string
	// Render overrides the helmfile subprocess. Tests only.
	Render Renderer
}

// Outcome is the aggregated result. The one advisory fallthrough left is a
// machine without the helmfile binary: Skipped, with the reason. Everything
// after that — a missing helmfile config, a render that fails or renders
// nothing — is a finding, never a skip.
type Outcome struct {
	Pass       bool
	Skipped    bool
	Failures   []string
	SkipReason string
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

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
		{"base_url with public auth host", regexp.MustCompile(`base_url:\s*"?https://auth\.getloopscale\.com/?"?`)},
		{"CORS enabled", regexp.MustCompile(`enabled:\s*true`)},
		{"CORS allow_credentials", regexp.MustCompile(`allow_credentials:\s*true`)},
		{"cookie domain set to getloopscale.com", regexp.MustCompile(`domain:\s*"?getloopscale\.com"?`)},
		{"allowed_return_urls present", regexp.MustCompile(`allowed_return_urls:`)},
		{"registration ui_url uses /register", regexp.MustCompile(`ui_url:\s*"?https://app\.getloopscale\.com/register"?`)},
		{"login ui_url uses /login", regexp.MustCompile(`ui_url:\s*"?https://app\.getloopscale\.com/login"?`)},
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

	render := cfg.Render
	if render == nil {
		if _, err := exec.LookPath("helmfile"); err != nil {
			return Outcome{
				Pass:       true,
				Skipped:    true,
				SkipReason: "helmfile binary not on PATH — advisory-fallthrough",
			}, nil
		}
		render = helmfileRender
	}

	if _, err := os.Stat(filepath.Join(root, helmfileRel)); err != nil {
		return Outcome{}, fmt.Errorf("helmfile config %s: %w", helmfileRel, err)
	}

	var failures []string
	for _, ea := range []envAssertion{standaloneAssertions, devAssertions} {
		rendered, err := render(root, ea.env)
		if err != nil {
			failures = append(failures, fmt.Sprintf("env %s: render failed — %v", ea.env, err))
			continue
		}
		if len(rendered) == 0 {
			failures = append(failures, fmt.Sprintf("env %s: render produced nothing", ea.env))
			continue
		}
		failures = append(failures, ea.check(string(rendered))...)
	}

	if len(failures) > 0 {
		return Outcome{Pass: false, Failures: failures}, nil
	}
	return Outcome{Pass: true}, nil
}

// check applies one environment's golden assertions to its rendered output.
func (ea envAssertion) check(content string) []string {
	var failures []string
	for _, m := range ea.musts {
		if !m.pattern.MatchString(content) {
			failures = append(failures, fmt.Sprintf(
				"env %s: MUST-match failed — %s (pattern: %s)", ea.env, m.name, m.pattern.String()))
		}
	}
	for _, mn := range ea.mustNots {
		if mn.pattern.MatchString(content) {
			failures = append(failures, fmt.Sprintf(
				"env %s: MUST-NOT-match failed — %s (pattern: %s)", ea.env, mn.name, mn.pattern.String()))
		}
	}
	// Double-slash invariant on standalone-public only.
	if ea.env == "standalone-public" && doubleSlashRE.MatchString(stripBase64Lines(content)) {
		failures = append(failures,
			fmt.Sprintf("env %s: URL double-slash detected (excluding base64 payloads)", ea.env))
	}
	return failures
}

// renderPlaceholderEnv satisfies the chart's requiredEnv secrets with a fixed,
// obviously fake value. This is a template golden test of URLs: it needs the
// render to complete, never a real credential — and a real one must not be read
// into rendered output here.
var renderPlaceholderEnv = []string{"LOOPZERO_DB_PASSWORD=golden-render-placeholder"}

// helmfileRender runs `helmfile template` for the env and returns the rendered
// YAML. A failure carries helmfile's stderr, which names the cause.
func helmfileRender(root, env string) ([]byte, error) {
	cmd := exec.Command("helmfile",
		"-f", helmfileRel,
		"-e", env,
		"--selector", "name=ory-kratos",
		"template", "--skip-deps")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), renderPlaceholderEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
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
