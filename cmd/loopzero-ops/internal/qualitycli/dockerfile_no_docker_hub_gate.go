// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dockerfilemirror"
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
	return func(inv Invocation) (Outcome, error) {
		outcome, err := dockerfilemirror.Check(dockerfilemirror.Config{
			ImagesDir: filepath.Join(inv.Root, "infra", "images"),
		})
		if err != nil {
			return Fail, err
		}
		fmt.Fprintf(inv.Stdout, "%d/%d Dockerfile FROMs OK\n", outcome.OK, outcome.Total)
		if !outcome.Pass {
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "FAIL: %s\n", v)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "dockerfile-no-docker-hub: PASS — all FROMs use a mirror/private registry")
		return Pass, nil
	}
}
