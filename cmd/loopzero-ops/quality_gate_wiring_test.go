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
// worked down — the same pattern infra/quality/shell-scripts-allowlist.md uses
// for shell scripts. Reclassifying one to a real category, or wiring it, is
// the way an entry leaves this list. Adding a NEW unwiredDebt entry is a smell
// that should not survive review.
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const qualityTaskfileRelPath = "../../taskfiles/quality.yml"

// wiringReason categorises why a registered gate is absent from quality.yml.
//
// It is a value object rather than a bare string because one of the categories
// carries an invariant. `operatorRun` asserts that the gate depends on state
// OUTSIDE the repository tree; that assertion was accepted on trust for as long
// as it was a string, and two entries turned out to be false — `helm-drift` and
// `clusterimagepolicy-mode` are hermetic comparisons over `charts/` and
// `infra/helm/`, measured 2026-08-11 (zero exec.Command / kubectl / client-go).
//
// Detecting the falsehood mechanically is not affordable: the external input
// lives in the gate's `Run` func in package main, five of the nine operatorRun
// gates have no package of their own, and a static probe produces FALSE
// POSITIVES on `runtime-slo` and `divergence-matrix` — both legitimately
// operator-run. A guard that cries wolf is switched off, and then the real ones
// are lost with it.
//
// So the invariant is a DECLARATION, not a detection: an operatorRun exemption
// must NAME the external state. `runtime-slo` names the go-test log; helm-drift
// has nothing to name. Writing the wrong category now requires writing a
// falsehood in prose, which a reviewer reads — strictly better than a heuristic
// that a reviewer disables.
type wiringReason struct {
	category      string
	externalInput string // REQUIRED when category == categoryOperatorRun
	// ambient marks external state that is always available (the wall clock),
	// and therefore cannot be proven by running the gate without it.
	ambient bool
}

// String keeps the ledger's error messages readable and makes wiringReason
// usable wherever the old string type was.
func (r wiringReason) String() string {
	if r.externalInput == "" {
		return r.category
	}
	return r.category + " [external input: " + r.externalInput + "]"
}

// validate enforces the one invariant that gives operatorRun its meaning.
// Returning a typed error rather than a bool keeps the caller's message
// specific, per .claude/rules/error-handling.md § 2.
func (r wiringReason) validate() error {
	if r.category != categoryOperatorRun {
		return nil
	}
	if strings.TrimSpace(r.externalInput) == "" {
		return errUnnamedExternalInput
	}
	return nil
}

// errUnnamedExternalInput is returned when an operatorRun exemption does not
// say WHICH external state the gate needs.
var errUnnamedExternalInput = errors.New(
	"classified operator-run but names no external input — an operator-run gate depends on state outside the repo tree, so say which " +
		"(a cluster, an env var, an artefact from a previous run). If nothing can be named, the gate is hermetic and this is the wrong category")

// categoryOperatorRun is the one category carrying the invariant above.
const categoryOperatorRun = "operator-run — needs live cluster state / a ceremony / a cadence, not a hermetic repo scan"

// operatorRunOn builds the only exemption that requires an argument. A named
// constructor makes the requirement impossible to forget at the call site,
// which a struct literal with an optional field would not.
//
// It marks the input as ABSENT-ABLE: a file or an environment variable that can
// be missing. That is what makes the claim testable — run the gate with the
// input gone, and a gate that truly depends on it cannot reach a verdict.
// TestOperatorRun_AbsentableInputsAreProven does exactly that.
func operatorRunOn(externalInput string) wiringReason {
	return wiringReason{category: categoryOperatorRun, externalInput: externalInput, ambient: false}
}

// operatorRunOnAmbient is for external state that is ALWAYS present and
// therefore cannot be proven by absence — the wall-clock date being the only
// instance measured (2026-08-12, B-045 bullet 1).
//
// The distinction is not bureaucracy. The bullet asked for the operatorRun claim
// to be PROVEN rather than accepted, and a behavioural probe answers that for a
// missing file or an unset variable: `drill-creds-isolation` errors out without
// its tokens, which is proof. Probing `cosign-cutover` the same way returns
// "marker absent. Check is dormant." — a clean verdict, because you cannot take
// the calendar away from a process.
//
// So the honest split is between claims a probe can settle and claims it cannot,
// and naming which is which is what keeps the second group from hiding inside
// the first. Two entries are ambient today; every other operatorRun exemption
// must survive the probe.
func operatorRunOnAmbient(externalInput string) wiringReason {
	return wiringReason{category: categoryOperatorRun, externalInput: externalInput, ambient: true}
}

var (
	// parameterized: requires mandatory flags, so a bare invocation from a
	// task would always fail. Invoked by a caller that supplies them.
	parameterized = wiringReason{category: "parameterized — takes mandatory flags; a bare run from quality:gates would fail"}

	// utility: produces input for other gates rather than a verdict.
	utility = wiringReason{category: "utility — emits data consumed by other gates, has no pass/fail verdict of its own"}

	// unwiredDebt: no reason beyond nobody wired it. Pre-existing #255 debt.
	unwiredDebt = wiringReason{category: "TODO(#255) — genuinely unwired; needs a decision on whether it blocks or is advisory"}

	// failingGate: runs, and reports a REAL finding today. Distinct from
	// brokenGate (which reports a wrong verdict): here the gate is right and
	// the tree is wrong. Wiring it before the findings are addressed would
	// turn quality:gates red for reasons unrelated to whatever change is
	// being tested, which is how a suite gets ignored.
	failingGate = wiringReason{category: "TODO(#255) — WORKS, and currently FAILS on a real finding; fix the finding, then wire"}

	// slowGate: exceeds the budget for a gate meant to run on every push.
	slowGate = wiringReason{category: "TODO(#255) — exceeds a 25s budget; needs profiling or a narrower scan before it can gate every push"}

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
	vacuousGate = wiringReason{category: "TODO(#255) — passes VACUOUSLY here; wiring it would read as coverage while asserting nothing"}

	// brokenGate: measured to produce a wrong verdict today. Distinct from
	// unwiredDebt on purpose — "nobody wired it" and "it does not work" call
	// for different fixes, and collapsing them loses the one fact a reader
	// needs. Wiring a gate in this state would make quality:gates red for a
	// reason that has nothing to do with the code under test.
	brokenGate = wiringReason{category: "TODO(#255) — BROKEN: produces a wrong verdict; must be repaired before it can be wired"}
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
	// Each MUST name the external state (wiringReason.validate). Measured
	// 2026-08-11 by reading each gate's Run func, not by trusting the label —
	// which is how four of the nine below turned out to be misfiled.
	"cosign-cutover": operatorRunOnAmbient(
		"the wall-clock date, compared against the GA marker .github/v1.0-ga-date.txt — a deadline ratchet has no verdict without knowing what day it is"),
	"service-auth-rotation": operatorRunOnAmbient(
		"the wall-clock date, compared against .github/service-auth-ed25519-rotation-date.txt — same ratchet shape as cosign-cutover; it reports on a deadline, it does not rotate anything"),
	"spotcheck-dimension": operatorRunOn(
		"SPOTCHECK_SEED in the environment — the sample is chosen by the operator running the spot check"),
	"drill-creds-isolation": operatorRunOn(
		"DRILL_DO_TOKEN / DIGITALOCEAN_TOKEN / DO_TOKEN / DRILL_PULUMI_TOKEN in the environment"),
	"runtime-slo": operatorRunOn(
		"/tmp/go-test-output.log, produced by a previous `go test` run — absent in a bare repo scan, where the gate SKIPs"),

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
	// Duas correcoes medidas em 2026-08-19 (B-161, B-164), porque o paragrafo acima
	// contava metade da historia:
	//
	//  1. O gate JA' distingue link de runbook, na parte que da' para provar sem rede:
	//     caminho deste repositorio que nao existe em disco, e URL do GitHub em org
	//     diferente da canonica (`getloopscale`, tirada do caminho de modulo dos go.mod).
	//     O que ele nao pode afirmar — se `docs.loopzero.example/runbooks/x` esta' viva —
	//     fica CONTADO como nao-verificavel em vez de silenciado.
	//
	//  2. "33 alertas" era o numero de UM diretorio. O B-164 fechou esse escopo: o gate
	//     varre os QUATRO diretorios de `DefaultRulesDirs`. Re-medido 2026-09-06 (B-067):
	//     110 + 86 + 19 + 18 = **233 sites, e o gate ve os 233** — o "47%" acima descrevia
	//     o estado ANTERIOR ao conserto e ficou no presente do indicativo depois dele.
	//
	//  3. Sites e alertas nao sao o mesmo numero, e confundi-los infla a divida. Os 233
	//     sites sao **145 alertas distintos**: `infra/helm/...` e `charts/...` sao copias
	//     espelhadas, entao 88 sites repetem um alerta ja' contado. Na parte que reprova,
	//     a diferenca e' 46 sites para **37 alertas**. A mensagem do gate agora diz os
	//     dois numeros, e a violacao carrega o diretorio — sem ele, os 9 pares espelhados
	//     saiam como linhas identicas e consertar uma copia deixava a outra reprovando
	//     com a mesma string.
	"alert-runbooks": failingGate,
	// 44 migrations with no matching api/tests/unit/migration_*_test.go (re-measured
	// 2026-09-06, B-067; era 42 aqui, 43 no item e 48 antes desta fatia).
	//
	// A parte "That is 42 pieces of real work, not a mislabel" ERA FALSA e foi medida:
	// CINCO das violacoes eram mislabel. `migration_loopzero_api_keys_test.go`,
	// `migration_memory_p3_test.go`, `migration_api_key_rotation_test.go` e
	// `migration_webhook_delivery_test.go` — 37 funcoes de teste e 68 asserts, verdes —
	// abrem o proprio `.up.sql` das migrations 000024/000025/000026/000031/000033 e
	// mesmo assim eram acusadas, porque o nome do arquivo nao comecava com o numero.
	// Renomear os quatro derrubou a contagem de 48 para 44 sem afrouxar o gate.
	//
	// O que continua verdadeiro, e e' a razao de nao alargar o casamento: o gate confere
	// que o ARQUIVO existe, entao stubs vazios o deixariam verde sem testar nada — a
	// barra e' a forma canonica de migration_012_fk_on_delete_test.go. Casar por
	// CONTEUDO trocaria este falso positivo por um falso negativo pior (mencionar o nome
	// da migration num comentario passaria a contar como teste).
	//
	// Residual conhecido: 000026_memory_exports segue acusada e E' testada — um arquivo
	// so' pode carregar um prefixo numerico. Ver a nota em migration_000025_000026_memory_test.go.
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
	// loopzero-ci-workflows foi APOSENTADO em 2026-08-18 (B-019): o chart inteiro executava uma
	// imagem sem caminho de publicacao nesta arvore. Resta argo-events, que segue sendo um
	// release real em build.helmfile.yaml.gotmpl, nao um POC morto. The
	// fix is a securityContext on charts whose workloads (argo-events/NATS, the
	// Argo WorkflowTemplate containers) can break under readOnlyRootFilesystem,
	// Measured 2026-08-11: reads charts/<name>/templates and infra/helm/<name>/
	// templates and NOTHING else — zero exec.Command / kubectl / client-go. It
	// was filed operatorRun, which is why the missing vap-tenant-cr-shape.yaml
	// survived: the label took it off the radar more durably than forgetting
	// would have. Exit 1 today on loopzero-admission-policies:admission-policies
	// (cosign-version-config.yaml + vap-tenant-cr-shape.yaml present in the
	// bootstrap chart, absent from the published one). Wiring is B-046.
	// B-046 CLOSED 2026-08-12 (slice-3 T1.2 + T1.4): the two missing templates
	// were copied into the published chart, and the allowlist was re-keyed from
	// PAIR to PAIR:FILE — a pair-level approval used to bless every file that
	// pair would ever drift on, which is how the VAP went missing under a gate
	// that names the boundary. helm-drift is GREEN and WIRED; no exemption.

	// Added 2026-08-12 (slice-3 T1.3). Hermetic — reads taskfiles/*.yml and
	// infra/helmfile/*.gotmpl, nothing else. Exit 1 today naming four
	// invocations of `task k3d:secrets:bootstrap-in-core`, which install the
	// SECRETS helmfile into the CORE cluster — the topology violation CLAUDE.md
	// and cleanroom-up-live-test-mandate.md both call FORBIDDEN in every tier.
	//
	// The gate rediscovered B-047 independently, which is the point. Its removal
	// is already DECIDED (ADR 2026-08-12-openbao-in-core-rule-vs-task, ACCEPTED)
	// and B-047 instructs it be the LAST step of any sequence touching it,
	// because removing the task also breaks the openbaobootstrapincore gate that
	// Measured 2026-08-11: reads infra/helm/admission-policies/values.yaml and
	// searches for an ADR; hermetic. Exit 1 — clusterImagePolicy.mode is
	// `enforce` (a live deny-flip that rejects unsigned images at admission)
	// with no ADR/MEET record referencing the cutover. Deciding that posture is
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

// TestOperatorRunNamesItsExternalInput enforces the wiringReason invariant.
//
// `operatorRun` is the only category that makes a factual claim about the world
// outside this repository, and until 2026-08-11 it was the only one nobody
// checked. Two entries were false: helm-drift and clusterimagepolicy-mode read
// charts/ and infra/helm/ and nothing else. The category cannot be verified by
// static probe without false-positiving on runtime-slo and divergence-matrix
// (measured), so it is verified by DECLARATION instead: name the external state.
//
// A gate with nothing to name is hermetic, and belongs in another category.
func TestOperatorRunNamesItsExternalInput(t *testing.T) {
	names := make([]string, 0, len(gateWiringExemptions))
	for name := range gateWiringExemptions {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := gateWiringExemptions[name].validate(); err != nil {
			t.Errorf("exemption for gate %q: %v", name, err)
		}
	}
}

// TestOperatorRunWithEmptyExternalInputFails is the negative control: it proves
// the invariant above can actually fail, rather than passing because validate()
// never returns an error.
func TestOperatorRunWithEmptyExternalInputFails(t *testing.T) {
	if err := operatorRunOn("").validate(); !errors.Is(err, errUnnamedExternalInput) {
		t.Errorf("an operator-run exemption with an empty external input must be rejected; got err=%v", err)
	}
	if err := operatorRunOn("a live cluster").validate(); err != nil {
		t.Errorf("a named external input must be accepted; got %v", err)
	}
	if err := (wiringReason{category: "utility — x"}).validate(); err != nil {
		t.Errorf("the invariant must apply to operator-run ONLY; a utility exemption was rejected: %v", err)
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

// The taxonomy is validated whole, not only the parts currently in use.
//
// slice-1 defined five categories and wired two, so `unwiredDebt`, `slowGate`
// and `vacuousGate` sat unused — dead code by golangci-lint's reading, and it
// was right. Deleting three of five would have been the easy fix and the wrong
// one: the vocabulary exists so the NEXT gate has a name to be classified
// under, and a vocabulary trimmed to whatever is in use today makes the next
// classification harder precisely when it matters.
//
// Holding every category to the same invariant is the honest resolution: it
// removes the dead-code finding by making the categories genuinely exercised,
// and it means a new category cannot be added without satisfying validate().
func TestEveryWiringCategorySatisfiesItsInvariant(t *testing.T) {
	all := map[string]wiringReason{
		"failingGate": failingGate,
		"brokenGate":  brokenGate,
		"unwiredDebt": unwiredDebt,
		"slowGate":    slowGate,
		"vacuousGate": vacuousGate,
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		r := all[n]
		if err := r.validate(); err != nil {
			t.Errorf("category %s does not satisfy its own invariant: %v", n, err)
		}
		if strings.TrimSpace(r.String()) == "" {
			t.Errorf("category %s renders empty — an exemption whose reason reads as nothing is an exemption nobody can audit", n)
		}
	}

	// operatorRun is the one category that carries an argument, and the one that
	// can be built wrong. Its failure path is covered separately; here we only
	// assert a well-formed one joins the family.
	if err := operatorRunOn("um insumo nomeado").validate(); err != nil {
		t.Errorf("a well-formed operatorRun must validate: %v", err)
	}
}

// TestOperatorRun_AbsentableInputsAreProven turns the operatorRun claim from a
// DECLARATION into a DETECTION, which is what B-045 bullet 1 asked for.
//
// The package doc argues, correctly, that a STATIC probe cannot do this: the
// external input lives in package main, and inspecting code for it produces
// false positives. This is a BEHAVIOURAL probe instead — it runs the gate
// against an empty tree with the named input removed, and asserts the gate
// cannot reach a verdict. It inspects nothing; it observes.
//
// A gate that concludes cleanly with its input gone did not need it, and the
// exemption that kept it out of quality:gates was resting on a false claim.
// That is precisely the failure this test exists to make impossible: two
// entries (`helm-drift`, `clusterimagepolicy-mode`) had already turned out to
// be hermetic comparisons wearing an operatorRun label.
//
// Ambient inputs are excluded BY NAME, not by silence — see operatorRunOnAmbient.
func TestOperatorRun_AbsentableInputsAreProven(t *testing.T) {
	// The binary is BUILT rather than skipped-on-absence. A skip here would make
	// the proof vanish exactly where it is least likely to be noticed — on a
	// machine that never ran `task quality:build` — and a probe that silently
	// does not run is the defect this whole item is about.
	bin := filepath.Join(t.TempDir(), "loopzero-ops-probe")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the gate binary for the probe: %v\n%s", err, out)
	}

	// The variables an operatorRun gate may name. Cleared so "absent" is real.
	cleared := []string{
		"SPOTCHECK_SEED", "DRILL_DO_TOKEN", "DIGITALOCEAN_TOKEN",
		"DO_TOKEN", "DRILL_PULUMI_TOKEN",
	}

	var probed int
	for name, reason := range gateWiringExemptions {
		if reason.category != categoryOperatorRun || reason.ambient {
			continue
		}
		t.Run(name, func(t *testing.T) {
			probed++
			cmd := exec.Command(bin, "quality", name, "--root", t.TempDir())
			cmd.Env = os.Environ()
			for _, v := range cleared {
				cmd.Env = append(cmd.Env, v+"=")
			}
			out, err := cmd.CombinedOutput()

			// A clean exit 0 means the gate reached a PASS verdict with its
			// declared input gone. Either the input is not required, or it is
			// not the input named — both make the exemption a false claim.
			if err == nil && !mentionsMissingInput(string(out)) {
				t.Errorf("`%s` is exempted as operatorRun on %q, but it reached a verdict with that "+
					"input absent (exit 0). Either it is hermetic and belongs in quality:gates, or "+
					"the named input is the wrong one.\noutput: %s",
					name, reason.externalInput, truncate(string(out)))
			}
		})
	}

	// Floor: a loop that probed nothing would pass silently, which is the
	// `PASS — 0 routes` shape this repository has paid for once (B-040).
	if probed == 0 {
		t.Error("no absent-able operatorRun gate was probed; either the registry is empty or the " +
			"filter is wrong — a probe that ran zero times proves nothing")
	}
}

// mentionsMissingInput reports whether the output says the gate could not
// proceed. A gate may exit 0 while SKIPping, which is not a verdict.
func mentionsMissingInput(out string) bool {
	low := strings.ToLower(out)
	for _, marker := range []string{"skip", "not set", "absent", "missing", "no such file", "error:", "required"} {
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
