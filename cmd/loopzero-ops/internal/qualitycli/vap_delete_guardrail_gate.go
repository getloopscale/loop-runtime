// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/vapdeleteguardrail"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "vap-delete-guardrail",
		Summary: "Boundary resources (Application/Tenant/Environment/ClusterCredential) AND their CRDs are protected from unauthorized DELETE (D4).",
		Setup:   setupVAPDeleteGuardrail,
	})
}

// setupVAPDeleteGuardrail implements the `vap-delete-guardrail` gate (plan
// d4-vap-delete-guardrail T1.3, ADR 2026-08-07-capi-byoc-cluster-lifecycle D4).
//
// Guards the mgmt boundary resources — and their CRDs — against unauthorized DELETE.
// The CRD rule is not redundant with the instance rules: deleting the CRD collects every
// instance without any instance rule being consulted, which is the vector of the reference
// incident this policy exists to prevent.
func setupVAPDeleteGuardrail(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		rep := vapdeleteguardrail.Check(inv.Root, nil)
		for _, w := range rep.Warnings {
			inv.Stderr.Printf("vap-delete-guardrail: WARN: %s\n", w)
		}
		if rep.Verdict() == verdict.Pass {
			inv.Stdout.Println("vap-delete-guardrail: OK")
			return rep, nil
		}
		for _, f := range rep.Findings {
			inv.Stderr.Printf("vap-delete-guardrail: FAIL [%s] %s\n", f.Check, f.Message)
		}
		return rep, nil
	}
}
