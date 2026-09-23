// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
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

// usageDB is a SqlcBridge whose generated DBTX records the last query.
type usageDB struct {
	db.DBTX
	row  usageRow
	sql  string
	args []any
}

func (u *usageDB) GeneratedDB() generated.DBTX { return usageGen{u} }

// usageGen is the generated.DBTX face of usageDB.
type usageGen struct{ u *usageDB }

func (usageGen) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("usageDB: Exec not expected")
}

func (usageGen) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("usageDB: Query not expected")
}

func (g usageGen) QueryRow(_ context.Context, sql string, args ...interface{}) pgx.Row {
	g.u.sql, g.u.args = sql, args
	return g.u.row
}

func TestPlanUsageStore_BuildsThisMonthCountsFromTheFirstInstantOfTheUTCMonth(t *testing.T) {
	fake := &usageDB{row: usageRow{n: 7}}
	s := &PostgresPlanUsageStore{DB: fake, Now: func() time.Time {
		return time.Date(2026, 9, 23, 18, 30, 0, 0, time.FixedZone("BRT", -3*3600))
	}}

	n, err := s.BuildsThisMonth(context.Background(), "tn-a")

	if err != nil || n != 7 {
		t.Fatalf("got (%d, %v), want (7, nil)", n, err)
	}
	since := fake.args[1].(pgtype.Timestamptz).Time
	if want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC); !since.Equal(want) {
		t.Fatalf("month start = %s, want %s", since, want)
	}
	if fake.args[0] != "tn-a" || !strings.Contains(fake.sql, "FROM builds") {
		t.Fatalf("unexpected query %q args %v", fake.sql, fake.args)
	}
}

func TestPlanUsageStore_PermanentEnvironmentsCountsEveryProjectEnvironmentTuple(t *testing.T) {
	fake := &usageDB{row: usageRow{n: 5}}
	s := NewPostgresPlanUsageStore(fake)

	n, err := s.PermanentEnvironments(context.Background(), "tn-a")

	if err != nil || n != 5 {
		t.Fatalf("got (%d, %v), want (5, nil)", n, err)
	}
	if !strings.Contains(fake.sql, "COUNT(*)") || !strings.Contains(fake.sql, "kind = 'permanent'") {
		t.Fatalf("the count must be per (project, environment) tuple and exclude previews: %q", fake.sql)
	}
}

func TestPlanUsageStore_SeatsCountsTenantMembers(t *testing.T) {
	fake := &usageDB{row: usageRow{n: 3}}
	s := NewPostgresPlanUsageStore(fake)

	n, err := s.Seats(context.Background(), "tn-a")

	if err != nil || n != 3 || !strings.Contains(fake.sql, "FROM tenant_members") {
		t.Fatalf("got (%d, %v) from %q", n, err, fake.sql)
	}
}

func TestPlanUsageStore_ReturnsTheQueryErrorInsteadOfZero(t *testing.T) {
	boom := errors.New("connection reset")
	counters := map[string]func(*PostgresPlanUsageStore) (int, error){
		"builds": func(s *PostgresPlanUsageStore) (int, error) { return s.BuildsThisMonth(context.Background(), "tn-a") },
		"environments": func(s *PostgresPlanUsageStore) (int, error) {
			return s.PermanentEnvironments(context.Background(), "tn-a")
		},
		"seats": func(s *PostgresPlanUsageStore) (int, error) { return s.Seats(context.Background(), "tn-a") },
	}
	for name, count := range counters {
		t.Run(name, func(t *testing.T) {
			_, err := count(NewPostgresPlanUsageStore(&usageDB{row: usageRow{err: boom}}))
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
