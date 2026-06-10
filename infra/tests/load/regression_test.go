// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//   LOOPZERO_REGRESSION=1 go test ./infra/tests/load/ -run TestRegression -v
package load

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
func runRegressionScript(t *testing.T) (string, int) {
	t.Helper()
	scriptPath := "../../helmfile/scripts/regression-checks.sh"
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("regression-checks.sh not found at %s", scriptPath)
	}
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), "LOOPZERO_DOMAIN=dev.loopzero.example")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return buf.String(), exitCode
}

func TestRegressionShellScriptIsExecutable(t *testing.T) {
	scriptPath := "../../helmfile/scripts/regression-checks.sh"
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("regression-checks.sh missing: %v", err)
	}
	mode := info.Mode().Perm()
	if mode&0o100 == 0 {
		t.Fatalf("regression-checks.sh is not executable (mode=%v)", mode)
	}
}

func TestRegressionScriptHasAllBugChecks(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/regression-checks.sh")
	if err != nil {
		t.Fatalf("read regression-checks.sh: %v", err)
	}
	content := string(data)
	for _, bug := range []string{"B1", "B2", "B3", "B4", "B5", "B6", "B7", "B11", "B16", "B18"} {
		if !strings.Contains(content, bug) {
			t.Errorf("regression-checks.sh missing reference to %s", bug)
		}
	}
}

func TestRegressionMatrixDocumentsAllBugs(t *testing.T) {
	t.Skip("runbook files removed — docs/runbooks/ deleted in governance consolidation 2026-05-11")
}

func TestRegressionRunsAgainstLiveCluster(t *testing.T) {
	if os.Getenv("LOOPZERO_REGRESSION_LIVE") == "" {
		t.Skip("LOOPZERO_REGRESSION_LIVE not set — skipping live regression run")
	}
	out, exit := runRegressionScript(t)
	t.Logf("regression-checks.sh exit=%d output:\n%s", exit, out)
	if exit != 0 {
		t.Fatalf("regression suite failed with exit %d", exit)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("expected at least one PASS line in output")
	}
}

// TestB18RuntimeMaxNodesConfig verifies the Pulumi config persists the
// runtimeMaxNodes>=8 setting required for L3 burst.
func TestB18RuntimeMaxNodesConfig(t *testing.T) {
	data, err := os.ReadFile("../../pulumi/Pulumi.dev.yaml")
	if err != nil {
		t.Fatalf("read Pulumi.dev.yaml: %v", err)
	}
	if !strings.Contains(string(data), "runtimeMaxNodes: \"8\"") {
		t.Errorf("B18 regression: runtimeMaxNodes is not 8 in Pulumi.dev.yaml")
	}
}

// TestB16ClusterImagePolicyChartExists verifies the Helm chart is present.
func TestB16ClusterImagePolicyChartExists(t *testing.T) {
	chartFile := "../../helm/admission-policies/Chart.yaml"
	if _, err := os.Stat(chartFile); err != nil {
		t.Fatalf("B16 regression: %s missing — was T0.1 reverted? %v", chartFile, err)
	}
	tplFile := "../../helm/admission-policies/templates/cluster-image-policy.yaml"
	if _, err := os.Stat(tplFile); err != nil {
		t.Fatalf("B16 regression: ClusterImagePolicy template missing: %v", err)
	}
}

// CHG-2026-04-28-LAYERED-BOOTSTRAP iter 28+ — regression tests for bugs B19-B58
// discovered during bootstrap end-to-end live in k3d (iter 22-27).
// Each test asserts the structural fix is in place — file content checks
// that detect regression at static-analysis time (no cluster needed).

// TestB19RedisLegacyImage — Bitnami removed bitnami/redis tag in mid-2025.
// Helmfile must use bitnamilegacy/redis mirror.
func TestB19RedisLegacyImage(t *testing.T) {
	data, err := os.ReadFile("../../helm/redis/values.yaml")
	if err != nil {
		t.Fatalf("read redis values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "repository: bitnamilegacy/redis") {
		t.Errorf("B19 regression: redis values.yaml must override image.repository to bitnamilegacy/redis")
	}
	if !strings.Contains(s, "repository: bitnamilegacy/redis-exporter") {
		t.Errorf("B19 regression: redis values.yaml must override metrics.image.repository to bitnamilegacy/redis-exporter")
	}
}

// TestB20NfsStorageClassNotHardcoded — `do-block-storage` must not be hardcoded
// in nfs-server-provisioner values; standalone needs cluster default.
func TestB20NfsStorageClassNotHardcoded(t *testing.T) {
	data, err := os.ReadFile("../../helm/nfs-server-provisioner/values.yaml")
	if err != nil {
		t.Fatalf("read nfs values.yaml: %v", err)
	}
	s := string(data)
	if strings.Contains(s, `storageClass: "do-block-storage"`) {
		t.Errorf("B20 regression: do-block-storage hardcoded in NFS values (must be per-env override)")
	}
}

// TestB21PostgresChartVersion18 — bitnami/postgresql 16.0.7 was removed from
// the public index; chart must be pinned to 18.x.
func TestB21PostgresChartVersion18(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/mgmt.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "version: 16.0.7") {
		t.Errorf("B21 regression: postgresql chart still pinned to 16.0.7 (deleted from index)")
	}
	if !strings.Contains(s, "version: 18.6.2") {
		t.Errorf("B21 regression: postgresql chart should be pinned to 18.6.2")
	}
}

// TestB22KratosNeedsPostgresConditional — when inClusterPostgres=true,
// ory-kratos must `needs:` postgresql release.
func TestB22KratosNeedsPostgresConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/mgmt.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "{{- if .Values.inClusterPostgres }}") {
		t.Errorf("B22 regression: conditional `needs:` block missing for inClusterPostgres")
	}
	if !strings.Contains(s, "- loopzero-system/postgresql") {
		t.Errorf("B22 regression: kratos/gitea must declare needs:postgresql")
	}
}

// TestB23ServiceMonitorDisabled — postgres+harbor must have serviceMonitor.enabled:false
// because we use prometheus-community/prometheus (no Operator CRDs).
func TestB23ServiceMonitorDisabled(t *testing.T) {
	files := []string{
		"../../helm/postgresql/values.yaml",
		"../../helm/harbor/values.yaml",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		// Walk through ALL serviceMonitor: occurrences and check each has
		// `enabled: false` before any `enabled: true` within the next 500 chars.
		offset := 0
		for {
			pos := strings.Index(s[offset:], "serviceMonitor:")
			if pos < 0 {
				break
			}
			abs := offset + pos
			window := s[abs:min(abs+500, len(s))]
			falseIdx := strings.Index(window, "enabled: false")
			trueIdx := strings.Index(window, "enabled: true")
			// FAIL only if `enabled: true` appears BEFORE `enabled: false`
			// (or `enabled: false` is absent entirely).
			if trueIdx >= 0 && (falseIdx < 0 || trueIdx < falseIdx) {
				t.Errorf("B23 regression: %s has serviceMonitor.enabled:true (Prometheus Operator absent)", f)
			}
			offset = abs + len("serviceMonitor:")
		}
	}
}

// TestB25MinioDistributedConditional — values-distributed.yaml must only apply
// in cloud profiles (not standalone or standalone-multi).
//
// History: original implementation used `{{- if ne .Environment.Name "standalone" }}`
// (negation). After standalone-multi was introduced, the gate was rewritten as a
// positive allowlist (`dev` OR `prod`) so future environments default to OFF for
// distributed MinIO — safer policy. Test asserts the allowlist exists immediately
// before the values-distributed.yaml include.
func TestB25MinioDistributedConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/mgmt.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	idx := strings.Index(s, "../helm/minio/values-distributed.yaml")
	if idx < 0 {
		t.Fatal("B25: values-distributed.yaml include not found")
	}
	// Walk back ~400 chars and assert one of: legacy negation OR positive allowlist.
	start := idx - 400
	if start < 0 {
		start = 0
	}
	context := s[start:idx]
	gateLegacy := strings.Contains(context, `{{- if ne .Environment.Name "standalone" }}`)
	gateAllowlist := strings.Contains(context, `{{- if or (eq .Environment.Name "dev") (eq .Environment.Name "prod") }}`)
	if !gateLegacy && !gateAllowlist {
		t.Errorf("B25 regression: minio values-distributed.yaml must be gated to cloud profiles only (dev|prod)")
	}
}

// TestB26MetricsServerNotInstalled — pre-installed by k3d/DOKS; default false.
func TestB26MetricsServerInstalledFalse(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/mgmt.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `installed: {{ .Values | get "installMetricsServer" false }}`) {
		t.Errorf("B26 regression: metrics-server release must be installed:false by default")
	}
}

// TestB27PostgresInitdbCreatesAllDBs — gitea, kratos, hydra, harbor.
func TestB27PostgresInitdbAllDBs(t *testing.T) {
	data, err := os.ReadFile("../../helm/postgresql/values.yaml")
	if err != nil {
		t.Fatalf("read postgresql values.yaml: %v", err)
	}
	s := string(data)
	required := []string{"CREATE DATABASE gitea", "CREATE DATABASE kratos", "CREATE DATABASE hydra", "CREATE DATABASE harbor"}
	for _, r := range required {
		if !strings.Contains(s, r) {
			t.Errorf("B27 regression: postgresql initdb missing `%s`", r)
		}
	}
}

// TestB28GiteaHaConditional — gitea values-ha.yaml must be cloud-profile only.
//
// History: same as B25 — gate migrated from `ne standalone` (negation) to
// `dev OR prod` (positive allowlist) when standalone-multi was added.
func TestB28GiteaHaConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/mgmt.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	// Find gitea release block
	idx := strings.Index(s, "- name: gitea\n")
	if idx < 0 {
		t.Fatal("B28: gitea release block not found")
	}
	end := strings.Index(s[idx+10:], "- name:")
	if end < 0 {
		end = len(s) - idx
	}
	block := s[idx : idx+10+end]
	gateLegacy := strings.Contains(block, `{{- if ne .Environment.Name "standalone" }}`)
	gateAllowlist := strings.Contains(block, `{{- if or (eq .Environment.Name "dev") (eq .Environment.Name "prod") }}`)
	if !gateLegacy && !gateAllowlist {
		t.Errorf("B28 regression: gitea values-ha.yaml must be gated to cloud profiles only (dev|prod)")
	}
}

// TestB29RedisServiceHostname — Gitea Redis service hostname.
//
// History (B29 original, 2026-04-28): Bitnami redis chart was in use
// and created a `<release>-master` Service, so the canonical hostname
// was `gitea-redis-master:6379`. The original assertion forbade the
// `gitea-redis:6379` bareword (which would DNS-no-such-host).
//
// Update (2026-05-05, commit bc95ca34 charts-sota plan):
// `infra/helm/gitea/templates/redis.yaml` now ships a standalone Redis
// StatefulSet whose Service is named `gitea-redis` (NOT
// `gitea-redis-master`). The values.yaml self-documents this with a
// 5-line comment block. The assertions had to invert: forbid
// `gitea-redis-master:6379`, require `gitea-redis:6379`.
//
// This test was caught in 2026-05-15 broad-verification iter 4 — see
// finding `concepts/operations/finding-2026-05-15-infra-tests-load-
// regression-obsolete.md` for the sibling Class A invertion bugs.
func TestB29RedisServiceHostname(t *testing.T) {
	data, err := os.ReadFile("../../helm/gitea/values.yaml")
	if err != nil {
		t.Fatalf("read gitea values.yaml: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "addr=gitea-redis-master:6379") {
		t.Errorf("B29 regression: gitea uses 'addr=gitea-redis-master:6379' (legacy Bitnami chart; current standalone Redis Service is `gitea-redis`)")
	}
	if !strings.Contains(s, "addr=gitea-redis:6379") {
		t.Errorf("B29 regression: gitea must use 'addr=gitea-redis:6379' (standalone Redis StatefulSet from `infra/helm/gitea/templates/redis.yaml`)")
	}
}

// TestB31ImageTagsPinned — controllers/api/cloud tags must match the canonical
// regex defined in ADR-0020 D1: semver | sha-* | latest.
//
// History: original test required pinning to a single branch hot-patch tag
// (`REDACTED_SECRET_061`). That was the exact anti-pattern ADR-0020 banned —
// branch tags mask the Git ↔ runtime relationship. The decision now permits:
//
//   - semver: v0.x.y, v1.0.0-rc1
//   - SHA: sha-{hex} or {hex}
//   - latest: dev convention with pullPolicy=Always (regression matrix #119)
//
// VAP `loopzero-disallow-latest-tag` blocks `:latest` in workload tenants in
// production. Branch-name tags remain rejected.
func TestB31ImageTagsPinned(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: `infra/helm/loopzero-controller/`
	// and `infra/helm/tenant-controller/` were DELETED in T1.5 (commit
	// 27cfe6cd, ADR-0023 operator-split). The same tag-pinning invariant
	// is now enforced for the 4 split-binary charts via the OCI-published
	// `charts/loopzero-*-operator/values.yaml` files. Re-targeted to loopzero-api
	// only (the surviving infra/helm chart of the original 3); split-
	// operator tag-pinning is covered by the Helm CI per chart.
	files := []string{
		"../../helm/loopzero-api/values.yaml",
	}
	semverRE := regexp.MustCompile(`(?m)^\s*tag:\s*v\d+\.\d+\.\d+(-[a-z0-9.-]+)?\s*$`)
	shaRE := regexp.MustCompile(`(?m)^\s*tag:\s*(sha-)?[a-f0-9]{8,40}\s*$`)
	latestRE := regexp.MustCompile(`(?m)^\s*tag:\s*latest\s*$`)
	branchRE := regexp.MustCompile(`(?m)^\s*tag:\s*(chg|feat|fix|hotfix|achado)[a-z0-9-]*\s*$`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if branchRE.MatchString(s) {
			t.Errorf("B31 regression: %s pins to a branch-name tag (banned by ADR-0020 D1)", f)
		}
		if !(semverRE.MatchString(s) || shaRE.MatchString(s) || latestRE.MatchString(s)) {
			t.Errorf("B31 regression: %s tag must match semver | sha-* | latest (ADR-0020 D1)", f)
		}
	}
}

// TestB32ControllerImageRepository — controllers must use loopzero-tenant-controller
// (the operators monolith binary), not loopzero-api (HTTP API server).
func TestB32ControllerImageRepository(t *testing.T) {
	t.Skip("loopzero-controller chart deleted T1.5 (commit 27cfe6cd) — split-operator image repos covered by per-chart Helm CI")
	files := []string{
		"../../helm/loopzero-controller/values.yaml",
		"../../helm/tenant-controller/values.yaml",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if !strings.Contains(s, "repository: registry.digitalocean.com/loopzero-system/loopzero-tenant-controller") {
			t.Errorf("B32 regression: %s repository must be loopzero-tenant-controller (operators binary)", f)
		}
	}
}

// TestB33B36GenerateSecretsStandaloneFallback — script must create
// loopzero-db-credentials and loopzero-cluster-kubeconfigs even without LOOPZERO_DB_HOST.
func TestB33B36GenerateSecretsStandaloneFallback(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "B33: standalone fallback") {
		t.Errorf("B33 regression: generate-secrets.sh missing standalone fallback marker for loopzero-db-credentials")
	}
	if !strings.Contains(s, "B36: standalone — generating synthetic in-cluster kubeconfig") {
		t.Errorf("B36 regression: generate-secrets.sh missing synthetic kubeconfig generator")
	}
	if !strings.Contains(s, "loopzero-cluster-kubeconfigs") {
		t.Errorf("B36 regression: generate-secrets.sh must create loopzero-cluster-kubeconfigs Secret")
	}
}

// TestB35DatabaseUrlMapping — chart deployment templates must map secret key
// `uri` to env var `DATABASE_URL` (app expectation differs from secret schema).
func TestB35DatabaseUrlMapping(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: `loopzero-controller` + `tenant-
	// controller` charts DELETED in T1.5 (commit 27cfe6cd). The 4 split
	// operators do NOT receive DATABASE_URL directly (they use the
	// API's data-access layer); only loopzero-api and loopzero-cloud carry
	// the env. See finding `concepts/operations/finding-2026-05-15-
	// infra-tests-load-regression-obsolete.md`.
	files := []string{
		"../../helm/loopzero-api/templates/deployment.yaml",
		"../../helm/loopzero-cloud/templates/deployment.yaml",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if !strings.Contains(s, "name: DATABASE_URL") {
			t.Errorf("B35 regression: %s must export env DATABASE_URL", f)
		}
		if !strings.Contains(s, "key: uri") {
			t.Errorf("B35 regression: %s must map secret key 'uri' to DATABASE_URL", f)
		}
	}
}

// TestB40LoopZeroApiServicePort3001 — service.port must match binary listen port.
func TestB40LoopZeroApiServicePort3001(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/values.yaml")
	if err != nil {
		t.Fatalf("read loopzero-api values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "port: 3001") {
		t.Errorf("B40 regression: loopzero-api service.port must be 3001 (matches binary listen)")
	}
}

// TestB41ControllersGiteaTokenEnv — controllers must map gitea-admin.token → GITEA_TOKEN.
func TestB41ControllersGiteaTokenEnv(t *testing.T) {
	t.Skip("loopzero-controller chart deleted T1.5 — Gitea token env covered by per-split-binary chart per ADR-0023")
	files := []string{
		"../../helm/loopzero-controller/templates/deployment.yaml",
		"../../helm/tenant-controller/templates/deployment.yaml",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if !strings.Contains(s, "name: GITEA_TOKEN") {
			t.Errorf("B41 regression: %s must export env GITEA_TOKEN", f)
		}
	}
}

// TestB43ControllersBaseDomain — controllers require BASE_DOMAIN env.
func TestB43ControllersBaseDomain(t *testing.T) {
	t.Skip("loopzero-controller chart deleted T1.5 — BASE_DOMAIN env covered by per-split-binary chart per ADR-0023")
	files := []string{
		"../../helm/loopzero-controller/values.yaml",
		"../../helm/tenant-controller/values.yaml",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if !strings.Contains(s, "BASE_DOMAIN:") {
			t.Errorf("B43 regression: %s missing BASE_DOMAIN env var", f)
		}
	}
}

// TestB44SmokeTestImageHasCurl — alpine/k8s bundles kubectl + curl;
// bitnamilegacy/kubectl does NOT have curl.
func TestB44SmokeTestImageHasCurl(t *testing.T) {
	data, err := os.ReadFile("../../helm/smoke-test/values.yaml")
	if err != nil {
		t.Fatalf("read smoke-test values.yaml: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "repository: bitnamilegacy/kubectl") {
		t.Errorf("B47 regression: bitnamilegacy/kubectl does not include curl (Phase 2 fails)")
	}
	if !strings.Contains(s, "repository: alpine/k8s") {
		t.Errorf("B44 regression: smoke-test must use alpine/k8s (kubectl + curl)")
	}
}

// TestB45LoopZeroCrdsRelease — helmfile must include loopzero-crds release.
func TestB45LoopZeroCrdsRelease(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/mgmt.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "- name: loopzero-crds") {
		t.Errorf("B45 regression: mgmt helmfile must declare loopzero-crds release")
	}
	if !strings.Contains(s, "../../charts/loopzero-crds") {
		t.Errorf("B45 regression: loopzero-crds release must point to charts/loopzero-crds")
	}
	// Controllers must `needs: loopzero-crds`
	if !strings.Contains(s, "- loopzero-system/loopzero-crds") {
		t.Errorf("B45 regression: controllers must declare needs:loopzero-system/loopzero-crds")
	}
}

// TestB48SmokeArgocdHttp — server.insecure: true serves HTTP.
func TestB48SmokeArgocdHttp(t *testing.T) {
	data, err := os.ReadFile("../../helm/smoke-test/values.yaml")
	if err != nil {
		t.Fatalf("read smoke-test values.yaml: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "url: https://argocd-server") {
		t.Errorf("B48 regression: smoke-test argocd URL must be http:// (server.insecure: true)")
	}
}

// TestB49LoopZeroCloudOryUrls — must point to loopzero-auth namespace.
func TestB49LoopZeroCloudOryUrls(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-cloud/values.yaml")
	if err != nil {
		t.Fatalf("read loopzero-cloud values.yaml: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "ory-hydra-admin.loopzero-system") {
		t.Errorf("B49 regression: loopzero-cloud Hydra URL points to loopzero-system (Hydra deploys to loopzero-auth)")
	}
	if !strings.Contains(s, "ory-hydra-admin.loopzero-auth.svc.cluster.local") {
		t.Errorf("B49 regression: loopzero-cloud must use ory-hydra-admin.loopzero-auth.svc.cluster.local")
	}
}

// TestB50LoopZeroCloudPort13002 — service.port must match binary :13002.
func TestB50LoopZeroCloudPort13002(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-cloud/values.yaml")
	if err != nil {
		t.Fatalf("read loopzero-cloud values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "port: 13002") {
		t.Errorf("B50 regression: loopzero-cloud service.port must be 13002 (matches binary :13002)")
	}
	tplData, err := os.ReadFile("../../helm/loopzero-cloud/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read loopzero-cloud deployment.yaml: %v", err)
	}
	if !strings.Contains(string(tplData), "path: /health") {
		t.Errorf("B50 regression: loopzero-cloud probes must use /health (not /)")
	}
}

// TestB51ControllerRolesWildcard — both ClusterRoles must have loopzero.io: ["*"].
func TestB51ControllerRolesWildcard(t *testing.T) {
	t.Skip("loopzero-controller chart deleted T1.5 — split-operator RBAC verified by infra/tests/unit/split_operator_clusterrole_grants_test.go (per I-7 of operator-split-invariants matrix)")
	files := []string{
		"../../helm/loopzero-controller/templates/rbac.yaml",
		"../../helm/tenant-controller/templates/rbac.yaml",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		// Need: apiGroups ["loopzero.io"] resources ["*"]
		if !strings.Contains(s, `apiGroups: ["loopzero.io"]`) {
			t.Errorf("B51 regression: %s missing loopzero.io apiGroup", f)
		}
		if !strings.Contains(s, `resources: ["*"]`) {
			t.Errorf("B51 regression: %s ClusterRole must have resources:[\"*\"]  for loopzero.io (binary monolith watches all 10 CRDs)", f)
		}
	}
}

// TestB51LoopZeroControllerReplicaCountZero — leader election dedup.
func TestB51LoopZeroControllerReplicaCountZero(t *testing.T) {
	t.Skip("loopzero-controller chart deleted T1.5 — replica counts for 4 split charts verified by infra/pulumi/src/__tests__/operator-split-invariants.test.ts (I-5: replicaCount: 2 minimum)")
	data, err := os.ReadFile("../../helm/loopzero-controller/values.yaml")
	if err != nil {
		t.Fatalf("read loopzero-controller values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "replicaCount: 0") {
		t.Errorf("B51 regression: loopzero-controller chart must replicaCount:0 (leader election dedup with tenant-controller)")
	}
}

// TestB53ArgocdAppsValuesObject — at least 25 Apps must have helm.valuesObject.
func TestB53ArgocdAppsValuesObject(t *testing.T) {
	entries, err := os.ReadDir("../../argocd-bootstrap/apps")
	if err != nil {
		t.Fatalf("read apps dir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile("../../argocd-bootstrap/apps/" + e.Name())
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "valuesObject:") {
			count++
		}
	}
	if count < 25 {
		t.Errorf("B53 regression: only %d Apps have helm.valuesObject (expected >= 25 — see iter 26 follow-up)", count)
	}
}

// TestB53ArgocdAppsNoAutomatedSelfHeal — until governance meeting decides
// re-enabling auto-sync, automated: block must be absent.
func TestB53ArgocdAppsNoAutomatedSelfHeal(t *testing.T) {
	entries, err := os.ReadDir("../../argocd-bootstrap/apps")
	if err != nil {
		t.Fatalf("read apps dir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile("../../argocd-bootstrap/apps/" + e.Name())
		if err != nil {
			continue
		}
		// `selfHeal: true` triggers ArgoCD to overwrite helmfile state.
		// Until T2 governance meeting decides re-enabling, this must be absent.
		if strings.Contains(string(data), "selfHeal: true") {
			t.Errorf("B53 regression: %s has selfHeal:true (governance meeting required first)", e.Name())
			count++
		}
	}
	if count > 0 {
		t.Errorf("B53 regression: %d Apps still have selfHeal:true — governance meeting required", count)
	}
}

// min returns the smaller of a, b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// end-to-end-deploy-validation-plan iter 1-4 — bugs B59-B64 discovered during
// 3-cluster local bootstrap (loopzero-mgmt + loopzero-build + loopzero-workload).

// TestB59BuildClusterPortNotConflicting — build cluster external host ports
// must not collide with mgmt cluster (mgmt uses 30093/30094).
func TestB59BuildClusterPortNotConflicting(t *testing.T) {
	data, err := os.ReadFile("../../k3d/build-cluster.yaml")
	if err != nil {
		t.Fatalf("read build-cluster.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "port: 30193:30093") {
		t.Errorf("B59 regression: build cluster must use external 30193 (mgmt already binds 30093)")
	}
	if !strings.Contains(s, "port: 30194:30094") {
		t.Errorf("B59 regression: build cluster must use external 30194 (mgmt already binds 30094)")
	}
}

// TestB60PromtailReadinessTcpProbe — promtail uses TCP probe instead of /ready.
func TestB60PromtailReadinessTcpProbe(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: per `ef7a7c16` (Loki cross-
	// cluster URL fix), promtail values are now `.gotmpl`-templated per
	// environment so the canonical file is `values.yaml.gotmpl`, not
	// `values.yaml`. See finding `concepts/operations/finding-2026-05-15-
	// infra-tests-load-regression-obsolete.md` and CLAUDE.md "Known
	// issues" Loki cross-cluster entry.
	data, err := os.ReadFile("../../helm/promtail/values.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read promtail values.yaml.gotmpl: %v", err)
	}
	s := string(data)
	idx := strings.Index(s, "readinessProbe:")
	if idx < 0 {
		t.Error("B60 regression: promtail values must override readinessProbe")
	}
	window := s[idx:min(idx+400, len(s))]
	if !strings.Contains(window, "tcpSocket:") {
		t.Errorf("B60 regression: readinessProbe must be tcpSocket")
	}
	if !strings.Contains(window, "httpGet: null") {
		t.Errorf("B60 regression: readinessProbe must explicitly null out chart httpGet")
	}
}

// TestB61CloudflaredStandaloneFallback — sentinel token + replicas:0.
func TestB61CloudflaredStandaloneFallback(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-runtime-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-runtime-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "B61: standalone fallback") {
		t.Errorf("B61 regression: generate-runtime-secrets.sh missing standalone fallback marker")
	}
	if !strings.Contains(s, "STANDALONE_LOCAL_NO_CF_TUNNEL") {
		t.Errorf("B61 regression: sentinel token absent")
	}
	rt, err := os.ReadFile("../../helmfile/runtime.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	if !strings.Contains(string(rt), "replicaCount: 0") {
		t.Errorf("B61 regression: cloudflared replicaCount:0 missing")
	}
}

// TestB62MetricsServerInstalledFalseInRuntime.
func TestB62MetricsServerInstalledFalseInRuntime(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/runtime.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	if !strings.Contains(string(data), `installed: {{ .Values | get "installMetricsServer" false }}`) {
		t.Errorf("B62 regression: runtime metrics-server release must default to installed:false")
	}
}

// TestB63KedaHttpAddonHasNeedsKeda — race condition prevention.
func TestB63KedaHttpAddonHasNeedsKeda(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/runtime.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	s := string(data)
	idx := strings.Index(s, "name: keda-http-addon")
	if idx < 0 {
		t.Fatal("B63: keda-http-addon block not found")
	}
	end := strings.Index(s[idx+10:], "- name:")
	if end < 0 {
		end = len(s) - idx - 10
	}
	block := s[idx : idx+10+end]
	if !strings.Contains(block, "- keda/keda") {
		t.Errorf("B63 regression: keda-http-addon must declare needs: keda/keda")
	}
}

// TestB64AdmissionPoliciesAlertsConditional.
func TestB64AdmissionPoliciesAlertsConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/runtime.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime helmfile: %v", err)
	}
	s := string(data)
	// Accept either direct .Values access or `get` with default (current form
	// is `(.Values | get "clusterTopology" "single-cluster")`).
	if !strings.Contains(s, `clusterTopology`) || !strings.Contains(s, `"multi-cluster"`) {
		t.Errorf("B64 regression: clusterTopology conditional missing")
	}
	idx := strings.Index(s, "name: admission-policies")
	if idx < 0 {
		t.Fatal("B64: admission-policies block not found")
	}
	end := strings.Index(s[idx+10:], "- name:")
	if end < 0 {
		end = len(s) - idx - 10
	}
	block := s[idx : idx+10+end]
	if !strings.Contains(block, "alerts:") || !strings.Contains(block, "enabled: false") {
		t.Errorf("B64 regression: admission-policies must disable alerts in standalone-multi")
	}
}

// TestStandaloneMultiEnvironmentsExist.
func TestStandaloneMultiEnvironmentsExist(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/environments/standalone-multi.yaml")
	if err != nil {
		t.Fatalf("read standalone-multi.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "clusterTopology: multi-cluster") {
		t.Error("regression: standalone-multi must set clusterTopology: multi-cluster")
	}
	if !strings.Contains(s, "host.k3d.internal") {
		t.Error("regression: standalone-multi must use host.k3d.internal for cross-cluster URLs")
	}
}

// TestK3dWorkloadConfigOneAgent — memory budget.
func TestK3dWorkloadConfigOneAgent(t *testing.T) {
	data, err := os.ReadFile("../../k3d/workload-cluster.yaml")
	if err != nil {
		t.Fatalf("read workload-cluster.yaml: %v", err)
	}
	if !strings.Contains(string(data), "agents: 1") {
		t.Error("end-to-end-deploy T0.2: workload cluster must have agents:1")
	}
}

// TestK3dBuildConfigOneAgent — memory budget.
func TestK3dBuildConfigOneAgent(t *testing.T) {
	data, err := os.ReadFile("../../k3d/build-cluster.yaml")
	if err != nil {
		t.Fatalf("read build-cluster.yaml: %v", err)
	}
	if !strings.Contains(string(data), "agents: 1") {
		t.Error("end-to-end-deploy T0.2: build cluster must have agents:1")
	}
}

// TestBootstrapClusterScript — exists + idempotent.
func TestBootstrapClusterScript(t *testing.T) {
	if _, err := os.Stat("../../scripts/bootstrap-cluster.sh"); err != nil {
		t.Errorf("end-to-end-deploy T1.1: bootstrap-cluster.sh missing: %v", err)
	}
	data, _ := os.ReadFile("../../scripts/bootstrap-cluster.sh")
	if !strings.Contains(string(data), "k3d cluster list") {
		t.Error("bootstrap-cluster.sh must check existing cluster (idempotent)")
	}
}

// end-to-end-deploy-validation-plan iter 7-8 — bugs B66, B69, B70 protections.

// TestB66TenantControllerHasGiteaUrl — GITEA_URL must be in values for
// tenant-controller (B66 fix). Without this, controller fails: "GITEA_TOKEN
// is required" / circuit breaker open / connection refused.
func TestB66TenantControllerHasGiteaUrl(t *testing.T) {
	t.Skip("tenant-controller chart deleted T1.5 — Gitea URL env now in charts/loopzero-tenant-operator/ (post operator-split)")
	for _, f := range []string{
		"../../helm/tenant-controller/values.yaml",
		"../../helm/loopzero-controller/values.yaml",
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if !strings.Contains(s, "GITEA_URL:") {
			t.Errorf("B66 regression: %s must declare GITEA_URL env var", f)
		}
	}
}

// TestB77LoopZeroApiGiteaOrgConfigurable — loopzero-api setup files must read
// orgName from cfg.GiteaOrg (env var GITEA_ORG), not hardcoded "loopzero".
// Same pattern as B69 (operators) but applied to api/cmd/setup_*.go.
func TestB77LoopZeroApiGiteaOrgConfigurable(t *testing.T) {
	files := []string{
		"../../../api/cmd/setup_workflows.go",
		"../../../api/cmd/setup_stores.go",
		"../../../api/cmd/controller/setup_services.go",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		if strings.Contains(s, `OrgName: "loopzero",`) {
			t.Errorf("B77 regression: %s has OrgName hardcoded to \"loopzero\" — must use cfg.GiteaOrg", f)
		}
		if !strings.Contains(s, "cfg.GiteaOrg") {
			t.Errorf("B77 regression: %s must reference cfg.GiteaOrg (configurable via GITEA_ORG env var)", f)
		}
	}
	// Config struct must declare GiteaOrg + read GITEA_ORG env var
	cfgData, err := os.ReadFile("../../../api/internal/config/config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	cfg := string(cfgData)
	if !strings.Contains(cfg, "GiteaOrg") {
		t.Error("B77 regression: Config struct must declare GiteaOrg field")
	}
	if !strings.Contains(cfg, `envOr("GITEA_ORG"`) {
		t.Error("B77 regression: config must read GITEA_ORG env var")
	}
}

// TestB78LoopZeroApiHasGiteaTokenEnv — loopzero-api Deployment template must map
// GITEA_TOKEN from gitea-admin Secret (same source as tenant-controller).
func TestB78LoopZeroApiHasGiteaTokenEnv(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read loopzero-api deployment.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "name: GITEA_TOKEN") {
		t.Error("B78 regression: loopzero-api Deployment must export env GITEA_TOKEN")
	}
	if !strings.Contains(s, "name: gitea-admin") {
		t.Error("B78 regression: GITEA_TOKEN must come from gitea-admin Secret")
	}
}

// TestB79LoopZeroApiHasTmpVolume — loopzero-api Deployment must mount writable /tmp
// emptyDir for gitops push (readOnlyRootFilesystem requires this).
func TestB79LoopZeroApiHasTmpVolume(t *testing.T) {
	data, err := os.ReadFile("../../helm/loopzero-api/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read loopzero-api deployment.yaml: %v", err)
	}
	s := string(data)
	// Must have a `name: tmp` volume with emptyDir
	if !strings.Contains(s, "name: tmp") {
		t.Error("B79 regression: loopzero-api Deployment must have a `tmp` volume")
	}
	if !strings.Contains(s, "emptyDir:") {
		t.Error("B79 regression: tmp volume must be emptyDir (writable)")
	}
	if !strings.Contains(s, "mountPath: /tmp") {
		t.Error("B79 regression: tmp volume must be mounted at /tmp")
	}
}

// TestB75MinioBucketsCompleteness — minio values must declare ALL buckets
// referenced by the LoopZero codebase. Source of truth: api/internal/argo/
// constants.go (s3Bucket* constants). Without loopzero-static-assets, the
// build pipeline fails at deliver-artifact step with "NoSuchBucket".
func TestB75MinioBucketsCompleteness(t *testing.T) {
	required := []string{"loopzero-builds", "loopzero-artifacts", "loopzero-logs", "loopzero-static-assets"}
	for _, f := range []string{
		"../../helm/minio/values.yaml",
		"../../helm/minio/values-distributed.yaml",
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(data)
		for _, b := range required {
			if !strings.Contains(s, "name: "+b) {
				t.Errorf("B75 regression: %s missing required bucket %q", f, b)
			}
		}
	}
	// Cross-reference: loopzero-static-assets must be in api/internal/argo/constants.go.
	apiData, err := os.ReadFile("../../../api/internal/argo/constants.go")
	if err != nil {
		t.Fatalf("read constants.go: %v", err)
	}
	if !strings.Contains(string(apiData), `"loopzero-static-assets"`) {
		t.Error("B75 regression: api code must reference loopzero-static-assets bucket name")
	}
}

// TestB71BuildHelmfileHasCWTs — build helmfile must include loopzero-build-cwts
// release that installs ClusterWorkflowTemplates (loopzero-clone, loopzero-cache-*).
// Without these, every build pipeline submission fails with "template
// reference loopzero-clone.clone-repo not found".
func TestB71BuildHelmfileHasCWTs(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/build.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read build helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "name: loopzero-build-cwts") {
		t.Error("B71 regression: build helmfile must declare loopzero-build-cwts release")
	}
	if !strings.Contains(s, "../../charts/loopzero-build-cwts") {
		t.Error("B71 regression: loopzero-build-cwts release must reference charts/loopzero-build-cwts")
	}
	// Must depend on argo-workflows (CRDs prerequisite)
	idx := strings.Index(s, "name: loopzero-build-cwts")
	end := strings.Index(s[idx:], "- name:")
	if end < 0 {
		end = len(s) - idx
	}
	block := s[idx : idx+end]
	if !strings.Contains(block, "argo/argo-workflows") {
		t.Error("B71 regression: loopzero-build-cwts must declare needs: argo/argo-workflows")
	}
}

// TestB69GiteaOrgConfigurable — operators/cmd/main.go must read GITEA_ORG
// from env (not hardcoded "loopzero"). Without env var-based config, the gitea
// org is fixed to "loopzero" which fails when (a) org doesn't exist, or
// (b) production uses a different org name.
func TestB69GiteaOrgConfigurable(t *testing.T) {
	t.Skip("monolith operators/cmd/main.go deleted T1.4 — Gitea org env covered by per-split-binary main.go (post operator-split)")
	data, err := os.ReadFile("../../../operators/cmd/main.go")
	if err != nil {
		t.Fatalf("read operators/cmd/main.go: %v", err)
	}
	s := string(data)
	// MUST NOT have hardcoded "loopzero" passed as 3rd arg to NewRealProvisioner.
	if strings.Contains(s, `gitea.NewRealProvisioner(giteaURL, giteaToken, "loopzero")`) {
		t.Error("B69 regression: orgName hardcoded as \"loopzero\" — must use os.Getenv(\"GITEA_ORG\")")
	}
	if !strings.Contains(s, `os.Getenv("GITEA_ORG")`) {
		t.Error("B69 regression: GITEA_ORG env var read missing — required for configurable org")
	}
	// Both controller charts must declare GITEA_ORG.
	for _, f := range []string{
		"../../helm/tenant-controller/values.yaml",
		"../../helm/loopzero-controller/values.yaml",
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(data), "GITEA_ORG:") {
			t.Errorf("B69 regression: %s must declare GITEA_ORG env var", f)
		}
	}
}

// TestB80GiteaCredentialsTemplate — argocd repo Secret pattern. ArgoCD reads
// the gitops repo from Gitea using HTTP basic auth; without
// `gitea-credentials-template` Secret in argocd namespace, sync returns
// "authentication required" silently and the App stays Unknown forever.
func TestB80GiteaCredentialsTemplate(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "gitea-credentials-template") {
		t.Error("B80 regression: generate-secrets.sh must create gitea-credentials-template Secret in argocd ns")
	}
	if !strings.Contains(s, "argocd.argoproj.io/secret-type: repo-creds") {
		t.Error("B80 regression: gitea-credentials-template Secret must carry repo-creds label so ArgoCD picks it up as a repository credential template")
	}
}

// TestB81ScopedClusterRoleCoversFrameworkCRDs — argocd Application sync of
// tenant manifests writes Traefik IngressRoute / Middleware and cert-manager
// Certificate CRs into the workload cluster. The scoped ServiceAccount used
// by ArgoCD's cluster Secret therefore needs create/update/patch/delete on
// those CRD apiGroups (in addition to the original core/apps set). Without
// this rule, sync stops at OutOfSync with health=Missing and a forbidden
// error in operationState.message.
func TestB81ScopedClusterRoleCoversFrameworkCRDs(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/lib/scoped-kubeconfig.sh")
	if err != nil {
		t.Fatalf("read scoped-kubeconfig.sh: %v", err)
	}
	s := string(data)
	for _, group := range []string{
		"traefik.io",
		"cert-manager.io",
		"keda.sh",
		"external-secrets.io",
	} {
		if !strings.Contains(s, group) {
			t.Errorf("B81 regression: ClusterRole must grant access to apiGroup %q", group)
		}
	}
	// Namespace lifecycle is mandatory because ArgoCD uses CreateNamespace=true
	// when the destination namespace does not yet exist.
	if !strings.Contains(s, `resources: ["namespaces"]`) {
		t.Error("B81 regression: ClusterRole must include namespaces (CreateNamespace=true requires it)")
	}
}

// TestB82StaticAssetsChartExists — static-frontend deploys generated by
// api/internal/manifests/renderer_static.go target Service
// `loopzero-system/static-assets:80`. Without a chart that provisions that
// Service, sync succeeds but the URL returns 404 (or worse, connection
// refused). Two checks: the chart exists, and runtime.helmfile registers it.
func TestB82StaticAssetsChartExists(t *testing.T) {
	for _, f := range []string{
		"../../helm/static-assets/Chart.yaml",
		"../../helm/static-assets/values.yaml",
		"../../helm/static-assets/templates/deployment.yaml",
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("B82 regression: %s missing — required for static-frontend delivery", f)
		}
	}
	// The deployment template MUST mount an emptyDir at /etc/nginx/conf.d so
	// the upstream image's docker-entrypoint envsubst can render the gateway
	// config (readOnlyRootFilesystem otherwise silently keeps the stock
	// nginx default.conf and every request returns 404).
	dep, err := os.ReadFile("../../helm/static-assets/templates/deployment.yaml")
	if err != nil {
		t.Fatalf("read static-assets deployment.yaml: %v", err)
	}
	dstr := string(dep)
	if !strings.Contains(dstr, "/etc/nginx/conf.d") {
		t.Error("B82 regression: static-assets deployment must mount writable /etc/nginx/conf.d for envsubst")
	}
	if !strings.Contains(dstr, "name: nginx-conf-d") {
		t.Error("B82 regression: static-assets deployment must declare nginx-conf-d emptyDir volume")
	}
	rt, err := os.ReadFile("../../helmfile/runtime.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read runtime.helmfile: %v", err)
	}
	rstr := string(rt)
	if !strings.Contains(rstr, "name: static-assets") {
		t.Error("B82 regression: runtime.helmfile must include static-assets release")
	}
	if !strings.Contains(rstr, "../helm/static-assets") {
		t.Error("B82 regression: runtime.helmfile must reference ../helm/static-assets chart")
	}
	// Workload cluster needs an `allow-static-assets` NetworkPolicy so Traefik
	// can reach the gateway through default-deny-ingress.
	np, err := os.ReadFile("../../helm/network-policies/templates/loopzero-system.yaml")
	if err != nil {
		t.Fatalf("read network-policies loopzero-system.yaml: %v", err)
	}
	npstr := string(np)
	if !strings.Contains(npstr, "name: allow-static-assets") {
		t.Error("B82 regression: NetworkPolicy allow-static-assets missing — Traefik traffic to gateway will be silently dropped")
	}
	if !strings.Contains(npstr, "kubernetes.io/metadata.name: traefik") {
		t.Error("B82 regression: allow-static-assets must whitelist traefik namespace as ingress source")
	}
}

// TestB82WorkloadClusterURLConfigurable — tenant-controller (operators
// monolith) reads WORKLOAD_CLUSTER_URL from env and falls back to
// kubernetes.default.svc. In standalone-multi the workload is a separate
// k3d cluster, so the env var MUST be present in the overlay or AppProject
// + Application destinations land on the wrong (mgmt) cluster.
func TestB82WorkloadClusterURLConfigurable(t *testing.T) {
	t.Skip("tenant-controller chart deleted T1.5 — workload-cluster URL now sourced from ClusterCredential CR + cellmgr (cell-aware reconcile per Phase 1 plan-2026-05-15-close-all-gaps)")
	overlay, err := os.ReadFile("../../helm/tenant-controller/values-standalone-multi.yaml")
	if err != nil {
		t.Fatalf("read tenant-controller standalone-multi overlay: %v", err)
	}
	o := string(overlay)
	if !strings.Contains(o, "WORKLOAD_CLUSTER_URL") {
		t.Error("B82 regression: tenant-controller standalone-multi overlay must set WORKLOAD_CLUSTER_URL — otherwise AppProject destination defaults to mgmt cluster and ArgoCD silently applies nothing")
	}
	// Code-level invariant: the controller must continue to read this env
	// (regression catch if someone deletes it).
	main, err := os.ReadFile("../../../operators/cmd/main.go")
	if err != nil {
		t.Fatalf("read operators/cmd/main.go: %v", err)
	}
	if !strings.Contains(string(main), `os.Getenv("WORKLOAD_CLUSTER_URL")`) {
		t.Error("B82 regression: operators/cmd/main.go must read WORKLOAD_CLUSTER_URL env")
	}
}

// TestB82ScopedKubeconfigK3dRewrite — in k3d the kubeconfig server defaults
// to https://0.0.0.0:6444 which resolves to localhost inside any pod. The
// scoped-kubeconfig.sh helper must rewrite this to the docker-bridge IP of
// the target k3d server-0 container so that ArgoCD running in mgmt can
// reach the workload cluster API. DOKS / production kubeconfigs are
// untouched (their URL is already a routable LB hostname).
func TestB82ScopedKubeconfigK3dRewrite(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/lib/scoped-kubeconfig.sh")
	if err != nil {
		t.Fatalf("read scoped-kubeconfig.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `0\.0\.0\.0`) {
		t.Error("B82 regression: scoped-kubeconfig.sh must detect 0.0.0.0/127.0.0.1 server URLs (k3d local) and rewrite")
	}
	if !strings.Contains(s, `k3d-loopzero-${CLUSTER_ID}-server-0`) {
		t.Error("B82 regression: scoped-kubeconfig.sh must derive the docker-bridge IP from the k3d server-0 container")
	}
	if !strings.Contains(s, "docker inspect") {
		t.Error("B82 regression: scoped-kubeconfig.sh must docker inspect to resolve the bridge IP")
	}
}

// =============================================================================
// CHG-2026-04-29-DOGFOOD — F-01..F-13 regression tests (dogfood remediation)
// =============================================================================

// TestF01LoopZeroApiNodePortInStandaloneMulti — em standalone-multi o overlay
// values-standalone-multi.yaml DEVE setar service.type=NodePort com
// nodePort=30001 (mapeado por k3d-mgmt para host:13001) e ingress.localhost=
// true. Sem isso, `loopzero whoami`/`loopzero deploy` retornam SERVICE_UNAVAILABLE
// porque nada ocupa NodePort 30001 no mgmt cluster.
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

// TestF01LoopZeroApiHasLocalhostIngressRoute — quando ingress.localhost=true, o
// template renderiza Route com Host(`localhost`) || Host(`127.0.0.1`) ||
// Host(`api.loopzero.local`) em entry point `web` (HTTP, sem TLS para localhost).
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

// TestF01DefaultProfileKeepsClusterIP — backward compat: dev/prod profiles
// (sem overlay standalone-multi) DEVEM manter Service ClusterIP + 1
// IngressRoute apenas (a de api.{domain}). Sem isso, mudanças no overlay
// vazam para production.
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

// TestF03CommanderExitOverrideRegistered — index.ts DEVE registrar
// configureOutput.writeErr que seta process.exitCode=1 e program.exitOverride
// que captura codes de parse error. Sem isso, `loopzero nonexistent-command` ou
// `loopzero deploy --bogus-flag` retornam exit 0 (Rule 4 violation).
func TestF03CommanderExitOverrideRegistered(t *testing.T) {
	data, err := os.ReadFile("../../../cli/src/index.ts")
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

// TestF05InitPreflightAuthDep — InitDeps DEVE expor preflightAuth opcional;
// quando não-offline e preflight falha, init retorna exit 4/5 ANTES de
// escrever loopzero.yaml. Anti-pattern silent-success (Rule 8) eliminado.
func TestF05InitPreflightAuthDep(t *testing.T) {
	data, err := os.ReadFile("../../../cli/src/commands/init.ts")
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

// TestF06LogoutHasTokenDep — LogoutDeps DEVE expor hasToken para distinguir
// "cancelled" de "no session". Sem isso, `loopzero logout` sem auth fica
// confuso ("Logout cancelled." sem ter havido prompt).
func TestF06LogoutHasTokenDep(t *testing.T) {
	data, err := os.ReadFile("../../../cli/src/commands/logout.ts")
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

// TestF07VersionEndpointRegistered — router.go DEVE registrar GET
// /api/v1/version sem middleware de auth (mesmo pattern que /health).
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

// TestF10TracingDisabledInStandaloneMulti — em standalone-multi o
// otel-collector chart NÃO é instalado pelo helmfile mgmt. O default do
// operator (`otel-collector.loopzero-system.svc.cluster.local:4317`) gera
// "rpc error: code = Unavailable" em loop. Overlay deve setar
// LOOPZERO_TRACING_ENABLED=false. Em dev/prod o env não é setado → tracing
// ativo via DefaultEndpoint (backward compat).
func TestF10TracingDisabledInStandaloneMulti(t *testing.T) {
	t.Skip("tenant-controller chart deleted T1.5 — tracing config now in charts/loopzero-tenant-operator/values.yaml (post operator-split)")
	data, err := os.ReadFile("../../helm/tenant-controller/values-standalone-multi.yaml")
	if err != nil {
		t.Fatalf("read tenant-controller standalone-multi overlay: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "LOOPZERO_TRACING_ENABLED") {
		t.Error("F-10 regression: standalone-multi overlay must set LOOPZERO_TRACING_ENABLED env")
	}
	if !strings.Contains(s, `LOOPZERO_TRACING_ENABLED: "false"`) {
		t.Error("F-10 regression: LOOPZERO_TRACING_ENABLED must be \"false\" in standalone-multi (no otel-collector chart)")
	}
	// Default values.yaml NÃO seta LOOPZERO_TRACING_ENABLED — preserve dev/prod.
	defaults, err := os.ReadFile("../../helm/tenant-controller/values.yaml")
	if err != nil {
		t.Fatalf("read tenant-controller defaults: %v", err)
	}
	if strings.Contains(string(defaults), "LOOPZERO_TRACING_ENABLED") {
		t.Error("F-10 regression: default values.yaml must NOT set LOOPZERO_TRACING_ENABLED (only standalone-multi opts out)")
	}
}

// TestF08LoopZeroApiServiceMonitorOptIn — chart DEVE renderizar metrics port
// no Service + Deployment + ServiceMonitor opt-in via toggle. Em
// standalone-multi sem prometheus-operator, ServiceMonitor não renderiza
// (toggle=false default) — evita erro de CRD missing em kubeconform.
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

// TestF11ArgocdLuaHealthChecksDefined — argocd values.yaml DEVE declarar
// Lua health checks para Traefik IngressRoute / Middleware / TLSOption +
// cert-manager Certificate. Sem isso, ArgoCD reporta health="" para esses
// CRDs e Apps ficam stuck em Progressing infinito.
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

// TestF12LoopZeroApiTokensSecretGenerated — generate-secrets.sh DEVE criar
// loopzero-api-tokens Secret com argocd-token JWT. Sem isso, /api/v1/health/deep
// reporta argocd:auth_failed degraded em standalone-multi.
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

// TestF04MigrationScriptExists — Tenant SoT fix (T1 partial of F-04).
// Migration script reconcilia Tenant/Environment CRs duplicados entre
// namespaces para o canônico loopzero-system. T3.1 (hard-code do namespace
// no código operator/API) e T3.2 (ValidatingAdmissionPolicy) ficam T2
// governance per CLAUDE.md Pre-release rule (tenant isolation contract).
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

// =============================================================================
// ADR-0018 v2 (MEET-ADR-0018) — Tenant SoT regression tests.
// Conditions C1-C16 enforced as static code/config invariants where possible.
// =============================================================================

// TestADR0018C1_NoEnvVarOverride — C1 enforcement at static analysis time.
// `tenant_cr_manager.go` MUST declare a Go `const` (not `var`) and MUST NOT
// read TENANT_NAMESPACE env. Without this, future PR could re-introduce the
// iter 7-8 misplacement vector.
func TestADR0018C1_NoEnvVarOverride(t *testing.T) {
	data, err := os.ReadFile("../../../api/internal/k8s/tenant_cr_manager.go")
	if err != nil {
		t.Fatalf("read tenant_cr_manager.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `const tenantNamespace = "loopzero-system"`) {
		t.Error("ADR-0018 C1 regression: tenant_cr_manager.go must declare `const tenantNamespace = \"loopzero-system\"`")
	}
	if strings.Contains(s, `os.Getenv("TENANT_NAMESPACE")`) {
		t.Error("ADR-0018 C1 regression: tenant_cr_manager.go must NOT read TENANT_NAMESPACE env")
	}
}

// TestADR0018C9_OperatorCacheScope — C9 enforcement: operator manager
// declares cache.Options.ByObject restricting Tenant/Environment/
// PlatformBootstrap/GitRepositoryBinding/ClusterCredential to loopzero-system.
// LoopZeroApplication/PreviewEnvironment/ExposedRoute remain cluster-wide.
func TestADR0018C9_OperatorCacheScope(t *testing.T) {
	t.Skip("monolith operators/cmd/main.go deleted T1.4 — ADR-0018 C9 cache scoping now per-binary (verified by sibling TestCacheScoping_* tests at the binary level)")
	data, err := os.ReadFile("../../../operators/cmd/main.go")
	if err != nil {
		t.Fatalf("read operators/cmd/main.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "cache.Options{") {
		t.Error("ADR-0018 C9 regression: manager must use cache.Options for per-Kind namespace scoping")
	}
	if !strings.Contains(s, "platformCRDNamespaces") {
		t.Error("ADR-0018 C9 regression: platformCRDNamespaces map missing — required for restricted cache")
	}
	for _, crd := range []string{"loopzerov1.Tenant{}", "loopzerov1.Environment{}"} {
		if !strings.Contains(s, crd) {
			t.Errorf("ADR-0018 C9 regression: cache.ByObject must include %q with platformCRDNamespaces", crd)
		}
	}
	// LoopZeroApplication MUST stay cluster-wide — assert it's NOT in the
	// restricted ByObject map. Heuristic: search for the comment that
	// documents the exclusion.
	if !strings.Contains(s, "intentionally\n\t\t\t\t// NOT listed") &&
		!strings.Contains(s, "intentionally") {
		t.Error("ADR-0018 C9 regression: LoopZeroApplication/PreviewEnvironment must remain cluster-wide; document the exclusion explicitly")
	}
}

// TestADR0018C2C5_StartupMisplacementScan — C2 + C5 enforcement: operator
// runs runMisplacedTenantScan at startup and emits loopzero_misplaced_tenant_total.
func TestADR0018C2C5_StartupMisplacementScan(t *testing.T) {
	t.Skip("monolith operators/cmd/main.go deleted T1.4 — ADR-0018 C2/C5 startup scan now per-binary (each split binary owns its CR types' scan)")
	mainData, err := os.ReadFile("../../../operators/cmd/main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(mainData), "runMisplacedTenantScan") {
		t.Error("ADR-0018 C2 regression: main.go must invoke runMisplacedTenantScan at startup")
	}
	if !strings.Contains(string(mainData), "func runMisplacedTenantScan") {
		t.Error("ADR-0018 C2 regression: runMisplacedTenantScan function must be defined")
	}
	metricsData, err := os.ReadFile("../../../operators/internal/metrics/metrics.go")
	if err != nil {
		t.Fatalf("read metrics.go: %v", err)
	}
	if !strings.Contains(string(metricsData), "MisplacedTenantTotal") {
		t.Error("ADR-0018 C5 regression: metrics package must export MisplacedTenantTotal")
	}
	if !strings.Contains(string(metricsData), "loopzero_misplaced_tenant_total") {
		t.Error("ADR-0018 C5 regression: metric name must be loopzero_misplaced_tenant_total")
	}
}

// TestADR0018T32_VAPTemplateExists — T3.2 chart artifact + opt-in toggle.
// VAP template MUST exclude status subresource per C6 to avoid blocking
// operator status writes (self-inflicted DoS).
func TestADR0018T32_VAPTemplateExists(t *testing.T) {
	const path = "../../helm/admission-policies/templates/vap-tenant-namespace.yaml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ADR-0018 T3.2 regression: VAP template missing: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "{{- if .Values.vaps.tenantNamespace }}") {
		t.Error("ADR-0018 T3.2 regression: VAP must be opt-in via .Values.vaps.tenantNamespace")
	}
	// C6 — status subresource exclusion via matchConditions.
	if !strings.Contains(s, "matchConditions:") {
		t.Error("ADR-0018 C6 regression: VAP must declare matchConditions to exclude subresources")
	}
	if !strings.Contains(s, `request.subResource == ""`) {
		t.Error("ADR-0018 C6 regression: matchConditions must skip when subResource != \"\" (status subresource bypass)")
	}
	// Validation expression check loopzero-system namespace.
	if !strings.Contains(s, `object.metadata.namespace == "loopzero-system"`) {
		t.Error("ADR-0018 T3.2 regression: validation expression must enforce namespace=loopzero-system")
	}
	// Default values.yaml: opt-out.
	values, err := os.ReadFile("../../helm/admission-policies/values.yaml")
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	if !strings.Contains(string(values), "tenantNamespace: false") {
		t.Error("ADR-0018 C4 regression: vaps.tenantNamespace default must be `false` (mandatory deployment order — VAP last)")
	}
}

// TestADR0018C7_RolloutRunbookExists — C7 enforcement: runbook with
// mandatory deployment order T3.3 → T3.1 → T3.2 + abort criteria + rollback.
func TestADR0018C7_RolloutRunbookExists(t *testing.T) {
	t.Skip("runbook files removed — docs/runbooks/ deleted in governance consolidation 2026-05-11")
}

// TestT12ExitCodeContractTestExists — T1.2 (CHG-2026-04-29-DOGFOOD).
// Garante que o fork-based exit code contract test exists e cobre matrix
// mínima de comandos (auth-gated, validate, parse errors, help/version).
// Sem este test, regressão em F-02 (exit code drift) volta silenciosa.
func TestT12ExitCodeContractTestExists(t *testing.T) {
	const path = "../../../cli/tests/integration/exit-code-contract.test.ts"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("T1.2 regression: contract test missing: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract test: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "spawnSync") {
		t.Error("T1.2 regression: contract test must use child_process.spawnSync to invoke real process")
	}
	for _, cmd := range []string{"whoami", "deploy", "validate", "build", "secret", "rollback"} {
		if !strings.Contains(s, "'"+cmd+"'") && !strings.Contains(s, `"`+cmd+`"`) {
			t.Errorf("T1.2 regression: contract matrix must cover %q command", cmd)
		}
	}
	// help/version SÃO exit 0, não erros — contract deve testar isso.
	if !strings.Contains(s, "--help") || !strings.Contains(s, "--version") {
		t.Error("T1.2 regression: contract test must cover --help / --version (exit 0 expected)")
	}
}

// TestT61DogfoodLoopH1TestExists — T6.1 (CHG-2026-04-29-DOGFOOD).
// Gate de release: dogfood loop H1 valida jornada user-real
// (login → init → deploy → status → delete) gated em LOOPZERO_E2E_DOGFOOD=1.
func TestT61DogfoodLoopH1TestExists(t *testing.T) {
	const path = "../../../infra/tests/e2e/dogfood_loop_h1_test.go"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("T6.1 regression: dogfood loop H1 test missing: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dogfood test: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "LOOPZERO_E2E_DOGFOOD") {
		t.Error("T6.1 regression: gating env var must be LOOPZERO_E2E_DOGFOOD")
	}
	if !strings.Contains(s, "func TestDogfoodLoopH1(") {
		t.Error("T6.1 regression: TestDogfoodLoopH1 function must exist")
	}
	// Cleanup: defer project delete é obrigatório (Prova de Morte).
	if !strings.Contains(s, `"project", "delete"`) {
		t.Error("T6.1 regression: test must defer project delete for cleanup (Prova de Morte)")
	}
	// Rule 3 enforcement scan.
	if !strings.Contains(s, "ArgoCD") || !strings.Contains(s, "GitOps") {
		t.Error("T6.1 regression: test must scan output for forbidden internal terms (Rule 3)")
	}
}

// TestF13WorkflowHasPodGC — F-13 fix: workflow_builder.go DEVE setar
// PodGC.Strategy=OnPodCompletion com DeleteDelayDuration=5m. Sem isso,
// pods de step com Failed acumulam na ns argo até GC do Workflow (24h).
func TestF13WorkflowHasPodGC(t *testing.T) {
	data, err := os.ReadFile("../../../api/internal/argo/workflow_builder.go")
	if err != nil {
		t.Fatalf("read workflow_builder.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "PodGC: &forge.PodGC{") {
		t.Error("F-13 regression: Workflow spec must include PodGC field")
	}
	if !strings.Contains(s, `Strategy:            "OnPodCompletion"`) &&
		!strings.Contains(s, `Strategy: "OnPodCompletion"`) {
		t.Error("F-13 regression: PodGC.Strategy must be OnPodCompletion (deletes Failed pods, not just Success)")
	}
	if !strings.Contains(s, `DeleteDelayDuration: "5m"`) {
		t.Error("F-13 regression: PodGC.DeleteDelayDuration must be 5m (debug window before delete)")
	}
}

// TestF09EnsurePullSecretHandlesNotFound — operator DEVE distinguir
// errors.IsNotFound (esperado em standalone-multi sem registry privado)
// de outras falhas. Sem isso, log "pull secret ensure failed" em loop.
func TestF09EnsurePullSecretHandlesNotFound(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: operator-split T1.4 moved this
	// file from `operators/internal/controller/` to
	// `operators/internal/tenant/` (tenant-operator binary owns the
	// EnvironmentReconciler). See finding `concepts/operations/
	// finding-2026-05-15-infra-tests-load-regression-obsolete.md`.
	data, err := os.ReadFile("../../../operators/internal/tenant/environment_rbac.go")
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

// TestCleanIncrementalScript — exists + preserves cluster state.
func TestCleanIncrementalScript(t *testing.T) {
	if _, err := os.Stat("../../scripts/local/clean-incremental.sh"); err != nil {
		t.Errorf("end-to-end-deploy T0.1: clean-incremental.sh missing: %v", err)
	}
	data, _ := os.ReadFile("../../scripts/local/clean-incremental.sh")
	s := string(data)
	clusterDeleteString := "cluster" + " " + "delete"
	if strings.Contains(s, "k3d "+clusterDeleteString) {
		t.Error("clean-incremental.sh must NOT delete clusters")
	}
	if !strings.Contains(s, "status.phase=Succeeded") {
		t.Error("clean-incremental.sh must clean Succeeded pods")
	}
}
