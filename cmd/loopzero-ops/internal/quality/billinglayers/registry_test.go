// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"strings"
	"testing"
)

const minimal = `
[ancora]
loopzero-stripe-webhook

[piso]
1

[camadas]
seed | loopzero | a.go,b.go | test:t_test.go | skips without a credential

[referencias]
c.go | it is the guard, not the layer
`

func TestCompleteRegistryIsParsed(t *testing.T) {
	r, err := ParseRegistry(strings.NewReader(minimal))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(r.Anchor) != 1 || r.Anchor[0] != "loopzero-stripe-webhook" {
		t.Fatalf("anchor: %+v", r.Anchor)
	}
	if r.Floor != 1 {
		t.Fatalf("floor: %d", r.Floor)
	}
	if len(r.Layers) != 1 || r.Layers[0].ID != "seed" || len(r.Layers[0].Files) != 2 {
		t.Fatalf("layers: %+v", r.Layers)
	}
	if r.Layers[0].Guard.Kind != GuardTest || r.Layers[0].Guard.Target != "t_test.go" {
		t.Fatalf("guard: %+v", r.Layers[0].Guard)
	}
	if len(r.References) != 1 || r.References[0].Reason == "" {
		t.Fatalf("references: %+v", r.References)
	}
}

// EC-3: a layer is several files. A single field would force duplicate ids.
func TestLayerAcceptsAListOfFiles(t *testing.T) {
	r, _ := ParseRegistry(strings.NewReader(minimal))
	if got := r.Layers[0].Files; got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("file list: %+v", got)
	}
}

func TestMalformedLineFails(t *testing.T) {
	for _, bad := range []string{
		"[camadas]\nseed | loopzero | a.go | test:t_test.go\n",   // 4 fields
		"[camadas]\nseed | loopzero | a.go | test:t_test.go |\n", // empty description
		"[camadas]\nseed | loopzero |  | test:t_test.go | why\n", // no file
		"[camadas]\nseed | loopzero | a.go | foo:x | why\n",      // unknown guard
		"[referencias]\nc.go\n",                              // reference without a reason
	} {
		if _, err := ParseRegistry(strings.NewReader(bad)); err == nil {
			t.Fatalf("a malformed line must fail: %q", bad)
		}
	}
}

// Two layers with the same id: one hides the other, and the floor stays satisfied.
func TestDuplicateIDFails(t *testing.T) {
	dup := "[camadas]\nseed | loopzero | a.go | test:t | x\nseed | loopzero | b.go | test:t | y\n"
	if _, err := ParseRegistry(strings.NewReader(dup)); err == nil {
		t.Fatal("a duplicate id must fail")
	}
}

func TestCommentsAndBlankLinesAreIgnored(t *testing.T) {
	withComment := "# comment\n\n" + minimal
	r, err := ParseRegistry(strings.NewReader(withComment))
	if err != nil || len(r.Layers) != 1 {
		t.Fatalf("a comment is not content: %v %+v", err, r)
	}
}

// EC-7: `Gate: X` and `gate:X` are the same intent.
func TestGuardNormalisesWhitespaceAndCase(t *testing.T) {
	r, err := ParseRegistry(strings.NewReader(
		"[ancora]\nx\n\n[piso]\n1\n\n[camadas]\nseed | loopzero | a.go |   Gate: billing-secrets   | x\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g := r.Layers[0].Guard
	if g.Kind != GuardGate || g.Target != "billing-secrets" {
		t.Fatalf("normalisation: %+v", g)
	}
}

// EC-4: deleting lines must not make the gate pass.
func TestMissingFloorFails(t *testing.T) {
	if _, err := ParseRegistry(strings.NewReader("[camadas]\nseed | loopzero | a.go | test:t | x\n")); err == nil {
		t.Fatal("a registry without a floor declares no minimum, and the minimum is what prevents self-silencing")
	}
}

func TestEmptyAnchorFails(t *testing.T) {
	noAnchor := "[piso]\n1\n\n[camadas]\nseed | loopzero | a.go | test:t | x\n"
	if _, err := ParseRegistry(strings.NewReader(noAnchor)); !errors.Is(err, ErrRegistryMalformed) {
		t.Fatalf("without an anchor there is no way to detect a new layer; err=%v", err)
	}
}

func TestDebtSectionIsParsed(t *testing.T) {
	r, err := ParseRegistry(strings.NewReader(minimal + "\n[debts]\nB-033 | approved | cost ceiling\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d, ok := r.Debts["B-033"]; !ok || d.Status != "approved" || d.Closes != "cost ceiling" {
		t.Fatalf("debts: %+v", r.Debts)
	}
}

// An unknown status is refused rather than read as open or closed: a status
// the gate cannot classify must not decide whether a layer is covered.
func TestDebtWithUnknownStatusIsMalformed(t *testing.T) {
	for _, bad := range []string{
		"[debts]\nB-033 | pending | x\n",
		"[debts]\nB-033 | approved\n",
		"[debts]\nB-033 | approved | x\nB-033 | raw | y\n",
		"[debts]\nB-033 | approved | \n",
	} {
		if _, err := ParseRegistry(strings.NewReader(minimal + "\n" + bad)); !errors.Is(err, ErrRegistryMalformed) {
			t.Errorf("%q: want ErrRegistryMalformed, got %v", bad, err)
		}
	}
}
