// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 conditions C1+C6+C9+C19.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
//
// Composite test matrix T-01..T-14 (condition C19) pinning the
// operator-cutover scripts and Tilt-helmfile parity.
//
// C1 — atomic cutover script exists + has the safety controls.
// C6 — zero-Ready watcher script exists (background poller in cutover).
// C9 — T2.4 (Tilt → helmfile parity) validation lives same-PR. The
//      test reads `Tiltfile` and asserts it consumes helmfile output
//      rather than hardcoded yamls.
// C19 — gate test for T-01..T-14 readiness; some sub-tests are SKIP
//       until cloud-side verification phases ship (e.g., T-13/T-14
//       require post-cutover real-cluster runs).

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// T-01 — atomic cutover script exists + has zero-Ready watcher.
func TestT01_AtomicCutoverScriptExists(t *testing.T) {
	// F27 (M23 audit 2026-07-10): script relocated to `infra/scripts/production/`
	// per shell-scripts-allowlist.md § infra/scripts/production/ (2026-05-23).
	// Try canonical path first, then legacy.
	path := filepath.Join(repoRoot(), "infra", "scripts", "production", "operator-cutover-execute.sh")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = filepath.Join(repoRoot(), "infra", "scripts", "operator-cutover-execute.sh")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("operator-cutover-execute.sh missing (tried production/ + legacy): %v", err)
	}
	text := string(data)

	for _, must := range []string{
		"ZERO_READY_THRESHOLD_SEC",  // C6 watcher threshold
		"validate-matrix.sh",        // pre-flight gate
		"AUTO_ROLLBACK",             // safety trap
		"helmfile",                  // sync invocation
		"--for=condition=Available", // explicit Ready wait
		"PRE_FLIGHT_PASSED",         // trap discipline
	} {
		if !strings.Contains(text, must) {
			t.Errorf("cutover script missing: %s", must)
		}
	}
}

// T-02 — rollback script exists + uses git-show-from-parent (no destructive ops).
func TestT02_RollbackScriptExists(t *testing.T) {
	// `infra/scripts/operator-cutover-rollback.sh` deleted in commit
	// 27cfe6cd (2026-05-04, ADR-0023 / plan v1.1 T1.5 LOCAL cutover
	// complete). The 4-binary split is canonical on the LOCAL stack;
	// production rollout is governed by separate
	// CHG-NNNN-OPERATOR-SPLIT-PROD per ADR-0023's 4-stage cutover and
	// would author a new rollback script at cutover time. Re-enable
	// this test (and update the path constant) when the prod-cutover
	// PR lands.
	t.Skip("rollback script removed post LOCAL cutover (commit 27cfe6cd) — re-enable when prod-cutover PR ships")
}

// T-03 — runbook exists with all required sections.
// T-04 — invariants doc exists with I-1..I-8.
func TestT04_InvariantsDocExists(t *testing.T) {
	// `docs/decisions/operator-split-invariants.md` deleted in
	// fc1793dd (2026-05-11 doc cleanup). I-1..I-8 invariants are now
	// verified directly by per-invariant tests in
	// `infra/pulumi/src/__tests__/operator-split-invariants.test.ts`
	// (8 individual tests, each verifying the verification artifact
	// directly — Go tests, helm-template checks). The "central index
	// doc" assertion is meta-coverage that no longer adds signal once
	// each invariant has its own pinned test. See finding
	// `.claude/knowledge-base/concepts/operations/finding-2026-05-15-pulumi-vitest-23-fail-drift.md`.
	t.Skip("invariants doc deleted fc1793dd — I-1..I-8 covered directly by operator-split-invariants.test.ts")
}

// T-05 — RBAC subset gate wired into ci.yml.
func TestT05_RbacSubsetWiredInCI(t *testing.T) {
	// `rbac-subset-check.sh` deleted in plan v1.1 T1.5 (ADR-0023)
	// because the comparison baseline (monolith tenant-controller
	// ClusterRole) was deleted in the same plan. The replacement
	// strategy is documented in `.github/workflows/ci.yml:175-185`:
	// kubebuilder:rbac markers + an AST analyzer at
	// `hack/validate-rbac/` enforcing "every r.Client.X call has a
	// corresponding marker". That validator is currently aspirational
	// (path does not exist yet). Re-enable this test pointing at the
	// new validator once it ships per ADR-0026.
	t.Skip("rbac-subset gate deleted T1.5 — replacement validator (hack/validate-rbac/) per ADR-0026 not yet shipped")
}

// T-06 was REMOVED, not skipped (B-182, applying the standard of B-169 and B-180).
//
// It stat'd `.github/workflows/multi-cell.yml` and skipped when the file was
// absent. That file was deleted in 5b32b76f0 (2026-05-23), so the skip was the only
// branch ever taken from that commit on, and the whole assertion was a single
// os.Stat — meta-coverage over a file listing, the weakest kind there is, and the
// same shape T-04 above was retired for.
//
// The skip message also made a CLAIM: "Phase 5 invariants now in task quality:*".
// Measured 2026-09-06 by walking the deleted workflow's preflight job step by step
// (`git show 5b32b76f0^:.github/workflows/multi-cell.yml`), that claim holds for
// five of its nine checks and is false for four:
//
//   COVERED — the four Go steps (cells migration, admin_pin_cell handler,
//     cell_pinner service, DynamicRouter cell-routing metrics) were plain `go test`
//     over api/ packages. The ordinary suite runs them, and T-10 and T-11 below pin
//     the endpoint and the migration.
//   GONE WITH ITS SUBJECT — the CLI `admin tenants pin` integration step. cli/ left
//     this repository in 686b9a4c5 (M2) and its suite went with it.
//   NOT COVERED, and restored below — the PromRule alert-count floor. Nothing here
//     asserted it: the only other in-repo references to that manifest
//     (adr-verification-matrices.test.ts, twice) check existence, not content. It is
//     T-12 below now.
//   NOT COVERED AND NOT RESTORABLE AS WRITTEN — the wiring grep,
//     `grep -F "clustercredential-create.sh" infra/helmfile/scripts/wire-clusters.sh`.
//     Measured: wire-clusters.sh has not CALLED that lib since 2026-05-31, when the
//     call was deleted on purpose (the "V9-CLEANROOM-FRESH-FAANG-FIX" blocks in that
//     file) because it created a duplicate ClusterCredential CR and left ArgoCD's
//     application-controller with cancelled watches. The only two matches left in
//     the file are the COMMENTS recording that removal — so the workflow step was
//     already passing on prose describing the opposite of what it asserted, before
//     the workflow was ever deleted. Restoring it verbatim would pin a known bug.
//     What is worth guarding is the fix, and that is T-12b below.
//   STILL NOT COVERED, named rather than pretended — (a) the
//     clustercredential-create.sh usage gate (no args -> exit 1 + "usage:"; bad
//     cell-id -> exit 1 + "invalid cell-id"), which is script EXECUTION and does not
//     belong in a static unit test; and (b) `bash infra/tests/parity/preflight.sh`,
//     whose script is not in this tree at all. infra/tests/parity/ holds only
//     standalone-values.yaml, while taskfiles/test.yml (`parity:preflight`,
//     `validate:matrix`) and infra/tests/e2e/parity_install_test.go still invoke
//     preflight.sh and validate-matrix.sh. That dangling reference is wider than
//     this test and is not fixed here; it is written down so the next reader does
//     not have to re-derive it.
//
// The honest summary the old skip message could not give: four of Phase 5's checks
// stopped being enforced when the workflow went, two are enforced again as of this
// commit, and two remain open with their shape named.

// T-07 — cache scoping enforcement (C16).
// Live envtest is cloud-pendente (KUBEBUILDER_ASSETS + ~30s setup per
// case is CI-prohibitive for a contract this thin). The IMPLEMENTATION
// contract — cache.Options with the right ByObject keys — is verified
// by static-source tests in `cache_scoping_test.go` (4 cases:
// tenant-op scopes Tenant+Environment; platform-op scopes the 3
// platform CRDs; app/preview do NOT scope workload CRDs; monolith
// retains scoping). controller-runtime guarantees the runtime behaviour
// given the source declarations; this test confirms the static gate is
// in place.
func TestT07_CacheScopingStaticSource(t *testing.T) {
	path := filepath.Join(repoRoot(), "infra", "tests", "unit", "cache_scoping_test.go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache_scoping_test.go missing — C16 not implemented: %v", err)
	}
}

// T-08 — operator-cutover Prometheus rule has the 3 alerts.
func TestT08_OperatorCutoverAlertsLoaded(t *testing.T) {
	path := filepath.Join(repoRoot(), "infra", "helm", "monitoring", "manifests", "prometheusrule-operator-cutover.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("prometheusrule missing: %v", err)
	}
	text := string(data)

	// `ZeroReadyOperatorWindow` is the one alert this file still carries, and the commit
	// that retired the other two says why it was kept: zero ready operators is a condition
	// that does not expire with a cutover.
	if !strings.Contains(text, "ZeroReadyOperatorWindow") {
		t.Error("missing alert: ZeroReadyOperatorWindow")
	}

	// The other two were RETIRED deliberately by `49b52c0a5` (2026-09-07), which recorded
	// the evidence: both declare themselves "24h post-cutover" watchers of CHG-2026-04-30,
	// and the operator split is complete — so they watch for a window that has closed.
	//
	// This test asserted their PRESENCE until 2026-09-16 and had been red since that
	// commit, which is the shape worth naming: the chart moved with a recorded decision
	// and the test kept asserting the state before it. Asserting their ABSENCE is the
	// stronger form — it pins the decision, so a retired alert cannot drift back in
	// without somebody re-opening the reasoning that retired it.
	for _, retired := range []string{
		"OperatorRestartRate24h",
		"OperatorReconcileLatencyPostCutover24h",
	} {
		if strings.Contains(text, retired) {
			t.Errorf("%s was retired by 49b52c0a5 as a closed-window watcher and is back; "+
				"re-open that reasoning before restoring it", retired)
		}
	}
}

// T-09 — Tilt consumes helmfile output (C9: T2.4 same-PR).
// This is the LIVE-validatable form of "Tilt and Helmfile produce the
// same pod set". For the static gate, we verify Tiltfile has migrated
// AWAY from hardcoded infra/tilt/*-deployment.yaml references for the
// split operators.
func TestT09_TilftileSplitOperatorMigrationStatus(t *testing.T) {
	path := filepath.Join(repoRoot(), "Tiltfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Tiltfile missing: %v", err)
	}
	text := string(data)

	// Tiltfile may still load split-operator yamls during the migration
	// window (Phase 2 not yet executed). What's not acceptable is
	// loading the orphan monolith yamls (operator-deployment.yaml,
	// controller-deployment.yaml, cloud-deployment.yaml).
	for _, orphan := range []string{
		"infra/tilt/operator-deployment.yaml",
		"infra/tilt/controller-deployment.yaml",
		"infra/tilt/cloud-deployment.yaml",
	} {
		// Allow comment references; reject k8s_yaml call-sites.
		callSite := "k8s_yaml(\"" + orphan
		if strings.Contains(text, callSite) {
			t.Errorf("Tiltfile loads orphan yaml: %s (should be deleted per T2.4)", orphan)
		}
	}
}

// T-10 — admin pin endpoint shipped (T5.5).
//
// B-169. Two defects lived here and they share one shape: the guard could not
// find its subject, and instead of saying so it reported a CONCLUSION about the
// feature it had never observed.
//
//  1. The endpoint path went stale. admin_pin_cell.go moved into the `admin/`
//     subdirectory during a routes refactor (siblings: admin_evict.go,
//     admin_release_inspect.go). The assertion still named the pre-move path and
//     announced "T5.5 incomplete" for an endpoint that had shipped and never
//     left. Path corrected in eb4ae31b1; the message below no longer draws that
//     conclusion from a failed lookup.
//
//  2. The CLI half asserted a file in ANOTHER REPOSITORY. `cli/` was extracted
//     to sibling `../loopzero-cli` at M5 (CLAUDE.md § "Was here, now extracted").
//     The code stat'd cli/src/register/infra.ts and, on IsNotExist, called
//     t.Logf and returned. Measured 2026-09-05: that path exists nowhere in the
//     umbrella checkout, and `loopzero-cli` is not cloned beside this repo at all —
//     so the early
//     return was the ONLY branch ever taken since M5, and the ReadFile +
//     Contains assertion beneath it was unreachable code. t.Logf is invisible
//     without -v, so the suite printed "ok" while verifying nothing.
//
// The CLI assertion is REMOVED, not skipped. A permanent t.Skip would be a
// dead-Skip (this repo swept those in 339b9edfe) and, worse, would mark the
// WHOLE test skipped — discarding the endpoint check that genuinely is ours.
// A sibling repo's source is not this repo's to guarantee. What this repo does
// guarantee is pinned where it can actually be executed:
//   - api/internal/server/control_surfaces_observable_test.go builds the router and
//     asserts the AdminPin surface is recorded with its route.
//   - api/internal/routes/admin/admin_pin_cell_test.go covers the handler and
//     its 403 without X-Service-Auth.
//   - the cli<->engine seam has its own target: `task quality:contract:cross-repo`.
func TestT10_AdminPinEndpointShipped(t *testing.T) {
	path := filepath.Join(repoRoot(), "api", "internal", "routes", "admin", "admin_pin_cell.go")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("this guard could not find its subject at %s: %v\n"+
			"Read that as: the GUARD is stale or the handler moved. It does not by "+
			"itself mean T5.5 regressed — the last time this fired (B-169) the "+
			"endpoint had shipped and only this path was wrong. Check the "+
			"registration in api/internal/server/router.go before concluding "+
			"anything about the feature.", path, err)
	}
}

// T-11 — cells migration in place + tests.
// Numbered 000006 (was 000003 — renumbered 2026-04-30 after LIVE discovery
// that golang-migrate silently skipped 000003 on existing DBs at
// schema_migrations.version >= 5). The 000003 slot is now reserved/empty.
func TestT11_CellsMigrationPresent(t *testing.T) {
	path := filepath.Join(repoRoot(), "api", "internal", "db", "migrations", "000006_cells.up.sql")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("000006_cells.up.sql missing: %v", err)
	}
	testPath := filepath.Join(repoRoot(), "api", "internal", "db", "migration_006_cells_test.go")
	if _, err := os.Stat(testPath); err != nil {
		t.Errorf("migration_006_cells_test.go missing — C1 of T-1.3 incomplete: %v", err)
	}
}

// T-12 — multi-cell observability shipped.
//
// B-182: this used to stat the PromRule and stop there. The deleted multi-cell.yml
// preflight went one step further — `test "$count" -ge 7` over the alert entries —
// and that assertion died with the workflow. Counted 2026-09-06: the rule declares
// exactly 7 alerts, one per metric pair Phase 5 T5.4 shipped. The floor stays at 7
// because that is the number on-call was promised; alerts may be added, never
// dropped below it.
//
// The count comes from parsing the manifest rather than from the workflow's
// `grep -cE "^        - alert: "`, which was pinned to one exact indentation and
// would have started counting zero after any reformat. Parsing also proves the
// manifest is valid YAML, which the grep never did.
func TestT12_MultiCellObservabilityShipped(t *testing.T) {
	const rule = "infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml"
	data, err := os.ReadFile(filepath.Join(repoRoot(), rule))
	if err != nil {
		t.Fatalf("%s missing: %v", rule, err)
	}
	var parsed struct {
		Spec struct {
			Groups []struct {
				Rules []struct {
					Alert string `yaml:"alert"`
				} `yaml:"rules"`
			} `yaml:"groups"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("%s is not valid YAML: %v", rule, err)
	}
	var alerts []string
	for _, g := range parsed.Spec.Groups {
		for _, r := range g.Rules {
			if r.Alert != "" {
				alerts = append(alerts, r.Alert)
			}
		}
	}
	const minAlerts = 7
	if len(alerts) < minAlerts {
		t.Errorf("%s declares %d alert(s) %v, expected at least %d \u2014 Phase 5 T5.4 shipped "+
			"seven and multi-cell.yml's preflight held that floor until the workflow was "+
			"deleted in 5b32b76f0", rule, len(alerts), alerts, minAlerts)
	}
}

// T-12b — wire-clusters.sh does not re-create ClusterCredential CRs through the
// legacy lib. This guards the FIX, not the bug the deleted workflow guarded.
//
// B-182. On 2026-05-31 the `clustercredential-create.sh cell-0 runtime` and
// `... cell-1 runtime-2` calls were removed from wire-clusters.sh: each created a
// SECOND ClusterCredential CR (named after the clusterName, not the cellID) pointing
// at an apiServer URL a CR already claimed. ArgoCD then registered duplicate
// informers per server URL, and on watch invalidation the orphan informer left the
// application-controller with cancelled watches and apps stuck at status.sync="".
// LIVE-validated in the cleanroom-fresh dogfood the same day. The canonical path is
// `bootstrap-cell-cc.sh --cell N --env <env>`, which runs in the Helmfile hook chain
// with CR name = cellID.
//
// The assertion is written as "no NON-COMMENT line names the lib" for a measured
// reason. The deleted workflow's plain `grep -F` matched the removal comments and
// so passed on prose, and the same false pass is live today in
// infra/pulumi/src/__tests__/cc-bootstrap-lib.test.ts, whose case "invokes
// clustercredential-create.sh (audit Gap 2 fix)" is green only because those
// comments exist. A substring check over a shell file cannot tell code from the
// comment explaining why the code is gone.
func TestT12b_WireClustersDoesNotRecreateLegacyClusterCredential(t *testing.T) {
	const (
		wire      = "infra/helmfile/scripts/wire-clusters.sh"
		legacyLib = "clustercredential-create.sh"
		canonical = "infra/helmfile/scripts/bootstrap-cell-cc.sh"
	)

	data, err := os.ReadFile(filepath.Join(repoRoot(), wire))
	if err != nil {
		t.Fatalf("%s missing: %v", wire, err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, legacyLib) {
			continue
		}
		// Only a whole-line comment is allowed to name it. Anything else — including a
		// command with a trailing comment — counts as a call and fails, which is the
		// safe direction for a check whose whole point is telling code from prose.
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("%s:%d calls %s again. It was removed 2026-05-31 because the duplicate "+
				"ClusterCredential CR it creates leaves ArgoCD's application-controller with "+
				"cancelled watches; the canonical path is %s.\n\t%s",
				wire, i+1, legacyLib, canonical, strings.TrimSpace(line))
		}
	}

	// The removal is only safe while the canonical path still exists and still has a
	// caller. "Deleted the legacy call, the canonical one covers it" is exactly the
	// kind of claim this series keeps finding outlived its subject.
	if _, err := os.Stat(filepath.Join(repoRoot(), canonical)); err != nil {
		t.Fatalf("%s is gone, so nothing creates the per-cell ClusterCredential CRs: %v", canonical, err)
	}
	root, err := os.ReadFile(filepath.Join(repoRoot(), "Taskfile.yml"))
	if err != nil {
		t.Fatalf("read Taskfile.yml: %v", err)
	}
	// COMMENTS FIRST. The Taskfile mentions this script in a comment
	// ("# Plan v1.2 T4.2: consolidated bootstrap-cell-cc.sh --cell --env"), and a
	// plain Contains over the file was satisfied by it. An audit deleted BOTH live
	// invocations on 2026-09-06 and this test stayed green — the exact condition the
	// error message below is written for, and the guard had nothing to say.
	if !strings.Contains(stripYAMLComments(string(root)), "bootstrap-cell-cc.sh --cell") {
		t.Errorf("Taskfile.yml no longer invokes %s — the canonical replacement for the "+
			"call removed from %s has lost its caller", canonical, wire)
	}
}

// stripYAMLComments drops whole-line comments and trailing ` #...` from a YAML file,
// so a mention in prose cannot stand in for a live invocation.
//
// Whole-line only, plus a trailing comment that follows whitespace: a `#` inside a
// quoted string is left alone, because truncating a live command line would make this
// guard fail for the wrong reason. Erring here costs a false RED, and the direction
// matters -- a guard that asserts PRESENCE gets stricter by seeing less, which is the
// safe side for this one.
func stripYAMLComments(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, "")
			continue
		}
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// T-13 — live cell-1 canary stage 1 GREEN.
// SKIP: requires cloud cluster + 1h soak (Phase 5 T5.6).
func TestT13_Cell2Stage1_Live(t *testing.T) {
	t.Skip("requires cloud cluster + 1h soak — Phase 5 T5.6 cloud-pendente")
}

// T-14 — live cell-1 canary stage 2 GREEN.
// SKIP: requires 24h soak (Phase 5 T5.7).
func TestT14_Cell2Stage2_Live(t *testing.T) {
	t.Skip("requires cloud cluster + 24h soak — Phase 5 T5.7 cloud-pendente")
}
