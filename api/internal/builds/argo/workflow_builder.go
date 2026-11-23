// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
	"github.com/getloopscale/argo-workflows-go/model"

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
	RegistryURL      string
	RegistryInsecure bool   // true for local dev registries without TLS
	BaseDomain       string // Base domain for app hostnames (e.g., "loopzero.local")
	// WorkspaceStorageClass is the StorageClass of the build's workspace PVC
	// (config LOOPZERO_BUILD_WORKSPACE_STORAGE_CLASS). Empty selects the build
	// cluster's default class, which is what an EKS cluster needs.
	WorkspaceStorageClass string
	// CosignImage and SyftImage override the signer and SBOM scanner images
	// (config LOOPZERO_COSIGN_IMAGE / LOOPZERO_SYFT_IMAGE; local k3d points them at
	// its registry). Empty means the built-in imageCosignSigner/imageSyftScanner.
	CosignImage string
	SyftImage   string
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
	// Group apps by `ResolveStrategyForApp`: a monorepo with node and python
	// cannot run one's Dockerfile generation over the other. (This used to
	// describe a `UserDockerfileStrategy` group for `build: dockerfile` apps;
	// that type and path were removed in T2.2, B-157.)
	groups := groupApps(spec.Plan.Apps, func(p deploycontracts.AppBuildPlan) (string, deploycontracts.AppSpec) {
		app := appSpecFromPlan(p)
		return ResolveStrategyForApp(app).Name(), app
	})

	templates, onExitName, err := b.workflowTemplates(spec, groups)
	if err != nil {
		return nil, err
	}
	volumes, err := b.buildVolumes(spec.TenantID)
	if err != nil {
		return nil, err
	}
	return b.newWorkflowCR(spec, templates, onExitName, primaryLanguage(groups), volumes), nil
}

// appSpecFromPlan is the one conversion from a resolved AppBuildPlan to the
// AppSpec the language strategies read.
func appSpecFromPlan(p deploycontracts.AppBuildPlan) deploycontracts.AppSpec {
	return deploycontracts.AppSpec{
		Name: p.AppName, Type: p.AppType, Port: p.Port,
		Path: p.AppPath, Runtime: p.Runtime,
		Build: p.Build, Start: p.Start, Secrets: p.Secrets,
	}
}

// primaryLanguage labels the workflow with the first group's language.
func primaryLanguage(groups []languageGroup) string {
	if len(groups) == 0 {
		return "custom"
	}
	return groups[0].Strategy.Name()
}

// workflowTemplates returns every template of the workflow — the main DAG,
// the onExit DAG when there is one (its name is returned too), the
// per-language steps and the per-app delivery steps.
func (b *WorkflowBuilder) workflowTemplates(spec deploycontracts.WorkflowSpec, groups []languageGroup) ([]forge.Templatable, string, error) {
	mainDAG, err := b.buildMainDAG(spec, groups)
	if err != nil {
		return nil, "", fmt.Errorf("build main DAG: %w", err)
	}
	// The onExit DAG saves caches per language; Argo requires >= 1 task per
	// DAG, so it is absent when no language caches.
	onExitDAG, err := b.buildOnExitDAG(spec, groups)
	if err != nil {
		return nil, "", fmt.Errorf("build onExit DAG: %w", err)
	}

	templates := []forge.Templatable{mainDAG}
	onExitName := ""
	if onExitDAG != nil {
		templates = append(templates, onExitDAG)
		onExitName = onExitDAGName
	}
	templates = append(templates, languageTemplates(groups, len(spec.Plan.Apps))...)
	for _, appPlan := range spec.Plan.Apps {
		delivery, err := b.appDeliveryTemplates(spec, appPlan)
		if err != nil {
			return nil, "", err
		}
		templates = append(templates, delivery...)
	}
	return templates, onExitName, nil
}

// languageTemplates emits the per-language steps (REVIEW-AP-3, mixed-language
// support): each group gets its own validate/install/build/scan/dockerfile.
// `DockerfileStep` dispatches per strategy — the orchestrator has no
// `if app.Build == "dockerfile"`; every strategy now generates the Dockerfile
// (the `loopzero-dockerfile-lint` path left in T2.2, B-157).
func languageTemplates(groups []languageGroup, totalApps int) []forge.Templatable {
	var templates []forge.Templatable
	for _, g := range groups {
		templates = append(templates,
			withSuffix(g.Strategy.ValidateStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.InstallStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.BuildStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.ScanStep(g.Apps), g.Suffix),
			withSuffix(g.Strategy.DockerfileStep(g.Apps, totalApps), g.Suffix),
		)
	}
	return templates
}

// appDeliveryTemplates emits one app's delivery templates: an upload for a
// static app; for a container app, the BuildKit build, the cosign signature,
// the SBOM (non-fatal at the DAG level, see appDeliveryTasks), the SLSA
// attestation when enabled.
func (b *WorkflowBuilder) appDeliveryTemplates(spec deploycontracts.WorkflowSpec, appPlan deploycontracts.AppBuildPlan) ([]forge.Templatable, error) {
	if appPlan.DeliveryModel == "static" {
		return []forge.Templatable{b.uploadAssetsTemplate(spec, appPlan)}, nil
	}
	buildkit, err := b.buildkitTemplateFromPlan(spec, appPlan)
	if err != nil {
		return nil, err
	}
	templates := []forge.Templatable{
		buildkit,
		cosignSignTemplateWithParams(CosignSignParams{
			AppName:          appPlan.AppName,
			ImageTag:         appPlan.ImageTag,
			InsecureRegistry: b.infraParams.RegistryInsecure,
			Keyless:          b.infraParams.CosignKeyless,
			FulcioURL:        b.infraParams.CosignFulcioURL,
			RekorURL:         b.infraParams.CosignRekorURL,
			InsecureSkipSCT:  b.infraParams.CosignInsecureSkipSCT, // T1.2 CR-1 fix
			SignerImage:      b.infraParams.CosignImage,
		}),
		syftScanTemplate(spec.TenantID, spec.ProjectID, spec.CommitSHA,
			appPlan.AppName, appPlan.ImageTag, b.infraParams.RegistryInsecure, b.infraParams.SyftImage),
	}
	// T2.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
	if b.infraParams.CosignAttestEnabled {
		templates = append(templates, cosignAttestTemplate(CosignAttestParams{
			AppName:          appPlan.AppName,
			ImageTag:         appPlan.ImageTag,
			SBOMLocalPath:    fmt.Sprintf("%s/sbom-%s.cdx.json", sbomLocalDir, appPlan.AppName),
			Keyless:          b.infraParams.CosignKeyless,
			InsecureRegistry: b.infraParams.RegistryInsecure,
			FulcioURL:        b.infraParams.CosignFulcioURL,
			RekorURL:         b.infraParams.CosignRekorURL,
			InsecureSkipSCT:  b.infraParams.CosignInsecureSkipSCT, // T1.2 CR-1 fix
			SignerImage:      b.infraParams.CosignImage,
		}))
	}
	return templates, nil
}

// newWorkflowCR assembles the Workflow resource around its templates.
func (b *WorkflowBuilder) newWorkflowCR(spec deploycontracts.WorkflowSpec, templates []forge.Templatable, onExitName, language string, volumes []forge.VolumeBuilder) *forge.Workflow {
	return &forge.Workflow{
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
		// F-13 (CHG-2026-04-29-DOGFOOD T5.5) — `OnPodCompletion` deletes each
		// step's pod once the step finishes (Success OR Error). Without it,
		// Error step pods piled up in the `argo` namespace until the whole
		// Workflow was GC'd via TTLStrategy (default 4h success / 24h
		// failure) — observed: 7+ Error pods after iter 12.
		// `DeleteDelayDuration: 5m` keeps the pod visible for 5 minutes for
		// inspection (`kubectl logs`/`describe`) before deletion — long enough
		// for local debugging, short enough to avoid pile-up. In production,
		// ttlAfterFailure (24h) still keeps the whole Workflow and its S3
		// artifacts, so deep debugging still works.
		PodGC: &forge.PodGC{
			Strategy:            "OnPodCompletion",
			DeleteDelayDuration: "5m",
		},
		Labels: map[string]string{
			labelTenant:      spec.TenantID,
			labelProject:     spec.ProjectID,
			labelEnvironment: spec.Environment,
			labelBuildID:     spec.BuildID,
			labelLanguage:    language,
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
		// The class comes from configuration; empty means the cluster default. A
		// cluster that wants local-path (k3d, or a node-local burst setup where the
		// cloud CSI rate-limits volume creation) sets it explicitly. The podAffinity
		// forge injects co-locates every DAG pod on one node, so a node-local volume
		// is shared too.
		VolumeClaimTemplates: []forge.PVCVolume{
			{
				BaseVolume:       forge.BaseVolume{Name: volumeWorkspace},
				Size:             workspacePVCSize,
				StorageClassName: b.infraParams.WorkspaceStorageClass,
			},
		},
		Volumes: volumes,
		// FSGroup ensures the PVC volume is writable by non-root user 1000.
		// Without this, the PVC is mounted as root-owned and cp/mkdir fail.
		SecurityContext: &model.PodSecurityContext{
			FSGroup:    forge.Ptr(rootlessUID),
			RunAsUser:  forge.Ptr(rootlessUID),
			RunAsGroup: forge.Ptr(rootlessUID),
		},
		// PVC multi-attach is handled upstream as of loopzero-forge v0.4.0
		// (issue getloopscale/argo-workflows-go#11): Workflow.Build() injects the
		// canonical podAffinity term automatically when
		// VolumeClaimTemplates is non-empty (as above), so all DAG steps
		// of the workflow are co-located on the same node and the RWO
		// workspace PVC stays attached for the workflow's lifetime.
		// No explicit Affinity is needed here.
	}
}

// buildMainDAG constructs the entrypoint DAG template.
//
// REVIEW-AP-3: the DAG supports mixed-language monorepos. Each language group
// gets its own steps (suffixed -node, -python, ...), chained clone ->
// restore-cache -> validate -> install -> build/scan -> dockerfile, then the
// per-app delivery tasks hang off their group's dockerfile (containers) or
// build (static) step.
func (b *WorkflowBuilder) buildMainDAG(spec deploycontracts.WorkflowSpec, groups []languageGroup) (*forge.DAG, error) {
	// The CWT downloads source from MinIO (production) or clones from Gitea
	// (local dev): build-id is the commit SHA for the MinIO path, project-id
	// the Gitea repo name. One clone is shared by every language.
	tasks := []*forge.Task{
		cwtTask("clone-repo", "loopzero-clone", "clone-repo", nil, map[string]string{
			"tenant-id":  spec.TenantID,
			"project-id": spec.ProjectID,
			"build-id":   spec.CommitSHA,
		}),
	}
	for _, g := range groups {
		tasks = append(tasks, languageTasks(spec, g)...)
	}
	// T1.3 (CR-2): Build() guarantees spec.Plan has apps, so this is the one
	// emission path for per-app tasks, keyed by appPlan.DeliveryModel.
	for _, appPlan := range spec.Plan.Apps {
		tasks = append(tasks, b.appDeliveryTasks(spec, groups, appPlan)...)
	}

	dag := &forge.DAG{Name: entrypointName}
	if err := dag.AddTasks(tasks...); err != nil {
		return nil, err
	}
	return dag, nil
}

// languageTasks wires one language group's steps after the shared clone.
func languageTasks(spec deploycontracts.WorkflowSpec, g languageGroup) []*forge.Task {
	suffix := g.Suffix
	var tasks []*forge.Task

	// restore-cache per language (different cache keys: node-modules vs pip);
	// a language without a cache anchors on the clone instead.
	restoreName := "clone-repo"
	if g.Strategy.CacheKey() != "" {
		restoreName = "restore-cache" + suffix
		tasks = append(tasks, cwtTask(restoreName, "loopzero-cache-restore", "restore-cache",
			[]string{"clone-repo"}, map[string]string{
				"tenant-id":  spec.TenantID,
				"project-id": spec.ProjectID,
				"cache-key":  g.Strategy.CacheKey(),
			}))
	}

	validateName := "validate-workspace" + suffix
	installName := "install-deps" + suffix
	// CODE-002 fix: security-scan depends on install-deps, so it sees the
	// installed packages.
	scanName := "security-scan" + suffix
	buildName := "build-apps" + suffix
	// CODE-008 fix: the Dockerfile step depends on security-scan AND
	// build-apps, so the scan is a true gate. Its name is the
	// `dockerfileStepTemplate` constant: the variation between strategies
	// collapsed in T2.2 (B-068, B-157), and
	// `TestEveryStrategyEmitsTheSameDockerfileStepName` fails if it returns.
	dockerfileName := dockerfileStepTemplate + suffix
	return append(tasks,
		inlineTask(validateName, validateName, []string{"clone-repo"}),
		inlineTask(installName, installName, []string{restoreName, validateName}),
		inlineTask(scanName, scanName, []string{installName}),
		inlineTask(buildName, buildName, []string{installName}),
		inlineTask(dockerfileName, dockerfileName, []string{buildName, scanName}),
	)
}

// appDeliveryTasks wires one app's delivery tasks after its language group's
// steps: deliver-artifact for a static app; for a container app build-image,
// sign-image, the SBOM and the attestation when enabled.
func (b *WorkflowBuilder) appDeliveryTasks(spec deploycontracts.WorkflowSpec, groups []languageGroup, appPlan deploycontracts.AppBuildPlan) []*forge.Task {
	suffix := suffixForStrategy(groups, ResolveStrategyForApp(appSpecFromPlan(appPlan)).Name())
	if appPlan.DeliveryModel == "static" {
		taskName := fmt.Sprintf("deliver-artifact-%s", appPlan.AppName)
		return []*forge.Task{inlineTask(taskName, taskName, []string{"build-apps" + suffix})}
	}

	buildTaskName := naming.BuildImageStepName(appPlan.AppName)
	signTaskName := naming.SignStepName(appPlan.AppName)
	sbomTaskName := naming.SBOMStepName(appPlan.AppName)
	// Sprint 6 / T1.1: the SBOM runs after the signature with
	// continueOn.failed — best-effort observability, not a hard gate.
	sbomTask := inlineTask(sbomTaskName, sbomTaskName, []string{signTaskName})
	sbomTask.ContinueOn = &model.ContinueOn{Failed: true}
	tasks := []*forge.Task{
		inlineTask(buildTaskName, buildTaskName, []string{dockerfileStepTemplate + suffix}),
		inlineTask(signTaskName, signTaskName, []string{buildTaskName}),
		sbomTask,
	}

	// v1.1 EC-NEW-5: attest depends on BOTH sign and sbom — SLSA L3 needs the
	// attestation linked to a signature already in Rekor. No continueOn: per
	// ADR D1 an attestation failure FAILS the build.
	if b.infraParams.CosignAttestEnabled {
		attestTaskName := naming.AttestStepName(appPlan.AppName)
		tasks = append(tasks, inlineTask(attestTaskName, attestTaskName, []string{signTaskName, sbomTaskName}))
	}
	return tasks
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
func (b *WorkflowBuilder) buildVolumes(tenantID string) ([]forge.VolumeBuilder, error) {
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
		// signing cert. Without this, keyless mode is silent theatre.
		audience, err := audienceFor(tenantID, b.infraParams.Env)
		if err != nil {
			return nil, err
		}
		expSec := int64(600) // K8s minimum (10min); kubelet rotates >80% expiry
		vols = append(vols, &forge.ProjectedVolume{
			BaseVolume: forge.BaseVolume{Name: sigstoreTokenVolumeName},
			Sources: []forge.VolumeProjection{
				{ServiceAccountToken: &forge.ServiceAccountTokenProjection{
					Audience:          audience,
					Path:              "token",
					ExpirationSeconds: &expSec,
				}},
			},
		})
	}
	return vols, nil
}

// audienceFor returns the Fulcio audience for a build's projected SA token.
//
// T1.1 of plan 2026-06-01-p1-security-hardening (sec-010 / CR-7):
//   - "production" / "staging": "sigstore-<tenantID>" — per-tenant binding.
//     Fulcio config (T1.2) accepts the matching audience list/regex; a pod
//     in tenant-A cannot mint a token with tenant-B's audience because the
//     kubelet refuses (SA → namespace scope) AND Fulcio rejects mismatches.
//   - "development": "sigstore" legacy shared (D4 dev-fallback) — local k3d
//     Fulcio not yet configured for the per-tenant form; preserves current
//     cleanroom-fresh trio behaviour.
//
// Only an explicitly "development" environment may use the shared audience:
// an unknown or empty environment name is an error, because falling back
// would silently drop per-tenant signing isolation on a typo. An empty
// tenantID in production/staging is an error too, rather than a sentinel
// audience left for Fulcio to reject.
func audienceFor(tenantID, env string) (string, error) {
	switch env {
	case "production", "staging":
		if tenantID == "" {
			return "", fmt.Errorf("argo: keyless signing in %s needs a tenant id for the per-tenant audience", env)
		}
		return "sigstore-" + tenantID, nil
	case "development":
		return "sigstore", nil // D4 dev fallback: legacy shared audience
	default:
		return "", fmt.Errorf("argo: keyless signing audience for environment %q is undefined (want development, staging or production)", env)
	}
}
