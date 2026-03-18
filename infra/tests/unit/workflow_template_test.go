package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// cwtDir returns the path to the ClusterWorkflowTemplate YAML files.
func cwtDir() string {
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
func loadCWT(t *testing.T, filename string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cwtDir(), filename))
	if err != nil {
		t.Fatalf("cannot read %s: %v", filename, err)
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
	cwts := loadAllCWTs(t)

	expectedCWTs := []string{
		"cwt-clone.yaml",
		"cwt-cache-restore.yaml",
		"cwt-cache-save.yaml",
		"cwt-kaniko.yaml",
		"cwt-manifests-generate.yaml",
		"cwt-manifests-validate.yaml",
		"cwt-notify.yaml",
		"cwt-summary.yaml",
	}

	for _, expected := range expectedCWTs {
		if _, ok := cwts[expected]; !ok {
			t.Errorf("missing CWT file: %s", expected)
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

func TestCWTs_AllHaveLoopZeroPrefix(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		metadata, ok := tmpl["metadata"].(map[string]interface{})
		if !ok {
			t.Errorf("%s: missing metadata", name)
			continue
		}
		cwtName, _ := metadata["name"].(string)
		if !strings.HasPrefix(cwtName, "loopzero-") {
			t.Errorf("%s: metadata.name = %q, want loopzero-* prefix", name, cwtName)
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

func TestCWTs_AllContainersNonRoot_ExceptKaniko(t *testing.T) {
	cwts := loadAllCWTs(t)

	for name, tmpl := range cwts {
		var containers []map[string]interface{}
		getAllContainers(tmpl, &containers)

		for i, c := range containers {
			image, _ := c["image"].(string)

			// Kaniko requires root to build container images
			if strings.Contains(image, "kaniko") {
				continue
			}

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
	assertCWTHasParams(t, tmpl, "cwt-clone.yaml", []string{
		"gitea-url", "gitea-token", "tenant-id", "project-id",
	})
}

// ---------------------------------------------------------------------------
// cwt-kaniko: specific validations
// ---------------------------------------------------------------------------

func TestCWT_Kaniko_HasDigestFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cwtDir(), "cwt-kaniko.yaml"))
	if err != nil {
		t.Fatalf("read cwt-kaniko.yaml: %v", err)
	}
	if !strings.Contains(string(data), "--digest-file") {
		t.Error("cwt-kaniko: missing --digest-file flag — callback needs real SHA256 digest (Helena review #5)")
	}
}

func TestCWT_Kaniko_HasRetryStrategy(t *testing.T) {
	tmpl := loadCWT(t, "cwt-kaniko.yaml")
	templates := getCWTTemplates(tmpl)
	for _, tm := range templates {
		if _, ok := tm["retryStrategy"]; !ok {
			t.Error("cwt-kaniko: missing retryStrategy")
		}
	}
}

// ---------------------------------------------------------------------------
// cwt-notify: specific validations
// ---------------------------------------------------------------------------

func TestCWT_Notify_HasRetryStrategy(t *testing.T) {
	tmpl := loadCWT(t, "cwt-notify.yaml")
	templates := getCWTTemplates(tmpl)
	for _, tm := range templates {
		retryStrategy, ok := tm["retryStrategy"].(map[string]interface{})
		if !ok {
			t.Error("cwt-notify: missing retryStrategy")
			continue
		}
		limit, _ := retryStrategy["limit"]
		limitStr := ""
		switch v := limit.(type) {
		case string:
			limitStr = v
		case int:
			if v == 3 {
				limitStr = "3"
			}
		}
		if limitStr != "3" {
			t.Errorf("cwt-notify: retryStrategy.limit = %v, want 3", limit)
		}
	}
}

func TestCWT_Notify_HasBearerAuth(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cwtDir(), "cwt-notify.yaml"))
	if err != nil {
		t.Fatalf("read cwt-notify.yaml: %v", err)
	}
	if !strings.Contains(string(data), "Bearer") {
		t.Error("cwt-notify: does not use Bearer token auth")
	}
}

func TestCWT_Notify_HasCorrelationID(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cwtDir(), "cwt-notify.yaml"))
	if err != nil {
		t.Fatalf("read cwt-notify.yaml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "correlation_id") && !strings.Contains(content, "correlation-id") {
		t.Error("cwt-notify: callback does not include correlation_id (Helena review #6)")
	}
}

func TestCWT_Notify_UsesDigestFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cwtDir(), "cwt-notify.yaml"))
	if err != nil {
		t.Fatalf("read cwt-notify.yaml: %v", err)
	}
	if !strings.Contains(string(data), "image-digest") {
		t.Error("cwt-notify: does not reference image-digest file — callback should use real SHA256 digest (Helena review #5)")
	}
}

func TestCWT_Notify_HasRequiredParams(t *testing.T) {
	tmpl := loadCWT(t, "cwt-notify.yaml")
	assertCWTHasParams(t, tmpl, "cwt-notify.yaml", []string{
		"api-callback-url", "callback-secret", "registry-url",
		"tenant-id", "project-id", "environment",
		"commit-sha", "build-id", "correlation-id", "workflow-status",
	})
}

// ---------------------------------------------------------------------------
// cwt-manifests-validate: specific validations
// ---------------------------------------------------------------------------

func TestCWT_ManifestsValidate_UsesKubeconformImage(t *testing.T) {
	tmpl := loadCWT(t, "cwt-manifests-validate.yaml")
	templates := getCWTTemplates(tmpl)
	for _, tm := range templates {
		container, ok := tm["container"].(map[string]interface{})
		if !ok {
			continue
		}
		image, _ := container["image"].(string)
		if strings.HasPrefix(image, "alpine:") {
			t.Errorf("cwt-manifests-validate: uses generic alpine image %q — should use kubeconform image (Helena review #4)", image)
		}
	}
}

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
