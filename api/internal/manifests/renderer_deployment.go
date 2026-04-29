// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"fmt"
	"sort"
)

// buildDeployment creates a Deployment resource for an app.
func buildDeployment(ri RenderInput, app AppManifestInput, port int) k8sDeployment {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	// Selector must use the project-scoped `app` label so two projects with
	// an app of the same name never share pods (Achado #17).
	selectorLabels := map[string]string{
		"app":             resName,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}

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
		// Mount writable /tmp for frameworks that need temp dirs at runtime
		// (Java Spring, Python, etc). Required with readOnlyRootFilesystem=true.
		VolumeMounts: []k8sVolumeMount{
			{Name: "tmp", MountPath: "/tmp"},
		},
	}

	if port > 0 {
		container.Ports = []k8sContainerPort{
			{ContainerPort: port, Protocol: "TCP"},
		}
		// Inject PORT env var so the app knows which port to listen on.
		container.Env = append(container.Env, k8sEnvVar{
			Name: "PORT", Value: fmt.Sprintf("%d", port),
		})
		container.StartupProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 30, // 30 * 5s = 150s max startup time
		}
		container.ReadinessProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    10,
			TimeoutSeconds:   3,
			SuccessThreshold: 1,
			FailureThreshold: 3, // 3 * 10s = 30s before removing from Service
		}
		container.LivenessProbe = &k8sProbe{
			HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
			PeriodSeconds:    15,
			TimeoutSeconds:   3,
			SuccessThreshold: 1,
			FailureThreshold: 5, // 5 * 15s = 75s — more tolerant than readiness
		}
	}

	// Environment variables — sorted by key for determinism
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

	// Secret references via envFrom
	if len(app.Secrets) > 0 {
		secretRefName := fmt.Sprintf("user-secrets-%s-%s-%s-%s",
			ri.TenantID, ri.ProjectID, ri.Environment, app.Name)
		container.EnvFrom = []k8sEnvFromSource{
			{SecretRef: k8sSecretRef{Name: secretRefName}},
		}
	}

	// Determine replica count:
	//   - If scale-to-zero (KEDA): replicas = 0 (KEDA manages lifecycle)
	//   - If autoscaling with MinReplicas >= 1: use minReplicas as baseline (HPA manages scaling)
	//   - If explicit replicas configured, use that
	//   - Otherwise default to 1
	replicas := 1
	if app.Autoscaling != nil {
		if isScaleToZero(app.Autoscaling) {
			replicas = 0
		} else {
			minR := app.Autoscaling.MinReplicas
			if minR < 1 {
				minR = 1
			}
			replicas = minR
		}
	} else if app.Replicas > 0 {
		replicas = app.Replicas
	}

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
			Replicas:                replicas,
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
				Metadata: k8sPodMeta{Labels: labels},
				Spec: k8sPodSpec{
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
				},
			},
		},
	}
}

// buildHPA creates a HorizontalPodAutoscaler for an app.
// Only called when app.Autoscaling is non-nil.
func buildHPA(ri RenderInput, app AppManifestInput) k8sHPA {
	ns := namespace(ri.TenantID, ri.Environment)
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
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       resName,
			},
			MinReplicas: minR,
			MaxReplicas: maxR,
			Metrics: []k8sMetricSpec{
				{
					Type: "Resource",
					Resource: k8sMetricTarget{
						Name: "cpu",
						Target: k8sMetricTargetSpec{
							Type:               "Utilization",
							AverageUtilization: targetCPU,
						},
					},
				},
			},
		},
	}
}

// buildScaledObject creates a KEDA ScaledObject for scale-to-zero apps.
// Only called when app.Autoscaling.MinReplicas == 0.
// KEDA manages the HPA internally — do NOT generate both HPA and ScaledObject.
func buildScaledObject(ri RenderInput, app AppManifestInput) kedaScaledObject {
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
	return kedaScaledObject{
		APIVersion: "keda.sh/v1alpha1",
		Kind:       "ScaledObject",
		Metadata: k8sMetadata{
			Name:      resName + "-scaledobject",
			Namespace: namespace(ri.TenantID, ri.Environment),
			Labels:    commonLabels(ri, app),
		},
		Spec: kedaScaledObjectSpec{
			ScaleTargetRef:  kedaScaleTargetRef{Name: resName},
			MinReplicaCount: 0,
			MaxReplicaCount: maxR,
			CooldownPeriod:  cooldown,
			PollingInterval: 30,
			Triggers: []kedaTrigger{
				{
					Type: "cpu",
					Metadata: map[string]string{
						"type":  "Utilization",
						"value": fmt.Sprintf("%d", targetCPU),
					},
				},
			},
		},
	}
}

// buildHTTPScaledObject creates a KEDA HTTP Add-on interceptor for cold start.
// Only called for server/frontend apps with scale-to-zero (workers don't receive HTTP).
func buildHTTPScaledObject(ri RenderInput, app AppManifestInput) kedaHTTPScaledObject {
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
	return kedaHTTPScaledObject{
		APIVersion: "http.keda.sh/v1alpha1",
		Kind:       "HTTPScaledObject",
		Metadata: k8sMetadata{
			Name:      resName + "-httpso",
			Namespace: namespace(ri.TenantID, ri.Environment),
			Labels:    commonLabels(ri, app),
		},
		Spec: kedaHTTPScaledObjectSpec{
			Hosts:                 hosts,
			TargetPendingRequests: 100,
			ScaleTargetRef: kedaHTTPScaleTargetRef{
				Name:    resName,
				Service: resName,
				Port:    port,
			},
			Replicas:        &kedaHTTPReplicas{Min: 0, Max: maxR},
			ScaledownPeriod: cooldown,
		},
	}
}

// buildPDB creates a PodDisruptionBudget for an app.
func buildPDB(ri RenderInput, app AppManifestInput) k8sPDB {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	resName := resourceName(ri.ProjectID, app.Name)
	selectorLabels := map[string]string{
		"app":             resName,
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
