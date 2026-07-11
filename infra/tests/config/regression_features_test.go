// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPZERO_REGRESSION=1 go test ./infra/tests/load/ -run TestRegression -v
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

func TestF03CommanderExitOverrideRegistered(t *testing.T) {
	// M23 T6.1 post-M5 monorepo-decomposition guard: cli/ moved to sibling
	// loopzero-cli/ repo. Cross-repo test wiring is a follow-up milestone.
	const indexPath = "../../../cli/src/index.ts"
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Skipf("F-03 skipped: cli/ moved to sibling loopzero-cli/ repo per M5 decomposition; cross-repo test wiring TBD")
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read cli index.ts: %v", err)
	}
	s := string(data)
	// writeErr seta exitCode=1 (cobre parse errors via configureOutput path).
	if !strings.Contains(s, "process.exitCode = 1") {
		t.Error("F-03 regression: index.ts writeErr/exitOverride must set process.exitCode = 1 for parse errors")
	}
	// exitOverride com lista de codes Commander.
	for _, code := range []string{
		"commander.unknownCommand",
		"commander.unknownOption",
		"commander.missingArgument",
	} {
		if !strings.Contains(s, code) {
			t.Errorf("F-03 regression: exitOverride must handle %q", code)
		}
	}
}

func TestF05InitPreflightAuthDep(t *testing.T) {
	// M23 T6.1 post-M5 monorepo-decomposition guard: cli/ moved to sibling
	// loopzero-cli/ repo. Cross-repo test wiring is a follow-up milestone.
	const initPath = "../../../cli/src/commands/init.ts"
	if _, err := os.Stat(initPath); os.IsNotExist(err) {
		t.Skipf("F-05 skipped: cli/ moved to sibling loopzero-cli/ repo per M5 decomposition; cross-repo test wiring TBD")
	}
	data, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read init.ts: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "preflightAuth?:") {
		t.Error("F-05 regression: InitDeps must declare optional preflightAuth")
	}
	if !strings.Contains(s, "offline?:") {
		t.Error("F-05 regression: InitOptions must declare optional offline flag")
	}
	if !strings.Contains(s, "AUTH_FAILED") {
		t.Error("F-05 regression: init must emit code=AUTH_FAILED on 401/403 preflight")
	}
	if !strings.Contains(s, "PREFLIGHT_FAILED") {
		t.Error("F-05 regression: init must emit code=PREFLIGHT_FAILED on 5xx/network preflight")
	}
}

func TestF06LogoutHasTokenDep(t *testing.T) {
	// M23 T6.1 post-M5 monorepo-decomposition guard: cli/ moved to sibling
	// loopzero-cli/ repo per CLAUDE.md § Workspace layout. Skip when missing
	// (cross-repo test wiring is a follow-up milestone).
	logoutPath := "../../../cli/src/commands/logout.ts"
	if _, err := os.Stat(logoutPath); os.IsNotExist(err) {
		t.Skipf("F-06 skipped: cli/ moved to sibling loopzero-cli/ repo per M5 decomposition; cross-repo test wiring TBD")
	}
	data, err := os.ReadFile(logoutPath)
	if err != nil {
		t.Fatalf("read logout.ts: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "hasToken?:") {
		t.Error("F-06 regression: LogoutDeps must declare optional hasToken")
	}
	if !strings.Contains(s, "Already logged out") {
		t.Error("F-06 regression: logout must emit \"Already logged out\" when hasToken=false")
	}
	if !strings.Contains(s, "already_logged_out") {
		t.Error("F-06 regression: logout JSON must set status=already_logged_out")
	}
	// Wire-up no register.
	regData, err := os.ReadFile("../../../cli/src/register/auth.ts")
	if err != nil {
		t.Fatalf("read register/auth.ts: %v", err)
	}
	if !strings.Contains(string(regData), "hasToken: () => Boolean") {
		t.Error("F-06 regression: register/auth.ts must wire hasToken to LogoutCommand")
	}
}

func TestF07VersionEndpointRegistered(t *testing.T) {
	data, err := os.ReadFile("../../../api/internal/server/router.go")
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `r.Get("/api/v1/version"`) {
		t.Error("F-07 regression: router must register GET /api/v1/version")
	}
	if !strings.Contains(s, "routes.VersionHandler") {
		t.Error("F-07 regression: route must call routes.VersionHandler()")
	}
	// Handler module exists.
	if _, err := os.Stat("../../../api/internal/routes/version.go"); err != nil {
		t.Errorf("F-07 regression: api/internal/routes/version.go missing: %v", err)
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
