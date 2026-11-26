// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/scrub"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/manifests"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// onWorkflowSucceeded handles a completed build workflow by creating a release
// and activating it through the standard flow (gitops push → ArgoCD sync).
//
// CHG-2026-04-25-ATTESTATION-WIRE: takes the WorkflowStatus (which carries
// per-step outputs from the Argo Workflow CR) and enriches each AppRelease
// with the cryptographic digest + cosign signature reference. Missing
// outputs are tolerated — workflows pre-this-change still produce valid
// callbacks with empty signature fields.
func (r *BuildReconcileLoop) onWorkflowSucceeded(ctx context.Context, build *db.Build, status *deploycontracts.WorkflowStatus) {
	r.logger.Info("reconcile loop: workflow succeeded, processing build completion",
		"build_id", build.ID, "workflow_id", build.WorkflowID)

	metrics.BuildsTotal.WithLabelValues(build.TenantID, build.Environment, "succeeded").Inc()
	metrics.BuildDuration.WithLabelValues(build.TenantID, build.Environment, "succeeded").Observe(time.Since(build.CreatedAt).Seconds())
	metrics.BuildQueueDepth.Dec()

	// Compute image references from deterministic pattern
	var buildApps []deploycontracts.AppRelease
	var manifestApps []manifests.AppManifestInput

	// Parse the apps from the build record (stored at submission time)
	apps, err := r.buildStore.GetBuildApps(ctx, build.ID)
	if err != nil {
		r.logger.Error("reconcile loop: failed to get build apps",
			"build_id", build.ID, "error", err)
		r.failBuild(ctx, build, "Failed to read build apps from database")
		return
	}

	// Build AppRelease list for the build callback (URL computation, etc.)
	for _, app := range apps {
		buildApps = append(buildApps, deploycontracts.BuildAppRelease(
			r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name,
			build.CommitSHA, build.Environment, r.cfg.BaseDomain, app.Type, app.Rendering,
		))
	}

	// CHG-2026-04-25-ATTESTATION-WIRE: enrich AppRelease entries with the
	// signature data the workflow produced. The displayName of each step
	// follows the deterministic pattern "sign-image-{appName}" /
	// "build-image-{appName}", established in argo/cosign.go and
	// argo/buildkit.go. We use API insert time (not workflow finish time)
	// for SignedAt because pod clocks are not authoritative. Apps whose
	// image was reused ran no image step; their fields come from the
	// stored reuse.
	applyReusedImages(buildApps, apps)
	enrichAppReleasesWithAttestation(buildApps, status)

	// F-010: shared manifest building function (includes custom domains).
	// CODE-002: errors are fatal — prevents silent IngressRoute erasure.
	//
	// CHG-2026-04-29-DOGFOOD iter 8: forward digest map from buildApps
	// (populated by enrichAppReleasesWithAttestation just above) so the
	// manifest renders use `image@sha256:...` instead of `image:tag`.
	// Mutable tag refs are rejected by VAP `loop-runtime-require-image-signature`
	// in tenant namespaces — pods would be Forbidden at admission time.
	digests := make(map[string]string, len(buildApps))
	for _, ar := range buildApps {
		if ar.ImageDigestSHA256 != "" {
			digests[ar.Name] = ar.ImageDigestSHA256
		}
	}
	manifestApps, err = deploycontracts.BuildManifestApps(ctx, deploycontracts.BuildManifestAppsInput{
		RegistryURL: r.cfg.RegistryURL,
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CommitSHA:   build.CommitSHA,
		Apps:        apps,
		AppDigests:  digests,
		DomainStore: r.domainStore,
	})
	if err != nil {
		r.logger.Error("reconcile loop: manifest building failed", "build_id", build.ID, "error", err)
		r.failBuild(ctx, build, manifestFailureReason(err))
		return
	}

	// Generate manifests in the API (C-02 fix: control plane generates manifests)
	// Plan v1.2 T3.1 — Render now returns (string, error). Empty ClusterIssuerName
	// (config wiring bug) used to panic the reconcile loop; now we fail the build
	// gracefully so the platform can recover.
	renderedManifests, renderErr := manifests.Render(manifests.RenderInput{
		TenantID:          build.TenantID,
		ProjectID:         build.ProjectID,
		Environment:       build.Environment,
		BuildID:           build.ID,
		ReleaseID:         "",
		BaseDomain:        r.cfg.BaseDomain,
		ClusterIssuerName: r.cfg.ClusterIssuer, // B14
		Apps:              manifestApps,
		// argo-rollouts-progressive-delivery-plan T1.2 — Progressive delivery wiring.
		// Plan defaults to "starter" inside the renderer when empty (V1: tenant
		// plan lookup is YAGNI — fixed conservative steps work for all tiers
		// until real plan-aware behavior is needed). PrometheusAddress,
		// ProgressiveDelivery and LeanResources come from Config.
		PrometheusAddress:   r.cfg.PrometheusAddress,
		ProgressiveDelivery: r.cfg.ProgressiveDelivery,
		LeanResources:       r.cfg.LeanResources,
	})
	if renderErr != nil {
		r.logger.Error("reconcile loop: manifests.Render failed", "build_id", build.ID, "error", renderErr)
		r.failBuild(ctx, build, "Failed to render manifests — platform configuration error")
		return
	}

	// Ensure LoopRuntimeApplication CRs exist on mgmt cluster (non-fatal).
	// Enables hostname assignment, ExposedRoute creation, TLS monitoring, reachability probes.
	r.ensureApplicationCRs(ctx, build, apps)

	// Construct the completion result and delegate to the orchestrator
	result := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      buildApps,
		Manifests: renderedManifests,
	}

	if err := r.orch.OnBuildCompleted(ctx, build.ID, result); err != nil {
		// F-01 fix: track completion failures. After 3 attempts, fail the build
		// to prevent permanent RUNNING state when completion processing is broken.
		attemptsVal, _ := r.completionAttempts.LoadOrStore(build.ID, 0)
		attempts := attemptsVal.(int) + 1
		r.completionAttempts.Store(build.ID, attempts)

		if attempts >= maxActivationAttempts {
			r.logger.Error("reconcile loop: build completion failed after max attempts, failing build",
				"build_id", build.ID, "attempts", attempts, "error", err)
			r.completionAttempts.Delete(build.ID)
			r.failBuild(ctx, build, fmt.Sprintf("Build completion processing failed after %d attempts: %v", attempts, err))
			return
		}

		r.logger.Error("reconcile loop: build completion processing failed, will retry",
			"build_id", build.ID, "attempt", attempts, "error", err)
	} else {
		r.completionAttempts.Delete(build.ID)
	}
}

// completeBuild transitions a RELEASING build to SUCCEEDED (MEET-2026-03-26-008).
// Called when the reconcile loop detects that the release is already ACTIVE.
func (r *BuildReconcileLoop) completeBuild(ctx context.Context, build *db.Build) {
	r.activationAttempts.Delete(build.ID) // BRC-003: cleanup on success
	err := r.finalizer().succeed(ctx, build)
	if err != nil {
		r.logger.Error("reconcile loop: failed to complete RELEASING build",
			"build_id", build.ID, "error", err)
	}
}

// ensureApplicationCRs creates LoopRuntimeApplication CRs on the mgmt cluster for each app
// in the build. This wires the ApplicationController and ExposureController to provide
// hostname assignment, ExposedRoute creation, TLS monitoring, and reachability probes.
// CHG-2026-04-08-001: Non-fatal — failure is logged but does not block the deploy.
func (r *BuildReconcileLoop) ensureApplicationCRs(ctx context.Context, build *db.Build, apps []db.BuildAppSpec) {
	if r.appCRMgr == nil {
		r.logger.Debug("reconcile loop: appCRMgr not configured, skipping LoopRuntimeApplication CR creation")
		return
	}
	for _, app := range apps {
		port := int32(app.Port)
		if port == 0 {
			port = 80
		}
		input := ApplicationCRInput{
			TenantID:    build.TenantID,
			Project:     build.ProjectID,
			Environment: build.Environment,
			AppName:     app.Name,
			ServiceName: app.Name,
			ServicePort: port,
		}
		if err := r.appCRMgr.EnsureLoopRuntimeApplication(ctx, input); err != nil {
			metrics.ApplicationCREnsureTotal.WithLabelValues(build.TenantID, app.Name, "failure").Inc()
			r.logger.Warn("reconcile loop: failed to ensure LoopRuntimeApplication CR (non-fatal)",
				"build_id", build.ID, "app", app.Name, "error", err)
		} else {
			metrics.ApplicationCREnsureTotal.WithLabelValues(build.TenantID, app.Name, "success").Inc()
			r.logger.Info("reconcile loop: LoopRuntimeApplication CR ensured",
				"build_id", build.ID, "app", app.Name,
				"tenant", build.TenantID, "environment", build.Environment)
		}
	}
}

// enrichAppReleasesWithAttestation walks `buildApps` in place and
// populates the cosign signature + image digest fields, plus Sprint 6
// SBOM metadata, from the workflow's per-step outputs.
//
// CHG-2026-04-25-ATTESTATION-WIRE Phase 4 (signature/digest):
//   - `sign-image-{name}` emits `signature` (pinned reference) + `digest`
//     (bare sha256:...). When `signature` is present, ImageSignedAt is
//     set to the API server's current time (pod clocks are not trusted).
//   - `build-image-{name}` is a fallback source for `digest`.
//
// Sprint 6 / T3.1 (SBOM):
//   - `sbom-{name}` emits `sbom_s3_key`, `sbom_format`, `sbom_component_count`.
//   - The SBOM step is best-effort (continueOn.failed at the DAG level),
//     so absence of these outputs is normal and must not be treated as
//     an error.
//   - All fields stay at their zero value when outputs are missing.
func enrichAppReleasesWithAttestation(apps []deploycontracts.AppRelease, status *deploycontracts.WorkflowStatus) {
	if status == nil || len(status.Outputs) == 0 {
		return
	}
	for i, app := range apps {
		if app.ImageReused {
			continue // no image step ran for it; applyReusedImages filled it
		}
		signStep := naming.SignStepName(app.Name)
		buildStep := naming.BuildImageStepName(app.Name)
		sbomStep := naming.SBOMStepName(app.Name)

		if signOutputs, ok := status.Outputs[signStep]; ok {
			if sig := signOutputs["signature"]; sig != "" {
				apps[i].ImageSignature = sig
				now := time.Now().UTC()
				apps[i].ImageSignedAt = &now
			}
			// cosign.go writes the bare sha256 too — prefer this over
			// build-image's digest because it was the actual input to
			// `cosign sign`.
			if dig := signOutputs["digest"]; dig != "" {
				apps[i].ImageDigestSHA256 = dig
			}
		}
		// Fallback: if cosign step was skipped or missing, fall back
		// to build-image digest.
		if apps[i].ImageDigestSHA256 == "" {
			if buildOutputs, ok := status.Outputs[buildStep]; ok {
				if dig := buildOutputs["digest"]; dig != "" {
					apps[i].ImageDigestSHA256 = dig
				}
			}
		}

		// Sprint 6 / T3.1: SBOM outputs (graceful degradation when absent).
		if sbomOutputs, ok := status.Outputs[sbomStep]; ok {
			if k := sbomOutputs["sbom_s3_key"]; k != "" {
				apps[i].SBOMS3Key = k
			}
			if f := sbomOutputs["sbom_format"]; f != "" {
				apps[i].SBOMFormat = f
			}
			if cs := sbomOutputs["sbom_component_count"]; cs != "" {
				if n, err := strconv.Atoi(cs); err == nil && n >= 0 {
					apps[i].SBOMComponentCount = n
				}
			}
		}

		// M07 Ralph iter 5 (plan 2026-05-28-M07-fulcio-cacert-keyless-LIVE v1.6
		// DoD item 9): attest-image step writes Rekor entry URL/index to
		// `rekor-url` output parameter (see cosign_attest.go outRekorURLPath).
		// Persist into BuildAppOutputs so container_delivery.go can write to
		// releases.image_attestation_rekor_url (migration 000019). Empty or
		// literal "unknown" (cosign output parser fallback) stays NULL —
		// downstream code already gates on empty before persisting.
		attestStep := naming.AttestStepName(app.Name)
		var rekorURL string
		if attestOutputs, ok := status.Outputs[attestStep]; ok {
			if url := attestOutputs["rekor-url"]; url != "" && url != "unknown" {
				apps[i].ImageAttestationRekorURL = url
				rekorURL = url
			}
		}

		// T4.1 (CR-8) of plan 2026-05-28-meet-001-engineering-closure v1.1:
		// emit Sigstore observability counters from the enrichment.
		// Duration histograms are intentionally NOT emitted here — they
		// require the cosign scripts to surface `sign_duration_ms` /
		// `attest_duration_ms` output params (plan T4.1 tasks #3+#4,
		// operator follow-up). The counters below are derived from the
		// existing outputs map and give immediate SLO signal.
		emitSigstoreObservations(status, app.Name, rekorURL)
	}
}

// applyReusedImages copies the stored reused image (digest, signature,
// signing time, SBOM) onto each app release whose build reused an image, and
// marks it reused. specs is the build's stored apps spec.
func applyReusedImages(apps []deploycontracts.AppRelease, specs []db.BuildAppSpec) {
	reused := make(map[string]*db.ReusedImage, len(specs))
	for _, spec := range specs {
		if spec.ReusedImage != nil {
			reused[spec.Name] = spec.ReusedImage
		}
	}
	for i := range apps {
		img, ok := reused[apps[i].Name]
		if !ok {
			continue
		}
		signedAt := img.SignedAt
		apps[i].ImageReused = true
		apps[i].ImageDigestSHA256 = img.Digest
		apps[i].ImageSignature = img.Signature
		apps[i].ImageSignedAt = &signedAt
		apps[i].SBOMS3Key = img.SBOMS3Key
		apps[i].SBOMFormat = img.SBOMFormat
		apps[i].SBOMComponentCount = img.SBOMComponentCount
	}
}

// emitSigstoreObservations is the metric-emission half of the
// enrichment. Split out so the unit tests in
// build_reconcile_complete_failure_test.go can keep asserting pure
// enrichment behavior while sigstore_observations_test.go pins the
// metric side-effects independently. EC-NEW (plan T4.1) guard: the
// duration histogram observation is skipped here entirely until
// `*_duration_ms` outputs land — emitting `Observe(0)` would create
// a misleading bucket-0 spike.
func emitSigstoreObservations(status *deploycontracts.WorkflowStatus, appName, rekorURL string) {
	if status == nil {
		return
	}
	// sign_total: success when the sign-image step produced a signature.
	signStep := naming.SignStepName(appName)
	signOK := false
	var signComponent string
	if out, ok := status.Outputs[signStep]; ok {
		if out["signature"] != "" {
			signOK = true
		} else {
			// Sign step ran but produced no signature → Fulcio failure
			// most likely (the cert issue step is where signless aborts
			// almost always originate). Operators can refine via the
			// step's exit code if surfaced.
			signComponent = "fulcio"
		}
	}
	metrics.ObserveCosignSign(signOK, signComponent)

	// Fulcio cert issuance duration (subset of cosign sign): wired forward-
	// compatibly. Operator follow-up to surface `fulcio_duration_ms` from the
	// cosign script will populate this with a real value; until then we observe
	// only when the workflow has produced the field, so no bucket-0 spike.
	// EC-NEW guard mirrors the surrounding sign/attest pattern.
	if out, ok := status.Outputs[signStep]; ok {
		if fulcioMs, err := strconv.ParseFloat(out["fulcio_duration_ms"], 64); err == nil && fulcioMs > 0 {
			metrics.ObserveFulcioCertIssuance(fulcioMs / 1000.0)
		}
	}

	// attest_total: success when the attest step produced a non-null URL.
	// Empty / "unknown" URL counts as failure with Rekor attribution.
	attestStep := naming.AttestStepName(appName)
	attestOK := false
	var attestComponent string
	if _, ok := status.Outputs[attestStep]; ok {
		if rekorURL != "" {
			attestOK = true
		} else {
			attestComponent = "rekor"
		}
	}
	metrics.ObserveCosignAttest(attestOK, attestComponent)

	// rekor_url null/populated SLI counter.
	metrics.ObserveRekorURLPersist(rekorURL)
}

// manifestFailureReason builds the message the user reads when the manifest
// stage fails.
//
// Until 2026-08-19 it was the CONSTANT "Failed to build manifests with custom
// domains": it named the stage and not the domain, the app or the reason, while
// the real error — for example `missing image digest for app "api"` — went only
// to the log (B-152). Whoever received the failure could not tell which app
// stopped.
//
// The reason passes through `scrub.Text`, which is what makes carrying it safe:
// the same table that keeps internal terms out of the timeline event protects
// this message. Without it, embedding raw error text would trade a useless
// message for a Rule 3 leak.
func manifestFailureReason(err error) string {
	if err == nil {
		return "Manifest build failed"
	}
	return "Manifest build failed: " + scrub.Text(err.Error())
}
