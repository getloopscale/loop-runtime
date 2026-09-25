// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contractconsumers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A field that never reached the wire (a nil pointer with omitempty, a type
// no route returns) can be deleted without touching the contract, but the
// gate cannot know that: it sees a tag leave and consumers that read a field
// of that name. An acknowledgement records the decision per (tag, file), with
// its reason, so the drop stays visible and stops blocking every push.

// dropTree commits base with the given files, then head with them rewritten,
// and adds an api-side consumer that reads `.gone`.
func dropTree(t *testing.T, base, head map[string]string) string {
	t.Helper()
	root := gitTree(t)
	write := func(files map[string]string) {
		for rel, body := range files {
			p := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(base)
	write(map[string]string{"api/internal/reader/reader.go": "package reader\n\nfunc r(x struct{ gone int }) int { return x.gone }\n"})
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")
	write(head)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "head")
	return root
}

func writeAcks(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(root, "acks.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const withGone = "package a\n\ntype A struct {\n\tKeep string `json:\"keep\"`\n\tGone *string `json:\"gone,omitempty\"`\n}\n"
const withoutGone = "package a\n\ntype A struct {\n\tKeep string `json:\"keep\"`\n}\n"

func TestAnAcknowledgedDropPassesAndStaysVisible(t *testing.T) {
	root := dropTree(t,
		map[string]string{"api/internal/routes/a/a.go": withGone},
		map[string]string{"api/internal/routes/a/a.go": withoutGone})
	acks := writeAcks(t, root, "gone | api/internal/routes/a/a.go | a nil pointer with omitempty that no handler ever set\n")

	out, err := Check(Config{RepoRoot: root, BaseRef: "HEAD~1", AcknowledgementsFile: acks})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass || len(out.Findings) != 0 {
		t.Fatalf("an acknowledged drop must pass; got pass=%v findings=%+v", out.Pass, out.Findings)
	}
	if len(out.Acknowledged) != 1 || out.Acknowledged[0] != "gone" {
		t.Errorf("the acknowledged drop must still be reported, got %v", out.Acknowledged)
	}
}

func TestADropAcknowledgedInOnlyOneOfItsFilesStillFails(t *testing.T) {
	root := dropTree(t,
		map[string]string{"api/internal/routes/a/a.go": withGone, "api/internal/routes/b/b.go": strings.Replace(withGone, "package a", "package b", 1)},
		map[string]string{"api/internal/routes/a/a.go": withoutGone, "api/internal/routes/b/b.go": strings.Replace(withoutGone, "package a", "package b", 1)})
	acks := writeAcks(t, root, "gone | api/internal/routes/a/a.go | a nil pointer with omitempty that no handler ever set\n")

	out, err := Check(Config{RepoRoot: root, BaseRef: "HEAD~1", AcknowledgementsFile: acks})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass || len(out.Findings) == 0 {
		t.Fatalf("the drop from b.go was never acknowledged and a consumer reads it; got pass=%v findings=%+v", out.Pass, out.Findings)
	}
}

// An acknowledgement no current drop matches has outlived its reason (the
// base moved past the drop, or the file was restored); left in place it would
// pre-approve the next removal of that tag from that file.
func TestAStaleAcknowledgementFails(t *testing.T) {
	root := dropTree(t,
		map[string]string{"api/internal/routes/a/a.go": withGone},
		map[string]string{"api/internal/routes/a/a.go": withGone + "\n// unchanged contract\n"})
	acks := writeAcks(t, root, "gone | api/internal/routes/a/a.go | a nil pointer with omitempty that no handler ever set\n")

	out, err := Check(Config{RepoRoot: root, BaseRef: "HEAD~1", AcknowledgementsFile: acks})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass || len(out.StaleAcknowledgements) != 1 {
		t.Fatalf("an acknowledgement matching no drop must fail the gate; got pass=%v stale=%v", out.Pass, out.StaleAcknowledgements)
	}
}

func TestAnAcknowledgementWithoutAReasonIsRefused(t *testing.T) {
	root := dropTree(t,
		map[string]string{"api/internal/routes/a/a.go": withGone},
		map[string]string{"api/internal/routes/a/a.go": withoutGone})
	acks := writeAcks(t, root, "gone | api/internal/routes/a/a.go |   \n")

	_, err := Check(Config{RepoRoot: root, BaseRef: "HEAD~1", AcknowledgementsFile: acks})
	if !errors.Is(err, ErrMalformedAcknowledgement) {
		t.Fatalf("an acknowledgement nobody justified must be refused, got %v", err)
	}
}
