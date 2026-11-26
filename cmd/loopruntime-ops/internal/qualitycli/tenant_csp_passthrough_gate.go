// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/tenantcsppassthrough"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "tenant-csp-passthrough",
		Summary: "Static-CSP Traefik middleware must not be attached to tenant routes (#59).",
		Setup:   setupTenantCSPPassthrough,
	})
}

// setupTenantCSPPassthrough is the #59 regression lock: a static-CSP Traefik
// middleware must never be attached to tenant IngressRoutes, or LoopZeroKit's
// per-request nonce stops matching the body and every inline script dies.
func setupTenantCSPPassthrough(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := tenantcsppassthrough.Check(tenantcsppassthrough.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Skip {
			inv.Stdout.Printf("tenant-csp-passthrough: SKIP — %s\n", out.Reason)
			return out, nil
		}
		if out.Verdict() == verdict.Pass {
			inv.Stdout.Printf("tenant-csp-passthrough: PASS — %s\n", out.Reason)
			return out, nil
		}
		inv.Stderr.Printf("tenant-csp-passthrough: FAIL — static-CSP middleware attached to tenant routes: %v\n", out.Attached)
		inv.Stderr.Println("Every LoopZeroKit per-request nonce is replaced by that fixed policy; inline scripts are blocked silently in the browser (issue #59).")
		return out, nil
	}
}
