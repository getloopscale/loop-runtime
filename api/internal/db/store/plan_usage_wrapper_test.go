// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// usageRow answers a single-integer COUNT query, or fails it.
type usageRow struct {
	n   int32
	err error
}

func (r usageRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int32)) = r.n
	return nil
}

// usageTx records what a count does inside its transaction.
type usageTx struct {
	pgx.Tx
	row        usageRow
	scopeErr   error
	scopedTo   any
	sql        string
	args       []any
	rolledBack bool
}

func (tx *usageTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "set_config('app.tenant_id'") {
		tx.scopedTo = args[0]
		return pgconn.CommandTag{}, tx.scopeErr
	}
	return pgconn.CommandTag{}, errors.New("usageTx: unexpected Exec " + sql)
}

func (tx *usageTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if tx.scopedTo == nil {
		return usageRow{err: errors.New("usageTx: count ran before the tenant scope was set")}
	}
	tx.sql, tx.args = sql, args
	return tx.row
}

func (tx *usageTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

// usagePool hands out usageTx and records the options it was asked for.
type usagePool struct {
	tx       *usageTx
	beginErr error
	opts     pgx.TxOptions
}

func (p *usagePool) BeginTx(_ context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	p.opts = opts
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return p.tx, nil
}

func TestPlanUsageStore_CountsInsideAReadOnlyTransactionScopedToTheTenant(t *testing.T) {
	pool := &usagePool{tx: &usageTx{row: usageRow{n: 3}}}
	s := NewPostgresPlanUsageStore(pool)

	n, err := s.Seats(context.Background(), "tn-a")

	if err != nil || n != 3 {
		t.Fatalf("got (%d, %v), want (3, nil)", n, err)
	}
	if pool.opts.AccessMode != pgx.ReadOnly {
		t.Errorf("access mode = %q, want read only", pool.opts.AccessMode)
	}
	if pool.tx.scopedTo != "tn-a" {
		t.Errorf("RLS scope = %v, want tn-a — under fail-closed RLS an unscoped count reads zero", pool.tx.scopedTo)
	}
	if !pool.tx.rolledBack {
		t.Error("the transaction must be closed")
	}
	if !strings.Contains(pool.tx.sql, "FROM tenant_members") {
		t.Errorf("unexpected query %q", pool.tx.sql)
	}
}

func TestPlanUsageStore_BuildsThisMonthCountsFromTheFirstInstantOfTheUTCMonth(t *testing.T) {
	pool := &usagePool{tx: &usageTx{row: usageRow{n: 7}}}
	s := &PostgresPlanUsageStore{Pool: pool, Now: func() time.Time {
		return time.Date(2026, 9, 23, 18, 30, 0, 0, time.FixedZone("BRT", -3*3600))
	}}

	n, err := s.BuildsThisMonth(context.Background(), "tn-a")

	if err != nil || n != 7 {
		t.Fatalf("got (%d, %v), want (7, nil)", n, err)
	}
	since := pool.tx.args[1].(pgtype.Timestamptz).Time
	if want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC); !since.Equal(want) {
		t.Fatalf("month start = %s, want %s", since, want)
	}
	if pool.tx.args[0] != "tn-a" || !strings.Contains(pool.tx.sql, "FROM builds") {
		t.Fatalf("unexpected query %q args %v", pool.tx.sql, pool.tx.args)
	}
}

func TestPlanUsageStore_PermanentEnvironmentsCountsEveryProjectEnvironmentTuple(t *testing.T) {
	pool := &usagePool{tx: &usageTx{row: usageRow{n: 5}}}

	n, err := NewPostgresPlanUsageStore(pool).PermanentEnvironments(context.Background(), "tn-a")

	if err != nil || n != 5 {
		t.Fatalf("got (%d, %v), want (5, nil)", n, err)
	}
	if !strings.Contains(pool.tx.sql, "COUNT(*)") || !strings.Contains(pool.tx.sql, "kind = 'permanent'") {
		t.Fatalf("the count must be per (project, environment) tuple and exclude previews: %q", pool.tx.sql)
	}
}

func TestPlanUsageStore_ReturnsTheFailureInsteadOfZero(t *testing.T) {
	boom := errors.New("connection reset")
	cases := map[string]*usagePool{
		"begin":  {beginErr: boom},
		"scope":  {tx: &usageTx{scopeErr: boom}},
		"select": {tx: &usageTx{row: usageRow{err: boom}}},
	}
	for name, pool := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewPostgresPlanUsageStore(pool).Seats(context.Background(), "tn-a")
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v, want it to wrap %v", err, boom)
			}
		})
	}
}

func TestPlanUsageStore_WithoutADatabaseReturnsAnError(t *testing.T) {
	var s *PostgresPlanUsageStore

	if _, err := s.Seats(context.Background(), "tn-a"); err == nil {
		t.Fatal("a store with no database must refuse to count, not answer zero")
	}
}
