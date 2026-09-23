// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	forge "github.com/getloopscale/loopzero-forge"
)

func testInfraParams() InfraParams {
	return InfraParams{
		RegistryURL:      "k3d-loopzero-registry:5000",
		RegistryInsecure: true,
		BaseDomain:       "loopzero.local",
	}
}

// testSpec returns a WorkflowSpec wired with the default test fixtures.
// T1.3 (CR-2): Build() now requires spec.Plan; testSpec auto-populates a
// minimal Plan synthesized from the default Apps so existing tests that only
// override spec.Apps continue to exercise the Plan-driven emission path
// transparently. Tests that need a custom Plan should overwrite spec.Plan
// AFTER calling testSpec(), and may also overwrite spec.Apps independently.
func testSpec() deploycontracts.WorkflowSpec {
	apps := []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Runtime: "node"},
	}
	return deploycontracts.WorkflowSpec{
		BuildID:       "build-001",
		TenantID:      "acme",
		ProjectID:     "my-app",
		Environment:   "production",
		CommitSHA:     "abc123",
		CorrelationID: "corr-001",
		Apps:          apps,
		Plan:          planFromApps(apps),
	}
}

// planFromApps invokes the production deploycontracts.CreateBuildPlan pipeline on a
// list of AppSpecs so test inputs and production semantics stay aligned.
// Defaults match testSpec(): tenant=acme, project=my-app, sha=abc123,
// registry=k3d-loopzero-registry:5000 (testInfraParams). Use planFromAppsCustom
// for non-default tenant/project/sha/registry combinations.
func planFromApps(apps []deploycontracts.AppSpec) *deploycontracts.BuildPlan {
	return planFromAppsCustom(apps, "acme", "my-app", "abc123", "k3d-loopzero-registry:5000")
}

func planFromAppsCustom(apps []deploycontracts.AppSpec, tenantID, projectID, commitSHA, registryURL string) *deploycontracts.BuildPlan {
	units := make([]deploycontracts.AppUnit, 0, len(apps))
	for _, a := range apps {
		unit, err := deploycontracts.NormalizeApp(a.Name, a)
		if err != nil {
			// Tests pass valid specs; normalization should never fail here.
			panic("planFromApps: NormalizeApp failed for " + a.Name + ": " + err.Error())
		}
		units = append(units, unit)
	}
	plan := deploycontracts.CreateBuildPlan(units, tenantID, projectID, commitSHA, registryURL)
	return &plan
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

	// F-13 (CHG-2026-04-29-DOGFOOD T5.5) — PodGC.Strategy = OnPodCompletion
	// remove pods de step individuais (Success OU Error) após DeleteDelayDuration.
	// Sem isso, pods Failed acumulam até GC do Workflow inteiro (24h).
	if wf.PodGC == nil {
		t.Fatal("F-13 regression: PodGC must be set on Workflow spec")
	}
	if wf.PodGC.Strategy != "OnPodCompletion" {
		t.Errorf("F-13 regression: PodGC.Strategy = %q, want OnPodCompletion", wf.PodGC.Strategy)
	}
	if wf.PodGC.DeleteDelayDuration != "5m" {
		t.Errorf("F-13 regression: PodGC.DeleteDelayDuration = %q, want 5m (debug window)", wf.PodGC.DeleteDelayDuration)
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

// PVC multi-attach fix — formerly a per-consumer wrap, now handled
// upstream as of loopzero-forge v0.4.0 (issue getloopscale/loopzero-forge#11).
// The builder no longer sets Affinity directly; instead it leaves it
// nil and relies on Workflow.Build() to inject the canonical
// podAffinity term automatically because VolumeClaimTemplates is set.
// We therefore assert on the MATERIALIZED model after Build(), which
// is what Argo actually receives.
func TestWorkflowBuilder_Build_DefaultPodAffinityInjectedByForge(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Builder leaves Affinity nil — defaulting is the forge package's job.
	if wf.Affinity != nil {
		t.Fatalf("WorkflowBuilder must not set Affinity directly; expected nil so forge injects the default, got %+v", wf.Affinity)
	}

	// Materialize the workflow as forge sends it to Argo. The default
	// podAffinity must appear on Spec.Affinity because the workflow
	// declares a VolumeClaimTemplate (workspace PVC).
	m, err := wf.Build()
	if err != nil {
		t.Fatalf("forge Workflow.Build() error: %v", err)
	}
	if m.Spec.Affinity == nil || m.Spec.Affinity.PodAffinity == nil {
		t.Fatalf("forge must inject default PodAffinity for VCT-bearing workflows, got Affinity=%+v", m.Spec.Affinity)
	}
	terms := m.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 1 {
		t.Fatalf("expected 1 RequiredDuringScheduling term, got %d", len(terms))
	}
	term := terms[0]
	if term.TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("TopologyKey = %q, want kubernetes.io/hostname", term.TopologyKey)
	}
	if term.LabelSelector == nil || term.LabelSelector.MatchLabels["workflows.argoproj.io/workflow"] != "{{workflow.name}}" {
		t.Errorf("LabelSelector not matching expected workflow label: %+v", term.LabelSelector)
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
	task := findTask(t, dag, "save-cache")
	if task == nil {
		t.Error("Node strategy should include save-cache in finalize DAG")
	}
}

func TestWorkflowBuilder_Build_CloneTask_UsesCWTRef(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	dag := findDAG(t, wf, "build-pipeline")
	task := findTask(t, dag, "clone-repo")

	if task.TemplateRef == nil {
		t.Fatal("clone-repo missing templateRef")
	}
	if task.TemplateRef.Name != "loopzero-clone" {
		t.Errorf("templateRef.Name = %q, want loopzero-clone", task.TemplateRef.Name)
	}
	if !task.TemplateRef.ClusterScope {
		t.Error("templateRef.ClusterScope should be true")
	}

	// Clone passes tenant-id + project-id (for Gitea repo name) + build-id (for MinIO path).
	// Credentials come from K8s Secret (envFrom), not workflow params.
	paramNames := taskParamNames(task)
	for _, expected := range []string{"tenant-id", "project-id", "build-id"} {
		if !paramNames[expected] {
			t.Errorf("clone-repo missing parameter: %s", expected)
		}
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
	task := findTask(t, dag, "build-image-api")

	if task.TemplateRef != nil {
		t.Error("build-image-api should NOT use templateRef — inline template")
	}
	if task.Template != "build-image-api" {
		t.Errorf("build-image-api task.Template = %q, want %q", task.Template, "build-image-api")
	}

	// Verify inline container template exists and uses BuildKit rootless image
	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found — no nil check needed
	if c.Image != imageBuildKit {
		t.Errorf("build-image image = %q, want moby/buildkit:v0.18.2-rootless", c.Image)
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
	// findContainer now fatals if not found — no nil check needed

	// BuildKit uses --allow security.insecure for insecure registries
	argsStr := strings.Join(c.Args, " ")
	if !strings.Contains(argsStr, "--allow") {
		t.Error("expected --allow flag when RegistryInsecure=true")
	}
}

func TestBuildkitEntrypointScript_InsecureIncludesEntitlement(t *testing.T) {
	script := buildkitEntrypointScript("unix:///run/user/1000/buildkitd/buildkitd.sock", true, "k3d-loopzero-registry:5000", metadataFileFor("api"))
	if !strings.Contains(script, "--allow-insecure-entitlement security.insecure") {
		t.Error("buildkitd must include --allow-insecure-entitlement security.insecure when insecure=true")
	}
}

func TestBuildkitEntrypointScript_InsecureIncludesRegistryConfig(t *testing.T) {
	script := buildkitEntrypointScript("unix:///run/user/1000/buildkitd/buildkitd.sock", true, "k3d-loopzero-registry:5000", metadataFileFor("api"))
	if !strings.Contains(script, "k3d-loopzero-registry:5000") {
		t.Error("buildkitd entrypoint must reference the registry URL for insecure config")
	}
	if !strings.Contains(script, "http = true") {
		t.Error("buildkitd config must set http = true for insecure registry")
	}
	if !strings.Contains(script, "--config") {
		t.Error("buildkitd must use --config flag for insecure registry TOML")
	}
}

func TestBuildkitEntrypointScript_SecureExcludesEntitlement(t *testing.T) {
	script := buildkitEntrypointScript("unix:///run/user/1000/buildkitd/buildkitd.sock", false, "registry.example.com", metadataFileFor("api"))
	if strings.Contains(script, "--allow-insecure-entitlement") {
		t.Error("buildkitd must NOT include --allow-insecure-entitlement when insecure=false")
	}
	if strings.Contains(script, "http = true") {
		t.Error("buildkitd must NOT include insecure registry config when insecure=false")
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
	// findContainer now fatals if not found — no nil check needed

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
	task := findTask(t, dag, "restore-cache")
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
	task := findTask(t, dag, "save-cache")
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
		findContainer(t, wf, name) // fatals if not found
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
	installTask := findTask(t, dag, "install-deps")
	assertDependencies(t, installTask, []string{"restore-cache", "validate-workspace"})

	// build-apps depends on install-deps
	buildTask := findTask(t, dag, "build-apps")
	assertDependencies(t, buildTask, []string{"install-deps"})

	// build-image depends on generate-dockerfile
	imageTask := findTask(t, dag, "build-image-api")
	assertDependencies(t, imageTask, []string{"generate-dockerfile"})

	// clone-repo has no dependencies
	cloneTask := findTask(t, dag, "clone-repo")
	if len(cloneTask.Dependencies) > 0 {
		t.Error("clone-repo should have no dependencies")
	}
}

// ---------------------------------------------------------------------------
// Language label
// ---------------------------------------------------------------------------

// T2.1: TestWorkflowBuilder_Build_GoStrategy_LanguageLabel removed — Go is no
// longer a supported runtime (fixed node/python stack).

func TestWorkflowBuilder_Build_PythonStrategy_LanguageLabel(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 8000, Runtime: "python"}}
	spec.Plan = planFromApps(spec.Apps)
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	assertLabel(t, wf, "loopzero.io/language", "python")
}

func TestWorkflowBuilder_Build_MultiApp_EndAtBuildImage(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: "services/api", Runtime: "node"},
		// T3.3: frontend removed — web is a node server (the LoopZeroKit shape).
		{Name: "web", Type: "server", Port: 3001, Path: "apps/web", Runtime: "node"},
		{Name: "worker", Type: "worker", Port: 0, Path: "services/worker", Runtime: "node"},
	}
	spec.Plan = planFromApps(spec.Apps)
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Verify build-image exists
	dag := findDAG(t, wf, "build-pipeline")
	if task := findTask(t, dag, "build-image-api"); task == nil {
		t.Fatal("build-image task not found in DAG")
	}

	// Verify generate-manifests does NOT exist
	if task := findTaskOpt(dag, "generate-manifests"); task != nil {
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
	apps := []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: ".", Runtime: "node"},
	}
	result := appsJSON(apps)
	assertStringContains(t, result, `"name":"api"`)
	assertStringContains(t, result, `"type":"server"`)
	assertStringContains(t, result, `"port":3000`)
}

func TestAppsJSON_WithSecrets(t *testing.T) {
	apps := []deploycontracts.AppSpec{
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

func findTask(t *testing.T, dag *forge.DAG, name string) *forge.Task {
	t.Helper()
	for _, task := range dag.Tasks {
		if task.Name == name {
			return task
		}
	}
	t.Fatalf("task %q not found in DAG", name)
	return nil
}

// findTaskOpt returns nil if not found (no fatal). Use when asserting absence.
func findTaskOpt(dag *forge.DAG, name string) *forge.Task {
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
	t.Fatalf("container template %q not found", name)
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
// BuildKit — single container pattern (MEET-2026-03-26-007)
// ---------------------------------------------------------------------------

func TestWorkflowBuilder_BuildKit_HasRegistryVolume(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

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
		t.Error("workflow missing volume: buildkit-registry-config")
	}
	// Single container pattern: buildkit-socket volume is NOT needed
	if volumeNames["buildkit-socket"] {
		t.Error("workflow should NOT have buildkit-socket volume (single container pattern)")
	}
}

func TestWorkflowBuilder_BuildKit_SingleContainerNoSidecars(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found — no nil check needed

	// Single container: rootless image, no sidecars (MEET-2026-03-26-007)
	if c.Image != imageBuildKit {
		t.Errorf("image = %q, want moby/buildkit:v0.18.2-rootless", c.Image)
	}
	if len(c.Sidecars) != 0 {
		t.Errorf("expected 0 sidecars (single container pattern), got %d", len(c.Sidecars))
	}
	// Args[0] must be "buildctl" (exec'd by entrypoint script via "$@")
	if len(c.Args) == 0 || c.Args[0] != "buildctl" {
		t.Errorf("args[0] = %q, want buildctl", c.Args[0])
	}
}

func TestWorkflowBuilder_BuildKit_EntrypointScript(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found

	// Command: ["rootlesskit", "sh", "-c", <script>, "--"]
	if len(c.Command) != 5 {
		t.Fatalf("expected 5 command elements, got %d: %v", len(c.Command), c.Command)
	}
	if c.Command[0] != "rootlesskit" || c.Command[1] != "sh" || c.Command[2] != "-c" || c.Command[4] != "--" {
		t.Errorf("command = %v, want [rootlesskit sh -c <script> --]", c.Command)
	}

	// Entrypoint script must:
	// 1. Start buildkitd in background
	// 2. Wait for socket via buildctl debug workers
	// 3. Have a timeout
	// 4. Trap EXIT for cleanup
	// 5. Run buildctl "$@"
	script := c.Command[3]
	assertStringContains(t, script, "buildkitd")
	assertStringContains(t, script, "buildctl --addr")
	assertStringContains(t, script, "debug workers")
	assertStringContains(t, script, "buildkitd.sock")
	assertStringContains(t, script, "timeout=60")
	assertStringContains(t, script, "BUILD_EXIT=$?")
	assertStringContains(t, script, "exit $BUILD_EXIT")
	assertStringContains(t, script, `"$@"`)

	// Args must start with "buildctl" followed by buildctl flags
	if c.Args[0] != "buildctl" {
		t.Errorf("args[0] = %q, want buildctl", c.Args[0])
	}
	argsStr := strings.Join(c.Args[1:], " ")
	assertStringContains(t, argsStr, "--addr")
	assertStringContains(t, argsStr, "build")
	assertStringContains(t, argsStr, "--frontend")
}

func TestWorkflowBuilder_BuildKit_SecurityContext_Rootless(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found — no nil check needed

	// Single container security context (runs both buildkitd + buildctl)
	if c.SecurityContext == nil {
		t.Fatal("missing SecurityContext")
	}
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 1000 {
		t.Errorf("RunAsUser = %v, want 1000", c.SecurityContext.RunAsUser)
	}
	if c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot {
		t.Error("RunAsNonRoot should be true")
	}
	// Must NOT have privileged: true (rootless doesn't need it)
	if c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
		t.Error("must NOT be privileged")
	}
}

func TestWorkflowBuilder_BuildKit_ResourceLimits(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found — no nil check needed

	// Single container resources: must cover both buildkitd + buildctl
	if c.Resources == nil {
		t.Fatal("missing Resources")
	}
	if c.Resources.Requests.CPU != "500m" {
		t.Errorf("requests.cpu = %q, want 500m", c.Resources.Requests.CPU)
	}
	if c.Resources.Limits.CPU != "2000m" {
		t.Errorf("limits.cpu = %q, want 2000m", c.Resources.Limits.CPU)
	}
	if c.Resources.Limits.Memory != "4Gi" {
		t.Errorf("limits.memory = %q, want 4Gi", c.Resources.Limits.Memory)
	}
}

func TestWorkflowBuilder_BuildKit_BuildctlArgs_ContainsCacheFlags(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found — no nil check needed

	argsStr := strings.Join(c.Args, " ")

	// --addr points to correct Unix socket
	assertStringContains(t, argsStr, "unix:///run/user/1000/buildkitd/buildkitd.sock")

	// Frontend
	assertStringContains(t, argsStr, "--frontend dockerfile.v0")

	// Output with push
	assertStringContains(t, argsStr, "--output type=image,name=k3d-loopzero-registry:5000/acme/my-app-api:abc123,push=true")

	// Export cache — same repo as image, tag :buildcache. ignore-error=true makes cache failures non-fatal.
	assertStringContains(t, argsStr, "--export-cache type=registry,ref=k3d-loopzero-registry:5000/acme/my-app-api:buildcache,mode=max,ignore-error=true")

	// Import cache — same ref
	assertStringContains(t, argsStr, "--import-cache type=registry,ref=k3d-loopzero-registry:5000/acme/my-app-api:buildcache")

	// Metadata file for digest extraction
	assertStringContains(t, argsStr, "--metadata-file /workspace/metadata-api.json")
}

func TestWorkflowBuilder_BuildKit_EntrypointHasBuildkitdFlags(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	script := c.Command[3]

	// Script must start buildkitd with --oci-worker-no-process-sandbox for rootless
	assertStringContains(t, script, "--oci-worker-no-process-sandbox")
	// Must listen on correct socket
	assertStringContains(t, script, "unix:///run/user/1000/buildkitd/buildkitd.sock")
}

func TestWorkflowBuilder_BuildKit_NoSocketVolume(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// Single container: no buildkit-socket volume mount needed
	for _, vm := range c.VolumeMounts {
		if ed, ok := vm.(*forge.EmptyDirVolume); ok && ed.Name == "buildkit-socket" {
			t.Error("single container should NOT have buildkit-socket volume mount")
		}
	}
}

func TestWorkflowBuilder_BuildKit_MultiApp_IndependentContainers(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Apps = []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Runtime: "node"},
		{Name: "worker", Type: "worker", Port: 0, Runtime: "node"},
	}
	spec.Plan = planFromApps(spec.Apps)
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	apiContainer := findContainer(t, wf, "build-image-api")
	workerContainer := findContainer(t, wf, "build-image-worker")
	// findContainer fatals if not found — no nil checks needed

	// Each app: single container, no sidecars, rootless image
	if len(apiContainer.Sidecars) != 0 {
		t.Errorf("api should have 0 sidecars, got %d", len(apiContainer.Sidecars))
	}
	if len(workerContainer.Sidecars) != 0 {
		t.Errorf("worker should have 0 sidecars, got %d", len(workerContainer.Sidecars))
	}
	if apiContainer.Image != imageBuildKit {
		t.Errorf("api image = %q, want rootless", apiContainer.Image)
	}
	if workerContainer.Image != imageBuildKit {
		t.Errorf("worker image = %q, want rootless", workerContainer.Image)
	}
}

func TestWorkflowBuilder_BuildKit_MetadataFilePath(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	// T1.3 (CR-2): The Plan-driven path enforces NormalizeApp rules; static
	// frontends route to "deliver-artifact-{app}" (S3 upload), not
	// "build-image-{app}". The assertion here is about per-app metadata file
	// paths in build-image containers, so both apps must be container-delivered.
	spec.Apps = []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Runtime: "node"},
		{Name: "web", Type: "server", Port: 4000, Runtime: "node"},
	}
	spec.Plan = planFromApps(spec.Apps)
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

func TestWorkflowBuilder_BuildKit_RootlesskitEntrypoint(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// Single container uses rootlesskit as entrypoint (sets up user namespace)
	if c.Command[0] != "rootlesskit" {
		t.Errorf("command[0] = %q, want rootlesskit", c.Command[0])
	}
	// Entrypoint script starts buildkitd with rootless flags
	script := c.Command[3]
	assertStringContains(t, script, "--oci-worker-no-process-sandbox")
}

func TestWorkflowBuilder_BuildKit_SeccompUnconfined(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	if c.SecurityContext == nil {
		t.Fatal("SecurityContext should not be nil")
	}
	if c.SecurityContext.SeccompProfile == nil {
		t.Fatal("SeccompProfile should not be nil — required for rootless BuildKit")
	}
	if c.SecurityContext.SeccompProfile.Type != "Unconfined" {
		t.Errorf("SeccompProfile.Type = %q, want Unconfined", c.SecurityContext.SeccompProfile.Type)
	}
}

func TestWorkflowBuilder_BuildKit_AppArmorAnnotation(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	wf, err := builder.Build(testSpec())
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// Single container: AppArmor targets "main" (Argo's container name)
	key := "container.apparmor.security.beta.kubernetes.io/main"
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
// BuildPlan integration — buildkitTemplateFromPlan
// ---------------------------------------------------------------------------

func testBuildPlan() *deploycontracts.BuildPlan {
	return &deploycontracts.BuildPlan{
		Apps: []deploycontracts.AppBuildPlan{
			{
				AppName:          "api",
				ImageTag:         "reg.io/acme/my-app/api:abc123",
				DockerfilePath:   "/workspace/apps/api/Dockerfile",
				ContextPath:      "/workspace/apps/api",
				BuildArgs:        map[string]string{"NODE_ENV": "production", "COMMIT_SHA": "abc123", "APP_NAME": "api"},
				CacheRef:         "reg.io/acme/my-app/api:buildcache",
				LanguageStrategy: "node",
				Runtime:          "node",
				Build:            "buildpack",
				AppType:          "server",
				Port:             3000,
			},
		},
	}
}

func TestWorkflowBuilder_BuildPlan_UsesResolvedPaths(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Plan = testBuildPlan()
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	// findContainer now fatals if not found

	argsStr := strings.Join(c.Args, " ")

	// DockerfilePath from plan (not inferred)
	assertStringContains(t, argsStr, "--local dockerfile=/workspace/apps/api")
	assertStringContains(t, argsStr, "--opt filename=Dockerfile")

	// ContextPath from plan
	assertStringContains(t, argsStr, "--local context=/workspace/apps/api")

	// ImageTag from plan
	assertStringContains(t, argsStr, "--output type=image,name=reg.io/acme/my-app/api:abc123,push=true")

	// CacheRef from plan
	assertStringContains(t, argsStr, "--export-cache type=registry,ref=reg.io/acme/my-app/api:buildcache")

	// BuildArgs from plan
	assertStringContains(t, argsStr, "--opt build-arg:NODE_ENV=production")
	assertStringContains(t, argsStr, "--opt build-arg:COMMIT_SHA=abc123")
}

func TestWorkflowBuilder_BuildPlan_NoCache(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := testSpec()
	spec.Plan = testBuildPlan()
	spec.NoCache = true
	wf, err := builder.Build(spec)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	c := findContainer(t, wf, "build-image-api")
	argsStr := strings.Join(c.Args, " ")

	if strings.Contains(argsStr, "--export-cache") {
		t.Error("BuildPlan with NoCache should NOT have --export-cache")
	}
	if strings.Contains(argsStr, "--import-cache") {
		t.Error("BuildPlan with NoCache should NOT have --import-cache")
	}
	// BuildArgs should still be present
	assertStringContains(t, argsStr, "--opt build-arg:NODE_ENV=production")
}

// ---------------------------------------------------------------------------
// BuildPlan — multi-language monorepo (M5 Tarefa 5.5)
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

// T1.3 (CR-2 of MEET-2026-05-28-001) — Build() must reject WorkflowSpec when
// spec.Plan is nil. The production caller (build_orchestrator_submit.go) ALWAYS
// constructs a BuildPlan via CreateBuildPlan before submitting; a nil Plan
// indicates a programming error or an attempt to use the deleted legacy
// spec.Apps fallback. Fail fast at the contract boundary.
func TestWorkflowBuilder_Build_RejectsNilPlan(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := deploycontracts.WorkflowSpec{
		BuildID:     "build-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node"},
		},
		Plan: nil, // legacy path — must be rejected
	}

	_, err := builder.Build(spec)
	if err == nil {
		t.Fatal("expected Build() to error when spec.Plan is nil (legacy path deleted), got nil")
	}
	if !strings.Contains(err.Error(), "Plan") {
		t.Errorf("expected error mentioning Plan, got: %v", err)
	}
}

// EC-coverage: Plan present but Apps empty is also a contract violation.
func TestWorkflowBuilder_Build_RejectsEmptyPlanApps(t *testing.T) {
	builder := NewWorkflowBuilder(testInfraParams())
	spec := deploycontracts.WorkflowSpec{
		BuildID:     "build-001",
		TenantID:    "acme",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
		Plan:        &deploycontracts.BuildPlan{Apps: nil}, // Plan non-nil but no apps
	}

	_, err := builder.Build(spec)
	if err == nil {
		t.Fatal("expected Build() to error when spec.Plan.Apps is empty, got nil")
	}
}
