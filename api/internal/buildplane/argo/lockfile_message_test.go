// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"
)

// Dependency installation runs at the workspace ROOT (`COPY . .` copies the
// whole bundle), while the app may declare `path: api`. When the lockfile sits
// in the subdirectory, the user reads "no lockfile found" with the file
// committed in their own repository, and the message did not say WHERE it had
// looked (B-155).
//
// Measured live on 2026-08-18: `api/requirements.txt` existed and the build died
// saying there was no lockfile.
func TestLockfileMessageNamesWhereItLooked(t *testing.T) {
	for name, script := range map[string]string{"node": nodeInstall, "python": pythonInstall} {
		if !strings.Contains(script, "$(pwd)") {
			t.Errorf("%s: the message does not name the directory it searched:\n%s", name, script)
		}
		if !strings.Contains(script, "repository root") {
			t.Errorf("%s: the message does not say where dependencies are installed from:\n%s", name, script)
		}
		if !strings.Contains(script, "Commit one of these files at the root") {
			t.Errorf("%s: the message does not say what to do:\n%s", name, script)
		}
	}
}

// The list of files searched stays in the message: without it, knowing the
// directory does not help — the user needs both.
func TestLockfileMessageStillListsTheFiles(t *testing.T) {
	if !strings.Contains(pythonInstall, "requirements.txt") || !strings.Contains(pythonInstall, "poetry.lock") {
		t.Errorf("the file list is gone from the python message:\n%s", pythonInstall)
	}
	if !strings.Contains(nodeInstall, "pnpm-lock.yaml") || !strings.Contains(nodeInstall, "package-lock.json") {
		t.Errorf("the file list is gone from the node message:\n%s", nodeInstall)
	}
}

// The missing-lockfile branch must FAIL, not merely warn. Otherwise installation
// would carry on without dependencies and the failure would surface later, in
// another step, with the wrong apparent cause — the same family as B-153, where
// an `exit 1` existed and was swallowed by `tee`.
func TestMissingLockfileFailsInsteadOfWarning(t *testing.T) {
	for name, script := range map[string]string{"node": nodeInstall, "python": pythonInstall} {
		if !strings.Contains(script, "exit 1") {
			t.Errorf("%s: the missing-lockfile branch does not fail; a build without dependencies "+
				"would fail later, in another step, with the wrong apparent cause:\n%s", name, script)
		}
		if !strings.Contains(script, ">&2") {
			t.Errorf("%s: the message does not go to stderr:\n%s", name, script)
		}
	}
}

// The DECISION B-155 asked to record, pinned here instead of left implicit in
// the code: **a per-app lockfile is not supported**. Installation runs in the
// image WORKDIR, which is the repository root; the BUILD step is
// monorepo-aware, not the install step.
//
// If installation ever receives the app directory, the decision changed — and
// this test forces the item to be revisited, instead of letting the message
// promising "installed from the repository root" silently become a lie.
func TestOnlyTheBuildStepIsMonorepoAware(t *testing.T) {
	const appDir = "services/api"
	if build := nodeBuild(appDir); !strings.Contains(build, appDir) {
		t.Errorf("the build step is no longer monorepo-aware; it does not cite %q:\n%s", appDir, build)
	}
	for name, script := range map[string]string{"node": nodeInstall, "python": pythonInstall} {
		if strings.Contains(script, appDir) {
			t.Errorf("%s: installation now cites the app directory. If deliberate, the message "+
				"saying \"installed from the repository root\" became a lie — revisit B-155", name)
		}
	}
}
