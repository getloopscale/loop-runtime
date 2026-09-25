// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/migrationtestcoverage"
)

func init() {
	register(Gate{
		Name:    "migration-test-coverage",
		Summary: "Every DB migration has corresponding test coverage.",
		// 44 migrations with no matching api/tests/unit/migration_*_test.go (re-measured
		// 2026-09-06, B-067; era 42 aqui, 43 no item e 48 antes desta fatia).
		//
		// A parte "That is 42 pieces of real work, not a mislabel" ERA FALSA e foi medida:
		// CINCO das violacoes eram mislabel. `migration_loopzero_api_keys_test.go`,
		// `migration_memory_p3_test.go`, `migration_api_key_rotation_test.go` e
		// `migration_webhook_delivery_test.go` — 37 funcoes de teste e 68 asserts, verdes —
		// abrem o proprio `.up.sql` das migrations 000024/000025/000026/000031/000033 e
		// mesmo assim eram acusadas, porque o nome do arquivo nao comecava com o numero.
		// Renomear os quatro derrubou a contagem de 48 para 44 sem afrouxar o gate.
		//
		// O que continua verdadeiro, e e' a razao de nao alargar o casamento: o gate confere
		// que o ARQUIVO existe, entao stubs vazios o deixariam verde sem testar nada — a
		// barra e' a forma canonica de migration_012_fk_on_delete_test.go. Casar por
		// CONTEUDO trocaria este falso positivo por um falso negativo pior (mencionar o nome
		// da migration num comentario passaria a contar como teste).
		//
		// Residual conhecido: 000026_memory_exports segue acusada e E' testada — um arquivo
		// so' pode carregar um prefixo numerico. Ver a nota em migration_000025_000026_memory_test.go.
		Wiring: failingGate,
		Setup:  setupMigrationTestCoverage,
	})
}

// setupMigrationTestCoverage is the gate replacing migration_test_coverage.sh:
// every DB migration must have a regression test. Pure-logic, hermetic.
func setupMigrationTestCoverage(fs *flag.FlagSet) Check {
	migrationsDir := fs.String("migrations-dir", "", "override migrations dir")
	testsDir := fs.String("tests-dir", "", "override unit tests dir")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := migrationtestcoverage.Check(migrationtestcoverage.Config{
			Root:          inv.Root,
			MigrationsDir: *migrationsDir,
			UnitTestsDir:  *testsDir,
		})
		if err != nil {
			return Fail, err
		}
		for _, m := range outcome.Messages {
			fmt.Fprintln(inv.Stdout, m)
		}
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
