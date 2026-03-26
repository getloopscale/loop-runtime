// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/loopzero-forge"

	"github.com/loopzero/loopzero/api/internal/services"
)

func testInfraParams() InfraParams {
	return InfraParams{
		RegistryURL:      "k3d-loopzero-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopzero.local",
	}
}

func testSpec() services.WorkflowSpec {
	return services.WorkflowSpec{
		BuildID:       "build-001",
		TenantID:      "acme",
		ProjectID:     "my-app",
		Environment:   "production",
		CommitSHA:     "abc123",
		CorrelationID: "corr-001",
		Apps: []services.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Framework: "express"},
		},
	}
}

// ---------------------------------------------------------------------------
// Workflow structure
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_ReturnsValidWorkflow(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Metadata
	if wf.GenerateName != "loopzero-build-build-001-" {
		t.Errorf("GenerateName = %q, want %q", wf.GenerateName, "loopzero-build-build-001-")
	}

	// Labels
	assertLabel(t, wf, "loopzero.io/tenant", "acme")
	assertLabel(t, wf, "loopzero.io/project", "my-app")
	assertLabel(t, wf, "loopzero.io/environment", "production")
	assertLabel(t, wf, "loopzero.io/build-id", "build-001")
	assertLabel(t, wf, "loopzero.io/language", "node")
}

func TestWorkflowBuilder_Build_SpecFields(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if wf.Entrypoint != "build-pipeline" {
		t.Errorf("Entrypoint = %q, want %q", wf.Entrypoint, "build-pipeline")
	}
	if wf.OnExit != "finalize" {
		t.Errorf("OnExit = %q, want %q", wf.OnExit, "finalize")
	}
	if wf.ServiceAccountName != "loopzero-build-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", wf.ServiceAccountName, "loopzero-build-sa")
	}
	if wf.Parallelism == nil || *wf.Parallelism != 4 {
		t.Errorf("Parallelism = %v, want 4", wf.Parallelism)
	}

	// TTL strategy
	if wf.TTLStrategy == nil {
		t.Fatal("TTLStrategy is nil")
	}
	// REVIEW-LEVE-2: TTL increased from 1h (3600) to 4h (14400)
	if wf.TTLStrategy.SecondsAfterCompletion == nil || *wf.TTLStrategy.SecondsAfterCompletion != 14400 {
		t.Errorf("SecondsAfterCompletion = %v, want 14400 (4h)", wf.TTLStrategy.SecondsAfterCompletion)
	}
	// LEVE-3 fix: Failed workflows retained 24h (was 6h) for debugging
	if wf.TTLStrategy.SecondsAfterFailure == nil || *wf.TTLStrategy.SecondsAfterFailure != 86400 {
		t.Errorf("SecondsAfterFailure = %v, want 86400 (24h)", wf.TTLStrategy.SecondsAfterFailure)
	}
}

// BLOCKER-2 fix: Workspace and manifests volumes are now emptyDir (not PVCs).
// emptyDir is automatically cleaned up when the pod terminates, preventing PVC storage leak.
func TestWorkflowBuilder_Build_HasSharedPVCVolume(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Workspace uses VolumeClaimTemplates for cross-pod sharing in DAG workflows.
	// Each DAG step runs in its own pod — emptyDir does NOT share across pods.
	if len(wf.VolumeClaimTemplates) != 1 {
		t.Fatalf("want 1 VolumeClaimTemplate (workspace PVC), got %d", len(wf.VolumeClaimTemplates))
	}
	if wf.VolumeClaimTemplates[0].Name != "workspace" {
		t.Errorf("VolumeClaimTemplate name = %q, want workspace", wf.VolumeClaimTemplates[0].Name)
	}
}

// ---------------------------------------------------------------------------
// DAG structure
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_MainDAGHasAllTasks(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "build-pipeline")

	// C-02 fix: generate-manifests and validate-manifests removed.
	// REVIEW-MOD-4: build-image is now per-app: build-image-{app.Name}
	expectedTasks := []string{
		"clone-repo",
		"restore-cache",
		"validate-workspace",
		"security-scan",
		"install-deps",
		"build-apps",
		"generate-dockerfile",
		"build-image-api", // per-app BuildKit task (testSpec has 1 app: "api")
	}

	taskNames := make(map[string]bool)
	for _, task := range dag.Tasks {
		taskNames[task.Name] = true
	}

	for _, expected := range expectedTasks {
		if !taskNames[expected] {
			t.Errorf("missing DAG task: %s", expected)
		}
	}
}

func TestWorkflowBuilder_Build_OnExitDAGHasTasks(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "finalize")

	taskNames := make(map[string]bool)
	for _, task := range dag.Tasks {
		taskNames[task.Name] = true
	}

	// REVIEW-LEVE-4: emit-summary removed — no consumer for step output.
	if taskNames["emit-summary"] {
		t.Error("finalize DAG should NOT contain emit-summary — removed as no consumer")
	}
	// C-01 fix: notify-api removed from onExit.
	if taskNames["notify-api"] {
		t.Error("finalize DAG should NOT contain notify-api — callback removed")
	}
}

func TestWorkflowBuilder_Build_NodeStrategy_HasSaveCache(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "finalize")
	task := findTask(dag, "save-cache")
	if task == nil {
		t.Error("Node strategy should include save-cache in finalize DAG")
	}
}

func TestWorkflowBuilder_Build_CustomStrategy_NoSaveCache(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{{Name: "api", Type: "server", Port: 3000, Framework: "custom"}}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Custom strategy has no cache key/dirs, so finalize DAG should not exist at all.
	// When there are no cache tasks, buildOnExitDAG returns nil and no finalize DAG is added.
	for _, tmpl := range wf.Templates {
		if dag, ok := tmpl.(*forge.DAG); ok && dag.Name == "finalize" {
			t.Error("Custom strategy should NOT have a finalize DAG — no cache to save")
		}
	}
	if wf.OnExit != "" {
		t.Errorf("OnExit = %q, want empty string for custom strategy (no cache)", wf.OnExit)
	}
}

// ---------------------------------------------------------------------------
// CWT refs
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_CloneTask_UsesCWTRef(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "build-pipeline")
	task := findTask(dag, "clone-repo")
	if task == nil {
		t.Fatal("clone-repo task not found")
	}

	if task.TemplateRef == nil {
		t.Fatal("clone-repo missing templateRef")
	}
	if task.TemplateRef.Name != "loopzero-clone" {
		t.Errorf("templateRef.Name = %q, want loopzero-clone", task.TemplateRef.Name)
	}
	if !task.TemplateRef.ClusterScope {
		t.Error("templateRef.ClusterScope should be true")
	}

	// BLOCKER-1 fix: Clone now uses MinIO (source bundle), not Gitea.
	// Parameters: tenant-id + build-id (for MinIO path).
	// MinIO credentials come from K8s Secret (envFrom), not workflow params.
	paramNames := taskParamNames(task)
	for _, expected := range []string{"tenant-id", "build-id"} {
		if !paramNames[expected] {
			t.Errorf("clone-repo missing parameter: %s", expected)
		}
	}
	// project-id is no longer needed (MinIO path uses build-id)
	if paramNames["project-id"] {
		t.Error("clone-repo should NOT have parameter: project-id (BLOCKER-1: use build-id for MinIO path)")
	}
	// Credential parameters must NOT be in workflow params (security: visible in Workflow CR)
	for _, forbidden := range []string{"gitea-url", "gitea-token", "minio-endpoint", "minio-access-key"} {
		if paramNames[forbidden] {
			t.Errorf("clone-repo should NOT have parameter: %s (use K8s Secret instead)", forbidden)
		}
	}
}

// BuildKit build-image task is an inline template, not a CWT ref.
func TestWorkflowBuilder_Build_BuildImageTask_IsInlineTemplate(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Per-app BuildKit task: build-image-{app}
	dag := findDAG(t, wf, "build-pipeline")
	task := findTask(dag, "build-image-api")
	if task == nil {
		t.Fatal("build-image-api task not found")
	}

	if task.TemplateRef != nil {
		t.Error("build-image-api should NOT use templateRef — inline template")
	}
	if task.Template != "build-image-api" {
		t.Errorf("build-image-api task.Template = %q, want %q", task.Template, "build-image-api")
	}

	// Verify inline container template exists and uses BuildKit
	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api inline container template not found")
	}
	if c.Image != "moby/buildkit:v0.18.2" {
		t.Errorf("buildctl image = %q, want moby/buildkit:v0.18.2", c.Image)
	}
}

func TestWorkflowBuilder_Build_InsecureFlags_WhenRegistryInsecure(t *testing.T) {
	params := testInfraParams()
	params.RegistryInsecure = true
	builder := NewWorkflowBuilder(params)
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api inline container template not found")
	}

	// BuildKit uses --allow security.insecure for insecure registries
	argsStr := strings.Join(c.Args, " ")
	if !strings.Contains(argsStr, "--allow") {
		t.Error("expected --allow flag when RegistryInsecure=true")
	}
}

func TestWorkflowBuilder_Build_NoInsecureFlags_WhenRegistrySecure(t *testing.T) {
	params := testInfraParams()
	params.RegistryInsecure = false
	builder := NewWorkflowBuilder(params)
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api inline container template not found")
	}

	argsStr := strings.Join(c.Args, " ")
	if strings.Contains(argsStr, "--allow") {
		t.Error("--allow flag should NOT be present when RegistryInsecure=false")
	}
}

// ---------------------------------------------------------------------------
// Cache CWT tasks do NOT pass MinIO params (credentials via K8s Secret envFrom)
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_RestoreCache_NoMinIOParams(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "build-pipeline")
	task := findTask(dag, "restore-cache")
	if task == nil {
		t.Fatal("restore-cache task not found")
	}

	paramNames := taskParamNames(task)

	// MinIO credentials come from K8s Secret (envFrom), not workflow params
	for _, forbidden := range []string{"minio-endpoint", "minio-access-key", "minio-secret-key"} {
		if paramNames[forbidden] {
			t.Errorf("restore-cache should NOT have parameter: %s (use K8s Secret instead)", forbidden)
		}
	}
	// But must have tenant-id, project-id, cache-key
	for _, required := range []string{"tenant-id", "project-id", "cache-key"} {
		if !paramNames[required] {
			t.Errorf("restore-cache missing required parameter: %s", required)
		}
	}
}

func TestWorkflowBuilder_Build_SaveCache_NoMinIOParams(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "finalize")
	task := findTask(dag, "save-cache")
	if task == nil {
		t.Fatal("save-cache task not found in finalize DAG")
	}

	paramNames := taskParamNames(task)

	for _, forbidden := range []string{"minio-endpoint", "minio-access-key", "minio-secret-key"} {
		if paramNames[forbidden] {
			t.Errorf("save-cache should NOT have parameter: %s (use K8s Secret instead)", forbidden)
		}
	}
	for _, required := range []string{"tenant-id", "project-id", "cache-key", "cache-dirs"} {
		if !paramNames[required] {
			t.Errorf("save-cache missing required parameter: %s", required)
		}
	}
}

// ---------------------------------------------------------------------------
// Inline templates
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_HasInlineTemplates(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	inlineNames := []string{
		"validate-workspace",
		"install-deps",
		"build-apps",
		"security-scan",
		"generate-dockerfile",
		"build-image-api", // per-app BuildKit template
	}

	for _, name := range inlineNames {
		c := findContainer(t, wf, name)
		if c == nil {
			t.Errorf("missing inline container template: %s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// DAG dependencies
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_DAGDependencies(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "build-pipeline")

	// install-deps depends on restore-cache and validate-workspace
	installTask := findTask(dag, "install-deps")
	assertDependencies(t, installTask, []string{"restore-cache", "validate-workspace"})

	// build-apps depends on install-deps
	buildTask := findTask(dag, "build-apps")
	assertDependencies(t, buildTask, []string{"install-deps"})

	// build-image depends on generate-dockerfile
	imageTask := findTask(dag, "build-image-api")
	assertDependencies(t, imageTask, []string{"generate-dockerfile"})

	// clone-repo has no dependencies
	cloneTask := findTask(dag, "clone-repo")
	if len(cloneTask.Dependencies) > 0 {
		t.Error("clone-repo should have no dependencies")
	}
}

// ---------------------------------------------------------------------------
// Language label
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_GoStrategy_LanguageLabel(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{{Name: "api", Type: "server", Port: 8080, Framework: "go"}}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	assertLabel(t, wf, "loopzero.io/language", "go")
}

func TestWorkflowBuilder_Build_PythonStrategy_LanguageLabel(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{{Name: "api", Type: "server", Port: 8000, Framework: "fastapi"}}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	assertLabel(t, wf, "loopzero.io/language", "python")
}

func TestWorkflowBuilder_Build_MixedLanguage_PerLanguageSteps(t *testing.T) {
	// REVIEW-AP-3: Mixed-language monorepos generate per-language build steps.
	// An app with Framework="" defaults to Node, and Framework="go" gets Go steps.
	// Both get independent pipelines in the same workflow.
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Framework: ""},      // → Node (default)
		{Name: "worker", Type: "worker", Port: 0, Framework: "go"},    // → Go
	}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Primary language label is first group (Node from default)
	assertLabel(t, wf, "loopzero.io/language", "node")

	// Both languages should have their own install-deps step
	dag := findDAG(t, wf, "build-pipeline")
	taskNames := make(map[string]bool)
	for _, task := range dag.Tasks {
		taskNames[task.Name] = true
	}

	// Per-language steps with suffix
	for _, expected := range []string{
		"install-deps-node", "install-deps-go",
		"build-apps-node", "build-apps-go",
		"generate-dockerfile-node", "generate-dockerfile-go",
		"build-image-api", "build-image-worker",
	} {
		if !taskNames[expected] {
			t.Errorf("missing DAG task for mixed-language: %s", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Multi-app monorepo
// ---------------------------------------------------------------------------

// C-02 fix: generate-manifests was removed from the workflow.
// Manifest generation now happens in the API (control plane).
// This test verifies multi-app workflows end at build-image step.
func TestWorkflowBuilder_Build_MultiApp_EndAtBuildImage(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: "services/api", Framework: "express"},
		{Name: "web", Type: "frontend", Port: 0, Path: "apps/web", Framework: "nextjs"},
		{Name: "worker", Type: "worker", Port: 0, Path: "services/worker", Framework: "node"},
	}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Verify build-image exists
	dag := findDAG(t, wf, "build-pipeline")
	if task := findTask(dag, "build-image-api"); task == nil {
		t.Fatal("build-image task not found in DAG")
	}

	// Verify generate-manifests does NOT exist
	if task := findTask(dag, "generate-manifests"); task != nil {
		t.Errorf("generate-manifests should not exist in workflow DAG — manifests are generated by API")
	}
}

// ---------------------------------------------------------------------------
// ToDict round-trip — ensures K8s compatibility
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_Build_ToDictRoundTrip(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	wf.Namespace = "argo"

	dict, err := wf.ToDict()
	if err != nil {
		t.Fatalf("ToDict() error: %v", err)
	}

	// Verify top-level K8s fields
	if dict["apiVersion"] != "argoproj.io/v1alpha1" {
		t.Errorf("apiVersion = %v, want argoproj.io/v1alpha1", dict["apiVersion"])
	}
	if dict["kind"] != "Workflow" {
		t.Errorf("kind = %v, want Workflow", dict["kind"])
	}

	// Verify metadata.namespace was set
	meta, ok := dict["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("dict missing metadata")
	}
	if meta["namespace"] != "argo" {
		t.Errorf("namespace = %v, want argo", meta["namespace"])
	}

	// Verify spec.entrypoint exists
	spec, ok := dict["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("dict missing spec")
	}
	if spec["entrypoint"] != "build-pipeline" {
		t.Errorf("entrypoint = %v, want build-pipeline", spec["entrypoint"])
	}
}

// ---------------------------------------------------------------------------
// appsJSON helper
// ---------------------------------------------------------------------------

func TestAppsJSON_EmptyApps(t *testing.T) {
	result := appsJSON(nil)
	if result != "[]" {
		t.Errorf("appsJSON(nil) = %q, want []", result)
	}
}

func TestAppsJSON_SingleApp(t *testing.T) {
	apps := []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: ".", Framework: "express"},
	}
	result := appsJSON(apps)
	assertStringContains(t, result, `"name":"api"`)
	assertStringContains(t, result, `"type":"server"`)
	assertStringContains(t, result, `"port":3000`)
}

func TestAppsJSON_WithSecrets(t *testing.T) {
	apps := []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Secrets: []string{"DB_URL", "API_KEY"}},
	}
	result := appsJSON(apps)
	assertStringContains(t, result, `"DB_URL"`)
	assertStringContains(t, result, `"API_KEY"`)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func findDAG(t *testing.T, wf *forge.Workflow, name string) *forge.DAG {
	t.Helper()
	for _, tmpl := range wf.Templates {
		if dag, ok := tmpl.(*forge.DAG); ok && dag.Name == name {
			return dag
		}
	}
	t.Fatalf("DAG template %q not found", name)
	return nil
}

func findTask(dag *forge.DAG, name string) *forge.Task {
	for _, task := range dag.Tasks {
		if task.Name == name {
			return task
		}
	}
	return nil
}

func findContainer(t *testing.T, wf *forge.Workflow, name string) *forge.Container {
	t.Helper()
	for _, tmpl := range wf.Templates {
		if c, ok := tmpl.(*forge.Container); ok && c.Name == name {
			return c
		}
	}
	return nil
}

func taskParamNames(task *forge.Task) map[string]bool {
	names := make(map[string]bool)
	for _, p := range task.Arguments {
		names[p.Name] = true
	}
	return names
}

func assertLabel(t *testing.T, wf *forge.Workflow, key, expected string) {
	t.Helper()
	val, ok := wf.Labels[key]
	if !ok || val != expected {
		t.Errorf("label %s = %q, want %q", key, val, expected)
	}
}

func assertDependencies(t *testing.T, task *forge.Task, expected []string) {
	t.Helper()
	depSet := make(map[string]bool)
	for _, d := range task.Dependencies {
		depSet[d] = true
	}
	for _, exp := range expected {
		if !depSet[exp] {
			t.Errorf("task %s missing dependency: %s", task.Name, exp)
		}
	}
}

func assertStringContains(t *testing.T, s, substr string) {
	t.Helper()
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("string does not contain %q:\n%.200s", substr, s)
}

// ---------------------------------------------------------------------------
// BuildKit — sidecar pattern, volumes, security, cache
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_BuildKit_HasBuildkitVolumes(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// BuildKit needs: buildkit-registry-config (Secret) + buildkit-socket (emptyDir)
	volumeNames := make(map[string]bool)
	for _, v := range wf.Volumes {
		switch vol := v.(type) {
		case *forge.SecretVolume:
			volumeNames[vol.Name] = true
		case *forge.EmptyDirVolume:
			volumeNames[vol.Name] = true
		}
	}

	if !volumeNames["buildkit-registry-config"] {
		t.Error("BuildKit workflow missing volume: buildkit-registry-config")
	}
	if !volumeNames["buildkit-socket"] {
		t.Error("BuildKit workflow missing volume: buildkit-socket")
	}
	// docker-config should NOT be present
	if volumeNames["docker-config"] {
		t.Error("workflow should NOT have docker-config volume")
	}
}

func TestWorkflowBuilder_BuildKit_HasSidecarPattern(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api template not found")
	}

	// Main container is buildctl
	if c.Image != "moby/buildkit:v0.18.2" {
		t.Errorf("buildctl image = %q, want moby/buildkit:v0.18.2", c.Image)
	}
	if len(c.Command) == 0 || c.Command[0] != "buildctl" {
		t.Errorf("buildctl command = %v, want [buildctl]", c.Command)
	}

	// Must have exactly one sidecar: buildkitd
	if len(c.Sidecars) != 1 {
		t.Fatalf("expected 1 sidecar, got %d", len(c.Sidecars))
	}
	sidecar := c.Sidecars[0]
	if sidecar.Name != "buildkitd-api" {
		t.Errorf("sidecar name = %q, want buildkitd-api", sidecar.Name)
	}
	if sidecar.Image != "moby/buildkit:v0.18.2-rootless" {
		t.Errorf("buildkitd image = %q, want moby/buildkit:v0.18.2-rootless", sidecar.Image)
	}
	if sidecar.Daemon == nil || !*sidecar.Daemon {
		t.Error("buildkitd sidecar must have Daemon=true")
	}
}

func TestWorkflowBuilder_BuildKit_SecurityContext_Rootless(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api template not found")
	}

	// buildctl (main container) security context
	if c.SecurityContext == nil {
		t.Fatal("buildctl missing SecurityContext")
	}
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 1000 {
		t.Errorf("buildctl RunAsUser = %v, want 1000", c.SecurityContext.RunAsUser)
	}
	if c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot {
		t.Error("buildctl RunAsNonRoot should be true")
	}

	// buildkitd (sidecar) security context
	sidecar := c.Sidecars[0]
	if sidecar.SecurityContext == nil {
		t.Fatal("buildkitd missing SecurityContext")
	}
	if sidecar.SecurityContext.RunAsUser == nil || *sidecar.SecurityContext.RunAsUser != 1000 {
		t.Errorf("buildkitd RunAsUser = %v, want 1000", sidecar.SecurityContext.RunAsUser)
	}
	if sidecar.SecurityContext.RunAsNonRoot == nil || !*sidecar.SecurityContext.RunAsNonRoot {
		t.Error("buildkitd RunAsNonRoot should be true")
	}
	// Must NOT have privileged: true
	if sidecar.SecurityContext.Privileged != nil && *sidecar.SecurityContext.Privileged {
		t.Error("buildkitd must NOT be privileged")
	}
}

func TestWorkflowBuilder_BuildKit_ResourceLimits(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api template not found")
	}

	// buildctl resources (lightweight client)
	if c.Resources == nil {
		t.Fatal("buildctl missing Resources")
	}
	if c.Resources.Requests.CPU != "100m" {
		t.Errorf("buildctl requests.cpu = %q, want 100m", c.Resources.Requests.CPU)
	}
	if c.Resources.Limits.Memory != "512Mi" {
		t.Errorf("buildctl limits.memory = %q, want 512Mi", c.Resources.Limits.Memory)
	}

	// buildkitd resources (heavy worker)
	sidecar := c.Sidecars[0]
	if sidecar.Resources == nil {
		t.Fatal("buildkitd missing Resources")
	}
	if sidecar.Resources.Requests.CPU != "500m" {
		t.Errorf("buildkitd requests.cpu = %q, want 500m", sidecar.Resources.Requests.CPU)
	}
	if sidecar.Resources.Limits.CPU != "2000m" {
		t.Errorf("buildkitd limits.cpu = %q, want 2000m", sidecar.Resources.Limits.CPU)
	}
	if sidecar.Resources.Limits.Memory != "4Gi" {
		t.Errorf("buildkitd limits.memory = %q, want 4Gi", sidecar.Resources.Limits.Memory)
	}
}

func TestWorkflowBuilder_BuildKit_BuildctlArgs_ContainsCacheFlags(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api template not found")
	}

	argsStr := strings.Join(c.Args, " ")

	// --addr points to correct Unix socket
	assertStringContains(t, argsStr, "unix:///run/user/1000/buildkitd/buildkitd.sock")

	// Frontend
	assertStringContains(t, argsStr, "--frontend dockerfile.v0")

	// Output with push
	assertStringContains(t, argsStr, "--output type=image,name=k3d-loopzero-registry:5000/acme/my-app/api:abc123,push=true")

	// Export cache — same repo as image, tag :buildcache. ignore-error=true makes cache failures non-fatal.
	assertStringContains(t, argsStr, "--export-cache type=registry,ref=k3d-loopzero-registry:5000/acme/my-app/api:buildcache,mode=max,ignore-error=true")

	// Import cache — same ref
	assertStringContains(t, argsStr, "--import-cache type=registry,ref=k3d-loopzero-registry:5000/acme/my-app/api:buildcache")

	// Metadata file for digest extraction
	assertStringContains(t, argsStr, "--metadata-file /workspace/metadata-api.json")
}

func TestWorkflowBuilder_BuildKit_BuildkitdArgs_RootlessFlags(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api template not found")
	}

	sidecar := c.Sidecars[0]
	argsStr := strings.Join(sidecar.Args, " ")

	// Must have --oci-worker-no-process-sandbox for rootless
	assertStringContains(t, argsStr, "--oci-worker-no-process-sandbox")

	// Must listen on correct socket
	assertStringContains(t, argsStr, "unix:///run/user/1000/buildkitd/buildkitd.sock")
}

func TestWorkflowBuilder_BuildKit_SharedSocketVolume(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c == nil {
		t.Fatal("build-image-api template not found")
	}

	// Both buildctl and buildkitd must mount buildkit-socket at /run/user/1000/buildkitd
	hasSocket := false
	for _, vm := range c.VolumeMounts {
		if ed, ok := vm.(*forge.EmptyDirVolume); ok && ed.Name == "buildkit-socket" {
			if ed.MountPath != "/run/user/1000/buildkitd" {
				t.Errorf("buildctl socket mount path = %q, want /run/user/1000/buildkitd", ed.MountPath)
			}
			hasSocket = true
		}
	}
	if !hasSocket {
		t.Error("buildctl missing buildkit-socket volume mount")
	}

	sidecar := c.Sidecars[0]
	hasSocket = false
	for _, vm := range sidecar.VolumeMounts {
		if ed, ok := vm.(*forge.EmptyDirVolume); ok && ed.Name == "buildkit-socket" {
			if ed.MountPath != "/run/user/1000/buildkitd" {
				t.Errorf("buildkitd socket mount path = %q, want /run/user/1000/buildkitd", ed.MountPath)
			}
			hasSocket = true
		}
	}
	if !hasSocket {
		t.Error("buildkitd missing buildkit-socket volume mount")
	}
}

func TestWorkflowBuilder_BuildKit_MultiApp_IndependentSidecars(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Framework: "express"},
		{Name: "worker", Type: "worker", Port: 0, Framework: "node"},
	}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Each app gets its own buildkit template with its own sidecar
	apiContainer := findContainer(t, wf, "build-image-api")
	workerContainer := findContainer(t, wf, "build-image-worker")

	if apiContainer == nil {
		t.Fatal("build-image-api not found")
	}
	if workerContainer == nil {
		t.Fatal("build-image-worker not found")
	}

	// Each has its own sidecar
	if len(apiContainer.Sidecars) != 1 || apiContainer.Sidecars[0].Name != "buildkitd-api" {
		t.Errorf("api sidecar name = %q, want buildkitd-api", apiContainer.Sidecars[0].Name)
	}
	if len(workerContainer.Sidecars) != 1 || workerContainer.Sidecars[0].Name != "buildkitd-worker" {
		t.Errorf("worker sidecar name = %q, want buildkitd-worker", workerContainer.Sidecars[0].Name)
	}
}

func TestWorkflowBuilder_BuildKit_MetadataFilePath(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []services.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Framework: "express"},
		{Name: "web", Type: "frontend", Port: 0, Framework: "nextjs"},
	}
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Each app gets its own metadata file path
	apiC := findContainer(t, wf, "build-image-api")
	webC := findContainer(t, wf, "build-image-web")

	apiArgs := strings.Join(apiC.Args, " ")
	webArgs := strings.Join(webC.Args, " ")

	assertStringContains(t, apiArgs, "--metadata-file /workspace/metadata-api.json")
	assertStringContains(t, webArgs, "--metadata-file /workspace/metadata-web.json")
}

// ---------------------------------------------------------------------------
// BuildKit DOKS compatibility — seccomp, apparmor, HOME, rootlesskit
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_BuildKit_SidecarNoCommand_UsesDefaultEntrypoint(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	sidecar := c.Sidecars[0]

	// Command MUST be nil/empty — the -rootless image entrypoint runs rootlesskit.
	// Setting Command: [buildkitd] bypasses RootlessKit and fails on managed K8s.
	if len(sidecar.Command) > 0 {
		t.Errorf("buildkitd sidecar Command = %v, want empty (use default entrypoint with RootlessKit)", sidecar.Command)
	}
	// Args should contain --oci-worker-no-process-sandbox
	argsStr := strings.Join(sidecar.Args, " ")
	assertStringContains(t, argsStr, "--oci-worker-no-process-sandbox")
}

func TestWorkflowBuilder_BuildKit_PodSpecPatch_SeccompUnconfined(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c.PodSpecPatch == "" {
		t.Fatal("PodSpecPatch should not be empty — needs seccompProfile: Unconfined")
	}
	assertStringContains(t, c.PodSpecPatch, "Unconfined")
	assertStringContains(t, c.PodSpecPatch, "buildkitd-api")
}

func TestWorkflowBuilder_BuildKit_AppArmorAnnotation(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	key := "container.apparmor.security.beta.kubernetes.io/buildkitd-api"
	if c.Annotations[key] != "unconfined" {
		t.Errorf("AppArmor annotation = %q, want unconfined", c.Annotations[key])
	}
}

func TestWorkflowBuilder_BuildKit_HomeEnvVar(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	hasHome := false
	for _, env := range c.Env {
		if e, ok := env.(forge.Env); ok && e.Name == "HOME" && e.Value == "/home/user" {
			hasHome = true
		}
	}
	if !hasHome {
		t.Error("buildctl missing HOME=/home/user env var")
	}
}

func TestWorkflowBuilder_BuildKit_MainContainerHasRegistryCredentials(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	hasRegistryMount := false
	for _, vm := range c.VolumeMounts {
		if sv, ok := vm.(*forge.SecretVolume); ok && sv.Name == "buildkit-registry-config" {
			if sv.MountPath != "/home/user/.docker" {
				t.Errorf("registry mount path = %q, want /home/user/.docker", sv.MountPath)
			}
			hasRegistryMount = true
		}
	}
	if !hasRegistryMount {
		t.Error("buildctl main container missing buildkit-registry-config volume mount")
	}
}

// ---------------------------------------------------------------------------
// --no-cache flag
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_BuildKit_NoCache_OmitsCacheArgs(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.NoCache = true
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	argsStr := strings.Join(c.Args, " ")

	if strings.Contains(argsStr, "--export-cache") {
		t.Error("--export-cache should NOT be present when NoCache=true")
	}
	if strings.Contains(argsStr, "--import-cache") {
		t.Error("--import-cache should NOT be present when NoCache=true")
	}
	// But --output and --metadata-file must still be present
	assertStringContains(t, argsStr, "--output type=image")
	assertStringContains(t, argsStr, "--metadata-file")
}

func TestWorkflowBuilder_BuildKit_WithCache_HasCacheArgs(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.NoCache = false // explicit default
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	argsStr := strings.Join(c.Args, " ")

	assertStringContains(t, argsStr, "--export-cache")
	assertStringContains(t, argsStr, "--import-cache")
}

// ---------------------------------------------------------------------------
// Digest parsing — BuildKit metadata file format
// ---------------------------------------------------------------------------

func TestParseBuildKitDigest_ValidJSON(t *testing.T) {
	input := `{"containerimage.digest":"sha256:abc123def456"}`
	digest, err := ParseBuildKitDigest([]byte(input))
	if err != nil {
		t.Fatalf("ParseBuildKitDigest() error: %v", err)
	}
	if digest != "sha256:abc123def456" {
		t.Errorf("digest = %q, want sha256:abc123def456", digest)
	}
}

func TestParseBuildKitDigest_MissingField(t *testing.T) {
	input := `{"some.other.field":"value"}`
	_, err := ParseBuildKitDigest([]byte(input))
	if err == nil {
		t.Error("expected error for missing containerimage.digest field")
	}
}

func TestParseBuildKitDigest_EmptyDigest(t *testing.T) {
	input := `{"containerimage.digest":""}`
	_, err := ParseBuildKitDigest([]byte(input))
	if err == nil {
		t.Error("expected error for empty digest")
	}
}

func TestParseBuildKitDigest_InvalidJSON(t *testing.T) {
	input := `not json at all`
	_, err := ParseBuildKitDigest([]byte(input))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseBuildKitDigest_MustStartWithSha256(t *testing.T) {
	input := `{"containerimage.digest":"md5:notasha"}`
	_, err := ParseBuildKitDigest([]byte(input))
	if err == nil {
		t.Error("expected error for non-sha256 digest")
	}
}

// ---------------------------------------------------------------------------
// dockerfileDir / dockerfileName helpers
// ---------------------------------------------------------------------------

func TestDockerfileDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/workspace/Dockerfile", "/workspace"},
		{"/workspace/Dockerfile.api", "/workspace"},
		{"Dockerfile", "."},
	}
	for _, tt := range tests {
		got := dockerfileDir(tt.input)
		if got != tt.want {
			t.Errorf("dockerfileDir(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDockerfileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/workspace/Dockerfile", "Dockerfile"},
		{"/workspace/Dockerfile.api", "Dockerfile.api"},
		{"Dockerfile", "Dockerfile"},
	}
	for _, tt := range tests {
		got := dockerfileName(tt.input)
		if got != tt.want {
			t.Errorf("dockerfileName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
