// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitclient

import "fmt"

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
