// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmfilecontextparity"
)

func init() {
	register(Gate{
		Name:    "helmfile-context-parity",
		Summary: "A task that invokes a helmfile targets the cluster that helmfile declares (B-044).",
		Setup:   setupHelmfileContextParity,
	})
}

// setupHelmfileContextParity — B-044 / slice-3 T1.3.
//
// Obtains the property that helmfile itself does NOT provide: a task whose
// --kube-context disagrees with the kubeContext its helmfile declares reproves.
// Measured against helmfile 1.1.6, the flag silently overrides the declaration,
// so declaring the context fixes the dangerous default and this gate supplies
// the conflict.
func setupHelmfileContextParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := helmfilecontextparity.Check(helmfilecontextparity.Config{RepoRoot: inv.Root})
		fmt.Fprintln(inv.Stdout, o.Summary())
		for _, d := range o.Divergences {
			fmt.Fprintf(inv.Stdout, "  DIVERGES %s:%d — %s is invoked with --kube-context %q but declares %q\n",
				d.Taskfile, d.Line, d.Helmfile, d.Invoked, d.Declared)
		}
		for _, m := range o.MissingEnv {
			fmt.Fprintf(inv.Stdout, "  MISSING VAR %s\n", m)
		}
		for _, u := range o.Undeclared {
			fmt.Fprintf(inv.Stdout, "  NO CONTEXT %s — a `helmfile sync` without the flag would inherit kubectl's active context\n", u)
		}
		if err != nil {
			if !isViolation(err, helmfilecontextparity.ErrContextDiverges, helmfilecontextparity.ErrContextNotDeclared, helmfilecontextparity.ErrRequiredEnvNotSet) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "helmfile-context-parity: %v\n", err)
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "PASS — every literal helmfile invocation targets the cluster its helmfile declares")
		return Pass, nil
	}
}
