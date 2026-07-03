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
	"strings"

	"gopkg.in/yaml.v3"
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
	{"db-generated-boundary", "MA-7 Gate A: no services/routes/server imports internal/db/generated."},
	{"ma7-status-locality", "MA-7 Gates B+C: sqlc status funcs + SafeTransition only in db/store/."},
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

// taskWiredWord captures the gate name in a `loopzero-ops quality <gate>` invocation.
// The gate name must start alphanumeric ([a-z0-9]) so the tool's own meta-flags
// (`--list`, `--names`, `--json`, `--help`) are NOT counted as gates. Applied
// ONLY to command strings extracted from `cmds:` blocks (see collectCmdStrings),
// so prose, `desc:` block scalars, and comments cannot leak a false gate.
// Compiled once at package scope.
var taskWiredWord = regexp.MustCompile(`loopzero-ops quality ([a-z0-9][a-z0-9-]*)`)

// taskGateTasks returns, per gate name, the SORTED set of top-level task names
// in the given taskfile whose `cmds:` invoke that gate. Detection is STRUCTURAL:
// the taskfile is YAML-parsed and only command strings under `cmds:` sequences
// are scanned — a `desc:` mention or a `#` comment can never be mistaken for an
// invocation. A read or parse failure returns a typed error naming the path —
// never a silent empty map (which would mislabel every gate un-wired).
//
// Per-task attribution (MEET-2026-07-02-001 MA-1; hardened per the
// verification-architect sign-off, MEET-2026-07-02-002-adjacent) replaces the
// former boolean whole-file UNION. The union masked a silent un-wire: a gate
// dropped from `quality:gates` but still referenced by `quality:diff` (or a
// stray `echo`) kept `wired=true`, so the baseline stayed byte-identical and the
// un-wire went undetected. Attributing each gate to the EXACT task set makes any
// drop / move (gates→diff) / removal change the baseline bytes, and lets the
// positive-control test assert the oracle fires (which the boolean union could
// not express). It is also honest: it shows whether a gate is enforced by the
// full-scan hard set (`quality:gates`), the changed-files set (`quality:diff`),
// both, or neither.
//
// Known limitation (no occurrences in the live taskfile): a gate name embedded
// inside a shell string within a cmd (e.g. `echo "loopzero-ops quality X"`) attributes
// X to that task, and a gate invoked only via `preconditions:` (not `cmds:`) is
// not counted. Both are acceptable for the repo's convention (all real
// invocations are direct `- <path>loopzero-ops quality <gate>` cmds items).
func taskGateTasks(taskfilePath string) (map[string][]string, error) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		return nil, fmt.Errorf("read taskfile %s: %w", taskfilePath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse taskfile %s: %w", taskfilePath, err)
	}
	acc := map[string]map[string]bool{} // gate -> set of task names
	for taskName, cmds := range collectTaskCmds(&doc) {
		for _, cmd := range cmds {
			for _, m := range taskWiredWord.FindAllStringSubmatch(cmd, -1) {
				if acc[m[1]] == nil {
					acc[m[1]] = map[string]bool{}
				}
				acc[m[1]][taskName] = true
			}
		}
	}
	out := make(map[string][]string, len(acc))
	for gate, tasks := range acc {
		list := make([]string, 0, len(tasks))
		for name := range tasks {
			list = append(list, name)
		}
		sort.Strings(list)
		out[gate] = list
	}
	return out, nil
}

// collectTaskCmds maps each top-level task name (under `tasks:`) to the command
// strings reachable under ITS OWN `cmds:` sequence. Task definitions do not nest
// task definitions, so a task body's cmds are unambiguously that task's. This is
// the per-task counterpart to the whole-file collectCmdStrings, and is what makes
// the wired attribution task-scoped instead of a file-wide union.
func collectTaskCmds(n *yaml.Node) map[string][]string {
	out := map[string][]string{}
	root := n
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "tasks" {
			continue
		}
		tasksMap := root.Content[i+1]
		if tasksMap.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(tasksMap.Content); j += 2 {
			taskName := tasksMap.Content[j].Value
			// collectCmdStrings on a single task body returns exactly that
			// task's cmds (a task body carries at most one `cmds:`).
			out[taskName] = collectCmdStrings(tasksMap.Content[j+1])
		}
	}
	return out
}

// collectCmdStrings walks a parsed taskfile and returns every command string
// reachable under a `cmds:` sequence. A cmds item is either a scalar (the
// command) or a mapping with a `cmd:` key (Task's map form); `task:`/`defer:`
// items carry no shell command and are skipped. Nodes outside `cmds:` (desc,
// vars, comments) are never scanned.
func collectCmdStrings(n *yaml.Node) []string {
	var out []string
	var walk func(node *yaml.Node)
	walk = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				if key.Value == "cmds" && val.Kind == yaml.SequenceNode {
					for _, item := range val.Content {
						switch item.Kind {
						case yaml.ScalarNode:
							out = append(out, item.Value)
						case yaml.MappingNode:
							for j := 0; j+1 < len(item.Content); j += 2 {
								if item.Content[j].Value == "cmd" {
									out = append(out, item.Content[j+1].Value)
								}
							}
						}
					}
				}
				walk(val)
			}
			return
		}
		for _, c := range node.Content {
			walk(c)
		}
	}
	walk(n)
	return out
}

// runQualityList is the production entry point for `loopzero-ops quality --list`.
func runQualityList(args []string) int {
	return runQualityListTo(os.Stdout, args)
}

// runQualityListTo renders the gate inventory to w. Modes:
//
//	default  -> NAME\tTASKS\tSUMMARY  (one gate per line; TASKS = comma-joined
//	            sorted task names invoking the gate, or "-" if none)
//	--names  -> NAME                  (names only; feeds the per-gate liveness loop)
//	--json   -> [{"name","tasks":[...],"summary"}, ...]
//
// TASKS replaces the former boolean WIRED bit (per-task attribution — see
// taskGateTasks). Output is always sorted by gate name and deterministic, so it
// can be captured as a byte-stable baseline (the MA-1 oracle). Any drop / move /
// removal of a gate invocation changes a TASKS cell, turning the baseline diff
// (and CI) red.
func runQualityListTo(w io.Writer, args []string) int {
	fs := flag.NewFlagSet("quality --list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	namesOnly := fs.Bool("names", false, "print gate names only, one per line")
	asJSON := fs.Bool("json", false, "print the gate inventory as JSON")
	taskfile := fs.String("taskfile", "taskfiles/quality.yml", "taskfile scanned for per-gate task attribution")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	gateTasks, err := taskGateTasks(*taskfile)
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
			Name    string   `json:"name"`
			Tasks   []string `json:"tasks"`
			Summary string   `json:"summary"`
		}
		rows := make([]row, 0, len(specs))
		for _, g := range specs {
			tasks := gateTasks[g.Name]
			if tasks == nil {
				tasks = []string{} // render [] not null
			}
			rows = append(rows, row{g.Name, tasks, g.Summary})
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
			tasksCol := "-"
			if ts := gateTasks[g.Name]; len(ts) > 0 {
				tasksCol = strings.Join(ts, ",")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", g.Name, tasksCol, g.Summary)
		}
	}
	return 0
}
