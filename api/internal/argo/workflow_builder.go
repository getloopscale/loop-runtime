package argo

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/loopzero/loopzero/api/internal/services"
)

// InfraParams holds infrastructure-level parameters that are injected into every workflow.
// These come from the API config and are not language-specific.
type InfraParams struct {
	CallbackURL      string
	CallbackSecret   string
	GiteaURL         string
	GiteaToken       string
	MinIOEndpoint    string
	MinIOAccessKey   string
	MinIOSecretKey   string
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

// Build generates a complete Workflow CR (as unstructured map) for the given spec.
// The returned map can be passed directly to the K8s dynamic client for creation.
func (b *WorkflowBuilder) Build(spec services.WorkflowSpec) map[string]interface{} {
	strategy := resolveStrategyFromApps(spec.Apps)

	// Collect inline templates from the language strategy
	inlineTemplates := []interface{}{
		strategy.ValidateStep(spec.Apps),
		strategy.InstallStep(spec.Apps),
		strategy.BuildStep(spec.Apps),
		strategy.ScanStep(),
		strategy.DockerfileStep(spec.Apps),
	}

	// Build the main DAG
	dagTemplate := b.buildMainDAG(spec, strategy)

	// Build the onExit DAG
	onExitTemplate := b.buildOnExitDAG(spec, strategy)

	// All templates: main DAG + onExit + inline language templates
	templates := []interface{}{dagTemplate, onExitTemplate}
	templates = append(templates, inlineTemplates...)

	workflow := map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata": map[string]interface{}{
			"generateName": fmt.Sprintf("loopzero-build-%s-", spec.BuildID),
			"labels": map[string]interface{}{
				"loopzero.io/tenant":      spec.TenantID,
				"loopzero.io/project":     spec.ProjectID,
				"loopzero.io/environment": spec.Environment,
				"loopzero.io/build-id":    spec.BuildID,
				"loopzero.io/language":    strategy.Name(),
			},
		},
		"spec": map[string]interface{}{
			"entrypoint":         "build-pipeline",
			"onExit":             "finalize",
			"serviceAccountName": "loopzero-build-sa",
			"parallelism":        int64(4),
			"ttlStrategy": map[string]interface{}{
				"secondsAfterCompletion": int64(3600),
				"secondsAfterSuccess":    int64(3600),
				"secondsAfterFailure":    int64(21600),
			},
			"templates": templates,
			"volumeClaimTemplates": []interface{}{
				volumeClaim("workspace", "2Gi"),
				volumeClaim("manifests", "256Mi"),
			},
		},
	}

	return workflow
}

// buildMainDAG constructs the entrypoint DAG template.
// It mixes CWT refs (clone, cache-restore, kaniko, manifests) with inline templates
// (validate, install, build, scan, dockerfile).
func (b *WorkflowBuilder) buildMainDAG(spec services.WorkflowSpec, strategy LanguageStrategy) map[string]interface{} {
	tasks := []interface{}{
		// Step 1: clone (CWT ref)
		cwtTask("clone-repo", "loopzero-clone", "clone-repo", nil, map[string]string{
			"gitea-url":  b.infraParams.GiteaURL,
			"gitea-token": b.infraParams.GiteaToken,
			"tenant-id":  spec.TenantID,
			"project-id": spec.ProjectID,
		}),

		// Step 1: restore-cache (CWT ref) — depends on clone
		cwtTask("restore-cache", "loopzero-cache-restore", "restore-cache", []string{"clone-repo"}, map[string]string{
			"tenant-id":        spec.TenantID,
			"project-id":       spec.ProjectID,
			"minio-endpoint":   b.infraParams.MinIOEndpoint,
			"minio-access-key": b.infraParams.MinIOAccessKey,
			"minio-secret-key": b.infraParams.MinIOSecretKey,
			"cache-key":        strategy.CacheKey(),
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
		cwtTask("build-image", "loopzero-kaniko", "build-image", []string{"generate-dockerfile"}, map[string]string{
			"registry-url":      b.infraParams.RegistryURL,
			"tenant-id":         spec.TenantID,
			"project-id":        spec.ProjectID,
			"commit-sha":        spec.CommitSHA,
			"dockerfile-path":   "/workspace/Dockerfile",
			"context-path":      "/workspace",
			"registry-insecure": fmt.Sprintf("%v", b.infraParams.RegistryInsecure),
		}),

		// Step 5: generate manifests (CWT ref) — depends on kaniko
		cwtTask("generate-manifests", "loopzero-manifests-generate", "generate-manifests", []string{"build-image"}, map[string]string{
			"registry-url": b.infraParams.RegistryURL,
			"tenant-id":    spec.TenantID,
			"project-id":   spec.ProjectID,
			"environment":  spec.Environment,
			"commit-sha":   spec.CommitSHA,
			"build-id":     spec.BuildID,
			"apps":         appsJSON(spec.Apps),
			"base-domain":  b.infraParams.BaseDomain,
		}),

		// Step 5: validate manifests (CWT ref) — depends on generate
		cwtTask("validate-manifests", "loopzero-manifests-validate", "validate-manifests", []string{"generate-manifests"}, map[string]string{
			"tenant-id":   spec.TenantID,
			"project-id":  spec.ProjectID,
			"environment": spec.Environment,
		}),
	}

	return map[string]interface{}{
		"name": "build-pipeline",
		"dag": map[string]interface{}{
			"tasks": tasks,
		},
	}
}

// buildOnExitDAG constructs the finalize DAG (always runs, even on failure).
func (b *WorkflowBuilder) buildOnExitDAG(spec services.WorkflowSpec, strategy LanguageStrategy) map[string]interface{} {
	tasks := []interface{}{}

	// save-cache (CWT ref) — only if language has cache dirs
	if strategy.CacheKey() != "" {
		tasks = append(tasks, cwtTask("save-cache", "loopzero-cache-save", "save-cache", nil, map[string]string{
			"tenant-id":        spec.TenantID,
			"project-id":       spec.ProjectID,
			"minio-endpoint":   b.infraParams.MinIOEndpoint,
			"minio-access-key": b.infraParams.MinIOAccessKey,
			"minio-secret-key": b.infraParams.MinIOSecretKey,
			"cache-key":        strategy.CacheKey(),
			"cache-dirs":       strings.Join(strategy.CacheDirs(), ","),
		}))
	}

	// summary (CWT ref)
	tasks = append(tasks, cwtTask("emit-summary", "loopzero-summary", "emit-summary", nil, map[string]string{
		"tenant-id":       spec.TenantID,
		"project-id":      spec.ProjectID,
		"environment":     spec.Environment,
		"commit-sha":      spec.CommitSHA,
		"build-id":        spec.BuildID,
		"workflow-status":  "{{workflow.status}}",
	}))

	// notify-api (CWT ref)
	tasks = append(tasks, cwtTask("notify-api", "loopzero-notify", "notify-api", nil, map[string]string{
		"api-callback-url": b.infraParams.CallbackURL,
		"callback-secret":  b.infraParams.CallbackSecret,
		"registry-url":     b.infraParams.RegistryURL,
		"tenant-id":        spec.TenantID,
		"project-id":       spec.ProjectID,
		"environment":      spec.Environment,
		"commit-sha":       spec.CommitSHA,
		"build-id":         spec.BuildID,
		"correlation-id":   spec.CorrelationID,
		"workflow-status":  "{{workflow.status}}",
	}))

	return map[string]interface{}{
		"name": "finalize",
		"dag": map[string]interface{}{
			"tasks": tasks,
		},
	}
}

// ---------------------------------------------------------------------------
// DAG task helpers
// ---------------------------------------------------------------------------

// cwtTask creates a DAG task that references a ClusterWorkflowTemplate.
func cwtTask(taskName, cwtName, templateName string, deps []string, params map[string]string) map[string]interface{} {
	arguments := make([]interface{}, 0, len(params))
	for k, v := range params {
		arguments = append(arguments, map[string]interface{}{
			"name":  k,
			"value": v,
		})
	}

	task := map[string]interface{}{
		"name": taskName,
		"templateRef": map[string]interface{}{
			"name":        cwtName,
			"template":    templateName,
			"clusterScope": true,
		},
		"arguments": map[string]interface{}{
			"parameters": arguments,
		},
	}

	if len(deps) > 0 {
		task["dependencies"] = toInterfaceSlice(deps)
	}

	return task
}

// inlineTask creates a DAG task that references an inline template by name.
func inlineTask(taskName, templateName string, deps []string) map[string]interface{} {
	task := map[string]interface{}{
		"name":     taskName,
		"template": templateName,
	}

	if len(deps) > 0 {
		task["dependencies"] = toInterfaceSlice(deps)
	}

	return task
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func volumeClaim(name, size string) map[string]interface{} {
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": name,
		},
		"spec": map[string]interface{}{
			"accessModes": []interface{}{"ReadWriteOnce"},
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"storage": size,
				},
			},
		},
	}
}

func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

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
