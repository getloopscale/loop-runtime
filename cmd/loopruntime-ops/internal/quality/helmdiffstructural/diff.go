// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package helmdiffstructural

import (
	"errors"
	"os/exec"
)

// runDiff shells out to `diff -u <base> <pr>`. Returns stdout (unified
// diff body). `diff` exit code 1 means "files differ" — not an error
// from our perspective; we extract the body either way. Real errors
// (exit > 1) return an error.
func runDiff(basePath, prPath string) (string, error) {
	cmd := exec.Command("diff", "-u", basePath, prPath) //nolint:gosec // operator-controlled
	out, err := cmd.Output()
	if err != nil {
		// `diff` returns exit 1 when files differ — we still want stdout.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return string(out), nil
		}
		return "", err
	}
	return string(out), nil
}
