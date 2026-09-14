// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The doc comment in adapters.go carries a hand-maintained list of ports whose real
// implementations are required. Nothing checked that list and it drifted: commit
// ad6de675f (2026-03-19) deleted the HealthProber port together with its caller and
// its composition-root wiring, and the row naming it survived two later edits to the
// same block. These tests make the list a checked contract instead of a convention.

const (
	// moduleRoot is the api module root, relative to this package directory.
	moduleRoot = "../.."
	// markerLine opens the block of required implementations in adapters.go.
	markerLine = "Real implementations are required:"
)

// docRow is one "- Port -> path" row of that block.
type docRow struct {
	Port string
	Path string
}

var docRowRE = regexp.MustCompile(`^//\s*-\s*(\S+)\s*(?:\x{2192}|->)\s*(\S+)\s*$`)

// docContractRows parses the required-implementations block of adapters.go. It fails
// the test rather than returning an empty slice: emptying the block is the one way a
// guard over a list silently stops guarding.
func docContractRows(t *testing.T) []docRow {
	t.Helper()
	src, err := os.ReadFile("adapters.go")
	if err != nil {
		abs, _ := filepath.Abs("adapters.go")
		t.Fatalf("read %s: %v", abs, err)
	}
	var rows []docRow
	started := false
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if !started {
			started = strings.Contains(line, markerLine)
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break // reached `package adapters`
		}
		if m := docRowRE.FindStringSubmatch(line); m != nil {
			rows = append(rows, docRow{Port: m[1], Path: m[2]})
		}
	}
	if len(rows) == 0 {
		t.Fatal("parsed 0 rows from the required-implementations block of adapters.go - " +
			"the guard would pass vacuously; check the marker line and the row format")
	}
	return rows
}

// TestDocCommentPathsResolveOnDisk asserts that every path the block cites exists.
func TestDocCommentPathsResolveOnDisk(t *testing.T) {
	for _, row := range docContractRows(t) {
		rel := strings.TrimPrefix(row.Path, "api/")
		if _, err := os.Stat(filepath.Join(moduleRoot, rel)); err != nil {
			t.Errorf("doc comment cites %s for port %s, which is not on disk; "+
				"repoint the row at the real file or delete the row", row.Path, row.Port)
		}
	}
}
