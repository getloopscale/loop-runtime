// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/skiplink"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "skip-link",
		Summary: "Every t.Skip/it.skip carries an issue URL (or lives in *_skip_test.go).",
		NoRoot:  true,
		Setup:   setupSkipLink,
	})
}

func setupSkipLink(fs *flag.FlagSet) Check {
	dir := fs.String("dir", ".", "target directory to scan")
	baseline := fs.String("baseline", "infra/quality/skip-link-baseline.txt", "baseline file")
	ignoreBaseline := fs.Bool("ignore-baseline", false, "treat all violations as new")
	emitBaseline := fs.Bool("emit-baseline", false, "print baseline keys for every violation, one per line, and exit 0")
	return func(inv Invocation) (verdict.Result, error) {
		o, err := skiplink.Check(skiplink.Config{
			TargetDir:      *dir,
			BaselineFile:   *baseline,
			IgnoreBaseline: *ignoreBaseline || *emitBaseline,
		})
		if err != nil {
			return nil, err
		}
		// A baseline nobody can regenerate is a baseline that decays. The keys are
		// not derivable from the human-facing output (which carries absolute paths
		// and line numbers), so emitting them is the only reproducible way to
		// re-freeze after a deliberate sweep.
		if *emitBaseline {
			for _, k := range o.Keys {
				inv.Stdout.Println(k)
			}
			return o, nil
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("[skip-link] FAIL: %d NEW skip(s) without issue URL:\n", len(o.NewViolations))
			for _, v := range o.NewViolations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("[skip-link] fix: add a GitHub issue URL near the Skip, OR rename to *_skip_test.go")
			if o.BaselineCount > 0 {
				inv.Stderr.Printf("[skip-link] note: %d grandfathered violations in baseline — do NOT add new entries\n", o.BaselineCount)
			}
			return o, nil
		}
		if o.BaselineCount > 0 {
			inv.Stdout.Printf("[skip-link] PASS — no NEW violations (%d grandfathered)\n", o.BaselineCount)
		} else {
			inv.Stdout.Println("[skip-link] PASS — all skip calls have issue URL")
		}
		return o, nil
	}
}
