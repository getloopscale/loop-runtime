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

// composeName builds a Kubernetes resource name using the given format
// + args, then validates it as RFC 1123. resourceKind is the human-readable
// kind used in error messages (e.g., "tenant namespace"). All variadic
// string args must be non-empty — emits a single canonical "argument must
// not be empty" error if any are. Phase 3 helper: eliminated ~10 inline
// copies of the "validate inputs → sprintf → validate name → wrap error"
// scaffold from the 8 *Namespace/*Name composers below.
func composeName(resourceKind, format string, args ...string) (string, error) {
	for _, a := range args {
		if a == "" {
			return "", fmt.Errorf("%s: argument must not be empty", resourceKind)
		}
	}
	ifaceArgs := make([]interface{}, len(args))
	for i, a := range args {
		ifaceArgs[i] = a
	}
	name := fmt.Sprintf(format, ifaceArgs...)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("%s %q: %w", resourceKind, name, err)
	}
	return name, nil
}

// TenantNamespace returns the namespace name for a tenant.
// tenantName is the K8s CRD resource name (e.g., "tn-loopzero").
// Format: "loopzero-{tenantName}" — maintains compatibility with deployed resources.
func TenantNamespace(tenantName string) (string, error) {
	return composeName("tenant namespace", "loopzero-%s", tenantName)
}

// EnvironmentNamespace returns the namespace name for a tenant's environment.
// tenantName is the K8s CRD resource name (e.g., "tn-loopzero").
// Format: "loopzero-{tenantName}-{envName}" — maintains compatibility with deployed resources.
func EnvironmentNamespace(tenantName, envName string) (string, error) {
	return composeName("environment namespace", "loopzero-%s-%s", tenantName, envName)
}

// BuildNamespace returns the build namespace name for a tenant.
// Format: "build-{tenantSlug}"
func BuildNamespace(tenantSlug string) (string, error) {
	return composeName("build namespace", "build-%s", tenantSlug)
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
	return composeName("repo name", "%s-%s", teamSlug, projectSlug)
}

// GitOpsRepoName returns the GitOps repository name for a specific environment.
// Format: "{teamSlug}-{projectSlug}-{envName}"
func GitOpsRepoName(teamSlug, projectSlug, envName string) (string, error) {
	return composeName("gitops repo name", "%s-%s-%s", teamSlug, projectSlug, envName)
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
	return composeName("argo app name", "%s-%s-%s", teamSlug, projectSlug, envName)
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
	return composeName("git repo binding name", "%s-%s", tenantName, envName)
}

// ArgoRepoSecretName returns the ArgoCD repository Secret name for a tenant.
// Format: "repo-{tenantName}-gitops"
func ArgoRepoSecretName(tenantName string) (string, error) {
	return composeName("argo repo secret name", "repo-%s-gitops", tenantName)
}

// Standard label keys for LoopZero Kubernetes resources. All use the
// "loopzero.io/" prefix. Use these constants instead of string literals so
// typos are caught at compile time and grep/audit is straightforward.
//
// Per pkg-sota-faang-grade-plan T1.5 + gate PKG-G02 (Minimal Public Surface).
const (
	LabelTenant      = "loopzero.io/tenant"
	LabelProject     = "loopzero.io/project"
	LabelEnvironment = "loopzero.io/environment"
	LabelManagedBy   = "loopzero.io/managed-by"

	// ManagedByValue is the canonical value for the LabelManagedBy key,
	// stable across all LoopZero-owned resources.
	ManagedByValue = "loopzero"
)

// StandardLabels is the typed representation of the four canonical LoopZero
// labels applied to every owned Kubernetes resource. Use this struct
// instead of the deprecated Labels() function so consumers get
// compile-time safety against typos.
//
// Example:
//
//	sl := naming.StandardLabels{Tenant: tenantName, Project: projectName, Environment: envName}
//	deployment.SetLabels(sl.AsMap())
//
// Per pkg-sota-faang-grade-plan T1.5 + gate PKG-G02.
type StandardLabels struct {
	Tenant      string
	Project     string
	Environment string
}

// AsMap returns the canonical label map for use in Kubernetes resource
// metadata. The returned map is fresh on each call — mutating it does
// not affect future invocations or other StandardLabels instances.
func (l StandardLabels) AsMap() map[string]string {
	return map[string]string{
		LabelTenant:      l.Tenant,
		LabelProject:     l.Project,
		LabelEnvironment: l.Environment,
		LabelManagedBy:   ManagedByValue,
	}
}

// ImageTag returns the container image reference for a built app.
// Format: "{registry}/{tenant}/{project}-{app}:{commitSHA}"
//
// ARCH-001 fix: This was duplicated in 4 independent sites (buildkit.go,
// build_plan.go, build_reconcile_loop.go x2). A format change in one site
// would cause image mismatch → RELEASING deadlock. Now there is ONE function.
//
// **Supply-chain note (CHG-2026-04-29-DOGFOOD iter 8)**: callers that
// render Pod manifests MUST prefer `ImageDigestRef` when the build pipeline
// emitted a sha256 digest. The `loopzero-require-image-signature` VAP rejects
// any container image reference using a mutable tag — using ImageTag here
// is correct only for: (a) build-time pull/push (BuildKit), (b) registry
// path debugging, (c) fallback when digest is not available. For deploy
// manifests, see ImageDigestRef.
func ImageTag(registry, tenant, project, app, commitSHA string) string {
	return fmt.Sprintf("%s/%s/%s-%s:%s", registry, tenant, project, app, commitSHA)
}

// ImageDigestRef returns the container image reference pinned to a
// cryptographic digest. Format: "{registry}/{tenant}/{project}-{app}@sha256:..."
//
// This is the form REQUIRED by the supply-chain VAP `loopzero-require-image-signature`
// (admission denies mutable tag references in tenant namespaces). The build
// pipeline captures the digest in cosign's "digest" output parameter (see
// api/internal/services/build_reconcile_complete.go::enrichAppReleasesWithAttestation);
// `digest` may arrive bare ("8f0a...") or already prefixed ("sha256:8f0a...");
// this function normalizes to the prefixed form expected by container runtimes.
func ImageDigestRef(registry, tenant, project, app, digest string) string {
	if digest == "" {
		// Caller error: empty digest. Return tag-style fallback so the
		// resulting reference is at least syntactically valid; admission
		// will reject it (which is the correct fail-loud behavior).
		return fmt.Sprintf("%s/%s/%s-%s@sha256:", registry, tenant, project, app)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	return fmt.Sprintf("%s/%s/%s-%s@%s", registry, tenant, project, app, digest)
}
