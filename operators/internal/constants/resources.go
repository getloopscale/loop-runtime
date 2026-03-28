// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package constants

// --- Secret Names ---
// These are the well-known names of Kubernetes Secrets created by controllers.
// CWTs and other components reference these by name — changing them is a breaking change.

const (
	// SecretMinIOCredentials is the per-tenant Secret with S3/MinIO credentials.
	// CWTs reference this via envFrom in Argo Workflow templates.
	SecretMinIOCredentials = "loopzero-minio-credentials"

	// SecretBuildKitRegistryConfig is the Docker config.json for BuildKit registry auth.
	SecretBuildKitRegistryConfig = "buildkit-registry-config"

	// SecretPlatformTokens is the platform-wide Secret storing Gitea/ArgoCD tokens.
	SecretPlatformTokens = "loopzero-platform-tokens"

	// SecretArgoCDInitialAdmin is the default ArgoCD admin password Secret.
	SecretArgoCDInitialAdmin = "argocd-initial-admin-secret"
)

// --- Secret Data Keys ---
// Keys inside Secret.Data maps. Must match what consumers expect.

const (
	KeyDockerConfigJSON    = "config.json"
	KeyGiteaToken          = "GITEA_TOKEN"
	KeyArgoCDToken         = "ARGOCD_TOKEN"
	KeyKubeconfig          = "kubeconfig"
	KeyUsername             = "username"
	KeyPassword             = "password"
	KeyAWSAccessKeyID      = "AWS_ACCESS_KEY_ID"
	KeyAWSSecretAccessKey  = "AWS_SECRET_ACCESS_KEY"
	KeyMinIOEndpoint       = "MINIO_ENDPOINT"
)

// --- ServiceAccount Names ---

const (
	// SABuildWorkflow is the ServiceAccount used by Argo Workflow pods in build namespaces.
	SABuildWorkflow = "loopzero-build-sa"

	// SADeployer is the ServiceAccount for ArgoCD-managed deployments in environment namespaces.
	SADeployer = "loopzero-deployer"
)

// --- ResourceQuota and Role Names ---

const (
	QuotaBuild       = "build-quota"
	LimitRangeBuild  = "build-limits"
	QuotaTenant      = "tenant-quota"
	LimitRangeTenant = "tenant-limits"

	RoleArgoExecutor        = "argo-workflow-executor"
	RoleBindingArgoExecutor = "argo-workflow-executor"
	// RoleWorkflowSubmitter allows the workflow-submitter SA (in argo ns) to create/manage
	// workflows in tenant build namespaces.
	RoleWorkflowSubmitter        = "loopzero-workflow-submitter"
	RoleBindingWorkflowSubmitter = "loopzero-workflow-submitter"
	// SAWorkflowSubmitter is the ServiceAccount name used by the API to submit workflows.
	SAWorkflowSubmitter   = "loopzero-workflow-submitter"
	NSArgoWorkflows       = "argo"
	RoleDeployer          = "loopzero-deployer"
	RoleBindingDeployer   = "loopzero-deployer"
)

// --- NetworkPolicy Names ---

const (
	NetworkPolicyBuildEgress      = "build-egress-restrict"
	NetworkPolicyDenyAll          = "deny-all"
	NetworkPolicyAllowIngress     = "allow-ingress-traefik"
	NetworkPolicyAllowEgress      = "allow-egress"
)

// --- ArgoCD Resource Name Patterns ---

// ArgoCDRepoSecretName returns the deterministic name for a GitRepositoryBinding's ArgoCD Secret.
func ArgoCDRepoSecretName(bindingName string) string {
	return "repo-" + bindingName + "-gitops"
}

// ArgoCDClusterSecretName returns the deterministic name for a ClusterCredential's ArgoCD Secret.
func ArgoCDClusterSecretName(clusterName string) string {
	return "cluster-" + clusterName
}

// ExposedRouteName returns the deterministic name for a LoopZeroApplication's ExposedRoute CR.
func ExposedRouteName(appName string) string {
	return appName + "-route"
}

// --- Default Token Name ---

const (
	DefaultGiteaTokenName       = "platform-bootstrap-token"
	DefaultArgoCDUsername        = "admin"
	DefaultArgoCDRepoUsername    = "loopzero-platform"
	// ArgoCDAPIAccount is the dedicated ArgoCD service account for the LoopZero API.
	// Configured in Helm values with capability: apiKey.
	// RBAC: sync + get on applications (least privilege).
	ArgoCDAPIAccount            = "loopzero-api"
)

// --- Probe Thresholds ---

const (
	// MaxConsecutiveProbeFailures is the number of consecutive probe failures
	// after which an ExposedRoute is marked as Failed.
	MaxConsecutiveProbeFailures = 5
)

// --- PriorityClass Configuration ---
// Plan-based scheduling priority for tenant workloads.
// Higher values = higher priority. Only Enterprise can preempt others.

const (
	PriorityClassStarter    = "loopzero-tenant-starter"
	PriorityClassGrowth     = "loopzero-tenant-growth"
	PriorityClassEnterprise = "loopzero-tenant-enterprise"

	PriorityValueStarter    int32 = 100
	PriorityValueGrowth     int32 = 500
	PriorityValueEnterprise int32 = 1000
)

// --- Default Service URLs ---
// In-cluster service URLs used as defaults when environment variables are not set.

const (
	// DefaultGiteaURL is the default in-cluster Gitea HTTP URL.
	DefaultGiteaURL = "http://gitea-http.loopzero-system.svc.cluster.local:3000"

	// DefaultArgoCDNamespace is the default namespace where ArgoCD is installed.
	DefaultArgoCDNamespace = "argocd"

	// DefaultWorkloadClusterURL is the default Kubernetes API server URL
	// (in-cluster endpoint for single-cluster or default workload cluster).
	DefaultWorkloadClusterURL = "https://kubernetes.default.svc"
)

// --- Environment ResourceQuota Limits ---
// Default hard limits for resources that are not tenant-configurable.

const (
	// QuotaPVCLimit is the max PersistentVolumeClaims per environment namespace.
	QuotaPVCLimit = "10"

	// QuotaLoadBalancerLimit prevents tenants from creating LoadBalancer services
	// (all traffic goes through the shared Traefik ingress).
	QuotaLoadBalancerLimit = "0"
)
