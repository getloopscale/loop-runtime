// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

func TestPostgresSecretStore_ListNames(t *testing.T) {
	t.Run("returns secrets matching tenant, project, and environment", func(t *testing.T) {
		// Arrange
		fakeRows := &fakeRowsImpl{
			rows: [][]any{
				{"DATABASE_URL", "production", "api"},
				{"REDIS_URL", "production", "api"},
			},
		}
		executor := &fakeQueryExecutorForSecrets{
			rows: fakeRows,
		}
		store := &db.PostgresSecretStore{DB: executor}

		// Act
		result, err := store.ListNames(context.Background(), "acme", "proj-1", "production")

		// Assert
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 secrets, got %d", len(result))
		}
		if result[0].Name != "DATABASE_URL" {
			t.Errorf("expected first secret name 'DATABASE_URL', got '%s'", result[0].Name)
		}
		if result[0].Environment != "production" {
			t.Errorf("expected first secret environment 'production', got '%s'", result[0].Environment)
		}
		if result[0].App != "api" {
			t.Errorf("expected first secret app 'api', got '%s'", result[0].App)
		}
		if result[1].Name != "REDIS_URL" {
			t.Errorf("expected second secret name 'REDIS_URL', got '%s'", result[1].Name)
		}

		// Verify query parameters were passed correctly
		if executor.capturedArgs[0] != "acme" {
			t.Errorf("expected tenantID 'acme', got '%v'", executor.capturedArgs[0])
		}
		if executor.capturedArgs[1] != "proj-1" {
			t.Errorf("expected projectID 'proj-1', got '%v'", executor.capturedArgs[1])
		}
		if executor.capturedArgs[2] != "production" {
			t.Errorf("expected env 'production', got '%v'", executor.capturedArgs[2])
		}
	})

	t.Run("returns empty slice when no secrets found", func(t *testing.T) {
		// Arrange
		fakeRows := &fakeRowsImpl{rows: [][]any{}}
		executor := &fakeQueryExecutorForSecrets{rows: fakeRows}
		store := &db.PostgresSecretStore{DB: executor}

		// Act
		result, err := store.ListNames(context.Background(), "acme", "proj-1", "staging")

		// Assert
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil empty slice, got nil")
		}
		if len(result) != 0 {
			t.Fatalf("expected 0 secrets, got %d", len(result))
		}
	})

	t.Run("returns error when query fails", func(t *testing.T) {
		// Arrange
		executor := &fakeQueryExecutorForSecrets{
			queryErr: errors.New("connection refused"),
		}
		store := &db.PostgresSecretStore{DB: executor}

		// Act
		_, err := store.ListNames(context.Background(), "acme", "proj-1", "production")

		// Assert
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, executor.queryErr) {
			t.Errorf("expected wrapped connection error, got: %v", err)
		}
	})

	t.Run("returns error when row scan fails", func(t *testing.T) {
		// Arrange
		fakeRows := &fakeRowsImpl{
			rows:    [][]any{{"DATABASE_URL", "production", "api"}},
			scanErr: errors.New("type mismatch"),
		}
		executor := &fakeQueryExecutorForSecrets{rows: fakeRows}
		store := &db.PostgresSecretStore{DB: executor}

		// Act
		_, err := store.ListNames(context.Background(), "acme", "proj-1", "production")

		// Assert
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error when rows iteration fails", func(t *testing.T) {
		// Arrange
		fakeRows := &fakeRowsImpl{
			rows:   [][]any{{"DATABASE_URL", "production", "api"}},
			rowErr: errors.New("broken pipe"),
		}
		executor := &fakeQueryExecutorForSecrets{rows: fakeRows}
		store := &db.PostgresSecretStore{DB: executor}

		// Act
		_, err := store.ListNames(context.Background(), "acme", "proj-1", "production")

		// Assert
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

// --- Test doubles ---

// fakeQueryExecutorForSecrets implements db.QueryExecutor for secret store tests.
type fakeQueryExecutorForSecrets struct {
	rows         *fakeRowsImpl
	queryErr     error
	capturedSQL  string
	capturedArgs []any
}

func (f *fakeQueryExecutorForSecrets) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	f.capturedSQL = sql
	f.capturedArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows, nil
}

// fakeRowsImpl implements db.Rows for testing.
type fakeRowsImpl struct {
	rows    [][]any
	idx     int
	scanErr error
	rowErr  error
	closed  bool
}

func (f *fakeRowsImpl) Next() bool {
	return f.idx < len(f.rows)
}

func (f *fakeRowsImpl) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	row := f.rows[f.idx]
	for i, d := range dest {
		switch v := d.(type) {
		case *string:
			*v = row[i].(string)
		}
	}
	f.idx++
	return nil
}

func (f *fakeRowsImpl) Err() error {
	return f.rowErr
}

func (f *fakeRowsImpl) Close() {
	f.closed = true
}

// Verify fakeQueryExecutorForSecrets satisfies the interface at compile time.
var _ db.QueryExecutor = (*fakeQueryExecutorForSecrets)(nil)
var _ db.Rows = (*fakeRowsImpl)(nil)

// Verify PostgresSecretStore satisfies the routes.SecretStore interface.
var _ routes.SecretStore = (*db.PostgresSecretStore)(nil)
