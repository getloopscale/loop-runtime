// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/nomigrateforce"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "no-migrate-force",
		Summary: "Forbids forced/destructive DB migration operations.",
		Setup:   setupNoMigrateForce,
	})
}

func setupNoMigrateForce(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o, err := nomigrateforce.Check(nomigrateforce.Config{Root: inv.Root})
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Fail {
			// allowlist: migrate force (this is the gate's own diagnostic message)
			msg := fmt.Sprintf("%d unallowlisted occurrence(s) of 'migrate force' in production code. "+
				"Per CORRECTIONS.txt item B, forcing a migration on a production DB corrupts "+
				"schema_migrations. Use psql for ad-hoc SQL, OR add a '# allowlist: migrate force "+
				"(rationale)' marker on the same line or within 3 lines above.", len(o.Blocked))
			if os.Getenv("GITHUB_ACTIONS") != "" {
				inv.Stdout.Printf("::error::%s\n", msg)
			} else {
				inv.Stderr.Printf("[no-migrate-force] BLOCK: %s\n", msg)
			}
			inv.Stderr.Println("[no-migrate-force] blocked matches:")
			for _, b := range o.Blocked {
				inv.Stderr.Printf("  %s\n", b)
			}
			return o, nil
		}
		if o.MatchCount > 0 {
			inv.Stdout.Printf("[no-migrate-force] all %d matches allowlisted — OK.\n", o.MatchCount)
		} else {
			inv.Stdout.Println("[no-migrate-force] no matches — OK.")
		}
		return o, nil
	}
}
