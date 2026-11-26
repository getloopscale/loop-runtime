// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package englishonly

import (
	"strings"
	"testing"
)

// Every Portuguese fixture below carries the exemption marker on its source
// line, so the gate that scans this repository skips the line while the test
// still receives the Portuguese value.

func TestClassify_FlagsUnaccentedPortugueseProse(t *testing.T) {
	line := "# o gate nao roda aqui porque isso ja foi medido" // english-only: fixture must be Portuguese to exercise the detector
	if got := Classify(line); !got.Portuguese {
		t.Fatalf("Classify(%q) = %+v, want Portuguese", line, got)
	}
}

func TestClassify_FlagsASingleAccentedPortugueseWord(t *testing.T) {
	line := "// Closes #37 cenário 4." // english-only: fixture must be Portuguese to exercise the detector
	if got := Classify(line); !got.Portuguese {
		t.Fatalf("Classify(%q) = %+v, want Portuguese", line, got)
	}
}

func TestClassify_FlagsTheApostropheStandInForAnAccent(t *testing.T) {
	line := "// e' o que o runbook pede, e so' isso" // english-only: fixture must be Portuguese to exercise the detector
	if got := Classify(line); !got.Portuguese {
		t.Fatalf("Classify(%q) = %+v, want Portuguese", line, got)
	}
}

func TestClassify_PassesEnglishProse(t *testing.T) {
	for _, line := range []string{
		"// The gate refuses a chart that renders nothing, and says which one.",
		"# Do not run this on main; a sweep over no files is not a pass.",
		"It is a matter of when, not if: the lock is taken inside the transaction.",
	} {
		if got := Classify(line); got.Portuguese {
			t.Errorf("Classify(%q) = %+v, want English", line, got)
		}
	}
}

func TestClassify_IgnoresIdentifiersPathsAndURLs(t *testing.T) {
	for _, line := range []string{
		"\tja, _ := json.Marshal(a)",
		"see https://example.com/nao/isso/aqui/depois for the rest",
		"cp infra/cenario-a/nao_existe.yaml /tmp/essa-pasta/",
		"	naoExiste := isso_aqui(depoisDisso)",
		"run `uma coisa que nao existe` against the cluster",
		"clean := \"could not apply the configuration — café, naïve\"",
	} {
		if got := Classify(line); got.Portuguese {
			t.Errorf("Classify(%q) = %+v, want not Portuguese", line, got)
		}
	}
}

func TestClassify_ReportsTheWordsThatDecided(t *testing.T) {
	line := "# isso nao existe" // english-only: fixture must be Portuguese to exercise the detector
	got := Classify(line)
	if !strings.Contains(strings.Join(got.Evidence, ","), "nao") {
		t.Fatalf("evidence %v does not name the word that decided", got.Evidence)
	}
}

func TestScanLines_ExemptionWithReasonPasses(t *testing.T) {
	src := "resp.Hint = \"Coletando dados agora, isso demora\" // " + marker + ": dashboard copy rendered in the Portuguese UI\n" // english-only: fixture must be Portuguese to exercise the detector
	findings, exempted, err := ScanLines("a.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	if exempted != 1 {
		t.Fatalf("exempted = %d, want 1", exempted)
	}
}

func TestScanLines_ExemptionWithoutReasonIsAFinding(t *testing.T) {
	src := "x := 1 // " + marker + ": quote\n"
	findings, _, err := ScanLines("a.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Kind != KindExemptionWithoutReason {
		t.Fatalf("findings = %+v, want one %s", findings, KindExemptionWithoutReason)
	}
}

func TestScanLines_PortugueseLineIsAFindingWithItsLineNumber(t *testing.T) {
	src := "first line in English\n# isso nao existe mais\n" // english-only: fixture must be Portuguese to exercise the detector
	findings, _, err := ScanLines("taskfiles/x.yml", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Line != 2 || findings[0].Kind != KindPortuguese {
		t.Fatalf("findings = %+v, want one Portuguese finding on line 2", findings)
	}
}
