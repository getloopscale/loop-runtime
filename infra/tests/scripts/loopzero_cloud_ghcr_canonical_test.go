// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"regexp"
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
	overlayPath := repoFile(t, "infra", "helmfile", "values", "dev-public", "loopzero-cloud.yaml.gotmpl")
	overlay := readFileText(t, overlayPath)

	// T1. Overlay MUST NOT use `tag: multi-aud-fix` as the active YAML
	// key value (comments documenting the history are fine — the test
	// cares about the YAML key, not prose).
	staleTagRe := regexp.MustCompile(`(?m)^\s*tag:\s*"?multi-aud-fix"?\s*$`)
	if staleTagRe.MatchString(overlay) {
		t.Error("infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl still USES `tag: multi-aud-fix` workaround — canonical fix drops this (issue #104 Architecture A)")
	}

	// T2. Overlay MUST NOT use `k3d-loopzero-registry:5000/loopzero-cloud` as
	// the active repository value.
	staleRepoRe := regexp.MustCompile(`(?m)^\s*repository:\s*"?k3d-loopzero-registry:5000/loopzero-cloud"?\s*$`)
	if staleRepoRe.MatchString(overlay) {
		t.Error("infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl still USES `repository: k3d-loopzero-registry:5000/loopzero-cloud` — issue #104 Architecture A")
	}

	// T3. The overlay MUST RESOLVE to the canonical GHCR repository
	// `ghcr.io/getloopscale/loopzero-cloud` — where the AM6 workflow pushes
	// loopzero-cloud images (`loopzero/.github/workflows/build-publish.yml:12`).
	//
	// Asserted on the RENDERED value, not on the source text. The repository is
	// built from a parameterised prefix (`{{ env … | default "ghcr.io/getloopscale/" }}`
	// followed by the literal `loopzero-cloud`), so the canonical string never
	// appears contiguously and a substring match reported this correct file as
	// broken — for an indeterminate time, because this package ran in no task
	// target until B-092. See TestRenderedImageRepository for the contract.
	gotRepo, err := renderedImageRepository(overlay)
	if err != nil {
		t.Errorf("could not render infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl: %v", err)
	} else if gotRepo != "ghcr.io/getloopscale/loopzero-cloud" {
		t.Errorf("dev-public overlay renders image.repository = %q, want the canonical GHCR repository %q — issue #104 Architecture A",
			gotRepo, "ghcr.io/getloopscale/loopzero-cloud")
	}

	// T4. Overlay MUST reference the `ghcr-pull` imagePullSecret —
	// `do-registry` is wrong for GHCR (DOCR-only). Architecture A
	// provisions `ghcr-pull` via a NEW task.
	if !strings.Contains(overlay, "ghcr-pull") {
		t.Error("infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl does NOT use `ghcr-pull` imagePullSecret — `do-registry` is DOCR-only, incompatible with GHCR (issue #104)")
	}

	// T5. Traceability comment cites issue #104.
	if !strings.Contains(overlay, "issue #104") && !strings.Contains(overlay, "Issue #104") {
		t.Error("infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl has no `issue #104` traceability comment")
	}

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
