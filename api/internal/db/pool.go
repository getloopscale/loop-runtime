package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a PgxAdapter wrapping a new pgxpool.Pool connected to the given URL.
func NewPool(ctx context.Context, databaseURL string) (*PgxAdapter, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create connection pool: %w", err)
	}
	return &PgxAdapter{Pool: pool}, nil
}
