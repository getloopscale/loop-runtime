// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/argoappidentity"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "argo-app-identity",
		Summary: "The ArgoCD Application name is derived only from naming.ArgoAppName (dropping the project made two projects of a tenant collide on one Application).",
		Setup:   setupArgoAppIdentity,
	})
}

// setupArgoAppIdentity is the Go home for the argo-app-identity gate.
//
// Keeps the ArgoCD Application name derived in exactly one place —
// pkg/naming.ArgoAppName(tenant, project, env). Ten sites across two modules rebuilt it
// by hand as {tenant}-{env}, dropping the project, so two projects of one tenant in one
// environment collided on a single Application with conflicting destinations (#271).
//
// Two of the ten were inside pruneOrphanPreviewApps, which DELETES every Application
// outside the set it builds — and that delete cascades through the ArgoCD finalizer into
// the tenant's workload. Renaming the emit side without them would have made the operator
// destroy exactly what it had just created.
func setupArgoAppIdentity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		rep, err := argoappidentity.Check(inv.Root)
		if err != nil {
			return nil, err
		}
		if rep.Verdict() == verdict.Pass {
			inv.Stdout.Printf("argo-app-identity: PASS — %d file(s) scanned; Application and Environment CR "+
				"identities derived only from naming.ArgoAppName / naming.EnvironmentCRName\n", rep.Scanned)
			return rep, nil
		}
		for _, f := range rep.Findings {
			inv.Stderr.Printf("argo-app-identity: FAIL %s\n", f.Message())
		}
		return rep, nil
	}
}
