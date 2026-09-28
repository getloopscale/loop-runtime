// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package shellgen accumulates the lines of a build-step shell script.
//
// What it guarantees is narrow: Cd, Echo, Env and MkdirP quote their value with
// ShellQuote. Raw appends a line verbatim, and most build scripts are built
// with Raw, so a value interpolated into a Raw line is safe only if the caller
// quoted it with ShellQuote. The package does not make that automatic.
package shellgen

import "strings"

// ScriptBuilder accumulates script lines.
type ScriptBuilder struct {
	lines []string
}

// New creates a new ScriptBuilder.
func New() *ScriptBuilder {
	return &ScriptBuilder{}
}

// SetE adds `set -euo pipefail` at the current position.
func (b *ScriptBuilder) SetE() *ScriptBuilder {
	b.lines = append(b.lines, "set -euo pipefail")
	return b
}

// Cd adds a cd command with the path safely quoted.
func (b *ScriptBuilder) Cd(dir string) *ScriptBuilder {
	b.lines = append(b.lines, "cd "+ShellQuote(dir))
	return b
}

// Echo adds an echo command with the message safely quoted.
func (b *ScriptBuilder) Echo(msg string) *ScriptBuilder {
	b.lines = append(b.lines, "echo "+ShellQuote(msg))
	return b
}

// Env adds an export statement with the value safely quoted.
func (b *ScriptBuilder) Env(key, val string) *ScriptBuilder {
	b.lines = append(b.lines, "export "+key+"="+ShellQuote(val))
	return b
}

// MkdirP adds a mkdir -p command with the path safely quoted.
func (b *ScriptBuilder) MkdirP(dir string) *ScriptBuilder {
	b.lines = append(b.lines, "mkdir -p "+ShellQuote(dir))
	return b
}

// Comment adds a shell comment.
func (b *ScriptBuilder) Comment(text string) *ScriptBuilder {
	b.lines = append(b.lines, "# "+text)
	return b
}

// Raw adds a line verbatim. Any value interpolated into it must already be
// quoted with ShellQuote.
func (b *ScriptBuilder) Raw(line string) *ScriptBuilder {
	b.lines = append(b.lines, line)
	return b
}

// Blank adds an empty line for readability.
func (b *ScriptBuilder) Blank() *ScriptBuilder {
	b.lines = append(b.lines, "")
	return b
}

// String returns the final script as a single string.
func (b *ScriptBuilder) String() string {
	return strings.Join(b.lines, "\n")
}
