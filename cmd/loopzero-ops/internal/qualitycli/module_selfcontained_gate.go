// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/moduleselfcontained"
)

func init() {
	register(Gate{
		Name:    "module-selfcontained",
		Summary: "Every Go module must build without go.work, which is what CI does.",
		Setup:   setupModuleSelfContained,
	})
}

// runBillingLayers verifica que a CADEIA de cobrança continua enumerável, com dono.
//
// O irmão `billing-secrets` cobre UMA camada. Este cobre a lista das cinco — e é a lista
// que o B-038 mede: "a quinta camada nasce sem a verificação que as outras quatro tinham".
func setupModuleSelfContained(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := moduleselfcontained.Check(moduleselfcontained.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, moduleselfcontained.ErrNotSelfContained) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "module-selfcontained: FAIL — %v\n", err)
			for _, f := range out.Failed {
				fmt.Fprintf(inv.Stderr, "  %s:\n%s\n", f.Module, f.Err)
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "module-selfcontained: PASS — %s\n", out.Reason)
		return Pass, nil
	}
}
