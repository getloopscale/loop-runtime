// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package naming

import (
	"fmt"
)

// PreviewEnvName returns the canonical synthetic environment label used
// when submitting a build for a per-PR preview. Distinguishes preview
// builds from normal staging/production builds at the env-name layer.
//
// Format: "preview-pr-{N}". Used together with pkg/naming.IsPreviewEnv
// to detect preview envs in the build-complete path and route them
// through SetPreviewDesired (instead of SetDesired) for env_targets
// persistence.
//
// T2.5 of plan 2026-06-11-code-review-remediation v1.2 — extracted from
// the in-line literal at api/internal/services/preview_service.go
// (previewEnvName) so the canonical formula lives at a single source
// of truth alongside pkg/naming.Namespace + pkg/naming.IsPreviewEnv.
func PreviewEnvName(prNumber int) string {
	return fmt.Sprintf("preview-pr-%d", prNumber)
}

// PreviewSubdomain returns the canonical hostname label that addresses
// a preview environment over the wildcard public domain.
//
// J-23 wildcard convention (sponsor verbatim 2026-06-07 "Continuar com
// wildcard CF + Caddy wildcard block agora"): the label mirrors the
// IngressRoute Host emitted by appHostname() so the public wildcard
// proxied by Caddy reverse-proxies cleanly to Traefik on the workload
// cluster.
//
// Shape:
//   - with entryApp: "{tenant}-{project}-{entryApp}-preview-pr-{N}"
//   - without:       "{tenant}-{project}-preview-pr-{N}"
//
// RFC 1123 hostname label hard-cap: any output longer than 63 chars is
// truncated then right-trimmed of trailing '-' so the label stays valid.
// Truncation is an exceptional path (tenant_id + project_id together
// would need to push past ~30 chars for a typical PR number) but the
// cap is part of the contract.
//
// T2.5 of plan 2026-06-11-code-review-remediation v1.2 — extracted from
// the in-line composition at api/internal/services/preview_service.go
// (previewURL inner block) so the formula has one source of truth.
func PreviewSubdomain(tenantID, projectID, entryApp string, prNumber int) string {
	envName := PreviewEnvName(prNumber)
	var subdomain string
	if entryApp != "" {
		subdomain = fmt.Sprintf("%s-%s-%s-%s", tenantID, projectID, entryApp, envName)
	} else {
		subdomain = fmt.Sprintf("%s-%s-%s", tenantID, projectID, envName)
	}
	return truncateLabel(subdomain, MaxKubernetesNameLength)
}

// PreviewURL composes the full public URL exposed to users for a
// preview environment.
//
// Shape: "https://{PreviewSubdomain}.{publicDomain}". Returns the empty
// string when publicDomain is empty so callers can detect "no preview
// URL configured" without parsing the result.
//
// publicDomain is the operator-configured wildcard root (e.g.
// `preview-dev.getloopscale.app` for the dev-public env). Callers in
// PreviewService resolve it from PreviewPublicDomain with fallback to
// the legacy BaseDomain — that resolution stays at the call site
// because it is a service-wiring concern, not a naming concern.
//
// T2.5 of plan 2026-06-11-code-review-remediation v1.2.
func PreviewURL(tenantID, projectID, entryApp, publicDomain string, prNumber int) string {
	if publicDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://%s.%s",
		PreviewSubdomain(tenantID, projectID, entryApp, prNumber),
		publicDomain,
	)
}
