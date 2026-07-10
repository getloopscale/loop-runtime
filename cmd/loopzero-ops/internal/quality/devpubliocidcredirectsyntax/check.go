// Package devpubliocidcredirectsyntax is a hermetic contract test for
// infra/tests/e2e/dev-public-oidc-redirect_test.sh (M22 T2.1 Wave A gate 2/17).
//
// Reproduces 9 assertions from the bash test verbatim following M20 T2.1
// canonical shape (devpublicbillingportalsyntax) per M22 blueprint ADR-D1.
package devpubliocidcredirectsyntax

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
	scriptRel    = "infra/tests/e2e/dev-public-oidc-redirect_test.sh"
	allowlistRel = ".claude/rules/shell-scripts-allowlist.md"
	allowlistTag = "infra/tests/e2e/dev-public-oidc-redirect_test.sh"
)

var (
	strictModeRE       = regexp.MustCompile(`(?m)^set -euo pipefail`)
	logFuncRE          = regexp.MustCompile(`(?m)^log\(\) \{`)
	assertFuncRE       = regexp.MustCompile(`(?m)^assert\(\) \{`)
	testProviderFuncRE = regexp.MustCompile(`(?m)^test_provider\(\) \{`)
	oidcProviderRE     = regexp.MustCompile(`(?i)google|github|oidc`)
	kratosBrowserRE    = regexp.MustCompile(`/self-service/login/browser|/self-service/methods/oidc/callback`)
	exitCodeRE         = regexp.MustCompile(`(?m)^exit ([0-9]+)`)
	allowedExits       = map[string]bool{"0": true, "1": true, "2": true}
)

type Config struct {
	RepoRoot string
}

type Outcome struct {
	Failures []string
}

var ErrViolations = errors.New("check-dev-public-oidc-redirect-syntax hermetic contract violated")

func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	script := filepath.Join(root, scriptRel)
	allowlist := filepath.Join(root, allowlistRel)

	var failures []string

	scriptBytes, existsErr := statAndRead(script)
	if existsErr != nil {
		failures = append(failures, fmt.Sprintf("file exists: %s missing", scriptRel))
	}

	if existsErr == nil {
		if err := runQuiet("bash", "-n", script); err != nil {
			failures = append(failures, fmt.Sprintf("bash -n %s: %v", scriptRel, err))
		}
	}

	if scriptBytes != nil {
		content := string(scriptBytes)

		if !strictModeRE.MatchString(content) {
			failures = append(failures, "set -euo pipefail strict mode: not declared at line start")
		}
		if !logFuncRE.MatchString(content) {
			failures = append(failures, "log function defined: `log() {` not found at line start")
		}
		if !assertFuncRE.MatchString(content) {
			failures = append(failures, "assert function defined: `assert() {` not found at line start")
		}
		if !testProviderFuncRE.MatchString(content) {
			failures = append(failures, "test_provider function defined: `test_provider() {` not found at line start")
		}
		if !oidcProviderRE.MatchString(content) {
			failures = append(failures, "OIDC redirect probe: no reference to google|github|oidc (case-insensitive)")
		}
		if !kratosBrowserRE.MatchString(content) {
			failures = append(failures, "Kratos browser flow: no reference to /self-service/login/browser or /self-service/methods/oidc/callback")
		}

		matches := exitCodeRE.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if !allowedExits[m[1]] {
				failures = append(failures, fmt.Sprintf(
					"exit 0/1/2 contract: found foreign exit code %s (allowed: 0, 1, 2)", m[1]))
				break
			}
		}
	}

	allowBytes, err := statAndRead(allowlist)
	if err != nil {
		failures = append(failures, fmt.Sprintf("allowlist file exists: %s missing", allowlistRel))
	} else if !strings.Contains(string(allowBytes), allowlistTag) {
		failures = append(failures, fmt.Sprintf(
			"script allowlisted: %q not found in %s", allowlistTag, allowlistRel))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

func statAndRead(path string) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
