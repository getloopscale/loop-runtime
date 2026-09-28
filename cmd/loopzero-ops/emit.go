// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import "io"

// emit writes a subcommand's result — out to stdout, msg to stderr — and
// returns its exit code. A result that could not be written was not delivered,
// which is exit 5: the run could not hand its verdict to the caller.
func emit(stdout, stderr io.Writer, code int, out, msg string) int {
	if out != "" {
		if _, err := io.WriteString(stdout, out); err != nil {
			return 5
		}
	}
	if msg != "" {
		if _, err := io.WriteString(stderr, msg); err != nil {
			return 5
		}
	}
	return code
}
