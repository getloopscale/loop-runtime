// Package credentialssot enforces T1.1 of plan 2026-05-21-deep-review-remediation:
// every K8s Secret created by canonical bootstrap scripts MUST be enumerated
// in docs/operations.md.
//
// Discovery strategy: grep `kubectl create secret {generic,docker-registry} <name>`
// literals across 4 canonical generator scripts, then assert each name
// appears in the SoT doc (either backticked or as a table cell). Skips
// `$VAR` / `"$..."` expansions (opaque to static analysis).
//
// Also asserts:
//   - 4 mandatory drill env vars are listed in the SoT doc
//     (DIGITALOCEAN_TOKEN, PULUMI_ACCESS_TOKEN, PULUMI_CONFIG_PASSPHRASE,
//     CLOUDFLARE_API_TOKEN)
//   - "Chart-hardcoded credentials" section exists (T1.3 traceability)
//   - "Rotation playbook" section exists
//
// Migrated from infra/tests/docs/credentials-sot_test.sh (M-Migrate2
// continuation 2026-07-07). Hermetic — pure text walk on generator scripts
// + SoT doc.
package credentialssot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

const sotDoc = "docs/operations.md"

// canonicalGenerators are the bootstrap scripts whose kubectl-create-secret
// literals get harvested. The bash guard hard-codes this exact list; new
// generators need an explicit code change here + a new row in the SoT doc.
var canonicalGenerators = []string{
	"infra/helmfile/scripts/generate-secrets.sh",
	"infra/helmfile/scripts/generate-build-secrets.sh",
	"infra/helmfile/scripts/generate-runtime-secrets.sh",
	"infra/scripts/production/provision-api-tokens.sh",
}

// mandatoryDrillEnvVars are the 4 env vars the SoT doc MUST enumerate for
// operator-side drill executions.
var mandatoryDrillEnvVars = []string{
	"DIGITALOCEAN_TOKEN",
	"PULUMI_ACCESS_TOKEN",
	"PULUMI_CONFIG_PASSPHRASE",
	"CLOUDFLARE_API_TOKEN",
}

// mandatorySections are the SoT doc structural headings required for T1.3
// traceability + operator rotation runbooks.
var mandatorySections = []string{
	"Chart-hardcoded credentials",
	"## Rotation playbook",
}

// secretDiscoveryRE ports the bash grep:
//
//	kubectl[^#]*create secret (generic|docker-registry)\s+[a-z][a-z0-9_-]+\s
//
// Captures the LITERAL secret name in group 1. Note: `[^#\n]*` (not `[^#]*`)
// because Go regexp does NOT special-case `\n` inside a negated class —
// `[^#]` alone would greedy-match across newlines and drop earlier hits.
// Bash `grep -hE` is inherently line-scoped so does not have this issue.
var secretDiscoveryRE = regexp.MustCompile(`kubectl[^#\n]*create secret (?:generic|docker-registry)\s+([a-z][a-z0-9_-]+)\s`)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome carries the audit summary.
type Outcome struct {
	Discovered []string
	Failures   []string
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

// ErrViolations means at least one Secret is created in code but not documented
// OR a mandatory env var / section is missing from the SoT doc.
var ErrViolations = errors.New("credentials SoT drift: secrets created in code are not documented OR mandatory env vars / sections missing")

// ErrSotMissing means the SoT doc itself is absent.
var ErrSotMissing = errors.New("docs/operations.md missing")

// Check runs the audit.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	sotPath := filepath.Join(root, sotDoc)
	sotBytes, err := os.ReadFile(sotPath)
	if err != nil {
		return Outcome{}, ErrSotMissing
	}
	sot := string(sotBytes)

	// Discover Secret names from canonical generators.
	discoveredSet := map[string]struct{}{}
	for _, gen := range canonicalGenerators {
		full := filepath.Join(root, gen)
		body, err := os.ReadFile(full)
		if err != nil {
			continue // tolerate missing generators (a strangler wave may have deleted one)
		}
		for _, m := range secretDiscoveryRE.FindAllStringSubmatch(string(body), -1) {
			if len(m) >= 2 {
				discoveredSet[m[1]] = struct{}{}
			}
		}
	}
	discovered := make([]string, 0, len(discoveredSet))
	for k := range discoveredSet {
		discovered = append(discovered, k)
	}
	sort.Strings(discovered)

	outcome := Outcome{Discovered: discovered}
	if len(discovered) == 0 {
		outcome.Failures = append(outcome.Failures, "no literal Secret names discovered — canonical generators may have moved or the grep pattern is broken")
		outcome.Violation = ErrViolations
		return outcome, nil
	}

	// Assert each discovered Secret appears in the SoT doc.
	for _, name := range discovered {
		if !sotMentions(sot, name) {
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("Secret '%s' created in code but NOT documented in %s", name, sotDoc))
		}
	}

	// Assert each mandatory drill env var is present.
	for _, envVar := range mandatoryDrillEnvVars {
		if !sotMentionsEnvVar(sot, envVar) {
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("Drill env var '%s' missing from SoT doc", envVar))
		}
	}

	// Assert each mandatory structural section is present.
	for _, section := range mandatorySections {
		if !strings.Contains(sot, section) {
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("SoT doc missing '%s' section", section))
		}
	}

	if len(outcome.Failures) > 0 {
		outcome.Violation = ErrViolations
		return outcome, nil
	}
	return outcome, nil
}

// sotMentions ports the bash grep: match `<name>` (backticked) OR a table
// cell of the form `| <name> `.
func sotMentions(sot, name string) bool {
	if strings.Contains(sot, "`"+name+"`") {
		return true
	}
	// Line-based table-cell check: `^\| <name> `
	re := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(name) + ` `)
	return re.MatchString(sot)
}

// sotMentionsEnvVar ports the same match for env-var-shaped names.
func sotMentionsEnvVar(sot, name string) bool {
	if strings.Contains(sot, "`"+name+"`") {
		return true
	}
	re := regexp.MustCompile(`(?m)\| ` + regexp.QuoteMeta(name) + ` `)
	return re.MatchString(sot)
}
