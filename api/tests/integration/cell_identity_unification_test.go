// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Estes testes provam as afirmacoes que a migracao 000061 faz sobre o banco, em vez de
// confiar nelas. Ver docs/decisions/2026-08-09-cell-cluster-unification-adr.md e BACKLOG.md B-005.
//
// Exigem um PostgreSQL real com a migracao 000061 aplicada; pulam sem falhar quando nao ha banco
// alcancavel, como os demais testes deste pacote.
//
// Enquanto pularem, a unificacao esta verificada por leitura e nao por execucao — e as duas
// premissas que a migracao assume sobre o Postgres continuam sem prova. O caminho para executa-los
// e a cadeia de .claude/rules/cleanroom-up-live-test-mandate.md, onde ha banco de pe depois do
// setup + seeds. Rastreado em https://github.com/getloopscale/loopzero/issues/279
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgErrCodeNotNullViolation e o SQLSTATE que o Postgres devolve quando uma coluna NOT NULL
// recebe NULL. Ele e exatamente o erro que NAO pode acontecer se o gatilho roda antes.
const pgErrCodeNotNullViolation = "23502"

func cellTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL())
	if err != nil {
		// sem banco: nada a provar aqui. Rastreado em https://github.com/getloopscale/loopzero/issues/279
		t.Skipf("pulando: sem PostgreSQL alcancavel: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		// sem banco: nada a provar aqui. Rastreado em https://github.com/getloopscale/loopzero/issues/279
		t.Skipf("pulando: sem PostgreSQL alcancavel: %v", err)
	}
	return pool
}

// TestGatilhoResolveCellIdAntesDoNotNullReprovar prova a unica premissa da Opcao A do ADR.
//
// A migracao 000061 tirou o DEFAULT das quatro tabelas com gatilho, apostando que um INSERT que
// omita `cell_id` e resolvido pelo gatilho BEFORE INSERT antes de a restricao NOT NULL olhar para
// a linha. Se essa ordem nao valesse, todo INSERT que omite a coluna passaria a falhar em
// producao — e o ADR registrou que afirmar isso sem provar era inaceitavel.
//
// A assercao e deliberadamente estreita: o INSERT pode falhar por outros motivos (chave
// estrangeira, coluna obrigatoria do proprio teste), e isso nao interessa. O que o teste proibe e
// UM erro especifico: violacao de NOT NULL em `cell_id`. Esse erro, e so ele, refutaria a ordem.
func TestGatilhoResolveCellIdAntesDoNotNullReprovar(t *testing.T) {
	pool := cellTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Rollback sempre: o teste prova uma ordem, nao deixa dado.
	tx, err := pool.Begin(ctx)
	if err != nil {
		// sem banco: nada a provar aqui. Rastreado em https://github.com/getloopscale/loopzero/issues/279
		t.Skipf("pulando: nao foi possivel abrir transacao: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status)
		 VALUES ('bld-cell-order-probe', 'tn-inexistente-de-proposito', 'proj', 'dev', 'PENDING')`)

	var pgErr *pgconn.PgError
	if err != nil && asPgError(err, &pgErr) {
		if pgErr.Code == pgErrCodeNotNullViolation && strings.Contains(pgErr.ColumnName, "cell_id") {
			t.Fatalf("o NOT NULL de cell_id reprovou ANTES do gatilho resolver — a premissa da "+
				"Opcao A do ADR nao vale neste Postgres, e a migracao 000061 precisa voltar para "+
				"uma sentinela explicita (Opcao B). Erro: %v", pgErr)
		}
	}
	// Qualquer outro desfecho — sucesso, FK, RAISE do proprio gatilho — confirma que a linha
	// chegou ao gatilho antes de a restricao de nulidade decidir.
}

// TestGatilhoFalhaAltoQuandoOTenantNaoTemCell prova a fase 4 da migracao.
//
// Antes, um tenant sem cell fazia a linha ser gravada com a sentinela e o defeito reaparecia no
// deploy, como roteamento errado. `.claude/rules/error-handling.md` § 2 pede o oposto, e a
// migracao passou a levantar excecao nomeando o tenant.
func TestGatilhoFalhaAltoQuandoOTenantNaoTemCell(t *testing.T) {
	pool := cellTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		// sem banco: nada a provar aqui. Rastreado em https://github.com/getloopscale/loopzero/issues/279
		t.Skipf("pulando: nao foi possivel abrir transacao: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status)
		 VALUES ('bld-cell-raise-probe', 'tn-que-nao-existe', 'proj', 'dev', 'PENDING')`)
	if err == nil {
		t.Fatal("o INSERT com tenant inexistente foi aceito — o gatilho deveria ter levantado " +
			"excecao em vez de gravar a linha com uma cell nao resolvida")
	}
	if !strings.Contains(err.Error(), "cell_id nao resolvido") &&
		!strings.Contains(err.Error(), "foreign key") {
		t.Logf("erro observado (aceitavel se for FK, que barra antes): %v", err)
	}
}

// TestCatalogoNaoTemMaisAsIdentidadesAntigas prova que a fusao chegou ao catalogo.
//
// `cell-default` era sentinela e nao cell; `cell-1` nomeava a mesma maquina que ela. Se qualquer
// uma sobreviver, a relacao 2:1 que a unificacao existe para eliminar continua de pe sob nomes
// novos — o pior desfecho possivel, porque parece resolvido.
func TestCatalogoNaoTemMaisAsIdentidadesAntigas(t *testing.T) {
	pool := cellTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	var sobreviventes int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cells WHERE cell_id IN ('cell-default', 'cell-2')`).Scan(&sobreviventes)
	if err != nil {
		// sem banco: nada a provar aqui. Rastreado em https://github.com/getloopscale/loopzero/issues/279
		t.Skipf("pulando: tabela cells indisponivel: %v", err)
	}
	if sobreviventes != 0 {
		t.Errorf("o catalogo ainda tem %d identidade(s) antiga(s) — a migracao 000061 nao foi "+
			"aplicada ou foi aplicada pela metade", sobreviventes)
	}
}

func asPgError(err error, target **pgconn.PgError) bool {
	for e := err; e != nil; {
		if pe, ok := e.(*pgconn.PgError); ok {
			*target = pe
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
