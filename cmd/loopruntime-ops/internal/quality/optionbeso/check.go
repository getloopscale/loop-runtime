// Package optionbeso is a hermetic contract test for
// docs/operations/option-b-mgmt-helmfile-eso-deploy.md.
//
// Locks that the Option B analysis doc:
//   - Documents 4 sub-paths (B1, B2, B3, B4) with each rated.
//   - Names B1 as RECOMMENDED for dev-public iter 1.
//   - Names B4 as REJECTED (mandate violation).
//   - Names B3 as NOT recommended for iter 1.
//   - Includes canonical commands for B1 (helmfile + kubectl).
//   - Cites the iter 31 architectural finding origin.
//   - Documents rollback path.
//   - References the runtime helmfile canonical pattern source.
//   - Stays in sync with the ESO chart version pinned in runtime helmfile
//     (drift defense — if runtime bumps ESO version, this doc must match).
//
// Migrated from infra/tests/check-option-b-eso-deploy-doc_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic: NO kubectl, NO network,
// NO helm — pure regex + awk-equivalent on the doc + runtime helmfile.
package optionbeso

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
	docPath             = "docs/operations/option-b-mgmt-helmfile-eso-deploy.md"
	runtimeHelmfilePath = "infra/helmfile/cell.helmfile.yaml.gotmpl"
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome carries the assertion failures.
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
var ErrViolations = errors.New("Option B ESO deploy doc contract violated")

// Check runs the 20+ assertions from the bash guard against the doc and the
// runtime helmfile.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	// A: doc file exists.
	docFull := filepath.Join(root, docPath)
	docBytes, err := os.ReadFile(docFull)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("Option B analysis doc missing at %s: %v", docPath, err)}, Violation: ErrViolations}, nil
	}
	doc := string(docBytes)

	var failures []string
	assertRE := func(desc string, pattern string) {
		if !regexp.MustCompile(pattern).MatchString(doc) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (regex `%s`)", desc, pattern))
		}
	}
	forbidRE := func(desc string, pattern string) {
		re := regexp.MustCompile(pattern)
		if re.MatchString(doc) {
			failures = append(failures, fmt.Sprintf("FORBIDDEN: %s (regex `%s`)", desc, pattern))
		}
	}

	// B: 4 sub-paths named.
	for _, p := range []string{"B1", "B2", "B3", "B4"} {
		assertRE(fmt.Sprintf("sub-path %s named (^### %s )", p, p), fmt.Sprintf(`(?m)^### %s `, p))
	}

	// C: decision rationale.
	assertRE("B1 marked RECOMMENDED for dev-public iter 1", `B1.*upstream.*RECOMMENDED|(?m)^### B1.*RECOMMENDED`)
	assertRE("B4 marked REJECTED (mandate violation)", `B4.*REJECTED|REJECTED.*defeats|REJECTED.*mandate`)
	assertRE("B3 marked NOT recommended for iter 1", `B3.*NOT recommended|NOT recommended for iter 1`)

	// D: canonical commands for B1.
	assertRE("B1 helmfile sync --selector name=external-secrets", `helmfile.*--selector name=external-secrets sync`)
	// D2 needs a kubectl wait line whose Available marker is within 2 lines.
	if !hasKubectlWaitAvailableWithin(doc, 2) {
		failures = append(failures, "MISSING: B1 kubectl wait ... Available (within 2 lines of a kubectl wait line)")
	}
	assertRE("B1 CRD verification kubectl get crd", `kubectl.*get crd`)

	// E: ESO version pin matches runtime helmfile.
	runtimeFull := filepath.Join(root, runtimeHelmfilePath)
	runtimeBytes, runErr := os.ReadFile(runtimeFull)
	if runErr != nil {
		failures = append(failures, fmt.Sprintf("runtime helmfile missing at %s: %v", runtimeHelmfilePath, runErr))
	} else {
		runtimeESO := extractRuntimeESOVersion(string(runtimeBytes))
		docESO := extractDocESOVersion(doc)
		switch {
		case runtimeESO == "":
			failures = append(failures, "could not extract ESO version from runtime helmfile (expected `- name: external-secrets` block within 6 lines of `version:`)")
		case docESO == "":
			failures = append(failures, "could not extract ESO version from doc (expected first `version: X.Y.Z` line)")
		case runtimeESO != docESO:
			failures = append(failures, fmt.Sprintf("ESO version DRIFT: doc=%s runtime=%s (bump one to match the other)", docESO, runtimeESO))
		}
	}

	// F: architectural finding + runbook citation.
	assertRE("iter 31 architectural finding origin cited", `iter 31.*architectural|architectural finding.*iter 31`)
	assertRE("runbook Open Question cross-link cited", `openbao-transit-cutover-runbook\.md|runbook.*Open Question`)

	// G: rollback path.
	assertRE("Rollback section header (^## Rollback)", `(?m)^## Rollback`)
	rollbackBody := extractSection(doc, "## Rollback")
	if !strings.Contains(rollbackBody, "helmfile") || !containsRE(rollbackBody, `helmfile.*destroy`) {
		failures = append(failures, "MISSING: Rollback body must include helmfile destroy command")
	}
	if !strings.Contains(rollbackBody, "git restore") {
		failures = append(failures, "MISSING: Rollback body must include git restore command")
	}

	// H: decision matrix.
	assertRE("Decision matrix section header (^## Decision matrix)", `(?m)^## Decision matrix`)
	matrixBody := extractSection(doc, "## Decision matrix")
	if !containsRE(matrixBody, `Time .* Risk`) {
		failures = append(failures, "MISSING: Decision matrix body must include Time + Risk columns")
	}

	// I: cross-references section + canonical pattern source.
	assertRE("Cross-references section header (^## Cross-references)", `(?m)^## Cross-references`)
	xrefsBody := extractTailSection(doc, "## Cross-references")
	if !strings.Contains(xrefsBody, "cell.helmfile.yaml.gotmpl") {
		failures = append(failures, "MISSING: Cross-references body must cite cell.helmfile.yaml.gotmpl as canonical pattern source")
	}

	// J: anti-patterns.
	forbidRE("Co-Authored-By trailer forbidden", `(?i)Co-Authored-By`)
	assertRE("Rule 4 mandate cited (or sponsor verbatim/authorization)", `Rule 4|sponsor.*verbatim|sponsor.*authorization`)

	// K: plan slug cited.
	assertRE("J-22 plan slug cited", `2026-06-10-j22-openbao-transit-eso-sync`)

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// extractRuntimeESOVersion ports the bash awk:
//
//	/^  - name: external-secrets$/ {hit=NR}
//	hit && NR > hit && NR < hit + 6 && /version: / { sub(...); print; exit }
func extractRuntimeESOVersion(body string) string {
	lines := strings.Split(body, "\n")
	hit := -1
	versionRE := regexp.MustCompile(`^[[:space:]]*version:[[:space:]]*(\S+)`)
	for i, line := range lines {
		if strings.TrimSpace(line) == "- name: external-secrets" {
			hit = i
			continue
		}
		if hit >= 0 && i > hit && i < hit+6 {
			if m := versionRE.FindStringSubmatch(line); m != nil {
				return strings.Trim(m[1], `"'`)
			}
		}
	}
	return ""
}

// extractDocESOVersion ports the bash grep -oE 'version: [v0-9][0-9]*\.[0-9]+\.[0-9]+' | head -1 | awk '{print $2}'.
func extractDocESOVersion(doc string) string {
	re := regexp.MustCompile(`version: ([v0-9][0-9]*\.[0-9]+\.[0-9]+)`)
	m := re.FindStringSubmatch(doc)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractSection returns lines strictly between `^## Header` and the next `^## `,
// mirroring the bash `awk '/^## Header/{f=1;next} f && /^## /{f=0} f'`.
func extractSection(doc, header string) string {
	lines := strings.Split(doc, "\n")
	var out []string
	in := false
	for _, l := range lines {
		if !in {
			if strings.HasPrefix(l, header) {
				in = true
			}
			continue
		}
		if strings.HasPrefix(l, "## ") {
			break
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// extractTailSection returns lines from after `^## Header` to EOF, mirroring
// the bash `awk '/^## Header/{f=1;next} f'` (used for the LAST section).
func extractTailSection(doc, header string) string {
	lines := strings.Split(doc, "\n")
	var out []string
	in := false
	for _, l := range lines {
		if !in {
			if strings.HasPrefix(l, header) {
				in = true
			}
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// containsRE returns true if body matches pattern (a Go regex).
func containsRE(body, pattern string) bool {
	return regexp.MustCompile(pattern).MatchString(body)
}

// hasKubectlWaitAvailableWithin scans for a `kubectl ... wait` line and checks
// that `Available` appears within the next `distance` lines. Ports the bash
// `grep -A 2 -E 'kubectl.*wait' '$DOC' | grep -q -E 'Available'`.
func hasKubectlWaitAvailableWithin(doc string, distance int) bool {
	waitRE := regexp.MustCompile(`kubectl.*wait`)
	availRE := regexp.MustCompile(`Available`)
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if !waitRE.MatchString(l) {
			continue
		}
		end := i + distance + 1
		if end > len(lines) {
			end = len(lines)
		}
		window := strings.Join(lines[i:end], "\n")
		if availRE.MatchString(window) {
			return true
		}
	}
	return false
}
