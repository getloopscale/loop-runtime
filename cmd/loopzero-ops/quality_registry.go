// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
)

// gateSpec is one entry in the canonical gate registry: the stable gate name
// dispatched by runQuality's switch plus a one-line human summary.
//
// The registry is the single enumerable source of truth for the gate NAME set.
// The dispatch switch in quality_subcmd.go remains the execution surface; a
// build-time parity test (quality_registry_test.go) asserts the two agree so
// the `--list` oracle can never lie about which gates exist.
type gateSpec struct {
	Name    string
	Summary string
}

// gateRegistry enumerates every `loopzero-ops quality <name>` gate. Kept in sync
// with the dispatch switch by TestGateRegistry_CoversAllSwitchCases. Ordering
// here is not load-bearing (--list sorts); alphabetical for reviewability.
var gateRegistry = []gateSpec{
	{"acyclic-graph", "Enforces the decomposition DAG (loopzero-contracts ◄── engine/cloud/cli); no import cycle."},
	{"advisory-lock-scope", "Forbids session-scoped pg_advisory_lock; only pg_advisory_xact_lock is allowed."},
	{"agpl-isolation", "Verifies AGPL-licensed code stays isolated from proprietary import paths."},
	{"alert-runbooks", "Every Prometheus alert has a linked runbook annotation."},
	{"anonymous-auth", "Forbids anonymous/unauthenticated access on protected API surfaces."},
	{"base-image-digest", "Base images are pinned by digest, not floating tags."},
	{"baseline-compare", "Compares a captured steady-state baseline against the current state."},
	{"baseline-update", "Updates the committed steady-state baseline artifact."},
	{"caddyfile-csp", "Operator-local edge Caddyfile invariants (no CSP on app-dev, preserved headers, rate-limit)."},
	{"cli-bundle-size", "Guards the CLI published bundle size against regression."},
	{"cli-exit-codes", "Enforces the permanent CLI exit-code contract (0/1/2/3/4/5 only)."},
	{"clusterimagepolicy-mode", "Validates the sigstore ClusterImagePolicy enforcement mode."},
	{"cluster-role-no-secrets-star", "Forbids unconditional cluster-wide secrets/configmaps RBAC grants."},
	{"contract-consumers", "Verifies cross-repo contract consumers stay in sync with the contract."},
	{"corrections-txt-closed", "Asserts CORRECTIONS.txt items are all closed."},
	{"correlation-header", "Correlation-ID header is forwarded across the request path."},
	{"cosign-cutover", "Enforces the cosign keyed→keyless cutover deadline ratchet."},
	{"coverage-diff", "Diffs test coverage against a baseline to catch regressions."},
	{"coverage-merge", "Merges per-package coverage profiles into one report."},
	{"crd-semantic-diff", "Semantic diff of CRD schemas to catch breaking spec changes."},
	{"dangling-data-refs", "Detects data/file references that no longer resolve after a change."},
	{"dangling-script-refs", "Detects shell-script references that no longer resolve after a change."},
	{"dashboard-promtool", "Validates Grafana dashboard + Prometheus rules via promtool."},
	{"decisions-deletion", "Guards deletion of decision/ADR records."},
	{"divergence-matrix", "Checks the local-vs-prod divergence matrix is current."},
	{"dockerfile-no-docker-hub", "Forbids unqualified docker.io/Docker Hub base images."},
	{"drill-creds-isolation", "Ensures DR-drill credentials are isolated from production."},
	{"drill-outputs-consumers", "Verifies drill Pulumi-output consumers use the canonical schema."},
	{"git-diff-to-lines", "Utility: converts a git diff into a changed-line set for other gates."},
	{"helm-diff-structural", "Structural pre-sync Helm diff to catch drift before apply."},
	{"helm-drift", "Detects drift between rendered Helm charts and the deployed state."},
	{"helm-overlap-parity", "Keeps the dual Helm chart systems (infra/helm vs charts) in parity."},
	{"migration-test-coverage", "Every DB migration has corresponding test coverage."},
	{"mock-budget", "Caps the number of mocks per test (SRP smell budget)."},
	{"no-aes-remaining", "Calendar-gated: fails on remaining AES code paths post-transit migration."},
	{"no-env-vault-secrets", "Forbids secrets-as-env-var regression after the ESO/OpenBao migration."},
	{"no-legacy-sa-token-secret", "Forbids legacy kubernetes.io/service-account-token Secrets."},
	{"no-migrate-force", "Forbids forced/destructive DB migration operations."},
	{"no-new-bash", "Blocks new .sh files not in the shell-scripts allowlist."},
	{"no-pulumi-inline-secrets", "Forbids inline secret literals in Pulumi code."},
	{"no-shell-wrap", "Forbids thin shell wrappers around pure-logic that belongs in Go."},
	{"orphan-alerts", "Detects Prometheus alerts with no owning dashboard/runbook."},
	{"pkg-usage-matrix", "Verifies the pkg/ cross-component usage matrix is current."},
	{"postgresql-pdb-config", "Validates the PostgreSQL PodDisruptionBudget configuration."},
	{"prod-image-digests", "Production manifests reference images by digest."},
	{"prod-image-tags", "Forbids mutable :latest-style image tags in production manifests."},
	{"quarantine-protect", "Protects the canonical-tests-v1 quarantine from accidental edits."},
	{"regression-matrix", "Verifies the regression-test matrix covers required scenarios."},
	{"regression-test", "Asserts a regression test exists for tracked fixed bugs."},
	{"runtime-slo", "Validates runtime SLO definitions and thresholds."},
	{"script-count", "Tracks the total shell-script count against the reduction target."},
	{"scripts-audit", "Audits shell scripts against the allowlist ledger."},
	{"security-context", "Enforces pod securityContext (non-root, limits, probes)."},
	{"service-auth-rotation", "Enforces the Ed25519 service-auth key rotation cadence."},
	{"shell-hygiene", "Lints shell scripts for strict-mode and hygiene rules."},
	{"skip-link", "Verifies accessibility skip-link presence in the dashboard."},
	{"spotcheck-dimension", "Spot-checks a maturity-audit dimension against evidence."},
	{"sqlc-sync", "Committed sqlc generated/ is in sync with the schema codegen."},
	{"sqlc-tx-prop", "Enforces pool-bound vs tx-bound query propagation inside withEnvironmentLock."},
	{"stack-coverage", "Verifies the tool-inventory stack-coverage claims via grep."},
	{"taskfile-interface", "Validates the Taskfile public-interface contract."},
	{"template-compliance", "Checks documents comply with their canonical templates."},
	{"tenant-predicate", "Enforces the multi-tenant isolation predicate (security veto-backed)."},
	{"test-deletion", "Guards deletion of test files against coverage loss."},
	{"test-pyramid", "Validates the unit/integration/e2e test-pyramid balance."},
	{"test-targets", "Verifies Taskfile test targets exist and are wired."},
	{"transit-coverage", "Enforces the OpenBao transit dispatcher invariant (encrypt/decrypt-for-store)."},
	{"trivy-scan", "Runs Trivy CVE scanning on filesystem/images."},
	{"trufflehog-scan", "Runs TruffleHog verified-secret scanning."},
	{"unhomed-logic", "Forbids un-homed operational logic that belongs in a pkg/quality gate."},
}

// gateRegistrySet returns the registry names as a set for membership checks.
func gateRegistrySet() map[string]bool {
	out := make(map[string]bool, len(gateRegistry))
	for _, g := range gateRegistry {
		out[g.Name] = true
	}
	return out
}

// taskWiredWord matches a canonical gate invocation on a YAML command-list line
// (`- <path>loopzero-ops quality <gate>`), not a prose mention like a task `desc:`.
// Anchoring to the `- ` cmd item excludes descriptions such as
// "Build the loopzero-ops quality binary" that would otherwise capture a false gate.
// The gate name must start alphanumeric ([a-z0-9]) so meta-flags of the tool
// itself (`--list`, `--names`, `--json`, `--help`) are NOT counted as gates.
// Compiled once at package scope.
var taskWiredWord = regexp.MustCompile(`(?m)^\s*-\s+\S*loopzero-ops quality ([a-z0-9][a-z0-9-]*)`)

// taskWiredSet returns the set of gate names invoked in the given taskfile via
// the canonical `loopzero-ops quality <name>` token. A read failure returns a typed
// error naming the path — never a silent empty set (which would mislabel every
// gate unwired and defeat the oracle).
func taskWiredSet(taskfilePath string) (map[string]bool, error) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		return nil, fmt.Errorf("read taskfile %s: %w", taskfilePath, err)
	}
	out := map[string]bool{}
	for _, m := range taskWiredWord.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = true
	}
	return out, nil
}

// runQualityList is the production entry point for `loopzero-ops quality --list`.
func runQualityList(args []string) int {
	return runQualityListTo(os.Stdout, args)
}

// runQualityListTo renders the gate inventory to w. Modes:
//
//	default  -> NAME\tWIRED\tSUMMARY  (one gate per line)
//	--names  -> NAME                  (names only; feeds the per-gate liveness loop)
//	--json   -> [{"name","wired","summary"}, ...]
//
// Output is always sorted by gate name and deterministic, so it can be captured
// as a byte-stable baseline (the MA-1 oracle).
func runQualityListTo(w io.Writer, args []string) int {
	fs := flag.NewFlagSet("quality --list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	namesOnly := fs.Bool("names", false, "print gate names only, one per line")
	asJSON := fs.Bool("json", false, "print the gate inventory as JSON")
	taskfile := fs.String("taskfile", "taskfiles/quality.yml", "taskfile scanned for the wired bit")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	wired, err := taskWiredSet(*taskfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quality --list: %v\n", err)
		return 1
	}

	specs := make([]gateSpec, len(gateRegistry))
	copy(specs, gateRegistry)
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })

	switch {
	case *asJSON:
		type row struct {
			Name    string `json:"name"`
			Wired   bool   `json:"wired"`
			Summary string `json:"summary"`
		}
		rows := make([]row, 0, len(specs))
		for _, g := range specs {
			rows = append(rows, row{g.Name, wired[g.Name], g.Summary})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fmt.Fprintf(os.Stderr, "quality --list: encode json: %v\n", err)
			return 1
		}
	case *namesOnly:
		for _, g := range specs {
			fmt.Fprintln(w, g.Name)
		}
	default:
		for _, g := range specs {
			bit := "unwired"
			if wired[g.Name] {
				bit = "wired"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", g.Name, bit, g.Summary)
		}
	}
	return 0
}
