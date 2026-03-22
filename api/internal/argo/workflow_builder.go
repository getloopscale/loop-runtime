package argo

import (
	"encoding/json"
	"fmt"
	"strings"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"

	"github.com/loopzero/loopzero/api/internal/services"
)

// InfraParams holds infrastructure-level parameters that are injected into every workflow.
// These come from the API config and are not language-specific.
//
// C-01 fix: CallbackURL and CallbackSecret removed. The workflow no longer calls
// the API on completion. Instead, the API's BuildReconcileLoop polls Workflow CRDs.
//
// C-02 fix: ManifestGeneration removed from workflow. The API generates manifests
// after detecting workflow completion. The workflow only builds images.
//
// BLOCKER-2 fix: GiteaURL and GiteaToken removed. The clone CWT now reads
// credentials from K8s Secret (loopzero-gitea-credentials) via envFrom. No more
// plaintext tokens in Workflow CRs.
//
// BLOCKER-1 fix: Kaniko is now an inline template (not a CWT ref) so that
// --insecure and --skip-tls-verify flags are conditionally added based on
// RegistryInsecure. In production, Config.Validate() ensures RegistryInsecure is false.
//
// Note: MinIO credentials are injected via K8s Secret (envFrom) in the CWT,
// not as workflow parameters. See loopzero-minio-credentials secret in build cluster.
type InfraParams struct {
	RegistryURL      string
	RegistryInsecure bool   // true for local dev registries without TLS
	BaseDomain       string // Base domain for app hostnames (e.g., "loopzero.local")
}

// WorkflowBuilder composes Argo Workflow CRs from ClusterWorkflowTemplate refs
// and inline language-specific templates. It replaces the monolithic WorkflowTemplate
// approach where a single 954-line YAML handled everything.
//
// Architecture:
//   - Reusable steps (clone, cache, kaniko, manifests, notify) → CWT refs
//   - Language-specific steps (validate, install, build, scan, dockerfile) → inline templates
//   - The builder glues them together in a DAG with correct dependencies
type WorkflowBuilder struct {
	infraParams InfraParams
}

// NewWorkflowBuilder creates a WorkflowBuilder with the given infrastructure parameters.
func NewWorkflowBuilder(params InfraParams) *WorkflowBuilder {
	return &WorkflowBuilder{infraParams: params}
}

// languageGroup represents apps grouped by their resolved language strategy.
// REVIEW-AP-3: Mixed-language monorepos are now supported. Apps are grouped
// by framework, and each group gets independent validate/install/build/scan/
// dockerfile steps with a language suffix (e.g., install-deps-node, install-deps-go).
type languageGroup struct {
	Strategy LanguageStrategy
	Apps     []services.AppSpec
	Suffix   string // e.g., "-node", "-go", "-python", "" for single-language
}

// groupAppsByLanguage groups apps by their resolved language strategy.
// Single-language projects get no suffix for backwards compatibility.
func groupAppsByLanguage(apps []services.AppSpec) []languageGroup {
	groups := make(map[string]*languageGroup)
	var order []string // preserve insertion order

	for _, app := range apps {
		strategy := ResolveStrategy(app.Framework)
		name := strategy.Name()
		if _, exists := groups[name]; !exists {
			groups[name] = &languageGroup{Strategy: strategy}
			order = append(order, name)
		}
		groups[name].Apps = append(groups[name].Apps, app)
	}

	result := make([]languageGroup, 0, len(order))
	needsSuffix := len(order) > 1
	for _, name := range order {
		g := groups[name]
		if needsSuffix {
			g.Suffix = "-" + name
		}
		result = append(result, *g)
	}
	return result
}

// Build generates a complete Workflow CR using forge types for the given spec.
// The returned *forge.Workflow can be serialized via ToDict() for the K8s dynamic client.
func (b *WorkflowBuilder) Build(spec services.WorkflowSpec) (*forge.Workflow, error) {
	groups := groupAppsByLanguage(spec.Apps)

	// REVIEW-AP-3: Generate per-language inline templates for mixed-language support.
	// Each language group gets its own validate/install/build/scan/dockerfile steps.
	var inlineTemplates []forge.Templatable
	for _, g := range groups {
		inlineTemplates = append(inlineTemplates,
			withSuffix(g.Strategy.ValidateStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.InstallStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.BuildStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.ScanStep(), g.Suffix),
			withSuffix(g.Strategy.DockerfileStep(g.Apps), g.Suffix),
		)
	}
	// REVIEW-MOD-4: Per-app Kaniko templates (independent of language grouping)
	for _, app := range spec.Apps {
		inlineTemplates = append(inlineTemplates, b.kanikoTemplateForApp(spec, app))
	}

	// Build the main DAG
	mainDAG, err := b.buildMainDAG(spec, groups)
	if err != nil {
		return nil, fmt.Errorf("build main DAG: %w", err)
	}

	// Build the onExit DAG (cache save per-language)
	onExitDAG, err := b.buildOnExitDAG(spec, groups)
	if err != nil {
		return nil, fmt.Errorf("build onExit DAG: %w", err)
	}

	// All templates: main DAG + onExit + inline language templates
	templates := []forge.Templatable{mainDAG, onExitDAG}
	templates = append(templates, inlineTemplates...)

	// Primary language for label (first group)
	primaryLanguage := "custom"
	if len(groups) > 0 {
		primaryLanguage = groups[0].Strategy.Name()
	}

	wf := &forge.Workflow{
		GenerateName:       fmt.Sprintf("loopzero-build-%s-", spec.BuildID),
		Entrypoint:         "build-pipeline",
		OnExit:             "finalize",
		ServiceAccountName: "loopzero-build-sa",
		Parallelism:        ptr(4),
		// REVIEW-LEVE-2: Succeeded TTL increased from 1h to 4h for post-deploy debugging.
		// Failed TTL stays at 24h for incident investigation.
		TTLStrategy: &forge.TTLStrategy{
			SecondsAfterCompletion: ptr(14400),
			SecondsAfterSuccess:    ptr(14400),
			SecondsAfterFailure:    ptr(86400),
		},
		Labels: map[string]string{
			"loopzero.io/tenant":      spec.TenantID,
			"loopzero.io/project":     spec.ProjectID,
			"loopzero.io/environment": spec.Environment,
			"loopzero.io/build-id":    spec.BuildID,
			"loopzero.io/language":    primaryLanguage,
		},
		Templates: templates,
		// Workspace shared across all DAG steps via PVC (VolumeClaimTemplate).
		// EmptyDir doesn't persist between pods — each DAG task is a separate pod.
		// Argo dynamically provisions this PVC per-workflow and cleans it on completion.
		VolumeClaimTemplates: []forge.PVCVolume{
			{
				BaseVolume:  forge.BaseVolume{Name: "workspace"},
				Size:        "5Gi",
				AccessModes: []forge.AccessMode{forge.ReadWriteOnce},
			},
		},
	}

	return wf, nil
}

// buildMainDAG constructs the entrypoint DAG template.
//
// REVIEW-AP-3: The DAG now supports mixed-language monorepos. For each language
// group, a set of per-language steps is created (with suffix like -node, -go).
// The dependency chain within each group is: clone → restore-cache-{lang} →
// validate-{lang} → install-{lang} → build-{lang} → dockerfile-{lang} →
// [per-app kaniko tasks].
func (b *WorkflowBuilder) buildMainDAG(spec services.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
	dag := &forge.DAG{Name: "build-pipeline"}

	// Step 1: clone (shared across all languages — one source bundle)
	tasks := []*forge.Task{
		cwtTask("clone-repo", "loopzero-clone", "clone-repo", nil, map[string]string{
			"tenant-id": spec.TenantID,
			"build-id":  spec.BuildID,
		}),
	}

	// Per-language build pipeline steps
	var allDockerfileTasks []string
	for _, g := range groups {
		suffix := g.Suffix

		// restore-cache per language (different cache keys: node-modules vs go-modules)
		restoreName := "restore-cache" + suffix
		if g.Strategy.CacheKey() != "" {
			tasks = append(tasks, cwtTask(restoreName, "loopzero-cache-restore", "restore-cache",
				[]string{"clone-repo"}, map[string]string{
					"tenant-id":  spec.TenantID,
					"project-id": spec.ProjectID,
					"cache-key":  g.Strategy.CacheKey(),
				}))
		} else {
			// No cache for this language — create a no-op dependency anchor
			restoreName = "clone-repo"
		}

		validateName := "validate-workspace" + suffix
		tasks = append(tasks, inlineTask(validateName, validateName, []string{"clone-repo"}))

		scanName := "security-scan" + suffix
		tasks = append(tasks, inlineTask(scanName, scanName, []string{restoreName}))

		installName := "install-deps" + suffix
		tasks = append(tasks, inlineTask(installName, installName, []string{restoreName, validateName}))

		buildName := "build-apps" + suffix
		tasks = append(tasks, inlineTask(buildName, buildName, []string{installName}))

		dockerfileName := "generate-dockerfile" + suffix
		tasks = append(tasks, inlineTask(dockerfileName, dockerfileName, []string{buildName}))
		allDockerfileTasks = append(allDockerfileTasks, dockerfileName)
	}

	// Per-app Kaniko — depends on its language group's dockerfile step
	for _, app := range spec.Apps {
		appStrategy := ResolveStrategy(app.Framework)
		// Find the suffix for this app's language
		dockerfileDep := "generate-dockerfile"
		for _, g := range groups {
			if g.Strategy.Name() == appStrategy.Name() {
				dockerfileDep = "generate-dockerfile" + g.Suffix
				break
			}
		}
		taskName := fmt.Sprintf("build-image-%s", app.Name)
		tasks = append(tasks, inlineTask(taskName, taskName, []string{dockerfileDep}))
	}

	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// buildOnExitDAG constructs the finalize DAG (always runs, even on failure).
// REVIEW-AP-3: saves cache per-language (each language has different cache dirs).
func (b *WorkflowBuilder) buildOnExitDAG(spec services.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
	dag := &forge.DAG{Name: "finalize"}

	var tasks []*forge.Task

	for _, g := range groups {
		if g.Strategy.CacheKey() != "" {
			taskName := "save-cache" + g.Suffix
			tasks = append(tasks, cwtTask(taskName, "loopzero-cache-save", "save-cache", nil, map[string]string{
				"tenant-id":  spec.TenantID,
				"project-id": spec.ProjectID,
				"cache-key":  g.Strategy.CacheKey(),
				"cache-dirs": strings.Join(g.Strategy.CacheDirs(), ","),
			}))
		}
	}

	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// kanikoTemplateForApp generates a Kaniko container template for a single app.
//
// REVIEW-MOD-4: Each app gets its own Kaniko build with independent image tag:
//   - Image: {registry}/{tenant}/{project}/{app}:{sha}
//   - Dockerfile: /workspace/Dockerfile.{app} (with fallback to /workspace/Dockerfile for single-app)
//   - Context: /workspace (shared — Kaniko reads only the Dockerfile)
//
// This replaces the single kanikoTemplate that produced one image for all apps.
func (b *WorkflowBuilder) kanikoTemplateForApp(spec services.WorkflowSpec, app services.AppSpec) *forge.Container {
	// Per-app image destination: {registry}/{tenant}/{project}/{app}:{sha}
	destination := fmt.Sprintf("%s/%s/%s/%s:%s",
		b.infraParams.RegistryURL, spec.TenantID, spec.ProjectID, app.Name, spec.CommitSHA)
	cacheRepo := fmt.Sprintf("%s/%s/cache", b.infraParams.RegistryURL, spec.TenantID)

	// Per-app Dockerfile: Dockerfile.{app} with fallback to Dockerfile for single-app projects.
	// The generate-dockerfile step creates Dockerfile.{app} for each app.
	dockerfile := "/workspace/Dockerfile"
	if len(spec.Apps) > 1 {
		dockerfile = fmt.Sprintf("/workspace/Dockerfile.%s", app.Name)
	}

	args := []string{
		fmt.Sprintf("--dockerfile=%s", dockerfile),
		fmt.Sprintf("--context=%s", "/workspace"),
		fmt.Sprintf("--destination=%s", destination),
		"--cache=true",
		fmt.Sprintf("--cache-repo=%s", cacheRepo),
		"--snapshot-mode=redo",
		"--compressed-caching=false",
		fmt.Sprintf("--digest-file=/workspace/image-digest-%s.txt", app.Name),
	}

	if b.infraParams.RegistryInsecure {
		args = append(args, "--insecure", "--skip-tls-verify")
	}

	return &forge.Container{
		Name:    fmt.Sprintf("build-image-%s", app.Name),
		Image:   "gcr.io/kaniko-project/executor:v1.23.2",
		Command: []string{"/kaniko/executor"},
		Args:    args,
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "workspace", MountPath: "/workspace"}},
			// Docker registry credentials for Kaniko push.
			// Secret "docker-config" must contain key "config.json" with Docker auth config.
			// Created by TenantController in each build namespace.
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: "docker-config", MountPath: "/kaniko/.docker"}, SecretName: "docker-config"},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "500m", Memory: "1Gi"},
			Limits:   forge.ResourceList{CPU: "2000m", Memory: "4Gi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsNonRoot: ptr(false),
			RunAsUser:    ptr(int64(0)), // Kaniko requires root to build images
		},
		RetryStrategy: retryStrategy(3, "10s", "2", "300s"),
	}
}

// ---------------------------------------------------------------------------
// DAG task helpers
// ---------------------------------------------------------------------------

// cwtTask creates a DAG task that references a ClusterWorkflowTemplate.
func cwtTask(taskName, cwtName, templateName string, deps []string, params map[string]string) *forge.Task {
	return &forge.Task{
		Name: taskName,
		TemplateRef: &forge.TemplateRef{
			Name:         cwtName,
			Template:     templateName,
			ClusterScope: true,
		},
		Dependencies: deps,
		Arguments:    paramsToForge(params),
	}
}

// inlineTask creates a DAG task that references an inline template by name.
func inlineTask(taskName, templateName string, deps []string) *forge.Task {
	return &forge.Task{
		Name:         taskName,
		Template:     templateName,
		Dependencies: deps,
	}
}

// paramsToForge converts a string map to forge Parameters for task arguments.
func paramsToForge(params map[string]string) []forge.Parameter {
	if len(params) == 0 {
		return nil
	}
	result := make([]forge.Parameter, 0, len(params))
	for k, v := range params {
		val := v
		result = append(result, forge.Parameter{Name: k, Value: &val})
	}
	return result
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// withSuffix appends a language suffix to a container template name.
// For single-language projects (suffix=""), the name is unchanged.
// For mixed-language (suffix="-node"), name becomes e.g. "install-deps-node".
func withSuffix(c *forge.Container, suffix string) *forge.Container {
	if suffix != "" {
		c.Name = c.Name + suffix
	}
	return c
}

// appsJSON serializes apps to JSON for passing as a workflow parameter.
// Uses json.Marshal for correct escaping of special characters (LEVE-1 fix).
func appsJSON(apps []services.AppSpec) string {
	if len(apps) == 0 {
		return "[]"
	}
	data, err := json.Marshal(apps)
	if err != nil {
		// Fallback to empty array — should never happen with AppSpec types
		return "[]"
	}
	return string(data)
}
