// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package manifests

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/pkg/naming"
	sigsyaml "sigs.k8s.io/yaml"
)

// appHostname returns the flat hostname for an app.
// Format: {tenant}-{project}-{app}-{env}.{baseDomain}
// This produces a single-level subdomain under baseDomain, which is compatible
// with wildcard TLS certificates (*.dev.getloopscale.com covers all apps).
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
// are SHARED across projects (`loopruntime-{tenant}-{env}`), so two projects with an
// app of the same name (very common: `api`, `web`, `worker`) would generate
// colliding `Deployment/api` + `Service/api` — the second deploy silently
// overwrites the first, and Service.spec.selector matches pods from BOTH
// projects (label-based). Discovered live during dogfood Phase 3 / journey-01:
// `solo-server` deploy returned the body of a previous `dogfood-fb` pod
// because Service `api` was shared.
//
// **User-facing impact**: zero. Hosts already include the project segment
// (`<tenant>-<project>-<app>-<env>.<base>`); CLI commands accept `--app <name>`
// and resolve via the `runtime.getloopscale.com/app-name` label (still populated). Internal
// K8s names are an implementation detail of the PaaS, never surfaced.
func resourceName(projectID, appName string) string {
	return fmt.Sprintf("%s-%s", projectID, appName)
}

// commonLabels returns the standard labels for all resources.
//
// `app` carries the namespace-unique resource name (project-app) so Service
// selectors don't cross project boundaries. `runtime.getloopscale.com/app-name` keeps the
// raw user-declared app name for CLI queries (logs, status, scale, etc.).
func commonLabels(ri renderScope, app AppManifestInput) map[string]string {
	return map[string]string{
		"app":                                  resourceName(ri.ProjectID, app.Name),
		"app.kubernetes.io/managed-by":         "loopruntime",
		"runtime.getloopscale.com/tenant":      ri.TenantID,
		"runtime.getloopscale.com/project":     ri.ProjectID,
		"runtime.getloopscale.com/app-name":    app.Name,
		"runtime.getloopscale.com/environment": ri.Environment,
		"runtime.getloopscale.com/app-type":    app.Type,
	}
}

// namespace returns the Kubernetes namespace for a tenant environment.
func int64Ptr(v int64) *int64 { return &v }

// intPtr returns a pointer to v. Used where 0 must stay distinguishable from
// unset — e.g. Rollout scaleDownDelaySeconds (#61).
func intPtr(v int) *int { return &v }

// renderScope is one render's input plus the namespace it renders into,
// resolved once through naming.Namespace so every resource agrees on it and an
// invalid one fails the render instead of reaching the cluster.
type renderScope struct {
	RenderInput
	ns string
}

func newRenderScope(ri RenderInput) (renderScope, error) {
	ns, err := naming.Namespace(ri.TenantID, ri.ProjectID, ri.Environment)
	if err != nil {
		return renderScope{}, fmt.Errorf("manifests: namespace: %w", err)
	}
	return renderScope{RenderInput: ri, ns: ns}, nil
}

// docSet collects rendered YAML documents and keeps the first marshal error,
// so a builder adds resources in a straight line and reports one failure.
type docSet struct {
	docs []string
	err  error
}

// add serializes v with sigs.k8s.io/yaml, which marshals to JSON first (sorted
// keys) and so produces the same output regardless of map iteration order.
func (d *docSet) add(v interface{}) {
	if d.err != nil {
		return
	}
	data, err := sigsyaml.Marshal(v)
	if err != nil {
		d.err = fmt.Errorf("manifests: marshal %T: %w", v, err)
		return
	}
	d.docs = append(d.docs, strings.TrimSpace(string(data)))
}

func (d *docSet) addAll(docs []string, err error) {
	if d.err != nil {
		return
	}
	if err != nil {
		d.err = err
		return
	}
	d.docs = append(d.docs, docs...)
}

func (d *docSet) result() ([]string, error) {
	if d.err != nil {
		return nil, d.err
	}
	return d.docs, nil
}

// maybeAutoscaler appends HPA or KEDA ScaledObject depending on min replicas.
// Scale-to-zero (KEDA) is only triggered when MinReplicas == 0 AND MaxReplicas > 0,
// because the caller must explicitly configure both to opt into scale-to-zero.
// A zero-value AutoscalingConfig (all fields 0) uses native HPA with defaults.
func maybeAutoscaler(docs *docSet, ri renderScope, app AppManifestInput) {
	if app.Autoscaling == nil {
		return
	}
	if isScaleToZero(app.Autoscaling) {
		docs.add(buildScaledObject(ri, app))
		if app.Type == ServiceTypeServer || app.Type == "" {
			docs.add(buildHTTPScaledObject(ri, app))
		}
		return
	}
	docs.add(buildHPA(ri, app))
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
func renderServerApp(ri renderScope, app AppManifestInput) ([]string, error) {
	port := app.Port
	if port == 0 {
		port = 3000 // sensible default
	}

	var docs docSet
	if ri.ProgressiveDelivery {
		// Rollout + 2 Services (stable/canary) + TraefikService.
		docs.add(buildRollout(ri, app, port))
		docs.add(buildPDB(ri, app))
		docs.add(buildStableService(ri, app, port))
		docs.add(buildCanaryService(ri, app, port))
		docs.add(buildTraefikService(ri, app, port))
		// AnalysisTemplate is only emitted when Prometheus is configured.
		if ri.PrometheusAddress != "" {
			docs.add(buildAnalysisTemplate(ri, app))
		}
	} else {
		// Legacy path (D8 fallback).
		docs.add(buildDeployment(ri, app, port))
		docs.add(buildPDB(ri, app))
		docs.add(buildService(ri, app, port))
	}
	docs.addAll(buildIngressResources(ri, app, port))
	maybeAutoscaler(&docs, ri, app)
	return docs.result()
}

// renderWorkerApp generates Deployment+PDB (legacy) or Rollout+PDB (progressive).
// Workers receive no traffic — no Service/IngressRoute/TraefikService.
// In progressive mode, Rollout uses pause-only steps (workerCanarySteps).
func renderWorkerApp(ri renderScope, app AppManifestInput) ([]string, error) {
	var docs docSet
	if ri.ProgressiveDelivery {
		docs.add(buildRollout(ri, app, 0))
	} else {
		docs.add(buildDeployment(ri, app, 0))
	}
	docs.add(buildPDB(ri, app))
	maybeAutoscaler(&docs, ri, app)
	return docs.result()
}

// T3.3 (plan simplify-deploy-loopzerokit-only v2.0): renderFrontendApp removed —
// frontend/static delivery is no longer supported (server|worker only).
