// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/securitycontext"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "security-context",
		Summary: "Enforces pod securityContext (non-root, limits, probes).",
		Setup:   setupSecurityContext,
	})
}

func setupSecurityContext(fs *flag.FlagSet) Check {
	gate := fs.Bool("gate", false, "gate mode: exit 1 if any NON-CONFORMANT chart lacks an ADR exception")
	return func(inv Invocation) (verdict.Result, error) {
		o, err := securitycontext.Check(securitycontext.Config{Root: inv.Root, Gate: *gate})
		if err != nil {
			return nil, err
		}

		if *gate {
			if o.Verdict() == verdict.Pass {
				inv.Stdout.Println("✅ securityContext audit GREEN — 0 NON-CONFORMANT charts without ADR exception.")
				return o, nil
			}
			inv.Stderr.Printf("❌ securityContext audit FAILED — %d NON-CONFORMANT chart(s) without ADR:\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  - %s\n", v)
				inv.Stderr.Printf("    Action: fix securityContext in infra/helm/%s/, OR create infra/helm/%s/ADR-securitycontext-exception.md\n", v, v)
			}
			return o, nil
		}

		// report mode — markdown matrix to stdout.
		yn := func(b bool) string {
			if b {
				return "yes"
			}
			return "no"
		}
		inv.Stdout.Println("# securityContext Audit — infra/helm/")
		inv.Stdout.Println()
		inv.Stdout.Printf("**Charts inspected:** %d\n\n", len(o.Charts))
		inv.Stdout.Println("| Chart | runAsNonRoot | readOnlyRootFS | drop:[ALL] | allowPrivilegeEscalation:false | Status | ADR Exception |")
		inv.Stdout.Println("|---|---|---|---|---|---|---|")
		var conformant, partial, nonconf, na int
		for _, c := range o.Charts {
			if !c.PodBearing {
				inv.Stdout.Printf("| %s | n/a | n/a | n/a | n/a | N/A (no pods) | — |\n", c.Name)
				na++
				continue
			}
			adr := "—"
			if c.HasADR {
				adr = "✅ exists"
			} else if c.Status == "NON-CONFORMANT" {
				adr = "❌ missing"
			}
			inv.Stdout.Printf("| %s | %s | %s | %s | %s | %s | %s |\n",
				c.Name, yn(c.RunAsNonRoot), yn(c.ReadOnlyRootFS), yn(c.DropAll), yn(c.NoPrivEsc), c.Status, adr)
			switch c.Status {
			case "CONFORMANT":
				conformant++
			case "PARTIAL":
				partial++
			case "NON-CONFORMANT":
				nonconf++
			}
		}
		inv.Stdout.Printf("\n## Summary\n\n- CONFORMANT: %d/%d\n- PARTIAL: %d/%d\n- NON-CONFORMANT: %d/%d\n- N/A (no pods): %d/%d\n- NON-CONFORMANT without ADR: %d\n",
			conformant, len(o.Charts), partial, len(o.Charts), nonconf, len(o.Charts), na, len(o.Charts), len(o.Violations))
		return o, nil
	}
}
