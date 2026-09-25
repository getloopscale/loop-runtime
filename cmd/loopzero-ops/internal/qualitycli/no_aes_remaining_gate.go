// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noaesremaining"
)

func init() {
	register(Gate{
		Name:    "no-aes-remaining",
		Summary: "Calendar-gated: fails on remaining AES code paths post-transit migration.",
		// Virou ENFORCED em 2026-08-01 (prazo T+60d) com o branch AES legado ainda no
		// dispatcher — 4 achados em api/internal/db/store/secret_wrapper.go:{148,152,188,192}.
		// Medido 2026-08-06. A divida T10.3 (deletar o branch) segue aberta: remover o
		// caminho de leitura AES antes de provar que nenhum segredo persistido usa o
		// prefixo antigo torna esses dados irrecuperaveis, entao a correcao exige
		// evidencia de migracao que este repo nao carrega.
		Wiring: failingGate,
		Setup:  setupNoAESRemaining,
	})
}

// setupNoAESRemaining is the Go home for the no-aes-remaining gate (T10.2 of
// plan 2026-06-02-openbao-adoption v1.3). Calendar-gated — enforces only
// after T+60d from CutoverDay.
func setupNoAESRemaining(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	return func(inv Invocation) (Outcome, error) {
		cfg := noaesremaining.Config{RepoRoot: inv.Root}
		if *allowlist != "" {
			for _, p := range strings.Split(*allowlist, ",") {
				cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
			}
		}
		outcome, err := noaesremaining.Check(cfg)
		if err != nil {
			return Fail, err
		}
		fmt.Fprint(inv.Stderr, noaesremaining.FormatOutcome(outcome))
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
