// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
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
// Build engine: BuildKit with sidecar pattern (buildkitd + buildctl).
// Registry credentials mounted from buildkit-registry-config Secret.
// RegistryInsecure is conditionally applied; Config.Validate() ensures false in production.
//
// Note: MinIO credentials are injected via K8s Secret (envFrom) in the CWT,
// not as workflow parameters. See loopzero-minio-credentials secret in build cluster.
type InfraParams struct {
	RegistryURL      string
	RegistryInsecure bool   // true for local dev registries without TLS
	BaseDomain       string // Base domain for app hostnames (e.g., "loopzero.local")
}

// WorkflowBuilder composes Argo Workflow CRs from ClusterWorkflowTemplate refs
// and inline language-specific templates.
//
// Architecture:
//   - Reusable steps (clone, cache) → CWT refs
//   - Language-specific steps (validate, install, build, scan, dockerfile) → inline templates
//   - Per-app BuildKit templates (buildkitd sidecar + buildctl) → inline templates
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

// groupApps groups items by language strategy using a generic extraction function.
// The extractor returns (strategyName, AppSpec) for each item. Single-language
// projects get no suffix; multi-language get "-node", "-go", etc.
func groupApps[T any](items []T, extract func(T) (string, services.AppSpec)) []languageGroup {
	groups := make(map[string]*languageGroup)
	var order []string

	for _, item := range items {
		stratName, app := extract(item)
		if _, exists := groups[stratName]; !exists {
			groups[stratName] = &languageGroup{Strategy: ResolveStrategy(stratName)}
			order = append(order, stratName)
		}
		groups[stratName].Apps = append(groups[stratName].Apps, app)
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
	var groups []languageGroup
	if spec.Plan != nil && len(spec.Plan.Apps) > 0 {
		groups = groupApps(spec.Plan.Apps, func(p services.AppBuildPlan) (string, services.AppSpec) {
			return p.LanguageStrategy, services.AppSpec{
				Name: p.AppName, Type: p.AppType, Port: p.Port,
				Path: p.AppPath, Framework: p.LanguageStrategy,
				Build: p.Build, Secrets: p.Secrets,
			}
		})
	} else {
		groups = groupApps(spec.Apps, func(app services.AppSpec) (string, services.AppSpec) {
			return ResolveStrategy(app.Framework).Name(), app
		})
	}

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
	// Per-app delivery templates: BuildKit for containers, S3 upload for static.
	if spec.Plan != nil {
		for _, appPlan := range spec.Plan.Apps {
			if appPlan.DeliveryModel == "static" {
				inlineTemplates = append(inlineTemplates, b.uploadAssetsTemplate(spec, appPlan))
			} else {
				inlineTemplates = append(inlineTemplates, b.buildkitTemplateFromPlan(spec, appPlan))
			}
		}
	} else {
		for _, app := range spec.Apps {
			inlineTemplates = append(inlineTemplates, b.buildkitTemplateForApp(spec, app))
		}
	}

	// Build the main DAG
	mainDAG, err := b.buildMainDAG(spec, groups)
	if err != nil {
		return nil, fmt.Errorf("build main DAG: %w", err)
	}

	// Build the onExit DAG (cache save per-language).
	// Only add onExit if there are cache tasks — Argo requires ≥1 task per DAG.
	onExitDAG, err := b.buildOnExitDAG(spec, groups)
	if err != nil {
		return nil, fmt.Errorf("build onExit DAG: %w", err)
	}

	templates := []forge.Templatable{mainDAG}
	onExitName := ""
	if onExitDAG != nil {
		templates = append(templates, onExitDAG)
		onExitName = onExitDAGName
	}
	templates = append(templates, inlineTemplates...)

	// Primary language for label (first group)
	primaryLanguage := "custom"
	if len(groups) > 0 {
		primaryLanguage = groups[0].Strategy.Name()
	}

	wf := &forge.Workflow{
		GenerateName:       fmt.Sprintf("loopzero-build-%s-", spec.BuildID),
		Entrypoint:         entrypointName,
		OnExit:             onExitName,
		ServiceAccountName: serviceAccountName,
		Parallelism:        ptr(workflowParallelism),
		TTLStrategy: &forge.TTLStrategy{
			SecondsAfterCompletion: ptr(ttlAfterCompletion),
			SecondsAfterSuccess:    ptr(ttlAfterSuccess),
			SecondsAfterFailure:    ptr(ttlAfterFailure),
		},
		Labels: map[string]string{
			labelTenant:      spec.TenantID,
			labelProject:     spec.ProjectID,
			labelEnvironment: spec.Environment,
			labelBuildID:     spec.BuildID,
			labelLanguage:    primaryLanguage,
		},
		Templates: templates,
		// Workspace uses a dynamically provisioned PVC shared across all DAG steps.
		// Each step in the DAG runs as a separate pod — emptyDir does NOT share across pods.
		// VolumeClaimTemplates create the PVC when the Workflow starts; it is shared
		// across all pods and cleaned up when the Workflow is garbage collected (TTL).
		VolumeClaimTemplates: []forge.PVCVolume{
			{BaseVolume: forge.BaseVolume{Name: volumeWorkspace}, Size: workspacePVCSize},
		},
		Volumes: b.buildVolumes(),
		// FSGroup ensures the PVC volume is writable by non-root user 1000.
		// Without this, the PVC is mounted as root-owned and cp/mkdir fail.
		SecurityContext: &model.PodSecurityContext{
			FSGroup:    ptr(rootlessUID),
			RunAsUser:  ptr(rootlessUID),
			RunAsGroup: ptr(rootlessUID),
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
// [per-app BuildKit tasks].
func (b *WorkflowBuilder) buildMainDAG(spec services.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
	dag := &forge.DAG{Name: entrypointName}

	// Step 1: clone (shared across all languages — one source bundle)
	tasks := []*forge.Task{
		// The CWT downloads source from: {tenant-id}/source/{build-id}.bundle
		// We pass CommitSHA as "build-id" because the source upload stores by commit SHA.
		cwtTask("clone-repo", "loopzero-clone", "clone-repo", nil, map[string]string{
			"tenant-id": spec.TenantID,
			"build-id":  spec.CommitSHA,
		}),
	}

	// Per-language build pipeline steps
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
	}

	// Per-app delivery — BuildKit for containers, upload for static.
	// Depends on its language group's dockerfile step (containers) or build step (static).
	if spec.Plan != nil && len(spec.Plan.Apps) > 0 {
		for _, appPlan := range spec.Plan.Apps {
			suffix := suffixForStrategy(groups, appPlan.LanguageStrategy)
			if appPlan.DeliveryModel == "static" {
				taskName := fmt.Sprintf("deliver-artifact-%s", appPlan.AppName)
				tasks = append(tasks, inlineTask(taskName, taskName, []string{"build-apps" + suffix}))
			} else {
				taskName := fmt.Sprintf("build-image-%s", appPlan.AppName)
				tasks = append(tasks, inlineTask(taskName, taskName, []string{"generate-dockerfile" + suffix}))
			}
		}
	} else {
		for _, app := range spec.Apps {
			suffix := suffixForStrategy(groups, ResolveStrategy(app.Framework).Name())
			taskName := fmt.Sprintf("build-image-%s", app.Name)
			tasks = append(tasks, inlineTask(taskName, taskName, []string{"generate-dockerfile" + suffix}))
		}
	}

	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// buildOnExitDAG constructs the finalize DAG (always runs, even on failure).
// REVIEW-AP-3: saves cache per-language (each language has different cache dirs).
func (b *WorkflowBuilder) buildOnExitDAG(spec services.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
	dag := &forge.DAG{Name: onExitDAGName}

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

	if len(tasks) == 0 {
		return nil, nil // No cache to save — skip onExit DAG entirely
	}
	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// buildVolumes returns the workflow-level volumes for the BuildKit build engine.
// Single container pattern: only registry credentials are needed as a workflow-level volume.
// The buildkitd socket is internal to each build-image container (no cross-container sharing).
func (b *WorkflowBuilder) buildVolumes() []forge.VolumeBuilder {
	return []forge.VolumeBuilder{
		// Registry credentials for BuildKit push — mounted at /home/user/.docker/ in each build container.
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: secretRegistryConfig}, SecretName: secretRegistryConfig},
	}
}


