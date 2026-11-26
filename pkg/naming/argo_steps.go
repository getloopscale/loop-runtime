// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package naming

// argo_steps.go — Single source of truth for Argo Workflow step display names
// consumed by producer (api/internal/argo/cosign.go, cosign_attest.go,
// workflow_builder.go) AND consumer (api/internal/services/build_reconcile_complete.go).
//
// T1.1 of plan 2026-05-28-meet-001-engineering-closure v1.1.
//
// Motivation: the 3-layer SQL bug class (commits f75a19bc + b3375ed0)
// included a near-miss where producer + consumer used inline `fmt.Sprintf`
// literals in 5+ files. A single typo would silently produce empty Outputs
// maps and DB columns go NULL. These helpers centralize the format so
// producer name == consumer expected key (round-trip invariant tested).
//
// EC-10 fix: empty app name panics with descriptive message. Silent
// trailing-dash retorno would propagate "sign-image-" as a step name and
// cause invisible map lookup failures downstream.

// SignStepName returns the Argo Workflow display name for the cosign sign
// step of `app`. Producer: api/internal/argo/cosign.go cosignSignTemplate.
// Consumer: api/internal/services/build_reconcile_complete.go enrichment.
func SignStepName(app string) string {
	if app == "" {
		panic("naming.SignStepName: empty app name (programming error)")
	}
	return "sign-image-" + app
}

// AttestStepName returns the Argo Workflow display name for the cosign
// attest step of `app`. Producer: api/internal/argo/cosign_attest.go.
// Consumer: enrichAppReleasesWithAttestation reads `rekor-url` output param.
func AttestStepName(app string) string {
	if app == "" {
		panic("naming.AttestStepName: empty app name (programming error)")
	}
	return "attest-image-" + app
}

// BuildImageStepName returns the Argo Workflow display name for the BuildKit
// image build step of `app`.
func BuildImageStepName(app string) string {
	if app == "" {
		panic("naming.BuildImageStepName: empty app name (programming error)")
	}
	return "build-image-" + app
}

// SBOMStepName returns the Argo Workflow display name for the syft SBOM
// step of `app`.
func SBOMStepName(app string) string {
	if app == "" {
		panic("naming.SBOMStepName: empty app name (programming error)")
	}
	return "sbom-" + app
}
