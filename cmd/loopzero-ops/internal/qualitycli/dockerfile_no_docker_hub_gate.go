// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"path/filepath"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/dockerfilemirror"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "dockerfile-no-docker-hub",
		Summary: "Forbids unqualified docker.io/Docker Hub base images.",
		Setup:   setupDockerfileNoDockerHub,
	})
}

// setupDockerfileNoDockerHub is the Go home for the dockerfile-no-docker-hub gate
// (migrated from infra/tests/scripts/dockerfile-no-docker-hub_test.sh). Fails if
// any infra/images/*/Dockerfile FROM would resolve to Docker Hub.
func setupDockerfileNoDockerHub(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := dockerfilemirror.Check(dockerfilemirror.Config{
			ImagesDir: filepath.Join(inv.Root, "infra", "images"),
		})
		if err != nil {
			return nil, err
		}
		inv.Stdout.Printf("%d/%d Dockerfile FROMs OK\n", outcome.OK, outcome.Total)
		if outcome.Verdict() == verdict.Fail {
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("FAIL: %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("dockerfile-no-docker-hub: PASS — all FROMs use a mirror/private registry")
		return outcome, nil
	}
}
