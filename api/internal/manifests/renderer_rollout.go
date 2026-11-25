// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package manifests — Argo Rollouts (canary progressive delivery).
//
// argo-rollouts-progressive-delivery-plan T1.3.
//
// This file implements the Rollout-based progressive delivery rendering:
//   - buildRollout: replaces buildDeployment when ProgressiveDelivery=true
//   - buildStableService / buildCanaryService: 2 Services for traffic split
//   - buildTraefikService: weighted routing CRD
//   - buildAnalysisTemplate: Prometheus-driven canary validation
//   - canaryStepsForPlan: per-plan canary strategy (D2)
//   - buildAppContainer / buildAppPodSpec: shared with buildDeployment
//
// The renderServerApp / renderWorkerApp / renderFrontendApp paths in
// renderer_helpers.go select between Deployment (legacy) and Rollout
// (progressive) based on ri.ProgressiveDelivery.

package manifests

import (
	"fmt"
	"sort"

	"github.com/getloopscale/loopzero/pkg/naming"
)

// buildRollout creates an Argo Rollouts Rollout resource for an app.
// Replaces buildDeployment when ProgressiveDelivery=true (D8).
//
// Workers (no traffic) get a Rollout with simple pause-only steps.
// Server/frontend apps get full canary with TraefikService weighted
// routing + Prometheus AnalysisTemplate when configured.
func buildRollout(ri renderScope, app AppManifestInput, port int) k8sRollout {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	selectorLabels := map[string]string{
		"app":                 resName,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}

	container := buildAppContainer(ri, app, port)
	podSpec := buildAppPodSpec(container)

	// #61 — how long the OLD ReplicaSet survives promotion.
	//
	// DECISION: LoopZero promises a long-lived connection GRACEFUL RECONNECTION,
	// not SURVIVAL.
	//
	// Survival would mean a connection outlives the process that owns it, which
	// requires handing session state between pods. LoopZero does not do that, and
	// building it is an architecture change, not a config value. Promising
	// survival and delivering a severed socket is worse than promising
	// reconnection and delivering it.
	//
	// Three settings implement that contract and no more:
	//   preStop 5s        endpoint removal wins the race (buildAppPodSpec)
	//   grace   30s       the app sends a close frame and drains (buildAppPodSpec)
	//   scaleDownDelay    the old generation is not torn down the instant the
	//                     canary is promoted — here
	//
	// 60s covers the pod's own teardown budget (5 + 30) with margin, so the
	// ReplicaSet is never removed while its pods are still draining. It is
	// deliberately NOT minutes: the old ReplicaSet holds quota and delays the
	// NEXT rollout — including the rollback reached for when a canary is
	// actively serving errors. Buying a guarantee LoopZero does not make, at cost
	// on the incident path, is the wrong trade.
	//
	// Stated explicitly rather than left to the controller default: an unstated
	// default is not a promise, because nobody can tell whether it may change.
	scaleDownDelay := 60
	replicas := replicasForApp(app)

	// EC-5 fix: when progressive delivery + scale-to-zero, Rollout starts
	// with replicas: 1 (not 0) so the canary step has pods to execute. KEDA
	// scales DOWN after rollout completes via transfer-hpa-ownership.
	if replicas == 0 {
		replicas = 1
	}

	// Worker apps: simple pause-only steps (no traffic to weight).
	// Server/frontend: full canary with TraefikService + AnalysisTemplate.
	var canary *k8sCanaryStrategy
	if app.Type == ServiceTypeWorker {
		canary = &k8sCanaryStrategy{
			Steps:                 workerCanarySteps(),
			ScaleDownDelaySeconds: &scaleDownDelay,
		}
	} else {
		canary = &k8sCanaryStrategy{
			ScaleDownDelaySeconds: &scaleDownDelay,
			StableService:         resName + "-stable",
			CanaryService:         resName + "-canary",
			TrafficRouting: &k8sTrafficRouting{
				// F-16 reverted (2026-05-07): use native Argo Rollouts
				// Traefik integration. Per docs/features/traffic-management/traefik.md
				// Traefik v3 (traefik.io API group) is the DEFAULT in
				// Argo Rollouts 1.7+ — no external plugin needed.
				Traefik: &k8sTraefikRouting{
					WeightedTraefikServiceName: resName,
				},
			},
			Steps: canaryStepsForPlan(ri.Plan, ri.PrometheusAddress != ""),
		}
	}

	// #61 — the OLD ReplicaSet must outlive the promotion.
	//
	// DECISION (2026-07-30): LoopZero promises a WebSocket GRACEFUL RECONNECTION,
	// not SURVIVAL. Survival would mean a connection outliving the process that
	// owns it, which needs session handoff between pods — an architecture
	// change, not a config value. Promising survival and delivering a severed
	// socket is worse than promising reconnection and delivering it.
	//
	// The teardown budget on a pod is preStop 5s + grace 30s (28a66a5c). This
	// field governs the generation ABOVE that: how long the old ReplicaSet
	// survives once the canary is promoted. Left unset it is whatever the
	// controller defaults to — and an unstated default is not a promise, because
	// nobody can tell whether it may be changed.
	//
	// 60s covers the 35s teardown with margin while staying short enough that
	// the NEXT rollout is not delayed. That matters most on the path nobody
	// plans for: the rollback reached for while a canary is serving errors. A
	// multi-minute window would buy a guarantee LoopZero does not make, and charge
	// for it during an incident.
	//
	// Applied to BOTH shapes — a worker has no traffic to weight, but it can
	// hold a long-running job, and the promise should not depend on app type.
	canary.ScaleDownDelaySeconds = intPtr(60)

	return k8sRollout{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Rollout",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
			Annotations: map[string]string{
				"loopzero.io/build-id":   ri.BuildID,
				"loopzero.io/release-id": ri.ReleaseID,
				// F-15 (2026-05-07 dogfood gate): sync-wave -1 was originally
				// added per EC-4 migration to make Rollout creation precede
				// legacy Deployment prune. In practice it broke fresh installs:
				// ArgoCD applied Rollout (wave -1) BEFORE Services (default
				// wave 0), Rollout immediately went Degraded (InvalidSpec —
				// stableService not found), and ArgoCD halted before applying
				// the Services. Wave-0-default keeps all resources in one
				// transaction; the Rollout's stableService validation finds
				// the Services after the apply completes.
			},
		},
		Spec: k8sRolloutSpec{
			Replicas:             replicas,
			RevisionHistoryLimit: 5,
			Selector:             k8sLabelSelector{MatchLabels: selectorLabels},
			Template: k8sPodTemplateSpec{
				Metadata: podTemplateMeta(ri, labels),
				Spec:     podSpec,
			},
			Strategy: k8sRolloutStrategy{Canary: canary},
			// progressDeadlineSeconds=300 (5min): when canary pods cannot reach
			// Ready within this window (e.g., readinessProbe /health returning
			// 5xx as in the rollout-bad-health fixture), Argo Rollouts marks
			// status.phase=Degraded. This is the pod-readiness abort path
			// available without Prometheus + AnalysisTemplate (which require
			// pro+ plan and a configured PrometheusAddress).
			//
			// 300s also fits inside the T5.2 E2E spec timeout of 8min.
			// The default 600s would exceed the test budget and cause flakes.
			ProgressDeadlineSeconds: 300,
		},
	}
}

// canaryStepsForPlan returns the canary step sequence for a tenant plan (D2).
// withAnalysis indicates whether Prometheus is configured; when false, the
// Analysis steps are replaced with extra Pause steps (still validates via
// pod health but skips Prometheus probe).
//
// Empty plan defaults to "starter" (most conservative — single 50/100 split).
func canaryStepsForPlan(plan string, withAnalysis bool) []k8sCanaryStep {
	switch plan {
	case "pro":
		steps := []k8sCanaryStep{
			intStep(20),
			pauseStep("30s"),
		}
		steps = append(steps, analysisOrPause(withAnalysis, "60s")...)
		steps = append(steps, intStep(50), pauseStep("30s"))
		return steps
	case "team", "enterprise":
		steps := []k8sCanaryStep{
			intStep(10),
			pauseStep("30s"),
		}
		steps = append(steps, analysisOrPause(withAnalysis, "60s")...)
		steps = append(steps, intStep(30), pauseStep("30s"))
		steps = append(steps, intStep(60), pauseStep("30s"))
		return steps
	default: // "starter" or empty
		return []k8sCanaryStep{
			intStep(50),
			pauseStep("60s"),
		}
	}
}

// workerCanarySteps returns simple steps for worker apps (no traffic split).
// Argo Rollouts still validates pod health between steps.
func workerCanarySteps() []k8sCanaryStep {
	return []k8sCanaryStep{
		pauseStep("30s"),
	}
}

func intStep(weight int) k8sCanaryStep {
	w := weight
	return k8sCanaryStep{SetWeight: &w}
}

func pauseStep(duration string) k8sCanaryStep {
	return k8sCanaryStep{Pause: &k8sCanaryPause{Duration: duration}}
}

func analysisOrPause(withAnalysis bool, fallbackPause string) []k8sCanaryStep {
	if withAnalysis {
		return []k8sCanaryStep{
			{Analysis: &k8sStepAnalysis{
				Templates: []k8sAnalysisTemplateRef{
					{TemplateName: canaryAnalysisTemplateName},
				},
				Args: []k8sAnalysisArg{},
			}},
		}
	}
	return []k8sCanaryStep{pauseStep(fallbackPause)}
}

// canaryAnalysisTemplateName is the AnalysisTemplate name referenced by
// Rollout canary steps. Per D3, the template is per-namespace, so this
// name is constant within each tenant env namespace.
const canaryAnalysisTemplateName = "loopzero-canary-analysis"

// buildStableService creates the stable Service for canary traffic split.
// Argo Rollouts manages spec.selector.rollouts-pod-template-hash to mark
// pods as stable. Initial selector matches all rollout pods; the controller
// injects the hash to scope it to the stable revision.
func buildStableService(ri renderScope, app AppManifestInput, port int) k8sService {
	resName := resourceName(ri.ProjectID, app.Name)
	return buildRolloutService(ri, app, resName+"-stable", port)
}

// buildCanaryService creates the canary Service for canary traffic split.
// Same selector as stable; Argo Rollouts injects hash to scope to canary
// revision pods.
func buildCanaryService(ri renderScope, app AppManifestInput, port int) k8sService {
	resName := resourceName(ri.ProjectID, app.Name)
	return buildRolloutService(ri, app, resName+"-canary", port)
}

func buildRolloutService(ri renderScope, app AppManifestInput, svcName string, port int) k8sService {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	selectorLabels := map[string]string{
		"app":                 resName,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}
	if port == 0 {
		port = 3000
	}
	return k8sService{
		APIVersion: "v1",
		Kind:       "Service",
		Metadata: k8sMetadata{
			Name:      svcName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sServiceSpec{
			Selector: selectorLabels,
			Ports: []k8sServicePort{
				{Port: port, TargetPort: port, Protocol: "TCP"},
			},
		},
	}
}

// buildTraefikService creates the TraefikService weighted routing CRD.
// Argo Rollouts modifies /spec/weighted/services/[*].weight at runtime
// during canary steps. ArgoCD must ignore those fields (T2.1).
//
// Initial state: stable=100, canary=0 (no traffic to canary until rollout
// progresses).
func buildTraefikService(ri renderScope, app AppManifestInput, port int) traefikWeightedService {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	if port == 0 {
		port = 3000
	}
	return traefikWeightedService{
		APIVersion: "traefik.io/v1alpha1",
		Kind:       "TraefikService",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: traefikWeightedServiceSpec{
			Weighted: traefikWeightedConfig{
				Services: []traefikServiceWeight{
					{Name: resName + "-stable", Port: port, Weight: 100},
					{Name: resName + "-canary", Port: port, Weight: 0},
				},
			},
		},
	}
}

// buildAnalysisTemplate creates the per-namespace AnalysisTemplate (D3).
// Queries Traefik service request rate on the canary service, comparing
// successful responses (non-5xx) against total. EC-3 fix: NaN inconclusive
// auto-promotes first deploy. EC-7 fix: consecutiveErrorLimit handles
// Prometheus outages gracefully. EC-14 fix: query targets canary svc name.
//
// Returns the template OR a zero-valued struct if PrometheusAddress is
// empty (caller checks before emitting). The renderer skips emission of
// the template doc in that case.
func buildAnalysisTemplate(ri renderScope, app AppManifestInput) analysisTemplate {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	canarySvcPattern := fmt.Sprintf("%s-%s-canary.*", ns, resName)
	return analysisTemplate{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "AnalysisTemplate",
		Metadata: k8sMetadata{
			Name:      canaryAnalysisTemplateName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: analysisTemplateSpec{
			Metrics: []analysisTemplateMetric{
				{
					Name:                  "success-rate",
					Interval:              "30s",
					InitialDelay:          "30s",
					Count:                 3,
					SuccessCondition:      "result[0] >= 0.95",
					FailureCondition:      "result[0] < 0.90",
					Inconclusive:          "isNaN(result[0])", // EC-3
					FailureLimit:          3,
					ConsecutiveErrorLimit: 4, // EC-7
					Provider: analysisTemplateProvider{
						Prometheus: &analysisPrometheus{
							Address: ri.PrometheusAddress,
							Query: fmt.Sprintf(
								`sum(rate(traefik_service_requests_total{service=~"%s",code!~"5.."}[2m])) / sum(rate(traefik_service_requests_total{service=~"%s"}[2m]))`,
								canarySvcPattern, canarySvcPattern,
							),
						},
					},
				},
			},
		},
	}
}

// --- Shared pod template helpers (extracted for DRY between Deployment and Rollout) ---

// buildAppContainer constructs the container spec from app config.
// Shared between buildDeployment (legacy) and buildRollout (progressive).
func buildAppContainer(ri renderScope, app AppManifestInput, port int) k8sContainer {
	falseVal := false
	trueVal := true

	container := k8sContainer{
		Name:  app.Name,
		Image: app.ImageRef,
		Resources: k8sResources{
			Requests: k8sResourceList{CPU: ri.cpuRequest(), Memory: ri.memoryRequest()},
			Limits:   k8sResourceList{CPU: ri.cpuLimit(), Memory: ri.memoryLimit()},
		},
		SecurityContext: &k8sContainerSecurityContext{
			AllowPrivilegeEscalation: &falseVal,
			ReadOnlyRootFilesystem:   &trueVal,
			Capabilities:             &k8sCapabilities{Drop: []string{"ALL"}},
		},
		VolumeMounts: []k8sVolumeMount{
			{Name: "tmp", MountPath: "/tmp"},
		},
	}

	if port > 0 {
		container.Ports = []k8sContainerPort{
			{ContainerPort: port, Protocol: "TCP"},
		}
		container.Env = append(container.Env, k8sEnvVar{
			Name: "PORT", Value: fmt.Sprintf("%d", port),
		})
		container.StartupProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 30,
		}
		container.ReadinessProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    10,
			TimeoutSeconds:   3,
			SuccessThreshold: 1,
			FailureThreshold: 3,
		}
		container.LivenessProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    15,
			TimeoutSeconds:   3,
			SuccessThreshold: 1,
			FailureThreshold: 5,
		}
	}

	if len(app.Env) > 0 {
		keys := make([]string, 0, len(app.Env))
		for k := range app.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			container.Env = append(container.Env, k8sEnvVar{Name: k, Value: app.Env[k]})
		}
	}

	// The Secrets secret sync writes from `loopscale secret set`: the environment's,
	// for a secret set without an app, then the app's, so the app's value wins on a
	// shared key. Both are mounted whether or not the app's configuration declares a
	// secret, because a secret set for the app is delivered either way; mounting only
	// declared ones left every undeclared secret out of the process. An app that
	// declares secrets requires its Secret, and the pod waits for it; otherwise both
	// are optional, so an app with no secret starts.
	optional := true
	appSecretOptional := len(app.Secrets) == 0
	container.EnvFrom = []k8sEnvFromSource{
		{SecretRef: k8sSecretRef{Name: naming.UserSecretName(ri.TenantID, ri.ProjectID, ri.Environment, ""), Optional: &optional}},
		{SecretRef: k8sSecretRef{Name: naming.UserSecretName(ri.TenantID, ri.ProjectID, ri.Environment, app.Name)}},
	}
	if appSecretOptional {
		container.EnvFrom[1].SecretRef.Optional = &optional
	}

	return container
}

// buildAppPodSpec constructs the pod spec wrapping a container.
// Shared between buildDeployment (legacy) and buildRollout (progressive).
func buildAppPodSpec(container k8sContainer) k8sPodSpec {
	falseVal := false
	trueVal := true

	// #61 — close the endpoint-removal race before SIGTERM.
	//
	// Deleting a pod does two things CONCURRENTLY: SIGTERM to the container,
	// and endpoint removal from Services. The second is asynchronous — kube-proxy
	// and Traefik take a moment to observe it — so without a hook the app begins
	// shutting down while new connections are still being routed to it. On a
	// canary step that scales the old ReplicaSet down, that is what severs
	// long-lived connections mid-flight.
	//
	// `terminationGracePeriodSeconds` does NOT address this: it bounds the time
	// AFTER SIGTERM, and the race happens before. The fix is to make the pod wait
	// so endpoint removal wins.
	//
	// preStopDrainSeconds is 5: endpoint propagation is typically 1-3s, so this
	// leaves margin while staying far below the 30s grace period — the sleep and
	// the app's own shutdown share that budget, and a sleep that consumed it
	// would get the app SIGKILLed the instant it finally received SIGTERM.
	//
	// This is the half of #61 that is NOT a product decision: the race harms
	// every rollout, WebSocket or not. How long a WebSocket should be allowed to
	// outlive a deploy — whether LoopZero promises survival or reconnection —
	// remains open on that issue.
	const preStopDrainSeconds = 5
	if container.Lifecycle == nil {
		container.Lifecycle = &k8sLifecycle{
			PreStop: &k8sLifecycleHandler{Sleep: &k8sSleepAction{Seconds: preStopDrainSeconds}},
		}
	}
	return k8sPodSpec{
		ServiceAccountName:           "loopzero-app",
		AutomountServiceAccountToken: &falseVal,
		SecurityContext: &k8sPodSecurityContext{
			RunAsNonRoot:   &trueVal,
			RunAsUser:      int64Ptr(1000),
			RunAsGroup:     int64Ptr(1000),
			FSGroup:        int64Ptr(1000),
			SeccompProfile: &k8sSeccompProfile{Type: "RuntimeDefault"},
		},
		Volumes: []k8sVolume{
			{Name: "tmp", EmptyDir: &k8sEmptyDir{}},
		},
		Containers:                    []k8sContainer{container},
		TerminationGracePeriodSeconds: 30,
	}
}

// replicasForApp computes the desired replica count, shared with buildDeployment.
//
// HA default per /chaos-test C03 fix (2026-05-14): when no explicit Autoscaling
// or Replicas configured, default to 2 (was 1). Single-replica tenant pods
// fail chaos test C03 (random-tenant-pod-kill) with 0% availability during
// chaos. 2 replicas + tenant PDB sustain chaos via pod-level fail-over.
//
// To opt out (cost-sensitive dev/test): set `apps.<name>.replicas: 1` in
// loopzero.yaml OR set `autoscaling.minReplicas: 1` for HPA-managed apps.
func replicasForApp(app AppManifestInput) int {
	replicas := 2 // HA default — overridable via app.Replicas or Autoscaling
	if app.Autoscaling != nil {
		if isScaleToZero(app.Autoscaling) {
			return 0
		}
		minR := app.Autoscaling.MinReplicas
		if minR < 1 {
			minR = 1
		}
		return minR
	}
	if app.Replicas > 0 {
		return app.Replicas
	}
	return replicas
}
