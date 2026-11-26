// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package config — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPRUNTIME_REGRESSION=1 go test ./infra/tests/config/ -run TestRegression -v
package config

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
func TestB18RuntimeMaxNodesConfig(t *testing.T) {
	data, err := os.ReadFile("../../pulumi/Pulumi.dev.yaml")
	if err != nil {
		t.Fatalf("read Pulumi.dev.yaml: %v", err)
	}
	if !strings.Contains(string(data), "runtimeMaxNodes: \"8\"") {
		t.Errorf("B18 regression: runtimeMaxNodes is not 8 in Pulumi.dev.yaml")
	}
}

func TestB16ClusterImagePolicyChartExists(t *testing.T) {
	chartFile := "../../../charts/loopruntime-admission-policies/Chart.yaml"
	if _, err := os.Stat(chartFile); err != nil {
		t.Fatalf("B16 regression: %s missing — was T0.1 reverted? %v", chartFile, err)
	}
	tplFile := "../../../charts/loopruntime-admission-policies/templates/cluster-image-policy.yaml"
	if _, err := os.Stat(tplFile); err != nil {
		t.Fatalf("B16 regression: ClusterImagePolicy template missing: %v", err)
	}
}

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

func TestB21PostgresChartVersion18(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/core.helmfile.yaml.gotmpl")
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

func TestB22KratosNeedsPostgresConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/core.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "{{- if .Values.inClusterPostgres }}") {
		t.Errorf("B22 regression: conditional `needs:` block missing for inClusterPostgres")
	}
	if !strings.Contains(s, "- loopruntime-system/postgresql") {
		t.Errorf("B22 regression: kratos/gitea must declare needs:postgresql")
	}
}

func TestB23ServiceMonitorDisabled(t *testing.T) {
	files := []string{
		"../../helm/postgresql/values.yaml",
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

func TestB25MinioDistributedConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/core.helmfile.yaml.gotmpl")
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
	if !strings.Contains(context, `{{- if .Values.profile.highAvailability }}`) {
		t.Errorf("B25 regression: minio values-distributed.yaml must be gated by profile.highAvailability")
	}
	assertHighAvailabilityOnlyInCloudProfiles(t, "B25")
}

func TestB26MetricsServerInstalledFalse(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/core.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `installed: {{ .Values | get "installMetricsServer" false }}`) {
		t.Errorf("B26 regression: metrics-server release must be installed:false by default")
	}
}

func TestB27PostgresInitdbAllDBs(t *testing.T) {
	data, err := os.ReadFile("../../helm/postgresql/values.yaml")
	if err != nil {
		t.Fatalf("read postgresql values.yaml: %v", err)
	}
	s := string(data)
	required := []string{"CREATE DATABASE gitea", "CREATE DATABASE kratos", "CREATE DATABASE hydra"}
	for _, r := range required {
		if !strings.Contains(s, r) {
			t.Errorf("B27 regression: postgresql initdb missing `%s`", r)
		}
	}
}

func TestB28GiteaHaConditional(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/core.helmfile.yaml.gotmpl")
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
	if !strings.Contains(block, `{{- if .Values.profile.highAvailability }}`) {
		t.Errorf("B28 regression: gitea values-ha.yaml must be gated by profile.highAvailability")
	}
	assertHighAvailabilityOnlyInCloudProfiles(t, "B28")
}

// assertHighAvailabilityOnlyInCloudProfiles checks the other half of the B25/B28 gate: the
// helmfiles read `profile.highAvailability`, and only the LoopRuntime Cloud environments (dev, prod)
// set it. Self-hosting and local profiles run single-replica on commodity hardware.
func assertHighAvailabilityOnlyInCloudProfiles(t *testing.T, bug string) {
	t.Helper()
	for _, env := range []string{"dev", "prod", "standalone", "standalone-multi", "standalone-public", "dev-public", "drill"} {
		raw, err := os.ReadFile("../../helmfile/environments/" + env + ".yaml")
		if err != nil {
			t.Fatalf("%s: read %s env: %v", bug, env, err)
		}
		var doc struct {
			Profile *struct {
				HighAvailability *bool `yaml:"highAvailability"`
			} `yaml:"profile"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: parse %s env: %v", bug, env, err)
		}
		if doc.Profile == nil || doc.Profile.HighAvailability == nil {
			t.Errorf("%s: environments/%s.yaml does not declare profile.highAvailability", bug, env)
			continue
		}
		want := env == "dev" || env == "prod"
		if *doc.Profile.HighAvailability != want {
			t.Errorf("%s regression: environments/%s.yaml sets profile.highAvailability=%v, want %v (HA is a LoopRuntime Cloud posture)",
				bug, env, *doc.Profile.HighAvailability, want)
		}
	}
}

func TestB29RedisServiceHostname(t *testing.T) {
	// F25 (M23 audit 2026-07-10): assertion was INVERTED — the local
	// `infra/helm/gitea/templates/redis.yaml` template is DEAD (release uses
	// remote gitea-charts/gitea chart per finding-2026-05-22-pg-password-
	// divergence § gitea-redis; pointing at bare `gitea-redis` crashed with
	// "lookup gitea-redis: no such host"). Canonical is `gitea-redis-master`
	// from Bitnami primary Service (prod parity — same release runs dev+prod).
	data, err := os.ReadFile("../../helm/gitea/values.yaml")
	if err != nil {
		t.Fatalf("read gitea values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "addr=gitea-redis-master:6379") {
		t.Errorf("B29 regression: gitea must use 'addr=gitea-redis-master:6379' (Bitnami primary Service; local redis.yaml template is DEAD)")
	}
	if strings.Contains(s, `addr=gitea-redis:6379"`) {
		t.Errorf("B29 regression: gitea must NOT use bare 'addr=gitea-redis:6379' (documented crash: lookup gitea-redis: no such host)")
	}
}

func TestB31ImageTagsPinned(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: `infra/helm/loopruntime-controller/`
	// and `infra/helm/tenant-controller/` were DELETED in T1.5 (commit
	// 27cfe6cd, ADR-0023 operator-split). The same tag-pinning invariant
	// is now enforced for the 4 split-binary charts via the OCI-published
	// `charts/loopruntime-*-operator/values.yaml` files. Re-targeted to loopruntime-api
	// only; split-operator tag-pinning is covered by the Helm CI per chart.
	// The loopruntime-api bootstrap defaults moved from infra/helm/loopruntime-api/values.yaml
	// to the values Helmfile installs the one chart with.
	files := []string{
		"../../../charts/loopruntime-api/values-bootstrap.yaml",
	}
	// The shape rules used to live here as four whole-file regexes. They moved
	// into `imageTagVerdict`, which judges the tag VALUE and states its reason —
	// see image_tag_policy_test.go for why the contract also changed (`latest`
	// is now refused, the `SET-BY-CI` sentinel accepted, per issue #232).
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var doc struct {
			Image struct {
				Tag string `yaml:"tag"`
			} `yaml:"image"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		if ok, reason := imageTagVerdict(doc.Image.Tag); !ok {
			t.Errorf("B31 regression: %s declares image.tag %q — %s", f, doc.Image.Tag, reason)
		}
	}
}

func TestB33B36GenerateSecretsStandaloneFallback(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "B33: standalone fallback") {
		t.Errorf("B33 regression: generate-secrets.sh missing standalone fallback marker for loopruntime-db-credentials")
	}
	// F26 (M23 audit 2026-07-10): B36 log-string canonicalized to
	// "ensuring loopruntime-cluster-kubeconfigs (per-slot idempotent)"; behavior
	// preserved. Assert the canonical form.
	if !strings.Contains(s, "B36: standalone — ensuring loopruntime-cluster-kubeconfigs") {
		t.Errorf("B36 regression: generate-secrets.sh missing canonical loopruntime-cluster-kubeconfigs ensure step")
	}
	if !strings.Contains(s, "loopruntime-cluster-kubeconfigs") {
		t.Errorf("B36 regression: generate-secrets.sh must create loopruntime-cluster-kubeconfigs Secret")
	}
}

func TestB35DatabaseUrlMapping(t *testing.T) {
	// Path drift fix 2026-05-15 iter 5: `loopruntime-controller` + `tenant-
	// controller` charts DELETED in T1.5 (commit 27cfe6cd). The 4 split
	// operators do NOT receive DATABASE_URL directly (they use the
	// API's data-access layer); only loopruntime-api and loopruntime-cloud carry
	// the env. See finding `concepts/operations/finding-2026-05-15-
	// infra-tests-load-regression-obsolete.md`.
	//
	// B35 was DATABASE_URL pointing at nothing. The invariant is that it maps to a
	// key the setup actually writes into loopruntime-db-credentials — not a fixed key name:
	// loopruntime-api reads `api-uri` since the role split (#396) and loopruntime-cloud reads `uri`.
	written := dbCredentialKeysWritten(t)
	for _, f := range []string{
		"../../../charts/loopruntime-api/values-bootstrap.yaml",
		"../../../charts/loopruntime-cloud/values-bootstrap.yaml",
	} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var values struct {
			Secrets struct {
				KeyRefs []struct {
					Name       string `yaml:"name"`
					SecretName string `yaml:"secretName"`
					Key        string `yaml:"key"`
				} `yaml:"keyRefs"`
			} `yaml:"secrets"`
		}
		if err := yaml.Unmarshal(data, &values); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		found := false
		for _, ref := range values.Secrets.KeyRefs {
			if ref.Name != "DATABASE_URL" {
				continue
			}
			found = true
			if ref.SecretName != "loopruntime-db-credentials" {
				t.Errorf("B35 regression: %s maps DATABASE_URL from %q, want loopruntime-db-credentials", f, ref.SecretName)
			}
			if !written[ref.Key] {
				t.Errorf("B35 regression: %s maps DATABASE_URL to key %q, which generate-secrets.sh never writes (writes %v)", f, ref.Key, written)
			}
		}
		if !found {
			t.Errorf("B35 regression: %s must export env DATABASE_URL", f)
		}
	}
}

// dbCredentialKeysWritten returns the keys generate-secrets.sh writes into
// loopruntime-db-credentials, read from its --from-literal flags.
func dbCredentialKeysWritten(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../../infra/helmfile/scripts/generate-secrets.sh")
	if err != nil {
		t.Fatalf("read generate-secrets.sh: %v", err)
	}
	s := string(raw)
	from := strings.Index(s, "DB_ROLE_LITERALS=(")
	to := strings.Index(s, `"${DB_ROLE_LITERALS[@]}"`)
	if from < 0 || to < from {
		t.Fatal("generate-secrets.sh no longer builds loopruntime-db-credentials the way this test reads it")
	}
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile(`--from-literal=([a-z-]+)=`).FindAllStringSubmatch(s[from:to], -1) {
		keys[m[1]] = true
	}
	return keys
}

func TestB40LoopRuntimeApiServicePort3001(t *testing.T) {
	data, err := os.ReadFile("../../../charts/loopruntime-api/values.yaml")
	if err != nil {
		t.Fatalf("read loopruntime-api values.yaml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "port: 3001") {
		t.Errorf("B40 regression: loopruntime-api service.port must be 3001 (matches binary listen)")
	}
}

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

func TestB45LoopRuntimeCrdsRelease(t *testing.T) {
	data, err := os.ReadFile("../../helmfile/core.helmfile.yaml.gotmpl")
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "- name: loopruntime-crds") {
		t.Errorf("B45 regression: mgmt helmfile must declare loopruntime-crds release")
	}
	if !strings.Contains(s, "../../charts/loopruntime-crds") {
		t.Errorf("B45 regression: loopruntime-crds release must point to charts/loopruntime-crds")
	}
	// Controllers must `needs: loopruntime-crds`
	if !strings.Contains(s, "- loopruntime-system/loopruntime-crds") {
		t.Errorf("B45 regression: controllers must declare needs:loopruntime-system/loopruntime-crds")
	}
}

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

func TestB49LoopRuntimeCloudOryUrls(t *testing.T) {
	data, err := os.ReadFile("../../../charts/loopruntime-cloud/values-bootstrap.yaml")
	if err != nil {
		t.Fatalf("read loopruntime-cloud values.yaml: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "ory-hydra-admin.loopruntime-system") {
		t.Errorf("B49 regression: loopruntime-cloud Hydra URL points to loopruntime-system (Hydra deploys to loopruntime-auth)")
	}
	if !strings.Contains(s, "ory-hydra-admin.loopruntime-auth.svc.cluster.local") {
		t.Errorf("B49 regression: loopruntime-cloud must use ory-hydra-admin.loopruntime-auth.svc.cluster.local")
	}
}
