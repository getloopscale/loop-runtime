// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/argo-workflows-go"
)

// Sprint 6 / T1.1 — SBOM scan template tests.

// TestSyftScanTemplate_DeclaresOutputs ensures the syft step exposes the
// three SBOM metadata outputs that build_reconcile_complete (T3.1) reads.
func TestSyftScanTemplate_DeclaresOutputs(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})

	if len(c.Outputs) < 3 {
		t.Fatalf("expected at least 3 outputs (s3 key, format, count), got %d", len(c.Outputs))
	}
	want := map[string]bool{"sbom_s3_key": false, "sbom_format": false, "sbom_component_count": false}
	for _, p := range c.Outputs {
		if _, ok := want[p.Name]; ok {
			want[p.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing required output parameter %q", name)
		}
	}
}

// TestSyftScanTemplate_OutputsOnLocalTmp — outputs must be written to
// the container's OWN /tmp filesystem (not the shared /workspace) so
// other containers in the pod cannot tamper with them. Mirrors the
// trust model of cosignSignTemplate.
func TestSyftScanTemplate_OutputsOnLocalTmp(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})

	for _, p := range c.Outputs {
		if p.ValueFrom == nil || p.ValueFrom.Path == "" {
			t.Errorf("output %q has no ValueFrom.Path", p.Name)
			continue
		}
		path := p.ValueFrom.Path
		if strings.HasPrefix(path, workspaceMountPath) || strings.HasPrefix(path, "/workspace") {
			t.Errorf("output %q path %q must not be on shared /workspace", p.Name, path)
		}
		if !strings.HasPrefix(path, "/tmp/") {
			t.Errorf("output %q path %q must be under /tmp/", p.Name, path)
		}
	}
}

// TestSyftScanTemplate_Insecure_EmitsFlag — when RegistryInsecure=true,
// syft must accept HTTP / self-signed certs against local k3d registry.
//
// Plan T6.1 dogfood unblock: syft v1.x removed `--source-registry-insecure`
// CLI flag. Now must use SYFT_REGISTRY_INSECURE_USE_HTTP env var.
func TestSyftScanTemplate_Insecure_EmitsFlag(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"k3d-loopruntime-registry:5000/tn-1/proj-1/api:abc", true, "", objectStore{})
	script := c.Source
	if !strings.Contains(script, "SYFT_REGISTRY_INSECURE_USE_HTTP=true") {
		t.Errorf("insecure registry MUST export SYFT_REGISTRY_INSECURE_USE_HTTP=true:\n%s", script)
	}
	if !strings.Contains(script, "SYFT_REGISTRY_INSECURE_SKIP_TLS_VERIFY=true") {
		t.Errorf("insecure registry MUST export SYFT_REGISTRY_INSECURE_SKIP_TLS_VERIFY=true:\n%s", script)
	}
	// Old flag must NOT appear (regression guard).
	if strings.Contains(script, "--source-registry-insecure") {
		t.Errorf("OBSOLETE flag --source-registry-insecure must not appear (syft v1.x removed it):\n%s", script)
	}
}

func TestSyftScanTemplate_Secure_NoInsecureFlag(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.digitalocean.com/loopruntime/api:abc", false, "", objectStore{})
	script := c.Source
	if strings.Contains(script, "--source-registry-insecure") {
		t.Errorf("secure registry must NOT emit --source-registry-insecure:\n%s", script)
	}
}

// TestSyftScanTemplate_SecurityContext_NonRoot — UID 1000 + RunAsNonRoot.
func TestSyftScanTemplate_SecurityContext_NonRoot(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	if c.SecurityContext == nil {
		t.Fatal("SecurityContext must not be nil")
	}
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != rootlessUID {
		t.Errorf("RunAsUser must be %d", rootlessUID)
	}
	if c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot {
		t.Error("RunAsNonRoot must be true")
	}
}

// TestSyftScanTemplate_UploadsToS3 — script must contain aws s3 cp using
// the canonical key sbom/{tenant}/{project}/{commit}/{app}.cdx.json.
func TestSyftScanTemplate_UploadsToS3(t *testing.T) {
	c := syftScanTemplate("tn-acme", "proj-shop", "deadbeef", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	script := c.Source
	if !strings.Contains(script, "aws s3 cp") {
		t.Errorf("script must contain aws s3 cp:\n%s", script)
	}
	wantKey := "sbom/tn-acme/proj-shop/deadbeef/api.cdx.json"
	if !strings.Contains(script, wantKey) {
		t.Errorf("script must reference S3 key %q:\n%s", wantKey, script)
	}
}

// TestSyftScanTemplate_UsesPreBuiltImage — SEC-006 requires the syft
// binary to come from the pre-built loopruntime-syft-scanner image, not a
// runtime download.
func TestSyftScanTemplate_UsesPreBuiltImage(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	if c.Image != imageSyftScanner {
		t.Errorf("expected image %q, got %q", imageSyftScanner, c.Image)
	}
}

// TestSyftScanTemplate_ReadsDigestFromMetadata — script must read the
// digest from the BuildKit metadata file (same source cosign uses).
// This pins the SBOM to the cryptographically signed bytes.
func TestSyftScanTemplate_ReadsDigestFromMetadata(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	script := c.Source
	if !strings.Contains(script, "metadata-api.json") {
		t.Errorf("script must read BuildKit metadata-api.json:\n%s", script)
	}
	if !strings.Contains(script, "@${DIGEST}") {
		t.Errorf("script must scan by digest, not tag:\n%s", script)
	}
}

// TestSyftScanTemplate_OutputFormatIsCycloneDX — D1 default format.
func TestSyftScanTemplate_OutputFormatIsCycloneDX(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	script := c.Source
	if !strings.Contains(script, "cyclonedx-json=") {
		t.Errorf("default SBOM format must be cyclonedx-json:\n%s", script)
	}
}

// TestSyftScanTemplate_HasMinIOEnv — aws-cli needs credentials wired
// from the K8s Secret (SecretEnv binding).
func TestSyftScanTemplate_HasMinIOEnv(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	want := map[string]bool{
		"AWS_ACCESS_KEY_ID":     false,
		"AWS_SECRET_ACCESS_KEY": false,
	}
	for _, e := range c.Env {
		if se, ok := e.(forge.SecretEnv); ok {
			if _, exists := want[se.Name]; exists {
				want[se.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing required env %q (must be wired via SecretEnv)", name)
		}
	}
}

// TestSyftScanTemplate_RegistryConfigReadOnly — defense in depth.
func TestSyftScanTemplate_RegistryConfigReadOnly(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	for _, vol := range c.VolumeMounts {
		if sv, ok := vol.(*forge.SecretVolume); ok {
			if sv.SecretName == secretRegistryConfig && !sv.ReadOnly {
				t.Errorf("registry config mount must be ReadOnly")
			}
		}
	}
}

// TestSyftScanTemplate_NoCosignKeyMount — defense in depth. The SBOM
// scanner has no business holding the signing key.
func TestSyftScanTemplate_NoCosignKeyMount(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	for _, vol := range c.VolumeMounts {
		if sv, ok := vol.(*forge.SecretVolume); ok {
			if sv.SecretName == secretCosignKey {
				t.Error("syft step must NOT mount cosign signing key")
			}
		}
	}
}

// =============================================================================
// Workflow integration — DAG includes sbom-{app} task
// =============================================================================

func TestWorkflowBuilder_IncludesSBOMStep(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	dag := findDAG(t, wf, "build-pipeline")
	if findTaskOpt(dag, "sbom-api") == nil {
		t.Errorf("DAG must include sbom-api task")
	}
}

func TestWorkflowBuilder_SBOMDependsOnSign(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, _ := builder.Build(testSpec())
	dag := findDAG(t, wf, "build-pipeline")
	task := findTask(t, dag, "sbom-api")

	depsOK := false
	for _, d := range task.Dependencies {
		if d == "sign-image-api" {
			depsOK = true
		}
	}
	if !depsOK {
		t.Errorf("sbom-api must depend on sign-image-api, deps=%v", task.Dependencies)
	}
}

// SBOM failure must not block the deploy.
func TestWorkflowBuilder_SBOMTaskContinueOnFailed(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, _ := builder.Build(testSpec())
	dag := findDAG(t, wf, "build-pipeline")
	task := findTask(t, dag, "sbom-api")
	if task.ContinueOn == nil || !task.ContinueOn.Failed {
		t.Error("sbom-{app} task MUST have continueOn.failed=true (best-effort)")
	}
}

// TestSyftScanTemplate_MemoryLimitsAccommodateNodeJSScans confirms the
// SBOM step carries enough memory to scan a typical loopzerokit Node.js
// image. LIVE-caught 2026-06-18 against build-25d1977102ec378b:
// 1Gi memory limit caused OOMKilled (exit code 137) on a single-app
// loopzerokit Node.js build. syft holds the layer index + dependency
// graph in memory proportional to image size; ~256Mi-1Gi insufficient
// for any non-trivial Node.js layer set. New floor: 2Gi limit /
// 512Mi request balances OOM headroom vs cluster scheduling pressure.
func TestSyftScanTemplate_MemoryLimitsAccommodateNodeJSScans(t *testing.T) {
	c := syftScanTemplate("tn-1", "proj-1", "abc1234", "app",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	if c.Resources == nil {
		t.Fatalf("syft template missing Resources spec")
	}
	if got := c.Resources.Limits.Memory; got != "2Gi" {
		t.Errorf("syft memory limit = %q; want 2Gi (closes LIVE OOM build-25d1977102ec378b)", got)
	}
	if got := c.Resources.Requests.Memory; got != "512Mi" {
		t.Errorf("syft memory request = %q; want 512Mi", got)
	}
}
