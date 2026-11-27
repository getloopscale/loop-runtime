// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsPGUniqueViolation checks if the err is a Postgres unique-violation
// error (SQLSTATE 23505). Unlike the legacy `db.UniqueViolationError`
// pathway, sqlc-backed queries flow through pgx-native types directly
// (NOT through PgxAdapter.translatePgError), so we must inspect the raw
// pgconn.PgError. Used by Create to surface ErrDeployInProgress on
// concurrent-deploy collision (Rule 6 + ARCH-002).
func IsPGUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	// Translated form (legacy callers that wrap raw pg errors through
	// PgxAdapter still surface UniqueViolationError; preserved here for
	// defense-in-depth even though sqlc-direct path shouldn't hit it).
	var uv *UniqueViolationError
	return errors.As(err, &uv)
}
