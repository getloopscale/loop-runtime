// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package naming

// UserSecretName is the Secret holding a user's secrets for one app — or, when
// appName is empty, for the whole environment. Secret sync writes it and the
// manifest renderer mounts it with envFrom, so both sides MUST derive it here:
// a writer and a reader that format the name separately drift apart silently.
// The empty-app form omits the segment instead of ending on a hyphen, which
// DNS-1123 forbids.
func UserSecretName(tenantID, projectID, envName, appName string) string {
	name := "user-secrets-" + tenantID + "-" + projectID + "-" + envName
	if appName == "" {
		return name
	}
	return name + "-" + appName
}

// TenantGitOpsRepoName is the repository that holds a tenant's deployment
// manifests, one per tenant in the platform's own organisation.
func TenantGitOpsRepoName(tenantID string) string {
	return tenantID + "-gitops"
}
