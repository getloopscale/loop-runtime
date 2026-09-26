// Package devpublicenv is a hermetic structural test for
// infra/helmfile/environments/dev-public.yaml.
//
// Validates the file has the canonical shape: dev URLs (-dev.loopzero.example),
// shared cookie domain (loopzero.example), CORS + allowedReturnUrls cover both
// -dev hosts, no localhost URLs, no legacy non-dev URLs, D5 OAuth reuse
// pattern (no _DEV env var override).
//
// Migrated from infra/tests/helm/dev-public-env_test.sh (M-Migrate2
// continuation 2026-07-07). Hermetic — pure text walk on YAML.
package devpublicenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

const envPath = "infra/helmfile/environments/dev-public.yaml"

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
var ErrViolations = errors.New("dev-public.yaml canonical shape violated")

var (
	localhostRE    = regexp.MustCompile(`localhost:4455|http://localhost|127\.0\.0\.1`)
	legacyURLRE    = regexp.MustCompile(`https://(auth|app|cloud)\.loopzero\.dev`)
	devSuffixEnvRE = regexp.MustCompile(`LOOPZERO_(GITHUB|GOOGLE)_CLIENT_(ID|SECRET)_DEV`)
)

// Check runs the 14 assertions from the bash guard.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, envPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("dev-public.yaml missing at %s: %v", envPath, err)}, Violation: ErrViolations}, nil
	}
	text := string(body)

	var failures []string
	assertContains := func(desc, needle string) {
		if !strings.Contains(text, needle) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (needle `%s`)", desc, needle))
		}
	}

	// Core public URLs.
	//
	// One dashboard, one name. `app-dev` has been the canonical surface since 2026-06-22,
	// but the environment kept declaring `cloud-dev` as dashboardOrigin and publicCloudUrl —
	// two names for the SAME thing, which only worked because `cloud-dev` answered
	// 301 to `app-dev`. It is the same defect that motivated renaming the clusters and
	// unifying the cells, now at the identity layer: CORS and return URL
	// authorizing a host that exists only to redirect.
	//
	// The `cloud-dev` host was removed on 2026-08-10. Before removing it, both halves of
	// it were verified to be covered: the GitHub webhook is already registered
	// against `app-dev` (cmd/loopzero-ops/internal/seed/github_webhook_fixture.go:44) and
	// `app-dev` already has the same direct bypass to loopzero-api, so nothing depended on it.
	assertContains("publicAuthUrl points to auth-dev", `publicAuthUrl: "https://auth-dev.loopzero.example"`)
	assertContains("publicCloudUrl points to app-dev", `publicCloudUrl: "https://app-dev.loopzero.example"`)
	assertContains("uiBaseUrl points to app-dev", `uiBaseUrl: "https://app-dev.loopzero.example"`)
	assertContains("dashboardOrigin points to app-dev", `dashboardOrigin: "https://app-dev.loopzero.example"`)
	assertContains("cookieDomain is eTLD+1", `cookieDomain: "loopzero.example"`)

	// CORS + allowedReturnUrls
	assertContains("corsAllowedOrigins includes app-dev", `  - "https://app-dev.loopzero.example"`)
	assertContains("allowedReturnUrls includes app-dev trailing slash", `  - "https://app-dev.loopzero.example/"`)

	// And the inverse, which is what stops the host from coming back by inertia: authorizing
	// in CORS or in a return URL a host that no longer exists is auth surface granted to
	// nobody.
	if strings.Contains(text, "cloud-dev.loopzero.example") {
		failures = append(failures, "PRESENT: cloud-dev.loopzero.example — the host was removed on 2026-08-10; "+
			"keeping it in CORS/allowedReturnUrls authorizes an origin that does not respond")
	}

	// No localhost leak
	if localhostRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: dev-public.yaml contains localhost URL(s) (regression risk)")
	}

	// No legacy non-dev URL leak
	if legacyURLRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: dev-public.yaml contains legacy *.loopzero.example URL(s) — expected -dev suffix everywhere")
	}

	// D5: no _DEV env var override
	if devSuffixEnvRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: dev-public.yaml references _DEV-suffix env vars — per D5 (2026-06-04), OAuth apps are reused")
	}

	// Stripe webhook per-env split IS documented (D5 caveat).
	assertContains("Stripe webhook split per-env documented", "loopzero-stripe-webhook-dev")

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}
