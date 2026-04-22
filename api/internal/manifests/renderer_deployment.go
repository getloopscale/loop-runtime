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
	selectorLabels := map[string]string{
		"app":              app.Name,
		"loopzero.io/tenant":   ri.TenantID,
		"loopzero.io/project":  ri.ProjectID,
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
	//   - If autoscaling is set, use minReplicas as baseline (HPA manages scaling)
	//   - If explicit replicas configured, use that
	//   - Otherwise default to 1
	replicas := 1
	if app.Autoscaling != nil {
		minR := app.Autoscaling.MinReplicas
		if minR < 1 {
			minR = 1
		}
		replicas = minR
	} else if app.Replicas > 0 {
		replicas = app.Replicas
	}

	return k8sDeployment{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata: k8sMetadata{
			Name:      app.Name,
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

	return k8sHPA{
		APIVersion: "autoscaling/v2",
		Kind:       "HorizontalPodAutoscaler",
		Metadata: k8sMetadata{
			Name:      app.Name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: k8sHPASpec{
			ScaleTargetRef: k8sScaleTargetRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       app.Name,
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

// buildPDB creates a PodDisruptionBudget for an app.
func buildPDB(ri RenderInput, app AppManifestInput) k8sPDB {
	ns := namespace(ri.TenantID, ri.Environment)
	labels := commonLabels(ri, app)
	selectorLabels := map[string]string{
		"app":             app.Name,
		"loopzero.io/tenant":  ri.TenantID,
		"loopzero.io/project": ri.ProjectID,
	}

	return k8sPDB{
		APIVersion: "policy/v1",
		Kind:       "PodDisruptionBudget",
		Metadata: k8sMetadata{
			Name:      app.Name + "-pdb",
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
