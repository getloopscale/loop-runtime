// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellgen

import "strings"

// ShellQuote wraps a string in POSIX-safe single quotes.
// This is the only correct way to embed arbitrary strings in shell scripts.
//
// Rule: wrap in single quotes, escape internal single quotes as '\” (end quote,
// escaped literal quote, reopen quote). Inside single quotes, no other character
// has special meaning — $, `, ", \, newlines are all literal.
//
// Examples:
//
//	ShellQuote("hello")          → 'hello'
//	ShellQuote("it's")           → 'it'\''s'
//	ShellQuote("$(whoami)")      → '$(whoami)'
//	ShellQuote("'; rm -rf /")    → ''\''; rm -rf /'
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
