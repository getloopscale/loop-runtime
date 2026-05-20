// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argo — Dockerfile linter step (build-mode-dispatch-and-dockerfile-security-plan T2.1).
//
// dockerfileLintStep returns a forge.Container that runs the
// `loopzero-dockerfile-lint` binary against user-supplied Dockerfiles before
// BuildKit consumes them. Hard rejection rules (non-allow-list FROM
// registries, --privileged/--security=insecure RUN flags, absolute bind
// mounts, reserved env keys, seccomp/apparmor unconfined) cause the
// workflow step to exit 1 and the build to Fail. Soft warnings are
// captured by Loki for audit but don't block.
//
// This is the DockerfileStep returned by UserDockerfileStrategy when an
// app declares `build: dockerfile` in loopzero.yaml. Other (language) strategies
// continue to return loopzeroPacksDockerfileStep for auto-generation.

package argo

import (
	"fmt"
	"os"
	"strings"

	forge "github.com/getloopscale/loopzero-forge"

	"github.com/loopzero/loopzero/api/internal/services"
)

// dockerfileLintStep produces the container template for the `dockerfile-lint`
// workflow step. The step takes the source workspace + each app's Dockerfile
// path (relative to source) and applies the static rules.
//
// Args format: --apps "<app1>:<dockerfile1>,<app2>:<dockerfile2>"
// Each <dockerfileN> is RELATIVE to the source root (workspaceMountPath).
func dockerfileLintStep(apps []services.AppSpec) *forge.Container {
	var pairs []string
	for _, app := range apps {
		appPath := app.Path
		if appPath == "" {
			appPath = "."
		}
		// User Dockerfile lives under app dir as `Dockerfile` (canonical) or
		// the explicit path declared in loopzero.yaml `apps.<name>.dockerfile`.
		// app.Dockerfile is the relative-to-app-path filename.
		dfRelToApp := app.Dockerfile
		if dfRelToApp == "" {
			dfRelToApp = "Dockerfile"
		}
		// Compose path relative to source root. Use forward-slash join — these
		// are container paths, not host paths.
		dfPath := joinPaths(appPath, dfRelToApp)
		pairs = append(pairs, fmt.Sprintf("%s:%s", appPath, dfPath))
	}
	args := strings.Join(pairs, ",")

	script := fmt.Sprintf(
		"set -e\n/usr/local/bin/loopzero-dockerfile-lint --source %q --apps %q\n",
		workspaceMountPath, args,
	)
	return buildContainer("dockerfile-lint", dockerfileLintImage(), forge.ResourcesTiny(), script)
}

// dockerfileLintImage returns the linter image, allowing override via env.
// Local dev sets LOOPZERO_DOCKERFILE_LINT_IMAGE to use the k3d registry image.
func dockerfileLintImage() string {
	if v := os.Getenv("LOOPZERO_DOCKERFILE_LINT_IMAGE"); v != "" {
		return v
	}
	return imageDockerfileLint
}

func joinPaths(base, sub string) string {
	if base == "" || base == "." {
		return sub
	}
	if sub == "" {
		return base
	}
	if strings.HasSuffix(base, "/") {
		return base + sub
	}
	return base + "/" + sub
}
