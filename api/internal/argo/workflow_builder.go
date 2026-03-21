package argo

import (
	"encoding/json"
	"fmt"
	"strings"

	forge "github.com/getloopscale/loopzero-forge"

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
// Note: MinIO credentials are passed as workflow parameters because the CWT
// templates (loopzero-cache-restore, loopzero-cache-save) require them as inputs.
// The CWTs run in the build cluster and need MinIO connectivity to store/restore
// build caches. The credentials are visible in the Workflow CR but are
// service-account-scoped (not user credentials).
type InfraParams struct {
	GiteaURL         string
	GiteaToken       string
	RegistryURL      string
	RegistryInsecure bool   // true for local dev registries without TLS
	BaseDomain       string // Base domain for app hostnames (e.g., "loopzero.local")
	// MinIO credentials are injected via K8s Secret (envFrom) in the CWT,
	// not as workflow parameters. See loopzero-minio-credentials secret in build cluster.
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

// Build generates a complete Workflow CR using forge types for the given spec.
// The returned *forge.Workflow can be serialized via ToDict() for the K8s dynamic client.
func (b *WorkflowBuilder) Build(spec services.WorkflowSpec) (*forge.Workflow, error) {
	strategy := resolveStrategyFromApps(spec.Apps)

	// Collect inline templates from the language strategy
	inlineTemplates := []forge.Templatable{
		strategy.ValidateStep(spec.Apps),
		strategy.InstallStep(spec.Apps),
		strategy.BuildStep(spec.Apps),
		strategy.ScanStep(),
		strategy.DockerfileStep(spec.Apps),
	}

	// Build the main DAG
	mainDAG, err := b.buildMainDAG(spec, strategy)
	if err != nil {
		return nil, fmt.Errorf("build main DAG: %w", err)
	}

	// Build the onExit DAG
	onExitDAG, err := b.buildOnExitDAG(spec, strategy)
	if err != nil {
		return nil, fmt.Errorf("build onExit DAG: %w", err)
	}

	// All templates: main DAG + onExit + inline language templates
	templates := []forge.Templatable{mainDAG, onExitDAG}
	templates = append(templates, inlineTemplates...)

	wf := &forge.Workflow{
		GenerateName:       fmt.Sprintf("loopzero-build-%s-", spec.BuildID),
		Entrypoint:         "build-pipeline",
		OnExit:             "finalize",
		ServiceAccountName: "loopzero-build-sa",
		Parallelism:        ptr(4),
		TTLStrategy: &forge.TTLStrategy{
			SecondsAfterCompletion: ptr(3600),
			SecondsAfterSuccess:    ptr(3600),
			SecondsAfterFailure:    ptr(21600),
		},
		Labels: map[string]string{
			"loopzero.io/tenant":      spec.TenantID,
			"loopzero.io/project":     spec.ProjectID,
			"loopzero.io/environment": spec.Environment,
			"loopzero.io/build-id":    spec.BuildID,
			"loopzero.io/language":    strategy.Name(),
		},
		Templates: templates,
		VolumeClaimTemplates: []forge.PVCVolume{
			{BaseVolume: forge.BaseVolume{Name: "workspace"}, Size: "2Gi"},
			{BaseVolume: forge.BaseVolume{Name: "manifests"}, Size: "256Mi"},
		},
	}

	return wf, nil
}

// buildMainDAG constructs the entrypoint DAG template.
// It mixes CWT refs (clone, cache-restore, kaniko) with inline templates
// (validate, install, build, scan, dockerfile).
func (b *WorkflowBuilder) buildMainDAG(spec services.WorkflowSpec, strategy LanguageStrategy) (*forge.DAG, error) {
	dag := &forge.DAG{Name: "build-pipeline"}

	tasks := []*forge.Task{
		// Step 1: clone (CWT ref)
		cwtTask("clone-repo", "loopzero-clone", "clone-repo", nil, map[string]string{
			"gitea-url":   b.infraParams.GiteaURL,
			"gitea-token": b.infraParams.GiteaToken,
			"tenant-id":   spec.TenantID,
			"project-id":  spec.ProjectID,
		}),

		// Step 1: restore-cache (CWT ref) — depends on clone
		// MinIO credentials injected via envFrom (K8s Secret loopzero-minio-credentials)
		cwtTask("restore-cache", "loopzero-cache-restore", "restore-cache", []string{"clone-repo"}, map[string]string{
			"tenant-id":  spec.TenantID,
			"project-id": spec.ProjectID,
			"cache-key":  strategy.CacheKey(),
		}),

		// Step 1.5: validate workspace (inline) — depends on clone
		inlineTask("validate-workspace", "validate-workspace", []string{"clone-repo"}),

		// Step 2: security scan (inline) — depends on cache restore
		inlineTask("security-scan", "security-scan", []string{"restore-cache"}),

		// Step 2: install deps (inline) — depends on cache restore + validate
		inlineTask("install-deps", "install-deps", []string{"restore-cache", "validate-workspace"}),

		// Step 3: build apps (inline) — depends on install
		inlineTask("build-apps", "build-apps", []string{"install-deps"}),

		// Step 3.5: generate dockerfile (inline) — depends on build
		inlineTask("generate-dockerfile", "generate-dockerfile", []string{"build-apps"}),

		// Step 4: kaniko (CWT ref) — depends on dockerfile
		// C-02 fix: This is now the LAST step. Manifest generation and gitops-push
		// have been moved to the API (control plane). The workflow only builds images.
		cwtTask("build-image", "loopzero-kaniko", "build-image", []string{"generate-dockerfile"}, map[string]string{
			"registry-url":      b.infraParams.RegistryURL,
			"tenant-id":         spec.TenantID,
			"project-id":        spec.ProjectID,
			"commit-sha":        spec.CommitSHA,
			"dockerfile-path":   "/workspace/Dockerfile",
			"context-path":      "/workspace",
			"registry-insecure": fmt.Sprintf("%v", b.infraParams.RegistryInsecure),
		}),
	}

	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// buildOnExitDAG constructs the finalize DAG (always runs, even on failure).
func (b *WorkflowBuilder) buildOnExitDAG(spec services.WorkflowSpec, strategy LanguageStrategy) (*forge.DAG, error) {
	dag := &forge.DAG{Name: "finalize"}

	var tasks []*forge.Task

	// save-cache (CWT ref) — only if language has cache dirs
	// MinIO credentials injected via envFrom (K8s Secret loopzero-minio-credentials)
	if strategy.CacheKey() != "" {
		tasks = append(tasks, cwtTask("save-cache", "loopzero-cache-save", "save-cache", nil, map[string]string{
			"tenant-id":  spec.TenantID,
			"project-id": spec.ProjectID,
			"cache-key":  strategy.CacheKey(),
			"cache-dirs": strings.Join(strategy.CacheDirs(), ","),
		}))
	}

	// summary (CWT ref)
	// C-01 fix: notify-api step removed. The API's BuildReconcileLoop polls
	// Workflow CRDs to detect completion. No HTTP callback from build to API.
	tasks = append(tasks, cwtTask("emit-summary", "loopzero-summary", "emit-summary", nil, map[string]string{
		"tenant-id":       spec.TenantID,
		"project-id":      spec.ProjectID,
		"environment":     spec.Environment,
		"commit-sha":      spec.CommitSHA,
		"build-id":        spec.BuildID,
		"workflow-status":  "{{workflow.status}}",
	}))

	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
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

// resolveStrategyFromApps picks a strategy from the first non-empty framework.
// If no app framework is populated, we default to Node for backwards compatibility.
// Mixed-language monorepos are still a documented limitation.
func resolveStrategyFromApps(apps []services.AppSpec) LanguageStrategy {
	if len(apps) == 0 {
		return ResolveStrategy("")
	}
	for _, app := range apps {
		if app.Framework != "" {
			return ResolveStrategy(app.Framework)
		}
	}
	return ResolveStrategy("")
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
