// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/packageadmission"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "package-admission",
		Summary: "B-058: the k8s plumbing package admits no domain vocabulary, and every home of the split stays populated.",
		Setup:   setupPackageAdmission,
	})
}

// setupPackageAdmission — B-058.
//
// The item is about an admission CRITERION that can say no. api/internal/k8s
// admitted by technology ("it uses client-go"), which admits every domain that
// happens to speak to the API server. The criterion is now "plumbing names no
// domain", and this gate is what makes it a rule rather than a paragraph.
//
// It fails in three distinguishable ways on purpose, because the fixes differ:
// a domain name readmitted into the plumbing package (move it to a home), a home
// that emptied out (the split was undone, and a gate green over a vacuum is the
// failure adapter-ports records), and nothing scanned at all.
func setupPackageAdmission(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := packageadmission.Check(packageadmission.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("package-admission: FAIL \u2014 %v\n", out.Violation)
			for _, v := range out.Violations {
				inv.Stderr.Printf("  api/internal/k8s/%s: %s carries the domain word %q \u2014 move it to k8s/tenancy, k8s/workload or k8s/secretsync\n", v.File, v.Where, v.Word)
			}
			return out, nil
		}
		inv.Stdout.Printf("package-admission: PASS \u2014 %d plumbing file(s) in api/internal/k8s name no domain; homes %v are populated\n", out.Scanned, out.Homes)
		return out, nil
	}
}
