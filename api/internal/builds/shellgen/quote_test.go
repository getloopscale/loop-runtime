// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellgen

import "testing"

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello", "'hello'"},
		{"with spaces", "hello world", "'hello world'"},
		{"with single quote", "it's", `'it'\''s'`},
		{"command substitution", "$(whoami)", "'$(whoami)'"},
		{"backtick injection", "`rm -rf /`", "'`rm -rf /`'"},
		{"double quotes", `say "hello"`, `'say "hello"'`},
		{"semicolon injection", "'; rm -rf /", `''\''; rm -rf /'`},
		{"dollar variable", "$HOME", "'$HOME'"},
		{"newline", "line1\nline2", "'line1\nline2'"},
		{"tab", "col1\tcol2", "'col1\tcol2'"},
		{"empty string", "", "''"},
		{"backslash", `path\to\file`, `'path\to\file'`},
		{"multiple single quotes", "it's a 'test'", `'it'\''s a '\''test'\'''`},
		{"glob chars", "*.txt", "'*.txt'"},
		{"pipe", "cat | grep foo", "'cat | grep foo'"},
		{"ampersand", "cmd & echo pwned", "'cmd & echo pwned'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShellQuote(tt.input)
			if got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
