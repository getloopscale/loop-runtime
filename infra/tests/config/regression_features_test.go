// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package config — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPZERO_REGRESSION=1 go test ./infra/tests/config/ -run TestRegression -v
package config

import (
	"os"
	"strings"
	"testing"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
func TestF01LoopZeroApiNodePortInStandaloneMulti(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/values-standalone-multi.yaml")
	if err != nil {
		t.Fatalf("read standalone-multi overlay: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "type: NodePort") {
		t.Error("F-01 regression: values-standalone-multi.yaml must set service.type=NodePort")
	}
	if !strings.Contains(s, "nodePort: 30001") {
		t.Error("F-01 regression: nodePort must be 30001 (k3d host:13001 mapping)")
	}
	if !strings.Contains(s, "localhost: true") {
		t.Error("F-01 regression: ingress.localhost must be true so IngressRoute matches `localhost`")
	}
}

func TestF01LoopZeroApiHasLocalhostIngressRoute(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/templates/ingressroute.yaml")
	if err != nil {
		t.Fatalf("read ingressroute template: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "{{- if .Values.ingress.localhost }}") {
		t.Error("F-01 regression: ingressroute template must gate localhost route on .Values.ingress.localhost")
	}
	for _, host := range []string{"localhost", "127.0.0.1", "api.loopzero.local"} {
		if !strings.Contains(s, host) {
			t.Errorf("F-01 regression: localhost IngressRoute must match Host(`%s`)", host)
		}
	}
	// Em entry point web (HTTP) — TLS não funciona com localhost sem cert custom.
	if !strings.Contains(s, "name: loopzero-api-localhost") {
		t.Error("F-01 regression: localhost IngressRoute must be named loopzero-api-localhost (separate from prod)")
	}
}

func TestF01DefaultProfileKeepsClusterIP(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/values.yaml")
	if err != nil {
		t.Fatalf("read default values: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "type: ClusterIP") {
		t.Error("F-01 regression: default values.yaml must keep service.type=ClusterIP for dev/prod")
	}
	// nodePort default null → não renderiza nodePort field (cluster aloca random ou skip).
	if !strings.Contains(s, "nodePort: null") {
		t.Error("F-01 regression: default values.yaml must set service.nodePort=null (NodePort field absent in dev/prod)")
	}
	if !strings.Contains(s, "localhost: false") {
		t.Error("F-01 regression: default values.yaml must set ingress.localhost=false (only standalone-multi opts in)")
	}
}

// F-03, F-05 and F-06 were REMOVED, not skipped (B-180, applying B-169's standard).
//
// All three asserted about `cli/src/**`, which the M5 decomposition moved to the
// sibling `loopzero-cli` repository. Each stat'd a path under `../../../cli/` and
// skipped when it was missing -- conditional in form, unconditional in fact:
// `cli/` is not in this tree and `loopzero-cli` is not cloned beside it, so the skip
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
//     runs loopzero-cli's half when the sibling IS present and prints an explicit
//     SKIP line when it is not.
//
// What is genuinely NOT covered here, said plainly rather than implied: the CLI's
// own internals -- Commander's exitOverride codes (F-03), init's AUTH_FAILED /
// PREFLIGHT_FAILED vocabulary (F-05) and logout's hasToken wiring (F-06) -- are
// loopzero-cli's behaviour and belong to loopzero-cli's suite. Asserting about them from
// a repository that does not contain them is what produced three permanent skips.
//
// The class is now guarded: `loopzero-ops quality test-skip-absent-path` fails when a
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
	// The family moved to its own subpackage (B-041 slice-8), so the qualifier is
	// the family name, not the parent package. What F-07 protects is that the
	// route is REGISTERED and its handler EXISTS — both survive the move; only
	// the two spellings changed.
	if !strings.Contains(s, "version.VersionHandler") {
		t.Error("F-07 regression: route must call version.VersionHandler()")
	}
	// Handler module exists.
	if _, err := os.Stat("../../../api/internal/routes/version/version.go"); err != nil {
		t.Errorf("F-07 regression: api/internal/routes/version/version.go missing: %v", err)
	}
}

func TestF08LoopZeroApiServiceMonitorOptIn(t *testing.T) {
	dep, err := os.ReadFile("../../helm/loopzero-api/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read deployment.yaml: %v", err)
	}
	depStr := string(dep)
	// Container expõe :9090.
	if !strings.Contains(depStr, "containerPort: 9090") {
		t.Error("F-08 regression: deployment template must declare containerPort 9090 for metrics scraping")
	}
	if !strings.Contains(depStr, "name: metrics") {
		t.Error("F-08 regression: container port must be named `metrics` (referenced by Service.targetPort)")
	}
	// Service expõe metrics port.
	if !strings.Contains(depStr, "targetPort: metrics") {
		t.Error("F-08 regression: Service must include port targetPort=metrics so ServiceMonitor scrape works")
	}
	// ServiceMonitor template existe + condicional.
	sm, err := os.ReadFile("../../helm/loopzero-api/templates/servicemonitor.yaml")
	if err != nil {
		t.Fatalf("F-08 regression: servicemonitor.yaml missing: %v", err)
	}
	smStr := string(sm)
	if !strings.Contains(smStr, "{{- if .Values.serviceMonitor.enabled }}") {
		t.Error("F-08 regression: ServiceMonitor must be opt-in via .Values.serviceMonitor.enabled")
	}
	if !strings.Contains(smStr, "port: metrics") {
		t.Error("F-08 regression: ServiceMonitor endpoints must reference port name=metrics")
	}
	// Default values.yaml: opt-out (standalone-multi sem prom-operator).
	values, err := os.ReadFile("../../helm/loopzero-api/values.yaml")
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	if !strings.Contains(string(values), "serviceMonitor:") {
		t.Error("F-08 regression: values.yaml must declare serviceMonitor block (default enabled=false)")
	}
	if !strings.Contains(string(values), "enabled: false") {
		t.Error("F-08 regression: serviceMonitor.enabled default must be false (opt-in via overlay)")
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
	// Lua deve usar `obj.spec` para IngressRoute (não `obj.status` — IngressRoute
	// não tem .status field).
	if !strings.Contains(s, "obj.spec.routes") {
		t.Error("F-11 regression: IngressRoute health must check obj.spec.routes (no .status field)")
	}
	// Certificate deve parsear .status.conditions Ready type.
	if !strings.Contains(s, "obj.status.conditions") || !strings.Contains(s, `c.type == "Ready"`) {
		t.Error("F-11 regression: Certificate health must parse status.conditions Ready type")
	}
}

func TestF12LoopZeroApiTokensSecretGenerated(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "loopzero-api-tokens") {
		t.Error("F-12 regression: generate-secrets.sh must create loopzero-api-tokens Secret")
	}
	if !strings.Contains(s, "argocd-token") {
		t.Error("F-12 regression: loopzero-api-tokens must include argocd-token key (JWT for /health/deep probe)")
	}
	if !strings.Contains(s, "/api/v1/session") {
		t.Error("F-12 regression: script must POST to argocd /api/v1/session for JWT generation")
	}
	// Idempotência: se login falhar, preserve existing token. Helper renamed
	// from `get_existing` to `get_or_generate` (latter handles both branches).
	if !strings.Contains(s, "loopzero-api-tokens loopzero-system argocd-token") {
		t.Error("F-12 regression: script must look up existing argocd-token in loopzero-api-tokens for mid-bootstrap idempotency")
	}
	if !strings.Contains(s, "preserving existing argocd-token") {
		t.Error("F-12 regression: script must log preservation of existing argocd-token on login failure")
	}
	// loopzero-api template DEVE referenciar o Secret.
	dep, err := os.ReadFile("../../helm/loopzero-api/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read loopzero-api deployment: %v", err)
	}
	depStr := string(dep)
	if !strings.Contains(depStr, "name: ARGOCD_TOKEN") {
		t.Error("F-12 regression: loopzero-api Deployment must export env ARGOCD_TOKEN")
	}
	if !strings.Contains(depStr, "name: loopzero-api-tokens") {
		t.Error("F-12 regression: ARGOCD_TOKEN must come from secretKeyRef loopzero-api-tokens")
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
	// DRY_RUN default (não destrutivo sem --apply explícito).
	if !strings.Contains(s, "DRY_RUN=1") {
		t.Error("F-04 regression: script must default to DRY_RUN=1 (require explicit --apply for destructive ops)")
	}
	// Backup before destructive ops.
	if !strings.Contains(s, "BACKUP_DIR=") || !strings.Contains(s, "tenant-migration-") {
		t.Error("F-04 regression: script must backup CR YAML to ~/.loopzero/backups/tenant-migration-* before delete")
	}
	// decide_winner logic: prefer canonical (loopzero-system) by creationTimestamp.
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
	// Condition reason "SourceMissing" para o caso esperado.
	if !strings.Contains(s, `"SourceMissing"`) {
		t.Error("F-09 regression: NotFound case must set condition reason=SourceMissing")
	}
}
