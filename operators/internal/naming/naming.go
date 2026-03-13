// Package naming provides deterministic naming functions for Kubernetes
// resources created by LoopZero operators. All names conform to RFC 1123:
// lowercase, alphanumeric + hyphens, max 63 characters.
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

// NamespaceName returns the namespace name for a tenant's project environment.
// Format: "{tenant}-{project}-{env}"
func NamespaceName(tenant, project, env string) (string, error) {
	name := fmt.Sprintf("%s-%s-%s", tenant, project, env)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("namespace name %q: %w", name, err)
	}
	return name, nil
}

// BuildNamespaceName returns the build namespace name for a tenant.
// Format: "build-{tenant}"
func BuildNamespaceName(tenant string) (string, error) {
	name := fmt.Sprintf("build-%s", tenant)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("build namespace name %q: %w", name, err)
	}
	return name, nil
}

// PreviewNamespaceName returns the namespace name for a preview environment.
// Format: "{tenant}-{project}-pr-{prNumber}"
func PreviewNamespaceName(tenant, project string, prNumber int) (string, error) {
	name := fmt.Sprintf("%s-%s-pr-%d", tenant, project, prNumber)
	if err := ValidateKubernetesName(name); err != nil {
		return "", fmt.Errorf("preview namespace name %q: %w", name, err)
	}
	return name, nil
}

// ValidateKubernetesName checks whether a name conforms to RFC 1123 DNS label rules.
// It returns an error describing the violation, or nil if the name is valid.
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
