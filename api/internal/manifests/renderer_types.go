// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

// --- K8s resource struct types ---
// Local definitions to avoid heavy k8s/api imports.
// Only fields we actually set are included.

type k8sMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Deployment types

type k8sDeployment struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   k8sMetadata       `json:"metadata"`
	Spec       k8sDeploymentSpec `json:"spec"`
}

type k8sDeploymentSpec struct {
	Replicas                int                 `json:"replicas"`
	ProgressDeadlineSeconds int                 `json:"progressDeadlineSeconds,omitempty"`
	MinReadySeconds         int                 `json:"minReadySeconds,omitempty"`
	Strategy                *k8sDeployStrategy  `json:"strategy,omitempty"`
	Selector                k8sLabelSelector    `json:"selector"`
	Template                k8sPodTemplateSpec  `json:"template"`
}

type k8sDeployStrategy struct {
	Type          string             `json:"type"`
	RollingUpdate *k8sRollingUpdate  `json:"rollingUpdate,omitempty"`
}

type k8sRollingUpdate struct {
	MaxUnavailable int `json:"maxUnavailable"`
	MaxSurge       int `json:"maxSurge"`
}

type k8sLabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

type k8sPodTemplateSpec struct {
	Metadata k8sPodMeta `json:"metadata"`
	Spec     k8sPodSpec `json:"spec"`
}

type k8sPodMeta struct {
	Labels map[string]string `json:"labels"`
}

type k8sPodSpec struct {
	ServiceAccountName            string                  `json:"serviceAccountName,omitempty"`
	AutomountServiceAccountToken  *bool                   `json:"automountServiceAccountToken,omitempty"`
	SecurityContext               *k8sPodSecurityContext   `json:"securityContext,omitempty"`
	Volumes                       []k8sVolume             `json:"volumes,omitempty"`
	Containers                    []k8sContainer           `json:"containers"`
	TerminationGracePeriodSeconds int                      `json:"terminationGracePeriodSeconds,omitempty"`
}

type k8sPodSecurityContext struct {
	RunAsNonRoot   *bool              `json:"runAsNonRoot,omitempty"`
	RunAsUser      *int64             `json:"runAsUser,omitempty"`
	RunAsGroup     *int64             `json:"runAsGroup,omitempty"`
	FSGroup        *int64             `json:"fsGroup,omitempty"`
	SeccompProfile *k8sSeccompProfile `json:"seccompProfile,omitempty"`
}

type k8sSeccompProfile struct {
	Type string `json:"type"`
}

type k8sContainer struct {
	Name            string                      `json:"name"`
	Image           string                      `json:"image"`
	Ports           []k8sContainerPort          `json:"ports,omitempty"`
	Resources       k8sResources                `json:"resources"`
	Env             []k8sEnvVar                 `json:"env,omitempty"`
	EnvFrom         []k8sEnvFromSource          `json:"envFrom,omitempty"`
	SecurityContext *k8sContainerSecurityContext `json:"securityContext,omitempty"`
	VolumeMounts    []k8sVolumeMount            `json:"volumeMounts,omitempty"`
	StartupProbe    *k8sProbe                   `json:"startupProbe,omitempty"`
	ReadinessProbe  *k8sProbe                   `json:"readinessProbe,omitempty"`
	LivenessProbe   *k8sProbe                   `json:"livenessProbe,omitempty"`
}

type k8sVolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

type k8sVolume struct {
	Name     string        `json:"name"`
	EmptyDir *k8sEmptyDir  `json:"emptyDir,omitempty"`
}

type k8sEmptyDir struct{}

type k8sContainerSecurityContext struct {
	AllowPrivilegeEscalation *bool              `json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool              `json:"readOnlyRootFilesystem,omitempty"`
	Capabilities             *k8sCapabilities   `json:"capabilities,omitempty"`
}

type k8sCapabilities struct {
	Drop []string `json:"drop,omitempty"`
}

type k8sContainerPort struct {
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type k8sResources struct {
	Requests k8sResourceList `json:"requests"`
	Limits   k8sResourceList `json:"limits"`
}

type k8sResourceList struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type k8sEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type k8sEnvFromSource struct {
	SecretRef k8sSecretRef `json:"secretRef"`
}

type k8sSecretRef struct {
	Name string `json:"name"`
}

type k8sProbe struct {
	HTTPGet             k8sHTTPGet `json:"httpGet"`
	InitialDelaySeconds int        `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int        `json:"periodSeconds"`
	TimeoutSeconds      int        `json:"timeoutSeconds,omitempty"`
	SuccessThreshold    int        `json:"successThreshold,omitempty"`
	FailureThreshold    int        `json:"failureThreshold,omitempty"`
}

type k8sHTTPGet struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// PodDisruptionBudget types

type k8sPDB struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   k8sMetadata `json:"metadata"`
	Spec       k8sPDBSpec  `json:"spec"`
}

type k8sPDBSpec struct {
	MaxUnavailable             int              `json:"maxUnavailable"`
	UnhealthyPodEvictionPolicy string           `json:"unhealthyPodEvictionPolicy,omitempty"`
	Selector                   k8sLabelSelector `json:"selector"`
}

// Service types

type k8sService struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   k8sMetadata    `json:"metadata"`
	Spec       k8sServiceSpec `json:"spec"`
}

type k8sServiceSpec struct {
	Selector map[string]string `json:"selector"`
	Ports    []k8sServicePort  `json:"ports"`
}

type k8sServicePort struct {
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
	Protocol   string `json:"protocol"`
}

// Traefik IngressRoute types (CRD)

type traefikIngressRoute struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   k8sMetadata            `json:"metadata"`
	Spec       traefikIngressRouteSpec `json:"spec"`
}

type traefikIngressRouteSpec struct {
	EntryPoints []string       `json:"entryPoints"`
	Routes      []traefikRoute `json:"routes"`
	TLS         *traefikTLS    `json:"tls,omitempty"`
}

type traefikRoute struct {
	Match       string                   `json:"match"`
	Kind        string                   `json:"kind"`
	Middlewares []traefikRouteMiddleware  `json:"middlewares,omitempty"`
	Services    []traefikRouteService    `json:"services"`
}

type traefikRouteService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // cross-namespace reference
	Port      int    `json:"port"`
	Scheme    string `json:"scheme,omitempty"`
}

// traefikTLS configures TLS on IngressRoute.
type traefikTLS struct {
	SecretName string `json:"secretName"`
}

// certManagerCertificate represents a cert-manager Certificate CR.
type certManagerCertificate struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Metadata   k8sMetadata                `json:"metadata"`
	Spec       certManagerCertificateSpec `json:"spec"`
}

type certManagerCertificateSpec struct {
	SecretName string           `json:"secretName"`
	IssuerRef  certIssuerRef    `json:"issuerRef"`
	DNSNames   []string         `json:"dnsNames"`
}

type certIssuerRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// HorizontalPodAutoscaler types

type k8sHPA struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   k8sMetadata `json:"metadata"`
	Spec       k8sHPASpec  `json:"spec"`
}

type k8sHPASpec struct {
	ScaleTargetRef k8sScaleTargetRef `json:"scaleTargetRef"`
	MinReplicas    int               `json:"minReplicas"`
	MaxReplicas    int               `json:"maxReplicas"`
	Metrics        []k8sMetricSpec   `json:"metrics"`
}

type k8sScaleTargetRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type k8sMetricSpec struct {
	Type     string          `json:"type"`
	Resource k8sMetricTarget `json:"resource"`
}

type k8sMetricTarget struct {
	Name   string              `json:"name"`
	Target k8sMetricTargetSpec `json:"target"`
}

type k8sMetricTargetSpec struct {
	Type               string `json:"type"`
	AverageUtilization int    `json:"averageUtilization"`
}

// --- Traefik Middleware types (for static frontend) ---

type traefikMiddleware struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   k8sMetadata            `json:"metadata"`
	Spec       traefikMiddlewareSpec   `json:"spec"`
}

type traefikMiddlewareSpec struct {
	AddPrefix     *traefikAddPrefix     `json:"addPrefix,omitempty"`
	RedirectRegex *traefikRedirectRegex `json:"redirectRegex,omitempty"`
}

type traefikRedirectRegex struct {
	Regex       string `json:"regex"`
	Replacement string `json:"replacement"`
	Permanent   bool   `json:"permanent"`
}

type traefikAddPrefix struct {
	Prefix string `json:"prefix"`
}

type traefikRouteMiddleware struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // cross-namespace reference
}
