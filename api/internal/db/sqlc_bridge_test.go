// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// plainDBTX satisfies DBTX and not SqlcBridge — a test fake or a mis-wired store.
type plainDBTX struct{}

func (plainDBTX) Exec(context.Context, string, ...any) (ExecResult, error) { return ExecResult{}, nil }
func (plainDBTX) Query(context.Context, string, ...any) (Rows, error)      { return nil, nil }
func (plainDBTX) QueryRow(context.Context, string, ...any) Row             { return nil }

type fixedGenerated struct{ generated.DBTX }

type bridgedDBTX struct {
	plainDBTX
	gen generated.DBTX
}

func (b bridgedDBTX) GeneratedDB() generated.DBTX { return b.gen }

func TestGeneratedReturnsTheBridgedHandle(t *testing.T) {
	want := fixedGenerated{}
	if got := Generated(bridgedDBTX{gen: want}); got != generated.DBTX(want) {
		t.Fatalf("Generated returned %T, want the bridge's own handle", got)
	}
}

func TestGeneratedFailsEveryQueryOnANonBridgeHandle(t *testing.T) {
	ctx := context.Background()
	g := Generated(plainDBTX{})

	_, execErr := g.Exec(ctx, "SELECT 1")
	rows, queryErr := g.Query(ctx, "SELECT 1")
	if rows != nil {
		rows.Close()
	}
	scanErr := g.QueryRow(ctx, "SELECT 1").Scan()

	for name, err := range map[string]error{"Exec": execErr, "Query": queryErr, "QueryRow.Scan": scanErr} {
		if !errors.Is(err, ErrNotSqlcBridge) {
			t.Errorf("%s error = %v, want ErrNotSqlcBridge", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), "db.plainDBTX") {
			t.Errorf("%s error %q does not name the handle's type", name, err)
		}
	}
}

func TestGeneratedFailsOnANilHandle(t *testing.T) {
	_, err := Generated(nil).Exec(context.Background(), "SELECT 1")
	if !errors.Is(err, ErrNotSqlcBridge) {
		t.Fatalf("Exec on a nil handle = %v, want ErrNotSqlcBridge", err)
	}
}
