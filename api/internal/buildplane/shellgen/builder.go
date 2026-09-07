// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package shellgen provides a type-safe shell script builder that replaces
// raw fmt.Sprintf shell generation. All interpolated values are escaped via
// ShellQuote (POSIX single-quote), preventing shell injection.
package shellgen

import "strings"

// ScriptBuilder constructs shell scripts line by line with safe quoting.
type ScriptBuilder struct {
	lines []string
}

// CondBuilder handles conditional blocks (if/then/else/fi).
type CondBuilder struct {
	parent *ScriptBuilder
	body   *ScriptBuilder
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

// Run adds a command with all arguments safely quoted.
func (b *ScriptBuilder) Run(cmd string, args ...string) *ScriptBuilder {
	parts := []string{cmd}
	for _, a := range args {
		parts = append(parts, ShellQuote(a))
	}
	b.lines = append(b.lines, strings.Join(parts, " "))
	return b
}

// Raw adds a line verbatim — escape hatch for complex shell constructs.
// Use sparingly; prefer the typed methods.
func (b *ScriptBuilder) Raw(line string) *ScriptBuilder {
	b.lines = append(b.lines, line)
	return b
}

// Comment adds a shell comment.
func (b *ScriptBuilder) Comment(text string) *ScriptBuilder {
	b.lines = append(b.lines, "# "+text)
	return b
}

// Blank adds an empty line for readability.
func (b *ScriptBuilder) Blank() *ScriptBuilder {
	b.lines = append(b.lines, "")
	return b
}

// IfFileExists starts a conditional block: if [ -f 'path' ]; then
func (b *ScriptBuilder) IfFileExists(path string) *CondBuilder {
	return &CondBuilder{
		parent: b,
		body: &ScriptBuilder{
			lines: []string{"if [ -f " + ShellQuote(path) + " ]; then"},
		},
	}
}

// Quality-Gate Phase 2: IfDirExists removed — no callers; sibling
// IfFileExists is used. Restore alongside a real caller when needed.

// Then adds lines to the conditional body and returns a builder for chaining.
func (c *CondBuilder) Then(fn func(b *ScriptBuilder)) *CondBuilder {
	inner := New()
	fn(inner)
	for _, line := range inner.lines {
		c.body.lines = append(c.body.lines, "  "+line)
	}
	return c
}

// Else adds an else branch.
func (c *CondBuilder) Else(fn func(b *ScriptBuilder)) *CondBuilder {
	c.body.lines = append(c.body.lines, "else")
	inner := New()
	fn(inner)
	for _, line := range inner.lines {
		c.body.lines = append(c.body.lines, "  "+line)
	}
	return c
}

// EndIf closes the if block and returns to the parent builder.
func (c *CondBuilder) EndIf() *ScriptBuilder {
	c.body.lines = append(c.body.lines, "fi")
	c.parent.lines = append(c.parent.lines, c.body.lines...)
	return c.parent
}

// Append adds all lines from another builder.
func (b *ScriptBuilder) Append(other *ScriptBuilder) *ScriptBuilder {
	b.lines = append(b.lines, other.lines...)
	return b
}

// String returns the final script as a single string.
func (b *ScriptBuilder) String() string {
	return strings.Join(b.lines, "\n")
}
