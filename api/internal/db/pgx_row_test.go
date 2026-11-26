// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type scanErrRow struct{ err error }

func (r scanErrRow) Scan(...any) error { return r.err }

func TestPgxRow_NoRowsBecomesErrNoRows(t *testing.T) {
	if err := (&pgxRow{row: scanErrRow{err: pgx.ErrNoRows}}).Scan(); !errors.Is(err, ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
}

func TestPgxRow_UniqueViolationIsTyped(t *testing.T) {
	err := (&pgxRow{row: scanErrRow{err: &pgconn.PgError{Code: "23505", ConstraintName: "projects_pkey"}}}).Scan()
	var uv *UniqueViolationError
	if !errors.As(err, &uv) || uv.Constraint != "projects_pkey" {
		t.Fatalf("want *UniqueViolationError on projects_pkey, got %v", err)
	}
}

func TestPgxRow_OtherErrorsPassThrough(t *testing.T) {
	boom := errors.New("connection refused")
	if err := (&pgxRow{row: scanErrRow{err: boom}}).Scan(); !errors.Is(err, boom) {
		t.Fatalf("want the original error, got %v", err)
	}
}

func TestPgxRow_SuccessIsNil(t *testing.T) {
	if err := (&pgxRow{row: scanErrRow{}}).Scan(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
