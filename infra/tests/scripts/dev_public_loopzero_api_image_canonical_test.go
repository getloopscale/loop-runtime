// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"regexp"
	"strings"
	"testing"
)

// TestDevPublicLoopZeroApiImageOverlayUsesCanonicalDOCR locks issue #100 Gap B
// canonical alignment (Cleanroom canonical chain run 2026-06-19).
//
// Pre-fix state: infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl
// pointed image.repository at `k3d-loopzero-registry:5000/loopzero-api` with
// tag `multi-aud-fix` — a workaround tag created during a one-off
// rebuild 2026-06-15 to ship the multi-audience JWT validator
// (api/cmd/setup_auth.go:69 loopzerojwt.NewValidatorWithAudiences). The
// workaround was never cleaned up after the change shipped to develop
// and the canonical DOCR `:dev` build pipeline picked up the multi-aud
// code. Result: dev-public Cleanroom loopzero-api pods ImagePullBackOff for
// a tag that no longer exists in the local k3d registry (Cleanroom
// destroys it via `task clean:all`).
//
// Additional gap: `task images:push:platform:dev-public` (the canonical
// DOCR push task) builds the loopzero-api image WITHOUT `--secret
// id=gomod_token`, which would re-trigger the same go-mod-download
// terminal-prompts-disabled error that PR #98 fixed in the Tiltfile
// path. Without the secret mount, loopzero-api's `go mod download` hits
// the M5 private loopzero-contracts module and fails terminal-prompts-
// disabled (same bug class).
//
// Canonical fix: drop the `multi-aud-fix` workaround in the overlay
// (use DOCR `${LOOPZERO_DEV_IMAGE_TAG:-dev}` instead) + pass `--secret
// id=gomod_token,src=$HOME/.loopzero/gomod-token.txt` to the docker buildx
// invocation for loopzero-api in the DOCR push task + add a fail-fast
// pre-flight matching the Tiltfile pattern.
//
// This test asserts the overlay + the build task are canonical. It
// does NOT execute the build — purely body inspection.
func TestDevPublicLoopZeroApiImageOverlayUsesCanonicalDOCR(t *testing.T) {
	overlayPath := repoFile(t, "infra", "helmfile", "values", "dev-public", "loopzero-api.yaml.gotmpl")
	overlay := readFileText(t, overlayPath)

	// T1. Overlay MUST NOT USE `multi-aud-fix` as the active tag
	// value (line `tag: multi-aud-fix` or `tag: "multi-aud-fix"`).
	// Comments documenting the history are fine — the test cares about
	// the YAML key, not the prose.
	staleTagRe := regexp.MustCompile(`(?m)^\s*tag:\s*"?multi-aud-fix"?\s*$`)
	if staleTagRe.MatchString(overlay) {
		t.Error("infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl still USES `tag: multi-aud-fix` workaround as the active value — canonical fix drops this (issue #100 Gap B)")
	}

	// T2. Overlay MUST NOT USE `k3d-loopzero-registry:5000/loopzero-api` as
	// the active repository value. Comments are fine; only the YAML
	// key matters.
	staleRepoRe := regexp.MustCompile(`(?m)^\s*repository:\s*"?k3d-loopzero-registry:5000/loopzero-api"?\s*$`)
	if staleRepoRe.MatchString(overlay) {
		t.Error("infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl still USES `repository: k3d-loopzero-registry:5000/loopzero-api` — dev-public canonical pulls from DOCR (issue #100 Gap B)")
	}

	// T3 (audit 2026-07-10): overlay must reference a canonical registry —
	// either the pre-#142 DOCR path (`registry.digitalocean.com/loopzero-system/
	// loopzero-api`) OR the post-#142 GHCR path (`ghcr.io/getloopscale/loopzero-api`).
	//
	// NAMESPACE SWITCHED 2026-07-30: it was `ghcr.io/getloopscale`. The repo was
	// transferred to the `getloopscale` org and CI started publishing there. The
	// old package does NOT disappear — it just stops receiving builds, so an
	// overlay pointing at it would leave dev-public running frozen on the
	// last pre-transfer image, with no error at all. That silence is what
	// this guard exists to prevent.
	// Canonical migrated 2026-06-22 to GHCR :develop (rolling) per
	// [[feedback_close_issue_update_live_dev_public_2026_06_22]] — CI
	// build-publish workflow auto-refreshes on every push to develop.
	usesDOCR := strings.Contains(overlay, "registry.digitalocean.com/loopzero-system/loopzero-api")
	usesGHCR := strings.Contains(overlay, "ghcr.io/getloopscale/loopzero-api")
	if !usesDOCR && !usesGHCR {
		t.Error("infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl does NOT reference canonical DOCR OR GHCR repository — issue #100 Gap B / #142 canonical")
	}

	// T4 (audit 2026-07-10): traceability comment cites either issue #100
	// (pre-#142 direction) OR issue #142 (post-#142 canonical); a future
	// "simplify" refactor sees the rationale for either registry choice.
	hasIssue100 := strings.Contains(overlay, "issue #100") || strings.Contains(overlay, "Issue #100")
	hasIssue142 := strings.Contains(overlay, "#142")
	if !hasIssue100 && !hasIssue142 {
		t.Error("infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl has no `issue #100` OR `#142` traceability comment")
	}

	// --- Build task assertions ---

	taskPath := repoFile(t, "taskfiles", "infra.yml")
	task := readFileText(t, taskPath)

	// T5. `images:push:platform:dev-public` task body MUST contain a
	// `--secret id=gomod_token` flag for the docker buildx invocation
	// — the api/Dockerfile step 8/11 needs the BuildKit secret mount
	// to fetch the private loopzero-contracts module.
	if !strings.Contains(task, "--secret id=gomod_token") {
		t.Error("task images:push:platform:dev-public does NOT pass `--secret id=gomod_token` — loopzero-api build will fail with `go mod download` terminal-prompts-disabled (issue #100 Gap B)")
	}

	// T6. Task body MUST cite the canonical gomod_token SoT path
	// (`~/.loopzero/gomod-token.txt` or `$HOME/.loopzero/gomod-token.txt`).
	// Without this anchor, an audit grep can't find the canonical
	// pattern.
	if !strings.Contains(task, ".loopzero/gomod-token.txt") {
		t.Error("task images:push:platform:dev-public does NOT reference canonical SoT path `~/.loopzero/gomod-token.txt` — gomod_token mount path missing (issue #100 Gap B + memory reference_gomod_token_canonical_path)")
	}

	// T7. Task body MUST contain a fail-fast pre-flight check that
	// errors with a clear remediation hint when the SoT file is
	// missing. Mirrors the Tiltfile pattern shipped in PR #98.
	if !strings.Contains(task, "gomod-token.txt") || !strings.Contains(task, "exit") {
		t.Error("task images:push:platform:dev-public does NOT have a fail-fast pre-flight for the gomod_token file (issue #100 Gap B)")
	}

	// T8. Issue #100 traceability comment present.
	if !strings.Contains(task, "issue #100") && !strings.Contains(task, "Issue #100") {
		t.Error("task images:push:platform:dev-public has no `issue #100` traceability comment")
	}
}
