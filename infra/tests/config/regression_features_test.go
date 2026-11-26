// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package config — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOP_RUNTIME_REGRESSION=1 go test ./infra/tests/config/ -run TestRegression -v
package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
// F-01 publishes the API to the user without `tilt up`: NodePort 30001 is what the
// k3d serverlb maps to host port 13001 (infra/k3d/core-cluster.yaml). It lived in
// infra/helm/loop-runtime-api/values-standalone-multi.yaml, whose helmfile guard applied it
// only when clusterRole was "standalone" — the one case the release was NOT
// installed — so the overlay reached no install. The environment that does publish
// the port is dev-public, and that overlay is what this pins now.
func TestF01LoopRuntimeApiNodePortInDevPublic(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/values/dev-public/loop-runtime-api.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read dev-public overlay: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "type: NodePort") {
		t.Error("F-01 regression: the dev-public overlay must set service.type=NodePort")
	}
	if !strings.Contains(s, "nodePort: 30001") {
		t.Error("F-01 regression: nodePort must be 30001 (k3d host:13001 mapping)")
	}
}

// The localhost IngressRoute F-01 also added (Host localhost / 127.0.0.1 /
// api.loop-runtime.local on the `web` entry point) was enabled by the same unreachable
// overlay, so no install ever rendered it; it was not carried into charts/loop-runtime-api
// and its test went with it (docs/decisions.md, "One chart per bootstrap/published
// pair").

func TestF01DefaultProfileKeepsClusterIP(t *testing.T) {
	data, err := os.ReadFile("../../../charts/loop-runtime-api/values.yaml")
	if err != nil {
		t.Fatalf("read default values: %v", err)
	}
	var doc struct {
		Service struct {
			Type     string `yaml:"type"`
			NodePort *int   `yaml:"nodePort"`
		} `yaml:"service"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse default values: %v", err)
	}
	if doc.Service.Type != "ClusterIP" {
		t.Errorf("F-01 regression: charts/loop-runtime-api/values.yaml must keep service.type=ClusterIP, got %q", doc.Service.Type)
	}
	// A null nodePort renders no nodePort field, so the cluster picks one only when an
	// install asks for NodePort without pinning it.
	if doc.Service.NodePort != nil {
		t.Errorf("F-01 regression: service.nodePort must default to null, got %d", *doc.Service.NodePort)
	}
}

// F-03, F-05 and F-06 were REMOVED, not skipped (B-180, applying B-169's standard).
//
// All three asserted about `cli/src/**`, which the M5 decomposition moved to the
// sibling `loop-runtime-cli` repository. Each stat'd a path under `../../../cli/` and
// skipped when it was missing -- conditional in form, unconditional in fact:
// `cli/` is not in this tree and `loop-runtime-cli` is not cloned beside it, so the skip
// branch was the only branch ever taken since M5. F-06 was worse than dead: its
// body continued past the skip with an UNGUARDED os.ReadFile of
// `../../../cli/src/register/auth.ts` and a t.Fatalf, unreachable code that read
// as a live assertion.
//
// Why removed rather than left skipping (B-169's three reasons, re-measured here):
//   - a permanent t.Skip is a dead-Skip: a deleted test with good manners, and
//     without `-v` the package still prints `ok`;
//   - a mid-function t.Skip marks the WHOLE test skipped, so it cannot be used
//     to fence off the part of an assertion that IS this repo's;
//   - the seam these three probed already has guards that execute. The cli<->
//     engine contract is enumerated in `docs/cross-repo-contract-matrix.md` and
//     enforced from this side by `api/internal/contract/cli_consumer_test.go`
//     (TestCLIConsumer_AllPathsInSpec is hermetic: it reads the pinned
//     `testdata/cli-consumed-paths.txt` against `api/openapi/engine.yaml` and
//     needs no sibling on disk), plus `task quality:contract:cross-repo`, which
//     runs loop-runtime-cli's half when the sibling IS present and prints an explicit
//     SKIP line when it is not.
//
// What is genuinely NOT covered here, said plainly rather than implied: the CLI's
// own internals -- Commander's exitOverride codes (F-03), init's AUTH_FAILED /
// PREFLIGHT_FAILED vocabulary (F-05) and logout's hasToken wiring (F-06) -- are
// loop-runtime-cli's behaviour and belong to loop-runtime-cli's suite. Asserting about them from
// a repository that does not contain them is what produced three permanent skips.
//
// The class is now guarded: `loop-runtime-ops quality test-skip-absent-path` fails when a
// test under infra/tests/ skips on a path absent from this tree.

func TestF07VersionEndpointRegistered(t *testing.T) {
	data, err := os.ReadFile("../../../api/internal/server/router.go")
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `r.Get("/api/v1/version"`) {
		t.Error("F-07 regression: router must register GET /api/v1/version")
	}
	// The family moved to its own subpackage (B-041 slice-8), and then under the
	// version feature (api/internal organized by feature, 2026-09-25), so the
	// qualifier is the HTTP package's name. What F-07 protects is that the route
	// is REGISTERED and its handler EXISTS — both survive the moves; only the
	// spellings changed.
	if !strings.Contains(s, "versionhttp.VersionHandler") {
		t.Error("F-07 regression: route must call versionhttp.VersionHandler()")
	}
	// Handler module exists.
	if _, err := os.Stat("../../../api/internal/version/versionhttp/version.go"); err != nil {
		t.Errorf("F-07 regression: api/internal/version/versionhttp/version.go missing: %v", err)
	}
}

func TestF08LoopRuntimeApiServiceMonitorOptIn(t *testing.T) {
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile("../../../charts/loop-runtime-api/" + rel)
		if err != nil {
			t.Fatalf("read charts/loop-runtime-api/%s: %v", rel, err)
		}
		return string(data)
	}
	dep := read("templates/deployment.yaml")
	if !strings.Contains(dep, "containerPort: {{ .Values.service.metricsPort }}") {
		t.Error("F-08 regression: the container must declare the metrics port for scraping")
	}
	if !strings.Contains(dep, "- name: metrics") {
		t.Error("F-08 regression: the container port must be named `metrics` (referenced by Service.targetPort)")
	}
	if !strings.Contains(read("templates/service.yaml"), "targetPort: metrics") {
		t.Error("F-08 regression: the Service must expose targetPort=metrics so the ServiceMonitor scrape works")
	}
	if !strings.Contains(read("templates/servicemonitor.yaml"), `include "loop-runtime-common.servicemonitor"`) {
		t.Error("F-08 regression: the ServiceMonitor must come from loop-runtime-common.servicemonitor, which is gated on metrics.serviceMonitor.enabled")
	}
	values := read("values.yaml")
	if !strings.Contains(values, "serviceMonitor:") || !strings.Contains(values, "port: metrics") {
		t.Error("F-08 regression: values.yaml must declare metrics.serviceMonitor scraping port=metrics")
	}
	if !strings.Contains(values, "metricsPort: 9090") {
		t.Error("F-08 regression: service.metricsPort must default to 9090, where the binary serves /metrics")
	}
}

func TestF11ArgocdLuaHealthChecksDefined(t *testing.T) {
	data, err := os.ReadFile("../../helm/argocd/values.yaml")
	if err != nil {
		t.Fatalf("read argocd values.yaml: %v", err)
	}
	s := string(data)
	expected := []string{
		"resource.customizations.health.traefik.io_IngressRoute",
		"resource.customizations.health.traefik.io_Middleware",
		"resource.customizations.health.cert-manager.io_Certificate",
	}
	for _, key := range expected {
		if !strings.Contains(s, key) {
			t.Errorf("F-11 regression: argocd values.yaml must declare %q Lua health check", key)
		}
	}
	// Lua must use `obj.spec` for IngressRoute (not `obj.status` — IngressRoute
	// has no .status field).
	if !strings.Contains(s, "obj.spec.routes") {
		t.Error("F-11 regression: IngressRoute health must check obj.spec.routes (no .status field)")
	}
	// Certificate must parse the .status.conditions Ready type.
	if !strings.Contains(s, "obj.status.conditions") || !strings.Contains(s, `c.type == "Ready"`) {
		t.Error("F-11 regression: Certificate health must parse status.conditions Ready type")
	}
}

func TestF12LoopRuntimeApiTokensSecretGenerated(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "loop-runtime-api-tokens") {
		t.Error("F-12 regression: generate-secrets.sh must create loop-runtime-api-tokens Secret")
	}
	if !strings.Contains(s, "argocd-token") {
		t.Error("F-12 regression: loop-runtime-api-tokens must include argocd-token key (JWT for /health/deep probe)")
	}
	if !strings.Contains(s, "/api/v1/session") {
		t.Error("F-12 regression: script must POST to argocd /api/v1/session for JWT generation")
	}
	// Idempotency: if login fails, preserve the existing token. Helper renamed
	// from `get_existing` to `get_or_generate` (latter handles both branches).
	if !strings.Contains(s, "loop-runtime-api-tokens loop-runtime-system argocd-token") {
		t.Error("F-12 regression: script must look up existing argocd-token in loop-runtime-api-tokens for mid-bootstrap idempotency")
	}
	if !strings.Contains(s, "preserving existing argocd-token") {
		t.Error("F-12 regression: script must log preservation of existing argocd-token on login failure")
	}
	// The loop-runtime-api install MUST reference the Secret. It is declared as a typed
	// secrets.keyRefs entry of the values Helmfile installs the chart with.
	dep, err := os.ReadFile("../../../charts/loop-runtime-api/values-bootstrap.yaml")
	if err != nil {
		t.Fatalf("read loop-runtime-api bootstrap values: %v", err)
	}
	depStr := string(dep)
	if !strings.Contains(depStr, "name: ARGOCD_TOKEN") {
		t.Error("F-12 regression: the loop-runtime-api install must export env ARGOCD_TOKEN")
	}
	if !strings.Contains(depStr, "secretName: loop-runtime-api-tokens") {
		t.Error("F-12 regression: ARGOCD_TOKEN must come from secretKeyRef loop-runtime-api-tokens")
	}
}

func TestF04MigrationScriptExists(t *testing.T) {
	const path = "../../scripts/production/migrate-tenant-namespace.sh"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("F-04 regression: migration script missing: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration script: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "skip-reconcile=true") {
		t.Error("F-04 regression: script must annotate stale CR with skip-reconcile BEFORE removing finalizer (incident 2026-04-29 iter 3 — cascade delete destroyed shared resources)")
	}
	if !strings.Contains(s, "force-reconcile=") {
		t.Error("F-04 regression: script must trigger force-reconcile on canonical CR after delete to re-validate shared resources")
	}
	// DRY_RUN by default (non-destructive without an explicit --apply).
	if !strings.Contains(s, "DRY_RUN=1") {
		t.Error("F-04 regression: script must default to DRY_RUN=1 (require explicit --apply for destructive ops)")
	}
	// Backup before destructive ops.
	if !strings.Contains(s, "BACKUP_DIR=") || !strings.Contains(s, "tenant-migration-") {
		t.Error("F-04 regression: script must backup CR YAML to ~/.loop-runtime/backups/tenant-migration-* before delete")
	}
	// decide_winner logic: prefer canonical (loop-runtime-system) by creationTimestamp.
	if !strings.Contains(s, "decide_winner") || !strings.Contains(s, "creationTimestamp") {
		t.Error("F-04 regression: script must compare creationTimestamp to decide canonical winner")
	}
}

func TestF09EnsurePullSecretHandlesNotFound(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: operator-split T1.4 moved this
	// file from `operators/internal/controller/` to
	// `operators/internal/tenant/` (tenant-operator binary owns the
	// EnvironmentReconciler). See finding `concepts/operations/
	// finding-2026-05-15-infra-tests-load-regression-obsolete.md`.
	data, err := os.ReadFile("../../../operators/internal/environment/environment_rbac.go")
	if err != nil {
		t.Fatalf("read environment_rbac.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "errors.IsNotFound") {
		t.Error("F-09 regression: ensurePullSecret path must check errors.IsNotFound to distinguish missing source from real failures")
	}
	// Condition reason "SourceMissing" for the expected case.
	if !strings.Contains(s, `"SourceMissing"`) {
		t.Error("F-09 regression: NotFound case must set condition reason=SourceMissing")
	}
}
