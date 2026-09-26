// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package englishonly

import (
	"reflect"
	"testing"
)

func TestSplitName_SplitsCamelSnakeKebabAndDigits(t *testing.T) {
	cases := map[string][]string{
		"RunCounterProof":      {"run", "counter", "proof"},
		"HTTPProbeURL":         {"http", "probe", "url"},
		"Layer5_ProofOfLife":   {"layer", "proof", "of", "life"},
		"layers_live_test.go":  {"layers", "live", "test", "go"},
		"counter-proof:v2":     {"counter", "proof", "v"},
		"PARTIALLY_APPROVED":   {"partially", "approved"},
		"tenantFixtureProject": {"tenant", "fixture", "project"},
		"":                     nil,
		"x":                    {"x"},
	}
	for in, want := range cases {
		if got := splitName(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScanLines_PortugueseGoIdentifierIsAFinding(t *testing.T) {
	src := "package x\n\nfunc RunContraProva() {}\n" // english-only: fixture must be Portuguese to exercise the gate
	findings, _, err := ScanLines("infra/tests/x.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Kind != KindPortugueseIdentifier || findings[0].Line != 3 || findings[0].Text != "RunContraProva" { // english-only: fixture must be Portuguese to exercise the gate
		t.Fatalf("findings = %+v, want one identifier finding for RunContraProva on line 3", findings) // english-only: fixture must be Portuguese to exercise the gate
	}
}

func TestScanLines_EnglishGoIdentifierPasses(t *testing.T) {
	findings, _, err := ScanLines("infra/tests/x.go", "package x\n\nfunc RunCounterProof() {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for an English identifier", findings)
	}
}

func TestScanLines_PortugueseWordInAGoStringOrCommentIsNotAnIdentifier(t *testing.T) {
	// A string is a value and a comment is prose: the prose detector reads the
	// comment, and a lone word there is below its threshold on purpose.
	src := "package x\n\n// camada\nvar layer = \"camada\"\n" // english-only: fixture must be Portuguese to exercise the gate
	findings, _, err := ScanLines("infra/tests/x.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: neither a string nor a comment is an identifier", findings)
	}
}

func TestScanLines_IdentifierExemptionWithAReasonKeepsIt(t *testing.T) {
	src := "package x\n\nconst Aprovada = 1 // english-only: persisted enum value read by an external consumer\n" // english-only: fixture must be Portuguese to exercise the gate
	findings, exempted, err := ScanLines("infra/tests/x.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || exempted != 1 {
		t.Fatalf("findings = %+v, exempted = %d; want the identifier kept by its exemption", findings, exempted)
	}
}

func TestScanLines_IdentifiersOutsideGoFilesAreNotRead(t *testing.T) {
	// Only Go is tokenised: in YAML, shell or TypeScript a word-level guess at
	// what is an identifier would mistake values and prose for names.
	findings, _, err := ScanLines("taskfiles/x.yml", "tasks:\n  camadas:\n") // english-only: fixture must be Portuguese to exercise the gate
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none outside .go files", findings)
	}
}

func TestCheck_PortugueseDirectoryNameIsOneFinding(t *testing.T) {
	root := newRepo(t)
	trackFile(t, root, "infra/tests/contraprova/a.md", "English.\n") // english-only: fixture must be Portuguese to exercise the gate
	trackFile(t, root, "infra/tests/contraprova/b.md", "English.\n") // english-only: fixture must be Portuguese to exercise the gate
	r, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Kind != KindPortuguesePath || r.Findings[0].Path != "infra/tests/contraprova" { // english-only: fixture must be Portuguese to exercise the gate
		t.Fatalf("findings = %+v, want the directory reported once", r.Findings)
	}
}

func TestCheck_PortugueseFileNameIsAFinding(t *testing.T) {
	root := newRepo(t)
	trackFile(t, root, "infra/tests/camadas_live.md", "English.\n") // english-only: fixture must be Portuguese to exercise the gate
	r, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Kind != KindPortuguesePath || r.Findings[0].Path != "infra/tests/camadas_live.md" { // english-only: fixture must be Portuguese to exercise the gate
		t.Fatalf("findings = %+v, want the file name reported", r.Findings)
	}
}

func TestCheck_EnglishPathsPass(t *testing.T) {
	root := newRepo(t)
	trackFile(t, root, "infra/tests/counterproof/layers_live.md", "English.\n")
	r, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 0 {
		t.Fatalf("findings = %+v, want none for English paths", r.Findings)
	}
}

func TestNameWords_NoneIsAnEnglishWordTheProseListsAccept(t *testing.T) {
	// A name word must never be an English word the prose detector already
	// weighs as English: that would flag an English identifier.
	for w := range nameWords {
		if englishWord[w] || loanwords[w] {
			t.Errorf("words/names.txt holds %q, which the gate itself treats as English", w)
		}
	}
}
