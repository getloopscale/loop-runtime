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
	Replicas                int                `json:"replicas"`
	ProgressDeadlineSeconds int                `json:"progressDeadlineSeconds,omitempty"`
	MinReadySeconds         int                `json:"minReadySeconds,omitempty"`
	Strategy                *k8sDeployStrategy `json:"strategy,omitempty"`
	Selector                k8sLabelSelector   `json:"selector"`
	Template                k8sPodTemplateSpec `json:"template"`
}

type k8sDeployStrategy struct {
	Type          string            `json:"type"`
	RollingUpdate *k8sRollingUpdate `json:"rollingUpdate,omitempty"`
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
	ServiceAccountName            string                 `json:"serviceAccountName,omitempty"`
	AutomountServiceAccountToken  *bool                  `json:"automountServiceAccountToken,omitempty"`
	SecurityContext               *k8sPodSecurityContext `json:"securityContext,omitempty"`
	Volumes                       []k8sVolume            `json:"volumes,omitempty"`
	Containers                    []k8sContainer         `json:"containers"`
	TerminationGracePeriodSeconds int                    `json:"terminationGracePeriodSeconds,omitempty"`
}

// k8sLifecycle carries the container lifecycle hooks (#61).
type k8sLifecycle struct {
	PreStop *k8sLifecycleHandler `json:"preStop,omitempty"`
}

// k8sLifecycleHandler intentionally exposes ONLY the native `sleep` action.
//
// `exec` is deliberately absent rather than merely unused: tenant images are
// arbitrary, and a distroless base has no /bin/sh (the crane image is one). A
// shell-based hook fails on
// exactly the most hardened images, and it fails at teardown, where the symptom
// is an abrupt connection drop rather than an error anyone reads. Leaving the
// field out means the mistake cannot be made here at all.
//
// The `sleep` action is GA since Kubernetes 1.30; the clusters run v1.32.13
// (infra/k3d/*.yaml).
type k8sLifecycleHandler struct {
	Sleep *k8sSleepAction `json:"sleep,omitempty"`
}

type k8sSleepAction struct {
	Seconds int `json:"seconds"`
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
	Name            string                       `json:"name"`
	Image           string                       `json:"image"`
	Ports           []k8sContainerPort           `json:"ports,omitempty"`
	Resources       k8sResources                 `json:"resources"`
	Lifecycle       *k8sLifecycle                `json:"lifecycle,omitempty"`
	Env             []k8sEnvVar                  `json:"env,omitempty"`
	EnvFrom         []k8sEnvFromSource           `json:"envFrom,omitempty"`
	SecurityContext *k8sContainerSecurityContext `json:"securityContext,omitempty"`
	VolumeMounts    []k8sVolumeMount             `json:"volumeMounts,omitempty"`
	StartupProbe    *k8sProbe                    `json:"startupProbe,omitempty"`
	ReadinessProbe  *k8sProbe                    `json:"readinessProbe,omitempty"`
	LivenessProbe   *k8sProbe                    `json:"livenessProbe,omitempty"`
}

type k8sVolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

type k8sVolume struct {
	Name     string       `json:"name"`
	EmptyDir *k8sEmptyDir `json:"emptyDir,omitempty"`
}

type k8sEmptyDir struct{}

type k8sContainerSecurityContext struct {
	AllowPrivilegeEscalation *bool            `json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool            `json:"readOnlyRootFilesystem,omitempty"`
	Capabilities             *k8sCapabilities `json:"capabilities,omitempty"`
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
	Name     string `json:"name"`
	Optional *bool  `json:"optional,omitempty"`
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
	APIVersion string                  `json:"apiVersion"`
	Kind       string                  `json:"kind"`
	Metadata   k8sMetadata             `json:"metadata"`
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
	Middlewares []traefikRouteMiddleware `json:"middlewares,omitempty"`
	Services    []traefikRouteService    `json:"services"`
}

type traefikRouteService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // cross-namespace reference
	// Kind is the resource kind of the target. Default (empty) treats the
	// reference as a Kubernetes Service. Set to "TraefikService" when the
	// route should target a weighted TraefikService for canary traffic
	// splitting (argo-rollouts-progressive-delivery-plan T1.4 / EC-2).
	Kind   string `json:"kind,omitempty"`
	Port   int    `json:"port,omitempty"`
	Scheme string `json:"scheme,omitempty"`
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
	SecretName string        `json:"secretName"`
	IssuerRef  certIssuerRef `json:"issuerRef"`
	DNSNames   []string      `json:"dnsNames"`
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

type traefikRouteMiddleware struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // cross-namespace reference
}

// --- KEDA types (scale-to-zero) ---

// kedaScaledObject represents a KEDA ScaledObject CRD.
// Generated instead of HPA when AutoscalingConfig.MinReplicas == 0.
type kedaScaledObject struct {
	APIVersion string               `json:"apiVersion"` // keda.sh/v1alpha1
	Kind       string               `json:"kind"`       // ScaledObject
	Metadata   k8sMetadata          `json:"metadata"`
	Spec       kedaScaledObjectSpec `json:"spec"`
}

type kedaScaledObjectSpec struct {
	ScaleTargetRef  kedaScaleTargetRef `json:"scaleTargetRef"`
	MinReplicaCount int                `json:"minReplicaCount"`
	MaxReplicaCount int                `json:"maxReplicaCount"`
	CooldownPeriod  int                `json:"cooldownPeriod,omitempty"`
	PollingInterval int                `json:"pollingInterval,omitempty"`
	Triggers        []kedaTrigger      `json:"triggers"`
}

type kedaScaleTargetRef struct {
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
	// APIVersion is the apiVersion of the scale target (kind+apiVersion
	// pair must match the actual CR). Required by KEDA when targeting
	// non-Deployment kinds (Rollout requires apiVersion: argoproj.io/v1alpha1).
	APIVersion string `json:"apiVersion,omitempty"`
}

type kedaTrigger struct {
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata"`
}

// kedaHTTPScaledObject represents a KEDA HTTP Add-on HTTPScaledObject CRD.
// Provides cold-start interceptor for scale-to-zero apps that receive HTTP traffic.
type kedaHTTPScaledObject struct {
	APIVersion string                   `json:"apiVersion"` // http.keda.sh/v1alpha1
	Kind       string                   `json:"kind"`       // HTTPScaledObject
	Metadata   k8sMetadata              `json:"metadata"`
	Spec       kedaHTTPScaledObjectSpec `json:"spec"`
}

type kedaHTTPScaledObjectSpec struct {
	Hosts                 []string               `json:"hosts"`
	TargetPendingRequests int                    `json:"targetPendingRequests,omitempty"`
	ScaleTargetRef        kedaHTTPScaleTargetRef `json:"scaleTargetRef"`
	Replicas              *kedaHTTPReplicas      `json:"replicas,omitempty"`
	ScaledownPeriod       int                    `json:"scaledownPeriod,omitempty"`
}

type kedaHTTPScaleTargetRef struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	Port    int    `json:"port"`
}

type kedaHTTPReplicas struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// --- Argo Rollouts types (progressive delivery) ---
//
// argo-rollouts-progressive-delivery-plan T1.1.
// Replaces k8sDeployment as the workload primitive when ProgressiveDelivery=true.
// API version argoproj.io/v1alpha1.

// k8sRollout is the Argo Rollouts Rollout CRD root.
type k8sRollout struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   k8sMetadata    `json:"metadata"`
	Spec       k8sRolloutSpec `json:"spec"`
}

type k8sRolloutSpec struct {
	Replicas                int                `json:"replicas"`
	RevisionHistoryLimit    int                `json:"revisionHistoryLimit,omitempty"`
	Selector                k8sLabelSelector   `json:"selector"`
	Template                k8sPodTemplateSpec `json:"template"`
	Strategy                k8sRolloutStrategy `json:"strategy"`
	ProgressDeadlineSeconds int                `json:"progressDeadlineSeconds,omitempty"`
}

type k8sRolloutStrategy struct {
	Canary *k8sCanaryStrategy `json:"canary,omitempty"`
}

type k8sCanaryStrategy struct {
	CanaryService  string             `json:"canaryService,omitempty"`
	StableService  string             `json:"stableService,omitempty"`
	TrafficRouting *k8sTrafficRouting `json:"trafficRouting,omitempty"`
	Steps          []k8sCanaryStep    `json:"steps"`
	MaxUnavailable int                `json:"maxUnavailable,omitempty"`
	MaxSurge       string             `json:"maxSurge,omitempty"`
	// ScaleDownDelaySeconds (#61) — how long the OLD ReplicaSet survives after
	// the canary is promoted. Pointer so 0 stays distinguishable from unset:
	// the zero value is a legitimate (if unwise) choice, and omitempty on an
	// int would silently drop it.
	ScaleDownDelaySeconds *int `json:"scaleDownDelaySeconds,omitempty"`
}

type k8sTrafficRouting struct {
	// Traefik configures the native Argo Rollouts Traefik traffic router.
	// Per official docs (argoproj/argo-rollouts/docs/features/traffic-management/traefik.md):
	//   - Traefik v3 (traefik.io API group) is the DEFAULT — no plugin needed.
	//   - Traefik v2 (legacy traefik.containo.us) requires controller flags
	//     --traefik-api-group=traefik.containo.us
	//     --traefik-api-version=traefik.containo.us/v1alpha1
	// LoopZero runs Traefik v3 so no flags needed.
	Traefik *k8sTraefikRouting `json:"traefik,omitempty"`
}

type k8sTraefikRouting struct {
	WeightedTraefikServiceName string `json:"weightedTraefikServiceName"`
}

// k8sCanaryStep is one of the discriminated-union steps Argo Rollouts
// executes in order. Exactly one of {SetWeight, Pause, Analysis} should
// be set per step. We use pointers so omitempty works correctly.
type k8sCanaryStep struct {
	SetWeight *int             `json:"setWeight,omitempty"`
	Pause     *k8sCanaryPause  `json:"pause,omitempty"`
	Analysis  *k8sStepAnalysis `json:"analysis,omitempty"`
}

type k8sCanaryPause struct {
	Duration string `json:"duration,omitempty"`
}

type k8sStepAnalysis struct {
	Templates []k8sAnalysisTemplateRef `json:"templates"`
	Args      []k8sAnalysisArg         `json:"args,omitempty"`
}

type k8sAnalysisTemplateRef struct {
	TemplateName string `json:"templateName"`
}

type k8sAnalysisArg struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// --- Traefik TraefikService types (weighted routing) ---
//
// argo-rollouts-progressive-delivery-plan T1.1 (D5).
// CRD provided by Traefik Proxy. Argo Rollouts modifies
// /spec/weighted/services[*].weight at runtime during canary steps.

type traefikWeightedService struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Metadata   k8sMetadata                `json:"metadata"`
	Spec       traefikWeightedServiceSpec `json:"spec"`
}

type traefikWeightedServiceSpec struct {
	Weighted traefikWeightedConfig `json:"weighted"`
}

type traefikWeightedConfig struct {
	Services []traefikServiceWeight `json:"services"`
}

type traefikServiceWeight struct {
	Name   string `json:"name"`
	Port   int    `json:"port"`
	Weight int    `json:"weight"`
}

// --- AnalysisTemplate types (Prometheus-driven canary validation) ---
//
// argo-rollouts-progressive-delivery-plan T1.1 (D3, D6).
// AnalysisTemplate is per-namespace (D3). EC-3: NaN inconclusive
// for first deploy. EC-7: consecutiveErrorLimit handles Prometheus
// outage. EC-14: query targets canary service name pattern.

type analysisTemplate struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   k8sMetadata          `json:"metadata"`
	Spec       analysisTemplateSpec `json:"spec"`
}

type analysisTemplateSpec struct {
	Args    []analysisTemplateArg    `json:"args,omitempty"`
	Metrics []analysisTemplateMetric `json:"metrics"`
}

type analysisTemplateArg struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type analysisTemplateMetric struct {
	Name                  string                   `json:"name"`
	Interval              string                   `json:"interval,omitempty"`
	InitialDelay          string                   `json:"initialDelay,omitempty"`
	Count                 int                      `json:"count,omitempty"`
	SuccessCondition      string                   `json:"successCondition,omitempty"`
	FailureCondition      string                   `json:"failureCondition,omitempty"`
	Inconclusive          string                   `json:"inconclusive,omitempty"` // EC-3
	FailureLimit          int                      `json:"failureLimit,omitempty"`
	ConsecutiveErrorLimit int                      `json:"consecutiveErrorLimit,omitempty"` // EC-7
	Provider              analysisTemplateProvider `json:"provider"`
}

type analysisTemplateProvider struct {
	Prometheus *analysisPrometheus `json:"prometheus,omitempty"`
}

type analysisPrometheus struct {
	Address string `json:"address"`
	Query   string `json:"query"`
}
