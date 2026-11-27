// Package vaultbootstrapunsealloop is a regression guard for the 2026-06-15
// dev-public Cleanroom canonical fix of infra/scripts/local/vault-bootstrap-dev.sh
// (Steps 2 + 3).
//
// Bug class: `bao status` uses exit-code semantics — rc=0 unsealed,
// rc=2 sealed-but-initialized (NORMAL), rc=1 error. The script was piping
// `bao status | python3` under `set -o pipefail`, which propagated rc=2 as
// PIPELINE FAILURE, triggering the `|| echo "true"` (lowercase) fallback
// REGARDLESS of stdout JSON. The lowercase string then failed the
// `== "True"` (Python-capitalized) compare → unseal loop SILENTLY SKIPPED
// on every fresh init. Vault stayed sealed; operator manually invoked
// `bao operator unseal $SHARD x3` as workaround.
//
// Sponsor mandate 2026-06-15 "sem WORKAROUNDs" ("no workarounds"): fix canonical means capture // english-only: verbatim quote of the sponsor mandate
// stdout first, parse rc-independent, normalize the bool string.
//
// Migrated from infra/tests/check-vault-bootstrap-unseal-loop_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic: NO network, NO bao — pure
// regex walk over target shell script.
package vaultbootstrapunsealloop

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

const targetPath = "infra/scripts/local/vault-bootstrap-dev.sh"

// Bit-for-bit ports of the bash `grep -E` patterns. Regex literals kept
// verbatim so future readers can compare against the original bash guard.
var (
	// B1: stdout captured before parse.
	step2CaptureRE = regexp.MustCompile(`BAO_STATUS_JSON=\$\(bao status -format=json 2>/dev/null \|\| true\)`)
	// B2: parse via echo captured var (rc-independent).
	step2EchoParseRE = regexp.MustCompile(`echo "\$BAO_STATUS_JSON" \| python3`)
	// B3: Python parse handles empty via or '{}'.
	step2EmptyOrRE = regexp.MustCompile(`json\.loads\(sys\.stdin\.read\(\) or '\{\}'\)`)
	// B4: fallback capitalized "False" (matches Python bool repr).
	falseCapRE = regexp.MustCompile(`echo "False"`)
	// B5: compare uses capitalized "True".
	step2CompareRE = regexp.MustCompile(`\[\[ "\$INIT_STATUS" != "True" \]\]`)

	// C1: BAO_STATUS_JSON capture ≥ 2 times (Step 2 + Step 3), allow leading whitespace.
	stepCaptureCountRE = regexp.MustCompile(`(?m)^[ \t]*BAO_STATUS_JSON=\$\(bao status -format=json`)
	// C2: SEALED parsed via echo BAO_STATUS_JSON.
	step3SealedParseRE = regexp.MustCompile(`SEALED=\$\(echo "\$BAO_STATUS_JSON" \| python3`)
	// C3: SEALED fallback capitalized "True".
	trueCapRE = regexp.MustCompile(`echo "True"`)
	// C4: SEALED compare uses capitalized "True".
	step3CompareRE = regexp.MustCompile(`\[\[ "\$SEALED" == "True" \]\]`)

	// D1: forbid the ORIGINAL buggy direct-pipe pattern
	// `bao status -format=json 2>/dev/null | python3` on non-comment lines.
	buggyDirectPipeRE = regexp.MustCompile(`bao status -format=json 2>/dev/null \| python3`)
	// D2: forbid lowercase `echo "true"` fallback on parse assign lines
	// (INIT_STATUS= or SEALED=). The parse compare wants Python-cap "True";
	// lowercase silently mismatches → the original bug.
	lowercaseTrueFallbackToken = `echo "true"`

	// E1a: CANONICAL FIX 2026-06-15 audit marker.
	auditCanonicalFixRE = regexp.MustCompile(`CANONICAL FIX 2026-06-15`)
	// E1b: exit-code SEMANTICS rationale.
	auditExitCodeRE = regexp.MustCompile(`exit-code SEMANTICS`)
	// E2: pipefail + bao rc=2 rationale.
	auditPipefailRE = regexp.MustCompile(`pipefail.*bao rc=2`)
	// E3: sponsor mandate cited, verbatim, in the language it was given.
	auditNoWorkaroundsRE = regexp.MustCompile(`sem WORKAROUNDs`) // english-only: verbatim quote the script must carry
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
var ErrViolations = errors.New("vault-bootstrap-dev.sh unseal-loop canonical fix invariants violated")

// Check reads the target shell script and asserts all invariants.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, targetPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("target script missing at %s: %v", targetPath, err)}, Violation: ErrViolations}, nil
	}

	var failures []string
	assertMatch := func(desc string, re *regexp.Regexp) {
		if !re.Match(body) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (regex `%s`)", desc, re.String()))
		}
	}

	// A: bash -n syntax (tolerant fallthrough when bash absent).
	if _, lookErr := exec.LookPath("bash"); lookErr == nil {
		cmd := exec.Command("bash", "-n", full)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			failures = append(failures, fmt.Sprintf("bash -n syntax check failed: %s", strings.TrimSpace(stderr.String())))
		}
	}

	// B: Step 2 canonical fix.
	assertMatch("Step 2 stdout captured into BAO_STATUS_JSON before parse", step2CaptureRE)
	assertMatch("Step 2 parse via echo BAO_STATUS_JSON (rc-independent)", step2EchoParseRE)
	assertMatch("Step 2 Python parse handles empty via or '{}'", step2EmptyOrRE)
	assertMatch("Step 2 fallback echo False (capitalized — Python repr)", falseCapRE)
	assertMatch("Step 2 compare against capitalized 'True'", step2CompareRE)

	// C: Step 3 canonical fix.
	captureCount := len(stepCaptureCountRE.FindAll(body, -1))
	if captureCount < 2 {
		failures = append(failures, fmt.Sprintf("Step 3: expected ≥2 BAO_STATUS_JSON captures (Step 2 + Step 3), got %d", captureCount))
	}
	assertMatch("Step 3 SEALED parsed via echo BAO_STATUS_JSON", step3SealedParseRE)
	assertMatch("Step 3 SEALED fallback 'True' capitalized", trueCapRE)
	assertMatch("Step 3 compare against capitalized 'True'", step3CompareRE)

	// D: anti-pattern guards. Both walk non-comment lines.
	if buggyLine := scanNonCommentLine(body, buggyDirectPipeRE); buggyLine != "" {
		failures = append(failures, fmt.Sprintf("FORBIDDEN anti-pattern (buggy direct-pipe parse): %s", buggyLine))
	}
	if lowerLine := scanLowercaseTrueFallback(body); lowerLine != "" {
		failures = append(failures, fmt.Sprintf("FORBIDDEN anti-pattern (lowercase \"true\" fallback on parse path): %s", lowerLine))
	}

	// E: audit trail cross-references.
	assertMatch("audit trail: CANONICAL FIX 2026-06-15 marker", auditCanonicalFixRE)
	assertMatch("audit trail: exit-code SEMANTICS rationale", auditExitCodeRE)
	assertMatch("audit trail: pipefail+bao rc=2 rationale", auditPipefailRE)
	assertMatch("audit trail: sponsor no-workarounds mandate cited", auditNoWorkaroundsRE)

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// scanNonCommentLine returns the first non-comment line matching re, or "".
// Ports the Python `line.strip().startswith("#") skip` loop from the bash guard.
func scanNonCommentLine(body []byte, re *regexp.Regexp) string {
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if re.MatchString(line) {
			return fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line))
		}
	}
	return ""
}

// scanLowercaseTrueFallback ports the bash guard's D2 Python scanner: search
// for `echo "true"` (lowercase) on non-comment lines that ALSO contain `||`
// AND (INIT_STATUS= OR SEALED=) — i.e. the parse assign paths.
func scanLowercaseTrueFallback(body []byte) string {
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.Contains(line, "||") {
			continue
		}
		if !strings.Contains(line, lowercaseTrueFallbackToken) {
			continue
		}
		if !strings.Contains(line, "INIT_STATUS=") && !strings.Contains(line, "SEALED=") {
			continue
		}
		return fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line))
	}
	return ""
}
