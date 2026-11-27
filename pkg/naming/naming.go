// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package naming provides deterministic naming functions for all LoopRuntime
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
	return truncateLabel(s, MaxKubernetesNameLength)
}

// truncateLabel cuts s to at most max bytes and removes trailing hyphens, so
// the result is still a valid RFC 1123 label end. The trim applies even when
// no cut was needed: a composed label with an empty last part ends in a
// hyphen too.
func truncateLabel(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	return strings.TrimRight(s, "-")
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

// namePart is one labelled input to composeName. The label is what an error
// names when the value is empty, so a caller holding several slugs is told
// which one was missing instead of having to guess.
type namePart struct {
	label string
	value string
}

// composeName builds a Kubernetes resource name from format and parts, then
// validates it as RFC 1123. resourceKind is the human-readable kind used in
// error messages (e.g., "tenant namespace"). Every part must be non-empty; the
// error for an empty one reads "<kind>: <label> must not be empty". It is the
// one copy of the "validate inputs, format, validate name, wrap error"
// scaffold the composers below share.
func composeName(resourceKind, format string, parts ...namePart) (string, error) {
	values := make([]interface{}, len(parts))
	for i, p := range parts {
		if p.value == "" {
			return "", fmt.Errorf("%s: %s must not be empty", resourceKind, p.label)
		}
		values[i] = p.value
	}
	name := fmt.Sprintf(format, values...)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("%s %q: %w", resourceKind, name, err)
	}
	return name, nil
}

// TenantNamespace returns the namespace name for a tenant.
// tenantName is the K8s CRD resource name (e.g., "tn-loop-runtime").
// Format: "loop-runtime-{tenantName}" — maintains compatibility with deployed resources.
func TenantNamespace(tenantName string) (string, error) {
	return composeName("tenant namespace", "loop-runtime-%s", namePart{"tenant name", tenantName})
}

// Namespace is the CANONICAL helper for ALL LoopRuntime workload namespaces.
// Format: "loop-runtime-{tenant}-{project}-{env}". One formula for every env
// type (production, staging, preview-pr-N, ...).
//
// The (tenant, project, env) triple is the real isolation boundary
// in LoopRuntime, so the namespace reflects it uniformly. Sponsor-directed
// canonical refactor 2026-06-07 (verbatim "TUDO DEVERIA esta separado
// por projeto"): no special-case branches for previews vs envs.
func Namespace(tenantSlug, projectSlug, envName string) (string, error) {
	return composeName("namespace", "loop-runtime-%s-%s-%s", namePart{"tenant slug", tenantSlug}, namePart{"project slug", projectSlug}, namePart{"env name", envName})
}

// IsPreviewEnv reports whether an env name is a preview-flavor environment.
// Used by the build complete path to decide between SetDesired
// (kind='permanent') and SetPreviewDesired (kind='preview') when
// persisting env_targets. D2 of plan
// 2026-06-07-preview-as-category-single-slot-plan.md.
//
// Lives here (not preview_service.go) so non-service callers
// (container_delivery, promotion, rollback) can detect previews
// without importing services.
//
// Red Team loop-console#41 absorption (2026-06-27): the original predicate
// only matched the canonical `preview-pr-N` namespace pattern emitted by
// the PreviewService. PRODUCTION data (`loopzerokit-example` / `preview`)
// has env_targets with the literal "preview" environment name from the
// CLI legacy path — those rows were mis-labeled `kind='permanent'` and
// counted as production envs by `production_env_count`, surfacing the
// dashboard's "Live envs: 1 + URL PENDING" pair. Broaden the predicate
// to also match the literal "preview" base name AND the
// `preview-{anything}` prefix the legacy CLI used. Stays consistent
// with the canonical CLI exit-code contract — "production", "staging"
// and other named envs are explicitly NOT preview.
func IsPreviewEnv(envName string) bool {
	if envName == "preview" {
		return true
	}
	return strings.HasPrefix(envName, "preview-")
}

// BuildNamespace returns the build namespace name for a tenant.
// Format: "build-{tenantSlug}"
func BuildNamespace(tenantSlug string) (string, error) {
	return composeName("build namespace", "build-%s", namePart{"tenant slug", tenantSlug})
}

// RepoName returns the source code repository name.
// Format: "{teamSlug}-{projectSlug}"
func RepoName(teamSlug, projectSlug string) (string, error) {
	return composeName("repo name", "%s-%s", namePart{"team slug", teamSlug}, namePart{"project slug", projectSlug})
}

// GitOpsRepoName returns the GitOps repository name for a specific environment.
// Format: "{teamSlug}-{projectSlug}-{envName}"
func GitOpsRepoName(teamSlug, projectSlug, envName string) (string, error) {
	return composeName("gitops repo name", "%s-%s-%s", namePart{"team slug", teamSlug}, namePart{"project slug", projectSlug}, namePart{"env name", envName})
}

// ArgoAppName returns the ArgoCD Application resource name.
// Format: "{teamSlug}-{projectSlug}-{envName}"
func ArgoAppName(teamSlug, projectSlug, envName string) (string, error) {
	return composeName("argo app name", "%s-%s-%s", namePart{"team slug", teamSlug}, namePart{"project slug", projectSlug}, namePart{"env name", envName})
}

// EnvironmentCRName returns the Environment CR resource name.
// Format: "{tenantName}-{projectSlug}-{envName}"
//
// The project belongs in the identity because the CR's content already depends on it:
// spec.project and spec.namespace.name are both project-scoped. While the name omitted
// it, two projects of one tenant sharing an env name collapsed onto a single CR, and the
// loser silently received no namespace, RBAC or NetworkPolicy at all (issue #273).
func EnvironmentCRName(tenantName, projectSlug, envName string) (string, error) {
	return composeName("environment CR name", "%s-%s-%s", namePart{"tenant name", tenantName}, namePart{"project slug", projectSlug}, namePart{"env name", envName})
}

// AppCertSecretName returns the TLS certificate secret name for one app of one project.
// Format: "tls-{projectSlug}-{appName}"
//
// This is the name the manifest renderer actually creates
// (api/internal/manifests/renderer_ingress.go). It carries the project because two
// projects of one tenant may run an app with the same name, and they must not share a
// certificate secret.
//
// It exists as a shared function because the name was previously derived twice, by two
// different formulas: the renderer built "tls-{project}-{app}" while the exposure
// controller looked for CertSecretName(hostname) => "{sanitized-hostname}-tls". They never
// matched, so every ExposedRoute stayed Pending with a valid certificate sitting next to
// it — 27 days in app-dev before anyone noticed (issue #277). Deriving the same name in
// two places is what produced that; there is now one place.
func AppCertSecretName(projectSlug, appName string) (string, error) {
	return composeName("app cert secret name", "tls-%s-%s", namePart{"project slug", projectSlug}, namePart{"app name", appName})
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
	return truncateLabel(sanitized, maxBase) + suffix, nil
}

// GitRepoBindingName returns the GitRepositoryBinding CR name for a tenant's environment.
// Format: "{tenantName}-{envName}"
func GitRepoBindingName(tenantName, envName string) (string, error) {
	return composeName("git repo binding name", "%s-%s", namePart{"tenant name", tenantName}, namePart{"env name", envName})
}

// ArgoRepoSecretName returns the ArgoCD repository Secret name for a tenant.
// Format: "repo-{tenantName}-gitops"
func ArgoRepoSecretName(tenantName string) (string, error) {
	return composeName("argo repo secret name", "repo-%s-gitops", namePart{"tenant name", tenantName})
}

// Standard label keys for LoopRuntime Kubernetes resources. All use the
// "runtime.getloopscale.com/" prefix. Use these constants instead of string literals so
// typos are caught at compile time and grep/audit is straightforward.
//
// Per pkg-sota-faang-grade-plan T1.5 + gate PKG-G02 (Minimal Public Surface).
const (
	LabelTenant      = "runtime.getloopscale.com/tenant"
	LabelProject     = "runtime.getloopscale.com/project"
	LabelEnvironment = "runtime.getloopscale.com/environment"
	LabelManagedBy   = "runtime.getloopscale.com/managed-by"

	// LabelApp names the application a resource belongs to (the service
	// name in loopzero.yaml). The api sets it on every LoopRuntimeApplication CR it
	// writes, and the app-operator reads it back for CRs that predate
	// LoopRuntimeApplication.spec.appName.
	LabelApp = "runtime.getloopscale.com/app"

	// ManagedByValue is the canonical value for the LabelManagedBy key,
	// stable across all LoopRuntime-owned resources.
	ManagedByValue = "loop-runtime"
)

// StandardLabels is the typed representation of the four canonical LoopRuntime
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
// emitted a sha256 digest. The `loop-runtime-require-image-signature` VAP rejects
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
// This is the form REQUIRED by the supply-chain VAP `loop-runtime-require-image-signature`
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
