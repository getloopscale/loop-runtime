package domain

import (
	"errors"
	"strings"

	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// EnvRef is the canonical Value Object identifying an environment within LoopRuntime.
// Per ADR D2 (decisions/2026-06-11-pkg-domain-env-ref-value-object.md):
// replaces the unconstrained `(tenant_id, project, env_name) string` triple
// that appears in 34 files / 70+ call sites. Closes pattern-suggester #58 +
// structural root of hardcode tendency.
//
// Field semantics:
//   - TenantID:  RFC 1123-compliant tenant identifier (e.g., "tn-08fa2ecedc3c")
//   - Project:   monorepo project slug from loopzero.yaml (e.g., "api", "frontend")
//   - EnvName:   environment name (e.g., DefaultPermanentEnv "production", "staging-eu-1", "preview-pr-42")
//   - CellID:    multi-cell placement target (e.g., DefaultCellID); empty for single-cell
//
// EnvRef is IMMUTABLE by construction (no mutator methods) — race-clean.
type EnvRef struct {
	TenantID string
	Project  string
	EnvName  string
	CellID   string
}

// ErrInvalidEnvRefField is returned when a constructor or parser detects
// invalid input — empty required field OR forbidden separator char.
var ErrInvalidEnvRefField = errors.New("invalid EnvRef field")

// envRefSeparator is the ASCII Unit Separator (U+001F) — guaranteed absent
// from valid RFC 1123 identifier chars. Per ADR D2 EC-4 absorption: prevents
// String/ParseEnvRef round-trip failure on values containing '-' (e.g., env
// name "staging-eu-1" from H6 reality).
const envRefSeparator = "\x1f"

// NewEnvRef constructs an EnvRef after validating each field is non-empty
// and free of the reserved separator char. Returns ErrInvalidEnvRefField
// on violation.
//
// CellID is optional (variadic for ergonomic callers). When omitted, CellID
// is empty — caller's responsibility to interpret as single-cell default.
func NewEnvRef(tenantID, project, envName string, cellID ...string) (EnvRef, error) {
	if tenantID == "" {
		return EnvRef{}, errors.Join(ErrInvalidEnvRefField, errors.New("tenantID empty"))
	}
	if project == "" {
		return EnvRef{}, errors.Join(ErrInvalidEnvRefField, errors.New("project empty"))
	}
	if envName == "" {
		return EnvRef{}, errors.Join(ErrInvalidEnvRefField, errors.New("envName empty"))
	}
	var cid string
	if len(cellID) > 0 {
		cid = cellID[0]
	}
	for _, field := range []string{tenantID, project, envName, cid} {
		if strings.Contains(field, envRefSeparator) {
			return EnvRef{}, errors.Join(ErrInvalidEnvRefField, errors.New("field contains reserved separator (U+001F)"))
		}
	}
	return EnvRef{TenantID: tenantID, Project: project, EnvName: envName, CellID: cid}, nil
}

// String formats the EnvRef using ASCII Unit Separator (U+001F) between
// fields — round-trip safe with ParseEnvRef. Format: "tenant␟project␟env␟cell"
// (cell may be empty trailing segment).
//
// NOTE: this is the INTERNAL canonical form for Value Object identity. Public-
// facing display formats (K8s namespaces, URLs) use `pkg/naming.Namespace(EnvRef)`
// which is hyphen-separated — that path is one-way (never round-trips).
func (e EnvRef) String() string {
	return e.TenantID + envRefSeparator + e.Project + envRefSeparator + e.EnvName + envRefSeparator + e.CellID
}

// ParseEnvRef parses the canonical String() form. Returns ErrInvalidEnvRefField
// if input has wrong number of segments OR any required field is empty.
func ParseEnvRef(s string) (EnvRef, error) {
	parts := strings.Split(s, envRefSeparator)
	if len(parts) != 4 {
		return EnvRef{}, errors.Join(ErrInvalidEnvRefField, errors.New("input does not have exactly 4 separator-delimited segments"))
	}
	return NewEnvRef(parts[0], parts[1], parts[2], parts[3])
}

// Equal returns true when both EnvRefs have identical field values.
// Value equality (not pointer) — consistent with Value Object semantics.
func (e EnvRef) Equal(other EnvRef) bool {
	return e.TenantID == other.TenantID &&
		e.Project == other.Project &&
		e.EnvName == other.EnvName &&
		e.CellID == other.CellID
}

// Namespace returns the canonical K8s workload namespace per ADR D2 +
// pkg/naming.Namespace formula: "loopruntime-{TenantID}-{Project}-{EnvName}".
//
// T1.3.b of plan 2026-06-11-code-review-remediation v1.2 — adds an
// EnvRef-keyed wrapper around the existing (tenant, project, env)
// triple consumed by pkg/naming.Namespace. Call sites that already
// construct an EnvRef pass it through (`ref.Namespace()`) instead of
// passing three loose strings. The string-triple call form remains
// supported in pkg/naming.Namespace for legacy callers; this method is
// the canonical pattern for new code.
//
// Returns a typed K8s namespace string. Error path mirrors
// pkg/naming.Namespace exactly: invalid RFC 1123 name (too long, bad
// chars, leading hyphen) surfaces as a wrapped error including the
// rejected name. Callers that need just the string can take the
// returned name and check err separately — same shape as the existing
// pkg/naming API.
//
// Delegates to pkg/naming.Namespace so the RFC 1123 + 63-char ceiling
// validation runs upstream of the call site. pkg/naming has zero
// external deps (stdlib only — see its package doc); no cycle risk
// from pkg/domain importing it.
//
// T1.3.f follow-through: an earlier draft built the namespace string
// inline (`"loopruntime-" + e.TenantID + ...`) to avoid an import. That draft
// was caught by TestTenantCRManager_AddEnvironment_RejectsCombinedNamespaceOver63Chars
// which legitimately expected the 63-char ceiling guard to fire BEFORE
// the apiserver round-trip. Delegating to naming.Namespace restores the
// full contract.
func (e EnvRef) Namespace() (string, error) {
	if e.TenantID == "" {
		return "", errors.Join(ErrInvalidEnvRefField, errors.New("tenantID empty (cannot derive namespace)"))
	}
	if e.Project == "" {
		return "", errors.Join(ErrInvalidEnvRefField, errors.New("project empty (cannot derive namespace)"))
	}
	if e.EnvName == "" {
		return "", errors.Join(ErrInvalidEnvRefField, errors.New("envName empty (cannot derive namespace)"))
	}
	return naming.Namespace(e.TenantID, e.Project, e.EnvName)
}
