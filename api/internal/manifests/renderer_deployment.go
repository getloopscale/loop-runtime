// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"fmt"
)

// podBuildIDAnnotation stamps the pod template with the build it was rendered
// for.
//
// A workload controller starts new pods only when the pod template changes. A
// redeploy of the same commit delivers the same image digest, so without the
// stamp it changed nothing: no pod restarted, and a secret set since the last
// deploy (read through envFrom at process start) never reached the app.
//
// The build id, and not the release id: a release's manifests are rendered
// again when a custom domain or the scale changes, with the same build, and
// those renders must not restart the app.
const podBuildIDAnnotation = "loopzero.io/build-id"

// podTemplateMeta is the metadata of an app's pod template.
func podTemplateMeta(ri renderScope, labels map[string]string) k8sPodMeta {
	meta := k8sPodMeta{Labels: labels}
	if ri.BuildID != "" {
		meta.Annotations = map[string]string{podBuildIDAnnotation: ri.BuildID}
	}
	return meta
}

// buildDeployment creates a Deployment resource for an app.
func buildDeployment(ri renderScope, app AppManifestInput, port int) k8sDeployment {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	// Selector must use the project-scoped `app` label so two projects with
	// an app of the same name never share pods (Achado #17).
	selectorLabels := map[string]string{
		"app":                 resName,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}

	// The container, pod spec and replica decision are the ones the Rollout
	// uses: this is the incident-rollback path, and it must ship the same pod.
	podSpec := buildAppPodSpec(buildAppContainer(ri, app, port))

	return k8sDeployment{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
			Annotations: map[string]string{
				"loopzero.io/build-id":   ri.BuildID,
				"loopzero.io/release-id": ri.ReleaseID,
			},
		},
		Spec: k8sDeploymentSpec{
			Replicas:                replicasForApp(app),
			ProgressDeadlineSeconds: 210,
			MinReadySeconds:         10,
			Strategy: &k8sDeployStrategy{
				Type: "RollingUpdate",
				RollingUpdate: &k8sRollingUpdate{
					MaxUnavailable: 0,
					MaxSurge:       1,
				},
			},
			Selector: k8sLabelSelector{MatchLabels: selectorLabels},
			Template: k8sPodTemplateSpec{
				Metadata: podTemplateMeta(ri, labels),
				Spec:     podSpec,
			},
		},
	}
}

// buildHPA creates a HorizontalPodAutoscaler for an app.
// Only called when app.Autoscaling is non-nil.
func buildHPA(ri renderScope, app AppManifestInput) k8sHPA {
	ns := ri.ns
	labels := commonLabels(ri, app)

	as := app.Autoscaling
	minR := as.MinReplicas
	if minR < 1 {
		minR = 1
	}
	maxR := as.MaxReplicas
	if maxR < 1 {
		maxR = 3
	}
	targetCPU := as.TargetCPU
	if targetCPU < 1 {
		targetCPU = 80
	}

	resName := resourceName(ri.ProjectID, app.Name)
	// argo-rollouts-progressive-delivery-plan T3.1 — Target Rollout when
	// progressive delivery is enabled, Deployment otherwise (D8 fallback).
	scaleAPIVersion := "apps/v1"
	scaleKind := "Deployment"
	if ri.ProgressiveDelivery {
		scaleAPIVersion = "argoproj.io/v1alpha1"
		scaleKind = "Rollout"
	}
	metrics := []k8sMetricSpec{utilizationMetric("cpu", targetCPU)}
	if as.TargetMemory > 0 {
		metrics = append(metrics, utilizationMetric("memory", as.TargetMemory))
	}
	return k8sHPA{
		APIVersion: "autoscaling/v2",
		Kind:       "HorizontalPodAutoscaler",
		Metadata: k8sMetadata{
			Name:      resName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sHPASpec{
			ScaleTargetRef: k8sScaleTargetRef{
				APIVersion: scaleAPIVersion,
				Kind:       scaleKind,
				Name:       resName,
			},
			MinReplicas: minR,
			MaxReplicas: maxR,
			Metrics:     metrics,
		},
	}
}

// utilizationMetric is an HPA resource metric that targets an average
// utilization, in percent of the pods' request for that resource.
func utilizationMetric(resource string, percent int) k8sMetricSpec {
	return k8sMetricSpec{
		Type: "Resource",
		Resource: k8sMetricTarget{
			Name:   resource,
			Target: k8sMetricTargetSpec{Type: "Utilization", AverageUtilization: percent},
		},
	}
}

// buildScaledObject creates a KEDA ScaledObject for scale-to-zero apps.
// Only called when app.Autoscaling.MinReplicas == 0.
// KEDA manages the HPA internally — do NOT generate both HPA and ScaledObject.
func buildScaledObject(ri renderScope, app AppManifestInput) kedaScaledObject {
	as := app.Autoscaling
	maxR := as.MaxReplicas
	if maxR < 1 {
		maxR = 3
	}
	targetCPU := as.TargetCPU
	if targetCPU < 1 {
		targetCPU = 80
	}
	cooldown := as.CooldownPeriod
	if cooldown < 1 {
		cooldown = 300
	}

	resName := resourceName(ri.ProjectID, app.Name)

	// argo-rollouts-progressive-delivery-plan T3.1 — KEDA ScaledObject must
	// reference Rollout (kind+apiVersion) when progressive delivery is on.
	// Without explicit kind, KEDA defaults to Deployment which doesn't exist
	// in the progressive-delivery rendering.
	scaleTarget := kedaScaleTargetRef{Name: resName}
	if ri.ProgressiveDelivery {
		scaleTarget.Kind = "Rollout"
		scaleTarget.APIVersion = "argoproj.io/v1alpha1"
	}

	// EC-5 fix: when progressive delivery is on, ScaledObject takes ownership
	// of HPA scaling AFTER the Rollout finishes its canary steps. The
	// transfer-hpa-ownership annotation lets KEDA assume the HPA that Argo
	// Rollouts creates internally for the Rollout, instead of generating a
	// new conflicting HPA.
	annotations := map[string]string{}
	if ri.ProgressiveDelivery {
		annotations["scaledobject.keda.sh/transfer-hpa-ownership"] = "true"
	}

	triggers := []kedaTrigger{utilizationTrigger("cpu", targetCPU)}
	if as.TargetMemory > 0 {
		triggers = append(triggers, utilizationTrigger("memory", as.TargetMemory))
	}

	return kedaScaledObject{
		APIVersion: "keda.sh/v1alpha1",
		Kind:       "ScaledObject",
		Metadata: k8sMetadata{
			Name:        resName + "-scaledobject",
			Namespace:   ri.ns,
			Labels:      commonLabels(ri, app),
			Annotations: annotations,
		},
		Spec: kedaScaledObjectSpec{
			ScaleTargetRef:  scaleTarget,
			MinReplicaCount: 0,
			MaxReplicaCount: maxR,
			CooldownPeriod:  cooldown,
			PollingInterval: 30,
			Triggers:        triggers,
		},
	}
}

// utilizationTrigger is a KEDA resource trigger on an average utilization, in
// percent of the pods' request for that resource.
func utilizationTrigger(resource string, percent int) kedaTrigger {
	return kedaTrigger{
		Type:     resource,
		Metadata: map[string]string{"type": "Utilization", "value": fmt.Sprintf("%d", percent)},
	}
}

// buildHTTPScaledObject creates a KEDA HTTP Add-on interceptor for cold start.
// Only called for server/frontend apps with scale-to-zero (workers don't receive HTTP).
func buildHTTPScaledObject(ri renderScope, app AppManifestInput) kedaHTTPScaledObject {
	maxR := app.Autoscaling.MaxReplicas
	if maxR < 1 {
		maxR = 3
	}
	port := app.Port
	if port == 0 {
		port = 3000
	}

	// Collect all hostnames: auto-generated + custom domains
	hosts := []string{appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)}
	for _, cd := range app.CustomDomains {
		hosts = append(hosts, cd.Domain)
	}

	cooldown := app.Autoscaling.CooldownPeriod
	if cooldown < 1 {
		cooldown = 300
	}

	resName := resourceName(ri.ProjectID, app.Name)
	// argo-rollouts-progressive-delivery-plan T3.1 / EC-1 — when progressive
	// delivery is enabled, the canonical app Service no longer exists (replaced
	// by -stable / -canary). HTTPScaledObject must point its Service field at
	// the -stable Service so the interceptor proxies to running pods. The
	// Name (Rollout target) stays the same.
	serviceName := resName
	if ri.ProgressiveDelivery {
		serviceName = resName + "-stable"
	}
	return kedaHTTPScaledObject{
		APIVersion: "http.keda.sh/v1alpha1",
		Kind:       "HTTPScaledObject",
		Metadata: k8sMetadata{
			Name:      resName + "-httpso",
			Namespace: ri.ns,
			Labels:    commonLabels(ri, app),
		},
		Spec: kedaHTTPScaledObjectSpec{
			Hosts:                 hosts,
			TargetPendingRequests: 100,
			ScaleTargetRef: kedaHTTPScaleTargetRef{
				Name:    resName,
				Service: serviceName,
				Port:    port,
			},
			Replicas:        &kedaHTTPReplicas{Min: 0, Max: maxR},
			ScaledownPeriod: cooldown,
		},
	}
}

// buildPDB creates a PodDisruptionBudget for an app.
func buildPDB(ri renderScope, app AppManifestInput) k8sPDB {
	ns := ri.ns
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	selectorLabels := map[string]string{
		"app":                 resName,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}

	return k8sPDB{
		APIVersion: "policy/v1",
		Kind:       "PodDisruptionBudget",
		Metadata: k8sMetadata{
			Name:      resName + "-pdb",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sPDBSpec{
			MaxUnavailable:             1,
			UnhealthyPodEvictionPolicy: "AlwaysAllow",
			Selector:                   k8sLabelSelector{MatchLabels: selectorLabels},
		},
	}
}
