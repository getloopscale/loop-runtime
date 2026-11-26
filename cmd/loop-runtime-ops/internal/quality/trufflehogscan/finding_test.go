// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trufflehogscan

import (
	"errors"
	"strings"
	"testing"
)

// fakeSecret is COMPOSED at runtime, never written as a literal.
//
// This file is scanned by the very gate it tests: a fixture with a token
// of the real shape would — correctly — be a new candidate, and the lock would fail
// itself. Composing keeps the assertion whole and the scan honest. Same remedy as
// `publishhygiene` (B-130), for the same reason.
func fakeSecret() string { return "dop" + "_v1_" + strings.Repeat("a", 64) }

// pgURI and credURI follow the same rule, and the first version of this slice forgot it:
// the literal `postgres://…` was spelled out here, got committed, and the gate
// FAILED the very push that carried it. Adjudicating it would put into the permanent list a string
// that exists only to test the gate.
func pgURI() string   { return "postgres" + "://u:p@h/db" }
func credURI() string { return "https" + "://u:p@h/x" }

func line(detector, file, raw string, verified bool) string {
	v := "false"
	if verified {
		v = "true"
	}
	return `{"SourceMetadata":{"Data":{"Git":{"commit":"abc","file":"` + file + `"}}},` +
		`"DetectorName":"` + detector + `","Raw":"` + raw + `","Verified":` + v + `}`
}

func TestParseExtractsDetectorFileAndLiveness(t *testing.T) {
	fs, err := ParseFindings(strings.NewReader(
		line("DigitalOceanToken", "infra/x.ts", fakeSecret(), true) + "\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(fs))
	}
	if fs[0].Detector != "DigitalOceanToken" || fs[0].File != "infra/x.ts" || !fs[0].Live {
		t.Fatalf("wrong fields: %+v", fs[0])
	}
}

// The identity goes into a VERSIONED file. Writing the raw value would leak exactly what
// this gate exists to prevent.
func TestIdentityDoesNotContainTheRawValue(t *testing.T) {
	s := fakeSecret()
	fs, _ := ParseFindings(strings.NewReader(line("DigitalOceanToken", "a.ts", s, false) + "\n"))
	id := fs[0].ID()
	if strings.Contains(id, s) || strings.Contains(id, s[:20]) {
		t.Fatalf("the identity carries secret material: %q", id)
	}
	if len(id) != 16 {
		t.Fatalf("the identity must be a short, stable hash; got %q", id)
	}
}

func TestIdentityIsStableAcrossRuns(t *testing.T) {
	l := line("Postgres", "a.md", pgURI(), false)
	a, _ := ParseFindings(strings.NewReader(l + "\n"))
	b, _ := ParseFindings(strings.NewReader(l + "\n"))
	if a[0].ID() != b[0].ID() {
		t.Fatalf("unstable identity: %q != %q", a[0].ID(), b[0].ID())
	}
	// A different detector with the same raw value is another candidate.
	c, _ := ParseFindings(strings.NewReader(line("URI", "a.md", pgURI(), false) + "\n"))
	if a[0].ID() == c[0].ID() {
		t.Fatal("different detectors need different identities")
	}
}

func TestNonJSONLineIsIgnored(t *testing.T) {
	in := "2026-08-18 info-0 trufflehog scanning...\n" +
		line("Docker", "a.yml", "x", false) + "\n"
	fs, err := ParseFindings(strings.NewReader(in))
	if err != nil {
		t.Fatalf("log noise is not an error: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(fs))
	}
}

// EC-3. A line that IS JSON and does not parse is DATA LOSS — not the absence of a finding.
// Skipping it silently is the exact shape of the defect this item fights.
func TestBrokenJSONFails(t *testing.T) {
	in := line("Docker", "a.yml", "x", false) + "\n" + `{"DetectorName":"Trunc` + "\n"
	_, err := ParseFindings(strings.NewReader(in))
	if !errors.Is(err, ErrUnparseable) {
		t.Fatalf("broken JSON must fail; err=%v", err)
	}
}

// EC-6. bufio.Scanner cuts at 64KiB SILENTLY.
func TestLongLineIsNotTruncated(t *testing.T) {
	long := line("PrivateKey", "k.pem", strings.Repeat("A", 200*1024), false)
	fs, err := ParseFindings(strings.NewReader(long + "\n"))
	if err != nil {
		t.Fatalf("long line: %v", err)
	}
	if len(fs) != 1 || fs[0].Detector != "PrivateKey" {
		t.Fatalf("long finding lost: %+v", fs)
	}
}

// EC-5. Measured: 3 of the 179 findings in this tree have no file (they come from a commit message).
func TestFindingWithoutFileStillHasIdentity(t *testing.T) {
	fs, err := ParseFindings(strings.NewReader(
		`{"SourceMetadata":{"Data":{"Git":{"commit":"abc"}}},"DetectorName":"Gitlab","Raw":"g","Verified":false}` + "\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) != 1 || fs[0].File != "" || fs[0].ID() == "" {
		t.Fatalf("a finding without a file needs a valid identity: %+v", fs)
	}
}
