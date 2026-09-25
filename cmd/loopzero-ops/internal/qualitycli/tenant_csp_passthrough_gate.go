// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/tenantcsppassthrough"
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
	return func(inv Invocation) (Outcome, error) {
		out, err := tenantcsppassthrough.Check(tenantcsppassthrough.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if out.Skipped {
			fmt.Fprintf(inv.Stdout, "tenant-csp-passthrough: SKIP — %s\n", out.Reason)
			return Pass, nil
		}
		if out.Pass {
			fmt.Fprintf(inv.Stdout, "tenant-csp-passthrough: PASS — %s\n", out.Reason)
			return Pass, nil
		}
		fmt.Fprintf(inv.Stderr, "tenant-csp-passthrough: FAIL — static-CSP middleware attached to tenant routes: %v\n", out.Attached)
		fmt.Fprintln(inv.Stderr, "Every LoopZeroKit per-request nonce is replaced by that fixed policy; inline scripts are blocked silently in the browser (issue #59).")
		return Fail, nil
	}
}
