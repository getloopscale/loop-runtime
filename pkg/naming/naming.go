// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package naming provides deterministic naming functions for all LoopZero
// platform resources. Every structural name (namespace, repo, hostname,
// ArgoCD app, cert secret, labels) MUST be generated through this package.
// All names conform to RFC 1123: lowercase alphanumeric + hyphens, max 63 chars.
//
// This package is pure Go with zero external dependencies (no K8s imports).
package naming

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxKubernetesNameLength is the maximum length for a Kubernetes resource name
// per RFC 1123 (DNS label standard).
const MaxKubernetesNameLength = 63

// validNameRegexp matches a valid RFC 1123 DNS label:
// lowercase alphanumeric, hyphens allowed (not at start or end), max 63 chars.
var validNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// nonAlphanumRegexp matches any character that is NOT lowercase alphanumeric.
var nonAlphanumRegexp = regexp.MustCompile(`[^a-z0-9]+`)

// Sanitize converts an arbitrary string into an RFC 1123 compliant name.
// It lowercases, replaces non-alphanumeric characters with hyphens,
// collapses consecutive hyphens, trims leading/trailing hyphens, and
// truncates to MaxKubernetesNameLength. Returns empty string for
// inputs that produce no valid characters.
func Sanitize(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > MaxKubernetesNameLength {
		s = s[:MaxKubernetesNameLength]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// ValidateKubernetesName checks whether a name conforms to RFC 1123 DNS label
// rules. Returns an error describing the violation, or nil if the name is valid.
func ValidateKubernetesName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(name) > MaxKubernetesNameLength {
		return fmt.Errorf("name must be at most %d characters, got %d", MaxKubernetesNameLength, len(name))
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("name must be lowercase")
	}
	if !validNameRegexp.MatchString(name) {
		return fmt.Errorf("name must match RFC 1123: lowercase alphanumeric and hyphens, cannot start or end with hyphen")
	}
	return nil
}

// TenantNamespace returns the namespace name for a tenant.
// tenantName is the K8s CRD resource name (e.g., "tn-loopzero").
// Format: "loopzero-{tenantName}" — maintains compatibility with deployed resources.
func TenantNamespace(tenantName string) (string, error) {
	if tenantName == "" {
		return "", fmt.Errorf("tenant name must not be empty")
	}
	name := fmt.Sprintf("loopzero-%s", tenantName)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("tenant namespace %q: %w", name, err)
	}
	return name, nil
}

// EnvironmentNamespace returns the namespace name for a tenant's environment.
// tenantName is the K8s CRD resource name (e.g., "tn-loopzero").
// Format: "loopzero-{tenantName}-{envName}" — maintains compatibility with deployed resources.
func EnvironmentNamespace(tenantName, envName string) (string, error) {
	if tenantName == "" {
		return "", fmt.Errorf("tenant name must not be empty")
	}
	if envName == "" {
		return "", fmt.Errorf("environment name must not be empty")
	}
	name := fmt.Sprintf("loopzero-%s-%s", tenantName, envName)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("environment namespace %q: %w", name, err)
	}
	return name, nil
}

// BuildNamespace returns the build namespace name for a tenant.
// Format: "build-{tenantSlug}"
func BuildNamespace(tenantSlug string) (string, error) {
	if tenantSlug == "" {
		return "", fmt.Errorf("tenant slug must not be empty")
	}
	name := fmt.Sprintf("build-%s", tenantSlug)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("build namespace %q: %w", name, err)
	}
	return name, nil
}

// PreviewNamespace returns the namespace name for a preview environment.
// Format: "{tenantSlug}-{projectSlug}-pr-{prNumber}"
func PreviewNamespace(tenantSlug, projectSlug string, prNumber int) (string, error) {
	if tenantSlug == "" {
		return "", fmt.Errorf("tenant slug must not be empty")
	}
	if projectSlug == "" {
		return "", fmt.Errorf("project slug must not be empty")
	}
	if prNumber <= 0 {
		return "", fmt.Errorf("PR number must be positive, got %d", prNumber)
	}
	name := fmt.Sprintf("%s-%s-pr-%d", tenantSlug, projectSlug, prNumber)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("preview namespace %q: %w", name, err)
	}
	return name, nil
}

// RepoName returns the source code repository name.
// Format: "{teamSlug}-{projectSlug}"
func RepoName(teamSlug, projectSlug string) (string, error) {
	if teamSlug == "" {
		return "", fmt.Errorf("team slug must not be empty")
	}
	if projectSlug == "" {
		return "", fmt.Errorf("project slug must not be empty")
	}
	name := fmt.Sprintf("%s-%s", teamSlug, projectSlug)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("repo name %q: %w", name, err)
	}
	return name, nil
}

// GitOpsRepoName returns the GitOps repository name for a specific environment.
// Format: "{teamSlug}-{projectSlug}-{envName}"
func GitOpsRepoName(teamSlug, projectSlug, envName string) (string, error) {
	if teamSlug == "" {
		return "", fmt.Errorf("team slug must not be empty")
	}
	if projectSlug == "" {
		return "", fmt.Errorf("project slug must not be empty")
	}
	if envName == "" {
		return "", fmt.Errorf("environment name must not be empty")
	}
	name := fmt.Sprintf("%s-%s-%s", teamSlug, projectSlug, envName)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("gitops repo name %q: %w", name, err)
	}
	return name, nil
}

// Hostname returns the hostname for a service in an environment.
// Format: "{projectSlug}-{envName}.{baseDomain}"
// The subdomain part is sanitized to ensure RFC 1123 compliance.
// Returns an error if the sanitized subdomain is empty (all-special-char inputs).
func Hostname(projectSlug, envName, baseDomain string) (string, error) {
	subdomain := Sanitize(fmt.Sprintf("%s-%s", projectSlug, envName))
	if subdomain == "" {
		return "", fmt.Errorf("hostname subdomain is empty after sanitizing project %q and env %q", projectSlug, envName)
	}
	return fmt.Sprintf("%s.%s", subdomain, baseDomain), nil
}

// ArgoAppName returns the ArgoCD Application resource name.
// Format: "{teamSlug}-{projectSlug}-{envName}"
func ArgoAppName(teamSlug, projectSlug, envName string) (string, error) {
	if teamSlug == "" {
		return "", fmt.Errorf("team slug must not be empty")
	}
	if projectSlug == "" {
		return "", fmt.Errorf("project slug must not be empty")
	}
	if envName == "" {
		return "", fmt.Errorf("environment name must not be empty")
	}
	name := fmt.Sprintf("%s-%s-%s", teamSlug, projectSlug, envName)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("argo app name %q: %w", name, err)
	}
	return name, nil
}

// CertSecretName returns the TLS certificate secret name for a hostname.
// Format: "{sanitized-hostname}-tls"
// The hostname is sanitized (dots become hyphens) and truncated to fit
// within MaxKubernetesNameLength including the "-tls" suffix.
// Returns an error if the sanitized hostname is empty.
func CertSecretName(hostname string) (string, error) {
	const suffix = "-tls"
	maxBase := MaxKubernetesNameLength - len(suffix)
	sanitized := Sanitize(hostname)
	if sanitized == "" {
		return "", fmt.Errorf("cert secret name is empty after sanitizing hostname %q", hostname)
	}
	if len(sanitized) > maxBase {
		sanitized = sanitized[:maxBase]
		sanitized = strings.TrimRight(sanitized, "-")
	}
	return sanitized + suffix, nil
}

// GitRepoBindingName returns the GitRepositoryBinding CR name for a tenant's environment.
// Format: "{tenantName}-{envName}"
func GitRepoBindingName(tenantName, envName string) (string, error) {
	if tenantName == "" {
		return "", fmt.Errorf("tenant name must not be empty")
	}
	if envName == "" {
		return "", fmt.Errorf("environment name must not be empty")
	}
	name := fmt.Sprintf("%s-%s", tenantName, envName)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("git repo binding name %q: %w", name, err)
	}
	return name, nil
}

// ArgoRepoSecretName returns the ArgoCD repository Secret name for a tenant.
// Format: "repo-{tenantName}-gitops"
func ArgoRepoSecretName(tenantName string) (string, error) {
	if tenantName == "" {
		return "", fmt.Errorf("tenant name must not be empty")
	}
	name := fmt.Sprintf("repo-%s-gitops", tenantName)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("argo repo secret name %q: %w", name, err)
	}
	return name, nil
}

// Labels returns the standard LoopZero labels for a Kubernetes resource.
// All labels use the "loopzero.io/" prefix.
func Labels(tenant, project, env string) map[string]string {
	return map[string]string{
		"loopzero.io/tenant":      tenant,
		"loopzero.io/project":     project,
		"loopzero.io/environment": env,
		"loopzero.io/managed-by":  "loopzero",
	}
}

// ImageTag returns the container image reference for a built app.
// Format: "{registry}/{tenant}/{project}-{app}:{commitSHA}"
//
// ARCH-001 fix: This was duplicated in 4 independent sites (buildkit.go,
// build_plan.go, build_reconcile_loop.go x2). A format change in one site
// would cause image mismatch → RELEASING deadlock. Now there is ONE function.
func ImageTag(registry, tenant, project, app, commitSHA string) string {
	return fmt.Sprintf("%s/%s/%s-%s:%s", registry, tenant, project, app, commitSHA)
}
