// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"strings"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"

	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/naming"
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
	RegistryURL       string
	RegistryInsecure  bool   // true for local dev registries without TLS
	BaseDomain        string // Base domain for app hostnames (e.g., "loopzero.local")
	HarborRegistryURL string // MEET-048: secondary registry for dual-push. Empty = disabled.
	// CosignKeyless toggles the signing mode used by cosignSignTemplate.
	// false (default): key-based, COSIGN_KEY mounted from `cosign-signing-key` Secret.
	// true:            keyless via Fulcio + Rekor; SA token mounted as OIDC bearer.
	// Plan T4.1 / MEET-2026-05-03-063 (MEET-B Track-2): keyless requires the build
	// cluster to issue OIDC tokens accepted by Fulcio (private Fulcio OR Spire OR
	// configured public Fulcio). Until that infrastructure exists, leave false.
	// The CODE supports both modes; runtime activation is the deferred part.
	CosignKeyless bool
	// CosignFulcioURL overrides the default Fulcio URL when CosignKeyless=true.
	// Empty value = use sigstore.dev public Fulcio (https://fulcio.sigstore.dev).
	// Set this to a private Fulcio endpoint when running in air-gapped clusters.
	CosignFulcioURL string
	// CosignRekorURL overrides the default Rekor URL when CosignKeyless=true.
	// Empty value = use sigstore.dev public Rekor (https://rekor.sigstore.dev).
	CosignRekorURL string
	// CosignAttestEnabled toggles SLSA L3 SBOM-to-image attestation linking
	// via `cosign attest --predicate <sbom>` step. T2.1 of plan
	// 2026-05-27-tenant-build-pipeline-gaps-closure.
	// false (default): no attest step; signature + SBOM remain unlinked
	//                  (functional but NOT SLSA L3 compliant)
	// true:           attest step inserted with deps on BOTH sign + sbom
	//                  (v1.1 EC-NEW-5); activeDeadlineSeconds: 180
	//                  (v1.1 EC-NEW-6). Build fails cleanly if attest fails
	//                  (no continueOn — SLSA L3 chain requires linked
	//                  attestation OR the chain is broken per ADR D1).
	CosignAttestEnabled bool
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
	// build-mode-dispatch-and-dockerfile-security-plan T1.5 (2026-05-07):
	// group apps by ResolveStrategyForApp so `build: dockerfile` apps land in
	// their own UserDockerfileStrategy group. Without this, mixed monorepos
	// (1 node app + 1 app with user Dockerfile) would falsely group under
	// node and run loopzero-packs on an app that should be lint-only.
	var groups []languageGroup
	if spec.Plan != nil && len(spec.Plan.Apps) > 0 {
		groups = groupApps(spec.Plan.Apps, func(p services.AppBuildPlan) (string, services.AppSpec) {
			app := services.AppSpec{
				Name: p.AppName, Type: p.AppType, Port: p.Port,
				Path: p.AppPath, Framework: p.LanguageStrategy,
				Build: p.Build, Secrets: p.Secrets,
			}
			return ResolveStrategyForApp(app).Name(), app
		})
	} else {
		groups = groupApps(spec.Apps, func(app services.AppSpec) (string, services.AppSpec) {
			return ResolveStrategyForApp(app).Name(), app
		})
	}

	// REVIEW-AP-3: Generate per-language inline templates for mixed-language support.
	// Each language group gets its own validate/install/build/scan/dockerfile steps.
	//
	// build-mode-dispatch-and-dockerfile-security-plan T1.6 (2026-05-07):
	// `g.Strategy.DockerfileStep(...)` dispatches per-strategy — language
	// groups invoke loopzero-packs auto-generation, UserDockerfileStrategy invokes
	// loopzero-dockerfile-lint. NO conditional `if app.Build == "dockerfile"` in
	// this orchestrator — Strategy pattern is the dispatch axis.
	var inlineTemplates []forge.Templatable
	for _, g := range groups {
		inlineTemplates = append(inlineTemplates,
			withSuffix(g.Strategy.ValidateStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.InstallStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.BuildStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.ScanStep(), g.Suffix),
			withSuffix(g.Strategy.DockerfileStep(g.Apps, len(spec.Apps)), g.Suffix),
		)
	}
	// Per-app delivery templates: BuildKit for containers, S3 upload for static.
	// Sprint 6: Each container build is followed by a cosign signing step.
	// MEET-048: When HarborRegistryURL is set, add a mirror template per container app.
	// Sprint 6 / T1.1: Each container build is followed by cosign signing
	// AND an SBOM scan (syft). The SBOM step is non-fatal at the DAG level
	// (continueOn.failed) — see buildMainDAG.
	if spec.Plan != nil {
		for _, appPlan := range spec.Plan.Apps {
			if appPlan.DeliveryModel == "static" {
				inlineTemplates = append(inlineTemplates, b.uploadAssetsTemplate(spec, appPlan))
			} else {
				inlineTemplates = append(inlineTemplates, b.buildkitTemplateFromPlan(spec, appPlan))
				inlineTemplates = append(inlineTemplates, cosignSignTemplate(appPlan.AppName, appPlan.ImageTag, b.infraParams.RegistryInsecure))
				inlineTemplates = append(inlineTemplates, syftScanTemplate(
					spec.TenantID, spec.ProjectID, spec.CommitSHA,
					appPlan.AppName, appPlan.ImageTag, b.infraParams.RegistryInsecure,
				))
				// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
				if b.infraParams.CosignAttestEnabled {
					sbomLocalPath := fmt.Sprintf("%s/sbom-%s.cdx.json", sbomLocalDir, appPlan.AppName)
					inlineTemplates = append(inlineTemplates, cosignAttestTemplate(CosignAttestParams{
						AppName:       appPlan.AppName,
						ImageTag:      appPlan.ImageTag,
						SBOMLocalPath: sbomLocalPath,
						Keyless:       b.infraParams.CosignKeyless,
					}))
				}
				if b.infraParams.HarborRegistryURL != "" {
					harborTag := naming.ImageTag(b.infraParams.HarborRegistryURL, spec.TenantID, spec.ProjectID, appPlan.AppName, spec.CommitSHA)
					inlineTemplates = append(inlineTemplates, harborMirrorTemplate(appPlan.AppName, appPlan.ImageTag, harborTag))
				}
			}
		}
	} else {
		for _, app := range spec.Apps {
			inlineTemplates = append(inlineTemplates, b.buildkitTemplateForApp(spec, app))
			imageTag := naming.ImageTag(b.infraParams.RegistryURL, spec.TenantID, spec.ProjectID, app.Name, spec.CommitSHA)
			inlineTemplates = append(inlineTemplates, cosignSignTemplate(app.Name, imageTag, b.infraParams.RegistryInsecure))
			inlineTemplates = append(inlineTemplates, syftScanTemplate(
				spec.TenantID, spec.ProjectID, spec.CommitSHA,
				app.Name, imageTag, b.infraParams.RegistryInsecure,
			))
			// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
			if b.infraParams.CosignAttestEnabled {
				sbomLocalPath := fmt.Sprintf("%s/sbom-%s.cdx.json", sbomLocalDir, app.Name)
				inlineTemplates = append(inlineTemplates, cosignAttestTemplate(CosignAttestParams{
					AppName:       app.Name,
					ImageTag:      imageTag,
					SBOMLocalPath: sbomLocalPath,
					Keyless:       b.infraParams.CosignKeyless,
				}))
			}
			if b.infraParams.HarborRegistryURL != "" {
				harborTag := naming.ImageTag(b.infraParams.HarborRegistryURL, spec.TenantID, spec.ProjectID, app.Name, spec.CommitSHA)
				inlineTemplates = append(inlineTemplates, harborMirrorTemplate(app.Name, imageTag, harborTag))
			}
		}
	}

	// T3.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
	// Per ADR D2: register single shared observability template invoked by
	// mirror-harbor-{app} lifecycle hooks on Failed status. Skipped when
	// HarborRegistryURL not set (no harbor mirrors exist → hook never fires).
	if b.infraParams.HarborRegistryURL != "" {
		inlineTemplates = append(inlineTemplates, harborMirrorObserveTemplate())
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
		Parallelism:        forge.Ptr(workflowParallelism),
		TTLStrategy: &forge.TTLStrategy{
			SecondsAfterCompletion: forge.Ptr(ttlAfterCompletion),
			SecondsAfterSuccess:    forge.Ptr(ttlAfterSuccess),
			SecondsAfterFailure:    forge.Ptr(ttlAfterFailure),
		},
		// F-13 (CHG-2026-04-29-DOGFOOD T5.5) — `OnPodCompletion` deleta o pod
		// individual após cada step finalizar (Success OU Error). Sem isso,
		// pods de step com Error ficavam acumulando na ns `argo` até o
		// Workflow inteiro ser GC'd via TTLStrategy (default 4h success /
		// 24h failure) — observado: 7+ pods Error pós iter 12.
		// `DeleteDelayDuration: 5m` mantém pod visível 5 minutos para
		// inspeção (`kubectl logs`/`describe`) antes do delete — janela
		// suficiente para debug local + curta o suficiente para evitar
		// acúmulo. Em produção, ttlAfterFailure (24h) ainda preserva o
		// Workflow completo + artifacts em S3, então debug deep continua OK.
		PodGC: &forge.PodGC{
			Strategy:            "OnPodCompletion",
			DeleteDelayDuration: "5m",
		},
		Labels: map[string]string{
			labelTenant:      spec.TenantID,
			labelProject:     spec.ProjectID,
			labelEnvironment: spec.Environment,
			labelBuildID:     spec.BuildID,
			labelLanguage:    primaryLanguage,
		},
		Annotations: map[string]string{
			// Plan 2026-05-15 T8.1 (close-all-gaps audit M08): propagate
			// the API request's correlation_id onto the Workflow CR so
			// the build pod logs, Argo controller events, and the API
			// can be cross-referenced with a single Loki query
			// `{correlation_id="X"}`. Operator already reads this
			// annotation in application_controller.go:99.
			"loopzero.io/correlation-id": spec.CorrelationID,
		},
		Templates: templates,
		// Workspace uses a dynamically provisioned PVC shared across all DAG steps.
		// Each step in the DAG runs as a separate pod — emptyDir does NOT share across pods.
		// VolumeClaimTemplates create the PVC when the Workflow starts; it is shared
		// across all pods and cleaned up when the Workflow is garbage collected (TTL).
		//
		// Bug #21/#40 fix (scale-50k-plan): use local-path-provisioner (Rancher) instead
		// of DO Block Storage. DO CSI rate-limits volume creation to ~3/min and hits
		// HTTP 429 at concurrency >15. local-path creates hostPath volumes instantly
		// without cloud API calls. The podAffinity rule below co-locates all DAG pods
		// on the same node, so hostPath sharing just works. Requires
		// local-path-provisioner deployed on the build cluster.
		VolumeClaimTemplates: []forge.PVCVolume{
			{
				BaseVolume:       forge.BaseVolume{Name: volumeWorkspace},
				Size:             workspacePVCSize,
				StorageClassName: "local-path",
			},
		},
		Volumes: b.buildVolumes(),
		// FSGroup ensures the PVC volume is writable by non-root user 1000.
		// Without this, the PVC is mounted as root-owned and cp/mkdir fail.
		SecurityContext: &model.PodSecurityContext{
			FSGroup:    forge.Ptr(rootlessUID),
			RunAsUser:  forge.Ptr(rootlessUID),
			RunAsGroup: forge.Ptr(rootlessUID),
		},
		// PVC multi-attach is handled upstream as of loopzero-forge v0.4.0
		// (issue getloopscale/loopzero-forge#11): Workflow.Build() injects the
		// canonical podAffinity term automatically when
		// VolumeClaimTemplates is non-empty (as above), so all DAG steps
		// of the workflow are co-located on the same node and the RWO
		// workspace PVC stays attached for the workflow's lifetime.
		// No explicit Affinity is needed here.
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
		// The CWT downloads source from MinIO (production) or clones from Gitea (local dev).
		// We pass both build-id (commit SHA for MinIO path) and project-id (for Gitea repo name).
		cwtTask("clone-repo", "loopzero-clone", "clone-repo", nil, map[string]string{
			"tenant-id":  spec.TenantID,
			"project-id": spec.ProjectID,
			"build-id":   spec.CommitSHA,
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

		installName := "install-deps" + suffix
		tasks = append(tasks, inlineTask(installName, installName, []string{restoreName, validateName}))

		// CODE-002 fix: security-scan now depends on install-deps (not restore-cache).
		// Previously, pip-audit scanned an empty Python environment because install
		// and scan ran in parallel. Now scan sees installed packages.
		scanName := "security-scan" + suffix
		tasks = append(tasks, inlineTask(scanName, scanName, []string{installName}))

		buildName := "build-apps" + suffix
		tasks = append(tasks, inlineTask(buildName, buildName, []string{installName}))

		// CODE-008 fix: generate-dockerfile now depends on security-scan AND build-apps.
		// Previously, scan had no downstream deps — builds with critical vulnerabilities
		// still produced container images. Now scan is a true gate.
		//
		// build-mode-dispatch-and-dockerfile-security-plan T1.6 (2026-05-07):
		// the dockerfile-step task name comes from the Strategy's emitted
		// container — "generate-dockerfile" for language strategies (loopzero-packs),
		// "dockerfile-lint" for UserDockerfileStrategy. Hardcoding "generate-
		// dockerfile" broke build:dockerfile workflows ("invalid spec: template
		// name 'generate-dockerfile' undefined").
		dockerfileTemplateName := dockerfileStepTemplateName(g.Strategy)
		dockerfileName := dockerfileTemplateName + suffix
		tasks = append(tasks, inlineTask(dockerfileName, dockerfileName, []string{buildName, scanName}))
	}

	// Per-app delivery — BuildKit for containers, upload for static.
	// Depends on its language group's dockerfile step (containers) or build step (static).
	// Sprint 6: Each container build is followed by a cosign signing step.
	// MEET-048: When HarborRegistryURL is configured, add a mirror step (crane copy)
	// after build-image. Mirror is best-effort (continueOn.failed=true).
	if spec.Plan != nil && len(spec.Plan.Apps) > 0 {
		for _, appPlan := range spec.Plan.Apps {
			// build-mode-dispatch-and-dockerfile-security-plan T1.5: route by
			// the per-app Strategy (build:dockerfile → UserDockerfileStrategy)
			// so suffix and dockerfile-step name match the actual emitted
			// templates.
			app := services.AppSpec{
				Name: appPlan.AppName, Type: appPlan.AppType, Port: appPlan.Port,
				Path: appPlan.AppPath, Framework: appPlan.LanguageStrategy,
				Build: appPlan.Build,
			}
			strategy := ResolveStrategyForApp(app)
			suffix := suffixForStrategy(groups, strategy.Name())
			dfTaskName := dockerfileStepTemplateName(strategy) + suffix
			if appPlan.DeliveryModel == "static" {
				taskName := fmt.Sprintf("deliver-artifact-%s", appPlan.AppName)
				tasks = append(tasks, inlineTask(taskName, taskName, []string{"build-apps" + suffix}))
			} else {
				buildTaskName := fmt.Sprintf("build-image-%s", appPlan.AppName)
				tasks = append(tasks, inlineTask(buildTaskName, buildTaskName, []string{dfTaskName}))
				signTaskName := fmt.Sprintf("sign-image-%s", appPlan.AppName)
				tasks = append(tasks, inlineTask(signTaskName, signTaskName, []string{buildTaskName}))
				// Sprint 6 / T1.1: SBOM scan after sign. continueOn.failed so
				// SBOM failure does NOT block the deploy (best-effort observability,
				// not a hard gate at this point).
				sbomTaskName := fmt.Sprintf("sbom-%s", appPlan.AppName)
				sbomTask := inlineTask(sbomTaskName, sbomTaskName, []string{signTaskName})
				sbomTask.ContinueOn = &model.ContinueOn{Failed: true}
				tasks = append(tasks, sbomTask)
				// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
				// v1.1 EC-NEW-5: attest depends on BOTH sign + sbom.
				// SLSA L3 requires linked attestation; without dep on sign,
				// attest could run before sig in Rekor → broken chain.
				// Gated by InfraParams.CosignAttestEnabled — disabled by
				// default until Sigstore active (T1.4) + operator flips.
				if b.infraParams.CosignAttestEnabled {
					attestTaskName := fmt.Sprintf("attest-image-%s", appPlan.AppName)
					attestTask := inlineTask(attestTaskName, attestTaskName, []string{signTaskName, sbomTaskName})
					// NO continueOn — per ADR D1, attestation failure FAILS the
					// build (signed image alone is INSUFFICIENT for SLSA L3).
					tasks = append(tasks, attestTask)
				}
				// MEET-048: dual-push mirror to Harbor (best-effort)
				if b.infraParams.HarborRegistryURL != "" {
					mirrorTaskName := fmt.Sprintf("mirror-harbor-%s", appPlan.AppName)
					mirrorTask := inlineTask(mirrorTaskName, mirrorTaskName, []string{buildTaskName})
					mirrorTask.ContinueOn = &model.ContinueOn{Failed: true}
					// T3.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
					// Per ADR D2: lifecycle hook fires harbor-mirror-observe
					// template when this task reaches Failed phase. Restores
					// signal to silent best-effort mirror failures.
					mirrorTask.Hooks = map[string]model.LifecycleHook{
						"exit": harborMirrorObservabilityHook(mirrorTaskName, appPlan.AppName, spec.TenantID, b.infraParams.HarborRegistryURL),
					}
					tasks = append(tasks, mirrorTask)
				}
			}
		}
	} else {
		for _, app := range spec.Apps {
			strategy := ResolveStrategyForApp(app)
			suffix := suffixForStrategy(groups, strategy.Name())
			dfTaskName := dockerfileStepTemplateName(strategy) + suffix
			buildTaskName := fmt.Sprintf("build-image-%s", app.Name)
			tasks = append(tasks, inlineTask(buildTaskName, buildTaskName, []string{dfTaskName}))
			signTaskName := fmt.Sprintf("sign-image-%s", app.Name)
			tasks = append(tasks, inlineTask(signTaskName, signTaskName, []string{buildTaskName}))
			// Sprint 6 / T1.1: SBOM scan after sign. continueOn.failed so
			// SBOM failure does NOT block the deploy.
			sbomTaskName := fmt.Sprintf("sbom-%s", app.Name)
			sbomTask := inlineTask(sbomTaskName, sbomTaskName, []string{signTaskName})
			sbomTask.ContinueOn = &model.ContinueOn{Failed: true}
			tasks = append(tasks, sbomTask)
			// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
			// v1.1 EC-NEW-5: attest depends on BOTH sign + sbom.
			if b.infraParams.CosignAttestEnabled {
				attestTaskName := fmt.Sprintf("attest-image-%s", app.Name)
				attestTask := inlineTask(attestTaskName, attestTaskName, []string{signTaskName, sbomTaskName})
				tasks = append(tasks, attestTask)
			}
			// MEET-048: dual-push mirror to Harbor (best-effort)
			if b.infraParams.HarborRegistryURL != "" {
				mirrorTaskName := fmt.Sprintf("mirror-harbor-%s", app.Name)
				mirrorTask := inlineTask(mirrorTaskName, mirrorTaskName, []string{buildTaskName})
				mirrorTask.ContinueOn = &model.ContinueOn{Failed: true}
				// T3.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
				// Per ADR D2: lifecycle hook fires harbor-mirror-observe
				// template when Failed; restores visibility.
				mirrorTask.Hooks = map[string]model.LifecycleHook{
					"exit": harborMirrorObservabilityHook(mirrorTaskName, app.Name, spec.TenantID, b.infraParams.HarborRegistryURL),
				}
				tasks = append(tasks, mirrorTask)
			}
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
		// Sprint 6: cosign signing key for image signing after build.
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: secretCosignKey}, SecretName: secretCosignKey},
	}
}


