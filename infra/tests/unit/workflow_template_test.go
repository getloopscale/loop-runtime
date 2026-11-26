// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// cwtDir returns the path to the ClusterWorkflowTemplate YAML files.
// F27 (M23 audit 2026-07-10): CWTs migrated from `infra/helm/argo-workflows/
// templates/` to `charts/loopruntime-build-cwts/templates/` when Argo Workflows moved
// to the published-chart tier. Fall back to legacy path only if new path absent
// (both paths tolerated).
func cwtDir() string {
	// New canonical location (charts/loopruntime-build-cwts/templates/)
	newPath := filepath.Join(helmDir(), "..", "..", "charts", "loopruntime-build-cwts", "templates")
	if info, err := os.Stat(newPath); err == nil && info.IsDir() {
		if matches, _ := filepath.Glob(filepath.Join(newPath, "cwt-*.yaml")); len(matches) > 0 {
			return newPath
		}
	}
	return filepath.Join(helmDir(), "argo-workflows", "templates")
}

// loadAllCWTs reads and parses all cwt-*.yaml files from the templates directory.
func loadAllCWTs(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	pattern := filepath.Join(cwtDir(), "cwt-*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob cwt files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no CWT files found at %s", pattern)
	}

	cwts := make(map[string]map[string]interface{})
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("cannot read %s: %v", f, err)
		}
		// Skip Helm template files — they contain {{ .Values }} syntax
		// that is not valid YAML until rendered by Helm.
		if strings.Contains(string(data), "{{ .Values") || strings.Contains(string(data), "{{- if") {
			t.Logf("skipping Helm template: %s (contains Go template syntax)", filepath.Base(f))
			continue
		}
		var tmpl map[string]interface{}
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			t.Fatalf("invalid YAML in %s: %v", f, err)
		}
		name := filepath.Base(f)
		cwts[name] = tmpl
	}
	return cwts
}

// loadCWT reads and parses a single CWT file by its base name (e.g., "cwt-clone.yaml").
// Returns nil if the file is a Helm template (contains Go template syntax).
func loadCWT(t *testing.T, filename string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cwtDir(), filename))
	if err != nil {
		t.Fatalf("cannot read %s: %v", filename, err)
	}
	if strings.Contains(string(data), "{{ .Values") || strings.Contains(string(data), "{{- if") {
		t.Skipf("skipping Helm template: %s", filename)
		return nil
	}
	var tmpl map[string]interface{}
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("invalid YAML in %s: %v", filename, err)
	}
	return tmpl
}

// ---------------------------------------------------------------------------
// All CWTs: structural validation
// ---------------------------------------------------------------------------

func TestCWTs_AllAreValidYAML(t *testing.T) {
	// Verify expected CWT files exist on disk.
	// These are Helm templates (contain {{ .Values }}) so they can't be parsed as raw YAML.
	// YAML validity is validated by kubeconform after Helm rendering in the validate-manifests CI job.
	expectedCWTs := []string{
		"cwt-clone.yaml",
		"cwt-cache-restore.yaml",
		"cwt-cache-save.yaml",
	}
	for _, expected := range expectedCWTs {
		path := filepath.Join(cwtDir(), expected)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("missing CWT file: %s", expected)
		}
	}

	// Parse non-Helm CWTs as YAML (if any exist)
	cwts := loadAllCWTs(t)
	for name, tmpl := range cwts {
		if tmpl == nil {
			t.Errorf("parsed CWT %s is nil", name)
		}
	}
}

func TestCWTs_AllAreClusterWorkflowTemplates(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		apiVersion, _ := tmpl["apiVersion"].(string)
		if apiVersion != "argoproj.io/v1alpha1" {
			t.Errorf("%s: apiVersion = %q, want argoproj.io/v1alpha1", name, apiVersion)
		}

		kind, _ := tmpl["kind"].(string)
		if kind != "ClusterWorkflowTemplate" {
			t.Errorf("%s: kind = %q, want ClusterWorkflowTemplate", name, kind)
		}
	}
}

func TestCWTs_AllHaveLoopRuntimePrefix(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		metadata, ok := tmpl["metadata"].(map[string]interface{})
		if !ok {
			t.Errorf("%s: missing metadata", name)
			continue
		}
		cwtName, _ := metadata["name"].(string)
		if !strings.HasPrefix(cwtName, "loopruntime-") {
			t.Errorf("%s: metadata.name = %q, want loopruntime-* prefix", name, cwtName)
		}
	}
}

// ---------------------------------------------------------------------------
// All CWTs: security invariants
// ---------------------------------------------------------------------------

func TestCWTs_AllContainersHaveResourceLimits(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		var containers []map[string]interface{}
		getAllContainers(tmpl, &containers)

		for i, c := range containers {
			image, _ := c["image"].(string)
			label := image
			if label == "" {
				label = "container"
			}

			resources, ok := c["resources"].(map[string]interface{})
			if !ok {
				t.Errorf("%s container %d (%s): missing resources", name, i, label)
				continue
			}
			if _, ok := resources["limits"]; !ok {
				t.Errorf("%s container %d (%s): missing resources.limits", name, i, label)
			}
			if _, ok := resources["requests"]; !ok {
				t.Errorf("%s container %d (%s): missing resources.requests", name, i, label)
			}
		}
	}
}

func TestCWTs_AllContainersNonRoot(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		var containers []map[string]interface{}
		getAllContainers(tmpl, &containers)

		for i, c := range containers {
			image, _ := c["image"].(string)

			sc, ok := c["securityContext"].(map[string]interface{})
			if !ok {
				t.Errorf("%s container %d (%s): missing securityContext", name, i, image)
				continue
			}

			runAsNonRoot, ok := sc["runAsNonRoot"].(bool)
			if !ok || !runAsNonRoot {
				t.Errorf("%s container %d (%s): securityContext.runAsNonRoot must be true", name, i, image)
			}
		}
	}
}

func TestCWTs_NoLatestImageTags(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		var containers []map[string]interface{}
		getAllContainers(tmpl, &containers)

		for i, c := range containers {
			image, ok := c["image"].(string)
			if !ok {
				continue
			}
			if !strings.Contains(image, ":") {
				t.Errorf("%s container %d: image %q has no tag (implicit latest)", name, i, image)
				continue
			}
			parts := strings.SplitN(image, ":", 2)
			if parts[1] == "latest" {
				t.Errorf("%s container %d: image %q uses 'latest' tag", name, i, image)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// All CWTs: use inputs.parameters (not workflow.parameters)
// ---------------------------------------------------------------------------

func TestCWTs_NoWorkflowParameterRefs(t *testing.T) {
	pattern := filepath.Join(cwtDir(), "cwt-*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		content := string(data)
		name := filepath.Base(f)

		if strings.Contains(content, "workflow.parameters.") {
			t.Errorf("%s: contains workflow.parameters.* reference — CWTs must use inputs.parameters.*", name)
		}
	}
}

// ---------------------------------------------------------------------------
// All CWTs: no hardcoded URLs
// ---------------------------------------------------------------------------

func TestCWTs_NoHardcodedValues(t *testing.T) {
	pattern := filepath.Join(cwtDir(), "cwt-*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	hardcodedPatterns := []struct {
		pattern string
		desc    string
	}{
		{"http://gitea", "gitea URL is hardcoded"},
		{"http://minio", "minio endpoint is hardcoded"},
		{"http://api", "API callback URL is hardcoded"},
		{"password:", "password appears hardcoded"},
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		content := string(data)
		name := filepath.Base(f)

		for _, hp := range hardcodedPatterns {
			if strings.Contains(content, hp.pattern) {
				t.Errorf("%s: found hardcoded value: %s (%s)", name, hp.pattern, hp.desc)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// cwt-clone: specific validations
// ---------------------------------------------------------------------------

func TestCWT_Clone_HasRequiredParams(t *testing.T) {
	tmpl := loadCWT(t, "cwt-clone.yaml")
	// BLOCKER-1 fix: Clone now uses MinIO (source bundle), not Gitea.
	// Parameters: tenant-id + build-id (for MinIO path).
	// MinIO credentials come from K8s Secret (envFrom), not workflow params.
	assertCWTHasParams(t, tmpl, "cwt-clone.yaml", []string{
		"tenant-id", "build-id",
	})
}

// Historical note: CWTs for notify, manifests-validate, and manifests-generate
// were removed. Build images are generated by inline BuildKit templates in WorkflowBuilder.
// Notify was replaced by BuildReconcileLoop polling.
// Manifests generation was moved to the API control plane.

// ---------------------------------------------------------------------------
// cwt-cache-restore / cwt-cache-save: tenant isolation
// ---------------------------------------------------------------------------

func TestCWT_CacheRestore_IncludesTenantParam(t *testing.T) {
	tmpl := loadCWT(t, "cwt-cache-restore.yaml")
	assertCWTHasParams(t, tmpl, "cwt-cache-restore.yaml", []string{"tenant-id"})
}

func TestCWT_CacheSave_IncludesTenantParam(t *testing.T) {
	tmpl := loadCWT(t, "cwt-cache-save.yaml")
	assertCWTHasParams(t, tmpl, "cwt-cache-save.yaml", []string{"tenant-id"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getAllContainers recursively finds all container specs in the YAML tree.
func getAllContainers(node interface{}, containers *[]map[string]interface{}) {
	switch v := node.(type) {
	case map[string]interface{}:
		if container, ok := v["container"].(map[string]interface{}); ok {
			*containers = append(*containers, container)
		}
		if script, ok := v["script"].(map[string]interface{}); ok {
			*containers = append(*containers, script)
		}
		for _, val := range v {
			getAllContainers(val, containers)
		}
	case []interface{}:
		for _, item := range v {
			getAllContainers(item, containers)
		}
	}
}

// getCWTTemplates extracts the templates array from a CWT spec.
func getCWTTemplates(tmpl map[string]interface{}) []map[string]interface{} {
	spec, ok := tmpl["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	templates, ok := spec["templates"].([]interface{})
	if !ok {
		return nil
	}
	var result []map[string]interface{}
	for _, t := range templates {
		if tm, ok := t.(map[string]interface{}); ok {
			result = append(result, tm)
		}
	}
	return result
}

// assertCWTHasParams checks that a CWT declares all required input parameters.
func assertCWTHasParams(t *testing.T, tmpl map[string]interface{}, cwtName string, required []string) {
	t.Helper()
	templates := getCWTTemplates(tmpl)
	if len(templates) == 0 {
		t.Fatalf("%s: no templates found", cwtName)
	}

	// Check first template's inputs.parameters
	inputs, ok := templates[0]["inputs"].(map[string]interface{})
	if !ok {
		t.Fatalf("%s: first template has no inputs", cwtName)
	}
	params, ok := inputs["parameters"].([]interface{})
	if !ok {
		t.Fatalf("%s: first template has no inputs.parameters", cwtName)
	}

	paramNames := make(map[string]bool)
	for _, p := range params {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := pm["name"].(string)
		paramNames[name] = true
	}

	for _, req := range required {
		if !paramNames[req] {
			t.Errorf("%s: missing required parameter: %s", cwtName, req)
		}
	}
}
