package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a PgxAdapter wrapping a new pgxpool.Pool connected to the given URL.
//
// Pool configuration follows spec §7.8:
//   - MaxConns=20: upper bound for concurrent connections
//   - MinConns=5: keep warm connections to avoid cold-start latency
//   - MaxConnIdleTime=30s: reclaim idle connections to prevent pool bloat
//   - MaxConnLifetime=1h: rotate connections to pick up DNS/config changes
//   - HealthCheckPeriod=30s: detect stale connections before they're used
//   - ConnectTimeout=5s: fail fast if PostgreSQL is unreachable
//   - statement_timeout=30s: safety net against runaway queries
func NewPool(ctx context.Context, databaseURL string) (*PgxAdapter, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: failed to parse database URL: %w", err)
	}

	cfg.MaxConns = 20
	cfg.MinConns = 5
	cfg.MaxConnIdleTime = 30 * time.Second
	cfg.MaxConnLifetime = 1 * time.Hour
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	// statement_timeout: safety net — kills any query running longer than 30s (spec §7.8)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create connection pool: %w", err)
	}
	return &PgxAdapter{Pool: pool}, nil
}
