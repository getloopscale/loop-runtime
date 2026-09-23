// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package coverageparse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseProfileLine_HappyPath(t *testing.T) {
	b, ok := ParseProfileLine("api/foo.go:10.2,12.10 3 5")
	assert.True(t, ok)
	assert.Equal(t, ProfileBlock{File: "api/foo.go", StartLine: 10, EndLine: 12, Stmts: 3, Count: 5}, b)
}

func TestParseProfileLine_Malformed(t *testing.T) {
	for _, line := range []string{
		"",                   // empty
		"no-colon-here 1 1",  // no colon
		"x.go:no-range 1 1",  // no comma in range
		"x.go:1.0,2.0",       // missing stmts/count
		"x.go:1.0,2.0 N C",   // non-numeric
		"x.go:bad.0,2.0 1 1", // non-numeric start line
		"x.go:1.0,bad.0 1 1", // non-numeric end line
		"x.go:1,2 1 1",       // no dot in range
		"x.go:0.0,2.0 1 1",   // start line 0 (invalid)
		"x.go:5.0,3.0 1 1",   // end < start
	} {
		_, ok := ParseProfileLine(line)
		assert.False(t, ok, "should reject: %q", line)
	}
}

// The range after the file carries no colon, so the LAST colon separates the
// file; a Windows path carries one of its own.
func TestParseProfileLine_WindowsPath_LastColonWins(t *testing.T) {
	b, ok := ParseProfileLine(`C:\repo\foo.go:10.2,12.10 3 5`)
	assert.True(t, ok)
	assert.Equal(t, `C:\repo\foo.go`, b.File)
	assert.Equal(t, 10, b.StartLine)
}

// ParseGoProfile split at the FIRST colon, so a drive-letter path was
// attributed to a file named "C" and dropped from every package.
func TestParseGoProfile_DriveLetterPathKeepsItsPackage(t *testing.T) {
	pkgs, err := ParseGoProfile(strings.NewReader("mode: set\nC:/repo/pkg/foo.go:1.1,2.2 4 1\n"))
	assert.NoError(t, err)
	if assert.Len(t, pkgs, 1) {
		assert.Equal(t, "C:/repo/pkg", pkgs[0].Package)
	}
}
