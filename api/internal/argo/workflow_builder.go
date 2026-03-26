// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

// groupAppsByPlan groups AppBuildPlan entries by their pre-resolved LanguageStrategy.
// Unlike groupAppsByLanguage, this does NOT call ResolveStrategy — the strategy was
// already resolved during CreateBuildPlan. This is the BuildPlan-native grouping.
func groupAppsByPlan(plans []services.AppBuildPlan) []languageGroup {
	groups := make(map[string]*languageGroup)
	var order []string

	for _, p := range plans {
		stratName := p.LanguageStrategy
		if _, exists := groups[stratName]; !exists {
			// Resolve the strategy object from the pre-resolved name.
			// This is safe because LanguageStrategy was set by resolveLanguageStrategy
			// in CreateBuildPlan — the mapping is consistent.
			strategy := ResolveStrategy(stratName)
			groups[stratName] = &languageGroup{Strategy: strategy}
			order = append(order, stratName)
		}
		// Convert AppBuildPlan back to AppSpec for language step generation.
		// Language steps (validate, install, build, dockerfile) need app paths and frameworks.
		groups[stratName].Apps = append(groups[stratName].Apps, services.AppSpec{
			Name:      p.AppName,
			Type:      p.AppType,
			Port:      p.Port,
			Path:      "", // Language steps use workspace-relative paths, not absolute
			Framework: stratName,
			Build:     p.Build,
			Secrets:   p.Secrets,
		})
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
	// v2: When a BuildPlan is present, group by pre-resolved strategy (no inline inference).
	// Legacy: group by framework using ResolveStrategy.
	var groups []languageGroup
	if spec.Plan != nil && len(spec.Plan.Apps) > 0 {
		groups = groupAppsByPlan(spec.Plan.Apps)
	} else {
		groups = groupAppsByLanguage(spec.Apps)
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
	// Per-app BuildKit templates (independent of language grouping).
	// Each app gets its own buildkitd sidecar + buildctl pair for isolated builds.
	if spec.Plan != nil {
		// v2: BuildPlan provides resolved paths, image tags, and build args.
		for _, appPlan := range spec.Plan.Apps {
			inlineTemplates = append(inlineTemplates, b.buildkitTemplateFromPlan(spec, appPlan))
		}
	} else {
		// Legacy: infer from AppSpec (backward compat with existing tests)
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
		onExitName = "finalize"
	}
	templates = append(templates, inlineTemplates...)

	// Primary language for label (first group)
	primaryLanguage := "custom"
	if len(groups) > 0 {
		primaryLanguage = groups[0].Strategy.Name()
	}

	wf := &forge.Workflow{
		GenerateName:       fmt.Sprintf("loopzero-build-%s-", spec.BuildID),
		Entrypoint:         "build-pipeline",
		OnExit:             onExitName,
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
		// Workspace uses a dynamically provisioned PVC shared across all DAG steps.
		// Each step in the DAG runs as a separate pod — emptyDir does NOT share across pods.
		// VolumeClaimTemplates create the PVC when the Workflow starts; it is shared
		// across all pods and cleaned up when the Workflow is garbage collected (TTL).
		VolumeClaimTemplates: []forge.PVCVolume{
			{BaseVolume: forge.BaseVolume{Name: "workspace"}, Size: "5Gi"},
		},
		Volumes: b.buildVolumes(),
		// FSGroup ensures the PVC volume is writable by non-root user 1000.
		// Without this, the PVC is mounted as root-owned and cp/mkdir fail.
		SecurityContext: &model.PodSecurityContext{
			FSGroup:    ptr(int64(1000)),
			RunAsUser:  ptr(int64(1000)),
			RunAsGroup: ptr(int64(1000)),
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
	dag := &forge.DAG{Name: "build-pipeline"}

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

	// Per-app BuildKit — depends on its language group's dockerfile step.
	// v2: When BuildPlan is present, use pre-resolved LanguageStrategy for dependency lookup.
	if spec.Plan != nil && len(spec.Plan.Apps) > 0 {
		for _, appPlan := range spec.Plan.Apps {
			dockerfileDep := "generate-dockerfile"
			for _, g := range groups {
				if g.Strategy.Name() == appPlan.LanguageStrategy {
					dockerfileDep = "generate-dockerfile" + g.Suffix
					break
				}
			}
			taskName := fmt.Sprintf("build-image-%s", appPlan.AppName)
			tasks = append(tasks, inlineTask(taskName, taskName, []string{dockerfileDep}))
		}
	} else {
		for _, app := range spec.Apps {
			appStrategy := ResolveStrategy(app.Framework)
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

	if len(tasks) == 0 {
		return nil, nil // No cache to save — skip onExit DAG entirely
	}
	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// buildVolumes returns the workflow-level volumes for the BuildKit build engine.
// BuildKit needs: buildkit-registry-config Secret + buildkit-socket emptyDir for daemon↔client communication.
func (b *WorkflowBuilder) buildVolumes() []forge.VolumeBuilder {
	return []forge.VolumeBuilder{
		// Registry credentials for BuildKit push — mounted at /home/user/.docker/ in buildkitd.
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: "buildkit-registry-config"}, SecretName: "buildkit-registry-config"},
		// Unix socket shared between buildkitd (daemon) and buildctl (client).
		&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "buildkit-socket"}},
	}
}

// buildkitTemplateForApp generates a BuildKit container template with sidecar pattern for a single app.
//
// Architecture: two containers sharing a Unix socket via emptyDir volume:
//   - buildkitd (sidecar): rootless daemon listening on Unix socket, started via RootlessKit entrypoint
//   - buildctl (main): client that submits the build and waits for completion
//
// The buildkitd sidecar is marked as Daemon=true so Argo keeps it alive until
// the main container (buildctl) exits. Argo then kills the sidecar automatically.
//
// Image digest is extracted from --metadata-file (JSON: {"containerimage.digest":"sha256:..."}).
//
// Cache: --import-cache and --export-cache use registry-based OCI cache (no MinIO).
//
// Security: The -rootless image uses RootlessKit to create a user namespace. This requires:
//   - seccompProfile: Unconfined (allows user namespace creation)
//   - AppArmor: unconfined (allows mount operations inside user namespace)
//   - NO privileged: true required
//
// These are set via PodSpecPatch (seccomp) and Annotations (AppArmor) because
// the forge SDK SecurityContext type doesn't include seccompProfile.
func (b *WorkflowBuilder) buildkitTemplateForApp(spec services.WorkflowSpec, app services.AppSpec) *forge.Container {
	destination := fmt.Sprintf("%s/%s/%s/%s:%s",
		b.infraParams.RegistryURL, spec.TenantID, spec.ProjectID, app.Name, spec.CommitSHA)
	// Cache ref uses the SAME repo as the image with tag :buildcache.
	// Using a separate cache/ path (e.g., {tenant}/cache/{app}) conflicts with existing
	// repos on DO Container Registry — subrepos under existing repos are forbidden.
	cacheRef := fmt.Sprintf("%s/%s/%s/%s:buildcache",
		b.infraParams.RegistryURL, spec.TenantID, spec.ProjectID, app.Name)

	// Per-app Dockerfile: Dockerfile.{app} for multi-app, Dockerfile for single-app.
	dockerfile := "/workspace/Dockerfile"
	if len(spec.Apps) > 1 {
		dockerfile = fmt.Sprintf("/workspace/Dockerfile.%s", app.Name)
	}

	metadataFile := fmt.Sprintf("/workspace/metadata-%s.json", app.Name)

	// buildctl args — the main container that drives the build.
	// Each flag and its value MUST be separate array elements. buildctl does NOT
	// do shell-style word splitting — "–local dockerfile=/x" is one arg, not two.
	buildctlArgs := []string{
		"build",
		"--frontend", "dockerfile.v0",
		"--local", "context=/workspace",
		"--local", fmt.Sprintf("dockerfile=%s", dockerfileDir(dockerfile)),
		"--opt", fmt.Sprintf("filename=%s", dockerfileName(dockerfile)),
		"--output", fmt.Sprintf("type=image,name=%s,push=true", destination),
	}
	// OCI registry layer cache: --import-cache pulls cached layers before build,
	// --export-cache pushes layers after build. Omitted when NoCache is set.
	if !spec.NoCache {
		buildctlArgs = append(buildctlArgs,
			"--export-cache", fmt.Sprintf("type=registry,ref=%s,mode=max,ignore-error=true", cacheRef),
			"--import-cache", fmt.Sprintf("type=registry,ref=%s", cacheRef),
		)
	}
	buildctlArgs = append(buildctlArgs, "--metadata-file", metadataFile)

	// buildkitd socket path — rootless user 1000
	socketPath := "unix:///run/user/1000/buildkitd/buildkitd.sock"

	// Prepend --addr to buildctl args
	buildctlArgs = append([]string{"--addr", socketPath}, buildctlArgs...)

	// buildkitd daemon args — passed to the default entrypoint (rootlesskit wrapper)
	// DO NOT set Command on the sidecar — the -rootless image entrypoint runs
	// "rootlesskit buildkitd" which sets up the user namespace before starting buildkitd.
	buildkitdArgs := []string{
		"--oci-worker-no-process-sandbox",
		"--addr", socketPath,
	}

	if b.infraParams.RegistryInsecure {
		// Allow pushing to HTTP registries (local dev only — blocked in production by Config.Validate)
		buildkitdArgs = append(buildkitdArgs,
			"--oci-worker-snapshotter", "native",
		)
		buildctlArgs = append(buildctlArgs,
			"--allow", "security.insecure",
		)
	}

	sidecarName := fmt.Sprintf("buildkitd-%s", app.Name)

	// buildkitd sidecar container — uses default entrypoint (rootlesskit wrapper).
	// Command is intentionally NOT set so the image's ENTRYPOINT runs RootlessKit.
	daemonTrue := true
	buildkitdSidecar := forge.UserContainer{
		Name:  sidecarName,
		Image: "moby/buildkit:v0.18.2-rootless",
		Args:  buildkitdArgs,
		SecurityContext: &model.SecurityContext{
			RunAsUser:    ptr(int64(1000)),
			RunAsGroup:   ptr(int64(1000)),
			RunAsNonRoot: ptr(true),
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "500m", Memory: "512Mi"},
			Limits:   forge.ResourceList{CPU: "2000m", Memory: "4Gi"},
		},
		VolumeMounts: []forge.VolumeBuilder{
			// Socket dir shared with buildctl
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{
				Name: "buildkit-socket", MountPath: "/run/user/1000/buildkitd",
			}},
			// Registry credentials for authenticated push
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: "buildkit-registry-config", MountPath: "/home/user/.docker", ReadOnly: true,
			}, SecretName: "buildkit-registry-config"},
		},
		Daemon: &daemonTrue,
	}

	// PodSpecPatch adds seccompProfile: Unconfined to the buildkitd sidecar.
	// Required for rootless BuildKit to create user namespaces on managed Kubernetes (DOKS, EKS, GKE).
	// The forge SDK SecurityContext doesn't include seccompProfile, so we use a JSON patch.
	podSpecPatch := fmt.Sprintf(`{"containers":[{"name":"%s","securityContext":{"seccompProfile":{"type":"Unconfined"}}}]}`, sidecarName)

	return &forge.Container{
		Name:    fmt.Sprintf("build-image-%s", app.Name),
		Image:   "moby/buildkit:v0.18.2",
		Command: []string{"buildctl"},
		Args:    buildctlArgs,
		// HOME must be set so buildctl can find ~/.docker/config.json for registry auth.
		// Without this, buildctl tries to create /.docker which fails as non-root.
		Env: []forge.EnvBuilder{
			forge.Env{Name: "HOME", Value: "/home/user"},
		},
		VolumeMounts: []forge.VolumeBuilder{
			// Workspace with source code and Dockerfiles
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "workspace", MountPath: "/workspace"}},
			// Socket dir shared with buildkitd
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{
				Name: "buildkit-socket", MountPath: "/run/user/1000/buildkitd",
			}},
			// Registry credentials — buildctl needs these to resolve base image metadata
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{
				Name: "buildkit-registry-config", MountPath: "/home/user/.docker", ReadOnly: true,
			}, SecretName: "buildkit-registry-config"},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "128Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "512Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser:    ptr(int64(1000)),
			RunAsGroup:   ptr(int64(1000)),
			RunAsNonRoot: ptr(true),
		},
		// AppArmor unconfined annotation — required for rootless BuildKit mount operations
		// inside user namespaces on managed Kubernetes with AppArmor enabled.
		Annotations: map[string]string{
			fmt.Sprintf("container.apparmor.security.beta.kubernetes.io/%s", sidecarName): "unconfined",
		},
		PodSpecPatch:  podSpecPatch,
		Sidecars:      []forge.UserContainer{buildkitdSidecar},
		RetryStrategy: retryStrategy(3, "10s", "2", "300s"),
	}
}

// buildkitTemplateFromPlan generates a BuildKit template using resolved fields from AppBuildPlan.
// Unlike buildkitTemplateForApp, this function doesn't infer anything — all paths, tags,
// and args come from the plan (created by CreateBuildPlan).
func (b *WorkflowBuilder) buildkitTemplateFromPlan(spec services.WorkflowSpec, plan services.AppBuildPlan) *forge.Container {
	// buildctl args from the resolved plan.
	// Each flag and its value MUST be separate array elements — no shell word splitting.
	buildctlArgs := []string{
		"build",
		"--frontend", "dockerfile.v0",
		"--local", fmt.Sprintf("context=%s", plan.ContextPath),
		"--local", fmt.Sprintf("dockerfile=%s", dockerfileDir(plan.DockerfilePath)),
		"--opt", fmt.Sprintf("filename=%s", dockerfileName(plan.DockerfilePath)),
		"--output", fmt.Sprintf("type=image,name=%s,push=true", plan.ImageTag),
	}

	// OCI registry layer cache (omitted when NoCache)
	if !spec.NoCache {
		buildctlArgs = append(buildctlArgs,
			"--export-cache", fmt.Sprintf("type=registry,ref=%s,mode=max,ignore-error=true", plan.CacheRef),
			"--import-cache", fmt.Sprintf("type=registry,ref=%s", plan.CacheRef),
		)
	}

	// Build args from the plan (user env + system vars)
	for key, val := range plan.BuildArgs {
		buildctlArgs = append(buildctlArgs, "--opt", fmt.Sprintf("build-arg:%s=%s", key, val))
	}

	metadataFile := fmt.Sprintf("/workspace/metadata-%s.json", plan.AppName)
	buildctlArgs = append(buildctlArgs, "--metadata-file", metadataFile)

	// Socket path and prepend --addr
	socketPath := "unix:///run/user/1000/buildkitd/buildkitd.sock"
	buildctlArgs = append([]string{"--addr", socketPath}, buildctlArgs...)

	// buildkitd args
	buildkitdArgs := []string{"--oci-worker-no-process-sandbox", "--addr", socketPath}
	if b.infraParams.RegistryInsecure {
		buildkitdArgs = append(buildkitdArgs, "--oci-worker-snapshotter", "native")
		buildctlArgs = append(buildctlArgs, "--allow", "security.insecure")
	}

	sidecarName := fmt.Sprintf("buildkitd-%s", plan.AppName)
	daemonTrue := true

	buildkitdSidecar := forge.UserContainer{
		Name:  sidecarName,
		Image: "moby/buildkit:v0.18.2-rootless",
		Args:  buildkitdArgs,
		SecurityContext: &model.SecurityContext{
			RunAsUser: ptr(int64(1000)), RunAsGroup: ptr(int64(1000)), RunAsNonRoot: ptr(true),
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "500m", Memory: "512Mi"},
			Limits:   forge.ResourceList{CPU: "2000m", Memory: "4Gi"},
		},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "buildkit-socket", MountPath: "/run/user/1000/buildkitd"}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: "buildkit-registry-config", MountPath: "/home/user/.docker", ReadOnly: true}, SecretName: "buildkit-registry-config"},
		},
		Daemon: &daemonTrue,
	}

	podSpecPatch := fmt.Sprintf(`{"containers":[{"name":"%s","securityContext":{"seccompProfile":{"type":"Unconfined"}}}]}`, sidecarName)

	return &forge.Container{
		Name:    fmt.Sprintf("build-image-%s", plan.AppName),
		Image:   "moby/buildkit:v0.18.2",
		Command: []string{"buildctl"},
		Args:    buildctlArgs,
		Env:     []forge.EnvBuilder{forge.Env{Name: "HOME", Value: "/home/user"}},
		VolumeMounts: []forge.VolumeBuilder{
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "workspace", MountPath: "/workspace"}},
			&forge.EmptyDirVolume{BaseVolume: forge.BaseVolume{Name: "buildkit-socket", MountPath: "/run/user/1000/buildkitd"}},
			&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: "buildkit-registry-config", MountPath: "/home/user/.docker", ReadOnly: true}, SecretName: "buildkit-registry-config"},
		},
		Resources: &forge.ResourceRequirements{
			Requests: forge.ResourceList{CPU: "100m", Memory: "128Mi"},
			Limits:   forge.ResourceList{CPU: "500m", Memory: "512Mi"},
		},
		SecurityContext: &model.SecurityContext{
			RunAsUser: ptr(int64(1000)), RunAsGroup: ptr(int64(1000)), RunAsNonRoot: ptr(true),
		},
		Annotations: map[string]string{
			fmt.Sprintf("container.apparmor.security.beta.kubernetes.io/%s", sidecarName): "unconfined",
		},
		PodSpecPatch:  podSpecPatch,
		Sidecars:      []forge.UserContainer{buildkitdSidecar},
		RetryStrategy: retryStrategy(3, "10s", "2", "300s"),
	}
}

// dockerfileDir returns the directory containing the Dockerfile.
// For "/workspace/Dockerfile.api" → "/workspace".
func dockerfileDir(dockerfilePath string) string {
	for i := len(dockerfilePath) - 1; i >= 0; i-- {
		if dockerfilePath[i] == '/' {
			return dockerfilePath[:i]
		}
	}
	return "."
}

// dockerfileName returns the filename portion of the Dockerfile path.
// For "/workspace/Dockerfile.api" → "Dockerfile.api".
func dockerfileName(dockerfilePath string) string {
	for i := len(dockerfilePath) - 1; i >= 0; i-- {
		if dockerfilePath[i] == '/' {
			return dockerfilePath[i+1:]
		}
	}
	return dockerfilePath
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

// ParseBuildKitDigest extracts the container image digest from BuildKit's --metadata-file output.
// BuildKit writes JSON like: {"containerimage.digest":"sha256:abc123..."}.
// The digest MUST start with "sha256:" — any other format is treated as a build error.
// Parsing failure must fail the build (deterministic digest extraction is critical for GitOps).
func ParseBuildKitDigest(data []byte) (string, error) {
	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse buildkit metadata: invalid JSON: %w", err)
	}
	digestVal, ok := metadata["containerimage.digest"]
	if !ok {
		return "", fmt.Errorf("parse buildkit metadata: missing field 'containerimage.digest'")
	}
	digest, ok := digestVal.(string)
	if !ok || digest == "" {
		return "", fmt.Errorf("parse buildkit metadata: 'containerimage.digest' is empty or not a string")
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("parse buildkit metadata: digest %q does not start with 'sha256:'", digest)
	}
	return digest, nil
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
