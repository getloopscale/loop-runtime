// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Guard for issue #255: a gate can be implemented, registered, unit-tested —
// and never actually run. 39 of 73 gates were in that state when this guard
// was written, and nothing in the repo said so. `loopzero-ops quality --list`
// renders a `-` in the TASKS column for them, but `-` is ambiguous: it reads
// the same for a comparator that takes mandatory flags (correctly unwired) and
// for a scan gate somebody forgot to add to taskfiles/quality.yml.
//
// This guard removes the ambiguity by construction. Every registered gate must
// be EITHER wired into taskfiles/quality.yml OR declared below with a reason.
// A new gate therefore cannot be born orphaned in silence: the author has to
// wire it or say, in writing, why not.
//
// It deliberately does NOT try to auto-classify. Whether a gate should block a
// merge is a judgement call about that gate, and a guess encoded here would be
// exactly the gate-theatre #255 is about.
//
// NOTE ON HONESTY: entries tagged unwiredDebt are GRANDFATHERED, not endorsed.
// They record pre-existing debt so the bleeding stops while the backlog is
// worked down — the same pattern .claude/rules/shell-scripts-allowlist.md uses
// for shell scripts. Reclassifying one to a real category, or wiring it, is
// the way an entry leaves this list. Adding a NEW unwiredDebt entry is a smell
// that should not survive review.
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const qualityTaskfileRelPath = "../../taskfiles/quality.yml"

// wiringReason categorises why a registered gate is absent from quality.yml.
type wiringReason string

const (
	// parameterized: requires mandatory flags, so a bare invocation from a
	// task would always fail. Invoked by a caller that supplies them.
	parameterized wiringReason = "parameterized — takes mandatory flags; a bare run from quality:gates would fail"

	// utility: produces input for other gates rather than a verdict.
	utility wiringReason = "utility — emits data consumed by other gates, has no pass/fail verdict of its own"

	// operatorRun: needs live cluster state, a ceremony, or a schedule.
	operatorRun wiringReason = "operator-run — needs live cluster state / a ceremony / a cadence, not a hermetic repo scan"

	// unwiredDebt: no reason beyond nobody wired it. Pre-existing #255 debt.
	unwiredDebt wiringReason = "TODO(#255) — genuinely unwired; needs a decision on whether it blocks or is advisory"

	// failingGate: runs, and reports a REAL finding today. Distinct from
	// brokenGate (which reports a wrong verdict): here the gate is right and
	// the tree is wrong. Wiring it before the findings are addressed would
	// turn quality:gates red for reasons unrelated to whatever change is
	// being tested, which is how a suite gets ignored.
	failingGate wiringReason = "TODO(#255) — WORKS, and currently FAILS on a real finding; fix the finding, then wire"

	// slowGate: exceeds the budget for a gate meant to run on every push.
	slowGate wiringReason = "TODO(#255) — exceeds a 25s budget; needs profiling or a narrower scan before it can gate every push"

	// vacuousGate: exits 0 without asserting anything in this repo, so wiring
	// it would buy coverage that does not exist. Usually means the subject
	// moved (the M2/M4 extractions) or the gate advises rather than blocks.
	vacuousGate wiringReason = "TODO(#255) — passes VACUOUSLY here; wiring it would read as coverage while asserting nothing"

	// brokenGate: measured to produce a wrong verdict today. Distinct from
	// unwiredDebt on purpose — "nobody wired it" and "it does not work" call
	// for different fixes, and collapsing them loses the one fact a reader
	// needs. Wiring a gate in this state would make quality:gates red for a
	// reason that has nothing to do with the code under test.
	brokenGate wiringReason = "TODO(#255) — BROKEN: produces a wrong verdict; must be repaired before it can be wired"
)

// gateWiringExemptions is the ledger. Key = gate name, value = why it is not
// in taskfiles/quality.yml.
var gateWiringExemptions = map[string]wiringReason{
	// --- takes mandatory flags -------------------------------------------
	"helm-diff-structural": parameterized, // --base and --pr are required (quality_gates_helm.go:93)

	// --- produce input for other gates ------------------------------------
	"git-diff-to-lines": utility,
	"coverage-merge":    utility,
	"coverage-diff":     utility,
	"baseline-update":   utility,
	"baseline-compare":  utility,
	"scripts-audit":     utility, // emits the CSV ledger; no-new-bash is the gate

	// --- need live state, a ceremony, or a cadence ------------------------
	"helm-drift":              operatorRun,
	"cosign-cutover":          operatorRun,
	"service-auth-rotation":   operatorRun,
	"clusterimagepolicy-mode": operatorRun,
	"divergence-matrix":       operatorRun,
	"spotcheck-dimension":     operatorRun,
	"drill-creds-isolation":   operatorRun,
	"drill-outputs-consumers": operatorRun,
	"runtime-slo":             operatorRun,

	// --- measured broken -------------------------------------------------
	// Reports coverage=0.00% against infra/tests/{unit,charts}, which have
	// passing tests (`go test ./charts/` is green). Measured 2026-07-30:
	//   $ loopzero-ops quality test-coverage-report
	//   test-coverage-report: FAIL — coverage=0.00% (threshold=30.00%)
	// Almost certainly measuring the test packages themselves (no non-test
	// statements to cover) rather than the code they exercise.
	"test-coverage-report": brokenGate,

	// --- work, but FAIL on a real finding today ---------------------------
	// Each was executed 2026-07-30 and exited 1 with a substantive message.
	// The gate is right; the tree is not. Fix the finding, then wire.
	"alert-runbooks":          failingGate, // a Prometheus alert has no runbook annotation
	"base-image-digest":       failingGate, // a base image drifted off its pinned digest
	"crd-semantic-diff":       failingGate, // chart CRD out of sync with kubebuilder output
	"migration-test-coverage": failingGate, // a migration has no matching test
	// Virou ENFORCED em 2026-08-01 (prazo T+60d) com o branch AES legado ainda no
	// dispatcher — 4 achados em api/internal/db/store/secret_wrapper.go:{148,152,188,192}.
	// Medido 2026-08-06. A divida T10.3 (deletar o branch) segue aberta: remover o
	// caminho de leitura AES antes de provar que nenhum segredo persistido usa o
	// prefixo antigo torna esses dados irrecuperaveis, entao a correcao exige
	// evidencia de migracao que este repo nao carrega.
	"no-aes-remaining":         failingGate,
	"no-pulumi-inline-secrets": failingGate, // inline secret literal in Pulumi code
	"skip-link":                failingGate, // 115 grandfathered a11y violations in the baseline
	"stack-coverage":           failingGate, // a declared strategy has no fixtures
	"template-compliance":      failingGate, // empty PR body vs the canonical template

	// --- exceed the per-push time budget ----------------------------------
	"mock-budget":      slowGate, // >25s
	"pkg-usage-matrix": slowGate, // >25s

	// --- pass without asserting anything here -----------------------------
	// cli-bundle-size is the #254 shape in gate form: it guards `cli/dist`,
	// which left this repo with the CLI at M2, so it now reports
	// "PASS — cli/dist not present" forever. It belongs in loopzero-cli.
	"cli-bundle-size": vacuousGate,
	// Prints "NON-CONFORMANT without ADR: 2" and still exits 0 — advisory,
	// not a gate. Wiring it would add noise without enforcement.
	"security-context": vacuousGate,
	// SKIPs when there are no bug-fix references, which is the normal state.
	"regression-test": vacuousGate,
}

// loadQualityTaskfile returns taskfiles/quality.yml as text.
func loadQualityTaskfile(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(qualityTaskfileRelPath)
	if err != nil {
		t.Fatalf("resolve %s: %v", qualityTaskfileRelPath, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// gateIsWired reports whether quality.yml invokes the gate. Matching on the
// invocation shape (`quality <name>` followed by end-of-token) avoids the
// prefix collision that a plain strings.Contains would hit — "test-targets"
// must not be satisfied by a line invoking "test-targets-extra".
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

// TestEveryGateIsWiredOrExempted is the #255 guard.
func TestEveryGateIsWiredOrExempted(t *testing.T) {
	taskfile := loadQualityTaskfile(t)

	var orphans []string
	for _, spec := range gateRegistry {
		if gateIsWired(taskfile, spec.Name) {
			continue
		}
		if _, exempt := gateWiringExemptions[spec.Name]; exempt {
			continue
		}
		orphans = append(orphans, spec.Name)
	}
	sort.Strings(orphans)

	for _, name := range orphans {
		t.Errorf("gate %q is registered but neither wired into taskfiles/quality.yml nor declared in gateWiringExemptions.\n"+
			"    An unwired gate is dead code that reads as coverage (issue #255).\n"+
			"    Either add it to a task in taskfiles/quality.yml, or add an entry here stating why not\n"+
			"    (parameterized / utility / operatorRun). Do not add a new unwiredDebt entry.",
			name)
	}
}

// TestGateWiringExemptionsAreNotStale keeps the ledger honest in both
// directions: an entry naming a gate that no longer exists is dead weight, and
// an entry for a gate that HAS since been wired silently keeps an exemption
// alive that nobody needs — the shape by which allowlists rot into decoration.
func TestGateWiringExemptionsAreNotStale(t *testing.T) {
	taskfile := loadQualityTaskfile(t)

	registered := map[string]bool{}
	for _, spec := range gateRegistry {
		registered[spec.Name] = true
	}

	names := make([]string, 0, len(gateWiringExemptions))
	for name := range gateWiringExemptions {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !registered[name] {
			t.Errorf("exemption for %q does not match any registered gate — remove the stale entry, or it will silently exempt a future gate of the same name", name)
			continue
		}
		if gateIsWired(taskfile, name) {
			t.Errorf("gate %q IS wired into taskfiles/quality.yml but still carries an exemption (%s) — drop the entry so the ledger keeps meaning what it says", name, gateWiringExemptions[name])
		}
	}
}

// TestGateWiringGuard_NegativeControl proves the guard detects an orphan,
// rather than passing because gateIsWired always returns true.
func TestGateWiringGuard_NegativeControl(t *testing.T) {
	taskfile := loadQualityTaskfile(t)

	// A wired gate must be seen as wired...
	if !gateIsWired(taskfile, "no-new-bash") {
		t.Fatalf("negative-control setup failed: no-new-bash is wired in quality.yml but gateIsWired says otherwise")
	}
	// ...and a name that appears nowhere must not be.
	if gateIsWired(taskfile, "gate-that-does-not-exist-anywhere") {
		t.Errorf("gateIsWired returned true for a name absent from quality.yml — the matcher is too loose, so every orphan would pass")
	}
	// Prefix collision: a longer name sharing a wired gate's prefix must not
	// inherit its wiring.
	if gateIsWired(taskfile, "no-new-bash-extended") {
		t.Errorf("gateIsWired matched on prefix — %q would be considered wired because %q is; orphans with a shared prefix would slip through", "no-new-bash-extended", "no-new-bash")
	}
}
