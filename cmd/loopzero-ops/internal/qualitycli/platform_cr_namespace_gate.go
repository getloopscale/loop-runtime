// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/platformcrnamespace"
)

func init() {
	register(Gate{
		Name:    "platform-cr-namespace",
		Summary: "Tenant/Environment CRs in a versioned manifest must live in loopzero-system.",
		Setup:   setupPlatformCRNamespace,
	})
}

// setupPlatformCRNamespace recusa manifesto versionado que aplique Tenant/Environment fora de
// `loopzero-system` (B-095). Fora do lugar o CR fica invisivel ao operator, cujo cache e' restrito a
// esse namespace — e indeletavel se a cerca de admissao for ligada, porque remover finalizer e'
// um UPDATE.
func setupPlatformCRNamespace(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := platformcrnamespace.Check(platformcrnamespace.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, platformcrnamespace.ErrOutsideNamespace) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "platform-cr-namespace: %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "platform-cr-namespace: PASS — %d platform CR(s), all in %s (%d file(s))\n",
			len(out.Checked), platformcrnamespace.CanonicalNamespace, out.Files)
		return Pass, nil
	}
}
