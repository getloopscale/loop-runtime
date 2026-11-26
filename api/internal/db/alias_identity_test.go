// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
)

// The port moved to db/dbtx and db re-exports it. These MUST be aliases — the
// same type under two names — not definitions.
//
// The distinction is invisible in a diff and total at compile time: with
// `type DBTX = dbtx.DBTX` every one of the 50 signatures naming `db.DBTX`
// keeps matching the single production implementer; with `type DBTX dbtx.DBTX`
// they all stop matching at once. This test is what makes the difference
// visible in a code review rather than in a broken build.
func TestPortTypesAreAliasesNotDefinitions(t *testing.T) {
	cases := map[string][2]reflect.Type{
		"DBTX":              {reflect.TypeOf((*db.DBTX)(nil)).Elem(), reflect.TypeOf((*dbtx.DBTX)(nil)).Elem()},
		"QueryExecutor":     {reflect.TypeOf((*db.QueryExecutor)(nil)).Elem(), reflect.TypeOf((*dbtx.QueryExecutor)(nil)).Elem()},
		"ExecQueryExecutor": {reflect.TypeOf((*db.ExecQueryExecutor)(nil)).Elem(), reflect.TypeOf((*dbtx.ExecQueryExecutor)(nil)).Elem()},
		"Rows":              {reflect.TypeOf((*db.Rows)(nil)).Elem(), reflect.TypeOf((*dbtx.Rows)(nil)).Elem()},
		"Row":               {reflect.TypeOf((*db.Row)(nil)).Elem(), reflect.TypeOf((*dbtx.Row)(nil)).Elem()},
		"ExecResult":        {reflect.TypeOf(db.ExecResult{}), reflect.TypeOf(dbtx.ExecResult{})},
		// Included for symmetry even though its mutation breaks the compiler
		// (the alias carries the Error() method; a definition would not, and
		// pgx_adapter.go stops compiling). Leaving one of the seven out would
		// invite the reader to assume the omission means something.
		"UniqueViolationError": {reflect.TypeOf(db.UniqueViolationError{}), reflect.TypeOf(dbtx.UniqueViolationError{})},
	}
	for name, pair := range cases {
		if pair[0] != pair[1] {
			t.Errorf("db.%s must be an ALIAS of dbtx.%s, not a separate definition — a definition creates a distinct type and breaks every signature naming it; got %v vs %v",
				name, name, pair[0], pair[1])
		}
	}
}

// Sentinel errors are aliased as variables, not copied: a caller doing
// errors.Is against either name must match the same value.
func TestSentinelErrorsAreTheSameValue(t *testing.T) {
	if !errors.Is(db.ErrNoRows, dbtx.ErrNoRows) {
		t.Error("db.ErrNoRows must BE dbtx.ErrNoRows — a copy would silently break every errors.Is in the tree")
	}
	if !errors.Is(db.ErrNilTxStatusMutation, dbtx.ErrNilTxStatusMutation) {
		t.Error("db.ErrNilTxStatusMutation must BE dbtx.ErrNilTxStatusMutation")
	}
}
