// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/billinglayers"
)

func init() {
	register(Gate{
		Name:    "billing-layers",
		Summary: "Billing layers must be enumerable and owned; closed debt without coverage fails.",
		Setup:   setupBillingLayers,
	})
}

func setupBillingLayers(fs *flag.FlagSet) Check {
	sibling := fs.String("sibling-root", "", "raiz de ../loopzero-cloud (padrão: irmão do repo-root)")
	return func(inv Invocation) (Outcome, error) {
		if *sibling == "" {
			*sibling = filepath.Join(inv.Root, "..", "loopzero-cloud")
		}

		out, err := billinglayers.Check(billinglayers.Config{RepoRoot: inv.Root, SiblingRoot: *sibling})
		if err != nil {
			if !isViolation(err, billinglayers.ErrUndeclaredSurface, billinglayers.ErrBelowFloor, billinglayers.ErrGuardUnresolved, billinglayers.ErrRegistryMalformed) {
				return Fail, err
			}
			// Não existe ramo que devolva 0 sem veredito: uma guarda que não resolve, uma
			// dívida fechada sem cobertura, ou uma camada nova não declarada reprovam aqui.
			fmt.Fprintf(inv.Stderr, "billing-layers: FAIL — %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "billing-layers: PASS — %s\n", out.Reason)
		return Pass, nil
	}
}
