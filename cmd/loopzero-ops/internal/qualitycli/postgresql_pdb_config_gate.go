// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pgpdb"
)

func init() {
	register(Gate{
		Name:    "postgresql-pdb-config",
		Summary: "Validates the PostgreSQL PodDisruptionBudget configuration.",
		Setup:   setupPostgresqlPDBConfig,
	})
}

// setupPostgresqlPDBConfig is the Go home for the postgresql-pdb-config gate
// (migrated from infra/tests/scripts/postgresql-pdb-config_test.sh). Guards the
// B2 regression: the postgresql chart must set primary.pdb.create: false.
func setupPostgresqlPDBConfig(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := pgpdb.Check(filepath.Join(inv.Root, "infra", "helm", "postgresql", "values.yaml"))
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Printf("postgresql-pdb-config: FAIL — %s\n", outcome.Message)
			return Fail, nil
		}
		inv.Stdout.Printf("postgresql-pdb-config: PASS — %s\n", outcome.Message)
		return Pass, nil
	}
}
