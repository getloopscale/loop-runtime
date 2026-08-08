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
	//
	// UNUSED as of 2026-08-06, and worth saying why: all three entries it once
	// held turned out to be misdiagnoses. cli-bundle-size was genuinely dead
	// (its subject left with the CLI at M2) and was deleted rather than
	// documented. security-context exits 0 only in report mode — with --gate it
	// fails on two real findings. regression-test needs PR_* in the environment.
	// "It passes vacuously" is an easy label to reach for when a gate is quiet;
	// twice out of three it was hiding a different problem.
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
	// Needs the PR body (--pr-body / $PR_BODY). Reported as "FAIL: empty PR
	// body" from a repo scan, which was read as a real finding and filed under
	// failingGate; it is a missing mandatory input, like helm-diff-structural.
	"template-compliance": parameterized,
	// Reads PR_TITLE / PR_BODY / PR_COMMITS / PR_DIFF_FILES from the environment
	// and SKIPs when they are absent — which is always, in a repo scan. Filed
	// under vacuousGate ("SKIPs when there are no bug-fix references, which is
	// the normal state"); the cause is the missing PR context, not the gate.
	"regression-test": parameterized,

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

	// --- work, but FAIL on a real finding today ---------------------------
	// The gate is right; the tree is not. Fix the finding, then wire.
	// Re-measured 2026-08-06 with the counts, so the size of each debt is on
	// the record rather than implied.
	//
	// 33 alerts without runbook_url. Worth knowing before "just add the
	// annotation": the URLs the conformant alerts already carry are 404 —
	// https://docs.loopzero.example/runbooks/vault-auth-failure-spike returns 404
	// while the site root returns 200. The gate checks that the string is
	// present, not that a runbook exists, so adding 33 more dead links would
	// buy a green gate and nothing else. An on-call operator following a 404 at
	// 3am is worse off than one who knows there is no runbook.
	"alert-runbooks": failingGate,
	// 42 migrations with no matching api/tests/unit/migration_*_test.go. The
	// gate only checks that the FILE exists, so 42 stubs would turn it green
	// without testing anything — the bar is the canonical shape in
	// migration_012_fk_on_delete_test.go, which asserts invariants specific to
	// its migration. That is 42 pieces of real work, not a mislabel.
	"migration-test-coverage": failingGate,
	// Virou ENFORCED em 2026-08-01 (prazo T+60d) com o branch AES legado ainda no
	// dispatcher — 4 achados em api/internal/db/store/secret_wrapper.go:{148,152,188,192}.
	// Medido 2026-08-06. A divida T10.3 (deletar o branch) segue aberta: remover o
	// caminho de leitura AES antes de provar que nenhum segredo persistido usa o
	// prefixo antigo torna esses dados irrecuperaveis, entao a correcao exige
	// evidencia de migracao que este repo nao carrega.
	"no-aes-remaining": failingGate,
	// NOT vacuous — the old label said it "still exits 0" and that wiring it
	// would add noise without enforcement. It exits 0 only in report mode; with
	// --gate it exits 1, measured 2026-08-06:
	//   argo-events, loopzero-ci-workflows — NON-CONFORMANT, no ADR exception
	// Both are real releases in build.helmfile.yaml.gotmpl, not dead POCs. The
	// fix is a securityContext on charts whose workloads (argo-events/NATS, the
	// Argo WorkflowTemplate containers) can break under readOnlyRootFilesystem,
	// which makes it a T2 change requiring LIVE validation on a cluster.
	"security-context": failingGate,
	// 9 inline encrypted secrets across infra/pulumi/Pulumi.{dev,prod}.yaml.
	// Closing this is the T9 Vault-provider migration, on the legacy DO/Pulumi
	// layer that the AWS re-platform is retiring (ADR 2026-06-17) — moving live
	// production secrets, not an edit. Measured 2026-08-06.
	"no-pulumi-inline-secrets": failingGate,

	// --- pass without asserting anything here -----------------------------
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
