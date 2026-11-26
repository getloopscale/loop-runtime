// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package constants

import "github.com/getloopscale/loop-runtime/pkg/gitearepo"

// --- Secret Names ---
// These are the well-known names of Kubernetes Secrets created by controllers.
// CWTs and other components reference these by name — changing them is a breaking change.

const (
	// SecretMinIOCredentials is the per-tenant Secret with S3/MinIO credentials.
	// CWTs reference this via envFrom in Argo Workflow templates.
	SecretMinIOCredentials = "loopruntime-minio-credentials" //nolint:gosec // G101 false positive: Secret resource NAME, not credential value

	// SecretBuildKitRegistryConfig is the Docker config.json for BuildKit registry auth.
	SecretBuildKitRegistryConfig = "buildkit-registry-config" //nolint:gosec // G101 false positive: Secret resource NAME, not credential value

	// SecretPlatformTokens is the platform-wide Secret storing Gitea/ArgoCD tokens.
	SecretPlatformTokens = "loopruntime-platform-tokens"

	// SecretRegistryPull is the registry pull secret kept in
	// NamespacePlatformSystem and copied into every environment namespace.
	SecretRegistryPull = "loopruntime-system"

	// SecretArgoCDInitialAdmin is the default ArgoCD admin password Secret.
	SecretArgoCDInitialAdmin = "argocd-initial-admin-secret" //nolint:gosec // G101 false positive: Secret resource NAME, not credential value
)

// --- Secret Data Keys ---
// Keys inside Secret.Data maps. Must match what consumers expect.

const (
	KeyDockerConfigJSON   = "config.json"
	KeyGiteaToken         = "GITEA_TOKEN"
	KeyArgoCDToken        = "ARGOCD_TOKEN" //nolint:gosec // G101 false positive: env var KEY name, not credential value
	KeyKubeconfig         = "kubeconfig"
	KeyUsername           = "username"
	KeyPassword           = "password"
	KeyAWSAccessKeyID     = "AWS_ACCESS_KEY_ID"
	KeyAWSSecretAccessKey = "AWS_SECRET_ACCESS_KEY" //nolint:gosec // G101 false positive: env var KEY name, not credential value
	KeyMinIOEndpoint      = "MINIO_ENDPOINT"
)

// --- ServiceAccount Names ---

const (
	// SABuildWorkflow is the ServiceAccount used by Argo Workflow pods in build namespaces.
	SABuildWorkflow = "loopruntime-build-sa"

	// SADeployer is the ServiceAccount for ArgoCD-managed deployments in environment namespaces.
	SADeployer = "loopruntime-deployer"

	// SAApp is the ServiceAccount tenant workloads run as in environment and
	// preview namespaces; it carries the registry pull secret.
	SAApp = "loopruntime-app"
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
	RoleWorkflowSubmitter        = "loopruntime-workflow-submitter"
	RoleBindingWorkflowSubmitter = "loopruntime-workflow-submitter"
	// SAWorkflowSubmitter is the ServiceAccount name used by the API to submit workflows.
	SAWorkflowSubmitter = "loopruntime-workflow-submitter"
	NSArgoWorkflows     = "argo"
	RoleDeployer        = "loopruntime-deployer"
	RoleBindingDeployer = "loopruntime-deployer"
)

// --- NetworkPolicy Names ---

const (
	NetworkPolicyBuildEgress  = "build-egress-restrict"
	NetworkPolicyDenyAll      = "deny-all"
	NetworkPolicyAllowIngress = "allow-ingress-traefik"
	NetworkPolicyAllowEgress  = "allow-egress"
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

// ExposedRouteName returns the deterministic name for a LoopRuntimeApplication's ExposedRoute CR.
func ExposedRouteName(appName string) string {
	return appName + "-route"
}

// --- Default Token Name ---

const (
	DefaultGiteaTokenName     = "platform-bootstrap-token"
	DefaultArgoCDUsername     = "admin"
	DefaultArgoCDRepoUsername = "loopruntime-platform"
	// ArgoCDAPIAccount is the dedicated ArgoCD service account for the LoopRuntime API.
	// Configured in Helm values with capability: apiKey.
	// RBAC: sync + get on applications (least privilege).
	ArgoCDAPIAccount = "loopruntime-api"
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
	PriorityClassStarter    = "loopruntime-tenant-starter"
	PriorityClassGrowth     = "loopruntime-tenant-growth"
	PriorityClassEnterprise = "loopruntime-tenant-enterprise"

	PriorityValueStarter    int32 = 100
	PriorityValueGrowth     int32 = 500
	PriorityValueEnterprise int32 = 1000
)

// --- Default Service URLs ---
// In-cluster service URLs used as defaults when environment variables are not set.

const (
	// DefaultGiteaURL is the default in-cluster Gitea HTTP URL. It is the
	// platform-wide gitearepo.DefaultInternalURL, so the operators and the api
	// fall back to the same Gitea (issue #354).
	DefaultGiteaURL = gitearepo.DefaultInternalURL

	// DefaultGiteaOrg is the Gitea organisation that owns the tenant gitops
	// repositories when the GITEA_ORG environment variable is unset. It must agree
	// with the default the API applies for the same variable (see the GiteaOrg
	// field in api/internal/config), because both sides address the same
	// repository and neither notices the other's organisation.
	//
	// B-191 corrected the direction this comment used to state. It is the TENANT
	// OPERATOR that creates the repository -- reconcileGitRepo calls
	// GitProvisioner.EnsureRepo on tenant.Spec.GitOps.RepoName and writes the
	// resulting URL into Status.GitOpsRepo, which reconcileArgoCDApp then uses
	// verbatim as the ArgoCD Application repoURL. The API is the side that PUSHES:
	// its gitops pusher targets "{tenantID}-gitops" in the API's own organisation.
	// So the operator's organisation is the one ArgoCD syncs and the API's is the
	// one the manifests land in. A disagreement is silent in both directions --
	// the pusher creates its own copy, so the push succeeds into a repository
	// nothing watches while ArgoCD keeps syncing an empty one. That is B-069.
	//
	// Agreement across every declaring site is asserted by
	// infra/tests/config/gitea_org_agreement_test.go, not by this sentence.
	//
	// Deliberately NOT written here as the API's call expression. The operator
	// chart-completeness guard discovers a binary's env reads by grepping source
	// text for the call spelling, without masking comments, and this package is
	// imported by all four operator binaries -- so writing it that way made three
	// binaries that never touch the variable appear to read it. That blind spot is
	// B-190; this comment simply stays out of its way.
	DefaultGiteaOrg = "loopruntime"

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
