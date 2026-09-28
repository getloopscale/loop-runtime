// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package installgithooks validates the M8 1.5 pre-push hook and the shape of
// its installer.
//
// Migrated from infra/tests/check-install-git-hooks_test.sh (M-Migrate2 2026-07-07).
// Hermetic: NO network, NO real push.
package installgithooks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Config selects the tree to check.
type Config struct{ RepoRoot string }

// Outcome lists the assertions that failed.
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

// ErrFailures means at least one assertion failed.
var ErrFailures = errors.New("install-git-hooks assertions failed")

// Check asserts the hook and the installer are present, executable, parse,
// and carry the lines the push gate depends on.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, err
		}
	}
	hook := filepath.Join(root, "infra/scripts/local/git-hooks/pre-push")
	inst := filepath.Join(root, "infra/scripts/local/install-git-hooks.sh")

	var failures []string
	chk := func(name string, pass bool) {
		if !pass {
			failures = append(failures, name)
		}
	}

	hookBody, hookErr := readExecutable(hook)
	if hookErr != nil {
		failures = append(failures, fmt.Sprintf("read hook: %v", hookErr))
	}
	chk("hook runs task quality:gates", bytes.Contains(hookBody, []byte("task quality:gates")))
	chk("hook honors LOOPZERO_SKIP_PREPUSH escape", bytes.Contains(hookBody, []byte("LOOPZERO_SKIP_PREPUSH")))
	chk("hook bash -n clean", hookErr == nil && bashSyntax(hook))

	instBody, instErr := readExecutable(inst)
	if instErr != nil {
		failures = append(failures, fmt.Sprintf("read installer: %v", instErr))
	}
	chk("installer idempotent (install -m 0755)", bytes.Contains(instBody, []byte("install -m 0755")))
	chk("installer bash -n clean", instErr == nil && bashSyntax(inst))

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrFailures}, nil
	}
	return Outcome{}, nil
}

// readExecutable returns the file's content, or an error when it is missing,
// unreadable, or not executable.
func readExecutable(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("%s is not executable", path)
	}
	return os.ReadFile(path) // #nosec G304 -- fixed paths under the repo root
}

func bashSyntax(path string) bool {
	return exec.Command("bash", "-n", path).Run() == nil
}
