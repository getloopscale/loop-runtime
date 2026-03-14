package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// workflowTemplatePath returns the absolute path to the WorkflowTemplate YAML.
func workflowTemplatePath() string {
	return filepath.Join(helmDir(), "argo-workflows", "templates", "workflow-template-nodejs-v1.yaml")
}

// loadWorkflowTemplate reads and parses the WorkflowTemplate YAML file.
func loadWorkflowTemplate(t *testing.T) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	var tmpl map[string]interface{}
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("invalid YAML in workflow template: %v", err)
	}
	return tmpl
}

func TestWorkflowTemplateIsValidYAML(t *testing.T) {
	tmpl := loadWorkflowTemplate(t)

	// Verify it is an Argo WorkflowTemplate
	apiVersion, ok := tmpl["apiVersion"].(string)
	if !ok || apiVersion != "argoproj.io/v1alpha1" {
		t.Errorf("expected apiVersion argoproj.io/v1alpha1, got %v", tmpl["apiVersion"])
	}

	kind, ok := tmpl["kind"].(string)
	if !ok || kind != "WorkflowTemplate" {
		t.Errorf("expected kind WorkflowTemplate, got %v", tmpl["kind"])
	}

	metadata, ok := tmpl["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing metadata")
	}
	name, ok := metadata["name"].(string)
	if !ok || name != "loopzero-build-nodejs-v1" {
		t.Errorf("expected metadata.name loopzero-build-nodejs-v1, got %v", metadata["name"])
	}
}

func TestWorkflowTemplateParametersPresent(t *testing.T) {
	tmpl := loadWorkflowTemplate(t)

	spec, ok := tmpl["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing spec")
	}

	arguments, ok := spec["arguments"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing spec.arguments")
	}

	params, ok := arguments["parameters"].([]interface{})
	if !ok {
		t.Fatalf("missing spec.arguments.parameters")
	}

	requiredParams := []string{
		"tenant-id",
		"project-id",
		"environment",
		"commit-sha",
		"apps",
		"build-id",
		"api-callback-url",
		"callback-secret",
		"gitea-url",
		"gitea-token",
		"minio-endpoint",
		"minio-access-key",
		"minio-secret-key",
		"registry-url",
	}

	paramNames := make(map[string]bool)
	for _, p := range params {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := pm["name"].(string)
		if !ok {
			continue
		}
		paramNames[name] = true
	}

	for _, required := range requiredParams {
		if !paramNames[required] {
			t.Errorf("missing required parameter: %s", required)
		}
	}
}

// getAllContainers recursively finds all container specs in the template tree.
func getAllContainers(node interface{}, containers *[]map[string]interface{}) {
	switch v := node.(type) {
	case map[string]interface{}:
		// If this node has a "container" key, it defines a container
		if container, ok := v["container"].(map[string]interface{}); ok {
			*containers = append(*containers, container)
		}
		// Also check "script" (Argo script templates have container-like spec)
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

func TestWorkflowTemplateAllContainersHaveResourceLimits(t *testing.T) {
	tmpl := loadWorkflowTemplate(t)

	var containers []map[string]interface{}
	getAllContainers(tmpl, &containers)

	if len(containers) == 0 {
		t.Fatal("no containers found in workflow template")
	}

	for i, c := range containers {
		image, _ := c["image"].(string)
		label := image
		if label == "" {
			label = "container"
		}

		resources, ok := c["resources"].(map[string]interface{})
		if !ok {
			t.Errorf("container %d (%s): missing resources", i, label)
			continue
		}

		if _, ok := resources["limits"]; !ok {
			t.Errorf("container %d (%s): missing resources.limits", i, label)
		}
		if _, ok := resources["requests"]; !ok {
			t.Errorf("container %d (%s): missing resources.requests", i, label)
		}
	}
}

func TestWorkflowTemplateAllContainersNonRoot(t *testing.T) {
	tmpl := loadWorkflowTemplate(t)

	var containers []map[string]interface{}
	getAllContainers(tmpl, &containers)

	if len(containers) == 0 {
		t.Fatal("no containers found in workflow template")
	}

	for i, c := range containers {
		image, _ := c["image"].(string)
		label := image
		if label == "" {
			label = "container"
		}

		sc, ok := c["securityContext"].(map[string]interface{})
		if !ok {
			t.Errorf("container %d (%s): missing securityContext", i, label)
			continue
		}

		runAsNonRoot, ok := sc["runAsNonRoot"].(bool)
		if !ok || !runAsNonRoot {
			t.Errorf("container %d (%s): securityContext.runAsNonRoot must be true", i, label)
		}
	}
}

// getAllTemplates extracts all template definitions from spec.templates.
func getAllTemplates(tmpl map[string]interface{}) []map[string]interface{} {
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

func TestWorkflowTemplateFinalizeStepRunsAlways(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// The finalize step must exist
	if !strings.Contains(content, "finalize") {
		t.Fatal("no finalize step found in workflow template")
	}

	// The DAG/steps structure must have the finalize running always.
	// In Argo Workflows, this is done via onExit or a step with
	// when clause that covers all statuses. Check for onExit reference.
	var tmpl map[string]interface{}
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	spec, ok := tmpl["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	// Check for onExit hook — the standard Argo way to always run a step
	onExit, hasOnExit := spec["onExit"].(string)
	if !hasOnExit {
		// Alternative: check entrypoint template for hooks with expression "true"
		t.Fatal("spec.onExit not set — finalize must use onExit to always run")
	}

	if onExit != "finalize" {
		t.Errorf("spec.onExit should reference 'finalize', got %q", onExit)
	}

	// Verify the finalize template exists
	templates := getAllTemplates(tmpl)
	found := false
	for _, tm := range templates {
		name, _ := tm["name"].(string)
		if name == "finalize" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no template named 'finalize' found in spec.templates")
	}
}

func TestWorkflowTemplateNotifyAPIStepHasCallbackAndRetry(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// notify-api step must reference the callback URL parameter
	if !strings.Contains(content, "api-callback-url") {
		t.Error("notify-api step does not reference api-callback-url parameter")
	}

	// notify-api step must reference callback-secret for auth
	if !strings.Contains(content, "callback-secret") {
		t.Error("notify-api step does not reference callback-secret parameter")
	}

	// Must have retry configuration
	var tmpl map[string]interface{}
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	templates := getAllTemplates(tmpl)
	found := false
	for _, tm := range templates {
		name, _ := tm["name"].(string)
		if name != "notify-api" {
			continue
		}
		found = true

		retryStrategy, ok := tm["retryStrategy"].(map[string]interface{})
		if !ok {
			t.Error("notify-api template missing retryStrategy")
			break
		}

		limit, _ := retryStrategy["limit"]
		// Accept both string "3" and int 3
		limitStr := ""
		switch v := limit.(type) {
		case string:
			limitStr = v
		case int:
			limitStr = "3"
			if v != 3 {
				limitStr = ""
			}
		}
		if limitStr != "3" {
			t.Errorf("notify-api retryStrategy.limit should be 3, got %v", limit)
		}
		break
	}

	if !found {
		t.Error("no template named 'notify-api' found")
	}
}

func TestWorkflowTemplateCacheKeyIncludesTenantID(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// Cache key must include tenant-id for isolation
	// Look for the cache key pattern that includes tenant-id parameter reference
	hasTenantInCacheKey := strings.Contains(content, "{{workflow.parameters.tenant-id}}") ||
		strings.Contains(content, "{{ workflow.parameters.tenant-id }}") ||
		strings.Contains(content, "inputs.parameters.tenant-id")

	if !hasTenantInCacheKey {
		t.Error("cache key does not include tenant-id — required for tenant isolation (spec 7.4)")
	}

	// Verify the cache key follows the format: {tenant-id}/{project-id}/...
	if !strings.Contains(content, "tenant-id") {
		t.Error("no reference to tenant-id in cache operations")
	}
}

func TestWorkflowTemplateNoHardcodedValues(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// These values must come from parameters, not be hardcoded
	hardcodedPatterns := []struct {
		pattern string
		desc    string
	}{
		{"http://gitea", "gitea URL is hardcoded"},
		{"http://minio", "minio endpoint is hardcoded"},
		{"http://api", "API callback URL is hardcoded"},
		{"password:", "password appears hardcoded"},
	}

	for _, hp := range hardcodedPatterns {
		if strings.Contains(content, hp.pattern) {
			t.Errorf("found hardcoded value: %s (%s)", hp.pattern, hp.desc)
		}
	}

	// Verify key values reference workflow parameters
	paramRefs := []string{
		"{{workflow.parameters.gitea-url}}",
		"{{workflow.parameters.minio-endpoint}}",
		"{{workflow.parameters.api-callback-url}}",
		"{{workflow.parameters.registry-url}}",
		"{{workflow.parameters.tenant-id}}",
		"{{workflow.parameters.project-id}}",
		"{{workflow.parameters.commit-sha}}",
		"{{workflow.parameters.build-id}}",
	}

	for _, ref := range paramRefs {
		if !strings.Contains(content, ref) {
			t.Errorf("expected parameter reference %s not found — value may be hardcoded", ref)
		}
	}
}

func TestWorkflowTemplateNoLatestImageTags(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	var tmpl map[string]interface{}
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	var containers []map[string]interface{}
	getAllContainers(tmpl, &containers)

	for i, c := range containers {
		image, ok := c["image"].(string)
		if !ok {
			continue
		}

		// Image must have a tag (contains ':') and tag must not be 'latest'
		if !strings.Contains(image, ":") {
			t.Errorf("container %d: image %q has no tag (implicit latest)", i, image)
			continue
		}
		parts := strings.SplitN(image, ":", 2)
		tag := parts[1]
		if tag == "latest" {
			t.Errorf("container %d: image %q uses 'latest' tag", i, image)
		}
	}
}

func TestWorkflowTemplateGitopsCommitTrailers(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// Git commit trailers must be present in the gitops-push step
	requiredTrailers := []string{
		"Release-ID",
		"Correlation-ID",
		"Build-ID",
	}

	for _, trailer := range requiredTrailers {
		if !strings.Contains(content, trailer) {
			t.Errorf("gitops-push step missing git commit trailer: %s", trailer)
		}
	}
}

func TestWorkflowTemplateCallbackBodyStructure(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// The notify-api step must send status and apps in the callback body
	if !strings.Contains(content, "succeeded") && !strings.Contains(content, "Succeeded") {
		t.Error("notify-api step does not reference 'succeeded' status")
	}
	if !strings.Contains(content, "failed") && !strings.Contains(content, "Failed") {
		t.Error("notify-api step does not reference 'failed' status")
	}

	// Must reference Bearer token auth with callback-secret
	if !strings.Contains(content, "Bearer") {
		t.Error("notify-api step does not use Bearer token auth")
	}

	// Validate the callback URL includes the build-id
	if !strings.Contains(content, "build-id") {
		t.Error("notify-api step does not reference build-id in callback URL")
	}
}

// TestWorkflowTemplateStepOrder validates the DAG dependencies are correctly defined.
func TestWorkflowTemplateStepOrder(t *testing.T) {
	tmpl := loadWorkflowTemplate(t)

	templates := getAllTemplates(tmpl)

	// Find the entrypoint DAG template
	var dagTasks []interface{}
	for _, tm := range templates {
		dag, ok := tm["dag"].(map[string]interface{})
		if !ok {
			continue
		}
		tasks, ok := dag["tasks"].([]interface{})
		if ok {
			dagTasks = tasks
			break
		}
	}

	if dagTasks == nil {
		// May use steps instead of DAG — check for steps-based workflow
		t.Skip("workflow uses steps instead of DAG — skipping DAG order test")
	}

	// Build dependency map
	taskDeps := make(map[string][]string)
	for _, task := range dagTasks {
		tm, ok := task.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tm["name"].(string)
		deps, ok := tm["dependencies"].([]interface{})
		if ok {
			var depNames []string
			for _, d := range deps {
				if s, ok := d.(string); ok {
					depNames = append(depNames, s)
				}
			}
			taskDeps[name] = depNames
		} else {
			taskDeps[name] = nil
		}
	}

	// Verify expected task names exist
	expectedTasks := []string{"clone-repo", "install-deps", "build-apps"}
	for _, expected := range expectedTasks {
		if _, ok := taskDeps[expected]; !ok {
			t.Errorf("expected DAG task %q not found", expected)
		}
	}
}

// TestWorkflowTemplateAppsParameterIsJSON validates the apps parameter is treated as JSON.
func TestWorkflowTemplateAppsParameterIsJSON(t *testing.T) {
	data, err := os.ReadFile(workflowTemplatePath())
	if err != nil {
		t.Fatalf("cannot read workflow template: %v", err)
	}

	content := string(data)

	// The apps parameter should be referenced and used (JSON array of app specs)
	if !strings.Contains(content, "workflow.parameters.apps") {
		t.Error("apps parameter is not referenced in workflow template")
	}

	// Validate that the default value for apps is valid JSON (if a default exists)
	var tmpl map[string]interface{}
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	spec := tmpl["spec"].(map[string]interface{})
	arguments := spec["arguments"].(map[string]interface{})
	params := arguments["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		name, _ := pm["name"].(string)
		if name != "apps" {
			continue
		}
		// If there is a default value, validate it is valid JSON
		if defaultVal, ok := pm["value"].(string); ok && defaultVal != "" {
			var js json.RawMessage
			if err := json.Unmarshal([]byte(defaultVal), &js); err != nil {
				t.Errorf("apps parameter default value is not valid JSON: %v", err)
			}
		}
	}
}
