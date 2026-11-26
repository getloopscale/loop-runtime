// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// Guard for issue #255: a gate can be implemented, registered, unit-tested —
// and never actually run. Every registered gate is EITHER wired into
// taskfiles/quality.yml OR carries a Wiring exemption on its own spec, so a new
// gate cannot be born orphaned in silence.
//
// It deliberately does not auto-classify: whether a gate should block a merge
// is a judgement about that gate, and a guess encoded here would be exactly the
// gate theatre #255 is about.

func loadQualityTaskfile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(qtestutil.MustRepoRoot(), "taskfiles", "quality.yml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// gateIsWired reports whether quality.yml invokes the gate. Matching the
// invocation shape (`quality <name>` then end of token) avoids the prefix
// collision a plain Contains would hit: "test-targets" must not be satisfied by
// a line invoking "test-targets-extra".
func gateIsWired(taskfile, gate string) bool {
	needle := "quality " + gate
	for _, line := range strings.Split(taskfile, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(needle):]
		if rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t") {
			return true
		}
	}
	return false
}

func TestEveryGateIsWiredOrExempted(t *testing.T) {
	taskfile := loadQualityTaskfile(t)
	for _, g := range Gates() {
		if !gateIsWired(taskfile, g.Name) && !g.Wiring.Exempt() {
			t.Errorf("gate %q is registered but neither wired into taskfiles/quality.yml nor given a Wiring "+
				"exemption on its spec. An unwired gate is dead code that reads as coverage (#255): wire it, "+
				"or declare why not (parameterized / utility / operatorRunOn).", g.Name)
		}
	}
}

// An exemption for a gate that HAS since been wired keeps alive a reason nobody
// needs — the shape by which allowlists rot into decoration.
func TestWiringExemptionsAreNotStale(t *testing.T) {
	taskfile := loadQualityTaskfile(t)
	for _, g := range Gates() {
		if g.Wiring.Exempt() && gateIsWired(taskfile, g.Name) {
			t.Errorf("gate %q IS wired into taskfiles/quality.yml but still carries an exemption (%s) — drop it",
				g.Name, g.Wiring)
		}
	}
}

// operatorRun is the only category that makes a factual claim about the world
// outside this repository. It is verified by declaration: name the external
// state, or the gate is hermetic and belongs in another category.
func TestOperatorRunNamesItsExternalInput(t *testing.T) {
	for _, g := range Gates() {
		if err := g.Wiring.Validate(); err != nil {
			t.Errorf("exemption for gate %q: %v", g.Name, err)
		}
	}
}

// Negative control: the invariant above can actually fail.
func TestOperatorRunWithEmptyExternalInputIsRejected(t *testing.T) {
	if err := operatorRunOn("").Validate(); !errors.Is(err, ErrUnnamedExternalInput) {
		t.Errorf("an operator-run exemption with no external input must be rejected; got %v", err)
	}
	if err := operatorRunOn("a live cluster").Validate(); err != nil {
		t.Errorf("a named external input must be accepted; got %v", err)
	}
	if err := utility.Validate(); err != nil {
		t.Errorf("the invariant applies to operator-run only; a utility exemption was rejected: %v", err)
	}
}

// Negative control: the wiring matcher detects an orphan and a prefix collision.
func TestGateIsWired_RejectsAbsentNamesAndPrefixCollisions(t *testing.T) {
	taskfile := loadQualityTaskfile(t)
	if !gateIsWired(taskfile, "no-new-bash") {
		t.Fatal("setup: no-new-bash is wired in quality.yml but gateIsWired says otherwise")
	}
	if gateIsWired(taskfile, "gate-that-does-not-exist-anywhere") {
		t.Error("gateIsWired matched a name absent from quality.yml — every orphan would pass")
	}
	if gateIsWired(taskfile, "no-new-bash-extended") {
		t.Error("gateIsWired matched on a shared prefix — orphans with a wired prefix would slip through")
	}
}

// Every category is held to the invariant, not only the ones in use today.
func TestEveryWiringCategorySatisfiesItsInvariant(t *testing.T) {
	for name, w := range wiringCategories {
		if err := w.Validate(); err != nil {
			t.Errorf("category %s does not satisfy its own invariant: %v", name, err)
		}
		if strings.TrimSpace(w.String()) == "" {
			t.Errorf("category %s renders empty — an exemption nobody can read is one nobody can audit", name)
		}
	}
}

// The operatorRun claim is turned from a declaration into a detection: each
// gate is run against an empty tree with its absent-able input removed, and
// must not reach a clean verdict. A gate that concludes cleanly did not need
// the input, so its exemption rests on a false claim. Ambient inputs (the
// wall clock) are excluded by name — see operatorRunOnAmbient.
func TestOperatorRun_AbsentableInputsAreProven(t *testing.T) {
	for _, v := range []string{"SPOTCHECK_SEED", "DRILL_DO_TOKEN", "DIGITALOCEAN_TOKEN", "DO_TOKEN", "DRILL_PULUMI_TOKEN"} {
		t.Setenv(v, "")
	}
	var probed int
	for _, g := range Gates() {
		if !g.Wiring.OperatorRun() || g.Wiring.Ambient() {
			continue
		}
		probed++
		var args []string
		if !g.NoRoot {
			args = []string{"--root", t.TempDir()}
		}
		var out bytes.Buffer
		if rc := g.Run(args, &out, &out); rc == 0 && !mentionsMissingInput(out.String()) {
			t.Errorf("%q is exempted as operatorRun on %q, but reached a clean verdict with that input absent. "+
				"Either it is hermetic and belongs in quality:gates, or the named input is the wrong one.\noutput: %s",
				g.Name, g.Wiring.ExternalInput(), truncate(out.String()))
		}
	}
	// A probe that ran zero times proves nothing (the `PASS — 0 routes` shape, B-040).
	if probed == 0 {
		t.Error("no absent-able operatorRun gate was probed — the registry is empty or the filter is wrong")
	}
}

// mentionsMissingInput reports whether the output says the gate could not
// proceed: a gate may exit 0 while SKIPping, which is not a verdict.
func mentionsMissingInput(out string) bool {
	low := strings.ToLower(out)
	for _, marker := range []string{"skip", "not set", "absent", "missing", "no such file", "error", "required"} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func truncate(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
