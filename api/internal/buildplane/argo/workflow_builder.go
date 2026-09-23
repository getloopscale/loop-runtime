// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"

	"github.com/getloopscale/loopzero/pkg/naming"
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
	// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
	// CR-1 fix: gate `--insecure-skip-verify` flag in cosign sign/attest
	// scripts behind explicit opt-in. Default false. When true, cosign
	// sign/attest scripts render the flag; otherwise omitted.
	// Config.Validate() rejects InsecureSkipSCT=true && IsProduction() &&
	// !TUFRootPinned. Wired via Config.CosignInsecureSkipSCT.
	CosignInsecureSkipSCT bool
	// Env is the canonical LOOPZERO_ENV value ("development" / "staging" /
	// "production"). T1.1 of plan 2026-06-01-p1-security-hardening
	// (sec-010 / CR-7): drives per-tenant Fulcio audience via D4
	// dev-fallback — development → legacy shared "sigstore" audience
	// (Fulcio config not yet upgraded); staging/production →
	// "sigstore-<tenantID>" enforcing cross-tenant signing isolation.
	Env string
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
	Apps     []deploycontracts.AppSpec
	Suffix   string // e.g., "-node", "-go", "-python", "" for single-language
}

// groupApps groups items by language strategy using a generic extraction function.
// The extractor returns (strategyName, AppSpec) for each item. Single-language
// projects get no suffix; multi-language get "-node", "-go", etc.
func groupApps[T any](items []T, extract func(T) (string, deploycontracts.AppSpec)) []languageGroup {
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
//
// T1.3 (CR-2 of MEET-2026-05-28-001): spec.Plan is REQUIRED. The legacy
// spec.Apps fallback was deleted — production callers always construct a
// BuildPlan via deploycontracts.CreateBuildPlan before submitting (see
// build_orchestrator_submit.go). Accepting a nil Plan would silently re-enable
// an unmaintained code path that bypasses appPlan.DeliveryModel branching
// (static vs container), build args, and resolved image tags.
func (b *WorkflowBuilder) Build(spec deploycontracts.WorkflowSpec) (*forge.Workflow, error) {
	if spec.Plan == nil || len(spec.Plan.Apps) == 0 {
		return nil, fmt.Errorf("argo: WorkflowSpec.Plan must be non-nil with at least one app (legacy spec.Apps fallback deleted in T1.3 CR-2)")
	}
	// Agrupa apps por `ResolveStrategyForApp`.
	//
	// O texto anterior descrevia um grupo `UserDockerfileStrategy` para apps
	// `build: dockerfile`. Esse tipo e esse caminho foram REMOVIDOS em T2.2 —
	// `user_dockerfile_strategy_test.go` assere a ausencia do passo de lint — e o
	// comentario ficou no presente descrevendo o que nao existe mais (B-157). O
	// agrupamento continua valendo pelo runtime: um monorepo com node e python nao
	// pode rodar a geracao de Dockerfile de um sobre o outro.
	groups := groupApps(spec.Plan.Apps, func(p deploycontracts.AppBuildPlan) (string, deploycontracts.AppSpec) {
		app := deploycontracts.AppSpec{
			Name: p.AppName, Type: p.AppType, Port: p.Port,
			Path: p.AppPath, Runtime: p.Runtime,
			Build: p.Build, Start: p.Start, Secrets: p.Secrets,
		}
		return ResolveStrategyForApp(app).Name(), app
	})

	// REVIEW-AP-3: Generate per-language inline templates for mixed-language support.
	// Each language group gets its own validate/install/build/scan/dockerfile steps.
	//
	// `g.Strategy.DockerfileStep(...)` despacha por estrategia, e o orquestrador
	// NAO tem `if app.Build == "dockerfile"` — o padrao Strategy e' o eixo de
	// despacho, e essa parte segue valendo.
	//
	// O que nao vale mais: o texto anterior dizia que a `UserDockerfileStrategy`
	// invoca o `loopzero-dockerfile-lint`. Esse tipo saiu em T2.2 e o linter nao e'
	// invocado por caminho nenhum — medido em 2026-08-19, nenhum codigo de
	// producao emite o passo e `LOOPZERO_DOCKERFILE_LINT_IMAGE` nao e' lida por
	// nenhum Go (B-157). Hoje todas as estrategias geram o Dockerfile.
	var inlineTemplates []forge.Templatable
	for _, g := range groups {
		inlineTemplates = append(inlineTemplates,
			withSuffix(g.Strategy.ValidateStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.InstallStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.BuildStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.ScanStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.DockerfileStep(g.Apps, len(spec.Plan.Apps)), g.Suffix),
		)
	}
	// Per-app delivery templates: BuildKit for containers, S3 upload for static.
	// Sprint 6: Each container build is followed by a cosign signing step.
	// MEET-048: When HarborRegistryURL is set, add a mirror template per container app.
	// Sprint 6 / T1.1: Each container build is followed by cosign signing
	// AND an SBOM scan (syft). The SBOM step is non-fatal at the DAG level
	// (continueOn.failed) — see buildMainDAG.
	// T1.3 (CR-2): legacy spec.Apps else branch deleted. Build() guard at the
	// top enforces spec.Plan != nil with non-empty Apps, so this loop is the
	// single emission path for per-app delivery templates.
	for _, appPlan := range spec.Plan.Apps {
		if appPlan.DeliveryModel == "static" {
			inlineTemplates = append(inlineTemplates, b.uploadAssetsTemplate(spec, appPlan))
			continue
		}
		inlineTemplates = append(inlineTemplates, b.buildkitTemplateFromPlan(spec, appPlan))
		inlineTemplates = append(inlineTemplates, cosignSignTemplateWithParams(CosignSignParams{
			AppName:          appPlan.AppName,
			ImageTag:         appPlan.ImageTag,
			InsecureRegistry: b.infraParams.RegistryInsecure,
			Keyless:          b.infraParams.CosignKeyless,
			FulcioURL:        b.infraParams.CosignFulcioURL,
			RekorURL:         b.infraParams.CosignRekorURL,
			InsecureSkipSCT:  b.infraParams.CosignInsecureSkipSCT, // T1.2 CR-1 fix
		}))
		inlineTemplates = append(inlineTemplates, syftScanTemplate(
			spec.TenantID, spec.ProjectID, spec.CommitSHA,
			appPlan.AppName, appPlan.ImageTag, b.infraParams.RegistryInsecure,
		))
		// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
		if b.infraParams.CosignAttestEnabled {
			sbomLocalPath := fmt.Sprintf("%s/sbom-%s.cdx.json", sbomLocalDir, appPlan.AppName)
			inlineTemplates = append(inlineTemplates, cosignAttestTemplate(CosignAttestParams{
				AppName:          appPlan.AppName,
				ImageTag:         appPlan.ImageTag,
				SBOMLocalPath:    sbomLocalPath,
				Keyless:          b.infraParams.CosignKeyless,
				InsecureRegistry: b.infraParams.RegistryInsecure,
				FulcioURL:        b.infraParams.CosignFulcioURL,
				RekorURL:         b.infraParams.CosignRekorURL,
				InsecureSkipSCT:  b.infraParams.CosignInsecureSkipSCT, // T1.2 CR-1 fix
			}))
		}
		if b.infraParams.HarborRegistryURL != "" {
			harborTag := naming.ImageTag(b.infraParams.HarborRegistryURL, spec.TenantID, spec.ProjectID, appPlan.AppName, spec.CommitSHA)
			inlineTemplates = append(inlineTemplates, harborMirrorTemplate(appPlan.AppName, appPlan.ImageTag, harborTag))
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
		Volumes: b.buildVolumes(spec.TenantID),
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
func (b *WorkflowBuilder) buildMainDAG(spec deploycontracts.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
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
		// O nome do passo de Dockerfile vem da constante `dockerfileStepTemplate`.
		//
		// Ele ja' foi perguntado a Strategy, porque havia duas respostas:
		// "generate-dockerfile" para as estrategias de linguagem e "dockerfile-lint"
		// para o caminho "traga seu Dockerfile". Esse caminho saiu em T2.2 e a
		// variacao colapsou (B-068); o comentario anterior seguia descrevendo as
		// duas como se coexistissem (B-157). Se voltar a haver duas,
		// `TestTodaEstrategiaEmiteOMesmoNomeDePassoDeDockerfile` reprova e manda
		// restaurar o ponto de variacao em vez de fixar um segundo literal aqui.
		dockerfileTemplateName := dockerfileStepTemplate
		dockerfileName := dockerfileTemplateName + suffix
		tasks = append(tasks, inlineTask(dockerfileName, dockerfileName, []string{buildName, scanName}))
	}

	// Per-app delivery — BuildKit for containers, upload for static.
	// Depends on its language group's dockerfile step (containers) or build step (static).
	// Sprint 6: Each container build is followed by a cosign signing step.
	// MEET-048: When HarborRegistryURL is configured, add a mirror step (crane copy)
	// after build-image. Mirror is best-effort (continueOn.failed=true).
	// T1.3 (CR-2): legacy spec.Apps else branch deleted. Build() guard at the
	// top enforces spec.Plan != nil + len(spec.Plan.Apps) > 0, so the per-app
	// DAG task wiring has a single emission path keyed by appPlan.DeliveryModel.
	for _, appPlan := range spec.Plan.Apps {
		// Roteia pela Strategy do app. O texto anterior citava
		// `build:dockerfile → UserDockerfileStrategy`, caminho removido em T2.2
		// (B-157) — hoje o roteamento e' por runtime.
		// so suffix and dockerfile-step name match the actual emitted templates.
		app := deploycontracts.AppSpec{
			Name: appPlan.AppName, Type: appPlan.AppType, Port: appPlan.Port,
			Path: appPlan.AppPath, Runtime: appPlan.Runtime,
			Build: appPlan.Build, Start: appPlan.Start,
		}
		strategy := ResolveStrategyForApp(app)
		suffix := suffixForStrategy(groups, strategy.Name())
		dfTaskName := dockerfileStepTemplate + suffix
		if appPlan.DeliveryModel == "static" {
			taskName := fmt.Sprintf("deliver-artifact-%s", appPlan.AppName)
			tasks = append(tasks, inlineTask(taskName, taskName, []string{"build-apps" + suffix}))
			continue
		}
		buildTaskName := naming.BuildImageStepName(appPlan.AppName)
		tasks = append(tasks, inlineTask(buildTaskName, buildTaskName, []string{dfTaskName}))
		signTaskName := naming.SignStepName(appPlan.AppName)
		tasks = append(tasks, inlineTask(signTaskName, signTaskName, []string{buildTaskName}))
		// Sprint 6 / T1.1: SBOM scan after sign. continueOn.failed so
		// SBOM failure does NOT block the deploy (best-effort observability,
		// not a hard gate at this point).
		sbomTaskName := naming.SBOMStepName(appPlan.AppName)
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
			attestTaskName := naming.AttestStepName(appPlan.AppName)
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

	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// buildOnExitDAG constructs the finalize DAG (always runs, even on failure).
// REVIEW-AP-3: saves cache per-language (each language has different cache dirs).
func (b *WorkflowBuilder) buildOnExitDAG(spec deploycontracts.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
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
//
// When CosignKeyless=true, a projected ServiceAccountToken volume with a
// per-tenant audience (D1 of plan 2026-06-01-p1-security-hardening) is
// added so cosign-sign + cosign-attest can present the SA token to Fulcio
// (loopzero-forge v0.6.0+, PR #20).
//
// sec-010 / CR-7 of MEET-2026-05-28-001: prior to T1.1 the audience was the
// shared literal "sigstore" across all tenants. A pod in tenant-A's
// namespace could present its SA token to Fulcio claiming to be tenant-B
// (no audience binding to the tenant). Per-tenant audience
// "sigstore-<tenantID>" enforces cross-tenant signing identity isolation.
//
// D4 dev-fallback: in development the Fulcio config has not yet been
// upgraded to accept per-tenant audiences, so we keep the legacy shared
// "sigstore" audience. Staging + production use the per-tenant form.
func (b *WorkflowBuilder) buildVolumes(tenantID string) []forge.VolumeBuilder {
	vols := []forge.VolumeBuilder{
		// Registry credentials for BuildKit push — mounted at /home/user/.docker/ in each build container.
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: secretRegistryConfig}, SecretName: secretRegistryConfig},
		// Sprint 6: cosign signing key for image signing after build.
		&forge.SecretVolume{BaseVolume: forge.BaseVolume{Name: secretCosignKey}, SecretName: secretCosignKey},
	}
	if b.infraParams.CosignKeyless {
		// M2 Phase 9 of plan 2026-05-27-M07-to-3-85 + T1.1 of plan
		// 2026-06-01-p1-security-hardening: pod-level projected SA token
		// volume with audience scoped per tenant. cosign reads token at
		// /var/run/secrets/sigstore/token + presents to Fulcio for short-lived
		// signing cert. Without this, keyless mode is teatro silencioso.
		expSec := int64(600) // K8s minimum (10min); kubelet rotates >80% expiry
		vols = append(vols, &forge.ProjectedVolume{
			BaseVolume: forge.BaseVolume{Name: sigstoreTokenVolumeName},
			Sources: []forge.VolumeProjection{
				{ServiceAccountToken: &forge.ServiceAccountTokenProjection{
					Audience:          audienceFor(tenantID, b.infraParams.Env),
					Path:              "token",
					ExpirationSeconds: &expSec,
				}},
			},
		})
	}
	return vols
}

// audienceFor returns the Fulcio audience for a build's projected SA token.
//
// T1.1 of plan 2026-06-01-p1-security-hardening (sec-010 / CR-7):
//   - "production" / "staging": "sigstore-<tenantID>" — per-tenant binding.
//     Fulcio config (T1.2) accepts the matching audience list/regex; a pod
//     in tenant-A cannot mint a token with tenant-B's audience because the
//     kubelet refuses (SA → namespace scope) AND Fulcio rejects mismatches.
//   - "development" / unknown / empty: "sigstore" legacy shared (D4
//     dev-fallback) — local k3d Fulcio not yet configured for the
//     per-tenant form; preserves current cleanroom-fresh trio behaviour.
//
// Empty tenantID in production+staging is treated as a programming error
// and returns "sigstore-MISSING-TENANT" which Fulcio will reject — fail
// closed rather than silently fall back to legacy. The convention surfaces
// in logs and is caught by Fulcio's audience allowlist (T1.2).
func audienceFor(tenantID, env string) string {
	switch env {
	case "production", "staging":
		if tenantID == "" {
			return "sigstore-MISSING-TENANT"
		}
		return "sigstore-" + tenantID
	default:
		// development OR unknown — legacy shared audience (D4 fallback).
		return "sigstore"
	}
}
