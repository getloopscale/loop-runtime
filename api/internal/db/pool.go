package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig holds configurable database pool parameters.
// All values have sensible defaults set by config.Load().
type PoolConfig struct {
	MaxConns         int32
	MinConns         int32
	MaxConnIdleTime  time.Duration
	MaxConnLifetime  time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout   time.Duration
	StatementTimeout string // milliseconds as string for PostgreSQL runtime param
}

// NewPool creates a PgxAdapter wrapping a new pgxpool.Pool connected to the given URL.
//
// Pool configuration follows spec §7.8:
//   - MaxConns: upper bound for concurrent connections
//   - MinConns: keep warm connections to avoid cold-start latency
//   - MaxConnIdleTime: reclaim idle connections to prevent pool bloat
//   - MaxConnLifetime: rotate connections to pick up DNS/config changes
//   - HealthCheckPeriod: detect stale connections before they're used
//   - ConnectTimeout: fail fast if PostgreSQL is unreachable
//   - StatementTimeout: safety net against runaway queries
func NewPool(ctx context.Context, databaseURL string, poolCfg PoolConfig) (*PgxAdapter, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: failed to parse database URL: %w", err)
	}

	cfg.MaxConns = poolCfg.MaxConns
	cfg.MinConns = poolCfg.MinConns
	cfg.MaxConnIdleTime = poolCfg.MaxConnIdleTime
	cfg.MaxConnLifetime = poolCfg.MaxConnLifetime
	cfg.HealthCheckPeriod = poolCfg.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = poolCfg.ConnectTimeout
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = poolCfg.StatementTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create connection pool: %w", err)
	}
	return &PgxAdapter{Pool: pool}, nil
}
