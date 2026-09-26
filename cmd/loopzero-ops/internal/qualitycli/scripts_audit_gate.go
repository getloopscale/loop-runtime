// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"io"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scriptsaudit"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "scripts-audit",
		Summary: "Audits shell scripts against the allowlist ledger.",
		// emits the CSV ledger; no-new-bash is the gate
		Wiring: utility,
		Setup:  setupScriptsAudit,
	})
}

func setupScriptsAudit(fs *flag.FlagSet) Check {
	output := fs.String("output", "", "write CSV to file (default: stdout)")
	return func(inv Invocation) (verdict.Result, error) {
		scripts, err := scriptsaudit.ScanRepo(inv.Root)
		if err != nil {
			return nil, err
		}

		var w io.Writer
		if *output != "" {
			f, err := os.Create(*output)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			w = f
		} else {
			w = inv.Stdout
		}

		if err := scriptsaudit.EmitCSV(scripts, w); err != nil {
			return nil, err
		}
		if *output != "" {
			// Visibility for operator running interactively
			inv.Stderr.Printf("scripts-audit: %d scripts inventoried to %s\n", len(scripts), *output)
		}
		return scripts, nil
	}
}
