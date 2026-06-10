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
)

func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// T-01 — atomic cutover script exists + has zero-Ready watcher.
func TestT01_AtomicCutoverScriptExists(t *testing.T) {
	path := filepath.Join(repoRoot(), "infra", "scripts", "operator-cutover-execute.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("operator-cutover-execute.sh missing: %v", err)
	}
	text := string(data)

	for _, must := range []string{
		"ZERO_READY_THRESHOLD_SEC",       // C6 watcher threshold
		"validate-matrix.sh",              // pre-flight gate
		"AUTO_ROLLBACK",                   // safety trap
		"helmfile",                        // sync invocation
		"--for=condition=Available",       // explicit Ready wait
		"PRE_FLIGHT_PASSED",               // trap discipline
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
func TestT03_CutoverRunbookExists(t *testing.T) {
	t.Skip("runbook files removed — docs/runbooks/ deleted in governance consolidation 2026-05-11")
}

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

// T-06 — multi-cell.yml workflow exists for Phase 5 stages.
func TestT06_MultiCellWorkflowExists(t *testing.T) {
	path := filepath.Join(repoRoot(), ".github", "workflows", "multi-cell.yml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("multi-cell.yml missing: %v", err)
	}
}

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
	for _, alert := range []string{
		"OperatorRestartRate24h",                  // C15
		"OperatorReconcileLatencyPostCutover24h",  // T2.1 paired
		"ZeroReadyOperatorWindow",                 // C6 alert pair
	} {
		if !strings.Contains(text, alert) {
			t.Errorf("missing alert: %s", alert)
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

// T-10 — admin pin endpoint + CLI shipped (T5.5).
func TestT10_AdminPinEndpointShipped(t *testing.T) {
	path := filepath.Join(repoRoot(), "api", "internal", "routes", "admin_pin_cell.go")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("admin_pin_cell.go missing — T5.5 incomplete: %v", err)
	}
	cliPath := filepath.Join(repoRoot(), "cli", "src", "register", "infra.ts")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("infra.ts missing: %v", err)
	}
	if !strings.Contains(string(data), "admin/tenants/pin") {
		t.Error("CLI infra.ts must reference admin/tenants/pin endpoint")
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
	testPath := filepath.Join(repoRoot(), "api", "tests", "unit", "migration_006_cells_test.go")
	if _, err := os.Stat(testPath); err != nil {
		t.Errorf("migration_006_cells_test.go missing — C1 of T-1.3 incomplete: %v", err)
	}
}

// T-12 — multi-cell observability shipped.
func TestT12_MultiCellObservabilityShipped(t *testing.T) {
	for _, p := range []string{
		"infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml",
	} {
		full := filepath.Join(repoRoot(), p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("%s missing: %v", p, err)
		}
	}
}

// T-13 — live cell-2 canary stage 1 GREEN.
// SKIP: requires cloud cluster + 1h soak (Phase 5 T5.6).
func TestT13_Cell2Stage1_Live(t *testing.T) {
	t.Skip("requires cloud cluster + 1h soak — Phase 5 T5.6 cloud-pendente")
}

// T-14 — live cell-2 canary stage 2 GREEN.
// SKIP: requires 24h soak (Phase 5 T5.7).
func TestT14_Cell2Stage2_Live(t *testing.T) {
	t.Skip("requires cloud cluster + 24h soak — Phase 5 T5.7 cloud-pendente")
}
