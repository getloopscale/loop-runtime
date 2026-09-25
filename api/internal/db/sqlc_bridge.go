// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// ErrNotSqlcBridge reports a store handed a DBTX that cannot run sqlc
// queries: a test fake, or a handle wired without PgxAdapter / PgxTxAdapter.
var ErrNotSqlcBridge = errors.New("DBTX does not implement SqlcBridge")

// Generated resolves the sqlc handle behind a store's DBTX. It is the one
// place the resolution happens, with one policy: a handle that is not a
// SqlcBridge (nil included) yields a DBTX whose every query fails with
// ErrNotSqlcBridge, naming the handle's type — so a mis-wired store returns
// an error from the call that used it, never a panic and never a silently
// dropped write.
func Generated(target DBTX) generated.DBTX {
	if bridge, ok := target.(SqlcBridge); ok {
		return bridge.GeneratedDB()
	}
	return unbridgedDBTX{err: fmt.Errorf("%w: %T", ErrNotSqlcBridge, target)}
}

type unbridgedDBTX struct{ err error }

func (u unbridgedDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, u.err
}

func (u unbridgedDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, u.err
}

func (u unbridgedDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return unbridgedRow(u)
}

type unbridgedRow struct{ err error }

func (r unbridgedRow) Scan(...any) error { return r.err }
