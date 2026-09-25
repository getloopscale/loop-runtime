// Package sprintsyntax is a hermetic contract test for
// infra/scripts/local/check-sprint.sh + its homed helper
// infra/scripts/local/check_sprint.py.
//
// Reproduces the 11 assertions from the original bash test
// (infra/tests/check-sprint-syntax_test.sh) as pure-logic Go:
//
//  1. script exists                       (os.Stat)
//  2. bash -n ok                          (exec.Command "bash" "-n")
//  3. helper exists                       (os.Stat)
//  4. helper compiles                     (exec.Command "python3" "-m" "py_compile")
//  5. no python3 heredoc remains          (regexp pattern absence)
//  6. JSON report is valid                (json.Unmarshal on helper stdout)
//  7. JSON report has "pass": 6           (bytes.Contains)
//  8. JSON report preserves spaced detail (bytes.Contains)
//  9. JSON report results[0]==[c1,PASS,OK] (json.Unmarshal + typed assertion)
//
// 10. allowlist file exists               (os.Stat)
// 11. script allowlisted                  (bytes.Contains)
//
// Migrated from infra/tests/check-sprint-syntax_test.sh
// (M-Migrate2 2026-07-10 M16 T2.1). Requires bash + python3 on PATH.
package sprintsyntax

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qgshell"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellscript"
)

const (
	scriptRel    = "infra/scripts/local/check-sprint.sh"
	helperRel    = "infra/scripts/local/check_sprint.py"
	allowlistRel = "infra/quality/shell-scripts-allowlist.md"
	allowlistTag = "check-sprint.sh"
)

// Config controls the check.
type Config struct {
	// RepoRoot resolves the target paths. When empty, os.Getwd() is used.
	RepoRoot string
}

// Outcome is the aggregated result (canonical M-Migrate2 shape — plan D2 of
// M16 mandates Failures []string per blueprint ADR D1).
type Outcome struct {
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed. Callers use errors.Is to distinguish
// assertion failures from structural errors (unrecoverable).
var ErrViolations = errors.New("check-sprint-syntax hermetic contract violated")

// Check runs the 11 assertions and returns (Outcome, error).
//
// Failure taxonomy mapped from bash exit codes per blueprint Q8:
//   - Structural (bash exit 2 → Go error): RepoRoot unreadable, essential file
//     stat error other than "missing"; unrecoverable.
//   - Assertion (bash exit 1 → Outcome.Failures + ErrViolations): any of the 11
//     assertions failed; collect-all pattern.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	script := filepath.Join(root, scriptRel)
	helper := filepath.Join(root, helperRel)
	allowlist := filepath.Join(root, allowlistRel)

	var failures []string

	// 1. script exists
	scriptBytes, existsErr := qgshell.StatAndRead(script)
	if existsErr != nil {
		failures = append(failures, fmt.Sprintf("script exists: %s missing", scriptRel))
	}

	// 2. bash -n ok
	if existsErr == nil {
		if err := qgshell.RunQuiet("bash", "-n", script); err != nil {
			failures = append(failures, fmt.Sprintf("bash -n %s: %v", scriptRel, err))
		}
	}

	// 3. helper exists
	if _, err := os.Stat(helper); err != nil {
		failures = append(failures, fmt.Sprintf("helper exists: %s missing", helperRel))
	}

	// 4. helper compiles
	if err := qgshell.RunQuiet("python3", "-m", "py_compile", helper); err != nil {
		failures = append(failures, fmt.Sprintf("helper compiles (python3 -m py_compile): %v", err))
	}

	// 5. no python3 heredoc remains — grep against script content
	if scriptBytes != nil && shellscript.PythonHeredoc.Match(scriptBytes) {
		failures = append(failures, "no python3 heredoc remains: found forbidden heredoc pattern (T3.2 de-costume gate)")
	}

	// 6-9. JSON report shape via CS_* env invocation
	report, reportErr := runHelperJSON(helper)
	if reportErr != nil {
		failures = append(failures, fmt.Sprintf("report shape: %v", reportErr))
	} else {
		var parsed map[string]any
		if err := json.Unmarshal(report, &parsed); err != nil {
			failures = append(failures, fmt.Sprintf("report is valid JSON: %v", err))
		} else {
			// 7. pass count present as 6 (int OR float64 depending on encode)
			if v, ok := parsed["pass"]; !ok || !jsonEquals(v, 6) {
				failures = append(failures, `report has pass count: expected "pass": 6`)
			}
			// 8. spaced detail preserved
			if !bytes.Contains(report, []byte("kubectl context unreachable")) {
				failures = append(failures, "report preserves spaced detail: 'kubectl context unreachable' not found")
			}
			// 9. results[0] == ["c1","PASS","OK"]
			if !resultsFirstEquals(parsed, "c1", "PASS", "OK") {
				failures = append(failures, "report results is a list of [id,status,detail]: results[0] != [c1,PASS,OK]")
			}
		}
	}

	// 10. allowlist file exists
	allowBytes, err := qgshell.StatAndRead(allowlist)
	if err != nil {
		failures = append(failures, fmt.Sprintf("allowlist file exists: %s missing", allowlistRel))
	} else {
		// 11. script allowlisted (substring check on canonical name)
		if !bytes.Contains(allowBytes, []byte(allowlistTag)) {
			failures = append(failures, fmt.Sprintf("script allowlisted: %q not found in %s", allowlistTag, allowlistRel))
		}
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// runHelperJSON reproduces the bash invocation:
//
//	printf 'c1:PASS:OK\nc3:WARN:kubectl context unreachable\n' > T
//	CS_RESULTS_FILE=T CS_PASS=6 CS_WARN=1 CS_FAIL=0 CS_SKIP=1 CS_ELAPSED=12 python3 HELPER
func runHelperJSON(helper string) (_ []byte, err error) {
	tmp, err := os.CreateTemp("", "check-sprint-syntax-*.txt")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(tmp.Name())) }()
	if _, err := tmp.WriteString("c1:PASS:OK\nc3:WARN:kubectl context unreachable\n"); err != nil {
		return nil, fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close tmp: %w", err)
	}

	cmd := exec.Command("python3", helper)
	cmd.Env = append(os.Environ(),
		"CS_RESULTS_FILE="+tmp.Name(),
		"CS_PASS=6",
		"CS_WARN=1",
		"CS_FAIL=0",
		"CS_SKIP=1",
		"CS_ELAPSED=12",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("python3 %s: %w", helper, err)
	}
	return out, nil
}

// jsonEquals compares a JSON-decoded value against an expected int, tolerating
// the encoding/json float64 shape.
func jsonEquals(v any, want int) bool {
	switch t := v.(type) {
	case float64:
		return int(t) == want
	case int:
		return t == want
	}
	return false
}

// resultsFirstEquals asserts parsed["results"][0] == [id, status, detail].
func resultsFirstEquals(parsed map[string]any, id, status, detail string) bool {
	results, ok := parsed["results"].([]any)
	if !ok || len(results) == 0 {
		return false
	}
	first, ok := results[0].([]any)
	if !ok || len(first) != 3 {
		return false
	}
	return jsonStringEquals(first[0], id) && jsonStringEquals(first[1], status) && jsonStringEquals(first[2], detail)
}

func jsonStringEquals(v any, want string) bool {
	s, ok := v.(string)
	return ok && s == want
}
