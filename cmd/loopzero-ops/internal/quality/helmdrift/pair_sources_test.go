// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// B-001 T5.1. "Declared pair" means two different things in this repository, and
// the difference is recorded in this package's doc comment rather than unified.
//
// A note nobody checks is the shape this whole item exists to refuse, so the note
// is checked: this test fails if the two sources ever agree — at which point the
// note has become false and must go — and fails if the note is no longer there.
//
// It reads directory names and one text file. No helm, so it stays in the
// hermetic set.
func TestThePairSourcesDivergeAndTheDivergenceIsRecorded(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..")

	pairs, err := discoverPairs(root)
	if err != nil {
		t.Fatalf("discoverPairs: %v", err)
	}
	audited := map[string]bool{}
	for _, p := range pairs {
		audited[p.published] = true
	}
	if len(audited) == 0 {
		t.Fatal("this gate audits no pair at all — the comparison below would be vacuous")
	}

	raw, err := os.ReadFile(filepath.Join(root, "infra", "quality", "chart-pairs.txt"))
	if err != nil {
		t.Fatalf("the written pair registry the other gate reads is unreadable: %v", err)
	}
	declared := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		declared[strings.TrimSpace(strings.SplitN(line, "|", 2)[0])] = true
	}
	if len(declared) == 0 {
		t.Fatal("infra/quality/chart-pairs.txt declares no pair — nothing to compare against")
	}

	var onlyAudited, onlyDeclared []string
	for name := range audited {
		if !declared[name] {
			onlyAudited = append(onlyAudited, name)
		}
	}
	for name := range declared {
		if !audited[name] {
			onlyDeclared = append(onlyDeclared, name)
		}
	}
	sort.Strings(onlyAudited)
	sort.Strings(onlyDeclared)

	if len(onlyAudited) == 0 && len(onlyDeclared) == 0 {
		t.Fatal("this gate and infra/quality/chart-pairs.txt now name the same pairs. The " +
			"divergence recorded in this package's doc comment has become false: delete the " +
			"\"Declared pair means two different things\" section, and delete this test with it.")
	}
	t.Logf("audited-only: %v; declared-only: %v", onlyAudited, onlyDeclared)

	doc, err := os.ReadFile("check.go")
	if err != nil {
		t.Fatalf("reading the package doc: %v", err)
	}
	for _, want := range []string{
		"infra/quality/chart-pairs.txt",
		"Declared pair",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("the package doc no longer records the divergence (missing %q), while the "+
				"two sources still disagree — which leaves the next reader to rediscover it", want)
		}
	}
}
