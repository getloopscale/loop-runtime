// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/noaesremaining"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "no-aes-remaining",
		Summary: "Calendar-gated: fails on remaining AES code paths post-transit migration.",
		// ENFORCED since 2026-08-01 (T+60d deadline) with the legacy AES branch still
		// in the dispatcher — 4 findings in
		// api/internal/db/store/secret_wrapper.go:{148,152,188,192}, measured
		// 2026-08-06. The T10.3 debt (delete the branch) stays open: removing the
		// AES read path before proving no persisted secret uses the old prefix makes
		// that data unrecoverable, so the fix needs migration evidence this
		// repository does not carry.
		Wiring: failingGate,
		Setup:  setupNoAESRemaining,
	})
}

// setupNoAESRemaining is the Go home for the no-aes-remaining gate (T10.2 of
// plan 2026-06-02-openbao-adoption v1.3). Calendar-gated — enforces only
// after T+60d from CutoverDay.
func setupNoAESRemaining(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	return func(inv Invocation) (verdict.Result, error) {
		cfg := noaesremaining.Config{RepoRoot: inv.Root}
		if *allowlist != "" {
			for _, p := range strings.Split(*allowlist, ",") {
				cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
			}
		}
		outcome, err := noaesremaining.Check(cfg)
		if err != nil {
			return nil, err
		}
		inv.Stderr.Print(noaesremaining.FormatOutcome(outcome))
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
