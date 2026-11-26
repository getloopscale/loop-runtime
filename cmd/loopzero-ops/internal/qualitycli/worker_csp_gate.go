// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/workercsp"
)

func init() {
	register(Gate{
		Name:    "worker-csp",
		Summary: "Cloudflare static worker CSP must not permit inline/eval script (#235).",
		Setup:   setupWorkerCSP,
	})
}

// setupWorkerCSP is the #235 gate: the Cloudflare static worker's CSP must not
// permit arbitrary inline/eval'd script. Registered but NOT wired into
// quality:gates — the finding is real and its fix lives in a
// governance-gated path; see the failingGate entry in quality_gate_wiring_test.go.
func setupWorkerCSP(fs *flag.FlagSet) Check {
	worker := fs.String("worker", "", "worker source path relative to root")
	return func(inv Invocation) (verdict.Result, error) {
		out, err := workercsp.Check(workercsp.Config{RepoRoot: inv.Root, WorkerPath: *worker})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Skip {
			inv.Stdout.Printf("worker-csp: SKIP — %s\n", out.Reason)
			return out, nil
		}
		if out.Verdict() == verdict.Pass {
			inv.Stdout.Printf("worker-csp: PASS — %s\n", out.Reason)
			return out, nil
		}
		inv.Stderr.Println("worker-csp: FAIL")
		for _, f := range out.Findings {
			inv.Stderr.Printf("  %s:%d — %s\n", workercsp.DefaultWorkerPath, f.Line, f.Detail)
		}
		inv.Stderr.Println("Fix: drop 'unsafe-inline' from script-src; use a per-request nonce or external+hashed scripts (issue #235).")
		return out, nil
	}
}
