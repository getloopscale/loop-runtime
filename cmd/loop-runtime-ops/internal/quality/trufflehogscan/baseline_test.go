// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trufflehogscan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mk(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func found(t *testing.T, lines ...string) []Finding {
	t.Helper()
	fs, err := ParseFindings(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestCandidateOutsideTheBaselineIsNew(t *testing.T) {
	fs := found(t,
		line("Postgres", "a.md", pgURI(), false),
		line("Docker", "b.yml", "new", false))
	b := Baseline{Entries: []Entry{{ID: fs[0].ID(), Detector: "Postgres", File: "a.md"}}}

	newOnes := b.Unadjudicated(fs)
	if len(newOnes) != 1 || newOnes[0].Detector != "Docker" {
		t.Fatalf("only the candidate absent from the baseline is new; got %+v", newOnes)
	}
}

// A stale entry is ratchet hygiene — it REPORTS, it does not fail. A secret removed from the
// tree cannot become a reason for the gate to lock.
func TestBaselineEntryNotFoundIsStale(t *testing.T) {
	fs := found(t, line("Postgres", "a.md", pgURI(), false))
	b := Baseline{Entries: []Entry{
		{ID: fs[0].ID(), Detector: "Postgres", File: "a.md"},
		{ID: "0000000000000000", Detector: "Eraser", File: "gone.md"},
	}}
	if st := b.Stale(fs); len(st) != 1 || st[0].Detector != "Eraser" {
		t.Fatalf("an entry with no matching finding is stale; got %+v", st)
	}
	if n := b.Unadjudicated(fs); len(n) != 0 {
		t.Fatalf("nothing is new here; got %+v", n)
	}
}

// EC-2. Deleting the file cannot become a bypass: absent is a FAILURE, never an implicit empty baseline.
func TestMissingBaselineIsNotAnEmptyBaseline(t *testing.T) {
	_, err := LoadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("a missing baseline must fail with a typed error; err=%v", err)
	}
}

func TestUnreadableBaselineFails(t *testing.T) {
	if _, err := LoadBaseline(mk(t, "{this is not json")); err == nil {
		t.Fatal("an unreadable baseline cannot be read as empty")
	}
}

// EC-1. The path through which the mechanism would become the opposite of itself.
func TestWriteRefusesWhileALiveFindingExists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.json")
	fs := found(t, line("DigitalOceanToken", "x.ts", fakeSecret(), true))
	if err := WriteBaseline(p, fs); !errors.Is(err, ErrLiveSecret) {
		t.Fatalf("rewriting cannot silence a live credential; err=%v", err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("the refusal must happen BEFORE writing — the file must not exist")
	}
}

func TestWrittenBaselineIsDeterministicAndSecretFree(t *testing.T) {
	s := fakeSecret()
	fs := found(t,
		line("URI", "z.md", credURI(), false),
		line("Postgres", "a.md", pgURI(), false),
		line("DigitalOceanToken", "m.ts", s, false))

	d := t.TempDir()
	p1, p2 := filepath.Join(d, "1.json"), filepath.Join(d, "2.json")
	for _, p := range []string{p1, p2} {
		if err := WriteBaseline(p, fs); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Fatal("rewriting twice must give identical bytes — otherwise the diff is noise")
	}
	if strings.Contains(string(a), s) || strings.Contains(string(a), s[:20]) {
		t.Fatal("the versioned baseline carries secret material")
	}
	loaded, err := LoadBaseline(p1)
	if err != nil || len(loaded.Entries) != 3 {
		t.Fatalf("round trip: %v %+v", err, loaded)
	}
	if loaded.Entries[0].File > loaded.Entries[2].File {
		t.Fatal("the entries must come out sorted")
	}
}

// Duplicate findings (the same secret in several commits) are ONE candidate.
func TestRepeatedFindingsAreOneEntry(t *testing.T) {
	l := line("Postgres", "a.md", pgURI(), false)
	fs := found(t, l, l, l)
	p := filepath.Join(t.TempDir(), "b.json")
	if err := WriteBaseline(p, fs); err != nil {
		t.Fatal(err)
	}
	b, _ := LoadBaseline(p)
	if len(b.Entries) != 1 {
		t.Fatalf("179 findings are 65 identities; got %d", len(b.Entries))
	}
}
