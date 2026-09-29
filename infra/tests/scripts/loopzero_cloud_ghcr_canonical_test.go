// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"strings"
	"testing"
)

// TestLoopZeroCloudImageOverlayUsesGHCRCanonical locks issue #104 Architecture A
// (ADR PROPOSED `2026-06-19-loopzero-cloud-image-registry-canonical`).
//
// Pre-fix state: the engine consumed loopzero-cloud images from inconsistent
// sources:
//
//   - loopzero-cloud CI (AM6 workflow `loopzero/.github/workflows/build-publish.yml:12`)
//     publishes EXCLUSIVELY to GHCR (`ghcr.io/getloopscale/loopzero-cloud:develop`
//     for branch pushes; `:vX.Y.Z` + `:latest` for tags).
//   - Engine chart default `infra/helm/loopzero-cloud/values.yaml` (since deleted) expected
//     DOCR (`registry.digitalocean.com/loopzero-system/loopzero-cloud:latest`) —
//     image never published there.
//   - Engine dev-public overlay points at local k3d-registry with stale
//     `multi-aud-fix` workaround tag + `do-registry` (DOCR pull secret).
//
// Architecture A (per ADR): align both chart default + dev-public overlay
// to GHCR. Provision a `kubernetes.io/dockerconfigjson` Secret named
// `ghcr-pull` from `~/.loopzero/ghcr-pull-token.txt` (NEW SoT, sibling of
// `gomod-token.txt` per memory `reference_gomod_token_canonical_path`).
//
// ADR is PROPOSED, awaiting sponsor grill-me. Implementation ships pre-
// ratification with same justification as PR #109 for issue #106:
// minimum surface, no cross-repo, single PR revertible. Sponsor can
// override via subsequent ADR.
//
// This test asserts the overlay + chart default + provisioning task
// canonical. It does NOT execute helmfile/kubectl.
func TestLoopZeroCloudImageOverlayUsesGHCRCanonical(t *testing.T) {
	// The dev-public overlay no longer pulls from GHCR: it pulls from ECR by commit
	// (#435), which infra/tests/charts/dev_public_platform_images_ecr_test.go proves on
	// the rendered Deployment. What stays here is the chart default and the task that
	// provisions the GHCR pull secret for the environments that still use it.

	// --- Chart default assertions ---

	// T6. The bootstrap default MUST point at GHCR (where the image actually
	// lives). The legacy DOCR default is stale per AM6 workflow. Every Helmfile
	// install layers charts/loopzero-cloud/values-bootstrap.yaml under its overlay,
	// so that file is the default an environment without an image override gets.
	bootstrapDefault := readFileText(t, repoFile(t, "charts", "loopzero-cloud", "values-bootstrap.yaml"))
	if got, err := renderedImageRepository(bootstrapDefault); err != nil {
		t.Errorf("could not read the image from charts/loopzero-cloud/values-bootstrap.yaml: %v", err)
	} else if got != "ghcr.io/getloopscale/loopzero-cloud" {
		t.Errorf("charts/loopzero-cloud/values-bootstrap.yaml pulls %q, not the canonical GHCR repository — the image was never published to the legacy DOCR default (issue #104 Architecture A)", got)
	}

	// --- Provisioning task assertions ---

	taskPath := repoFile(t, "taskfiles", "infra.yml")
	task := readFileText(t, taskPath)

	// T7. NEW task MUST exist for provisioning the `ghcr-pull` Secret
	// from `~/.loopzero/ghcr-pull-token.txt` (sibling SoT of `gomod-token.txt`).
	if !strings.Contains(task, "ghcr-pull") {
		t.Error("taskfiles/infra.yml does NOT contain a `ghcr-pull` provisioning task — issue #104 Architecture A")
	}

	// T8. Provisioning task MUST reference the canonical SoT path
	// `~/.loopzero/ghcr-pull-token.txt` so an audit grep finds the file
	// location.
	if !strings.Contains(task, ".loopzero/ghcr-pull-token.txt") {
		t.Error("taskfiles/infra.yml does NOT reference canonical SoT `~/.loopzero/ghcr-pull-token.txt` — issue #104 Architecture A")
	}

	// T9. Provisioning task MUST have a fail-fast pre-flight when the
	// SoT file is missing (mirrors PR #98 + #105 patterns).
	if !strings.Contains(task, "ghcr-pull-token.txt") || !strings.Contains(task, "issue #104") {
		t.Error("taskfiles/infra.yml provisioning task does NOT cite issue #104 / lacks fail-fast pre-flight")
	}
}
