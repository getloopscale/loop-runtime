// Package pulumirequiredsecrets enforces T1.5 of plan
// 2026-05-21-deep-review-remediation: every Pulumi.<env>.yaml stack
// config must enumerate the required secret keys for that environment.
//
// Background: prior to this gate, Pulumi.prod.yaml + Pulumi.staging.yaml
// shipped with ZERO `secure:` keys. Required secrets were documented
// only in comments. `pulumi preview --stack prod` locally without the
// secrets emitted cryptic provider errors deep in resource creation
// instead of a fail-fast "missing required config" error.
//
// EC-14 documented: an operator without PULUMI_CONFIG_PASSPHRASE gets
// "failed to decrypt" instead of fail-fast — mitigated by documenting
// the passphrase as pre-requisite in docs/operations.md.
//
// Migrated from infra/tests/docs/pulumi-required-secrets_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic — pure text walk.
package pulumirequiredsecrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

const (
	prodStackPath    = "infra/pulumi/Pulumi.prod.yaml"
	stagingStackPath = "infra/pulumi/Pulumi.staging.yaml"
	drillStackPath   = "infra/pulumi-drill/Pulumi.drill.yaml"
	sotDocPath       = "docs/operations.md"
)

// requiredProd is the set of secret keys that Pulumi.prod.yaml MUST list
// (either as a `secure:` entry or as a comment referencing the key).
// Order irrelevant; a map keeps parity with the bash `declare -A`.
var requiredProd = []string{
	"spacesAccessKeyId",
	"spacesSecretAccessKey",
	"digitaloceanToken",
	"cfApiToken",
	"giteaAdminPassword",
	"minioRootPassword",
}

// requiredStaging is the subset for staging (drops giteaAdminPassword +
// minioRootPassword since staging inherits DOKS-managed PG + local Gitea).
var requiredStaging = []string{
	"spacesAccessKeyId",
	"spacesSecretAccessKey",
	"digitaloceanToken",
	"cfApiToken",
}

// requiredDrill for the drill stack — drill uses DIGITALOCEAN_TOKEN via env
// var (D7' sponsor override 2026-05-16), so only 3 CF + Spaces keys are
// stored in stack config.
var requiredDrill = []string{
	"cfAccountId",
	"spacesAccessId",
	"spacesSecretKey",
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the audit summary.
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

// ErrViolations means at least one required secret key is not enumerated
// in its stack file OR missing from the SoT doc.
var ErrViolations = errors.New("required Pulumi secret key missing from stack file OR SoT doc")

// Check runs the audit.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var failures []string

	// Prod stack.
	if prodBody, err := os.ReadFile(filepath.Join(root, prodStackPath)); err != nil {
		failures = append(failures, fmt.Sprintf("%s missing", prodStackPath))
	} else {
		for _, key := range requiredProd {
			if !hasKeyMention(string(prodBody), key) {
				failures = append(failures, fmt.Sprintf("prod: required secret '%s' NOT mentioned in %s (neither as config nor as comment)", key, prodStackPath))
			}
		}
	}

	// Staging stack.
	if stagingBody, err := os.ReadFile(filepath.Join(root, stagingStackPath)); err != nil {
		failures = append(failures, fmt.Sprintf("%s missing", stagingStackPath))
	} else {
		for _, key := range requiredStaging {
			if !hasKeyMention(string(stagingBody), key) {
				failures = append(failures, fmt.Sprintf("staging: required secret '%s' NOT mentioned in %s", key, stagingStackPath))
			}
		}
	}

	// Drill stack — TOLERATE ABSENCE (bash guard also gates behind `-f`).
	if drillBody, err := os.ReadFile(filepath.Join(root, drillStackPath)); err == nil {
		for _, key := range requiredDrill {
			if !hasKeyMention(string(drillBody), key) {
				failures = append(failures, fmt.Sprintf("drill: required secret '%s' NOT mentioned in %s", key, drillStackPath))
			}
		}
	}

	// Cross-check: SoT doc lists same prod secrets.
	if sotBody, err := os.ReadFile(filepath.Join(root, sotDocPath)); err != nil {
		failures = append(failures, fmt.Sprintf("SoT doc missing: %s (run T1.1 first)", sotDocPath))
	} else {
		sot := string(sotBody)
		for _, key := range requiredProd {
			if !strings.Contains(sot, "`"+key+"`") {
				failures = append(failures, fmt.Sprintf("Required secret '%s' missing from SoT doc — update %s", key, sotDocPath))
			}
		}
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// hasKeyMention ports the bash grep:
//
//	^\s*<key>:|^\s*[a-z][a-z0-9-]*:<key>:|#.*<key>
//
// Matches the key as a bare config entry, a namespaced config entry, or
// inside a comment. Line-anchored via (?m).
func hasKeyMention(body, key string) bool {
	esc := regexp.QuoteMeta(key)
	re := regexp.MustCompile(`(?m)^\s*` + esc + `:|^\s*[a-z][a-z0-9-]*:` + esc + `:|#.*` + esc)
	return re.MatchString(body)
}
