// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/pkg/naming"
	sigsyaml "sigs.k8s.io/yaml"
)

// appHostname returns the flat hostname for an app.
// Format: {tenant}-{project}-{app}-{env}.{baseDomain}
// This produces a single-level subdomain under baseDomain, which is compatible
// with wildcard TLS certificates (*.dev.loopzero.example covers all apps).
//
// FAANG canonical (J-25 sponsor walkthrough catch 2026-06-12): delegate to
// pkg/naming.AppHostname so the IngressRoute Host emitter AND the public
// URL emitter (status.go) share single source of truth for the formula.
// Previously the in-line composition here was duplicated (and subtly
// diverged) at api/internal/routes/status.go:252 — the simplified formula
// at status.go did NOT match the IngressRoute Host emitted by this
// renderer, so dashboard "Visit" URLs never matched any real Traefik
// route. Single source of truth in pkg/naming closes that catch.
func appHostname(tenantID, projectID, appName, environment, baseDomain string) string {
	return naming.AppHostname(tenantID, projectID, appName, environment, baseDomain)
}

// resourceName returns the namespace-unique name to use for K8s resources
// (Deployment, Service, IngressRoute, Certificate, HPA, ScaledObject, PDB)
// generated for an app inside a tenant environment namespace.
//
// **Why `<project>-<app>` instead of `<app>`**: tenant environment namespaces
// are SHARED across projects (`loopzero-{tenant}-{env}`), so two projects with an
// app of the same name (very common: `api`, `web`, `worker`) would generate
// colliding `Deployment/api` + `Service/api` — the second deploy silently
// overwrites the first, and Service.spec.selector matches pods from BOTH
// projects (label-based). Discovered live during dogfood Phase 3 / journey-01:
// `solo-server` deploy returned the body of a previous `dogfood-fb` pod
// because Service `api` was shared.
//
// **User-facing impact**: zero. Hosts already include the project segment
// (`<tenant>-<project>-<app>-<env>.<base>`); CLI commands accept `--app <name>`
// and resolve via the `loopzero.io/app-name` label (still populated). Internal
// K8s names are an implementation detail of the PaaS, never surfaced.
func resourceName(projectID, appName string) string {
	return fmt.Sprintf("%s-%s", projectID, appName)
}

// commonLabels returns the standard labels for all resources.
//
// `app` carries the namespace-unique resource name (project-app) so Service
// selectors don't cross project boundaries. `loopzero.io/app-name` keeps the
// raw user-declared app name for CLI queries (logs, status, scale, etc.).
func commonLabels(ri RenderInput, app AppManifestInput) map[string]string {
	return map[string]string{
		"app":                          resourceName(ri.ProjectID, app.Name),
		"app.kubernetes.io/managed-by": "loopzero",
		"loopzero.io/tenant":               ri.TenantID,
		"loopzero.io/project":              ri.ProjectID,
		"loopzero.io/app-name":             app.Name,
		"loopzero.io/environment":          ri.Environment,
		"loopzero.io/app-type":             app.Type,
	}
}

// namespace returns the Kubernetes namespace for a tenant environment.
func int64Ptr(v int64) *int64 { return &v }

// intPtr returns a pointer to v. Used where 0 must stay distinguishable from
// unset — e.g. Rollout scaleDownDelaySeconds (#61).
func intPtr(v int) *int { return &v }

// namespace returns the canonical Kubernetes namespace for a render
// target. ONE formula for ALL environments (production, staging,
// preview-pr-N, ...) — `loopzero-<tenant>-<project>-<env>`. The isolation
// boundary in LoopZero is the (tenant, project, env) triple; the namespace
// reflects that uniformly so no env type needs special-case branching.
//
// J-23 sponsor-directed canonical refactor 2026-06-07 (verbatim
// "TUDO DEVERIA esta separado por projeto"): single uniform formula, // english-only: verbatim sponsor quote
// no if/else for previews. Thin wrapper over pkg/naming.Namespace so
// the renderer is the only place that emits the namespace string;
// downstream consumers (tenant_gitops, preview controller, ArgoCD
// destination) call the same helper.
func namespace(tenantID, projectID, environment string) string {
	return fmt.Sprintf("loopzero-%s-%s-%s", tenantID, projectID, environment)
}

// marshalResource serializes a struct to YAML using sigs.k8s.io/yaml.
// This library marshals to JSON first (sorted keys) then converts to YAML,
// guaranteeing deterministic output regardless of map iteration order.
func marshalResource(v interface{}) string {
	data, err := sigsyaml.Marshal(v)
	if err != nil {
		// This should never happen with our controlled struct types.
		// If it does, it is a programming error — panic is appropriate.
		panic(fmt.Sprintf("manifests: failed to marshal resource: %v", err))
	}
	return strings.TrimSpace(string(data))
}

// maybeAutoscaler appends HPA or KEDA ScaledObject depending on min replicas.
// Scale-to-zero (KEDA) is only triggered when MinReplicas == 0 AND MaxReplicas > 0,
// because the caller must explicitly configure both to opt into scale-to-zero.
// A zero-value AutoscalingConfig (all fields 0) uses native HPA with defaults.
func maybeAutoscaler(docs []string, ri RenderInput, app AppManifestInput) []string {
	if app.Autoscaling == nil {
		return docs
	}
	if isScaleToZero(app.Autoscaling) {
		docs = append(docs, marshalResource(buildScaledObject(ri, app)))
		if app.Type == "server" || app.Type == "" {
			docs = append(docs, marshalResource(buildHTTPScaledObject(ri, app)))
		}
		return docs
	}
	docs = append(docs, marshalResource(buildHPA(ri, app)))
	return docs
}

// isScaleToZero returns true when autoscaling is explicitly configured for
// scale-to-zero. MinReplicas must be 0 AND MaxReplicas must be > 0 (explicitly set).
// A zero-value AutoscalingConfig (all 0s) is treated as HPA with defaults, not KEDA.
// Returns false for nil AutoscalingConfig (no autoscaling configured).
func isScaleToZero(as *AutoscalingConfig) bool {
	return as != nil && as.MinReplicas == 0 && as.MaxReplicas > 0
}

// renderServerApp generates Deployment + Service + IngressRoute for a server app.
//
// argo-rollouts-progressive-delivery-plan T1.4: when ProgressiveDelivery=true,
// emits Rollout + 2 Services (stable+canary) + TraefikService + AnalysisTemplate
// (when PrometheusAddress configured). IngressRoute points to the TraefikService
// for weighted routing. Legacy path (Deployment + Service) preserved when flag false.
func renderServerApp(ri RenderInput, app AppManifestInput) []string {
	port := app.Port
	if port == 0 {
		port = 3000 // sensible default
	}

	pdb := buildPDB(ri, app)
	ingressResources := buildIngressResources(ri, app, port)

	var docs []string
	if ri.ProgressiveDelivery {
		// Rollout + 2 Services (stable/canary) + TraefikService.
		rollout := buildRollout(ri, app, port)
		stableSvc := buildStableService(ri, app, port)
		canarySvc := buildCanaryService(ri, app, port)
		traefikSvc := buildTraefikService(ri, app, port)
		docs = []string{
			marshalResource(rollout),
			marshalResource(pdb),
			marshalResource(stableSvc),
			marshalResource(canarySvc),
			marshalResource(traefikSvc),
		}
		// AnalysisTemplate is only emitted when Prometheus is configured.
		if ri.PrometheusAddress != "" {
			docs = append(docs, marshalResource(buildAnalysisTemplate(ri, app)))
		}
	} else {
		// Legacy path (D8 fallback).
		deployment := buildDeployment(ri, app, port)
		service := buildService(ri, app, port)
		docs = []string{
			marshalResource(deployment),
			marshalResource(pdb),
			marshalResource(service),
		}
	}
	docs = append(docs, ingressResources...)
	docs = maybeAutoscaler(docs, ri, app)
	return docs
}

// renderWorkerApp generates Deployment+PDB (legacy) or Rollout+PDB (progressive).
// Workers receive no traffic — no Service/IngressRoute/TraefikService.
// In progressive mode, Rollout uses pause-only steps (workerCanarySteps).
func renderWorkerApp(ri RenderInput, app AppManifestInput) []string {
	pdb := buildPDB(ri, app)
	if ri.ProgressiveDelivery {
		rollout := buildRollout(ri, app, 0)
		docs := []string{marshalResource(rollout), marshalResource(pdb)}
		docs = maybeAutoscaler(docs, ri, app)
		return docs
	}
	deployment := buildDeployment(ri, app, 0)
	docs := []string{marshalResource(deployment), marshalResource(pdb)}
	docs = maybeAutoscaler(docs, ri, app)
	return docs
}

// T3.3 (plan simplify-deploy-loopzerokit-only v2.0): renderFrontendApp removed —
// frontend/static delivery is no longer supported (server|worker only).
