// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/caddyfilenodockerip"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "caddyfile-no-docker-ip",
		Summary: "Blocks hardcoded 172.18.x.x Docker network IPs in infra/local/Caddyfile + enforces canonical https://127.0.0.1:8443 upstream in both wildcard blocks (M-Migrate2 2026-07-07 replaces infra/tests/check-caddyfile-no-docker-network-ip_test.sh — closes Red Team loop-console#127 guard).",
		Setup:   setupCaddyfileNoDockerIP,
	})
}

// setupCaddyfileNoDockerIP enforces that the canonical infra/local/Caddyfile does
// NOT hardcode Docker network IPs (172.18.x.x) in non-comment lines and that
// both wildcard blocks reverse_proxy to the stable https://127.0.0.1:8443.
// Migrated from infra/tests/check-caddyfile-no-docker-network-ip_test.sh
// (M-Migrate2 continuation 2026-07-07 — closes Red Team loop-console#127 guard).
func setupCaddyfileNoDockerIP(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := caddyfilenodockerip.Check(caddyfilenodockerip.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("caddyfile-no-docker-ip: FAIL — %d violation(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("caddyfile-no-docker-ip: PASS — Caddyfile canonical (no 172.18.x.x, both wildcard blocks reach the cells over loopback host ports)")
		return outcome, nil
	}
}
