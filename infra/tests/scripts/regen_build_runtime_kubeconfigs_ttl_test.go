// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestRegenBuildRuntimeKubeconfigsTokenIsLongLived locks the fix for
// getloopscale/loopzero#242 (build-cluster / argocd-gitea credentials expire with
// no rotation on the always-on dev-public droplet).
//
// Root cause (build-cluster half): infra/scripts/local/
// regen-build-runtime-kubeconfigs.py minted the build-cluster
// `argo/loopzero-workflow-submitter` SA token with `--duration=24h`. On an
// ephemeral Cleanroom that never bites, but the always-on dev-public droplet
// outlives the token: after ~24h `loopzero deploy` fails at workflow submit with
// `argo step watcher: build cluster auth failed` (401), the per-step deploy
// timeline stalls, and the deploy record ends up `Failed` even though nothing
// about the user's app changed. The script's own docstring already blamed the
// "~24h TTL" yet still requested 24h.
//
// Fix: mint a long-lived token whose TTL comfortably exceeds any reasonable
// re-seed interval (720h / 30d → ~29-day margin). The build cluster accepts
// 720h (verified LIVE 2026-07-19). This mirrors the same-day runbook fix in
// docs/operations/build-cluster-token-rotation.md (commit 93e238d7).
//
// Body-inspection only — does NOT execute kubectl.
func TestRegenBuildRuntimeKubeconfigsTokenIsLongLived(t *testing.T) {
	scriptPath := repoFile(t, "infra", "scripts", "local", "regen-build-runtime-kubeconfigs.py")
	body := readFileText(t, scriptPath)

	// T1. The `create token ... --duration=<N>h` request MUST be long-lived.
	// Extract every `--duration=<N>h` literal and require each to be >= 720h.
	// 24h (the pre-#242 value) is the exact bug this closes.
	durRe := regexp.MustCompile(`--duration=(\d+)h`)
	matches := durRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatal("regen-build-runtime-kubeconfigs.py: no `--duration=<N>h` found in the kubectl create-token call — cannot verify token TTL (#242)")
	}
	const minHours = 720 // 30 days
	for _, m := range matches {
		hours, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("regen-build-runtime-kubeconfigs.py: unparseable --duration value %q", m[1])
		}
		if hours < minHours {
			t.Errorf("regen-build-runtime-kubeconfigs.py mints an SA token with --duration=%dh, want >= %dh (720h/30d) — a token shorter than the re-seed interval expires and silently breaks every deploy on the always-on dev-public droplet (#242)", hours, minHours)
		}
	}

	// T2. The stale 24h value MUST NOT be the active mint duration.
	if strings.Contains(body, "--duration=24h") {
		t.Error("regen-build-runtime-kubeconfigs.py still mints `--duration=24h` — the exact TTL that expired and broke deploys on dev-public (#242)")
	}

	// T3. The build-cluster SA MUST remain the workflow SUBMITTER, not the
	// executor. loopzero-api submits + watches Workflows; `loopzero-build-sa` is the
	// executor (workflowtaskresults only) and authenticates but is Forbidden
	// (403) to create/list Workflows. Guard against a regression to the wrong
	// SA (the exact trap in the stale rotation runbook, commit 93e238d7).
	if !strings.Contains(body, "loopzero-workflow-submitter") {
		t.Error("regen-build-runtime-kubeconfigs.py no longer references `loopzero-workflow-submitter` for the build cluster — the submitter SA is required for `workflows: create/list/watch` (#242 / Sprint 7 T2.1)")
	}

	// T4. #242 traceability comment present so a future audit grep finds the
	// rationale for the long TTL.
	if !strings.Contains(body, "#242") {
		t.Error("regen-build-runtime-kubeconfigs.py has no `#242` traceability comment for the long-lived-token fix")
	}
}
