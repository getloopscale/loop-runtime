// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package qgshell holds shared helpers for the hermetic shell-syntax quality
// gates (bash -n / strict-mode contract). Extracted from 3 byte-identical copies
// across the *syntax gate packages (M27 DRY: dry-knowledge-shell-syntax-contract).
package qgshell

import (
	"io"
	"os"
	"os/exec"
	"regexp"
)

// StatAndRead stats then reads a file, distinguishing "missing" from a read error.
func StatAndRead(path string) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// RunQuiet executes the command with stdout/stderr discarded. Returns nil on exit
// 0, error otherwise (distinguishes "binary missing on PATH" from "non-zero exit").
func RunQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// ExtractBraceBlock returns the body between the `{` matched by openRe and its
// balanced closing `}`. Returns ("", false) if openRe does not match or the
// braces are unbalanced. Extracted from the brace-balanced Caddyfile host-block
// extractor copy-pasted across the caddy gates (M27 DRY:
// dry-knowledge-caddy-block-extractor).
func ExtractBraceBlock(content string, openRe *regexp.Regexp) (string, bool) {
	loc := openRe.FindStringIndex(content)
	if loc == nil {
		return "", false
	}
	depth := 1
	start := loc[1]
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start:i], true
			}
		}
	}
	return "", false
}
