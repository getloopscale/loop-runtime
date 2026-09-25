// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitclient

import "fmt"

// HTTPStatusError is a Gitea API call answered with an unexpected status. It
// keeps the status as a number so callers classify it with errors.As instead
// of searching the message for digits.
type HTTPStatusError struct {
	Op         string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s: status %d", e.Op, e.StatusCode)
}

// CommandError is a git command that exited unsuccessfully. Output is git's
// combined output with the credential removed; Err is the exec error (exit
// status), so errors.As reaches both.
type CommandError struct {
	Args   string
	Output string
	Err    error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("git %s: %v (output: %s)", e.Args, e.Err, e.Output)
}

func (e *CommandError) Unwrap() error { return e.Err }
